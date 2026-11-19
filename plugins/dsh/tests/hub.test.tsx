// @vitest-environment jsdom
import React from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { HandoffHub } from '../src/client/ui.tsx'
import { en, zh, type Translate } from '../src/client/locales.ts'
import type { ApiCall } from '../src/client/api.ts'
import type { ApiRequest } from '../src/types.ts'
const t: Translate = key => en[key]
const sessions = [
  { id: 'older-task', title: 'Earlier task', directory: '/tmp/earlier' },
  { id: 'current-task', title: 'Current task', directory: '/tmp/current' },
]
function backend() {
  const calls: ApiRequest[] = []
  const call: ApiCall = async <T,>(request: ApiRequest) => {
    calls.push(request)
    const data = request.action === 'summary' ? { sessionId: request.sessionId, title: request.sessionId === 'current-task' ? 'Current task' : 'Earlier task', messageCount: 4, warnings: [] }
      : request.action === 'workspaces' ? [{ id: 'local', name: 'Local workspace', directory: '/tmp/local' }]
      : request.action === 'preview' ? { ticketId: 'ticket', sourceAgent: 'codex', sourceSessionId: 'source-task', title: 'Received task', messageCount: 3, fingerprint: 'hash', checksumStatus: 'ok', workspace: { id: 'local', name: 'Local workspace', directory: '/tmp/local' }, modes: ['conversation'], warnings: [], expiresAt: '2099-01-01T00:00:00Z' }
      : { status: 'ok', title: 'Exported task', messageCount: 4, downloadId: 'download', warnings: [] }
    return data as T
  }
  return { call, calls }
}
beforeEach(() => vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} }))
afterEach(() => { cleanup(); vi.unstubAllGlobals() })

it('shows separate Export and Import regions and exports the current metadata-selected task inside the hub', async () => {
  const api = backend()
  render(<HandoffHub call={api.call} sessions={sessions} currentSessionId="current-task" openSession={async () => {}} t={t} />)
  expect(screen.getByRole('tab', { name: 'Export task' }).getAttribute('aria-selected')).toBe('true')
  expect(screen.getByRole('tab', { name: 'Import task' })).toBeTruthy()
  expect(screen.getByRole('tabpanel', { name: 'Export task' })).toBeTruthy()
  await waitFor(() => expect(api.calls.filter(r => r.action === 'summary')).toEqual([{ action: 'summary', sessionId: 'current-task' }]))
  expect(screen.getByRole('combobox', { name: 'Task to export' })).toHaveProperty('value', 'Current task — /tmp/current')
  fireEvent.click(await screen.findByRole('button', { name: 'Create share' }))
  await screen.findByRole('button', { name: 'Download ZIP' })
  expect(api.calls.find(r => r.action === 'share')).toEqual({ action: 'share', sessionId: 'current-task', format: 'zip' })
})

it('only reads the selected task summary and resets export state after choosing another task', async () => {
  const api = backend()
  render(<HandoffHub call={api.call} sessions={sessions} currentSessionId="current-task" openSession={async () => {}} t={t} />)
  await screen.findByText('Messages: 4')
  fireEvent.click(screen.getByRole('combobox', { name: 'Task to export' }))
  fireEvent.click(screen.getByRole('option', { name: 'Earlier task /tmp/earlier' }))
  await waitFor(() => expect(api.calls.filter(r => r.action === 'summary')).toHaveLength(2))
  fireEvent.click(screen.getByRole('button', { name: 'Create share' }))
  await screen.findByRole('button', { name: 'Download ZIP' })
  expect(api.calls.find(r => r.action === 'share')).toMatchObject({ sessionId: 'older-task' })
})

it('does not read an unrelated task when no current task is retained and waits for explicit selection', async () => {
  const api = backend()
  render(<HandoffHub call={api.call} sessions={sessions} currentSessionId="not-in-catalog" openSession={async () => {}} t={t} />)
  expect(screen.getByRole('combobox', { name: 'Task to export' })).toHaveProperty('value', '')
  expect(screen.queryByRole('button', { name: 'Create share' })).toBeNull()
  await new Promise(resolve => setTimeout(resolve, 0))
  expect(api.calls).toEqual([])
  fireEvent.click(screen.getByRole('combobox', { name: 'Task to export' }))
  fireEvent.click(screen.getByRole('option', { name: 'Earlier task /tmp/earlier' }))
  await waitFor(() => expect(api.calls).toEqual([{ action: 'summary', sessionId: 'older-task' }]))
})

