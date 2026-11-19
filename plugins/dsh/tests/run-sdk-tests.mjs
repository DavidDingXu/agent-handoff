/** Run the same Host/runtime tests against existing SDK software without modifying it. */
import { cp, mkdir, mkdtemp, readFile, readdir, realpath, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, isAbsolute, join, resolve, win32 } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { spawn } from 'node:child_process'

const plugin = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const args = process.argv.slice(2)
const option = name => { const at = args.indexOf(name); return at < 0 ? undefined : args[at + 1] }
const sdkRoot = option('--sdk-root')
if (!sdkRoot) throw new Error('Usage: node tests/run-sdk-tests.mjs --sdk-root /absolute/existing/node_modules [--tools-root /absolute/node_modules] [--keep]')
const sdk = await realpath(resolve(sdkRoot)), tools = await realpath(resolve(option('--tools-root') ?? join(plugin, 'node_modules')))
const overlay = await mkdtemp(join(tmpdir(), 'agent-handoff-sdk-test-'))
const serverTests = ['session.test.ts', 'host.test.ts', 'bridge.test.ts', 'runtime.test.ts']
const clientTests = ['client.test.tsx', 'client-api.test.ts', 'client-registry.test.tsx', 'hub.test.tsx']
const withClient = args.includes('--client')
async function assertSdkPackage(directory, name, version) {
  const actual = JSON.parse(await readFile(join(directory, 'package.json'), 'utf8'))
  if (actual.name !== `@deepseek-ai/${name}` || actual.version !== version) {
    throw new Error(`SDK package mismatch at ${directory}: expected @deepseek-ai/${name}@${version}, received ${actual.name}@${actual.version}`)
  }
}
async function run(command, argv) {
  await new Promise((resolvePromise, reject) => {
    const child = spawn(command, argv, { cwd: overlay, stdio: 'inherit', shell: false })
    child.once('error', reject)
    child.once('exit', (code, signal) => code === 0 ? resolvePromise() : reject(new Error(`SDK test exited ${code ?? signal}`)))
  })
}
try {
  await cp(join(plugin, 'src'), join(overlay, 'src'), { recursive: true })
  await mkdir(join(overlay, 'tests')); await mkdir(join(overlay, 'node_modules', '@deepseek-ai'), { recursive: true })
  for (const test of serverTests) await cp(join(plugin, 'tests', test), join(overlay, 'tests', test))
  if (withClient) for (const test of [...clientTests, 'native-registry.ts', 'styles.d.ts']) await cp(join(plugin, 'tests', test), join(overlay, 'tests', test))
  for (const entry of await readdir(tools, { withFileTypes: true })) if (entry.name !== '@deepseek-ai') await symlink(join(tools, entry.name), join(overlay, 'node_modules', entry.name), entry.isDirectory() ? 'junction' : 'file')
  const packages = await readdir(join(sdk, '@deepseek-ai'))
  for (const entry of packages) await symlink(join(sdk, '@deepseek-ai', entry), join(overlay, 'node_modules', '@deepseek-ai', entry), 'junction')
  if (withClient) {
    const extra = option('--client-packages-root')
    const { version } = JSON.parse(await readFile(join(sdk, '@deepseek-ai', 'dsh-session', 'package.json'), 'utf8'))
    if (typeof version !== 'string') throw new Error('Selected SDK session package has no version')
    await assertSdkPackage(join(sdk, '@deepseek-ai', 'dsh-client-ui-renderer'), 'dsh-client-ui-renderer', version)
    for (const name of ['dsh-client-store', 'dsh-client-ui-slots', 'dsh-client-ui-primitives']) {
      if (packages.includes(name)) {
        await assertSdkPackage(join(sdk, '@deepseek-ai', name), name, version)
        continue
      }
      if (!extra) throw new Error(`Client tests need the selected SDK's @deepseek-ai/${name}; install that exact version in the SDK root, or provide --client-packages-root with the three verified package folders.`)
      const source = join(resolve(extra), name, 'package')
      await assertSdkPackage(source, name, version)
      await cp(source, join(overlay, 'node_modules', '@deepseek-ai', name), { recursive: true })
    }
    // Released rc.1 primitives ship compiled ESM/CSS, but no source. Exercise the
    // actual selected SDK's controls without loading unrelated Markdown engines.
    const { build } = await import(pathToFileURL(join(tools, 'esbuild', 'lib', 'main.js')).href)
    const typescript = (await import(pathToFileURL(join(tools, 'typescript', 'lib', 'typescript.js')).href)).default
    const nativeEntry = join(overlay, 'node_modules', '@deepseek-ai', 'dsh-client-ui-primitives', 'lib', 'index.js')
    await build({
      stdin: { contents: `export {Button, Input, Modal, IconShareOutlineRegular, SegmentedTabs, writeClipboard} from ${JSON.stringify(nativeEntry)}`, resolveDir: overlay },
      outfile: join(overlay, 'tests', 'native-primitives-runtime.mjs'), bundle: true, format: 'esm', platform: 'browser', treeShaking: true,
      plugins: [{ name: 'native-controls-only', setup(buildContext) {
        buildContext.onLoad({ filter: /dsh-client-ui-primitives[/\\]lib[/\\]index\.js$/ }, async item => {
          // Upstream describes this barrel as pure React atoms. Its older build
          // loses PURE annotations on unused Markdown/highlighter initializers.
          // Restore annotations only inside top-level variable initializers;
          // component functions, Hooks, effects and selected state stay intact.
          const source = typescript.createSourceFile(item.path, await readFile(item.path, 'utf8'), typescript.ScriptTarget.Latest, true, typescript.ScriptKind.JS)
          const transformed = typescript.transform(source, [context => root => {
            const pure = initializer => typescript.addSyntheticLeadingComment(typescript.factory.createCallExpression(
              typescript.factory.createParenthesizedExpression(typescript.factory.createArrowFunction(undefined, undefined, [], undefined, typescript.factory.createToken(typescript.SyntaxKind.EqualsGreaterThanToken), initializer)), undefined, [],
            ), typescript.SyntaxKind.MultiLineCommentTrivia, ' @__PURE__ ', false)
            return typescript.factory.updateSourceFile(root, root.statements.map(statement => typescript.isVariableStatement(statement)
              ? typescript.visitEachChild(statement, node => typescript.isVariableDeclarationList(node)
                ? typescript.factory.updateVariableDeclarationList(node, node.declarations.map(declaration => typescript.factory.updateVariableDeclaration(declaration, declaration.name, declaration.exclamationToken, declaration.type, declaration.initializer ? pure(declaration.initializer) : undefined))) : node, context)
              : typescript.isExpressionStatement(statement) && statement.expression.getText(source).startsWith('setTimeout(')
                ? typescript.factory.updateExpressionStatement(statement, pure(statement.expression))
                : statement))
          }])
          const contents = typescript.createPrinter().printFile(transformed.transformed[0]); transformed.dispose()
          return { contents, loader: 'js' }
        })
        buildContext.onResolve({ filter: /\.css$/ }, item => ({ path: resolve(dirname(item.importer), item.path), external: true, sideEffects: false }))
        buildContext.onResolve({ filter: /^[^./]|^@/ }, item => {
          // Absolute native entries must reach onLoad, including Windows drive and UNC paths.
          if (isAbsolute(item.path) || win32.isAbsolute(item.path)) return
          return { path: item.path, external: true, sideEffects: false }
        })
      } }],
    })
  }
  // Test-only helper has no credentials/model defaults. Copy its small built package so
  // its prerequisite imports resolve to THIS SDK rather than the tools SDK's realpath.
  if (!packages.includes('dsh-agent-loop-testkit')) await cp(join(tools, '@deepseek-ai', 'dsh-agent-loop-testkit'), join(overlay, 'node_modules', '@deepseek-ai', 'dsh-agent-loop-testkit'), { recursive: true })
  await writeFile(join(overlay, 'package.json'), JSON.stringify({ private: true, type: 'module' }))
  await writeFile(join(overlay, 'vitest.config.ts'), `import {defineConfig} from 'vitest/config'; export default defineConfig({${withClient ? `resolve:{alias:[{find:/^@deepseek-ai\\/dsh-client-ui-primitives$/,replacement:${JSON.stringify(join(overlay, 'tests', 'native-primitives-runtime.mjs'))}}]},` : ''}test:{include:['tests/**/*.test.ts','tests/**/*.test.tsx'],testTimeout:30000,hookTimeout:30000,environment:'node'}})`)
  await writeFile(join(overlay, 'tsconfig.json'), JSON.stringify({ compilerOptions: {
    target: 'ES2024', module: 'NodeNext', moduleResolution: 'NodeNext', lib: ['ES2024', 'DOM'], strict: true,
    skipLibCheck: true, noUncheckedIndexedAccess: true, verbatimModuleSyntax: true, allowImportingTsExtensions: true,
    rewriteRelativeImportExtensions: true, jsx: 'react-jsx', types: ['node', 'vitest/globals'],
  }, include: ['src/index.ts', 'src/host.ts', 'src/session.ts', 'src/bridge.ts', 'src/types.ts', 'tests/*.test.ts'] }))
  console.log(`SDK root: ${sdk}\nIsolated test overlay: ${overlay}`)
  await run(process.execPath, [join(tools, 'typescript', 'bin', 'tsc'), '--noEmit'])
  await run(process.execPath, [join(tools, 'vitest', 'vitest.mjs'), 'run', ...[...serverTests, ...withClient ? clientTests : []].map(test => `tests/${test}`)])
} finally {
  if (args.includes('--keep')) console.log(`Kept task-owned SDK test overlay: ${overlay}`)
  else await rm(overlay, { recursive: true, force: true })
}
