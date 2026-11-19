// @vitest-environment jsdom
import React from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { ImportPanel, ShareDialog, ImportAction } from '../src/client/ui.tsx'
import { en, zh, type Translate } from '../src/client/locales.ts'
const makeTranslator = (language: 'en' | 'zh'): Translate => key => (language === 'zh' ? zh : en)[key]
import { HandoffApiError, type ApiCall } from '../src/client/api.ts'
import type { ApiRequest, ImportPreview, ShareResult } from '../src/types.ts'

const t = makeTranslator('en')
const workspace = { id: 'workspace-a', name: 'Chosen project', directory: '/projects/chosen' }
const second = { id: 'workspace-b', name: 'Other project', directory: '/projects/other' }
const summary = { sessionId: 'session-a', title: 'Original session', messageCount: 3, warnings: [] }
const preview: ImportPreview = {
  ticketId: 'ticket-bound-to-a', sourceAgent: 'codex', sourceSessionId: 'source-42', title: 'Received task', messageCount: 7,
  fingerprint: 'sha256:abc', checksumStatus: 'ok', workspace, modes: ['native', 'conversation'],
  warnings: ['An imported tool record is evidence only.'], expiresAt: '2099-11-19T12:00:00Z',
}
function api(handler: (request: ApiRequest, signal?: AbortSignal) => unknown | Promise<unknown>) {
  const calls: ApiRequest[] = []
  const call: ApiCall = async <T,>(request: ApiRequest, signal?: AbortSignal) => {
    calls.push(request)
    return await handler(request, signal) as T
  }
  return { call, calls }
}
function importApi(handler?: (request: ApiRequest) => unknown | Promise<unknown>) {
  return api(request => request.action === 'workspaces' ? [workspace, second] : handler?.(request))
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
async function fillImport() {
  await screen.findByRole('option', { name: /Chosen project/ })
  fireEvent.change(screen.getByLabelText('Workspace'), { target: { value: workspace.id } })
  fireEvent.change(screen.getByLabelText('Source'), { target: { value: 'link' } })
  fireEvent.change(screen.getByLabelText('Share link'), { target: { value: 'https://share.example/item#local-key' } })
}
afterEach(() => { cleanup(); vi.restoreAllMocks() })
beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
})

