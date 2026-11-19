import { afterEach, describe, expect, it, vi } from 'vitest'
import { mkdtemp, readFile, writeFile, rm, readdir, stat } from 'node:fs/promises'
import { join } from 'node:path'
import { spawn } from 'node:child_process'
import { withFileLock } from '@deepseek-ai/dsh-atomic-write'
import { tmpdir } from 'node:os'
import { Session, type SessionEvent, type SessionHeader } from '@deepseek-ai/dsh-session'
import { assertContiguous, validateStoredEvents } from '@deepseek-ai/dsh-session-persistence'
import { createHandoffService, createHandoffRoute, type HandoffHostContext, type Workspace } from '../src/host.ts'
import { HandoffFailure, fromNeutral, freshHeader, nativeEnvelope, type ReadResult, type NeutralTranscript } from '../src/session.ts'
import type { HandoffService } from '../src/types.ts'
const roots: string[] = [], services: HandoffService[] = []
afterEach(async () => { for (const s of services.splice(0)) await s.dispose(); for (const path of roots.splice(0)) await rm(path, { recursive: true, force: true }) })
async function harness(options: { attachFail?: boolean; native?: boolean; now?: () => number } = {}) {
  const root = await mkdtemp(join(tmpdir(), 'handoff-host-test-')); roots.push(root)
  const source = join(root, 'source.zip'); await writeFile(source, 'verified synthetic bytes')
  const state = join(root, 'state'), sessions = new Map<string, { header: SessionHeader; events: SessionEvent[] }>(), originals = new Map<string, SessionEvent[]>()
  const ws: Workspace = { id: 'workspace', title: 'Synthetic', path: root, attachSession: vi.fn(async () => { if (options.attachFail) throw new Error('secret raw attach detail') }) }
  const neutral: NeutralTranscript = { schema: 'agent-handoff.neutral.v1', source_agent: options.native ? 'dsh' : 'codex', source_id: 'source-id', entries: [{ kind: 'message', role: 'user', text: 'synthetic question' }, { kind: 'message', role: 'assistant', text: 'synthetic answer' }] }
  const original = fromNeutral(neutral), header = freshHeader('source-id', root)
  sessions.set('source-id', { header, events: original }); originals.set('source-id', structuredClone(original))
  const payload: ReadResult = { schema: 'agent-handoff.bridge.read-result.v1', status: 'ok', checksum_status: 'ok', fingerprint: 'a'.repeat(64), manifest: { source_agent: neutral.source_agent, source_thread_id: 'source-id', title: 'Synthetic task', message_count: 2 }, neutral,
    native: options.native ? nativeEnvelope({ session: header, events: original, inheritedEventCount: 0 as never }) : null, source_session: '{"synthetic":"original inert source"}', warnings: [] }
  const query = vi.fn(async (id: string) => {
    const found = sessions.get(id); if (!found) throw new Error('not found')
    // Real SDK replay validation, but no user profile or model execution.
    Session.create(found.header.id, found.events, found.header)
    return { session: structuredClone(found.header), events: structuredClone(found.events), inheritedEventCount: 0 as never }
  })
  const create = vi.fn(async (h: SessionHeader) => {
    if (sessions.has(h.id)) throw new Error('collision')
    const stored = { header: structuredClone(h), events: [] as SessionEvent[] }; sessions.set(h.id, stored)
    return { append: async (events: readonly SessionEvent[]) => { assertContiguous(h.id, events, stored.events.length); validateStoredEvents(h, structuredClone([...events])); stored.events.push(...structuredClone(events)) }, flush: async () => {}, close: async () => {} }
  })
  const ctx: HandoffHostContext = { workspaceRegistry: { list: () => [ws], get: id => id === ws.id ? ws : undefined }, sessionQuery: { readSession: query, readTitle: async () => ({ title: 'Synthetic task' }) }, sessionPersistence: { create, flush: async () => {} }, sessionController: { resolveAgent: vi.fn(async id => ({ agent: sessions.get(id) })), rename: vi.fn(async () => {}) } }
  const run = vi.fn<(args: string[], input?: unknown, signal?: AbortSignal) => Promise<Record<string, unknown>>>(async () => structuredClone(payload) as unknown as Record<string, unknown>)
  const service = createHandoffService(ctx, { stateDir: state }, { runBridge: run, now: options.now }); services.push(service)
  return { root, source, state, sessions, originals, service, ctx, create, ws, run, payload, query }
}
describe('owner Host service', () => {
  it('defaults private state to the trusted managed profile directory', async () => {
    const h = await harness(); h.ctx.profileDirectory = join(h.root, 'managed-profile')
    const service = createHandoffService(h.ctx, {}, { runBridge: h.run }); services.push(service)
    await service.workspaces()
    expect((await stat(join(h.ctx.profileDirectory, 'agent-handoff'))).isDirectory()).toBe(true)
    expect(() => createHandoffService(h.ctx, { configPath: './relative' }, { runBridge: h.run })).toThrow(/absolute/)
  })
  it('imports only verified cached data, fresh identity, private audit and append-only duplicate ledger', async () => {
    const h = await harness(), preview = await h.service.preview({ kind: 'path', path: h.source }, h.ws.id)
    await writeFile(h.source, 'replaced after preview')
    const result = await h.service.import(preview.ticketId, h.ws.id, 'conversation')
    expect(result.status).toBe('imported'); expect(result.sessionId).not.toBe('source-id'); expect(h.run).toHaveBeenCalledTimes(1)
    expect(h.sessions.get('source-id')?.events).toEqual(h.originals.get('source-id'))
    const stored = h.sessions.get(result.sessionId)!
    expect(stored.header).toMatchObject({ cwd: h.root, isSeeded: false }); expect(stored.header).not.toHaveProperty('agentPreset')
    const auditPath = join(h.state, 'audit', `${result.sessionId}.json`), audit = JSON.parse(await readFile(auditPath, 'utf8'))
    expect(audit.source_session).toBe(h.payload.source_session)
    if (process.platform !== 'win32') expect((await stat(auditPath)).mode & 0o777).toBe(0o600)
    expect((await readFile(join(h.state, 'imports.jsonl'), 'utf8')).trim().split('\n')).toHaveLength(2)
    const duplicate = await h.service.import(preview.ticketId, h.ws.id, 'conversation')
    expect(duplicate).toMatchObject({ status: 'duplicate', sessionId: result.sessionId }); expect(h.create).toHaveBeenCalledTimes(1)
  })
  it('serializes simultaneous confirmations and deduplicates separate tickets', async () => {
    const h = await harness(), a = await h.service.preview({ kind: 'path', path: h.source }, h.ws.id), b = await h.service.preview({ kind: 'path', path: h.source }, h.ws.id)
    const [one, two] = await Promise.all([h.service.import(a.ticketId, h.ws.id, 'conversation'), h.service.import(b.ticketId, h.ws.id, 'conversation')])
    expect(one.sessionId).toBe(two.sessionId); expect(h.create).toHaveBeenCalledTimes(1)
    expect([one.status, two.status]).toEqual(['imported', 'duplicate'])
  })
  it('binds ticket to workspace and mode and expires without a write', async () => {
    let time = Date.now(); const h = await harness({ now: () => time }), p = await h.service.preview({ kind: 'path', path: h.source }, h.ws.id)
    await expect(h.service.import(p.ticketId, 'different', 'conversation')).rejects.toMatchObject({ code: 'ticket_invalid' })
    await expect(h.service.import(p.ticketId, h.ws.id, 'native')).rejects.toMatchObject({ code: 'ticket_invalid' })
    time += 11 * 60_000
    await expect(h.service.import(p.ticketId, h.ws.id, 'conversation')).rejects.toMatchObject({ code: 'ticket_invalid' }); expect(h.create).not.toHaveBeenCalled()
  })
  it('rejects malformed native tool relationships during preview before any create', async () => {
    const h = await harness({ native: true })
    const events = fromNeutral({ ...h.payload.neutral, entries: [{ kind: 'tool', tool: 'inert', status: 'completed', input: '{}', output: 'evidence' }] })
    h.payload.native!.events = events.filter(event => event.type !== 'tool/result').map((event, seq) => ({ ...event, seq: seq as never }))
    await expect(h.service.preview({ kind: 'path', path: h.source }, h.ws.id)).rejects.toMatchObject({ code: 'invalid_session' })
    expect(h.create).not.toHaveBeenCalled()
  })
  it('offers native only for validated inert history and rejects authority without silent degradation', async () => {
    const h = await harness({ native: true }), safe = await h.service.preview({ kind: 'path', path: h.source }, h.ws.id)
    expect(safe.modes).toEqual(['native', 'conversation'])
    h.payload.native!.events.push({ type: 'permission/preset', data: { preset: 'danger-full-access' }, seq: h.payload.native!.events.length, time: 1 } as never)
    const unsafe = await h.service.preview({ kind: 'path', path: h.source }, h.ws.id)
    expect(unsafe.modes).toEqual(['conversation']); expect(unsafe.warnings.join(' ')).toMatch(/authority or plugin/)
    await expect(h.service.import(unsafe.ticketId, h.ws.id, 'native')).rejects.toMatchObject({ code: 'ticket_invalid' })
    const result = await h.service.import(safe.ticketId, h.ws.id, 'native'); expect(result.mode).toBe('native')
  })
  it('reports recovery identity after creation failure and prevents invisible retries', async () => {
    const h = await harness({ attachFail: true }), p = await h.service.preview({ kind: 'path', path: h.source }, h.ws.id)
    let recovery = ''
    try { await h.service.import(p.ticketId, h.ws.id, 'conversation') } catch (e) { recovery = (e as { recoverySessionId: string }).recoverySessionId; expect(String(e)).not.toContain('secret raw') }
    expect(recovery).toMatch(/^session-/); expect(h.sessions.has(recovery)).toBe(true)
    await expect(h.service.import(p.ticketId, h.ws.id, 'conversation')).rejects.toMatchObject({ code: 'import_partial', recoverySessionId: recovery })
    expect(h.create).toHaveBeenCalledTimes(1)
    const ledger = (await readFile(join(h.state, 'imports.jsonl'), 'utf8')).trim().split('\n').map(line => JSON.parse(line))
    expect(ledger).toHaveLength(1); expect(ledger[0]).toMatchObject({ status: 'created', sessionId: recovery })
    const again = await h.service.preview({ kind: 'path', path: h.source }, h.ws.id)
    await expect(h.service.import(again.ticketId, h.ws.id, 'conversation')).rejects.toMatchObject({ code: 'import_partial', recoverySessionId: recovery })
    expect(h.create).toHaveBeenCalledTimes(1)
  })
  it('reads only the selected export session without listing other history', async () => {
    const h = await harness(); const summary = await h.service.summary('source-id')
    expect(summary.messageCount).toBe(2); expect(h.query).toHaveBeenCalledExactlyOnceWith('source-id')
  })
  it('requires the exact secret confirmation snapshot and sanitizes findings', async () => {
    const h = await harness()
    h.run.mockImplementation(async (args: string[], input?: unknown) => {
      if (!args.includes('--include-secrets')) return { status: 'blocked', error_code: 'secret_detected', safety: { findings: [{ rule: 'token', path: '$.native.events[0]', line: 1, hint: 'DO NOT LEAK' }] } }
      const out = args[args.indexOf('--out') + 1]!; await writeFile(out, 'synthetic zip')
      expect(JSON.stringify(input)).not.toContain('modified after scan')
      return { schema: 'agent-handoff.bridge.share-result.v1', status: 'ok', path: out }
    })
    const blocked = await h.service.share('source-id', 'zip')
    expect(blocked.status).toBe('blocked'); expect(JSON.stringify(blocked)).not.toContain('DO NOT LEAK')
    await expect(h.service.share('source-id', 'link', blocked.secretTicket)).rejects.toMatchObject({ code: 'ticket_invalid' })
    h.sessions.get('source-id')!.events = fromNeutral({ ...h.payload.neutral, entries: [{ kind: 'message', role: 'user', text: 'modified after scan' }] })
    const shared = await h.service.share('source-id', 'zip', blocked.secretTicket)
    expect(shared.downloadId).toBeTruthy(); expect((await h.service.download(shared.downloadId!)).dataBase64).toBe(Buffer.from('synthetic zip').toString('base64'))
  })
  it('reclaims only a proven-dead SDK lock holder', async () => {
    const h = await harness(), p = await h.service.preview({ kind: 'path', path: h.source }, h.ws.id)
    const child = spawn(process.execPath, ['-e', ''])
    await new Promise<void>((resolve, reject) => { child.once('error', reject); child.once('close', () => resolve()) })
    expect(child.pid).toBeGreaterThan(0)
    await writeFile(join(h.state, 'imports.jsonl.lock'), String(child.pid) + '\n', { mode: 0o600 })
    expect((await h.service.import(p.ticketId, h.ws.id, 'conversation')).status).toBe('imported')
    await expect(stat(join(h.state, 'imports.jsonl.lock'))).rejects.toMatchObject({ code: 'ENOENT' })
  })
  it('does not remove a live SDK lock holder or bypass its ledger claim', async () => {
    const h = await harness(), p = await h.service.preview({ kind: 'path', path: h.source }, h.ws.id)
    let entered!: () => void, release!: () => void
    const ready = new Promise<void>(resolve => { entered = resolve }), gate = new Promise<void>(resolve => { release = resolve })
    const holder = withFileLock(join(h.state, 'imports.jsonl'), async () => { entered(); await gate })
    await ready
    try {
      await expect(h.service.import(p.ticketId, h.ws.id, 'conversation')).rejects.toMatchObject({ code: 'import_busy' })
      expect(await readFile(join(h.state, 'imports.jsonl.lock'), 'utf8')).toBe(String(process.pid) + '\n')
      expect(h.create).not.toHaveBeenCalled()
    } finally { release(); await holder }
    expect((await h.service.import(p.ticketId, h.ws.id, 'conversation')).status).toBe('imported')
  })
  it('propagates HTTP cancellation to that transfer operation only', async () => {
    const h = await harness(), route = createHandoffRoute(h.service), abort = new AbortController()
    let received: AbortSignal | undefined
    h.run.mockImplementation(async (_args, _input, signal?: AbortSignal) => {
      received = signal
      return await new Promise((_resolve, reject) => { signal?.addEventListener('abort', () => reject(new HandoffFailure('cancelled', 'The handoff operation was cancelled.')), { once: true }) })
    })
    const request = new Request('http://fixture/api/agent-handoff', { method: 'POST', body: JSON.stringify({ action: 'share', sessionId: 'source-id', format: 'link' }), signal: abort.signal })
    const pending = route(request)
    for (let i = 0; i < 100 && !received; i++) await new Promise(resolve => setTimeout(resolve, 1))
    expect(received).toBe(request.signal); abort.abort()
    expect(await (await pending).json()).toMatchObject({ ok: false, error: { code: 'cancelled' } })
    expect((await h.service.workspaces()).length).toBe(1)
  })
  it('cancels before import creation and retains a recovery claim if aborted after create', async () => {
    const h = await harness(), p = await h.service.preview({ kind: 'path', path: h.source }, h.ws.id)
    const before = new AbortController(); before.abort()
    await expect(h.service.import(p.ticketId, h.ws.id, 'conversation', before.signal)).rejects.toMatchObject({ code: 'cancelled' })
    expect(h.create).not.toHaveBeenCalled()
    const after = new AbortController(), create = h.create.getMockImplementation()!
    h.create.mockImplementation(async header => { const writer = await create(header); after.abort(); return writer })
    await expect(h.service.import(p.ticketId, h.ws.id, 'conversation', after.signal)).rejects.toMatchObject({ code: 'import_partial', recoverySessionId: expect.stringMatching(/^session-/) })
    expect(h.create).toHaveBeenCalledTimes(1)
    const rows = (await readFile(join(h.state, 'imports.jsonl'), 'utf8')).trim().split('\n').map(line => JSON.parse(line))
    expect(rows).toHaveLength(1); expect(rows[0].status).toBe('created')
  })
  it('rejects non-web share URLs rather than exposing an active result link', async () => {
    const h = await harness()
    h.run.mockResolvedValue({ schema: 'agent-handoff.bridge.share-result.v1', status: 'ok', share_url: 'javascript:alert(1)' })
    await expect(h.service.share('source-id', 'link')).rejects.toMatchObject({ code: 'invalid_bridge_response' })
  })
  it('bounds preview slots while bridge reads are still in flight', async () => {
    const h = await harness(); await h.service.workspaces()
    let release!: () => void
    const gate = new Promise<void>(resolve => { release = resolve })
    h.run.mockImplementation(async () => { await gate; return structuredClone(h.payload) as unknown as Record<string, unknown> })
    const requests = Array.from({ length: 5 }, () => h.service.preview({ kind: 'path', path: h.source }, h.ws.id).then(value => ({ value }), error => ({ error })))
    for (let i = 0; i < 100 && h.run.mock.calls.length < 4; i++) await new Promise(resolve => setTimeout(resolve, 1))
    const calls = h.run.mock.calls.length; release()
    const results = await Promise.all(requests)
    expect(calls).toBe(4); expect(results.filter(result => 'error' in result)).toHaveLength(1)
  })
  it('promptly cancels an incomplete streaming request while waiting for the next chunk', async () => {
    const h = await harness(), route = createHandoffRoute(h.service), abort = new AbortController(), cancelled = vi.fn()
    const body = new ReadableStream<Uint8Array>({ start(controller) { controller.enqueue(new TextEncoder().encode('{"action":"share"')) }, cancel: cancelled })
    const request = new Request('http://fixture/api/agent-handoff', { method: 'POST', body, signal: abort.signal, duplex: 'half' } as RequestInit & { duplex: string })
    const pending = route(request)
    await new Promise(resolve => setTimeout(resolve, 5)); abort.abort()
    let timer: ReturnType<typeof setTimeout> | undefined
    try {
      const response = await Promise.race([pending, new Promise<never>((_resolve, reject) => { timer = setTimeout(() => reject(new Error('aborted reader did not settle')), 250) })])
      expect(await response.json()).toMatchObject({ ok: false, error: { code: 'cancelled' } })
      expect(cancelled).toHaveBeenCalledTimes(1); expect(h.run).not.toHaveBeenCalled()
    } finally { clearTimeout(timer); await request.body?.cancel().catch(() => {}) }
  })
  it('cancels oversized streaming requests instead of leaving the body open', async () => {
    const h = await harness(), cancelled = vi.fn(), route = createHandoffRoute(h.service)
    const body = new ReadableStream<Uint8Array>({ start(controller) { controller.enqueue(new Uint8Array(32 * 1024 * 1024 + 1)) }, cancel: cancelled })
    const request = new Request('http://fixture/api/agent-handoff', { method: 'POST', body, duplex: 'half' } as RequestInit & { duplex: string })
    const response = await route(request)
    expect(await response.json()).toMatchObject({ ok: false, error: { code: 'input_too_large' } })
    expect(cancelled).toHaveBeenCalledTimes(1)
  })
  it('returns sanitized API errors and validates action inputs', async () => {
    const h = await harness(), route = createHandoffRoute(h.service)
    const response = await route(new Request('http://fixture/api/agent-handoff', { method: 'POST', body: JSON.stringify({ action: 'import', ticketId: 'unknown', workspaceId: h.ws.id, mode: 'conversation' }) }))
    expect(await response.json()).toMatchObject({ ok: false, error: { code: 'ticket_invalid' } })
    const bad = await route(new Request('http://fixture/api/agent-handoff', { method: 'POST', body: '[]' }))
    expect((await bad.json()).ok).toBe(false)
  })
})
