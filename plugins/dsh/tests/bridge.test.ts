import { afterEach, describe, expect, it } from 'vitest'
import { mkdtemp, writeFile, rm, readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { createBridge, coreBinary } from '../src/bridge.ts'
const roots: string[] = []
afterEach(async () => { for (const root of roots.splice(0)) await rm(root, { recursive: true, force: true }) })
async function script(content: string) {
  const root = await mkdtemp(join(tmpdir(), 'handoff-child-test-')); roots.push(root)
  const path = join(root, 'fixture.mjs'); await writeFile(path, content)
  return { root, path }
}
describe('bounded transfer subprocess', () => {
  it('passes JSON over stdin without a shell and never reports raw child diagnostics', async () => {
    const fixture = await script(`let input='';for await(const c of process.stdin)input+=c;process.stderr.write('DO NOT LEAK credentials');console.log(JSON.stringify({status:'ok',value:JSON.parse(input)}));`)
    const run = createBridge({ corePath: process.execPath }, new AbortController().signal)
    const data = { text: '$(never-run) `never-run` ; path with spaces' }
    expect(await run([fixture.path], data)).toEqual({ status: 'ok', value: data })
    const bad = await script(`console.error('DO NOT LEAK raw private path');console.log(JSON.stringify({status:'error',error_code:'read_failed',message:'DO NOT LEAK'}));process.exitCode=1;`)
    await expect(run([bad.path])).rejects.toMatchObject({ code: 'read_failed', message: 'The transfer operation failed (read_failed).' })
  })
  it('accepts redacted secret block results even with a nonzero exit', async () => {
    const fixture = await script(`console.log(JSON.stringify({status:'blocked',error_code:'secret_detected',safety:{findings:[{rule:'token',path:'$.neutral'}]}}));process.exitCode=1;`)
    const result = await createBridge({ corePath: process.execPath }, new AbortController().signal)([fixture.path])
    expect(result.status).toBe('blocked')
  })
  it('waits for child exit on cancellation and kills timed-out transfers', async () => {
    const fixture = await script(`import{writeFileSync}from'node:fs';writeFileSync(new URL('./pid',import.meta.url),String(process.pid));setInterval(()=>{},1000);`)
    const abort = new AbortController(), run = createBridge({ corePath: process.execPath, timeoutMs: 2000 }, abort.signal)
    const pending = run([fixture.path]); const outcome = pending.catch(error => error)
    // A bounded fixture wait observes actual child startup before cancelling it.
    let pid = 0
    for (let i = 0; i < 100 && !pid; i++) { try { pid = Number(await readFile(join(fixture.root, 'pid'), 'utf8')) } catch { await new Promise(resolve => setTimeout(resolve, 5)) } }
    expect(pid).toBeGreaterThan(0); abort.abort()
    expect(await outcome).toMatchObject({ code: 'disposed' })
    expect(() => process.kill(pid, 0)).toThrow()
    await expect(createBridge({ corePath: process.execPath, timeoutMs: 30 }, new AbortController().signal)([fixture.path])).rejects.toMatchObject({ code: 'bridge_timeout' })
  })
  it('cancels one request without aborting another operation or the plugin lifetime', async () => {
    const slow = await script(`setInterval(()=>{},1000);`), fast = await script(`console.log(JSON.stringify({status:'ok'}));`)
    const lifetime = new AbortController(), operation = new AbortController(), run = createBridge({ corePath: process.execPath }, lifetime.signal)
    const cancelled = run([slow.path], undefined, operation.signal).catch(error => error)
    const unaffected = run([fast.path])
    operation.abort()
    expect(await cancelled).toMatchObject({ code: 'cancelled' })
    expect(await unaffected).toEqual({ status: 'ok' }); expect(lifetime.signal.aborted).toBe(false)
  })
  it('rejects malformed and oversized child output', async () => {
    const malformed = await script(`process.stdout.write('not JSON');`)
    const run = createBridge({ corePath: process.execPath }, new AbortController().signal)
    await expect(run([malformed.path])).rejects.toMatchObject({ code: 'invalid_bridge_response' })
    const large = await script(`process.stdout.write('x'.repeat(33*1024*1024));`)
    await expect(run([large.path])).rejects.toMatchObject({ code: 'bridge_output_too_large' })
  })
  it('requires an absolute configured binary path', () => {
    expect(() => coreBinary({ corePath: './untrusted-relative' })).toThrow(/absolute/)
  })
})
