/** Data-only protocol shared by the DSH Host and Client. Never import Host SDK code here. */
export const API_PATH = '/api/agent-handoff'
export const DSH_VERSION = '0.2.2-rc.5'
export const SUPPORTED_DSH_VERSIONS = ['0.2.2-rc.1', '0.2.2-rc.5'] as const
export const MAX_UPLOAD_BYTES = 16 * 1024 * 1024
export const PROVIDER_WARNING = 'One or more upload providers were unavailable.'

export interface WorkspaceChoice { id: string; name: string; directory: string }
export interface BriefExcerpt { entry_index: number; text: string; truncated?: boolean }
export interface TaskBrief { schema: 'agent-handoff.brief.v1'; objective?: BriefExcerpt; latest_request?: BriefExcerpt; latest_update?: BriefExcerpt; evidence: { entry_index: number; tool?: string; status?: string; input?: string; output?: string }[]; notes?: string }
export interface CodeCandidate { path: string; status: string; bytes: number; eligible: boolean; reason?: string }
export interface CodeInspection { root: string; base_commit: string; branch: string; files: CodeCandidate[] }
export interface CodeManifest { schema?: string; base_commit: string; branch?: string; files: { path: string; status: string; bytes: number }[] }
export interface CodePayload { manifest: CodeManifest; patch_base64: string; files_base64: Record<string, string> }
export interface ExportOptions { briefNote?: string; codeFiles?: string[] }
export type SwitchTarget = 'codex' | 'claude'
export interface SwitchedTask { status: 'ok' | 'duplicate'; targetAgent: SwitchTarget; sessionId: string; title: string; directory: string; command: string; warnings: string[] }
export type SwitchResult = SwitchedTask | (ShareResult & { status: 'blocked' | 'fallback_zip' })
export interface SessionSummary { sessionId: string; title: string; messageCount: number; warnings: string[]; brief?: TaskBrief }
export type ShareFormat = 'zip' | 'link'
export type ImportMode = 'native' | 'conversation'
export interface SafeFinding { rule: string; field?: string; line?: number }
export interface ShareResult {
  status: 'ok' | 'fallback_zip' | 'blocked'
  title: string
  messageCount: number
  shareUrl?: string
  expiresAt?: string
  downloadId?: string
  fileName?: string
  sizeBytes?: number
  warnings: string[]
  findings?: SafeFinding[]
  secretTicket?: string
}
export type ImportSource =
  | { kind: 'upload'; fileName: string; dataBase64: string }
  | { kind: 'link'; url: string }
  | { kind: 'path'; path: string }
export interface ImportPreview {
  ticketId: string
  sourceAgent: string
  sourceSessionId: string
  title: string
  messageCount: number
  fingerprint: string
  checksumStatus: string
  workspace: WorkspaceChoice
  modes: ImportMode[]
  warnings: string[]
  existingSessionId?: string
  existingCodeSessionId?: string
  expiresAt: string
  brief?: TaskBrief
  code?: CodeManifest
  codeDirectory?: string
}
export interface ImportResult {
  status: 'imported' | 'duplicate'
  sessionId: string
  title: string
  workspaceId: string
  mode: ImportMode
  warnings: string[]
  codeDirectory?: string
}
export interface DownloadResult { fileName: string; dataBase64: string }
export interface HandoffError { code: string; message: string; recoverySessionId?: string; recoveryDirectory?: string }
export type ApiResponse<T> = { ok: true; data: T } | { ok: false; error: HandoffError }
export type ApiRequest =
  | { action: 'workspaces' }
  | { action: 'summary'; sessionId: string }
  | { action: 'share'; sessionId: string; format: ShareFormat; secretTicket?: string; options?: ExportOptions }
  | { action: 'switch'; sessionId: string; target: SwitchTarget; options?: ExportOptions; secretTicket?: string }
  | { action: 'code-preview'; sessionId: string }
  | { action: 'preview'; source: ImportSource; workspaceId: string }
  | { action: 'import'; ticketId: string; workspaceId: string; mode: ImportMode; applyCode?: boolean }
  | { action: 'download'; downloadId: string }

/** Only the authenticated Host route exposes these methods to a Client. */
export interface HandoffService {
  workspaces(signal?: AbortSignal): Promise<WorkspaceChoice[]>
  summary(sessionId: string, signal?: AbortSignal): Promise<SessionSummary>
  share(sessionId: string, format: ShareFormat, secretTicket?: string, signal?: AbortSignal, options?: ExportOptions): Promise<ShareResult>
  switch(sessionId: string, target: SwitchTarget, options?: ExportOptions, secretTicket?: string, signal?: AbortSignal): Promise<SwitchResult>
  inspectCode(sessionId: string, signal?: AbortSignal): Promise<CodeInspection>
  preview(source: ImportSource, workspaceId: string, signal?: AbortSignal): Promise<ImportPreview>
  import(ticketId: string, workspaceId: string, mode: ImportMode, signal?: AbortSignal, applyCode?: boolean): Promise<ImportResult>
  download(downloadId: string, signal?: AbortSignal): Promise<DownloadResult>
  dispose(): Promise<void>
}
