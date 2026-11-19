/** Native DSH controls, shared by the installed slot entries and focused interaction tests. */
import { useCallback, useEffect, useId, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from 'react'
import { Button, Input, MenuSurface, Modal, SegmentedTabs, IconShareOutlineRegular, IconChevronDownOutlineRegular, IconCloseOutlineRegular, IconCheckOutlineRegular, writeClipboard } from '@deepseek-ai/dsh-client-ui-primitives'
import { handoffStyles } from './styles.ts'
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
  return <div role="alert" className="handoff-alert" style={{ ...column, gap: 8 }}>
    <p style={{ margin: 0 }}>{errorText(error, t)}</p>
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
  const empty = summary?.messageCount === 0
  const share = (secretTicket?: string) => {
    if (!summary || empty) return
    void task.run('preparing', async signal => {
      setNotice(null); setCopyError(false)
      const value = await call<ShareResult>({ action: 'share', sessionId, format, ...(secretTicket ? { secretTicket } : {}) }, signal)
      signal.throwIfAborted(); setResult(value); setConsent(false)
      setSummary({ sessionId, title: value.title, messageCount: value.messageCount, warnings: value.warnings })
      if (value.status !== 'blocked') setNotice(value.status === 'fallback_zip' ? 'fallback' : 'shareReady')
    })
  }
  const blocked = result?.status === 'blocked'
  return <div className="agent-handoff handoff-columns">
    <section className="handoff-card handoff-stack" aria-label={t('exportSettings')}>
      <div className="handoff-card-header"><h2>{t('exportSettings')}</h2><p>{t('exportHelp')}</p></div>
      <label className="handoff-field"><span>{t('format')}</span><select aria-label={t('format')} value={format} disabled={busy} className="handoff-select" onChange={event => {
        setFormat(event.currentTarget.value as ShareFormat); setResult(null); setConsent(false); setNotice(null); setCopyError(false); task.clearError()
      }}><option value="zip">{t('zip')}</option><option value="link">{t('link')}</option></select></label>
      <div className="handoff-notice"><IconShareOutlineRegular size={16} /><p>{t(format === 'zip' ? 'zipNotice' : 'linkNotice')}</p></div>
      {empty && <div className="handoff-stack"><p className="handoff-caption">{t('emptyTask')}</p><div className="handoff-actions"><Button size="sm" variant="outline" disabled={busy} onClick={() => setReload(value => value + 1)}>{t('refreshSummary')}</Button></div></div>}
      {!blocked && <div className="handoff-actions"><Button variant="primary" disabled={busy || summary === null || empty} onClick={() => share()}>{t('createShare')}</Button></div>}
      {blocked && <section className="handoff-warning handoff-stack" aria-label={t('blocked')}>
        <strong>{t('blocked')}</strong><p style={muted}>{t('blockedDescription')}</p>
        {result.findings?.length ? <ul>{result.findings.map((finding, at) => <li key={at} style={{ overflowWrap: 'anywhere' }}><strong>{finding.rule}</strong>{finding.field && <p style={identity}>{t('field')}: {finding.field}</p>}{finding.line !== undefined && <p style={identity}>{t('line')}: {finding.line}</p>}</li>)}</ul> : <p style={muted}>{t('noFindingDetails')}</p>}
        {result.secretTicket && <><label style={{ ...row, alignItems: 'flex-start' }}><input type="checkbox" checked={consent} disabled={busy} onChange={event => setConsent(event.currentTarget.checked)} /><span>{t('allowSecrets')}</span></label>
          <div className="handoff-actions"><Button variant="primary" disabled={busy || !consent} onClick={() => { if (consent) share(result.secretTicket) }}>{t('overrideShare')}</Button></div></>}
        <div className="handoff-actions"><Button size="sm" variant="outline" disabled={busy} onClick={() => share()}>{t('retry')}</Button></div>
      </section>}
      <ErrorBox error={task.error} t={t} />
      {!summary && !busy && <div className="handoff-actions"><Button size="sm" variant="outline" onClick={() => setReload(value => value + 1)}>{t('retry')}</Button></div>}
      <Status phase={task.phase} notice={notice ? t(notice) : undefined} t={t} />
    </section>
    <aside className="handoff-card handoff-context handoff-stack" aria-label={t('taskOverview')}>
      <div><h2>{t('taskOverview')}</h2>{summary ? <><p className="handoff-task-title">{summary.title}</p><p style={identity}>{t('session')}: {sessionId}</p><p className="handoff-caption">{t('messages')}: {summary.messageCount}</p></> : <p className="handoff-caption">{t('loading')}</p>}</div>
      {summary && !result && <Warnings warnings={summary.warnings} t={t} />}
      {result && !blocked && <section className="handoff-result" aria-label={t('resultHeading')}>
        <hr className="handoff-divider" />
        <Warnings warnings={result.warnings} t={t} />
        {result.shareUrl && <><label className="handoff-field"><span>{t('resultLink')}</span><Input className="handoff-input" aria-label={t('resultLink')} readOnly value={result.shareUrl} onFocus={event => event.currentTarget.select()} /></label>
          {result.expiresAt && <p className="handoff-caption">{t('expires')}: {result.expiresAt}</p>}
          <div className="handoff-actions"><Button variant="outline" size="sm" disabled={busy} onClick={() => void task.run('copying', async signal => {
            const accepted = await copy(result.shareUrl!); signal.throwIfAborted(); setCopyError(!accepted); setNotice(accepted ? 'copied' : null)
          })}>{t('copyLink')}</Button></div></>}
        {result.downloadId && <div className="handoff-actions"><Button variant="outline" size="sm" disabled={busy} onClick={() => void task.run('downloading', async signal => {
          const file = await call<DownloadResult>({ action: 'download', downloadId: result.downloadId! }, signal)
          signal.throwIfAborted(); save(file); setNotice('downloadStarted')
        })}>{t('download')}</Button></div>}
      </section>}
      {copyError && <p role="alert" className="handoff-alert">{t('copyFailed')}</p>}
    </aside>
  </div>
}

