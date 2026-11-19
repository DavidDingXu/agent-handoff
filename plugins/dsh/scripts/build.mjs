import { build } from 'esbuild'
import { cp, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const platformModules = [
  'react', 'react/jsx-runtime', 'react-dom', 'react-dom/client',
  '@deepseek-ai/cordis', 'zod', '@tanstack/react-virtual',
  '@deepseek-ai/dsh-client-store', '@deepseek-ai/dsh-client-ui-slots',
  '@deepseek-ai/dsh-client-ui-primitives', '@deepseek-ai/dsh-client-ui-dockkit',
]
await rm(resolve(root, 'lib'), { recursive: true, force: true })
await mkdir(resolve(root, 'lib'), { recursive: true })
await build({
  absWorkingDir: root, entryPoints: ['src/index.ts'], outfile: 'lib/index.js',
  bundle: true, platform: 'node', format: 'esm', target: 'node24',
  external: ['@deepseek-ai/*'], sourcemap: true,
})
await build({
  absWorkingDir: root, entryPoints: ['src/client.tsx'], outfile: 'lib/client.js',
  bundle: true, platform: 'browser', format: 'cjs', target: 'es2022', jsx: 'automatic',
  external: platformModules, sourcemap: true,
  banner: { js: 'window.__ModuleLoader__.load({ id: "dsh-agent-handoff", factory: (require) => { var module = { exports: {} }; var exports = module.exports;' },
  footer: { js: 'return module.exports; } });' },
})
const declarations = spawnSync(process.execPath, [resolve(root, 'node_modules/typescript/bin/tsc'), '--project', 'tsconfig.build.json'], { cwd: root, stdio: 'inherit' })
if (declarations.status !== 0) process.exit(declarations.status ?? 1)
// Licenses travel with the compiled core, without duplicating generated files in Git.
await mkdir(resolve(root, 'lib/legal'), { recursive: true })
await cp(resolve(root, '../../LICENSE'), resolve(root, 'lib/legal/LICENSE'))
await cp(resolve(root, '../../THIRD_PARTY_NOTICES.md'), resolve(root, 'lib/legal/THIRD_PARTY_NOTICES.md'))
await cp(resolve(root, '../../third_party_licenses'), resolve(root, 'lib/legal/third_party_licenses'), { recursive: true })
const manifest = JSON.parse(await readFile(resolve(root, 'package.json'), 'utf8'))
await writeFile(resolve(root, 'lib/build-receipt.json'), JSON.stringify({
  name: manifest.name, version: manifest.version, dsh: '0.2.2-rc.5',
  upstream: '2e39e70496aa02986ca6a820c5d497e816b69d7e',
  platformModules,
}, null, 2) + '\n')
console.log('Built DSH Host and native Client entries')
