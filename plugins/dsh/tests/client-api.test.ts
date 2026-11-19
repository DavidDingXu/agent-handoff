// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApiCall, HandoffApiError, readUpload, saveDownload } from '../src/client/api.ts'
import { MAX_UPLOAD_BYTES } from '../src/types.ts'

afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })
describe('native handoff transport and file actions', () => {
  it('uses the desktop carrier with a prefix-safe relative API and preserves structured recovery errors', async () => {
    const nativeFetch = vi.fn(async () => Response.json({ ok: false, error: { code: 'import/partial', message: 'A task was created', recoverySessionId: 'recover-id' } }))
    vi.stubGlobal('__DSH_TRANSPORT__', { fetch: nativeFetch })
    const direct = vi.fn()
    vi.stubGlobal('fetch', direct)
    const base = document.createElement('base'); base.href = 'https://host.example/dsh/'; document.head.append(base)
    try {
      const call = createApiCall()
      await expect(call({ action: 'import', ticketId: 'ticket', workspaceId: 'w', mode: 'native' })).rejects.toMatchObject({ detail: { recoverySessionId: 'recover-id' } })
      expect(nativeFetch).toHaveBeenCalledWith('https://host.example/dsh/api/agent-handoff', expect.objectContaining({ method: 'POST', body: '{"action":"import","ticketId":"ticket","workspaceId":"w","mode":"native"}' }))
      expect(direct).not.toHaveBeenCalled()
    } finally { base.remove() }
  })

  it('surfaces forbidden and malformed responses without returning partial data', async () => {
    const forbidden = createApiCall(async () => new Response('forbidden', { status: 403 }))
    await expect(forbidden({ action: 'workspaces' })).rejects.toMatchObject({ detail: { code: 'ui/owner-only' } })
    const malformed = createApiCall(async () => Response.json({ ok: true }))
    await expect(malformed({ action: 'workspaces' })).rejects.toBeInstanceOf(HandoffApiError)
  })

  it('reads an explicitly chosen ZIP and rejects oversized files before reading', async () => {
    const file = new File(['PK'], 'handoff.zip', { type: 'application/zip' })
    await expect(readUpload(file)).resolves.toEqual({ kind: 'upload', fileName: 'handoff.zip', dataBase64: 'UEs=' })
    const huge = new File([], 'large.zip')
    Object.defineProperty(huge, 'size', { value: MAX_UPLOAD_BYTES + 1 })
    await expect(readUpload(huge)).rejects.toMatchObject({ detail: { code: 'ui/upload-too-large' } })
  })

  it('saves returned ZIP bytes with a safe filename and releases its temporary URL', () => {
    vi.useFakeTimers()
    const create = vi.fn((_blob: Blob) => 'blob:test-zip')
    const revoke = vi.fn()
    vi.stubGlobal('URL', class extends URL { static createObjectURL = create; static revokeObjectURL = revoke })
    let saved = ''
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) { saved = this.download })
    try {
      saveDownload({ fileName: '../../handoff\n.zip', dataBase64: 'UEs=' })
      expect(saved).not.toMatch(/[\n/\\]/)
      expect(create.mock.calls[0]?.[0]).toBeInstanceOf(Blob)
      expect(revoke).not.toHaveBeenCalled()
      vi.runAllTimers()
      expect(revoke).toHaveBeenCalledWith('blob:test-zip')
    } finally { vi.useRealTimers() }
  })
})
