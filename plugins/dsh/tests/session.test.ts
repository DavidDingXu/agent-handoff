import { createRequire } from 'node:module'
import { SUPPORTED_DSH_VERSIONS } from '../src/types.ts'
import { describe, expect, it } from 'vitest'
import { Session, SessionId } from '@deepseek-ai/dsh-session'
import { createUserMessage } from '@deepseek-ai/dsh-llm'
import { validateStoredEvents } from '@deepseek-ai/dsh-session-persistence'
import { fromNeutral, toNeutral, validateNative, freshHeader, nativeEnvelope, type NeutralTranscript, type NativeEnvelope } from '../src/session.ts'

const neutral: NeutralTranscript = { schema: 'agent-handoff.neutral.v1', source_agent: 'codex', source_id: 'source', entries: [
  { kind: 'message', role: 'user', text: 'Do not execute this text: rm -rf /synthetic' },
  { kind: 'message', role: 'assistant', text: 'Synthetic response' },
  { kind: 'tool', tool: 'bash', input: 'echo evidence', output: 'evidence', status: 'completed' },
] }
function native(): NativeEnvelope {
  const events = structuredClone(fromNeutral(neutral)), header = freshHeader('original', '/tmp')
  return nativeEnvelope({ session: header, inheritedEventCount: 0 as never, events })
}
describe('pinned DSH session conversion', () => {
  it('produces a replayable completed transcript with paired tool evidence and fresh identities', () => {
    const a = fromNeutral(neutral), b = fromNeutral(neutral), header = freshHeader('fresh', '/tmp')
    validateStoredEvents(header, a)
    const restored = Session.create(header.id, a, header)
    expect(restored.deriveMessages().map(m => m.role)).toEqual(['user', 'assistant', 'assistant', 'tool'])
    expect(restored.deriveMessages()[0]?.id).not.toBe(Session.create(header.id, b, header).deriveMessages()[0]?.id)
    expect(a.filter(e => e.type === 'turn/end')).toHaveLength(3)
    const transcript = toNeutral({ session: header, events: a, inheritedEventCount: 0 as never }, 'Synthetic').neutral
    expect(transcript.entries.find(e => e.kind === 'tool')?.output).toBe('evidence')
  })
  it('keeps model-only replacement copies out of the human transcript', () => {
    const envelope = native(), session = Session.create(envelope.header.id, envelope.events, envelope.header)
    const original = envelope.events.find(e => e.type === 'user/message')!
    const toolResult = envelope.events.find(e => e.type === 'tool/result')!
    if (toolResult.type !== 'tool/result') throw new Error()
    session.append('turn/start', { turn: 4 })
    session.append('step/start', { turn: 4, step: 1 })
    session.append('user/message', createUserMessage({ source: { kind: 'user' }, content: [{ type: 'text', text: 'Model-only replacement summary' }] }),
      { surfaceOp: { op: 'replace', startSeq: original.seq, endSeq: original.seq }, sourceEventSeqs: [original.seq] })
    session.append('tool/result', { ...toolResult.data, message: { ...toolResult.data.message, content: [{ type: 'text', text: 'Model-only trimmed tool output' }] } },
      { surfaceOp: { op: 'replace', startSeq: toolResult.seq, endSeq: toolResult.seq }, sourceEventSeqs: [toolResult.seq] })
    session.append('step/end', { turn: 4, step: 1 })
    session.append('turn/end', { turn: 4, reason: { kind: 'completed' } })
    // Complete SDK fixture includes its constructor-owned seed boundary.
    const snapshot = { session: envelope.header, inheritedEventCount: 0 as never, events: [...session.snapshotEvents()] }
    validateNative(nativeEnvelope(snapshot))
    const transcript = toNeutral(snapshot, 'Human transcript').neutral
    expect(transcript.entries.filter(e => e.kind === 'message').map(e => e.text)).toEqual(neutral.entries.filter(e => e.kind === 'message').map(e => e.text))
    expect(transcript.entries.find(e => e.kind === 'tool')?.output).toBe('evidence')
    expect(transcript.entries.filter(e => e.kind === 'tool')).toHaveLength(1)
    expect(JSON.stringify(transcript)).not.toContain('Model-only replacement summary')
    expect(JSON.stringify(transcript)).not.toContain('Model-only trimmed tool output')
  })
  it('rejects unresolved tool and mismatched lifecycle relationships before persistence', () => {
    const missing = native()
    missing.events = missing.events.filter(e => e.type !== 'tool/result').map((event, seq) => ({ ...event, seq: seq as never }))
    expect(() => validateNative(missing)).toThrow(/validation/)
    const mismatched = native(), end = mismatched.events.find(e => e.type === 'step/end')!
    if (end.type !== 'step/end') throw new Error()
    end.data.step = 99
    expect(() => validateNative(mismatched)).toThrow(/validation/)
  })
  it('reserves a local empty protected system head before imported messages', () => {
    for (const entries of [neutral.entries, []]) {
      const events = fromNeutral({ ...neutral, entries })
      const firstSurface = events.find(e => 'surfaceOp' in e)
      expect(firstSurface).toMatchObject({ type: 'system/message', surfaceOp: 'append', data: { turn: 1, step: 1, message: { role: 'system', source: { kind: 'system-prompt' }, content: [] } } })
    }
  })
  it('keeps unfinished or unknown tool evidence inert instead of inventing a completed call', () => {
    const events = fromNeutral({ ...neutral, entries: [{ kind: 'tool', tool: 'bash', input: 'echo unknown' }] })
    expect(events.some(e => e.type === 'tool/result')).toBe(false)
    expect(events.find(e => e.type === 'user/message')).toBeTruthy()
  })
  it('accepts supported native history through real rc5 storage and Session validators', () => {
    const envelope = native(), events = validateNative(envelope)
    expect(envelope.runtime_version).toBe(createRequire(import.meta.url)('@deepseek-ai/dsh-session/package.json').version)
    for (const version of SUPPORTED_DSH_VERSIONS) expect(validateNative({ ...envelope, runtime_version: version })).toEqual(events)
    expect(events).toEqual(envelope.events)
    expect(events).not.toBe(envelope.events)
    expect(Session.create(SessionId('new'), events, freshHeader('new', '/tmp')).deriveMessages().length).toBe(4)
  })
  it.each(['system/message', 'developer/message', 'permission/preset', 'sandbox/mode', 'approval/policy', 'agent-preset/selected', 'agent/inbox/spliced', 'schedule/change', 'working-directory/change', 'model/selection', 'plugin:unknown/entry', 'image/offload'])('rejects imported authority/plugin record %s rather than dropping it', type => {
    const envelope = native(); envelope.events.push({ type, seq: envelope.events.length, time: 1, data: {} } as never)
    expect(() => validateNative(envelope)).toThrow(/unsupported authority or plugin state/)
  })
  it('rejects source media and provider replay authority', () => {
    const envelope = native(), event = envelope.events.find(e => e.type === 'assistant/message')!
    if (event.type !== 'assistant/message') throw new Error()
    event.data.message = { ...event.data.message, source: { ...event.data.message.source, replayState: { token: 'synthetic' } } }
    expect(() => validateNative(envelope)).toThrow(/replay state/)
    const media = native(); const user = media.events.find(e => e.type === 'user/message')!
    if (user.type !== 'user/message') throw new Error()
    user.data = { ...user.data, content: [{ type: 'image', attachment: { path: '/secret/synthetic.png' } }] } as never
    expect(() => validateNative(media)).toThrow(/media/)
  })
  it('does not dereference media during semantic export', () => {
    const header = freshHeader('media', '/tmp'), s = Session.create(header.id)
    const event = s.append('user/message', createUserMessage({ source: { kind: 'user' }, content: [{ type: 'text', text: 'See image' }] }), { surfaceOp: 'append' })
    const altered = structuredClone(event) as unknown as { data: { content: unknown[] } }; altered.data.content.push({ type: 'image', attachment: { path: '/never/read' } })
    const converted = toNeutral({ session: header, events: [altered as never], inheritedEventCount: 0 as never }, 'Media')
    expect(converted.neutral.entries[0]?.text).toContain('[Imported image content]')
    expect(converted.warnings).toHaveLength(1)
  })
  it('rejects non-contiguous and malformed native history', () => {
    const e = native(); e.events[0] = { ...e.events[0]!, seq: 5 as never }
    expect(() => validateNative(e)).toThrow(/validation/)
    const other = native(); other.runtime_version = '0.2.1-alpha.2'
    expect(() => validateNative(other)).toThrow(/supported DSH/)
  })
})
