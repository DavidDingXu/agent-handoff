// @vitest-environment jsdom
import React from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { ShareForm, ImportPanel } from '../src/client/ui.tsx'
import { en, type Translate } from '../src/client/locales.ts'
import type { ApiCall } from '../src/client/api.ts'
import type { ApiRequest } from '../src/types.ts'
const t: Translate = key => en[key]
const brief = { schema: 'agent-handoff.brief.v1', objective: { entry_index: 0, text: 'Fix the synthetic parser' }, latest_request: { entry_index: 2, text: 'Keep paths portable' }, latest_update: { entry_index: 3, text: 'Added a regression test' }, evidence: [], notes: '' }
afterEach(() => { cleanup(); vi.restoreAllMocks() })
function service(handler: (r: ApiRequest) => unknown) { const calls: ApiRequest[] = []; const call: ApiCall = async <T,>(r: ApiRequest) => { calls.push(r);return handler(r) as T };return {call,calls} }

it('keeps code off by default, previews recorded brief, and switches the exact selected task locally', async () => {
  const api = service(r => r.action === 'summary' ? {sessionId:'picked-task',title:'Synthetic task',messageCount:4,warnings:[],brief}
    : r.action === 'switch' ? { status:'ok',targetAgent:'claude',sessionId:'new-native-id',title:'Synthetic task',directory:'/synthetic/project',command:'claude --resume new-native-id',warnings:[] } : undefined)
  render(<ShareForm sessionId="picked-task" call={api.call} t={t} />)
  await screen.findByText('Fix the synthetic parser')
  expect(api.calls).toEqual([{action:'summary',sessionId:'picked-task'}])
  fireEvent.click(screen.getByRole('tab',{name:'Switch agent'}))
  expect(screen.getByRole('heading',{name:'Switch agent'})).toBeTruthy()
  fireEvent.change(screen.getByLabelText('Continue in'),{target:{value:'claude'}})
  fireEvent.click(screen.getByRole('button',{name:'Switch agent'}))
  await screen.findByDisplayValue('claude --resume new-native-id')
  expect(api.calls.at(-1)).toEqual({action:'switch',sessionId:'picked-task',target:'claude'})
  expect(api.calls.some(r=>r.action==='code-preview')).toBe(false)
})

it('includes only explicitly selected code files and clears stale share results after notes change', async () => {
  const api=service(r=>r.action==='summary'?{sessionId:'picked-task',title:'Synthetic task',messageCount:4,warnings:[],brief}
    :r.action==='code-preview'?{root:'/synthetic/project',base_commit:'a'.repeat(40),branch:'main',files:[{path:'src/fix.go',status:'modified',bytes:22,eligible:true},{path:'new.txt',status:'untracked',bytes:12,eligible:true},{path:'.env',status:'untracked',bytes:20,eligible:false,reason:'sensitive_path'}]}
    :r.action==='share'?{status:'ok',title:'Synthetic task',messageCount:4,warnings:[],shareUrl:'https://share.example/s/example#k=synthetic'}:undefined)
  render(<ShareForm sessionId="picked-task" call={api.call} t={t} />)
  await screen.findByText('Synthetic task')
  fireEvent.click(screen.getByLabelText('Include code changes'))
  await screen.findByLabelText('src/fix.go')
  expect((screen.getByLabelText('new.txt') as HTMLInputElement).checked).toBe(false)
  expect((screen.getByLabelText('.env') as HTMLInputElement).disabled).toBe(true)
  fireEvent.click(screen.getByLabelText('new.txt'))
  fireEvent.change(screen.getByLabelText('Handoff note'),{target:{value:'Please review the new file.'}})
  fireEvent.click(screen.getByRole('button',{name:'Create share'}))
  await screen.findByDisplayValue('https://share.example/s/example#k=synthetic')
  expect(api.calls.at(-1)).toMatchObject({action:'share',sessionId:'picked-task',options:{briefNote:'Please review the new file.',codeFiles:['src/fix.go','new.txt']}})
  fireEvent.change(screen.getByLabelText('Handoff note'),{target:{value:'Changed note'}})
  expect(screen.queryByDisplayValue('https://share.example/s/example#k=synthetic')).toBeNull()
})

it('shows the receiving code destination and requires explicit consent to apply it', async () => {
  const workspace={id:'local',name:'Local project',directory:'/synthetic/repo'}
  const api=service(r=>r.action==='workspaces'?[workspace]:r.action==='preview'?{ticketId:'review',sourceAgent:'claude',sourceSessionId:'source',title:'Synthetic task',messageCount:4,fingerprint:'a'.repeat(64),checksumStatus:'ok',workspace,modes:['conversation'],warnings:[],expiresAt:'2099-01-01T00:00:00Z',brief,code:{base_commit:'a'.repeat(40),files:[{path:'src/fix.go',status:'modified',bytes:22}]},codeDirectory:'/synthetic/repo-handoff-new'}:r.action==='import'?{status:'imported',sessionId:'new',title:'Synthetic task',workspaceId:'new-worktree',mode:'conversation',warnings:[]}:undefined)
  render(<ImportPanel call={api.call} t={t} openSession={async()=>{}} />)
  await screen.findByRole('option',{name:/Local project/})
  fireEvent.change(screen.getByLabelText('Workspace'),{target:{value:'local'}})
  fireEvent.change(screen.getByLabelText('Source'),{target:{value:'link'}})
  fireEvent.change(screen.getByLabelText('Share link'),{target:{value:'https://share.example/s/source#k=synthetic'}})
  fireEvent.click(screen.getByRole('button',{name:'Preview handoff'}))
  await screen.findByText('/synthetic/repo-handoff-new')
  const apply=screen.getByLabelText('Apply code in a new workspace') as HTMLInputElement
  expect(apply.checked).toBe(false)
  fireEvent.click(apply)
  fireEvent.change(screen.getByLabelText('Import mode'),{target:{value:'conversation'}})
  fireEvent.click(screen.getByRole('button',{name:'Import as a new task'}))
  await waitFor(()=>expect(api.calls.at(-1)).toMatchObject({action:'import',ticketId:'review',workspaceId:'local',mode:'conversation',applyCode:true}))
})
