// @vitest-environment jsdom
import React, { type ComponentType } from 'react'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { Context } from '@deepseek-ai/cordis'
import { SlotRegistry } from './native-registry.ts'
import { createSnapshotStore } from '@deepseek-ai/dsh-client-store'
import { apply, inject } from '../src/client.tsx'
import { en, zh, type Translate } from '../src/client/locales.ts'
const makeTranslator = (language: 'en' | 'zh'): Translate => key => (language === 'zh' ? zh : en)[key]

const roots: Context[] = []
afterEach(async () => { cleanup(); vi.unstubAllGlobals(); for (const ctx of roots.splice(0)) await ctx.fiber.dispose() })
it('contributes a real header action, import panel and preserved New session HOC with a shared root overlay, then withdraws on unload', async () => {
  const ctx = new Context(); roots.push(ctx)
  await ctx.plugin(SlotRegistry).await()
  ctx.slots.register({ name: 'root', children: {
    'conversation.session.header.utilities': { kind: 'list', scope: 'session' },
    'shell.overlay': { kind: 'list', scope: 'root' },
    main: { kind: 'keyed', scope: 'root' },
    'sidebar.panellist': { kind: 'list', scope: 'root' },
    'sidebar.session.action': { kind: 'hoc', scope: 'root' },
    'tool.call.toolview': { kind: 'keyed', scope: 'session' },
  } } as never, () => null)
  ctx.provide('locale', { register: () => () => {}, bind: () => makeTranslator('en') } as never)
  ctx.provide('sessions', { refresh: vi.fn(async () => {}), list: createSnapshotStore({ phase: 'ready', byId: { imported: { id: 'imported' } } }) } as never)
  const open = vi.fn()
  ctx.provide('uiWorkspace', { openSession: open } as never)
  const selectPanel = vi.fn()
  ctx.provide('layout', { selectPanel } as never)
  const fiber = ctx.plugin({ inject: [...inject], apply })
  await fiber.await()
  const header = ctx.slots.entries('conversation.session.header.utilities').find(entry => entry.options.id === 'agent-handoff')
  expect(header).toBeTruthy()
  const overlay = ctx.slots.entries('shell.overlay').find(entry => entry.options.id === 'agent-handoff-share')
  expect(overlay).toBeTruthy()
  const headerFace = (header!.inject as () => { openShare: (id: string) => void })()
  const overlayFace = (overlay!.inject as () => { hooks: { share: { getSnapshot: () => unknown } } })()
  headerFace.openShare('clicked-session')
  expect(overlayFace.hooks.share.getSnapshot()).toEqual({ sessionId: 'clicked-session' })
  expect(ctx.slots.entries('main').some(entry => entry.options.key === 'agent-handoff')).toBe(true)
  expect(ctx.slots.entries('sidebar.panellist').some(entry => entry.options.id === 'agent-handoff')).toBe(true)
  expect(ctx.slots.entries('sidebar.session.action').some(entry => entry.options.id === 'agent-handoff-import')).toBe(true)
  const handoffTool = ctx.slots.entries('tool.call.toolview').find(entry => entry.options.key === 'open_agent_handoff')!
  const HandoffTool = handoffTool.component as ComponentType<Record<string, unknown>>
  const toolFace = (handoffTool.inject as () => object)()
  const toolProps = { ...toolFace, sessionId: 'chat-session', phase: 'result', t: makeTranslator('en') }
  const view = render(<HandoffTool {...toolProps} block={{ args: { text: () => 'export' }, isError: false }} />)
  fireEvent.click(screen.getByRole('button', { name: 'Share task' }))
  expect(overlayFace.hooks.share.getSnapshot()).toEqual({ sessionId: 'chat-session' })
  expect(selectPanel).not.toHaveBeenCalled()
  view.rerender(<HandoffTool {...toolProps} block={{ args: { text: () => 'import' }, isError: false }} />)
  fireEvent.click(screen.getByRole('button', { name: 'Import task' }))
  expect(selectPanel).toHaveBeenCalledWith('agent-handoff')
  view.unmount()
  const main = ctx.slots.entries('main').find(entry => entry.options.key === 'agent-handoff')!
  const mainFace = (main.inject as () => { openSession: (id: string) => Promise<void> })()
  await mainFace.openSession('imported')
  expect(open).toHaveBeenCalledWith('imported')
  await expect(mainFace.openSession('not-listed')).rejects.toThrow()
  expect(open).toHaveBeenCalledTimes(1)
  await fiber.dispose()
  expect(ctx.slots.entries('conversation.session.header.utilities')).toHaveLength(0)
  expect(ctx.slots.entries('shell.overlay')).toHaveLength(0)
  expect(ctx.slots.entries('main')).toHaveLength(0)
})

it('offers a usable workspace selector when an older Host has no workspace picker factory', async () => {
  const ctx = new Context(); roots.push(ctx); await ctx.plugin(SlotRegistry).await()
  ctx.slots.register({ name: 'root', children: {
    'conversation.session.header.utilities': { kind: 'list', scope: 'session' },
    'shell.overlay': { kind: 'list', scope: 'root' }, main: { kind: 'keyed', scope: 'root' },
    'sidebar.panellist': { kind: 'list', scope: 'root' },
  } } as never, () => null)
  ctx.provide('locale', { register: () => () => {}, bind: () => makeTranslator('en') } as never)
  ctx.provide('sessions', { refresh: vi.fn(async () => {}), list: createSnapshotStore({ phase: 'ready', byId: { imported: { id: 'imported' } }, ids: [], projectionsBySession: {} }) } as never)
  ctx.provide('uiWorkspace', { openSession: vi.fn() } as never); ctx.provide('layout', { selectPanel: vi.fn() } as never)
  vi.stubGlobal('fetch', async () => Response.json({ ok: true, data: [{ id: 'local', name: 'Local project', directory: '/tmp/project' }] }))
  const fiber = ctx.plugin({ inject: [...inject], apply }); await fiber.await()
  const entry = ctx.slots.entries('main')[0]!, face = (entry.inject as () => object)()
  const Panel = entry.component as ComponentType<Record<string, unknown>>, renderFactorySlot = vi.fn(() => null)
  render(<Panel {...face} useSessions={(select: (value: object) => unknown) => select({ phase: 'ready', byId: {}, ids: [] })} useHubTab={(select: (value: string) => unknown) => select('import')} t={makeTranslator('en')} renderFactorySlot={renderFactorySlot} />)
  const selector = await screen.findByRole('combobox', { name: 'Workspace' })
  fireEvent.change(selector, { target: { value: 'local' } })
  expect(selector).toHaveProperty('value', 'local')
  expect(renderFactorySlot).not.toHaveBeenCalled()
  expect(ctx.slots.entries('sidebar.panellist')).toHaveLength(1)
  expect(ctx.slots.entries('sidebar.session.action')).toHaveLength(0)
})
