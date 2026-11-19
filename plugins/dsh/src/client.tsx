/** Native DSH browser plugin: selected-session Share, Import panel, sidebar HOC and root overlay. */
import type { ISessions } from '@deepseek-ai/dsh-api-session-controller/client'
import type { Context } from '@deepseek-ai/cordis'
import { createSnapshotStore, type ObservableSnapshot } from '@deepseek-ai/dsh-client-store'
import { Button, IconShareOutlineRegular } from '@deepseek-ai/dsh-client-ui-primitives'
import type { MainPanelId } from '@deepseek-ai/dsh-client-ui-layout/client'
import type { SidebarPanelIconOwnerProps } from '@deepseek-ai/dsh-client-ui-sidebar/client'
import type { PropsHooks, PropsLocale, PropsRenderFactories, PropsRuntime, HocShare, SlotMap } from '@deepseek-ai/dsh-client-ui-slots'
import type { SessionId } from '@deepseek-ai/dsh-session/types'
import type { WorkspaceId } from '@deepseek-ai/dsh-workspace/types'
import type {} from '@deepseek-ai/dsh-api-session-controller/client'
import type {} from '@deepseek-ai/dsh-client-ui-conversation/client'
import type {} from '@deepseek-ai/dsh-client-ui-session/client'
import type {} from '@deepseek-ai/dsh-client-ui-workspace/client'
import type {} from '@deepseek-ai/dsh-client-ui-renderer/client'
import type {} from '@deepseek-ai/dsh-client-locale/client'
import { createApiCall, HandoffApiError, type ApiCall } from './client/api.ts'
import { ImportAction, HandoffHub, ShareDialog, type HubTab } from './client/ui.tsx'
import { NS, en, zh, type CopyKey } from './client/locales.ts'

declare module '@deepseek-ai/dsh-client-ui-slots' {
  interface LocaleNamespaceMap { agentHandoff: CopyKey }
}
interface ShareTarget { sessionId: string }
type HeaderProps = PropsRuntime<'conversation.session.header.utilities'> & PropsLocale<typeof NS> & { openShare: (id: string) => void }
function HeaderAction({ sessionId, openShare, t }: HeaderProps) {
  return <Button size="sm" onClick={() => openShare(sessionId)}>{t('share')}</Button>
}
type OverlayProps = PropsRuntime<'shell.overlay'> & PropsLocale<typeof NS> & PropsHooks<{ share: ObservableSnapshot<ShareTarget | null> }>
  & { call: ApiCall; close: () => void }
function ShareOverlay({ useShare, call, close, t }: OverlayProps) {
  const target = useShare(value => value)
  return target ? <ShareDialog key={target.sessionId} sessionId={target.sessionId} call={call} onClose={close} t={t} /> : null
}
type PanelProps = PropsRuntime<'main'> & PropsLocale<typeof NS> & PropsRenderFactories & PropsHooks<{ hubTab: ObservableSnapshot<HubTab> }>
  & { call: ApiCall; openSession: (id: string) => Promise<void>; hasWorkspacePicker: () => boolean; setHubTab: (tab: HubTab) => void; refreshSessions: () => Promise<void> }
function NativeHandoffPanel({ call, openSession, renderFactorySlot, t, hasWorkspacePicker, useSessions, useHubTab, setHubTab, refreshSessions }: PanelProps) {
  const catalog = useSessions(value => value)
  const rows = catalog.ids.map(id => catalog.byId[id]).filter(row => row !== undefined)
  const current = rows.find(row => (row.retainedBy.mainView ?? 0) > 0)?.id
  const tab = useHubTab(value => value)
  return <HandoffHub call={call} openSession={openSession} t={t} tab={tab} onTabChange={setHubTab}
    sessions={rows.map(row => ({ id: row.id, title: row.title ?? row.displayTitle, directory: row.cwd }))}
    currentSessionId={current} catalogLoading={catalog.phase === 'pending'} refreshSessions={refreshSessions}
    renderWorkspacePicker={hasWorkspacePicker() ? input => renderFactorySlot('workspace.picker', {
      ...input, selectedId: input.selectedId as WorkspaceId | undefined, onPick: id => input.onPick(id),
    }) : undefined} />
}
type SidebarActionProps = PropsRuntime<'sidebar.session.action'> & HocShare<SlotMap['sidebar.session.action']>
  & PropsLocale<typeof NS> & { onOpen: () => void }
function SidebarImportAction({ children, wide, t, onOpen }: SidebarActionProps) {
  return <ImportAction wide={wide} t={t} onOpen={onOpen}>{children}</ImportAction>
}
function PanelIcon({ size }: SidebarPanelIconOwnerProps) { return <IconShareOutlineRegular size={size} /> }

export const inject = ['slots', 'locale', 'sessions', 'uiWorkspace', 'layout']
/** All effects belong to the plugin fiber; unload removes entries and the dialogs they own. */
export function apply(ctx: Context & { sessions: ISessions }): void {
  ctx.effect(() => ctx.locale.register(NS, { zh, en }), 'agent-handoff: dictionaries')
  const t = ctx.locale.bind(NS)
  const call = createApiCall()
  const share = createSnapshotStore<ShareTarget | null>(null)
  const hubTab = createSnapshotStore<HubTab>('export')
  const panelId = 'agent-handoff' as MainPanelId
  const openShare = (sessionId: string) => { share.set({ sessionId }) }
  const openSession = async (id: string) => {
    await ctx.sessions.refresh()
    const list = ctx.sessions.list.getSnapshot()
    if (list.phase !== 'ready' || !list.byId[id as SessionId]) throw new HandoffApiError({ code: 'ui/session-not-listed', message: '' })
    ctx.uiWorkspace.openSession(id as SessionId)
  }
  ctx.slots.inject('conversation.session.header.utilities', () => ctx.slots.register({
    name: 'conversation.session.header.utilities', id: 'agent-handoff', order: 10, locale: NS,
    inject: () => ({ openShare }),
  }, HeaderAction))
  ctx.slots.inject('shell.overlay', () => ctx.slots.register({
    name: 'shell.overlay', id: 'agent-handoff-share', locale: NS,
    inject: () => ({ call, hooks: { share }, close: () => { share.set(null) } }),
  }, ShareOverlay))
  ctx.slots.inject('main', () => ctx.slots.register({
    name: 'main', key: panelId, locale: NS, inject: () => ({ call, openSession, hooks: { hubTab }, setHubTab: (value: HubTab) => { hubTab.set(value) }, refreshSessions: () => ctx.sessions.refresh(), hasWorkspacePicker: () => ctx.slots.snapshot('factory:workspace.picker').length > 0 }),
  }, NativeHandoffPanel))
  ctx.slots.inject('sidebar.panellist', () => ctx.slots.register({
    name: 'sidebar.panellist', id: panelId, order: 20, label: () => t('panel'),
  }, PanelIcon))
  ctx.slots.inject('sidebar.session.action', () => ctx.slots.register({
    name: 'sidebar.session.action', id: 'agent-handoff-import', locale: NS,
    inject: () => ({ onOpen: () => { hubTab.set('import'); ctx.layout.selectPanel(panelId) } }),
  }, SidebarImportAction))
}
