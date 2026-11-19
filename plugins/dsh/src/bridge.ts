/** Bounded shell-free bridge process. No bundle content is interpreted as a command. */
import { spawn } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { isAbsolute } from 'node:path'
import { HandoffFailure, record } from './session.ts'

export interface BridgeConfig { corePath?: string; endpoint?: string; configPath?: string; ttlSeconds?: number; timeoutMs?: number }
export type BridgeRunner = (args: string[], input?: unknown, signal?: AbortSignal) => Promise<Record<string, unknown>>
export function coreBinary(config: BridgeConfig): string {
  if (config.corePath) {
    if (!isAbsolute(config.corePath)) throw new HandoffFailure('setup_required', 'corePath must name an absolute agent-handoff binary path.')
    return config.corePath
  }
  const platform = process.platform === 'win32' ? 'windows' : process.platform === 'darwin' ? 'darwin' : process.platform === 'linux' ? 'linux' : ''
  const arch = process.arch === 'x64' ? 'amd64' : process.arch === 'arm64' ? 'arm64' : ''
  if (!platform || !arch) throw new HandoffFailure('unsupported_platform', 'This platform has no bundled transfer binary; configure corePath.')
  return fileURLToPath(new URL(`../bin/${platform}-${arch}/agent-handoff${process.platform === 'win32' ? '.exe' : ''}`, import.meta.url))
}
export function createBridge(config: BridgeConfig, lifetime: AbortSignal): BridgeRunner {
  const executable = coreBinary(config)
  return async (args, input, requestSignal) => {
    const signal = requestSignal ? AbortSignal.any([lifetime, requestSignal]) : lifetime
    if (requestSignal?.aborted) throw new HandoffFailure('cancelled', 'The handoff operation was cancelled.')
    if (lifetime.aborted) throw new HandoffFailure('disposed', 'The handoff plugin is stopping.')
    const data = input === undefined ? undefined : JSON.stringify(input)
    if (data !== undefined && Buffer.byteLength(data) > 32 * 1024 * 1024) throw new HandoffFailure('input_too_large', 'The transfer request exceeds the size limit.')
    return await new Promise<Record<string, unknown>>((resolve, reject) => {
      const child = spawn(executable, args, { shell: false, windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'] })
      const chunks: Buffer[] = []; let bytes = 0, settled = false, stopping: HandoffFailure | undefined, killTimer: ReturnType<typeof setTimeout> | undefined
      const finish = (error?: HandoffFailure, result?: Record<string, unknown>) => {
        if (settled) return
        settled = true; clearTimeout(timer); clearTimeout(killTimer); signal.removeEventListener('abort', cancel)
        if (error) reject(error); else resolve(result!)
      }
      const stop = (error: HandoffFailure) => {
        if (settled || stopping) return
        stopping = error; child.kill()
        killTimer = setTimeout(() => { child.kill('SIGKILL') }, 1000); killTimer.unref()
      }
      const cancel = () => stop(new HandoffFailure(lifetime.aborted ? 'disposed' : 'cancelled', 'The handoff operation was cancelled.'))
      const timer = setTimeout(() => stop(new HandoffFailure('bridge_timeout', 'The transfer operation timed out. Retry when the service is available.')), config.timeoutMs ?? 60_000)
      signal.addEventListener('abort', cancel, { once: true })
      child.stdout.on('data', (chunk: Buffer) => {
        bytes += chunk.length
        if (bytes > 32 * 1024 * 1024) stop(new HandoffFailure('bridge_output_too_large', 'The transfer response exceeds the size limit.'))
        else if (!stopping) chunks.push(chunk)
      })
      // Never expose subprocess diagnostics: they may contain local paths, links or credentials.
      child.stderr.on('data', () => {})
      child.on('error', () => finish(new HandoffFailure('bridge_unavailable', 'The transfer binary could not start. Check corePath or reinstall the plugin.')))
      child.stdin.on('error', () => {})
      child.on('close', code => {
        if (settled) return
        if (stopping) return finish(stopping)
        let value: unknown
        try { value = JSON.parse(Buffer.concat(chunks).toString('utf8')) } catch { return finish(new HandoffFailure('invalid_bridge_response', 'The transfer binary returned an invalid response.')) }
        if (!record(value)) return finish(new HandoffFailure('invalid_bridge_response', 'The transfer binary returned an invalid response.'))
        if (code !== 0 && !(value.status === 'blocked' && value.error_code === 'secret_detected')) {
          const safeCode = typeof value.error_code === 'string' && /^[a-z_]{1,64}$/.test(value.error_code) ? value.error_code : 'bridge_failed'
          return finish(new HandoffFailure(safeCode, `The transfer operation failed (${safeCode}).`))
        }
        finish(undefined, value)
      })
      child.stdin.end(data)
      if (signal.aborted) cancel()
    })
  }
}
