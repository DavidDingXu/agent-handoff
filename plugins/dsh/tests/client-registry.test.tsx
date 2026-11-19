// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { Context } from '@deepseek-ai/cordis'
import { SlotRegistry } from '@deepseek-ai/dsh-client-ui-renderer/src/client/registry.ts'
import { createSnapshotStore } from '@deepseek-ai/dsh-client-store'
import { apply, inject } from '../src/client.tsx'
import { en, zh, type Translate } from '../src/client/locales.ts'
const makeTranslator = (language: 'en' | 'zh'): Translate => key => (language === 'zh' ? zh : en)[key]

const roots: Context[] = []
afterEach(async () => { for (const ctx of roots.splice(0)) await ctx.fiber.dispose() })
it('contributes a real header action, import panel and preserved New session HOC with a shared root overlay, then withdraws on unload', async () => {
  const ctx = new Context(); roots.push(ctx)
  await ctx.plugin(SlotRegistry).await()
  ctx.slots.register({ name: 'root', children: {
    'conversation.session.header.utilities': { kind: 'list', scope: 'session' },
    'shell.overlay': { kind: 'list', scope: 'root' },
    main: { kind: 'keyed', scope: 'root' },
    'sidebar.panellist': { kind: 'list', scope: 'root' },
    'sidebar.session.action': { kind: 'hoc', scope: 'root' },
  } } as never, () => null)
  ctx.provide('locale', { register: () => () => {}, bind: () => makeTranslator('en') } as never)
  ctx.provide('sessions', { refresh: vi.fn(async () => {}), list: createSnapshotStore({ phase: 'ready', byId: { imported: { id: 'imported' } } }) } as never)
  const open = vi.fn()
  ctx.provide('uiWorkspace', { openSession: open } as never)
  ctx.provide('layout', { selectPanel: vi.fn() } as never)
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