it('supports keyboard navigation to Import and keeps its source input while switching regions', async () => {
  const api = backend()
  render(<HandoffHub call={api.call} sessions={sessions} openSession={async () => {}} t={t} />)
  const tab = screen.getByRole('tab', { name: 'Export task' }); tab.focus()
  fireEvent.keyDown(tab, { key: 'ArrowRight' })
  expect(screen.getByRole('tabpanel', { name: 'Import task' })).toBeTruthy()
  await screen.findByRole('option', { name: /Local workspace/ })
  fireEvent.change(screen.getByLabelText('Source'), { target: { value: 'link' } })
  fireEvent.change(screen.getByLabelText('Share link'), { target: { value: 'https://share.example/task#key' } })
  fireEvent.click(screen.getByRole('tab', { name: 'Export task' }))
  fireEvent.click(screen.getByRole('tab', { name: 'Import task' }))
  expect(screen.getByLabelText('Share link')).toHaveProperty('value', 'https://share.example/task#key')
})

it('keeps import preview and explicit confirmation in the Import region and discards it after a source change', async () => {
  const api = backend()
  render(<HandoffHub call={api.call} sessions={[]} initialTab="import" openSession={async () => {}} t={t} />)
  await screen.findByRole('option', { name: /Local workspace/ })
  fireEvent.change(screen.getByLabelText('Workspace'), { target: { value: 'local' } })
  fireEvent.change(screen.getByLabelText('Source'), { target: { value: 'link' } })
  fireEvent.change(screen.getByLabelText('Share link'), { target: { value: 'https://share.example/task#key' } })
  fireEvent.click(screen.getByRole('button', { name: 'Preview handoff' }))
  await screen.findByText('Received task')
  expect(api.calls.some(r => r.action === 'import')).toBe(false)
  fireEvent.change(screen.getByLabelText('Share link'), { target: { value: 'https://share.example/other#key' } })
  expect(screen.queryByRole('button', { name: 'Import as a new task' })).toBeNull()
})

it('offers a compact refresh action for an empty catalog and labels both regions in Chinese', () => {
  const api = backend(), refresh = vi.fn(async () => {})
  render(<HandoffHub call={api.call} sessions={[]} refreshSessions={refresh} openSession={async () => {}} t={key => zh[key]} />)
  expect(screen.getByRole('tab', { name: '导出任务' })).toBeTruthy()
  expect(screen.getByRole('tab', { name: '导入任务' })).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '刷新任务列表' }))
  expect(refresh).toHaveBeenCalledTimes(1)
  expect(api.calls.filter(r => r.action === 'summary')).toHaveLength(0)
})

it.each([
  ['  EARLIER  ', 'Earlier task /tmp/earlier'],
  ['/TMP/CURRENT', 'Current task /tmp/current'],
  ['控制台', '修复登录 C:\\Projects\\控制台'],
])('filters task choices by title or path without reading a task (%s)', (query, expected) => {
  const api = backend()
  render(<HandoffHub call={api.call} sessions={[...sessions, { id: 'chinese-task', title: '修复登录', directory: 'C:\\Projects\\控制台' }]} openSession={async () => {}} t={t} />)
  const picker = screen.getByRole('combobox', { name: 'Task to export' })
  fireEvent.click(picker)
  fireEvent.change(picker, { target: { value: query } })
  expect(screen.getAllByRole('option')).toHaveLength(1)
  expect(screen.getByRole('option', { name: expected })).toBeTruthy()
  expect(api.calls).toEqual([])
  fireEvent.click(screen.getByRole('button', { name: 'Clear search' }))
  expect(screen.getAllByRole('option')).toHaveLength(3)
})

