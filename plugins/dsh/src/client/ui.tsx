/** Native DSH controls, shared by the installed slot entries and focused interaction tests. */
import { useCallback, useEffect, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from 'react'
import { Button, Input, Modal, IconUploadOutlineRegular, writeClipboard } from '@deepseek-ai/dsh-client-ui-primitives'
import { HandoffApiError, readUpload, saveDownload, type ApiCall } from './api.ts'
import { warningText, type Translate, type CopyKey } from './locales.ts'
import type { DownloadResult, HandoffError, ImportMode, ImportPreview, ImportResult, ImportSource, SessionSummary, ShareFormat, ShareResult, WorkspaceChoice } from '../types.ts'

const column: CSSProperties = { display: 'flex', flexDirection: 'column', gap: 16, minWidth: 0 }
const row: CSSProperties = { display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }
const muted: CSSProperties = { color: 'var(--dsw-alias-label-secondary)', margin: 0, lineHeight: 1.6 }
const field: CSSProperties = { ...column, gap: 6 }
const selectStyle: CSSProperties = { font: 'inherit', color: 'var(--dsw-alias-label-primary)', background: 'var(--dsw-alias-bg-layer-1)', border: '0.5px solid var(--dsw-alias-border-l4)', borderRadius: 'var(--dsw-radius-md)', padding: '8px 10px', maxWidth: '100%' }
const identity: CSSProperties = { overflowWrap: 'anywhere', margin: 0, fontSize: 12, color: 'var(--dsw-alias-label-secondary)' }

/** Immediate in-flight guard also covers two clicks before React commits a disabled button. */
function useTask() {
  const active = useRef<AbortController | null>(null)
  const [phase, setPhase] = useState<CopyKey | null>(null)
  const [error, setError] = useState<HandoffError | undefined>()
  useEffect(() => () => { active.current?.abort(); active.current = null }, [])
  const run = useCallback(async (label: CopyKey, action: (signal: AbortSignal) => Promise<void>) => {
    if (active.current !== null) return
    const controller = new AbortController(); active.current = controller
    setPhase(label); setError(undefined)
    try { await action(controller.signal) } catch (reason) {
      if (!controller.signal.aborted) setError(reason instanceof HandoffApiError ? reason.detail : { code: 'ui/unknown', message: '' })
    } finally {
      if (active.current === controller) { active.current = null; setPhase(null) }
    }
  }, [])
  const cancel = useCallback(() => { active.current?.abort(); active.current = null }, [])
  return { phase, error, run, cancel, clearError: () => setError(undefined) }
}
function errorText(error: HandoffError, t: Translate): string {
  const copy: Record<string, CopyKey> = {
    'ui/owner-only': 'ownerOnly', 'ui/offline': 'offline', 'ui/invalid-response': 'invalidResponse',
    'ui/upload-too-large': 'uploadTooLarge', 'ui/upload-failed': 'uploadFailed', 'ui/session-not-listed': 'sessionNotListed',
    'ui/unknown': 'unknownError',
    setup_required: 'setupError', unsupported_platform: 'setupError', unsupported_runtime: 'setupError', bridge_unavailable: 'setupError',
    invalid_workspace: 'selectionError', workspace_not_found: 'selectionError', invalid_session: 'selectionError',
    invalid_source: 'handoffError', invalid_upload: 'handoffError', invalid_native: 'handoffError', invalid_neutral: 'handoffError',
    invalid_input: 'handoffError', unsupported_schema: 'handoffError', read_failed: 'handoffError', integrity_failed: 'handoffError',
    invalid_bridge_response: 'invalidResponse', invalid_request: 'invalidResponse',
    native_unsupported: 'nativeError', ticket_invalid: 'confirmationError', too_many_previews: 'capacityError', too_many_downloads: 'capacityError',
    import_busy: 'importBusy', import_partial: 'partialImport', import_failed: 'importFailed', download_expired: 'downloadExpired',
    input_too_large: 'sizeError', payload_too_large: 'sizeError', output_too_large: 'sizeError', bridge_output_too_large: 'sizeError',
    invalid_link: 'linkError', bridge_timeout: 'transferTimeout', ledger_invalid: 'ledgerError', operation_failed: 'unknownError', share_failed: 'unknownError', disposed: 'offline',
  }
  const key = Object.hasOwn(copy, error.code) ? copy[error.code] : undefined
  return key ? t(key) : error.message || t('unknownError')
}
function ErrorBox({ error, t, openSession, busy }: { error?: HandoffError; t: Translate; openSession?: (id: string) => void; busy?: boolean }) {
  if (!error) return null
  return <div role="alert" style={{ ...column, gap: 8, color: 'var(--dsw-alias-state-error-primary)' }}>
    <p style={{ margin: 0 }}>{errorText(error, t)}</p>
    {error.recoverySessionId && openSession && <>
      <p style={identity}>{error.recoverySessionId}</p>
      <Button variant="outline" disabled={busy} onClick={() => openSession(error.recoverySessionId!)}>{t('openCreated')}</Button>
    </>}
  </div>
}
function Warnings({ warnings, t }: { warnings: string[]; t: Translate }) {
  if (!warnings.length) return null
  return <div style={{ ...column, gap: 4 }}><strong>{t('warnings')}</strong><ul style={{ margin: 0, paddingInlineStart: 20, ...muted }}>{warnings.map((text, at) => <li key={at}>{warningText(text, t)}</li>)}</ul></div>
}
function Status({ phase, notice, t }: { phase: CopyKey | null; notice?: string; t: Translate }) {
  return <div role="status" aria-live="polite" style={muted}>
    {phase ? <><progress aria-label={t(phase)} style={{ width: '100%' }} /><p style={{ margin: '4px 0 0' }}>{t(phase)}</p></> : notice ?? ''}
  </div>
}
export interface ShareDialogProps {
  sessionId: string
  call: ApiCall
  t: Translate
  onClose: () => void
  copy?: (text: string) => Promise<boolean>
  save?: (result: DownloadResult) => void
}
/** The captured identity remains fixed while the user changes DSH's current selection. */
export function ShareDialog({ sessionId, call, t, onClose, copy = writeClipboard, save = saveDownload }: ShareDialogProps) {
  const task = useTask()
  const [summary, setSummary] = useState<SessionSummary | null>(null)
  const [format, setFormat] = useState<ShareFormat>('zip')
  const [result, setResult] = useState<ShareResult | null>(null)
  const [consent, setConsent] = useState(false)
  const [notice, setNotice] = useState('')
  const [copyError, setCopyError] = useState(false)
  const [reload, setReload] = useState(0)
  useEffect(() => {
    void task.run('loading', async signal => {
      const value = await call<SessionSummary>({ action: 'summary', sessionId }, signal)
      signal.throwIfAborted(); setSummary(value)
    })
    return task.cancel
  }, [call, sessionId, reload, task.run, task.cancel])
  const busy = task.phase !== null
  const close = () => { task.cancel(); onClose() }
  const share = (secretTicket?: string) => void task.run('preparing', async signal => {
    setNotice(''); setCopyError(false)
    const value = await call<ShareResult>({ action: 'share', sessionId, format, ...(secretTicket ? { secretTicket } : {}) }, signal)
    signal.throwIfAborted(); setResult(value); setConsent(false)
    setSummary({ sessionId, title: value.title, messageCount: value.messageCount, warnings: value.warnings })
    if (value.status !== 'blocked') setNotice(t(value.status === 'fallback_zip' ? 'fallback' : 'shareReady'))
  })
  const blocked = result?.status === 'blocked'
  return <Modal open onClose={close} closeLabel={t('close')} title={t('share')} description={t('shareDescription')}
    className="agent-handoff-share" contentClassName="agent-handoff-share-content"
    footer={<Button variant="outline" onClick={close}>{t('close')}</Button>}>
    <div style={{ ...column, font: 'inherit' }}>
      <style>{'.agent-handoff-share{width:min(560px,100%);max-height:100%}.agent-handoff-share-content{overflow-y:auto;min-height:0}'}</style>
      {summary ? <div style={{ ...column, gap: 4 }}><strong>{summary.title}</strong><p style={identity}>{t('session')}: {sessionId}</p><p style={muted}>{t('messages')}: {summary.messageCount}</p><Warnings warnings={summary.warnings} t={t} /></div>
        : !busy && <Button variant="outline" onClick={() => setReload(value => value + 1)}>{t('retry')}</Button>}
      <label style={field}><span>{t('format')}</span><select aria-label={t('format')} value={format} disabled={busy} style={selectStyle} onChange={event => {
        setFormat(event.currentTarget.value as ShareFormat); setResult(null); setConsent(false); setNotice(''); setCopyError(false); task.clearError()
      }}><option value="zip">{t('zip')}</option><option value="link">{t('link')}</option></select></label>
      <p style={muted}>{t(format === 'zip' ? 'zipNotice' : 'linkNotice')}</p>
      {!blocked && <Button variant="primary" disabled={busy || summary === null} onClick={() => share()}>{t('createShare')}</Button>}
      {blocked && <section style={column} aria-label={t('blocked')}>
        <strong>{t('blocked')}</strong><p style={muted}>{t('blockedDescription')}</p>
        {result.findings?.length ? <ul style={{ margin: 0, paddingInlineStart: 20 }}>{result.findings.map((finding, at) => <li key={at} style={{ overflowWrap: 'anywhere' }}><strong>{finding.rule}</strong>{finding.field && <p style={identity}>{t('field')}: {finding.field}</p>}{finding.line !== undefined && <p style={identity}>{t('line')}: {finding.line}</p>}</li>)}</ul> : <p style={muted}>{t('noFindingDetails')}</p>}
        <Button variant="outline" disabled={busy} onClick={() => share()}>{t('retry')}</Button>
        {result.secretTicket && <><label style={{ ...row, alignItems: 'flex-start' }}><input type="checkbox" checked={consent} disabled={busy} onChange={event => setConsent(event.currentTarget.checked)} /><span>{t('allowSecrets')}</span></label>
          <Button variant="primary" disabled={busy || !consent} onClick={() => { if (consent) share(result.secretTicket) }}>{t('overrideShare')}</Button></>}
      </section>}
      {result && !blocked && <section style={column}>
        <Warnings warnings={result.warnings} t={t} />
        {result.shareUrl && <><label style={field}><span>{t('resultLink')}</span><Input aria-label={t('resultLink')} readOnly value={result.shareUrl} onFocus={event => event.currentTarget.select()} /></label>
          {result.expiresAt && <p style={muted}>{t('expires')}: {result.expiresAt}</p>}
          <Button variant="outline" disabled={busy} onClick={() => void task.run('copying', async signal => {
            const accepted = await copy(result.shareUrl!); signal.throwIfAborted(); setCopyError(!accepted); setNotice(accepted ? t('copied') : '')
          })}>{t('copyLink')}</Button></>}
        {result.downloadId && <Button variant="outline" disabled={busy} onClick={() => void task.run('downloading', async signal => {
          const file = await call<DownloadResult>({ action: 'download', downloadId: result.downloadId! }, signal)
          signal.throwIfAborted(); save(file); setNotice(t('downloadStarted'))
        })}>{t('download')}</Button>}
      </section>}
      {copyError && <p role="alert" style={muted}>{t('copyFailed')}</p>}
      <ErrorBox error={task.error} t={t} />
      <Status phase={task.phase} notice={notice} t={t} />
    </div>
  </Modal>
}

export interface WorkspacePickerInput {
  open: boolean
  anchorRef: RefObject<HTMLButtonElement>
  selectedId?: string
  onPick: (id: string) => void
  onClose: () => void
}
export interface ImportPanelProps {
  call: ApiCall
  t: Translate
  openSession: (id: string) => Promise<void>
  renderWorkspacePicker?: (input: WorkspacePickerInput) => ReactNode
}
/** Preview is discarded for every source/target change; confirmation can only consume the displayed ticket. */
export function ImportPanel({ call, t, openSession, renderWorkspacePicker }: ImportPanelProps) {
  const task = useTask()
  const [workspaces, setWorkspaces] = useState<WorkspaceChoice[]>([])
  const [workspaceId, setWorkspaceId] = useState('')
  const [sourceKind, setSourceKind] = useState<ImportSource['kind']>('upload')
  const [file, setFile] = useState<File | null>(null)
  const [link, setLink] = useState('')
  const [path, setPath] = useState('')
  const [preview, setPreview] = useState<ImportPreview | null>(null)
  const [mode, setMode] = useState<ImportMode | ''>('')
  const [result, setResult] = useState<ImportResult | null>(null)
  const [recoverySessionId, setRecoverySessionId] = useState<string | undefined>()
  const [recoveryMessage, setRecoveryMessage] = useState('')
  const [pickerOpen, setPickerOpen] = useState(false)
  const [expiredTicket, setExpiredTicket] = useState(false)
  const [reload, setReload] = useState(0)
  const pickerAnchor = useRef<HTMLButtonElement>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  useEffect(() => {
    void task.run('loading', async signal => {
      const items = await call<WorkspaceChoice[]>({ action: 'workspaces' }, signal)
      signal.throwIfAborted(); setWorkspaces(items)
    })
    return task.cancel
  }, [call, reload, task.run, task.cancel])
  const busy = task.phase !== null
  const selected = workspaces.find(item => item.id === workspaceId)
  const resetPreview = () => { setPreview(null); setMode(''); setResult(null); setRecoverySessionId(undefined); setRecoveryMessage(''); setExpiredTicket(false); task.clearError() }
  const changeWorkspace = (id: string) => {
    setWorkspaceId(id); setPickerOpen(false); resetPreview()
    // Native picker can add a new registered workspace. Refresh only its choices, retaining the selected id.
    if (!workspaces.some(item => item.id === id)) setReload(value => value + 1)
  }
  const sourceReady = sourceKind === 'upload' ? file !== null : sourceKind === 'link' ? link.trim() !== '' : path.trim() !== ''
  const loadPreview = () => void task.run('previewing', async signal => {
    setPreview(null); setMode(''); setResult(null); setExpiredTicket(false)
    const source: ImportSource = sourceKind === 'upload' ? await readUpload(file!, signal)
      : sourceKind === 'link' ? { kind: 'link', url: link.trim() } : { kind: 'path', path: path.trim() }
    const value = await call<ImportPreview>({ action: 'preview', source, workspaceId }, signal)
    signal.throwIfAborted(); setPreview(value)
  })
  const confirm = () => {
    if (!preview || !mode || !preview.modes.includes(mode) || preview.workspace.id !== workspaceId || recoverySessionId) return
    if (!Number.isFinite(Date.parse(preview.expiresAt)) || Date.parse(preview.expiresAt) <= Date.now()) { setExpiredTicket(true); return }
    void task.run('importing', async signal => {
      try {
        const value = await call<ImportResult>({ action: 'import', ticketId: preview.ticketId, workspaceId: preview.workspace.id, mode }, signal)
        signal.throwIfAborted(); setResult(value)
      } catch (reason) {
        if (!signal.aborted && reason instanceof HandoffApiError && reason.detail.recoverySessionId) {
          setRecoverySessionId(reason.detail.recoverySessionId); setRecoveryMessage(reason.detail.message)
        }
        throw reason
      }
    })
  }
  const open = (id: string) => void task.run('opening', async signal => { await openSession(id); signal.throwIfAborted() })
  const expired = expiredTicket || (preview !== null && (!Number.isFinite(Date.parse(preview.expiresAt)) || Date.parse(preview.expiresAt) <= Date.now()))
  return <section aria-label={t('import')} style={{ ...column, padding: '28px 24px', maxWidth: 760, width: '100%', boxSizing: 'border-box', margin: '0 auto', font: 'inherit', color: 'var(--dsw-alias-label-primary)', overflowY: 'auto', maxHeight: '100%' }}>
    <div><h1 style={{ margin: '0 0 8px', fontSize: 22, fontWeight: 600 }}>{t('import')}</h1><p style={muted}>{t('importDescription')}</p></div>
    <div style={field}><label htmlFor="agent-handoff-workspace">{t('workspace')}</label>
      {renderWorkspacePicker ? <><Button ref={pickerAnchor} variant="outline" disabled={busy} aria-label={t('workspace')} aria-haspopup="menu" aria-expanded={pickerOpen} onClick={() => setPickerOpen(value => !value)}>{selected?.name ?? t('chooseWorkspace')}</Button>
        {renderWorkspacePicker({ open: pickerOpen, anchorRef: pickerAnchor, ...(workspaceId ? { selectedId: workspaceId } : {}), onPick: changeWorkspace, onClose: () => setPickerOpen(false) })}</>
        : <select id="agent-handoff-workspace" aria-label={t('workspace')} style={selectStyle} disabled={busy} value={workspaceId} onChange={event => changeWorkspace(event.currentTarget.value)}><option value="">{t('chooseWorkspace')}</option>{workspaces.map(item => <option key={item.id} value={item.id}>{item.name} — {item.directory}</option>)}</select>}
      {selected && <p style={identity}>{selected.directory}</p>}
      {!workspaces.length && !busy && <Button variant="outline" onClick={() => setReload(value => value + 1)}>{t('refreshWorkspaces')}</Button>}
    </div>
    <label style={field}><span>{t('source')}</span><select aria-label={t('source')} style={selectStyle} disabled={busy} value={sourceKind} onChange={event => { setSourceKind(event.currentTarget.value as ImportSource['kind']); resetPreview() }}><option value="upload">{t('file')}</option><option value="link">{t('shareLink')}</option><option value="path">{t('path')}</option></select></label>
    {sourceKind === 'upload' ? <div style={field}><span>{t('file')}</span><div style={row}><Button variant="outline" disabled={busy} onClick={() => fileInput.current?.click()}>{t('chooseFile')}</Button><span style={muted}>{file?.name ?? t('noFile')}</span><input ref={fileInput} type="file" accept=".zip,application/zip,application/x-zip-compressed" aria-label={t('chooseFile')} disabled={busy} style={{ display: 'none' }} onChange={event => { setFile(event.currentTarget.files?.[0] ?? null); event.currentTarget.value = ''; resetPreview() }} /></div></div>
      : sourceKind === 'link' ? <label style={field}><span>{t('shareLink')}</span><Input aria-label={t('shareLink')} value={link} disabled={busy} placeholder="https://…#…" onChange={event => { setLink(event.currentTarget.value); resetPreview() }} /></label>
        : <label style={field}><span>{t('hostPath')}</span><Input aria-label={t('hostPath')} value={path} disabled={busy} onChange={event => { setPath(event.currentTarget.value); resetPreview() }} /><span style={muted}>{t('hostPathNotice')}</span></label>}
    {!result && <Button variant="outline" disabled={busy || !selected || !sourceReady} onClick={loadPreview}>{t('preview')}</Button>}
    {preview && !result && <section style={{ ...column, borderTop: '0.5px solid var(--dsw-alias-border-l4)', paddingTop: 16 }} aria-label={t('previewTitle')}>
      <h2 style={{ margin: 0, fontSize: 16 }}>{t('previewTitle')}</h2><strong>{preview.title}</strong>
      <dl style={{ display: 'grid', gridTemplateColumns: 'max-content minmax(0, 1fr)', gap: '8px 16px', margin: 0 }}>
        <dt>{t('sourceAgent')}</dt><dd style={{ margin: 0 }}>{({ dsh: 'DSH', codex: 'Codex', claude: 'Claude Code' } as Record<string, string>)[preview.sourceAgent] ?? preview.sourceAgent}</dd>
        <dt>{t('sourceIdentity')}</dt><dd style={identity}>{preview.sourceSessionId}</dd>
        <dt>{t('messages')}</dt><dd style={{ margin: 0 }}>{preview.messageCount}</dd>
        <dt>{t('selectedWorkspace')}</dt><dd style={{ margin: 0, overflowWrap: 'anywhere' }}>{preview.workspace.name} — {preview.workspace.directory}</dd>
        <dt>{t('checksum')}</dt><dd style={{ margin: 0 }}>{['ok', 'verified'].includes(preview.checksumStatus) ? t('verified') : preview.checksumStatus === 'not_available_v1' ? t('legacyChecksum') : preview.checksumStatus}</dd>
      </dl>
      <Warnings warnings={preview.warnings} t={t} />
      {preview.existingSessionId && <p style={muted}>{t('duplicatePreview')}</p>}
      <label style={field}><span>{t('importMode')}</span><select aria-label={t('importMode')} style={selectStyle} disabled={busy} value={mode} onChange={event => setMode(event.currentTarget.value as ImportMode)}><option value="">{t('chooseMode')}</option>{preview.modes.map(item => <option value={item} key={item}>{t(item === 'native' ? 'native' : 'conversation')}</option>)}</select></label>
      <p style={muted}>{t(mode === 'native' ? 'nativeDescription' : 'conversationDescription')}</p>
      {expired && <p role="alert">{t('expired')}</p>}
      <Button variant="primary" disabled={busy || !mode || expired || !!recoverySessionId || preview.workspace.id !== workspaceId} onClick={confirm}>{t('confirmation')}</Button>
    </section>}
    {result && <section style={column}><strong>{result.title}</strong><p style={identity}>{result.sessionId}</p><Warnings warnings={result.warnings} t={t} /><div style={row}><Button variant="primary" disabled={busy} onClick={() => open(result.sessionId)}>{t('openTask')}</Button><Button variant="outline" disabled={busy} onClick={resetPreview}>{t('importAnother')}</Button></div></section>}
    <ErrorBox error={recoverySessionId ? { ...(task.error ?? { code: 'import/recovery', message: recoveryMessage }), recoverySessionId } : task.error} t={t} openSession={open} busy={busy} />
    <Status phase={task.phase} notice={result ? t(result.status === 'duplicate' ? 'duplicate' : 'imported') : undefined} t={t} />
  </section>
}

/** HOC contribution adds a control beside, never instead of, the existing New session action. */
export function ImportAction({ children, wide, t, onOpen }: { children: ReactNode; wide: boolean; t: Translate; onOpen: () => void }) {
  return <>{children}<Button size="sm" aria-label={t('import')} title={t('import')} onClick={onOpen} icon={<IconUploadOutlineRegular size={16} />}>{wide ? t('import') : null}</Button></>
}
