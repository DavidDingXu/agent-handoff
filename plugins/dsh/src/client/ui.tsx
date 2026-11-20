/** Native DSH controls, shared by the installed slot entries and focused interaction tests. */
import { useCallback, useEffect, useId, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from 'react'
import { Button, Input, MenuSurface, Modal, SegmentedTabs, IconShareOutlineRegular, IconChevronDownOutlineRegular, IconCloseOutlineRegular, IconCheckOutlineRegular, writeClipboard } from '@deepseek-ai/dsh-client-ui-primitives'
import { handoffStyles } from './styles.ts'
import { HandoffApiError, readUpload, saveDownload, type ApiCall } from './api.ts'
import { warningText, type Translate, type CopyKey } from './locales.ts'
import { PROVIDER_WARNING } from '../types.ts'
import type { DownloadResult, HandoffError, ImportMode, ImportPreview, ImportResult, ImportSource, SessionSummary, ShareFormat, ShareResult, WorkspaceChoice, TaskBrief, CodeInspection, ExportOptions, SwitchTarget, SwitchResult, SwitchedTask, CodeManifest } from '../types.ts'

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
    code_unavailable: 'codeError', code_capture_failed: 'codeError', invalid_code: 'handoffError', invalid_brief: 'handoffError', code_apply_failed: 'codeApplyError',
    code_import_partial: 'codePartial',
    code_import_uncertain: 'codeUncertain',
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
  return <div role="alert" className="handoff-alert" style={{ ...column, gap: 8 }}>
    <p style={{ margin: 0 }}>{errorText(error, t)}</p>
    {error.recoveryDirectory && <p className="handoff-excerpt">{error.recoveryDirectory}</p>}
    {error.recoverySessionId && openSession && <>
      <p style={identity}>{error.recoverySessionId}</p>
      <Button variant="outline" disabled={busy} onClick={() => openSession(error.recoverySessionId!)}>{t('openCreated')}</Button>
    </>}
  </div>
}
function Warnings({ warnings, t }: { warnings: string[]; t: Translate }) {
  if (!warnings.length) return null
  return <div className="handoff-warning"><strong>{t('warnings')}</strong><ul>{warnings.map((text, at) => <li key={at}>{warningText(text, t)}</li>)}</ul></div>
}
function Status({ phase, notice, t }: { phase: CopyKey | null; notice?: string; t: Translate }) {
  return <div role="status" aria-live="polite" className="handoff-status">
    {phase ? <><progress aria-label={t(phase)} style={{ width: '100%' }} /><p style={{ margin: '4px 0 0' }}>{t(phase)}</p></> : notice ?? ''}
  </div>
}
function BriefView({ value, t }: { value?: TaskBrief; t: Translate }) {
  if (!value) return null
  return <section className="handoff-brief handoff-stack" aria-label={t('brief')}>
    <div><h3>{t('brief')}</h3><p className="handoff-caption">{t('briefHelp')}</p></div>
    {(['objective', 'latest_request', 'latest_update'] as const).map(key => {
      const excerpt = value[key]
      if (!excerpt || key === 'latest_request' && excerpt.entry_index === value.objective?.entry_index) return null
      return <details key={key}><summary>{t(key === 'latest_request' ? 'latestRequest' : key === 'latest_update' ? 'latestUpdate' : 'objective')}<span className="handoff-brief-teaser">“{excerpt.text.slice(0, 100)}{excerpt.text.length > 100 ? '…' : ''}”</span></summary>
        <p className="handoff-excerpt">{excerpt.text}</p><span className="handoff-caption">{t('sourceEntry')} {excerpt.entry_index + 1}{excerpt.truncated ? ' …' : ''}</span></details>
    })}
    {value.evidence?.length > 0 && <details><summary>{t('evidence')} · {value.evidence.length}</summary>{value.evidence.map((entry, at) => <details key={at}><summary>{entry.tool} · {entry.status}</summary><pre className="handoff-excerpt">{entry.input}{'\n'}{entry.output}</pre></details>)}</details>}
    {value.notes && <div><h3>{t('handoffNote')}</h3><p className="handoff-excerpt">{value.notes}</p></div>}
  </section>
}
function CodeInventory({ code, t }: { code: CodeManifest; t: Translate }) {
  return <div className="handoff-stack"><p className="handoff-caption">{t('codeBase')}: <code>{code.base_commit.slice(0, 12)}</code></p><ul className="handoff-code-list">{code.files.map(file => <li key={file.path}><span>{file.path}</span><small>{file.status} · {file.bytes.toLocaleString()} B</small></li>)}</ul></div>
}
function SwitchResultDialog({ value, t, copy, onClose }: { value: SwitchedTask; t: Translate; copy: (text: string) => Promise<boolean>; onClose: () => void }) {
  const task = useTask()
  const [copied, setCopied] = useState(false)
  const [failed, setFailed] = useState(false)
  const copyCommand = () => void task.run('copyingCommand', async signal => {
    setCopied(false); setFailed(false)
    let accepted = false
    try { accepted = await copy(value.command) } catch { /* Keep the command available for manual copying. */ }
    signal.throwIfAborted(); setCopied(accepted); setFailed(!accepted)
  })
  return <Modal open onClose={onClose} closeLabel={t('close')} title={t('switchResultTitle')} description={t('switchNextStep')}
    className="agent-handoff agent-handoff-switch"
    footer={<><Button variant="outline" onClick={onClose}>{t('close')}</Button><Button variant="primary" data-modal-autofocus disabled={task.phase !== null} onClick={copyCommand}>{t('copyCommand')}</Button></>}>
    <style>{handoffStyles}</style>
    <div className="handoff-stack">
      <div><strong className="handoff-result-title">{value.targetAgent === 'claude' ? 'Claude Code' : 'Codex'}</strong><p className="handoff-caption">{value.title}</p></div>
      <label className="handoff-field"><span>{t('continueCommand')}</span><textarea readOnly rows={3} className="handoff-textarea handoff-command" value={value.command} aria-label={t('continueCommand')} onFocus={event => event.currentTarget.select()} /></label>
      <div><span className="handoff-label">{t('workingDirectory')}</span><p className="handoff-switch-directory">{value.directory}</p><p className="handoff-caption">{t('switchDirectoryHelp')}</p></div>
      <Status phase={task.phase} notice={copied ? t('commandCopied') : undefined} t={t} />
      {failed && <p role="alert" className="handoff-alert">{t('commandCopyFailed')}</p>}
    </div>
  </Modal>
}
function ShareResultDialog({ value, call, t, copy, save, busy, error, switchFallback, onClose, onRegenerate }: {
  value: ShareResult; call: ApiCall; t: Translate; copy: (text: string) => Promise<boolean>; save: (file: DownloadResult) => void;
  busy: boolean; error?: HandoffError; switchFallback: boolean; onClose: () => void; onRegenerate: () => void
}) {
  const task = useTask()
  const [notice, setNotice] = useState<CopyKey | null>(null)
  const [copyFailed, setCopyFailed] = useState(false)
  const [now, setNow] = useState(Date.now)
  const link = value.status === 'ok' ? value.shareUrl : undefined
  const expires = value.expiresAt ? Date.parse(value.expiresAt) : NaN
  const expired = !!link && Number.isFinite(expires) && expires <= now
  useEffect(() => {
    if (!link || !Number.isFinite(expires) || expires <= now) return
    const timer = setTimeout(() => setNow(Date.now()), Math.min(expires - now, 2147483647))
    return () => clearTimeout(timer)
  }, [link, expires, now])
  const download = () => void task.run('downloading', async signal => {
    setNotice(null)
    const file = await call<DownloadResult>({ action: 'download', downloadId: value.downloadId! }, signal)
    signal.throwIfAborted(); save(file); setNotice('downloadStarted')
  })
  const copyLink = () => {
    if (Number.isFinite(expires) && expires <= Date.now()) { setNow(Date.now()); return }
    void task.run('copying', async signal => {
      setNotice(null); setCopyFailed(false)
      let accepted = false
      try { accepted = await copy(link!) } catch { /* Preserve the link for manual copying. */ }
      signal.throwIfAborted(); setCopyFailed(!accepted); if (accepted) setNotice('copied')
    })
  }
  const fallback = value.status === 'fallback_zip'
  const warnings = value.warnings.filter(text => text !== PROVIDER_WARNING && (!fallback || text !== 'Encrypted link upload was unavailable. The local file is ready to download.'))
  const disabled = busy || task.phase !== null
  const downloadExpired = !switchFallback && task.error?.code === 'download_expired'
  return <Modal open onClose={onClose} closeLabel={t('close')}
    title={t(switchFallback ? 'switchUnconfirmedTitle' : link ? 'linkReadyTitle' : fallback ? 'linkFailedTitle' : 'zipReadyTitle')}
    description={t(switchFallback ? 'switchFallback' : fallback ? 'fallback' : link ? 'linkNextStep' : 'zipNextStep')}
    className="agent-handoff agent-handoff-switch"
    footer={<><Button variant="outline" onClick={onClose}>{t('close')}</Button>
      {expired || downloadExpired ? <Button variant="primary" data-modal-autofocus disabled={disabled} onClick={onRegenerate}>{t(link ? 'createNewShare' : 'prepareZipAgain')}</Button>
        : link ? <Button variant="primary" data-modal-autofocus disabled={disabled} onClick={copyLink}>{t('copyLink')}</Button>
        : value.downloadId && <Button variant="primary" data-modal-autofocus disabled={disabled} onClick={download}>{t('download')}</Button>}</>}>
    <style>{handoffStyles}</style>
    <div className="handoff-stack">
      <div><strong className="handoff-result-title">{value.title}</strong><p className="handoff-caption">{t('messages')}: {value.messageCount}</p></div>
      {link && <label className="handoff-field"><span>{t('resultLink')}</span><textarea className="handoff-textarea handoff-command" readOnly rows={3} aria-label={t('resultLink')} value={link} onFocus={event => event.currentTarget.select()} /></label>}
      {link && Number.isFinite(expires) && <p className="handoff-caption">{t('expires')}: <time dateTime={value.expiresAt}>{new Intl.DateTimeFormat(t('dateLocale'), { dateStyle: 'medium', timeStyle: 'short' }).format(expires)}</time></p>}
      {expired && <p role="alert" className="handoff-alert">{t('shareExpired')}</p>}
      <Warnings warnings={warnings} t={t} />
      {link && (value.downloadId || value.warnings.includes(PROVIDER_WARNING)) && <details className="handoff-details"><summary>{t('shareDetails')}</summary><div className="handoff-stack handoff-details-content">
        {value.warnings.includes(PROVIDER_WARNING) && <p className="handoff-caption">{t('warnProviders')}</p>}
        {value.downloadId && <div className="handoff-actions"><Button variant="outline" size="sm" disabled={disabled} onClick={download}>{t('download')}</Button></div>}
      </div></details>}
      <ErrorBox error={task.error ?? error} t={t} />
      {copyFailed && <p role="alert" className="handoff-alert">{t('copyFailed')}</p>}
      <Status phase={busy ? 'preparing' : task.phase} notice={notice ? t(notice) : undefined} t={t} />
    </div>
  </Modal>
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
export function ShareForm({ sessionId, call, t, copy = writeClipboard, save = saveDownload }: Omit<ShareDialogProps, 'onClose'>) {
  const task = useTask()
  const [summary, setSummary] = useState<SessionSummary | null>(null)
  const [format, setFormat] = useState<ShareFormat>('zip')
  const [result, setResult] = useState<ShareResult | null>(null)
  const [consent, setConsent] = useState(false)
  const [notice, setNotice] = useState<CopyKey | null>(null)
  const [shareDialogOpen, setShareDialogOpen] = useState(false)
  const [reload, setReload] = useState(0)
  const actionId = useId()
  const [operation, setOperation] = useState<'share' | 'switch'>('share')
  const [target, setTarget] = useState<SwitchTarget>('claude')
  const [switched, setSwitched] = useState<SwitchedTask | null>(null)
  const [switchDialogOpen, setSwitchDialogOpen] = useState(false)
  const [briefNote, setBriefNote] = useState('')
  const [includeCode, setIncludeCode] = useState(false)
  const [code, setCode] = useState<CodeInspection | null>(null)
  const [selectedFiles, setSelectedFiles] = useState<string[]>([])
  useEffect(() => {
    void task.run('loading', async signal => {
      const value = await call<SessionSummary>({ action: 'summary', sessionId }, signal)
      signal.throwIfAborted(); setSummary(value)
    })
    return task.cancel
  }, [call, sessionId, reload, task.run, task.cancel])
  const busy = task.phase !== null
  const empty = summary?.messageCount === 0
  const invalidate = () => { setResult(null); setShareDialogOpen(false); setSwitched(null); setSwitchDialogOpen(false); setConsent(false); setNotice(null); task.clearError() }
  const inspectCode = () => void task.run('inspectingCode', async signal => {
    const value = await call<CodeInspection>({ action: 'code-preview', sessionId }, signal)
    signal.throwIfAborted(); setCode(value); setSelectedFiles(value.files.filter(file => file.eligible && (file.status === 'modified' || file.status === 'deleted')).map(file => file.path))
  })
  const share = (secretTicket?: string) => {
    if (!summary || empty) return
    void task.run(operation === 'switch' ? 'switching' : 'preparing', async signal => {
      setNotice(null)
      const options: ExportOptions = { ...(briefNote ? { briefNote } : {}), ...(includeCode ? { codeFiles: selectedFiles } : {}) }
      const extra = { ...(Object.keys(options).length ? { options } : {}), ...(secretTicket ? { secretTicket } : {}) }
      if (operation === 'switch') {
        const value = await call<SwitchResult>({ action: 'switch', sessionId, target, ...extra }, signal)
        signal.throwIfAborted(); setConsent(false)
        if ('targetAgent' in value) { setResult(null); setSwitched(value); setSwitchDialogOpen(true); setNotice('switchReady') }
        else { setResult(value); setShareDialogOpen(value.status === 'fallback_zip'); if (value.status === 'fallback_zip') setNotice('switchFallback') }
        return
      }
      const value = await call<ShareResult>({ action: 'share', sessionId, format, ...extra }, signal)
      signal.throwIfAborted(); setResult(value); setConsent(false)
      setShareDialogOpen(value.status !== 'blocked')
      if (value.status !== 'blocked') setNotice(value.status === 'fallback_zip' ? 'fallback' : 'shareReady')
    })
  }
  const blocked = result?.status === 'blocked'
  return <div className="agent-handoff handoff-columns">
    <section className="handoff-card handoff-stack" aria-label={t('exportSettings')}>
      <div className="handoff-card-header"><h2>{t(operation === 'switch' ? 'switchAgent' : 'exportSettings')}</h2><p>{t(operation === 'switch' ? 'switchHelp' : 'exportHelp')}</p></div>
      <div className="handoff-primary-controls handoff-stack">
      <SegmentedTabs<'share' | 'switch'> label={t('actions')} value={operation} onChange={value => { if(busy)return;setOperation(value); invalidate() }} items={[
        {value:'share',label:t('share'),id:`${actionId}-share`,panelId:`${actionId}-share-body`},{value:'switch',label:t('switchAgent'),id:`${actionId}-switch`,panelId:`${actionId}-switch-body`},
      ]} />
      <div id={`${actionId}-share-body`} role="tabpanel" aria-labelledby={`${actionId}-share`} hidden={operation !== 'share'} className="handoff-stack">
      <label className="handoff-field"><span>{t('format')}</span><select aria-label={t('format')} value={format} disabled={busy} className="handoff-select" onChange={event => {
        setFormat(event.currentTarget.value as ShareFormat); invalidate()
      }}><option value="zip">{t('zip')}</option><option value="link">{t('link')}</option></select></label>
      <div className="handoff-notice"><IconShareOutlineRegular size={16} /><p>{t(format === 'zip' ? 'zipNotice' : 'linkNotice')}</p></div>
      </div>
      <div id={`${actionId}-switch-body`} role="tabpanel" aria-labelledby={`${actionId}-switch`} hidden={operation !== 'switch'} className="handoff-stack">
        <label className="handoff-field"><span>{t('continueIn')}</span><select aria-label={t('continueIn')} className="handoff-select" value={target} disabled={busy} onChange={event => { setTarget(event.currentTarget.value as SwitchTarget); invalidate() }}><option value="claude">Claude Code</option><option value="codex">Codex</option></select></label>
        <div className="handoff-notice"><p>{t('localOnly')}</p></div>
      </div>
      {!blocked && <div className="handoff-actions"><Button variant="primary" disabled={busy || summary === null || empty || includeCode && !selectedFiles.length} onClick={() => {
        if (switched) setSwitchDialogOpen(true); else if (result) setShareDialogOpen(true); else share()
      }}>{t(switched ? 'viewContinueCommand' : result ? 'viewShareResult' : operation === 'switch' ? 'switchAgent' : 'createShare')}</Button>
        {operation === 'share' && format === 'link' && result && <Button variant="outline" size="sm" disabled={busy} onClick={() => share()}>{t('createNewShare')}</Button>}
      </div>}
      {blocked && result.secretTicket && <div className="handoff-actions"><Button variant="primary" disabled={busy || !consent} onClick={() => { if (consent) share(result.secretTicket) }}>{t('overrideShare')}</Button></div>}
      <ErrorBox error={task.error} t={t} />
      <Status phase={task.phase} notice={notice ? t(notice) : undefined} t={t} />
      </div>
      <details className="handoff-details"><summary>{t('optionalExport')}</summary><div className="handoff-stack handoff-details-content">
      <label className="handoff-field"><span>{t('handoffNote')}</span><textarea aria-label={t('handoffNote')} className="handoff-textarea" rows={2} maxLength={8192} value={briefNote} disabled={busy} placeholder={t('noteHint')} onChange={event => { setBriefNote(event.currentTarget.value); invalidate() }} /></label>
      <section className="handoff-stack handoff-code" aria-label={t('codeFiles')}>
        <label className="handoff-check"><input type="checkbox" checked={includeCode} disabled={busy} onChange={event => { const checked=event.currentTarget.checked;setIncludeCode(checked);invalidate();if(checked)inspectCode() }} /><span>{t('includeCode')}</span></label>
        {includeCode && <><p className="handoff-caption">{t('codeHelp')}</p>{code ? <>
          <p className="handoff-caption">{t('codeBase')}: <code>{code.base_commit.slice(0,12)}</code></p>
          <div className="handoff-code-selection">{code.files.map(file => <label key={file.path} className="handoff-code-option"><input type="checkbox" aria-label={file.path} disabled={busy || !file.eligible} checked={selectedFiles.includes(file.path)} onChange={event => { const checked=event.currentTarget.checked;setSelectedFiles(files=>checked?[...files,file.path]:files.filter(path=>path!==file.path));invalidate() }} /><span><span>{file.path}</span><small>{file.status} · {file.bytes.toLocaleString()} B{file.reason ? ` · ${file.reason}` : ''}</small></span></label>)}</div>
          {!code.files.some(file=>file.eligible) && <p className="handoff-caption">{t('noCode')}</p>}
          <p className="handoff-caption">{t('selectedBytes')}: {code.files.filter(file=>selectedFiles.includes(file.path)).reduce((sum,file)=>sum+file.bytes,0).toLocaleString()} B</p>
        </> : !busy && <Button size="sm" variant="outline" onClick={inspectCode}>{t('retry')}</Button>}</>}
      </section>
      </div></details>
      {empty && <div className="handoff-stack"><p className="handoff-caption">{t('emptyTask')}</p><div className="handoff-actions"><Button size="sm" variant="outline" disabled={busy} onClick={() => setReload(value => value + 1)}>{t('refreshSummary')}</Button></div></div>}
      {blocked && <section className="handoff-warning handoff-stack" aria-label={t('blocked')}>
        <strong>{t('blocked')}</strong><p style={muted}>{t('blockedDescription')}</p>
        {result.findings?.length ? <ul>{result.findings.map((finding, at) => <li key={at} style={{ overflowWrap: 'anywhere' }}><strong>{finding.rule}</strong>{finding.field && <p style={identity}>{t('field')}: {finding.field}</p>}{finding.line !== undefined && <p style={identity}>{t('line')}: {finding.line}</p>}</li>)}</ul> : <p style={muted}>{t('noFindingDetails')}</p>}
        {result.secretTicket && <><label style={{ ...row, alignItems: 'flex-start' }}><input type="checkbox" checked={consent} disabled={busy} onChange={event => setConsent(event.currentTarget.checked)} /><span>{t('allowSecrets')}</span></label>
          </>}
        <div className="handoff-actions"><Button size="sm" variant="outline" disabled={busy} onClick={() => share()}>{t('retry')}</Button></div>
      </section>}
      {!summary && !busy && <div className="handoff-actions"><Button size="sm" variant="outline" onClick={() => setReload(value => value + 1)}>{t('retry')}</Button></div>}
    </section>
    <aside className="handoff-card handoff-context handoff-stack" aria-label={t('taskOverview')}>
      <div><h2>{t('taskOverview')}</h2>{summary ? <><p className="handoff-task-title">{summary.title}</p><p className="handoff-caption">{t('messages')}: {summary.messageCount}</p></> : <p className="handoff-caption">{t('loading')}</p>}</div>
      {summary && <Warnings warnings={summary.warnings} t={t} />}
      <BriefView value={summary?.brief} t={t} />
      <details className="handoff-details"><summary>{t('handoffDetails')}</summary><p style={identity}>{t('session')}: <span>{sessionId}</span></p></details>
    </aside>
    {switched && switchDialogOpen && <SwitchResultDialog value={switched} t={t} copy={copy} onClose={() => setSwitchDialogOpen(false)} />}
    {result && !blocked && shareDialogOpen && <ShareResultDialog key={result.shareUrl ?? result.downloadId} value={result} call={call} t={t} copy={copy} save={save} busy={busy} error={task.error} switchFallback={operation === 'switch'} onClose={() => setShareDialogOpen(false)} onRegenerate={() => share()} />}
  </div>
}

/** Header shortcut shares the same safe form as the hub, inside DSH's native overlay. */
export function ShareDialog({ onClose, ...props }: ShareDialogProps) {
  return <Modal open onClose={onClose} closeLabel={props.t('close')} title={props.t('panel')} description={props.t('hubDescription')}
    className="agent-handoff-share" contentClassName="agent-handoff-share-content"
    footer={<Button variant="outline" onClick={onClose}>{props.t('close')}</Button>}>
    <div className="handoff-dialog"><style>{handoffStyles}</style><ShareForm {...props} /></div>
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
  const [sourceKind, setSourceKind] = useState<'upload' | 'link'>('upload')
  const [file, setFile] = useState<File | null>(null)
  const [dragging, setDragging] = useState(false)
  const [link, setLink] = useState('')
  const [preview, setPreview] = useState<ImportPreview | null>(null)
  const [mode, setMode] = useState<ImportMode | ''>('')
  const [result, setResult] = useState<ImportResult | null>(null)
  const [recoverySessionId, setRecoverySessionId] = useState<string | undefined>()
  const [recoveryMessage, setRecoveryMessage] = useState('')
  const [pickerOpen, setPickerOpen] = useState(false)
  const [expiredTicket, setExpiredTicket] = useState(false)
  const [reload, setReload] = useState(0)
  const pickerAnchor = useRef<HTMLButtonElement>(null)
  const modeInput = useRef<HTMLSelectElement>(null)
  const focusedPreview = useRef('')
  const [applyCode, setApplyCode] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)
  useEffect(() => {
    void task.run('loading', async signal => {
      const items = await call<WorkspaceChoice[]>({ action: 'workspaces' }, signal)
      signal.throwIfAborted(); setWorkspaces(items)
    })
    return task.cancel
  }, [call, reload, task.run, task.cancel])
  const busy = task.phase !== null
  useEffect(() => {
    if (!busy && preview && focusedPreview.current !== preview.ticketId) {
      modeInput.current?.focus()
      focusedPreview.current = preview.ticketId
    }
  }, [busy, preview?.ticketId])
  const selected = workspaces.find(item => item.id === workspaceId)
  const resetPreview = () => { setPreview(null); setMode(''); setApplyCode(false); setResult(null); setRecoverySessionId(undefined); setRecoveryMessage(''); setExpiredTicket(false); task.clearError() }
  const changeWorkspace = (id: string) => {
    setWorkspaceId(id); setPickerOpen(false); resetPreview()
    // Native picker can add a new registered workspace. Refresh only its choices, retaining the selected id.
    if (!workspaces.some(item => item.id === id)) setReload(value => value + 1)
  }
  const sourceReady = sourceKind === 'upload' ? file !== null : link.trim() !== ''
  const loadPreview = () => void task.run('previewing', async signal => {
    setPreview(null); setMode(''); setApplyCode(false); setResult(null); setExpiredTicket(false)
    const source: ImportSource = sourceKind === 'upload' ? await readUpload(file!, signal)
      : { kind: 'link', url: link.trim() }
    const value = await call<ImportPreview>({ action: 'preview', source, workspaceId }, signal)
    signal.throwIfAborted(); setPreview(value)
  })
  const confirm = () => {
    if (!preview || !mode || !preview.modes.includes(mode) || preview.workspace.id !== workspaceId || recoverySessionId) return
    if (!Number.isFinite(Date.parse(preview.expiresAt)) || Date.parse(preview.expiresAt) <= Date.now()) { setExpiredTicket(true); return }
    void task.run('importing', async signal => {
      try {
        const value = await call<ImportResult>({ action: 'import', ticketId: preview.ticketId, workspaceId: preview.workspace.id, mode, ...(applyCode ? { applyCode: true } : {}) }, signal)
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
  return <section className="agent-handoff handoff-columns" aria-label={t('import')}>
    <style>{handoffStyles}</style>
    <div className="handoff-card handoff-stack">
      <div className="handoff-card-header"><h2>{t('importSource')}</h2><p>{t('importHelp')}</p></div>
      <div className="handoff-fields">
        <label className="handoff-field"><span>{t('source')}</span><select aria-label={t('source')} className="handoff-select" disabled={busy} value={sourceKind} onChange={event => { setSourceKind(event.currentTarget.value === 'link' ? 'link' : 'upload'); resetPreview() }}><option value="upload">{t('file')}</option><option value="link">{t('shareLink')}</option></select></label>
        <div className="handoff-field"><label className="handoff-label" htmlFor="agent-handoff-workspace">{t('workspace')}</label>
          {renderWorkspacePicker ? <><Button id="agent-handoff-workspace" ref={pickerAnchor} className="handoff-workspace-button" variant="outline" disabled={busy} aria-label={t('workspace')} aria-haspopup="menu" aria-expanded={pickerOpen} onClick={() => setPickerOpen(value => !value)}>{selected?.name ?? t('chooseWorkspace')}</Button>
            {renderWorkspacePicker({ open: pickerOpen, anchorRef: pickerAnchor, ...(workspaceId ? { selectedId: workspaceId } : {}), onPick: changeWorkspace, onClose: () => setPickerOpen(false) })}</>
            : <select id="agent-handoff-workspace" aria-label={t('workspace')} className="handoff-select" disabled={busy} value={workspaceId} onChange={event => changeWorkspace(event.currentTarget.value)}><option value="">{t('chooseWorkspace')}</option>{workspaces.map(item => <option key={item.id} value={item.id}>{item.name} — {item.directory}</option>)}</select>}
          {selected && <span className="handoff-path" title={selected.directory}>{selected.directory}</span>}
        </div>
      </div>
      {!workspaces.length && !busy && <div className="handoff-stack"><p className="handoff-caption">{t('noWorkspaces')}</p><div className="handoff-actions"><Button size="sm" variant="outline" onClick={() => setReload(value => value + 1)}>{t('refreshWorkspaces')}</Button></div></div>}
      {sourceKind === 'upload' ? <div className="handoff-upload" data-dragging={dragging} onDragOver={event => { event.preventDefault(); if (!busy) setDragging(true) }} onDragLeave={() => setDragging(false)} onDrop={event => { event.preventDefault(); setDragging(false); if (busy) return; const chosen = event.dataTransfer.files[0]; if (chosen) { setFile(chosen); resetPreview() } }}>
        <IconShareOutlineRegular size={25} />
        <span className="handoff-file-name">{file?.name ?? t('uploadHint')}</span><p>{t('uploadLimit')}</p>
        <Button variant="outline" size="sm" disabled={busy} onClick={() => fileInput.current?.click()}>{t('chooseFile')}</Button>
        <input ref={fileInput} type="file" accept=".zip,application/zip,application/x-zip-compressed" aria-label={t('chooseFile')} disabled={busy} style={{ display: 'none' }} onChange={event => { setFile(event.currentTarget.files?.[0] ?? null); event.currentTarget.value = ''; resetPreview() }} />
      </div> : <label className="handoff-field"><span>{t('shareLink')}</span><Input className="handoff-input" aria-label={t('shareLink')} value={link} disabled={busy} placeholder="https://…#…" onChange={event => { setLink(event.currentTarget.value); resetPreview() }} /></label>}
      {!result && <div className="handoff-actions"><Button variant="primary" disabled={busy || !selected || !sourceReady} onClick={loadPreview}>{t('preview')}</Button></div>}
      <ErrorBox error={recoverySessionId ? { ...(task.error ?? { code: 'import/recovery', message: recoveryMessage }), recoverySessionId } : task.error} t={t} openSession={open} busy={busy} />
      <Status phase={task.phase} notice={result ? t(result.status === 'duplicate' ? 'duplicate' : 'imported') : undefined} t={t} />
    </div>
    <aside className="handoff-card handoff-context handoff-stack" aria-label={t('previewTitle')}>
      {preview && !result ? <>
        <div><h2>{t('previewTitle')}</h2><p className="handoff-task-title">{preview.title}</p><p className="handoff-meta-line"><span>{({ dsh: 'DSH', codex: 'Codex', claude: 'Claude Code' } as Record<string, string>)[preview.sourceAgent] ?? preview.sourceAgent}</span><span aria-hidden>·</span><span>{t('messages')}: {preview.messageCount}</span><span aria-hidden>·</span><span>{['ok', 'verified'].includes(preview.checksumStatus) ? t('verified') : preview.checksumStatus === 'not_available_v1' ? t('legacyChecksum') : preview.checksumStatus}</span></p></div>
        <div className="handoff-primary-controls handoff-stack">
        <label className="handoff-field"><span>{t('importMode')}</span><select ref={modeInput} aria-label={t('importMode')} className="handoff-select" disabled={busy} value={mode} onChange={event => setMode(event.currentTarget.value as ImportMode)}><option value="">{t('chooseMode')}</option>{preview.modes.map(item => <option value={item} key={item}>{t(item === 'native' ? 'native' : 'conversation')}</option>)}</select></label>
        <p className="handoff-caption">{t(!mode && preview.modes.length !== 1 ? 'modeHelp' : (mode || preview.modes[0]) === 'native' ? 'nativeDescription' : 'conversationDescription')}</p>
        {preview.code && <label className="handoff-check"><input type="checkbox" disabled={busy || !preview.codeDirectory} checked={applyCode} onChange={event=>setApplyCode(event.currentTarget.checked)} /><span>{t('applyCode')}</span></label>}
        {applyCode && preview.codeDirectory && <p className="handoff-path" title={preview.codeDirectory}>{t('codeDestination')}: {preview.codeDirectory}</p>}
        {expired && <p role="alert" className="handoff-alert">{t('expired')}</p>}
        {(applyCode ? preview.existingCodeSessionId : preview.existingSessionId) && <p className="handoff-caption">{t('duplicatePreview')}</p>}
        <div className="handoff-actions"><Button variant="primary" disabled={busy || !mode || expired || !!recoverySessionId || preview.workspace.id !== workspaceId} onClick={confirm}>{t('confirmation')}</Button></div>
        </div>
        <BriefView value={preview.brief} t={t} />
        {preview.code && <section className="handoff-stack"><h3>{t('codeIncluded')}</h3><CodeInventory code={preview.code} t={t} /><p className="handoff-caption">{t('codeNotApplied')}</p>
          <p className="handoff-caption">{t('applyCodeHelp')}</p>
          {preview.codeDirectory && <div><h3>{t('codeDestination')}</h3><p className="handoff-excerpt">{preview.codeDirectory}</p></div>}
        </section>}
        <dl className="handoff-facts">
          <div><dt>{t('selectedWorkspace')}</dt><dd>{preview.workspace.name}<span className="handoff-path" title={preview.workspace.directory}>{preview.workspace.directory}</span></dd></div>
        </dl>
        <details className="handoff-details"><summary>{t('handoffDetails')}</summary><p style={identity}>{t('sourceIdentity')}: <span>{preview.sourceSessionId}</span></p></details>
        <Warnings warnings={preview.warnings} t={t} />
      </> : result ? <section className="handoff-result"><h2>{t('resultHeading')}</h2><strong className="handoff-result-title">{result.title}</strong><p style={identity}>{result.sessionId}</p><Warnings warnings={result.warnings} t={t} /><div className="handoff-actions"><Button variant="primary" disabled={busy} onClick={() => open(result.sessionId)}>{t('openTask')}</Button><Button variant="outline" size="sm" disabled={busy} onClick={resetPreview}>{t('importAnother')}</Button></div></section>
        : <><h2>{t('previewContext')}</h2><ol className="handoff-steps">{(['stepSource', 'stepPreview', 'stepConfirm'] as const).map((key, index) => <li key={key}><span className="handoff-step-number" aria-hidden>{index + 1}</span><div><strong>{t(key)}</strong><p>{t((`${key}Help`) as CopyKey)}</p></div></li>)}</ol></>}
    </aside>
  </section>
}

/** HOC contribution adds a control beside, never instead of, the existing New session action. */
export function ImportAction({ children, wide, t, onOpen }: { children: ReactNode; wide: boolean; t: Translate; onOpen: () => void }) {
  return <>{children}<Button size="sm" aria-label={t('import')} title={t('import')} onClick={onOpen} icon={<IconShareOutlineRegular size={16} />}>{wide ? t('import') : null}</Button></>
}

export interface ExportSessionChoice { id: string; title: string; directory?: string }
/** One editable selector: queries never commit a different export identity. */
function TaskPicker({ sessions, selectedId, onSelect, disabled, t }: { sessions: ExportSessionChoice[]; selectedId: string; onSelect: (id: string) => void; disabled?: boolean; t: Translate }) {
  const id = useId()
  const root = useRef<HTMLDivElement>(null)
  const input = useRef<HTMLInputElement>(null)
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [active, setActive] = useState(0)
  const selected = sessions.find(item => item.id === selectedId)
  const query = search.trim().toLowerCase()
  const matches = sessions.filter(item => `${item.title} ${item.directory ?? ''}`.toLowerCase().includes(query))
  const expanded = open && !disabled
  const activeId = expanded && matches[active] ? `${id}-option-${active}` : undefined
  const selectedLabel = selected ? `${selected.title}${selected.directory ? ` — ${selected.directory}` : ''}` : ''
  const begin = () => { setSearch(''); setActive(Math.max(0, sessions.findIndex(item => item.id === selectedId))); setOpen(true) }
  const choose = (item: ExportSessionChoice) => { onSelect(item.id); input.current?.focus(); setOpen(false); setSearch('') }
  useEffect(() => {
    if (!expanded) return
    const dismiss = (event: PointerEvent) => { if (!root.current?.contains(event.target as Node)) setOpen(false) }
    const blur = () => setOpen(false)
    document.addEventListener('pointerdown', dismiss)
    window.addEventListener('blur', blur)
    return () => { document.removeEventListener('pointerdown', dismiss); window.removeEventListener('blur', blur) }
  }, [expanded])
  useEffect(() => { if (activeId) document.getElementById(activeId)?.scrollIntoView?.({ block: 'nearest' }) }, [activeId, search])
  return <div className="handoff-field">
    <label className="handoff-label" htmlFor={`${id}-input`}>{t('taskToExport')}</label>
    <div className="handoff-task-picker" ref={root} onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setOpen(false) }}>
      <Input ref={input} id={`${id}-input`} className="handoff-input handoff-picker-input" role="combobox" aria-autocomplete="list" aria-expanded={expanded} aria-controls={expanded ? `${id}-list` : undefined} aria-activedescendant={activeId}
        autoComplete="off" placeholder={t('searchTasksHint')} disabled={disabled} value={expanded ? search : selectedLabel}
        onFocus={begin} onClick={() => { if (!open) begin() }} onChange={event => { setSearch(event.currentTarget.value); setActive(0); setOpen(true) }}
        onKeyDown={event => {
          if (event.nativeEvent.isComposing || event.keyCode === 229) return
          if (event.key === 'Escape' || event.key === 'Tab') {
            if (event.key === 'Escape' && expanded) { event.preventDefault(); event.stopPropagation() }
            setOpen(false)
          } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            event.preventDefault()
            if (!expanded) begin()
            else if (matches.length) setActive(value => (value + (event.key === 'ArrowDown' ? 1 : -1) + matches.length) % matches.length)
          } else if (event.key === 'Enter' && expanded) {
            event.preventDefault()
            const item = matches[active]
            if (item) choose(item)
          }
        }} />
      <div className="handoff-picker-actions">
        {expanded && search && <Button size="sm" variant="ghost" tabIndex={-1} aria-label={t('clearSearch')} onMouseDown={event => event.preventDefault()} onClick={() => { setSearch(''); setActive(0); input.current?.focus() }} icon={<IconCloseOutlineRegular size={14} />} />}
        <Button size="sm" variant="ghost" tabIndex={-1} aria-label={t('chooseTask')} disabled={disabled} onMouseDown={event => event.preventDefault()} onClick={() => { if (expanded) setOpen(false); else { input.current?.focus(); begin() } }} icon={<IconChevronDownOutlineRegular size={16} />} />
      </div>
      {expanded && <MenuSurface compact className="handoff-picker-menu">
        {selected && query && !matches.some(item => item.id === selectedId) && <p className="handoff-picker-current">{t('selectedTask')}: {selected.title}</p>}
        <div role="listbox" id={`${id}-list`} aria-label={t('taskToExport')} className="handoff-picker-results">
          {matches.map((item, index) => <div role="option" id={`${id}-option-${index}`} key={item.id} aria-selected={item.id === selectedId} tabIndex={-1} className="handoff-picker-option" data-active={index === active} onMouseEnter={() => setActive(index)} onMouseDown={event => event.preventDefault()} onClick={() => choose(item)}>
            <span className="handoff-picker-copy"><span className="handoff-picker-title">{item.title}</span>{item.directory && <> <span className="handoff-picker-path">{item.directory}</span></>}</span>
            {item.id === selectedId && <IconCheckOutlineRegular size={16} aria-hidden />}
          </div>)}
        </div>
        <p className="handoff-picker-footer" role="status" aria-live="polite">{matches.length ? `${t('matchingTasks')}: ${matches.length} / ${sessions.length}` : t('noMatchingTasks')}</p>
      </MenuSurface>}
    </div>
  </div>
}
export type HubTab = 'export' | 'import'
export interface HandoffHubProps extends ImportPanelProps {
  sessions: ExportSessionChoice[]
  currentSessionId?: string
  catalogLoading?: boolean
  refreshSessions?: () => Promise<void>
  initialTab?: HubTab
  tab?: HubTab
  onTabChange?: (tab: HubTab) => void
}
/** One native hub makes both transfer directions discoverable without loading every transcript. */
export function HandoffHub({ sessions, currentSessionId, catalogLoading, refreshSessions, initialTab = 'export', tab: controlledTab, onTabChange, ...props }: HandoffHubProps) {
  const { t } = props
  const id = useId()
  const [localTab, setLocalTab] = useState<HubTab>(initialTab)
  const tab = controlledTab ?? localTab
  const [visited, setVisited] = useState({ export: true, import: initialTab === 'import' || controlledTab === 'import' })
  const [selectedId, setSelectedId] = useState(() => sessions.some(item => item.id === currentSessionId) ? currentSessionId! : '')
  const task = useTask()
  useEffect(() => { if (!sessions.some(item => item.id === selectedId)) setSelectedId(sessions.some(item => item.id === currentSessionId) ? currentSessionId! : '') }, [sessions, currentSessionId, selectedId])
  useEffect(() => { setVisited(value => value[tab] ? value : { ...value, [tab]: true }) }, [tab])
  const selectTab = (next: HubTab) => { setLocalTab(next); setVisited(value => ({ ...value, [next]: true })); onTabChange?.(next) }
  const selected = sessions.find(item => item.id === selectedId)
  return <section className="agent-handoff handoff-hub" aria-label={t('panel')}>
    <style>{handoffStyles}</style>
    <header className="handoff-heading"><h1>{t('panel')}</h1><p>{t('hubDescription')}</p></header>
    <SegmentedTabs<HubTab> className="handoff-tabs" label={t('actions')} value={tab} onChange={selectTab} items={[
      { value: 'export', label: t('export'), id: `${id}-export`, panelId: `${id}-export-panel` },
      { value: 'import', label: t('import'), id: `${id}-import`, panelId: `${id}-import-panel` },
    ]} />
    <div className="handoff-panel" id={`${id}-export-panel`} role="tabpanel" aria-labelledby={`${id}-export`} hidden={tab !== 'export'}>
      <div className="handoff-session-picker"><TaskPicker sessions={sessions} selectedId={selectedId} onSelect={setSelectedId} disabled={catalogLoading || !sessions.length} t={t} />
        {refreshSessions && <Button size="sm" variant="outline" disabled={task.phase !== null} onClick={() => void task.run('loading', async signal => { await refreshSessions(); signal.throwIfAborted() })}>{t('refreshTasks')}</Button>}
      </div>
      {selected ? <ShareForm key={selected.id} sessionId={selected.id} call={props.call} t={t} /> : <div className="handoff-columns">
        <section className="handoff-card handoff-stack"><div className="handoff-card-header"><h2>{t('exportSettings')}</h2><p>{t('exportHelp')}</p></div><p className="handoff-caption">{t(catalogLoading ? 'loading' : sessions.length ? 'chooseTask' : 'noTasks')}</p></section>
        <aside className="handoff-card handoff-context handoff-stack"><h2>{t('taskOverview')}</h2><p className="handoff-caption">{t('chooseTask')}</p></aside>
      </div>}
      <ErrorBox error={task.error} t={t} />
      <Status phase={task.phase} t={t} />
    </div>
    {visited.import && <div className="handoff-panel" id={`${id}-import-panel`} role="tabpanel" aria-labelledby={`${id}-import`} hidden={tab !== 'import'}><ImportPanel {...props} /></div>}
  </section>
}
