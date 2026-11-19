/** Real rc1/rc5 JSONL persistence, cold Session replay and controller resume. */
import { afterEach, describe, expect, it } from 'vitest'
import { Context } from '@deepseek-ai/cordis'
import { mountAgentLoopTestDependencies, mountAgentLoopTestHarness } from '@deepseek-ai/dsh-agent-loop-testkit'
import { LlmAdapter, createUserMessage, createSystemMessage, createAssistantMessage, createToolResultMessage, type ToolCallId, type GenerateOptions, type LlmResolvedModelInfo, type StreamChunk } from '@deepseek-ai/dsh-llm'
import type { Agent } from '@deepseek-ai/dsh-agent'
import type {} from '@deepseek-ai/dsh-tools'
import { Session, SessionId, type SessionEvent, type SessionEventType, type SessionEventMap, type SurfaceEventType, type SurfaceIntent } from '@deepseek-ai/dsh-session'
import * as JsonlPersistence from '@deepseek-ai/dsh-session-persistence-jsonl'
import QueryEngine from '@deepseek-ai/dsh-session-query-sqlite'
import AgentDefaultModel from '@deepseek-ai/dsh-agent-default-model'
import SessionTitle from '@deepseek-ai/dsh-session-title'
import { createRequire } from 'node:module'
import { createHash } from 'node:crypto'
import { mkdtemp, mkdir, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { pathToFileURL } from 'node:url'
import { createHandoffService, type HandoffHostContext, type Workspace } from '../src/host.ts'
import { freshHeader, nativeEnvelope, toNeutral, type NeutralTranscript, type ReadResult } from '../src/session.ts'
import { SUPPORTED_DSH_VERSIONS, type HandoffService } from '../src/types.ts'

// rc1's official npm tarball omits src files. Both exact releases provide
// these compiled production controller modules beside their declarations.
const require = createRequire(import.meta.url)
const controllerRoot = dirname(require.resolve('@deepseek-ai/dsh-api-session-controller/package.json'))
const { ApiSessionAgentController } = await import(pathToFileURL(join(controllerRoot, 'lib/types/agent.js')).href)
const { SessionCommandController } = await import(pathToFileURL(join(controllerRoot, 'lib/types/commands.js')).href)
const runtimeVersion = (require('@deepseek-ai/dsh-session/package.json') as { version: string }).version

const roots: string[] = [], contexts: Context[] = [], services: HandoffService[] = []
afterEach(async () => {
  for (const service of services.splice(0).reverse()) await service.dispose()
  for (const ctx of contexts.splice(0).reverse()) await ctx.fiber.dispose()
  for (const root of roots.splice(0)) await rm(root, { recursive: true, force: true })
})
/** Pure in-process model: no HTTP adapter, credential, or download. */
class LocalTextAdapter extends LlmAdapter {
  readonly requests: GenerateOptions[] = []
  override async resolveModel(provider: string, model: string): Promise<LlmResolvedModelInfo> { return { provider, id: model, name: model } }
  override async *stream(request: GenerateOptions): AsyncIterable<StreamChunk> {
    this.requests.push(request); const text = `local continuation ${this.requests.length}`
    yield { type: 'block-start', index: 0, blockType: 'text' }
    yield { type: 'text-delta', index: 0, text }
    yield { type: 'block-end', index: 0, block: { type: 'text', text } }
    yield { type: 'usage', usage: { inputTokens: 10, outputTokens: 4 } }
    yield { type: 'finish', reason: { kind: 'stop' } }
  }
}
async function turn(ctx: Context, agent: Agent, text: string): Promise<void> {
  let off: () => void = () => {}, timer: ReturnType<typeof setTimeout> | undefined
  const done = new Promise<void>((resolve, reject) => {
    timer = setTimeout(() => { off(); reject(new Error('local fixture turn did not reach idle')) }, 5_000)
    off = ctx.on('agent/status', ({ agent: subject, status }) => { if (subject === agent && status === 'idle') { clearTimeout(timer); off(); resolve() } })
  })
  try { agent.followup(createUserMessage({ source: { kind: 'user' }, content: [{ type: 'text', text }] })); await done } finally { clearTimeout(timer); off() }
}
async function stored(ctx: Context, id: string): Promise<readonly SessionEvent[]> {
  const reader = await ctx.sessionPersistence.open(SessionId(id), 'read')
  try { return structuredClone((await reader.read()).events) } finally { await reader.close() }
}
async function files(dir: string): Promise<string[]> {
  return (await Promise.all((await readdir(dir, { withFileTypes: true })).map(async entry => entry.isDirectory() ? files(join(dir, entry.name)) : [join(dir, entry.name)]))).flat()
}
// Construct a genuine SDK-native passive vocabulary fixture, then persist it
// through the actual backend. Ordinary model logs may contain authority events;
// those require the separately tested explicit conversation fallback.
async function passiveNativeSource(ctx: Context, root: string, sourceId: string) {
  const header = freshHeader(sourceId,root), source = Session.create(header.id), events: SessionEvent[]=[]
  // The single-event API is shared by rc1 and rc5; rc1 has no batch overload.
  function add<T extends SessionEventType>(type: T, data: SessionEventMap[T], ...opts: T extends SurfaceEventType ? [opts: SurfaceIntent<T>] : []): void {
    events.push(source.append<T>(type, data, ...opts))
  }
  const callId='historical-inert-probe' as ToolCallId
  add('turn/start',{turn:1});add('step/start',{turn:1,step:1})
  add('system/message',{turn:1,step:1,message:createSystemMessage('')},{surfaceOp:'append'})
  add('user/message',createUserMessage({source:{kind:'user'},content:[{type:'text',text:'Original native DSH question'}]}),{surfaceOp:'append'})
  add('assistant/message',{turn:1,step:1,stream:[],message:createAssistantMessage({source:{provider:'local-fixture',model:'local-fixture'},content:[{type:'tool-call',id:callId,name:'inert_probe',arguments:'{}'}]})},{surfaceOp:'append'})
  add('tool/call',{turn:1,step:1,callId,name:'inert_probe',arguments:'{}'})
  add('tool/result',{turn:1,step:1,message:createToolResultMessage({callId,content:[{type:'text',text:'Imported inert tool evidence'}],isError:false})},{surfaceOp:'append'})
  add('assistant/message',{turn:1,step:1,stream:[],message:createAssistantMessage({source:{provider:'local-fixture',model:'local-fixture'},content:[{type:'text',text:'Original native DSH answer'}]})},{surfaceOp:'append'})
  add('step/end',{turn:1,step:1});add('turn/end',{turn:1,reason:{kind:'completed'}})
  const handle=await ctx.sessionPersistence.create(header)
  try {await handle.append(events);await handle.flush()}finally{await handle.close()}
  return ctx.sessionQuery.readSession(header.id)
}
async function runtime(root: string) {
  const ctx = new Context(); contexts.push(ctx)
  await mountAgentLoopTestDependencies(ctx)
  // Production shutdown order: loop drains before the backend closes.
  await ctx.plugin(JsonlPersistence, { root: join(root, 'sessions'), compression: 'none' })
  await ctx.plugin(QueryEngine, { path: join(root, 'query.sqlite'), openAt: 'never' })
  await ctx.plugin(AgentDefaultModel, { provider: 'local-fixture', model: 'local-fixture' })
  await ctx.plugin(SessionTitle, { fallbackMaxWords: 12, fallbackMaxBytes: 80, maxTitleBytes: 80 })
  const loop = await mountAgentLoopTestHarness(ctx), adapter = new LocalTextAdapter()
  ctx.llm.registerAdapter(['local-fixture'], adapter)
  // The pinned Remote facade delegates to these exact production controllers.
  const agents = new ApiSessionAgentController(ctx), commands = new SessionCommandController(ctx, agents, root)
  let importedToolRuns = 0
  ctx.tools.register({ name: 'inert_probe', description: 'Never execute imported history', parameters: { type: 'object', properties: {} },
    output: { schema: { type: 'string' }, render: (_args, value) => [{ type: 'text', text: String(value) }] },
    async execute() { importedToolRuns++; await writeFile(join(root, 'TOOL_EXECUTED'), 'unexpected'); return 'unexpected' },
  })
  const attached = new Set<string>(), workspacePath = join(root, 'workspace'); await mkdir(workspacePath, { recursive: true })
  const ws: Workspace = { id: 'local-workspace', title: 'Disposable workspace', path: workspacePath, async attachSession(id) { attached.add(id) } }
  const host: HandoffHostContext = { sessionPersistence: ctx.sessionPersistence, sessionQuery: ctx.sessionQuery,
    workspaceRegistry: { list: () => [ws], get: id => id === ws.id ? ws : undefined },
    sessionController: { resolveAgent: id => agents.resolveAgent(id), rename: request => commands.rename(request) } }
  return { ctx, loop, adapter, agents, host, ws, attached, get importedToolRuns() { return importedToolRuns } }
}
async function disposeRuntime(ctx: Context): Promise<void> { await ctx.fiber.dispose(); contexts.splice(contexts.indexOf(ctx), 1) }
async function serviceFor(host: HandoffHostContext, stateDir: string, payload: ReadResult) {
  const service = createHandoffService(host, { stateDir }, { runBridge: async args => {
    expect(args.slice(0, 2)).toEqual(['bridge', 'read'])
    return structuredClone(payload) as unknown as Record<string, unknown>
  } }); services.push(service); return service
}

describe(`DSH ${runtimeVersion} durable handoff runtime`, () => {
  it('uses the exact installed SDK version', () => {
    expect(SUPPORTED_DSH_VERSIONS).toEqual(['0.2.2-rc.1', '0.2.2-rc.5'])
    expect(SUPPORTED_DSH_VERSIONS).toContain(runtimeVersion)
    for (const name of ['dsh-session', 'dsh-session-persistence-jsonl', 'dsh-agent-loop', 'dsh-session-query-sqlite', 'dsh-api-session-controller']) expect(require(`@deepseek-ai/${name}/package.json`).version).toBe(runtimeVersion)
  })
  it('requires explicit conversation fallback for an ordinary native loop log with unsupported source authority', async () => {
    const root=await mkdtemp(join(tmpdir(),'agent-handoff-runtime-'));roots.push(root)
    const h=await runtime(root),id=SessionId('ordinary-native-source'),sourcePath=join(root,'source.zip')
    await writeFile(sourcePath,'synthetic bridge input')
    const agent=await h.loop.create(id,{provider:'local-fixture',model:'local-fixture'},{cwd:h.ws.path})
    await turn(h.ctx,agent,'Actual local source turn')
    const snapshot=await h.ctx.sessionQuery.readSession(id),native=nativeEnvelope(snapshot),neutral=toNeutral(snapshot,'Ordinary task').neutral
    const payload:ReadResult={schema:'agent-handoff.bridge.read-result.v1',status:'ok',checksum_status:'ok',fingerprint:'f'.repeat(64),manifest:{source_agent:'dsh',source_thread_id:id,title:'Ordinary task',message_count:2},neutral,native,source_session:JSON.stringify(native)+'\n',warnings:[]}
    const service=await serviceFor(h.host,join(root,'plugin-state'),payload),preview=await service.preview({kind:'path',path:sourcePath},h.ws.id)
    expect(preview.modes).toEqual(['conversation']);expect(preview.warnings.join(' ')).toMatch(/authority|plugin|unsupported/i)
    await expect(service.import(preview.ticketId,h.ws.id,'native')).rejects.toMatchObject({code:'ticket_invalid'})
    expect(h.attached.size).toBe(0)
    const result=await service.import(preview.ticketId,h.ws.id,'conversation')
    expect(result.mode).toBe('conversation');expect(result.sessionId).not.toBe(id)
    expect((await h.ctx.sessionQuery.readSession(id)).events).toEqual(snapshot.events)
    expect(h.importedToolRuns).toBe(0);expect(h.adapter.requests).toHaveLength(1)
  })
  it.each(['dsh', 'codex', 'claude'] as const)('%s -> DSH persists, cold-resumes, and continues a new local model turn', async source => {
    const root = await mkdtemp(join(tmpdir(), 'agent-handoff-runtime-')); roots.push(root)
    const initial = await runtime(root), sourcePath = join(root, 'source.zip'); await writeFile(sourcePath, 'synthetic verified bridge input')
    const sourceId = 'source-session', title = 'Synthetic source task'
    let neutral: NeutralTranscript = { schema: 'agent-handoff.neutral.v1', source_agent: source, source_id: sourceId, entries: [
      { kind: 'message', role: 'user', text: 'Imported user question' },
      { kind: 'tool', tool: 'inert_probe', status: 'completed', input: '{}', output: 'Imported inert tool evidence' },
      { kind: 'message', role: 'assistant', text: 'Imported assistant answer' },
    ] }
    let native: ReadResult['native'] = null, originalSourceBytes = 'raw foreign native data kept only in private audit'
    if (source === 'dsh') {
      const snapshot = await passiveNativeSource(initial.ctx,initial.ws.path,sourceId)
      native = nativeEnvelope(snapshot); neutral = toNeutral(snapshot, title).neutral; originalSourceBytes = JSON.stringify(native) + '\n'
    }
    const fingerprint = createHash('sha256').update(source).update(originalSourceBytes).digest('hex')
    const payload: ReadResult = { schema: 'agent-handoff.bridge.read-result.v1', status: 'ok', checksum_status: 'ok', fingerprint,
      manifest: { source_agent: source, source_thread_id: sourceId, title, message_count: neutral.entries.filter(e => e.kind === 'message').length }, neutral, native, source_session: originalSourceBytes, warnings: [] }
    const service = await serviceFor(initial.host, join(root, 'plugin-state'), payload)
    const preview = await service.preview({ kind: 'path', path: sourcePath }, initial.ws.id), mode = source === 'dsh' ? 'native' : 'conversation'
    expect(preview.modes).toContain(mode)
    const result = await service.import(preview.ticketId, initial.ws.id, mode)
    expect(result.status).toBe('imported'); expect(result.sessionId).not.toBe(sourceId); expect(initial.attached.has(result.sessionId)).toBe(true)
    expect(initial.importedToolRuns).toBe(0)
    const imported = await initial.ctx.sessionQuery.readSession(SessionId(result.sessionId)), prefix = structuredClone(imported.events)
    expect(imported.session.cwd).toBe(initial.ws.path); expect(imported.session.isSeeded).toBe(false); expect(imported.session).not.toHaveProperty('agentPreset')
    if (native) {
      expect(imported.events.slice(0,native.events.length)).toEqual(native.events)
      // Native messages and their opaque IDs remain identical.
      const originalMessages=Session.create(native.header.id,native.events,native.header).deriveMessages()
      const importedMessages=Session.create(imported.session.id,imported.events,imported.session).deriveMessages()
      expect(importedMessages.filter(message=>message.role!=='system')).toEqual(originalMessages)
    }
    else expect(JSON.stringify(Session.create(imported.session.id, imported.events, imported.session).deriveMessages())).toContain('Imported inert tool evidence')
    const auditPath = join(root, 'plugin-state', 'audit', `${result.sessionId}.json`)
    expect(JSON.parse(await readFile(auditPath, 'utf8')).source_session).toBe(originalSourceBytes)
    if (process.platform !== 'win32') expect((await stat(auditPath)).mode & 0o777).toBe(0o600)
    await service.dispose(); services.splice(services.indexOf(service), 1); await disposeRuntime(initial.ctx)
    expect((await files(join(root, 'sessions'))).some(path => path.endsWith('.jsonl'))).toBe(true)
    // New service graph: no attached Session or Agent survives the restart.
    const restarted = await runtime(root)
    expect(restarted.ctx.sessions.get(SessionId(result.sessionId))).toBeUndefined(); expect(restarted.ctx.agents.get(SessionId(result.sessionId))).toBeUndefined()
    const cold = await restarted.ctx.sessionQuery.readSession(SessionId(result.sessionId))
    expect(cold.events).toEqual(prefix); expect(restarted.ctx.agents.get(SessionId(result.sessionId))).toBeUndefined()
    const resolved = await restarted.agents.resolveAgent(SessionId(result.sessionId)); if ('error' in resolved) throw resolved.error
    expect(restarted.adapter.requests).toHaveLength(0); expect(restarted.importedToolRuns).toBe(0)
    await turn(restarted.ctx, resolved.agent, 'Explicit fresh local continuation')
    expect(restarted.adapter.requests).toHaveLength(1); expect(restarted.importedToolRuns).toBe(0)
    expect(JSON.stringify(resolved.agent.session.deriveMessages())).toContain('local continuation 1')
    expect(JSON.stringify(restarted.adapter.requests[0])).toContain(source === 'dsh' ? 'Original native DSH question' : 'Imported user question')
    expect(JSON.stringify(restarted.adapter.requests[0])).toContain('Imported inert tool evidence')
    const restartedService = await serviceFor(restarted.host, join(root, 'plugin-state'), payload)
    const repeatedPreview = await restartedService.preview({ kind: 'path', path: sourcePath }, restarted.ws.id)
    expect(await restartedService.import(repeatedPreview.ticketId, restarted.ws.id, mode)).toMatchObject({ status: 'duplicate', sessionId: result.sessionId })
    await restartedService.dispose(); services.splice(services.indexOf(restartedService), 1); await disposeRuntime(restarted.ctx)
    const finalRead = await runtime(root), durable = await stored(finalRead.ctx, result.sessionId)
    expect(durable.slice(0, prefix.length)).toEqual(prefix)
    expect(JSON.stringify(durable)).toContain('Explicit fresh local continuation'); expect(JSON.stringify(durable)).toContain('local continuation 1')
    expect(finalRead.importedToolRuns).toBe(0); await expect(stat(join(root, 'TOOL_EXECUTED'))).rejects.toMatchObject({ code: 'ENOENT' })
    await disposeRuntime(finalRead.ctx)
  })
})
