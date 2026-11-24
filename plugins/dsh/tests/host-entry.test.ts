/** The installed entry must forward native workspace creation, not only a test harness. */
import { afterEach, expect, it, vi } from 'vitest'
import { Context } from '@deepseek-ai/cordis'
import { mountAgentLoopTestDependencies } from '@deepseek-ai/dsh-agent-loop-testkit'
import SkillRegistry from '@deepseek-ai/dsh-skill'
import Storage from '@deepseek-ai/dsh-storage'
import * as StorageJson from '@deepseek-ai/dsh-storage-json'
import * as StorageDomain from '@deepseek-ai/dsh-storage-domain'
import * as JsonlPersistence from '@deepseek-ai/dsh-session-persistence-jsonl'
import QueryEngine from '@deepseek-ai/dsh-session-query-sqlite'
import { WorkspaceRegistry, WorkspaceId } from '@deepseek-ai/dsh-workspace'
import { SessionId } from '@deepseek-ai/dsh-session'
import { mkdtemp, mkdir, readFile, realpath, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { API_PATH, type ApiResponse, type ImportPreview, type ImportResult } from '../src/types.ts'
import type { ReadResult } from '../src/session.ts'
import { apply, inject } from '../src/index.ts'

const { runBridge } = vi.hoisted(() => ({ runBridge: vi.fn() }))
// Mock only the external Go process. Exercise the real installed entry,
// Host orchestration, SDK workspace registry, and durable session backends.
vi.mock('../src/bridge.ts', () => ({ createBridge: () => runBridge }))

const contexts: Context[] = [], roots: string[] = []
afterEach(async () => {
  for (const ctx of contexts.splice(0).reverse()) await ctx.fiber.dispose()
  for (const root of roots.splice(0)) await rm(root, { recursive: true, force: true })
  runBridge.mockReset()
})

it('registers the applied code directory and persists its new task through the installed Host entry', async () => {
  const root = await mkdtemp(join(tmpdir(), 'handoff-entry-')); roots.push(root)
  const ctx = new Context(); contexts.push(ctx)
  await mountAgentLoopTestDependencies(ctx)
  await ctx.plugin(SkillRegistry).await()
  await ctx.plugin(JsonlPersistence, { root: join(root, 'sessions'), compression: 'none' })
  await ctx.plugin(QueryEngine, { path: join(root, 'query.sqlite'), openAt: 'never' })
  await ctx.plugin(Storage)
  await ctx.plugin(StorageJson, { root: join(root, 'storage') })
  await ctx.plugin(StorageDomain, { backend: 'json' })
  await ctx.plugin(WorkspaceRegistry)
  const originalPath = join(root, 'original'); await mkdir(originalPath)
  const original = await ctx.workspaceRegistry.create(originalPath, 'Original workspace')

  let route: ((request: Request) => Promise<Response>) | undefined
  ctx.provide('connection', { fetch: { register: (entry: { path: string; fetch: (request: Request) => Promise<Response> }) => {
    if (entry.path !== API_PATH) throw new Error('Unexpected installed route')
    route = entry.fetch; return () => { route = undefined }
  } } } as never)
  // Native controller resolution is deliberately passive: no model or tool runs.
  ctx.provide('sessionController', { resolveAgent: async () => ({ agent: {} }), rename: async () => {} } as never)
  const sourcePath = join(root, 'source.zip'); await writeFile(sourcePath, 'synthetic verified bridge boundary')
  const payload: ReadResult = {
    schema: 'agent-handoff.bridge.read-result.v1', status: 'ok', checksum_status: 'ok', fingerprint: 'a'.repeat(64),
    manifest: { source_agent: 'codex', source_thread_id: 'source-id', title: 'Synthetic selected code', message_count: 1 },
    neutral: { schema: 'agent-handoff.neutral.v1', source_agent: 'codex', source_id: 'source-id', entries: [{ kind: 'message', role: 'user', text: 'Continue the selected fix' }] },
    native: null, source_session: 'inert source evidence', warnings: [],
    code: { manifest: { schema: 'agent-handoff.code.v1', base_commit: 'b'.repeat(40), files: [{ path: 'fix.txt', status: 'untracked', bytes: 6 }] }, patch_base64: '', files_base64: { 'fix.txt': Buffer.from('fixed\n').toString('base64') } },
  }
  runBridge.mockImplementation(async (args: string[]) => {
    if (args[1] === 'read') return structuredClone(payload)
    if (args[1] === 'code-apply') {
      const destination = args[args.indexOf('--out') + 1]!
      await mkdir(destination)
      await writeFile(join(destination, 'fix.txt'), 'fixed\n')
      return { status: 'ok', directory: destination }
    }
    throw new Error('Unexpected subprocess request')
  })
  const plugin = ctx.plugin({ inject: [...inject], apply }, { stateDir: join(root, 'state') }); await plugin.await()
  const skill = await ctx.skills.get('agent-handoff')
  expect(skill?.invocation).toEqual({ userInvocable: true, modelInvocable: true })
  expect(skill?.content).toContain('open_agent_handoff')
  expect(ctx.tools.get('open_agent_handoff')).toBeDefined()
  expect(runBridge).not.toHaveBeenCalled()
  async function request<T>(body: object): Promise<T> {
    if (!route) throw new Error('Installed Host registered no route')
    const response = await route(new Request('http://dsh.local' + API_PATH, { method: 'POST', body: JSON.stringify(body) }))
    const value = await response.json() as ApiResponse<T>
    if (!value.ok) throw new Error(JSON.stringify(value.error))
    return value.data
  }
  const preview = await request<ImportPreview>({ action: 'preview', source: { kind: 'path', path: sourcePath }, workspaceId: original.id })
  const imported = await request<ImportResult>({ action: 'import', ticketId: preview.ticketId, workspaceId: original.id, mode: 'conversation', applyCode: true })
  expect(imported.status).toBe('imported')
  expect(imported.workspaceId).not.toBe(original.id)
  const destination = ctx.workspaceRegistry.get(WorkspaceId(imported.workspaceId))
  expect(destination?.path).toBe(await realpath(preview.codeDirectory!))
  expect(destination?.sessionIds).toContain(imported.sessionId)
  expect(original.sessionIds).not.toContain(imported.sessionId)
  expect((await ctx.sessionQuery.readSession(SessionId(imported.sessionId))).session.cwd).toBe(destination?.path)
  expect(await readFile(join(destination!.path, 'fix.txt'), 'utf8')).toBe('fixed\n')
  expect(ctx.workspaceRegistry.get(original.id)?.path).toBe(original.path)
})