it('keeps the selected task visible while searching and shares only an explicitly chosen match', async () => {
  const api = backend()
  render(<HandoffHub call={api.call} sessions={sessions} currentSessionId="current-task" openSession={async () => {}} t={t} />)
  await screen.findByText('Messages: 4')
  const picker = screen.getByRole('combobox', { name: 'Task to export' })
  fireEvent.click(picker)
  fireEvent.change(picker, { target: { value: 'earlier' } })
  expect(screen.getByText('Selected task: Current task')).toBeTruthy()
  expect(api.calls.filter(r => r.action === 'summary')).toEqual([{ action: 'summary', sessionId: 'current-task' }])
  fireEvent.click(screen.getByRole('button', { name: 'Create share' }))
  await screen.findByRole('button', { name: 'Download ZIP' })
  expect(api.calls.find(r => r.action === 'share')).toMatchObject({ sessionId: 'current-task' })
  fireEvent.click(picker)
  fireEvent.change(picker, { target: { value: 'earlier' } })
  fireEvent.click(screen.getByRole('option', { name: 'Earlier task /tmp/earlier' }))
  await waitFor(() => expect(api.calls.filter(r => r.action === 'summary')).toHaveLength(2))
  expect(screen.queryByRole('button', { name: 'Download ZIP' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Create share' }))
  await screen.findByRole('button', { name: 'Download ZIP' })
  expect(api.calls.filter(r => r.action === 'share').at(-1)).toMatchObject({ sessionId: 'older-task' })
})

it('shows no matches without selecting or loading a different task', () => {
  const api = backend()
  render(<HandoffHub call={api.call} sessions={sessions} openSession={async () => {}} t={t} />)
  const picker = screen.getByRole('combobox', { name: 'Task to export' })
  fireEvent.click(picker)
  fireEvent.change(picker, { target: { value: 'missing task' } })
  expect(screen.getByText('No matching tasks.')).toBeTruthy()
  expect(screen.queryByRole('option')).toBeNull()
  expect(screen.queryByRole('button', { name: 'Create share' })).toBeNull()
  expect(api.calls).toEqual([])
})

it('uses one editable task picker that closes after a result is chosen', async () => {
  const api = backend()
  render(<HandoffHub call={api.call} sessions={sessions} openSession={async () => {}} t={t} />)
  const picker = screen.getByRole('combobox', { name: 'Task to export' })
  expect(picker.tagName).toBe('INPUT')
  expect(picker.getAttribute('aria-expanded')).toBe('false')
  expect(screen.queryByRole('listbox')).toBeNull()
  fireEvent.click(picker)
  expect(picker.getAttribute('aria-expanded')).toBe('true')
  fireEvent.change(picker, { target: { value: 'earlier' } })
  fireEvent.click(screen.getByRole('option', { name: 'Earlier task /tmp/earlier' }))
  expect(picker.getAttribute('aria-expanded')).toBe('false')
  expect(picker).toHaveProperty('value', 'Earlier task — /tmp/earlier')
  expect(screen.queryByRole('listbox')).toBeNull()
  await waitFor(() => expect(api.calls).toEqual([{ action: 'summary', sessionId: 'older-task' }]))
})

it('supports arrow-key selection and ignores Enter while composing Chinese text', async () => {
  const api = backend()
  render(<HandoffHub call={api.call} sessions={sessions} openSession={async () => {}} t={t} />)
  const picker = screen.getByRole('combobox', { name: 'Task to export' })
  fireEvent.focus(picker)
  fireEvent.keyDown(picker, { key: 'ArrowDown' })
  const active = document.getElementById(picker.getAttribute('aria-activedescendant')!)
  expect(active?.textContent).toContain('Current task')
  fireEvent.keyDown(picker, { key: 'Enter', isComposing: true })
  expect(api.calls).toEqual([])
  expect(picker.getAttribute('aria-expanded')).toBe('true')
  fireEvent.keyDown(picker, { key: 'ArrowUp' })
  fireEvent.keyDown(picker, { key: 'Enter' })
  await waitFor(() => expect(api.calls).toEqual([{ action: 'summary', sessionId: 'older-task' }]))
  expect(picker).toHaveProperty('value', 'Earlier task — /tmp/earlier')
})

it('restores the selected label on Escape, Tab or outside click without choosing a match', async () => {
  const api = backend()
  render(<HandoffHub call={api.call} sessions={sessions} currentSessionId="current-task" openSession={async () => {}} t={t} />)
  await screen.findByText('Messages: 4')
  const picker = screen.getByRole('combobox', { name: 'Task to export' })
  for (const dismiss of ['Escape', 'Tab', 'outside']) {
    fireEvent.click(picker)
    fireEvent.change(picker, { target: { value: 'earlier' } })
    if (dismiss === 'outside') fireEvent.pointerDown(document.body)
    else fireEvent.keyDown(picker, { key: dismiss })
    expect(picker.getAttribute('aria-expanded')).toBe('false')
    expect(picker).toHaveProperty('value', 'Current task — /tmp/current')
    expect(screen.queryByRole('listbox')).toBeNull()
  }
  expect(api.calls).toEqual([{ action: 'summary', sessionId: 'current-task' }])
})
