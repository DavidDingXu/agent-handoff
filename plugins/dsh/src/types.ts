/** Data-only protocol shared by the DSH Host and Client. Never import Host SDK code here. */
export const API_PATH = '/api/agent-handoff'
export const DSH_VERSION = '0.2.2-rc.5'
export const SUPPORTED_DSH_VERSIONS = ['0.2.2-rc.1', '0.2.2-rc.5'] as const
export const MAX_UPLOAD_BYTES = 16 * 1024 * 1024

export interface WorkspaceChoice { id: string; name: string; directory: string }
export interface SessionSummary { sessionId: string; title: string; messageCount: number; warnings: string[] }
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
  expiresAt: string
}
export interface ImportResult {
  status: 'imported' | 'duplicate'
  sessionId: string
  title: string
  workspaceId: string
  mode: ImportMode
  warnings: string[]
}
export interface DownloadResult { fileName: string; dataBase64: string }
export interface HandoffError { code: string; message: string; recoverySessionId?: string }
export type ApiResponse<T> = { ok: true; data: T } | { ok: false; error: HandoffError }
export type ApiRequest =
  | { action: 'workspaces' }
  | { action: 'summary'; sessionId: string }
  | { action: 'share'; sessionId: string; format: ShareFormat; secretTicket?: string }
  | { action: 'preview'; source: ImportSource; workspaceId: string }
  | { action: 'import'; ticketId: string; workspaceId: string; mode: ImportMode }
  | { action: 'download'; downloadId: string }

/** Only the authenticated Host route exposes these methods to a Client. */
export interface HandoffService {
  workspaces(signal?: AbortSignal): Promise<WorkspaceChoice[]>
  summary(sessionId: string, signal?: AbortSignal): Promise<SessionSummary>
  share(sessionId: string, format: ShareFormat, secretTicket?: string, signal?: AbortSignal): Promise<ShareResult>
  preview(source: ImportSource, workspaceId: string, signal?: AbortSignal): Promise<ImportPreview>
  import(ticketId: string, workspaceId: string, mode: ImportMode, signal?: AbortSignal): Promise<ImportResult>
  download(downloadId: string, signal?: AbortSignal): Promise<DownloadResult>
  dispose(): Promise<void>
}
