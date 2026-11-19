/** Reuse an existing SDK locally; CI installs only the other supported runtime in a disposable root. */
import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawn } from 'node:child_process'

const plugin = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const args = process.argv.slice(2)
const at = args.indexOf('--sdk-root')
let sdkRoot = at < 0 ? undefined : args[at + 1]
const clientAt = args.indexOf('--client-packages-root')
const clientRoot = clientAt < 0 ? undefined : args[clientAt + 1]
let temporary
async function run(argv) {
  await new Promise((accept, reject) => {
    const child = spawn(process.execPath, argv, { cwd: plugin, stdio: 'inherit', shell: false })
    child.once('error', reject)
    child.once('exit', (code, signal) => code === 0 ? accept() : reject(new Error(`Compatibility check exited ${code ?? signal}`)))
  })
}
try {
  if (!sdkRoot) {
    if (!process.env.npm_execpath) throw new Error('Run this check with npm run check:compat, or provide --sdk-root.')
    temporary = await mkdtemp(join(tmpdir(), 'agent-handoff-rc1-sdk-'))
    await writeFile(join(temporary, 'package.json'), JSON.stringify({ private: true, dependencies: {
      '@deepseek-ai/dsh': '0.2.2-rc.1',
      '@deepseek-ai/dsh-client-store': '0.2.2-rc.1',
      '@deepseek-ai/dsh-client-ui-slots': '0.2.2-rc.1',
      '@deepseek-ai/dsh-client-ui-primitives': '0.2.2-rc.1',
    } }))
    await run([process.env.npm_execpath, 'install', '--prefix', temporary, '--ignore-scripts', '--no-fund', '--no-audit'])
    sdkRoot = join(temporary, 'node_modules')
  }
  await run([join(plugin, 'tests/run-sdk-tests.mjs'), '--sdk-root', resolve(sdkRoot), '--client',
    ...(clientRoot ? ['--client-packages-root', resolve(clientRoot)] : []),
  ])
} finally {
  if (temporary) await rm(temporary, { recursive: true, force: true })
}
