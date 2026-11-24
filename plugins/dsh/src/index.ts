/** Installable DSH Host entry. All API calls pass the shared owner-only connection boundary. */
import { readFileSync } from 'node:fs'
import type { Context } from '@deepseek-ai/cordis'
import Schema from '@deepseek-ai/schemastery'
import type {} from '@deepseek-ai/dsh-client-connection'
import type {} from '@deepseek-ai/dsh-session-query'
import type {} from '@deepseek-ai/dsh-session-persistence'
import type {} from '@deepseek-ai/dsh-api-session-controller'
import type {} from '@deepseek-ai/dsh-app-boot'
import type {} from '@deepseek-ai/dsh-skill'
import { defineTool } from '@deepseek-ai/dsh-tools'
import { WorkspaceId } from '@deepseek-ai/dsh-workspace'
import { createHandoffService, createHandoffRoute, type HandoffConfig } from './host.ts'
import { API_PATH } from './types.ts'
export type { HandoffConfig } from './host.ts'
export { createHandoffService } from './host.ts'
export const name = 'dsh-agent-handoff'
export const inject = ['connection', 'sessionQuery', 'sessionPersistence', 'workspaceRegistry', 'sessionController']
const skillPath = new URL('../skills/agent-handoff/SKILL.md', import.meta.url)
const openHandoff = defineTool({
  name: 'open_agent_handoff',
  description: 'Offer a button to share the current DSH task or open the handoff import panel.',
  parameters: { mode: { type: 'string', enum: ['export', 'import'], required: true } },
  output: {
    schema: { type: 'string' },
    render: () => [{ type: 'text', text: 'Use the Task handoff button in this result. The panel controls the transfer.' }],
  },
  async execute(args) { return args.mode },
})
export const Config: Schema<HandoffConfig> = Schema.object({
  corePath: Schema.string().description('Absolute agent-handoff core binary path; omit to use the matching bundled binary.'),
  stateDir: Schema.string().description('Private audit and duplicate-ledger directory; defaults to agent-handoff beneath the current managed DSH profile.'),
  endpoint: Schema.string().description('Optional encrypted share service endpoint.'),
  configPath: Schema.string().description('Optional absolute provider configuration file; never included in a bundle.'),
  ttlSeconds: Schema.natural().min(60).max(86400).default(3600).description('Encrypted-link lifetime in seconds.'),
  timeoutMs: Schema.natural().min(1000).max(300000).default(60000).description('Transfer subprocess timeout.'),
})
export function apply(ctx: Context, config: HandoffConfig): void {
  const service = createHandoffService({
    profileDirectory: ctx.get('profileContext')?.dir,
    sessionQuery: { readSession: id => ctx.sessionQuery.readSession(id), readTitle: id => ctx.sessionQuery.readTitle(id) },
    sessionPersistence: { create: (header, options) => ctx.sessionPersistence.create(header, options), flush: () => ctx.sessionPersistence.flush() },
    workspaceRegistry: { list: () => ctx.workspaceRegistry.list(), get: id => ctx.workspaceRegistry.get(WorkspaceId(id)), create: (path, title) => ctx.workspaceRegistry.create(path, title) },
    sessionController: { resolveAgent: id => ctx.sessionController.resolveAgent(id), rename: request => ctx.sessionController.rename(request) },
  }, config)
  ctx.effect(() => ctx.connection.fetch.register({ path: API_PATH, methods: ['POST'], requestBody: 'streaming', fetch: createHandoffRoute(service) }), 'agent-handoff: owner API')
  ctx.effect(() => () => service.dispose(), 'agent-handoff: active transfers')
  ctx.inject(['skills', 'tools'], (scoped) => {
    const raw = readFileSync(skillPath, 'utf8')
    const skill = /^---\r?\nname: (agent-handoff)\r?\ndescription: ([^\r\n]+)\r?\n---\s*([\s\S]+)$/u.exec(raw)
    if (!skill) throw new Error('Invalid bundled Agent Handoff skill')
    scoped.skills.register({ name: skill[1]!, description: skill[2]!, content: skill[3]!, source: 'bundled' })
    scoped.tools.register(openHandoff)
  })
}
