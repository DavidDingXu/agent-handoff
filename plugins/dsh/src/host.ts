/** Owner-facing orchestration, verified preview tickets and append-only native import. */
import { randomUUID, createHash } from 'node:crypto'
import { mkdir, mkdtemp, readFile, writeFile, stat, rm, open, chmod } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { isAbsolute, join, dirname, basename } from 'node:path'
import { Session, SessionId, SessionLogOffset, type SessionEvent, type SessionHeader } from '@deepseek-ai/dsh-session'
import { validateStoredEvents, assertContiguous } from '@deepseek-ai/dsh-session-persistence'
import { withFileLock } from '@deepseek-ai/dsh-atomic-write'
import { createBridge, type BridgeConfig, type BridgeRunner } from './bridge.ts'
import { HandoffFailure, installedDshVersion, nativeEnvelope, nativeSupport, validateNative, validateSessionRelationships, fromNeutral, toNeutral, freshHeader, validateReadResult, record, type ReadResult, type SessionSnapshot } from './session.ts'
import { MAX_UPLOAD_BYTES, PROVIDER_WARNING, type ApiResponse, type HandoffService, type ImportMode, type ImportSource, type ImportResult, type ImportPreview, type ShareResult, type ShareFormat, type WorkspaceChoice, type SafeFinding, type ExportOptions, type CodeInspection, type SwitchTarget, type SwitchResult, type TaskBrief } from './types.ts'