/** Header shortcut shares the same safe form as the hub, inside DSH's native overlay. */
export function ShareDialog({ onClose, ...props }: ShareDialogProps) {
  return <Modal open onClose={onClose} closeLabel={props.t('close')} title={props.t('share')} description={props.t('shareDescription')}
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
  const sourceReady = sourceKind === 'upload' ? file !== null : link.trim() !== ''
  const loadPreview = () => void task.run('previewing', async signal => {
    setPreview(null); setMode(''); setResult(null); setExpiredTicket(false)
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
        <div><h2>{t('previewTitle')}</h2><p className="handoff-task-title">{preview.title}</p></div>
        <dl className="handoff-facts">
          <div><dt>{t('sourceAgent')}</dt><dd>{({ dsh: 'DSH', codex: 'Codex', claude: 'Claude Code' } as Record<string, string>)[preview.sourceAgent] ?? preview.sourceAgent}</dd></div>
          <div><dt>{t('sourceIdentity')}</dt><dd>{preview.sourceSessionId}</dd></div>
          <div><dt>{t('messages')}</dt><dd>{preview.messageCount}</dd></div>
          <div><dt>{t('selectedWorkspace')}</dt><dd>{preview.workspace.name}<span className="handoff-path" title={preview.workspace.directory}>{preview.workspace.directory}</span></dd></div>
          <div><dt>{t('checksum')}</dt><dd>{['ok', 'verified'].includes(preview.checksumStatus) ? t('verified') : preview.checksumStatus === 'not_available_v1' ? t('legacyChecksum') : preview.checksumStatus}</dd></div>
        </dl>
        <Warnings warnings={preview.warnings} t={t} />
        {preview.existingSessionId && <p className="handoff-caption">{t('duplicatePreview')}</p>}
        <label className="handoff-field"><span>{t('importMode')}</span><select aria-label={t('importMode')} className="handoff-select" disabled={busy} value={mode} onChange={event => setMode(event.currentTarget.value as ImportMode)}><option value="">{t('chooseMode')}</option>{preview.modes.map(item => <option value={item} key={item}>{t(item === 'native' ? 'native' : 'conversation')}</option>)}</select></label>
        <p className="handoff-caption">{t(mode === 'native' ? 'nativeDescription' : 'conversationDescription')}</p>
        {expired && <p role="alert" className="handoff-alert">{t('expired')}</p>}
        <div className="handoff-actions"><Button variant="primary" disabled={busy || !mode || expired || !!recoverySessionId || preview.workspace.id !== workspaceId} onClick={confirm}>{t('confirmation')}</Button></div>
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
