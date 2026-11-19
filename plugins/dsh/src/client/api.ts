/** Browser-only transport. The shell's authenticated carrier also supports file:// desktop pages. */
import { API_PATH, MAX_UPLOAD_BYTES, type ApiRequest, type ApiResponse, type DownloadResult, type HandoffError, type ImportSource } from '../types.ts'
export type ApiCall = <T>(input: ApiRequest, signal?: AbortSignal) => Promise<T>
export type FetchCarrier = (input: string | URL, init: RequestInit) => Promise<Response>
export class HandoffApiError extends Error {
  constructor(readonly detail: HandoffError) { super(detail.message); this.name = 'HandoffApiError' }
}
function failure(code: string, message: string): HandoffApiError { return new HandoffApiError({ code, message }) }
function record(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value) }
/** One caller preserves browser mount prefixes and lets the installed shell supply authentication. */
export function createApiCall(fetcher?: FetchCarrier): ApiCall {
  return async <T>(input: ApiRequest, signal?: AbortSignal): Promise<T> => {
    const shell = (globalThis as { __DSH_TRANSPORT__?: { fetch?: FetchCarrier } }).__DSH_TRANSPORT__?.fetch
    const send = fetcher ?? shell ?? ((url, init) => fetch(url, init))
    let response: Response
    try {
      response = await send(new URL(API_PATH.slice(1), document.baseURI).href, {
        method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(input), signal,
      })
    } catch (error) {
      if (signal?.aborted) throw error
      throw failure('ui/offline', 'DSH could not be reached')
    }
    if (response.status === 401 || response.status === 403) throw failure('ui/owner-only', 'Only the owner may perform this operation')
    let body: unknown
    try { body = await response.json() } catch { throw failure('ui/invalid-response', 'Invalid handoff response') }
    signal?.throwIfAborted()
    if (!record(body) || typeof body.ok !== 'boolean') throw failure('ui/invalid-response', 'Invalid handoff response')
    if (!body.ok) {
      const error = body.error
      if (!record(error) || typeof error.code !== 'string' || typeof error.message !== 'string') throw failure('ui/invalid-response', 'Invalid handoff response')
      throw new HandoffApiError({ code: error.code, message: error.message, ...(typeof error.recoverySessionId === 'string' ? { recoverySessionId: error.recoverySessionId } : {}), ...(typeof error.recoveryDirectory === 'string' ? {recoveryDirectory:error.recoveryDirectory} : {}) })
    }
    if (!response.ok || !Object.hasOwn(body, 'data')) throw failure('ui/invalid-response', 'Invalid handoff response')
    return (body as unknown as ApiResponse<T> & { ok: true }).data
  }
}
/** Read only the file the user selected, enforcing the upload limit before allocating its encoded content. */
export function readUpload(file: File, signal?: AbortSignal): Promise<Extract<ImportSource, { kind: 'upload' }>> {
  if (file.size > MAX_UPLOAD_BYTES) return Promise.reject(failure('ui/upload-too-large', 'The selected ZIP is too large'))
  return new Promise((resolve, reject) => {
    signal?.throwIfAborted()
    const reader = new FileReader()
    const abort = () => reader.abort()
    const off = () => signal?.removeEventListener('abort', abort)
    reader.onerror = () => { off(); reject(failure('ui/upload-failed', 'The selected file could not be read')) }
    reader.onabort = () => { off(); reject(new DOMException('Cancelled', 'AbortError')) }
    reader.onload = () => {
      off()
      const value = reader.result
      if (typeof value !== 'string' || !value.includes(',')) { reject(failure('ui/upload-failed', 'The selected file could not be read')); return }
      resolve({ kind: 'upload', fileName: file.name, dataBase64: value.slice(value.indexOf(',') + 1) })
    }
    signal?.addEventListener('abort', abort, { once: true })
    reader.readAsDataURL(file)
  })
}
/** Hand verified returned bytes to the browser; the temporary URL lives long enough for its download manager. */
export function saveDownload(result: DownloadResult): void {
  const decoded = atob(result.dataBase64)
  const bytes = Uint8Array.from(decoded, character => character.charCodeAt(0))
  const blob = new Blob([bytes], { type: 'application/zip' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = result.fileName.replace(/[\x00-\x1f\x7f/\\]/g, '_').replace(/^\.+/, '') || 'handoff.zip'
  document.body.append(anchor)
  try { anchor.click() } finally { anchor.remove(); setTimeout(() => URL.revokeObjectURL(url), 1000) }
}
