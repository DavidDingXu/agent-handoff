import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { readFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const npm = process.platform === 'win32' ? 'npm.cmd' : 'npm'
const packed = spawnSync(npm, ['pack', '--dry-run', '--ignore-scripts', '--json'], { cwd: root, encoding: 'utf8', shell: process.platform === 'win32' })
assert.equal(packed.status, 0, packed.stderr)
const files = new Set(JSON.parse(packed.stdout)[0].files.map(file => file.path))
for (const name of ['lib/index.js', 'lib/client.js', 'lib/types/index.d.ts', 'lib/build-receipt.json', 'lib/legal/LICENSE', 'cordis.patch.yml', 'icon.svg', 'README.md', 'README.zh-CN.md']) assert.ok(files.has(name), `Package missing ${name}`)
for (const target of ['darwin-amd64', 'darwin-arm64', 'linux-amd64', 'linux-arm64', 'windows-amd64']) {
  assert.ok(files.has(`bin/${target}/agent-handoff${target.startsWith('windows-') ? '.exe' : ''}`), `Package missing core for ${target}`)
}
assert.ok(![...files].some(file => file.includes('node_modules/') || file.startsWith('tests/') || file.includes('.test-runtime/')), 'Development state must stay out of the package')
const client = await readFile(resolve(root, 'lib/client.js'), 'utf8')
assert.ok(client.startsWith('window.__ModuleLoader__.load({ id: "dsh-agent-handoff"'), 'Native DSH module-loader wrapper required')
const architecture = process.arch === 'x64' ? 'amd64' : process.arch
const platform = process.platform === 'win32' ? 'windows' : process.platform
const core = resolve(root, 'bin', `${platform}-${architecture}`, `agent-handoff${process.platform === 'win32' ? '.exe' : ''}`)
const probe = spawnSync(core, ['bridge', 'share', '--input', '-'], { input: '{}', encoding: 'utf8', maxBuffer: 1024 * 1024 })
assert.notEqual(probe.status, 0)
assert.equal(JSON.parse(probe.stdout).status, 'error', 'Packaged core must implement the versioned bridge')
console.log(`Installable package checked: ${files.size} files, five cores, native Client wrapper, bridge protocol`)