describe('native share controls', () => {
  it.each(['en', 'zh'] as const)('does not share an empty task and offers a summary refresh (%s)', async language => {
    const service = api(request => request.action === 'summary' ? { ...summary, messageCount: 0 } : undefined)
    const copy = makeTranslator(language)
    render(<ShareDialog sessionId="session-a" call={service.call} onClose={() => {}} t={copy} />)
    await screen.findByText('Original session')
    const button = screen.getByRole('button', { name: copy('createShare') })
    expect(button).toHaveProperty('disabled', true)
    fireEvent.click(button)
    expect(service.calls.some(request => request.action === 'share')).toBe(false)
    expect(screen.getByText(language === 'zh' ? '此任务还没有消息，请选择其他任务。' : 'This task has no messages yet. Choose another task.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: language === 'zh' ? '刷新任务信息' : 'Refresh task details' }))
    await waitFor(() => expect(service.calls.filter(request => request.action === 'summary')).toHaveLength(2))
    expect(service.calls.some(request => request.action === 'share')).toBe(false)
  })

  it('captures the clicked session, blocks duplicate submits, and preserves retry after an error', async () => {
    const operation = deferred<ShareResult>()
    let shares = 0
    const service = api(request => {
      if (request.action === 'summary') return summary
      if (request.action === 'share') {
        shares++
        if (shares === 1) return operation.promise
        return { status: 'ok', title: summary.title, messageCount: 3, downloadId: 'zip-one', warnings: [] }
      }
    })
    render(<ShareDialog sessionId="session-a" call={service.call} onClose={() => {}} t={t} />)
    await screen.findByText('Original session')
    const button = screen.getByRole('button', { name: 'Create share' })
    act(() => { fireEvent.click(button); fireEvent.click(button) })
    expect(service.calls.filter(r => r.action === 'share')).toEqual([{ action: 'share', sessionId: 'session-a', format: 'zip' }])
    expect(screen.getByRole('status').textContent).toContain('Preparing')
    await act(async () => { operation.resolve(Promise.reject(new HandoffApiError({ code: 'share/failed', message: 'Transfer unavailable' })) as never) })
    await screen.findByRole('alert')
    expect(screen.getByLabelText('Format')).toHaveProperty('value', 'zip')
    fireEvent.click(screen.getByRole('button', { name: 'Create share' }))
    await screen.findByRole('button', { name: 'Download ZIP' })
  })

  it('requires a separate informed click before sharing the snapshot with detected secrets', async () => {
    const service = api(request => {
      if (request.action === 'summary') return summary
      if (request.action === 'share') return request.secretTicket
        ? { status: 'ok', title: summary.title, messageCount: 3, downloadId: 'override-zip', warnings: [] }
        : { status: 'blocked', title: summary.title, messageCount: 3, warnings: [], findings: [{ rule: 'api-key', field: '$.native.events[0]' }], secretTicket: 'snapshot-ticket' }
    })
    render(<ShareDialog sessionId="session-a" call={service.call} onClose={() => {}} t={t} />)
    await screen.findByText('Original session')
    fireEvent.click(screen.getByRole('button', { name: 'Create share' }))
    await screen.findByText('api-key')
    const override = screen.getByRole('button', { name: 'Share with detected secrets' }) as HTMLButtonElement
    expect(override.disabled).toBe(true)
    fireEvent.click(override)
    expect(service.calls.filter(r => r.action === 'share')).toHaveLength(1)
    fireEvent.click(screen.getByRole('checkbox', { name: /I understand/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Share with detected secrets' }))
    await screen.findByRole('button', { name: 'Download ZIP' })
    expect(service.calls.filter(r => r.action === 'share').at(-1)).toEqual({ action: 'share', sessionId: 'session-a', format: 'zip', secretTicket: 'snapshot-ticket' })
  })

  it('discards a blocked override when format changes and renders link expiry, copy and file fallback', async () => {
    const copy = vi.fn(async () => true)
    const save = vi.fn()
    const service = api(request => {
      if (request.action === 'summary') return summary
      if (request.action === 'share') return request.format === 'zip'
        ? { status: 'blocked', title: summary.title, messageCount: 3, warnings: [], secretTicket: 'old-ticket', findings: [] }
        : { status: 'fallback_zip', title: summary.title, messageCount: 3, warnings: ['A local ZIP is available.'], shareUrl: 'https://share.example/item#key', expiresAt: '2026-11-20T12:00:00Z', downloadId: 'download-one' }
      if (request.action === 'download') return { fileName: 'handoff.zip', dataBase64: 'UEs=' }
    })
    render(<ShareDialog sessionId="session-a" call={service.call} onClose={() => {}} t={t} copy={copy} save={save} />)
    await screen.findByText('Original session')
    fireEvent.click(screen.getByRole('button', { name: 'Create share' }))
    await screen.findByRole('checkbox', { name: /I understand/ })
    fireEvent.change(screen.getByLabelText('Format'), { target: { value: 'link' } })
    expect(screen.queryByRole('checkbox', { name: /I understand/ })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Create share' }))
    await screen.findByDisplayValue('https://share.example/item#key')
    expect(screen.getByText(/2026-11-20T12:00:00Z/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Copy link' }))
    await waitFor(() => expect(copy).toHaveBeenCalledWith('https://share.example/item#key'))
    fireEvent.click(screen.getByRole('button', { name: 'Download ZIP' }))
    await waitFor(() => expect(save).toHaveBeenCalledWith({ fileName: 'handoff.zip', dataBase64: 'UEs=' }))
    expect(service.calls.filter(r => r.action === 'share').at(-1)).toEqual({ action: 'share', sessionId: 'session-a', format: 'link' })
  })

  it('shows the exported snapshot title and message count when the live task changed after loading', async () => {
    const service = api(r => r.action === 'summary' ? summary : { status: 'ok', title: 'Exported snapshot', messageCount: 8, warnings: [], downloadId: 'snapshot-file' })
    render(<ShareDialog sessionId="session-a" call={service.call} onClose={() => {}} t={t} />)
    await screen.findByText('Original session')
    fireEvent.click(screen.getByRole('button', { name: 'Create share' }))
    await screen.findByRole('button', { name: 'Download ZIP' })
    expect(screen.getByText('Exported snapshot')).toBeTruthy()
    expect(screen.getByText('Messages: 8')).toBeTruthy()
  })

  it('reports clipboard rejection without claiming the link was copied', async () => {
    const service = api(r => r.action === 'summary' ? summary : { status: 'ok', title: summary.title, messageCount: 3, warnings: [], shareUrl: 'https://share.example/a#key' })
    render(<ShareDialog sessionId="session-a" call={service.call} onClose={() => {}} t={t} copy={async () => false} />)
    await screen.findByText('Original session')
    fireEvent.click(screen.getByRole('button', { name: 'Create share' }))
    await screen.findByRole('button', { name: 'Copy link' })
    fireEvent.click(screen.getByRole('button', { name: 'Copy link' }))
    expect((await screen.findByRole('alert')).textContent).toMatch(/select and copy/i)
  })
})

it('aborts pending native share work when its dialog is removed', async () => {
  let signal: AbortSignal | undefined
  const operation = deferred<ShareResult>()
  const service = api((r, active) => {
    if (r.action === 'summary') return summary
    signal = active
    return operation.promise
  })
  const view = render(<ShareDialog sessionId="session-a" call={service.call} onClose={() => {}} t={t} />)
  await screen.findByText('Original session')
  fireEvent.click(screen.getByRole('button', { name: 'Create share' }))
  view.unmount()
  expect(signal?.aborted).toBe(true)
  await act(async () => { operation.resolve({ status: 'ok', title: summary.title, messageCount: 3, warnings: [] }) })
})

describe('native import controls', () => {
  it('previews without importing, requires fidelity choice, then creates one new task from the exact ticket', async () => {
    const pending = deferred<unknown>()
    const opened = vi.fn(async () => {})
    const service = importApi(r => r.action === 'preview' ? preview : r.action === 'import' ? pending.promise : undefined)
    render(<ImportPanel call={service.call} t={t} openSession={opened} />)
    await fillImport()
    fireEvent.click(screen.getByRole('button', { name: 'Preview handoff' }))
    await screen.findByText('Received task')
    expect(service.calls.filter(r => r.action === 'import')).toEqual([])
    expect(screen.getByText('source-42')).toBeTruthy()
    expect(screen.getByText('Verified')).toBeTruthy()
    expect(screen.getByText('An imported tool record is evidence only.')).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Import as a new task' }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(screen.getByLabelText('Import mode'), { target: { value: 'conversation' } })
    const confirm = screen.getByRole('button', { name: 'Import as a new task' })
    act(() => { fireEvent.click(confirm); fireEvent.click(confirm) })
    expect(service.calls.filter(r => r.action === 'import')).toEqual([{ action: 'import', ticketId: 'ticket-bound-to-a', workspaceId: 'workspace-a', mode: 'conversation' }])
    await act(async () => { pending.resolve({ status: 'imported', sessionId: 'fresh-host-id', title: 'Received task', workspaceId: workspace.id, mode: 'conversation', warnings: [] }) })
    fireEvent.click(await screen.findByRole('button', { name: 'Open task' }))
    await waitFor(() => expect(opened).toHaveBeenCalledWith('fresh-host-id'))
  })

  it('invalidates the preview when source or target changes, including to the same visible title', async () => {
    const service = importApi(r => r.action === 'preview' ? preview : undefined)
    render(<ImportPanel call={service.call} t={t} openSession={async () => {}} />)
    await fillImport()
    fireEvent.click(screen.getByRole('button', { name: 'Preview handoff' }))
    await screen.findByText('Received task')
    fireEvent.change(screen.getByLabelText('Share link'), { target: { value: 'https://share.example/changed#other-key' } })
    expect(screen.queryByText('Received task')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Import as a new task' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Preview handoff' }))
    await screen.findByText('Received task')
    fireEvent.change(screen.getByLabelText('Workspace'), { target: { value: second.id } })
    expect(screen.queryByText('Received task')).toBeNull()
  })

  it('labels semantic-only imports honestly and preserves a recoverable created task after failure', async () => {
    const opened = vi.fn(async () => {})
    const service = importApi(r => {
      if (r.action === 'preview') return { ...preview, modes: ['conversation'] }
      if (r.action === 'import') throw new HandoffApiError({ code: 'import/partial', message: 'The new task needs attention', recoverySessionId: 'recover-me' })
    })
    render(<ImportPanel call={service.call} t={t} openSession={opened} />)
    await fillImport()
    fireEvent.click(screen.getByRole('button', { name: 'Preview handoff' }))
    await screen.findByText('Received task')
    expect(screen.queryByRole('option', { name: /Native/ })).toBeNull()
    expect(screen.getByText(/Visible conversation and tool evidence/)).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Import mode'), { target: { value: 'conversation' } })
    fireEvent.click(screen.getByRole('button', { name: 'Import as a new task' }))
    expect((await screen.findByRole('alert')).textContent).toContain('needs attention')
    fireEvent.click(screen.getByRole('button', { name: 'Open created task' }))
    await waitFor(() => expect(opened).toHaveBeenCalledWith('recover-me'))
    expect((screen.getByRole('button', { name: 'Import as a new task' }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByLabelText('Share link')).toHaveProperty('value', 'https://share.example/item#local-key')
  })

  it('does not create another task for a duplicate and shows the already imported identity', async () => {
    const service = importApi(r => r.action === 'preview' ? { ...preview, existingSessionId: 'existing-import' } : { status: 'duplicate', sessionId: 'existing-import', title: 'Received task', workspaceId: workspace.id, mode: 'native', warnings: [] })
    render(<ImportPanel call={service.call} t={t} openSession={async () => {}} />)
    await fillImport()
    fireEvent.click(screen.getByRole('button', { name: 'Preview handoff' }))
    await screen.findByText(/already imported/i)
    fireEvent.change(screen.getByLabelText('Import mode'), { target: { value: 'native' } })
    fireEvent.click(screen.getByRole('button', { name: 'Import as a new task' }))
    await screen.findByText('existing-import')
    expect(screen.getByRole('status').textContent).toMatch(/already imported/i)
  })

  it('refuses an expired confirmation and asks for another preview', async () => {
    const service = importApi(r => r.action === 'preview' ? preview : undefined)
    render(<ImportPanel call={service.call} t={t} openSession={async () => {}} />)
    await fillImport()
    fireEvent.click(screen.getByRole('button', { name: 'Preview handoff' }))
    await screen.findByText('Received task')
    fireEvent.change(screen.getByLabelText('Import mode'), { target: { value: 'native' } })
    vi.spyOn(Date, 'now').mockReturnValue(Date.parse('2100-01-01T00:00:00Z'))
    fireEvent.click(screen.getByRole('button', { name: 'Import as a new task' }))
    expect(service.calls.filter(r => r.action === 'import')).toEqual([])
    expect((await screen.findByRole('alert')).textContent).toContain('expired')
  })

  it('reads only the explicitly chosen file for preview and never imports on file selection', async () => {
    const service = importApi(r => r.action === 'preview' ? preview : undefined)
    render(<ImportPanel call={service.call} t={t} openSession={async () => {}} />)
    await screen.findByRole('option', { name: /Chosen project/ })
    fireEvent.change(screen.getByLabelText('Workspace'), { target: { value: workspace.id } })
    fireEvent.change(screen.getByLabelText('Choose ZIP file', { selector: 'input' }), { target: { files: [new File(['PK'], 'chosen.zip', { type: 'application/zip' })] } })
    expect(service.calls.filter(r => r.action === 'preview' || r.action === 'import')).toEqual([])
    fireEvent.click(screen.getByRole('button', { name: 'Preview handoff' }))
    await screen.findByText('Received task')
    expect(service.calls.find(r => r.action === 'preview')).toEqual({ action: 'preview', source: { kind: 'upload', fileName: 'chosen.zip', dataBase64: 'UEs=' }, workspaceId: 'workspace-a' })
  })

  it('translates fidelity and permission warnings in Chinese while preserving safe source-specific notices', async () => {
    const service = importApi(r => r.action === 'preview' ? { ...preview, modes: ['conversation'], warnings: [
      'Native history contains unsupported authority or plugin state. Choose conversation import explicitly.',
      'Conversation import preserves visible messages and tool evidence; source permissions, runtime state and media files are not restored.',
      'A safe notice supplied by the source.',
    ] } : undefined)
    render(<ImportPanel call={service.call} t={makeTranslator('zh')} openSession={async () => {}} />)
    await screen.findByRole('option', { name: /Chosen project/ })
    fireEvent.change(screen.getByLabelText('工作区'), { target: { value: workspace.id } })
    fireEvent.change(screen.getByLabelText('来源'), { target: { value: 'link' } })
    fireEvent.change(screen.getByLabelText('分享链接'), { target: { value: 'https://share.example/item#key' } })
    fireEvent.click(screen.getByRole('button', { name: '预览交接内容' }))
    await screen.findByText('Received task')
    expect(screen.getByText('来源包含权限、系统指令或插件状态，不能原生恢复，请选择对话导入。')).toBeTruthy()
    expect(screen.getByText('对话导入保留可见消息与工具记录，不恢复来源权限、运行状态和媒体文件。')).toBeTruthy()
    expect(screen.getByText('A safe notice supplied by the source.')).toBeTruthy()
    expect(screen.queryByText('Native history contains unsupported authority or plugin state. Choose conversation import explicitly.')).toBeNull()
  })

  it('keeps the New session control intact in the sidebar HOC and renders Chinese copy', () => {
    render(<ImportAction t={makeTranslator('zh')} onOpen={() => {}} wide><button>New session</button></ImportAction>)
    expect(screen.getByRole('button', { name: 'New session' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '导入任务' })).toBeTruthy()
  })
})
