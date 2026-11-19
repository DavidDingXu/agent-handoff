import { chmod, copyFile, mkdir } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
for (const target of ['darwin-amd64', 'darwin-arm64', 'linux-amd64', 'linux-arm64', 'windows-amd64']) {
  const file = target.startsWith('windows-') ? 'agent-handoff.exe' : 'agent-handoff'
  const destination = resolve(root, 'bin', target, file)
  await mkdir(dirname(destination), { recursive: true })
  await copyFile(resolve(root, '../../bin', target, file), destination)
  await chmod(destination, 0o755)
}
await mkdir(resolve(root, 'dist'), { recursive: true })
console.log('Copied the five repository core binaries into the installable package')
