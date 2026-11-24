/** Pinned SDK transcript conversion. Imported state never supplies runtime authority. */
import { randomUUID } from 'node:crypto'
import { createRequire } from 'node:module'
import { Session, SessionId, SessionLogOffset, SESSION_FORMAT_VERSION, type SessionEvent, type SessionHeader, type SessionEventType, type SessionEventMap, type SurfaceEventType, type SurfaceIntent, KNOWN_SESSION_EVENT_TYPES, isSurfaceEvent, isAppendSurfaceEvent } from '@deepseek-ai/dsh-session'
import { createUserMessage, createAssistantMessage, createToolResultMessage, createSystemMessage, type ToolCallId } from '@deepseek-ai/dsh-llm'
import { restoreReleasedV4Artifact } from '@deepseek-ai/dsh-session-format-v3-to-v4'
import { assertContiguous, validateStoredEvents } from '@deepseek-ai/dsh-session-persistence'
import { SUPPORTED_DSH_VERSIONS, type TaskBrief, type CodePayload } from './types.ts'

export interface NeutralEntry { kind: 'message' | 'tool'; role?: string; text?: string; tool?: string; status?: string; input?: string; output?: string; timestamp?: string }
export interface NeutralTranscript { schema: 'agent-handoff.neutral.v1'; source_agent: string; source_id: string; title?: string; source_cwd?: string; entries: NeutralEntry[] }
export interface NativeEnvelope {
  schema: 'agent-handoff.dsh.native.v1'; runtime_version: string; session_format_version: number
  header: SessionHeader; inherited_event_count: number; events: SessionEvent[]
}
export interface SessionSnapshot { session: SessionHeader; inheritedEventCount: SessionLogOffset; events: SessionEvent[] }
export interface ReadResult {
  schema: 'agent-handoff.bridge.read-result.v1'; status: 'ok'; checksum_status: string; fingerprint: string
  manifest: { source_agent: string; source_thread_id: string; title: string; message_count: number; source_cwd?: string }
  native: NativeEnvelope | null; neutral: NeutralTranscript; source_session: string; warnings: string[]
  brief?: TaskBrief; code?: CodePayload | null
}
export class HandoffFailure extends Error {
  constructor(readonly code: string, message: string, readonly recoverySessionId?: string, readonly recoveryDirectory?: string) { super(message); this.name = 'HandoffFailure' }
}
function fail(message: string): never { throw new HandoffFailure('invalid_session', message) }
export function record(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value) }
export function installedDshVersion(): string {
  const sdk = createRequire(import.meta.url)('@deepseek-ai/dsh-session/package.json') as { version: string }
  if (!SUPPORTED_DSH_VERSIONS.some(version => version === sdk.version)) throw new HandoffFailure('unsupported_runtime', `This plugin requires DSH ${SUPPORTED_DSH_VERSIONS.join(' or ')}.`)
  return sdk.version
}
export function nativeEnvelope(snapshot: SessionSnapshot): NativeEnvelope {
  return { schema: 'agent-handoff.dsh.native.v1', runtime_version: installedDshVersion(), session_format_version: SESSION_FORMAT_VERSION,
    header: snapshot.session, inherited_event_count: snapshot.inheritedEventCount, events: snapshot.events }
}
/** Text-only conversion deliberately labels media rather than reading source paths or URLs. */
function visibleContent(value: unknown, warnings: Set<string>): string {
  if (!Array.isArray(value)) return ''
  return value.map(block => {
    if (!record(block)) return ''
    if (block.type === 'text' && typeof block.text === 'string') return block.text
    if (block.type === 'reasoning' || block.type === 'tool-call') return ''
    warnings.add('Source media and non-text content remain inert labels; no source files or URLs were opened.')
    return `[Imported ${typeof block.type === 'string' ? block.type : 'non-text'} content]`
  }).filter(Boolean).join('\n')
}
export function toNeutral(snapshot: SessionSnapshot, title: string): { neutral: NeutralTranscript; warnings: string[] } {
  const warnings = new Set<string>(), entries: NeutralEntry[] = [], calls = new Map<string, NeutralEntry>(), seen = new Set<string>()
  for (const event of snapshot.events) {
    // Replacements are model-context copies; human history retains the append-origin evidence.
    if (isSurfaceEvent(event) && !isAppendSurfaceEvent(event)) continue
    const d = event.data as unknown
    if (!record(d)) continue
    const message = event.type === 'user/message' ? d : record(d.message) ? d.message : undefined
    if (event.type === 'user/message' || event.type === 'assistant/message' || event.type === 'system/message' || event.type === 'developer/message') {
      if (!message) continue
      if (typeof message.id === 'string' && seen.has(message.id)) continue
      if (typeof message.id === 'string') seen.add(message.id)
      const text = visibleContent(message.content, warnings)
      if (text) entries.push({ kind: 'message', role: event.type === 'assistant/message' ? 'assistant' : 'user',
        text: event.type === 'system/message' || event.type === 'developer/message' ? `[Historical imported ${message.role} context; not local runtime instructions]\n${text}` : text,
        timestamp: new Date(event.time).toISOString() })
      if (event.type === 'assistant/message' && Array.isArray(message.content)) {
        for (const block of message.content) if (record(block) && block.type === 'tool-call' && typeof block.id === 'string') {
          const entry: NeutralEntry = { kind: 'tool', tool: typeof block.name === 'string' ? block.name : 'unknown', status: 'called', input: typeof block.arguments === 'string' ? block.arguments : '' }
          entries.push(entry); calls.set(block.id, entry)
        }
      }
    } else if (event.type === 'tool/call' && typeof d.callId === 'string' && !calls.has(d.callId)) {
      const entry: NeutralEntry = { kind: 'tool', tool: typeof d.name === 'string' ? d.name : 'unknown', status: 'called', input: typeof d.arguments === 'string' ? d.arguments : '' }
      entries.push(entry); calls.set(d.callId, entry)
    } else if (event.type === 'tool/result' && message) {
      const callId = String(message.toolCallId ?? ''), entry = calls.get(callId)
      const result = { output: visibleContent(message.content, warnings), status: message.isError ? 'failed' : 'completed' }
      if (entry) { Object.assign(entry, result); calls.delete(callId) }
      else entries.push({ kind: 'tool', tool: 'unknown', ...result })
    }
  }
  return { neutral: { schema: 'agent-handoff.neutral.v1', source_agent: 'dsh', source_id: snapshot.session.id, title,
    source_cwd: snapshot.session.cwd, entries }, warnings: [...warnings] }
}
const passiveEvents = new Set(['turn/start', 'turn/end', 'step/start', 'step/end', 'user/message', 'assistant/message', 'assistant/attempt', 'tool/call', 'tool/result', 'system/message', 'session/end-seed', 'session/title'])
/** Fail closed: SDK-known events can still restore permissions, queued work or plugins. */
export function validateNative(native: NativeEnvelope): SessionEvent[] {
  if (!record(native) || native.schema !== 'agent-handoff.dsh.native.v1') fail('Invalid native session envelope.')
  if (!SUPPORTED_DSH_VERSIONS.some(version => version === native.runtime_version) || native.session_format_version !== SESSION_FORMAT_VERSION) throw new HandoffFailure('native_unsupported', 'Native history requires the supported DSH runtime and session format.')
  if (!record(native.header) || !Array.isArray(native.events) || native.events.length > 100_000 || !Number.isSafeInteger(native.inherited_event_count) || native.inherited_event_count < 0) fail('Invalid native session envelope.')
  if (native.header.isSeeded || native.inherited_event_count !== 0 || native.header.origin === 'subagent') throw new HandoffFailure('native_unsupported', 'Fork and subagent lineage require conversation import in this version.')
  const owned = structuredClone(native.events)
  const firstSurface = owned.find(event => 'surfaceOp' in event)
  if (firstSurface && firstSurface.type !== 'system/message') throw new HandoffFailure('native_unsupported', 'Native message history needs an empty protected system head; choose conversation import.')
  for (const event of owned) {
    if (!record(event) || !passiveEvents.has(event.type) || event.ignorable !== undefined) throw new HandoffFailure('native_unsupported', 'Native history contains unsupported authority or plugin state. Choose conversation import explicitly.')
    const d = event.data as unknown
    if (!record(d)) fail('Invalid native event payload.')
    // An empty first system node reserves the SDK slot but carries no source instructions.
    if (event.type === 'system/message' && (event !== firstSurface || event.surfaceOp !== 'append' || !record(d.message) || !Array.isArray(d.message.content) || d.message.content.length !== 0)) throw new HandoffFailure('native_unsupported', 'Native history contains unsupported authority or plugin state: source system instructions are not imported.')
    if (event.type === 'tool/result' && ('meta' in d || 'error' in d)) throw new HandoffFailure('native_unsupported', 'Tool-private state is not supported by native import.')
    const m = event.type === 'user/message' ? d : record(d.message) ? d.message : undefined
    if (m) {
      if (record(m.source) && 'replayState' in m.source) throw new HandoffFailure('native_unsupported', 'Provider replay state cannot be imported natively.')
      if (!Array.isArray(m.content) || m.content.some(b => !record(b) || !['text', 'reasoning', 'tool-call'].includes(String(b.type)))) throw new HandoffFailure('native_unsupported', 'Source media or offload references require conversation import.')
    }
    if (event.type === 'session/end-seed' && d.inherited === true) throw new HandoffFailure('native_unsupported', 'Fork lineage requires conversation import in this version.')
  }
  try {
    const id = SessionId(String(native.header.id))
    assertContiguous(id, owned, 0)
    validateStoredEvents(native.header, owned)
    Session.create(id, owned, native.header, SessionLogOffset(native.inherited_event_count))
    validateSessionRelationships(native.header, owned, native.inherited_event_count)
  } catch { fail('Native history failed DSH session validation.') }
  return owned
}
export function nativeSupport(native: NativeEnvelope | null): { supported: boolean; warning?: string } {
  if (!native) return { supported: false }
  try { validateNative(native); return { supported: true } } catch (e) {
    if (!(e instanceof HandoffFailure) || e.code !== 'native_unsupported') throw e
    return { supported: false, warning: e instanceof HandoffFailure ? e.message : 'Native history is not supported; choose conversation import.' }
  }
}
/** Detached Session checks do not cover durable cross-event tool/lifecycle relations. */
export function validateSessionRelationships(header: SessionHeader, events: readonly SessionEvent[], inheritedEventCount = 0): void {
  try {
    // The official released-format validator receives the same lossless JSON boundary
    // that the persistence codec reads; it validates without modifying that artifact.
    const artifact: Parameters<typeof restoreReleasedV4Artifact>[0] = JSON.parse(JSON.stringify({
      header: { ...header, delegationDepth: header.delegationDepth ?? 0 }, events, inheritedEventCount,
    }))
    restoreReleasedV4Artifact(artifact, KNOWN_SESSION_EVENT_TYPES)
  } catch { fail('Native history failed DSH durable relationship validation.') }
}
export function freshHeader(id: string, cwd: string): SessionHeader {
  return { version: SESSION_FORMAT_VERSION, id: SessionId(id), createdAt: Date.now(), cwd, isSeeded: false }
}
/** Validate preview data without allocating an entire converted Session before confirmation. */
function validateNeutral(neutral: NeutralTranscript): void {
  if (!record(neutral) || neutral.schema !== 'agent-handoff.neutral.v1' || !Array.isArray(neutral.entries) || neutral.entries.length > 100_000) fail('Invalid neutral transcript.')
  for (const entry of neutral.entries) {
    if (!record(entry) || !['message', 'tool'].includes(entry.kind)) fail('Invalid neutral entry.')
    if (entry.kind === 'message' && (typeof entry.text !== 'string' || !['user', 'assistant'].includes(String(entry.role)))) fail('Invalid neutral message.')
    if (entry.kind === 'tool') {
      if (entry.status !== undefined && !['called', 'completed', 'failed'].includes(String(entry.status))) fail('Invalid tool evidence status.')
      if (entry.input !== undefined && typeof entry.input !== 'string' || entry.output !== undefined && typeof entry.output !== 'string') fail('Invalid tool evidence.')
    }
  }
}
/** New events describe completed evidence; no imported tool or prompt is dispatched. */
export function fromNeutral(neutral: NeutralTranscript): SessionEvent[] {
  validateNeutral(neutral)
  const s = Session.create(SessionId(`convert-${randomUUID()}`)), events: SessionEvent[] = []
  function add<T extends SessionEventType>(type: T, data: SessionEventMap[T], ...opts: T extends SurfaceEventType ? [opts: SurfaceIntent<T>] : []): void {
    events.push(s.append<T>(type, data, ...opts))
  }
  let turn = 0
  for (const entry of neutral.entries) {
    if (!record(entry) || !['message', 'tool'].includes(entry.kind)) fail('Invalid neutral entry.')
    turn++
    add('turn/start', { turn }); add('step/start', { turn, step: 1 })
    // This locally-created empty node is mandatory for cold replay after the next real turn.
    // It reserves surface node 0 without installing any instructions from the source.
    if (turn === 1) add('system/message', { turn, step: 1, message: createSystemMessage('') }, { surfaceOp: 'append' })
    if (entry.kind === 'message') {
      if (typeof entry.text !== 'string' || !['user', 'assistant'].includes(String(entry.role))) fail('Invalid neutral message.')
      if (entry.role === 'assistant') add('assistant/message', { turn, step: 1, stream: [], message: createAssistantMessage({ content: [{ type: 'text', text: entry.text }], source: { provider: 'agent-handoff', model: 'imported-history' } }) }, { surfaceOp: 'append' })
      else add('user/message', createUserMessage({ source: { kind: 'user' }, content: [{ type: 'text', text: entry.text }] }), { surfaceOp: 'append' })
    } else {
      if (entry.status !== undefined && !['called', 'completed', 'failed'].includes(String(entry.status))) fail('Invalid tool evidence status.')
      const name = typeof entry.tool === 'string' && entry.tool ? entry.tool : 'unknown'
      if (entry.input !== undefined && typeof entry.input !== 'string' || entry.output !== undefined && typeof entry.output !== 'string') fail('Invalid tool evidence.')
      if (!['completed', 'failed'].includes(String(entry.status)) || !entry.input) {
        add('user/message', createUserMessage({ source: { kind: 'user' }, content: [{ type: 'text', text: `[Imported tool evidence: ${name}]\nInput: ${entry.input ?? ''}\nOutput: ${entry.output ?? ''}\nStatus: ${entry.status ?? 'unknown'}` }] }), { surfaceOp: 'append' })
      } else {
        const callId = `handoff-${randomUUID()}` as ToolCallId
        const args = JSON.stringify({ imported_input: entry.input })
        add('assistant/message', { turn, step: 1, stream: [], message: createAssistantMessage({ source: { provider: 'agent-handoff', model: 'imported-history' }, content: [{ type: 'tool-call', id: callId, name, arguments: args }] }) }, { surfaceOp: 'append' })
        add('tool/call', { turn, step: 1, callId, name, arguments: args })
        add('tool/result', { turn, step: 1, message: createToolResultMessage({ callId, content: [{ type: 'text', text: entry.output ?? '' }], isError: entry.status === 'failed' }) }, { surfaceOp: 'append' })
      }
    }
    add('step/end', { turn, step: 1 }); add('turn/end', { turn, reason: { kind: 'completed' } })
  }
  // A nonempty, balanced log is resumable even for a source without visible messages.
  if (!events.length) {
    add('turn/start', { turn: 1 }); add('step/start', { turn: 1, step: 1 })
    add('system/message', { turn: 1, step: 1, message: createSystemMessage('') }, { surfaceOp: 'append' })
    add('step/end', { turn: 1, step: 1 }); add('turn/end', { turn: 1, reason: { kind: 'completed' } })
  }
  validateSessionRelationships(s.header, events)
  return events
}
export function validateReadResult(value: unknown): ReadResult {
  if (!record(value) || value.schema !== 'agent-handoff.bridge.read-result.v1' || value.status !== 'ok' || typeof value.fingerprint !== 'string' || !/^[a-f0-9]{64}$/.test(value.fingerprint)
    || !['ok', 'not_available_v1'].includes(String(value.checksum_status)) || !record(value.manifest) || !record(value.neutral) || typeof value.source_session !== 'string'
    || !['codex', 'claude', 'dsh', 'qoder'].includes(String(value.manifest.source_agent)) || typeof value.manifest.source_thread_id !== 'string' || !value.manifest.source_thread_id
    || typeof value.manifest.title !== 'string' || !Array.isArray(value.warnings) || value.warnings.some(x => typeof x !== 'string')) throw new HandoffFailure('invalid_bridge_response', 'The verified bundle response is invalid.')
  const result = value as unknown as ReadResult
  if (result.neutral.schema !== 'agent-handoff.neutral.v1' || result.neutral.source_agent !== result.manifest.source_agent || result.neutral.source_id !== result.manifest.source_thread_id) fail('Bundle identities do not match.')
  validateNeutral(result.neutral)
  return result
}