export interface HandoffConfig extends BridgeConfig { stateDir?: string }
export interface Workspace { id: string; title: string; path: string; attachSession(id: SessionId): Promise<void> }
export interface HandoffHostContext {
  /** Trusted launch context; imported metadata never supplies this directory. */
  profileDirectory?: string
  sessionQuery: { readSession(id: SessionId): Promise<SessionSnapshot>; readTitle?(id: SessionId): Promise<{ title: string } | undefined> }
  sessionPersistence: { create(header: SessionHeader, options?: { inheritedEventCount: SessionLogOffset }): Promise<{ append(events: readonly SessionEvent[]): Promise<void>; flush(): Promise<void>; close(): Promise<void> }>; flush(): Promise<void> }
  workspaceRegistry: { list(): Workspace[]; get(id: string): Workspace | undefined; create?(path: string, title?: string): Promise<Workspace> }
  sessionController: { resolveAgent(id: SessionId): Promise<{ agent: unknown } | { error: unknown }>; rename(request: { sessionId: SessionId; title: string }): Promise<unknown> }
}
interface Ticket { payload: ReadResult; digest: string; workspaceId: string; modes: ImportMode[]; expires: number; recoveryId?: string; result?: ImportResult; codeDirectory?: string; appliedCode?: boolean; codeApplied?: boolean; codeWorkspace?: Workspace; codeUncertain?: boolean }
interface SecretTicket { sessionId: string; format: ShareFormat; input: unknown; warnings: string[]; title: string; count: number; expires: number; switchTarget?: SwitchTarget }
interface Ledger { status: 'created' | 'imported'; fingerprint: string; sessionId: string; workspaceId: string; mode: ImportMode; title: string }
const TTL = 10 * 60_000
function identity(value: unknown): value is string { return typeof value === 'string' && value.length > 0 && Buffer.byteLength(value) <= 8192 && !/[\x00-\x1f]/.test(value) }
function choice(workspace: Workspace): WorkspaceChoice { return { id: workspace.id, name: workspace.title, directory: workspace.path } }
function exportOptions(value?: ExportOptions): ExportOptions {
  if (value === undefined) return {}
  if (!record(value) || Object.keys(value).some(key => !['briefNote', 'codeFiles'].includes(key)) || value.briefNote !== undefined && (typeof value.briefNote !== 'string' || Buffer.byteLength(value.briefNote) > 8192)
    || value.codeFiles !== undefined && (!Array.isArray(value.codeFiles) || value.codeFiles.length > 256 || value.codeFiles.some(path => !identity(path)))) throw new HandoffFailure('invalid_request', 'Review the note and selected code files.')
  return value
}
function shortTitle(value: string): string { let title = ''; for (const c of value.trim()) { if (Buffer.byteLength(title + c) > 80) break; title += c }; return title || 'Imported conversation' }
function safeFindings(value: unknown): SafeFinding[] {
  if (!record(value) || !Array.isArray(value.findings)) return []
  return value.findings.slice(0, 1000).filter(record).map(f => ({ rule: typeof f.rule === 'string' && /^[\w .-]{1,120}$/.test(f.rule) ? f.rule : 'secret',
    ...(typeof f.path === 'string' ? { field: f.path.slice(0, 512) } : {}), ...(typeof f.line === 'number' && Number.isSafeInteger(f.line) ? { line: f.line } : {}) }))
}
function digest(value: unknown): string {
  const canonical = (v: unknown): unknown => Array.isArray(v) ? v.map(canonical) : record(v) ? Object.fromEntries(Object.keys(v).sort().map(k => [k, canonical(v[k])])) : v
  return createHash('sha256').update(JSON.stringify(canonical(value))).digest('hex')
}
function importFingerprint(payload: ReadResult, applyCode: boolean): string { return applyCode ? createHash('sha256').update(payload.fingerprint + ':apply-code').digest('hex') : payload.fingerprint }
export function createHandoffService(ctx: HandoffHostContext, config: HandoffConfig, dependencies: { runBridge?: BridgeRunner; now?: () => number } = {}): HandoffService {
  const runtimeVersion = installedDshVersion()
  if (config.configPath !== undefined && (!config.configPath || !isAbsolute(config.configPath))) throw new HandoffFailure('setup_required', 'configPath must name an absolute provider configuration file.')
  const profileDirectory = ctx.profileDirectory ?? process.env.DSH_PROFILE_DIR
  const stateDir = config.stateDir ?? (profileDirectory ? join(profileDirectory, 'agent-handoff') : undefined)
  if (!stateDir || !isAbsolute(stateDir)) throw new HandoffFailure('setup_required', 'Configure an absolute stateDir or run inside a managed DSH profile.')
  const lifetime = new AbortController(), now = dependencies.now ?? Date.now, run = dependencies.runBridge ?? createBridge(config, lifetime.signal)
  const tickets = new Map<string, Ticket>(), secrets = new Map<string, SecretTicket>(), downloads = new Map<string, { path: string; expires: number; directory: string }>()
  const pending = new Set<Promise<unknown>>()
  let serial: Promise<unknown> = Promise.resolve(), temporary = '', disposed = false, previewing = 0
  const ready = (async () => {
    await mkdir(stateDir, { recursive: true, mode: 0o700 }); if (process.platform !== 'win32') await chmod(stateDir, 0o700)
    temporary = await mkdtemp(join(tmpdir(), 'agent-handoff-dsh-')); await mkdir(join(stateDir, 'audit'), { mode: 0o700 }).catch(e => { if ((e as NodeJS.ErrnoException).code !== 'EEXIST') throw e })
  })()
  void ready.catch(() => {})
  function check(signal?: AbortSignal): void {
    if (disposed || lifetime.signal.aborted) throw new HandoffFailure('disposed', 'The handoff plugin is stopping.')
    if (signal?.aborted) throw new HandoffFailure('cancelled', 'The handoff operation was cancelled.')
  }
  async function active(signal?: AbortSignal): Promise<void> { check(signal); await ready; check(signal) }
  function track<T>(job: Promise<T>): Promise<T> { pending.add(job); void job.finally(() => pending.delete(job)).catch(() => {}); return job }
  function sweep(): void {
    for (const [id, t] of tickets) if (t.expires <= now()) tickets.delete(id)
    for (const [id, t] of secrets) if (t.expires <= now()) secrets.delete(id)
    for (const [id, d] of downloads) if (d.expires <= now()) { downloads.delete(id); void rm(d.path, { force: true }).catch(() => {}) }
  }
  function workspace(id: string): Workspace {
    if (!identity(id)) throw new HandoffFailure('invalid_workspace', 'Choose an existing local workspace.')
    const ws = ctx.workspaceRegistry.get(id)
    if (!ws || !isAbsolute(ws.path)) throw new HandoffFailure('workspace_not_found', 'The selected workspace is no longer available.')
    return ws
  }
  async function ledger(): Promise<Ledger[]> {
    try {
      const path = join(stateDir!, 'imports.jsonl'); if ((await stat(path)).size > 16 * 1024 * 1024) throw new Error('large ledger')
      const lines = (await readFile(path, 'utf8')).split('\n').filter(Boolean), rows: Ledger[] = []
      for (const line of lines) {
        const row: unknown = JSON.parse(line)
        if (!record(row) || !/^[a-f0-9]{64}$/.test(String(row.fingerprint)) || !identity(row.sessionId) || !identity(row.workspaceId) || !['native', 'conversation'].includes(String(row.mode)) || typeof row.title !== 'string' || row.status !== undefined && !['created', 'imported'].includes(String(row.status))) throw new Error('invalid ledger')
        rows.push({ ...row, status: row.status ?? 'imported' } as unknown as Ledger)
      }
      return rows
    } catch (e) { if ((e as NodeJS.ErrnoException).code === 'ENOENT') return []; throw new HandoffFailure('ledger_invalid', 'The import ledger could not be read. Check stateDir before retrying.') }
  }
  async function appendLedger(row: Ledger): Promise<void> {
    const writer = await open(join(stateDir!, 'imports.jsonl'), 'a', 0o600)
    try { await writer.writeFile(JSON.stringify(row) + '\n'); await writer.sync() } finally { await writer.close() }
  }
  async function exported(id: string, signal?: AbortSignal) {
    check(signal)
    if (!identity(id)) throw new HandoffFailure('invalid_session', 'Choose the session to share.')
    const snapshot = await ctx.sessionQuery.readSession(SessionId(id))
    if (snapshot.session.id !== id) throw new HandoffFailure('invalid_session', 'The selected session identity does not match its history.')
    check(signal)
    const title = (await ctx.sessionQuery.readTitle?.(SessionId(id)))?.title ?? 'Conversation'
    check(signal)
    const converted = toNeutral(snapshot, title)
    return { input: { schema: 'agent-handoff.bridge.share.v1', source_thread_id: id, title, source_cwd: snapshot.session.cwd ?? '', source_cli: `dsh@${runtimeVersion}`, native: nativeEnvelope(snapshot), neutral: converted.neutral },
      title, count: converted.neutral.entries.filter(e => e.kind === 'message').length, warnings: converted.warnings }
  }
  async function share(id: string, format: ShareFormat, secretTicket?: string, signal?: AbortSignal, options?: ExportOptions, switchTarget?: SwitchTarget): Promise<ShareResult> {
    await active(signal); sweep()
    if (!['zip', 'link'].includes(format)) throw new HandoffFailure('invalid_request', 'Choose a file or encrypted link.')
    let source: { input: unknown; title: string; count: number; warnings: string[] }
    if (secretTicket) {
      const ticket = secrets.get(secretTicket)
      if (!ticket || ticket.sessionId !== id || ticket.format !== format || ticket.switchTarget !== switchTarget) throw new HandoffFailure('ticket_invalid', 'The secret confirmation expired or its session changed. Share again to rescan.')
      source = ticket
    } else {
      const selected = exportOptions(options), captured = await exported(id, signal)
      const code = selected.codeFiles?.length ? await run(['bridge', 'code-capture', '--cwd', captured.input.source_cwd, '--input', '-'], selected.codeFiles, signal) : undefined
      source = { ...captured, input: { ...captured.input, ...(selected.briefNote ? { brief_note: selected.briefNote } : {}), ...(code ? { code } : {}) } }
    }
    const path = join(temporary, `${randomUUID()}.zip`)
    const args = ['bridge', 'share', '--input', '-', '--format', format, '--out', path]
    if (config.endpoint) args.push('--endpoint', config.endpoint)
    if (config.configPath) args.push('--config', config.configPath)
    if (config.ttlSeconds) args.push('--ttl', String(config.ttlSeconds))
    if (secretTicket) args.push('--include-secrets')
    let retained = false
    try {
      check(signal)
      const result = await run(args, source.input, signal)
      check(signal)
      if (result.status === 'blocked' && result.error_code === 'secret_detected') {
        if (secrets.size >= 8) throw new HandoffFailure('too_many_previews', 'Too many pending secret confirmations. Retry after they expire.')
        const token = randomUUID(); secrets.set(token, { sessionId: id, format, ...source, input: structuredClone(source.input), expires: now() + TTL, ...(switchTarget ? { switchTarget } : {}) })
        return { status: 'blocked', title: source.title, messageCount: source.count, warnings: source.warnings, findings: safeFindings(result.safety), secretTicket: token }
      }
      if (result.schema !== 'agent-handoff.bridge.share-result.v1' || !['ok', 'fallback_zip'].includes(String(result.status))) throw new HandoffFailure('invalid_bridge_response', 'The transfer binary returned an invalid share result.')
      const output: ShareResult = { status: result.status as ShareResult['status'], title: source.title, messageCount: source.count, warnings: [...source.warnings] }
      if (typeof result.share_url === 'string') {
        try { const link = new URL(result.share_url); if (!['https:', 'http:'].includes(link.protocol) || link.username || link.password) throw new Error() }
        catch { throw new HandoffFailure('invalid_bridge_response', 'The transfer binary returned an invalid share link.') }
        output.shareUrl = result.share_url; if (typeof result.expires_at === 'string') output.expiresAt = result.expires_at
      }
      if (result.status === 'fallback_zip') output.warnings.push('Encrypted link upload was unavailable. The local file is ready to download.')
      if (Array.isArray(result.provider_warnings) && result.provider_warnings.length) output.warnings.push(PROVIDER_WARNING)
      if (result.path !== undefined) {
        // Only the generated output path is an allowed download; a child response cannot select another local file.
        if (result.path !== path) throw new HandoffFailure('invalid_bridge_response', 'The generated transfer file path did not match the request.')
        const info = await stat(path)
        if (!info.isFile() || info.size > MAX_UPLOAD_BYTES) throw new HandoffFailure('output_too_large', 'The transfer file exceeds the plugin download limit.')
        if (process.platform !== 'win32') await chmod(path, 0o600)
        if (downloads.size >= 8) throw new HandoffFailure('too_many_downloads', 'Too many pending downloads. Retry after they expire.')
        const directory = record(source.input) && typeof source.input.source_cwd === 'string' ? source.input.source_cwd : ''
        output.downloadId = randomUUID(); downloads.set(output.downloadId, { path, expires: now() + TTL, directory }); retained = true
        output.fileName = 'agent-handoff.zip'; output.sizeBytes = info.size
      } else if (!output.shareUrl) throw new HandoffFailure('invalid_bridge_response', 'The share result did not contain a file or link.')
      if (secretTicket) secrets.delete(secretTicket)
      return output
    } finally { if (!retained) await rm(path, { force: true }).catch(() => {}) }
  }
  async function switchAgent(id: string, target: SwitchTarget, options?: ExportOptions, secretTicket?: string, signal?: AbortSignal): Promise<SwitchResult> {
    if (!['codex', 'claude'].includes(target)) throw new HandoffFailure('invalid_request', 'Choose Codex or Claude Code.')
    const shared = await share(id, 'zip', secretTicket, signal, options, target)
    if (shared.status === 'blocked') return { ...shared, status: 'blocked' }
    const item = shared.downloadId ? downloads.get(shared.downloadId) : undefined
    if (!item || !isAbsolute(item.directory)) throw new HandoffFailure('invalid_workspace', 'Open this task in a local workspace before switching agents.')
    try {
      const imported = await run(['import', item.path, '--target', target, '--cwd', item.directory, '--execute', '--allow-duplicate'], undefined, signal)
      const newId = imported.status === 'duplicate' ? imported.existing_thread_id : target === 'codex' ? imported.thread_id : imported.session_id
      if (!identity(newId) || !['ok', 'duplicate'].includes(String(imported.status)) || typeof imported.fallback_command !== 'string') throw new HandoffFailure('invalid_bridge_response', 'The new agent task could not be verified.')
      downloads.delete(shared.downloadId!); await rm(item.path,{force:true}).catch(()=>{})
      return { status: imported.status as 'ok' | 'duplicate', targetAgent: target, sessionId: newId, title: shared.title, directory: item.directory, command: imported.fallback_command, warnings: shared.warnings }
    } catch {
      check(signal)
      return {...shared,status:'fallback_zip',warnings:[...shared.warnings,'The target task could not be verified. Download the ZIP to inspect or import it manually before retrying.']}
    }
  }
  async function preview(source: ImportSource, workspaceId: string, signal?: AbortSignal): Promise<ImportPreview> {
    await active(signal); sweep(); const ws = workspace(workspaceId)
    if (tickets.size + previewing >= 4) throw new HandoffFailure('too_many_previews', 'Too many pending imports. Wait for a preview to expire.')
    previewing++
    try {
      let target: string, upload: string | undefined
      if (!record(source)) throw new HandoffFailure('invalid_source', 'Choose a file or encrypted handoff link.')
      if (source.kind === 'upload') {
        if (typeof source.dataBase64 !== 'string' || source.dataBase64.length > Math.ceil(MAX_UPLOAD_BYTES / 3) * 4 || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(source.dataBase64)) throw new HandoffFailure('invalid_upload', 'The uploaded file is invalid or too large.')
        const bytes = Buffer.from(source.dataBase64, 'base64')
        if (!bytes.length || bytes.length > MAX_UPLOAD_BYTES) throw new HandoffFailure('invalid_upload', 'The uploaded file is empty or too large.')
        target = upload = join(temporary, `${randomUUID()}.zip`); await writeFile(upload, bytes, { flag: 'wx', mode: 0o600 })
      } else if (source.kind === 'link') {
        try { const url = new URL(source.url); if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || source.url.length > 8192) throw new Error(); target = source.url } catch { throw new HandoffFailure('invalid_link', 'Enter a complete encrypted handoff link.') }
      } else if (source.kind === 'path' && typeof source.path === 'string' && isAbsolute(source.path) && !source.path.includes('\0')) {
        const info = await stat(source.path).catch(() => undefined)
        if (!info?.isFile() || info.size > MAX_UPLOAD_BYTES) throw new HandoffFailure('invalid_source', 'The selected file is unavailable or too large.')
        target = source.path
      } else throw new HandoffFailure('invalid_source', 'Choose a local file or encrypted handoff link.')
      let payload: ReadResult
      try { check(signal); payload = validateReadResult(await run(['bridge', 'read', target], undefined, signal)); check(signal) } finally { if (upload) await rm(upload, { force: true }) }
      if (Buffer.byteLength(JSON.stringify(payload)) > 32 * 1024 * 1024) throw new HandoffFailure('input_too_large', 'The verified bundle exceeds the size limit.')
      const native = nativeSupport(payload.native), modes: ImportMode[] = native.supported ? ['native', 'conversation'] : ['conversation']
      const warnings = [...payload.warnings]
      if (native.warning) warnings.push(native.warning)
      if (!native.supported) warnings.push('Conversation import preserves visible messages and tool evidence; source permissions, runtime state and media files are not restored.')
      const ticketId = randomUUID(), expires = now() + TTL
      const imported = await ledger(), existing = imported.findLast(row => row.fingerprint === payload.fingerprint)
      const existingCode = payload.code ? imported.findLast(row => row.fingerprint === importFingerprint(payload, true)) : undefined
      if (existing?.status === 'created') warnings.push('A previous import created a recovery session but did not finish verification. Open that session; importing again is blocked.')
      check(signal)
      const codeDirectory = payload.code ? join(dirname(ws.path), `${basename(ws.path)}-handoff-${randomUUID().slice(0, 8)}`) : undefined
      tickets.set(ticketId, { payload: structuredClone(payload), digest: digest(payload), workspaceId, modes, expires, codeDirectory })
      return { ticketId, sourceAgent: payload.manifest.source_agent, sourceSessionId: payload.manifest.source_thread_id, title: payload.manifest.title,
        messageCount: payload.neutral.entries.filter(e => e.kind === 'message').length, fingerprint: payload.fingerprint, checksumStatus: payload.checksum_status,
        workspace: choice(ws), modes, warnings, expiresAt: new Date(expires).toISOString(), ...(existing ? { existingSessionId: existing.sessionId } : {}),
        ...(payload.brief ? { brief: payload.brief } : {}), ...(payload.code ? { code: payload.code.manifest, codeDirectory } : {}), ...(existingCode ? {existingCodeSessionId:existingCode.sessionId} : {}) }
    } finally { previewing-- }
  }
  async function performImport(ticketId: string, workspaceId: string, mode: ImportMode, signal?: AbortSignal, applyCode = false): Promise<ImportResult> {
    await active(signal); sweep()
    const ticket = tickets.get(ticketId)
    if (!ticket || ticket.workspaceId !== workspaceId || !ticket.modes.includes(mode)) throw new HandoffFailure('ticket_invalid', 'The import preview expired or its workspace or mode changed. Preview again.')
    if (ticket.recoveryId) throw new HandoffFailure('import_partial', 'This import created a session before failing. Open the recovery session instead of importing again.', ticket.recoveryId)
    if (ticket.result) { if (ticket.appliedCode !== applyCode) throw new HandoffFailure('ticket_invalid', 'Preview again before changing the code import choice.'); return { ...ticket.result, status: 'duplicate' } }
    if (applyCode && (!ticket.payload.code || !ticket.codeDirectory || !ctx.workspaceRegistry.create)) throw new HandoffFailure('invalid_request', 'Code import needs a selected local Git repository and workspace support.')
    if (applyCode && ticket.codeUncertain) throw new HandoffFailure('code_import_uncertain', 'Code application could not be confirmed. Inspect the displayed directory, then preview again or import without applying code.', undefined, ticket.codeDirectory)
    if (digest(ticket.payload) !== ticket.digest) throw new HandoffFailure('ticket_invalid', 'The verified preview changed. Preview the bundle again.')
    let ws = applyCode && ticket.codeWorkspace ? ticket.codeWorkspace : workspace(workspaceId)
    const fingerprint = importFingerprint(ticket.payload, applyCode)
    let id: string | undefined, created = false, entered = false
    try {
      return await withFileLock(join(stateDir!, 'imports.jsonl'), async () => {
        entered = true
        const existing = (await ledger()).findLast(row => row.fingerprint === fingerprint)
        if (existing?.status === 'created') throw new HandoffFailure('import_partial', 'A previous import created a recovery session. Open it before retrying this bundle.', existing.sessionId)
        if (existing) return { status: 'duplicate', sessionId: existing.sessionId, workspaceId: existing.workspaceId, mode: existing.mode, title: existing.title, warnings: ['This bundle was already imported; the existing session was preserved.'] }
        check(signal)
        const events = mode === 'native' ? validateNative(ticket.payload.native!) : fromNeutral(ticket.payload.neutral)
        if (applyCode) {
          if (!ticket.codeApplied) {
            try {
              const applied = await run(['bridge', 'code-apply', '--input', '-', '--cwd', ws.path, '--out', ticket.codeDirectory!], ticket.payload.code, signal)
              if (applied.status !== 'ok' || applied.directory !== ticket.codeDirectory) throw new HandoffFailure('invalid_bridge_response', 'Code application could not be confirmed.')
              ticket.codeApplied = true
            } catch(error) {
              if(error instanceof HandoffFailure && ['code_apply_failed','invalid_code','bridge_unavailable'].includes(error.code))throw error
              ticket.codeUncertain=true
              throw new HandoffFailure('code_import_uncertain','Code application could not be confirmed. Inspect the displayed directory, then preview again or import without applying code.',undefined,ticket.codeDirectory)
            }
          }
          ws = ticket.codeWorkspace ?? await ctx.workspaceRegistry.create!(ticket.codeDirectory!, `${ws.title} · handoff`)
          ticket.codeWorkspace = ws
        }
        id = `session-${randomUUID()}`; const header = freshHeader(id, ws.path)
        assertContiguous(header.id, events, 0); validateStoredEvents(header, events); Session.create(header.id, events, header); validateSessionRelationships(header, events)
        check(signal)
        const audit = { schema: 'agent-handoff.dsh.audit.v1', fingerprint, source_agent: ticket.payload.manifest.source_agent,
          source_session_id: ticket.payload.manifest.source_thread_id, source_session: ticket.payload.source_session, brief: ticket.payload.brief, code: ticket.payload.code, imported_session_id: id, mode, created_at: new Date().toISOString() }
        const auditWriter = await open(join(stateDir!, 'audit', `${id}.json`), 'wx', 0o600)
        try { await auditWriter.writeFile(JSON.stringify(audit)); await auditWriter.sync() } finally { await auditWriter.close() }
        check(signal)
        const writer = await ctx.sessionPersistence.create(header, { inheritedEventCount: SessionLogOffset(0) })
        created = true; ticket.recoveryId = id
        try {
          await appendLedger({ status: 'created', fingerprint, sessionId: id, workspaceId: ws.id, mode, title: ticket.payload.manifest.title })
          check(signal)
          await writer.append(events); check(signal); await writer.flush()
        } finally { await writer.close() }
        check(signal)
        await ws.attachSession(header.id)
        check(signal)
        const resumed = await ctx.sessionController.resolveAgent(header.id)
        if ('error' in resumed) throw new Error('resume failed')
        check(signal)
        await ctx.sessionController.rename({ sessionId: header.id, title: shortTitle(ticket.payload.manifest.title) })
        await ctx.sessionPersistence.flush()
        const verified = await ctx.sessionQuery.readSession(header.id)
        if (verified.session.id !== id || verified.session.cwd !== ws.path || verified.events.length < events.length) throw new Error('readback mismatch')
        // Check the immutable imported prefix, not merely that a catalog row exists.
        if (digest(verified.events.slice(0, events.length)) !== digest(events)) throw new Error('history prefix mismatch')
        check(signal)
        const row: Ledger = { status: 'imported', fingerprint, sessionId: id, workspaceId: ws.id, mode, title: ticket.payload.manifest.title }
        await appendLedger(row)
        ticket.recoveryId = undefined
        const result: ImportResult = { status: 'imported', sessionId: id, title: row.title, workspaceId: ws.id, mode, ...(applyCode ? { codeDirectory: ticket.codeDirectory } : {}),
          warnings: mode === 'conversation' ? ['Imported visible conversation and tool evidence; source runtime state, permissions and media files were not restored.'] : [] }
        ticket.result = result; ticket.appliedCode = applyCode
        return result
      }, { waitMs: 0, signal: signal ? AbortSignal.any([lifetime.signal, signal]) : lifetime.signal })
    } catch (e) {
      if (!entered) { check(signal); throw new HandoffFailure('import_busy', 'Another host is importing into this profile. Retry after it finishes.') }
      if (created && id) throw new HandoffFailure('import_partial', 'A new session was created, but import verification did not finish. Open the recovery session; no existing session was overwritten.', id)
      if (ticket.codeApplied) throw new HandoffFailure('code_import_partial', 'The code workspace is ready, but the task was not created. Retry this import to reuse that workspace.', undefined, ticket.codeDirectory)
      if (e instanceof HandoffFailure) throw e
      throw new HandoffFailure('import_failed', 'The bundle could not be imported. No existing session was overwritten.')
    }
  }
  return {
    workspaces: signal => track((async () => { await active(signal); return ctx.workspaceRegistry.list().map(choice) })()),
    summary: (id, signal) => track((async () => { await active(signal); const source = await exported(id, signal); const value = await run(['bridge', 'brief', '--input', '-'], source.input.neutral, signal); if (value.schema !== 'agent-handoff.brief.v1') throw new HandoffFailure('invalid_bridge_response', 'The task brief could not be read.'); return { sessionId: id, title: source.title, messageCount: source.count, warnings: source.warnings, brief: value as unknown as TaskBrief } })()),
    inspectCode: (id, signal) => track((async () => { await active(signal); const source = await exported(id, signal); return await run(['bridge', 'code-inspect', '--cwd', source.input.source_cwd], undefined, signal) as unknown as CodeInspection })()),
    share: (id, format, ticket, signal, options) => track(share(id, format, ticket, signal, options)),
    switch: (id, target, options, ticket, signal) => { const job=serial.then(()=>switchAgent(id,target,options,ticket,signal));serial=job.catch(()=>{});return track(job) },
    preview: (source, id, signal) => track(preview(source, id, signal)),
    import: (ticket, id, mode, signal, applyCode) => { const job = serial.then(() => performImport(ticket, id, mode, signal, applyCode)); serial = job.catch(() => {}); return track(job) },
    download: (id, signal) => track((async () => {
      await active(signal); sweep(); const item = downloads.get(id)
      if (!item) throw new HandoffFailure('download_expired', 'The download expired. Share the session again.')
      const bytes = await readFile(item.path, { signal }); check(signal); if (bytes.length > MAX_UPLOAD_BYTES) throw new HandoffFailure('output_too_large', 'The transfer file exceeds the size limit.')
      return { fileName: 'agent-handoff.zip', dataBase64: bytes.toString('base64') }
    })()),
    async dispose() { if (disposed) return; disposed = true; lifetime.abort(); await Promise.allSettled([...pending]); await ready.catch(() => {}); tickets.clear(); secrets.clear(); downloads.clear(); if (temporary) await rm(temporary, { recursive: true, force: true }) },
  }
}
export function handoffError(error: unknown) {
  return error instanceof HandoffFailure ? { code: error.code, message: error.message, ...(error.recoverySessionId ? { recoverySessionId: error.recoverySessionId } : {}), ...(error.recoveryDirectory ? {recoveryDirectory:error.recoveryDirectory} : {}) }
    : { code: 'operation_failed', message: 'The handoff operation failed. Retry or check the plugin setup.' }
}
/** Exact owner-only connection route; no unauthenticated loopback/webServer bypass. */
export function createHandoffRoute(service: HandoffService): (request: Request) => Promise<Response> {
  return async request => {
    let response: ApiResponse<unknown>
    try {
      const reader = request.body?.getReader(); const chunks: Uint8Array[] = []; let size = 0
      if (!reader) throw new HandoffFailure('invalid_request', 'A JSON request is required.')
      const checkRequest = () => { if (request.signal.aborted) throw new HandoffFailure('cancelled', 'The handoff operation was cancelled.') }
      const cancelBody = () => { void reader.cancel().catch(() => {}) }
      request.signal.addEventListener('abort', cancelBody, { once: true })
      try {
        checkRequest()
        for (;;) {
          checkRequest(); const part = await reader.read(); checkRequest()
          if (part.done) break
          size += part.value.length
          if (size > 32 * 1024 * 1024) throw new HandoffFailure('input_too_large', 'The request exceeds the size limit.')
          chunks.push(part.value)
        }
      } catch (error) { await reader.cancel().catch(() => {}); checkRequest(); throw error }
      finally { request.signal.removeEventListener('abort', cancelBody); reader.releaseLock() }
      const req: unknown = JSON.parse(Buffer.concat(chunks).toString('utf8'))
      if (!record(req)) throw new HandoffFailure('invalid_request', 'Invalid handoff request.')
      let value: unknown
      switch (req.action) {
        case 'workspaces': value = await service.workspaces(request.signal); break
        case 'summary': if (!identity(req.sessionId)) throw new Error(); value = await service.summary(req.sessionId, request.signal); break
        case 'share': if (!identity(req.sessionId) || !['zip', 'link'].includes(String(req.format)) || req.secretTicket !== undefined && !identity(req.secretTicket)) throw new Error(); value = await service.share(req.sessionId, req.format as ShareFormat, req.secretTicket as string | undefined, request.signal, exportOptions(req.options as ExportOptions | undefined)); break
        case 'code-preview': if (!identity(req.sessionId)) throw new Error(); value = await service.inspectCode(req.sessionId, request.signal); break
        case 'switch': if (!identity(req.sessionId) || !['codex', 'claude'].includes(String(req.target)) || req.secretTicket !== undefined && !identity(req.secretTicket)) throw new Error(); value = await service.switch(req.sessionId, req.target as SwitchTarget, exportOptions(req.options as ExportOptions | undefined), req.secretTicket as string | undefined, request.signal); break
        case 'preview': if (!record(req.source) || !identity(req.workspaceId)) throw new Error(); value = await service.preview(req.source as unknown as ImportSource, req.workspaceId, request.signal); break
        case 'import': if (!identity(req.ticketId) || !identity(req.workspaceId) || !['native', 'conversation'].includes(String(req.mode)) || req.applyCode !== undefined && typeof req.applyCode !== 'boolean') throw new Error(); value = await service.import(req.ticketId, req.workspaceId, req.mode as ImportMode, request.signal, req.applyCode as boolean | undefined); break
        case 'download': if (!identity(req.downloadId)) throw new Error(); value = await service.download(req.downloadId, request.signal); break
        default: throw new HandoffFailure('invalid_request', 'Unknown handoff action.')
      }
      response = { ok: true, data: value }
    } catch (error) { response = { ok: false, error: handoffError(error) } }
    return Response.json(response, { status: response.ok ? 200 : 400, headers: { 'cache-control': 'no-store', 'x-content-type-options': 'nosniff' } })
  }
}
