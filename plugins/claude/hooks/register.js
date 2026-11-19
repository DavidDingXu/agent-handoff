// Claude Code Mods API, verified with v2.1.322. This command owns its local
// action and never calls a model, submits a prompt, or runs a shell.
let switching = false

// The Mods runtime has web/JavaScript primitives but no Node APIs. Keep path
// handling independent of Node's process, path, and fileURLToPath helpers.
export function bundledBinaryPath(root, platform, arch) {
  if (!['darwin-amd64', 'darwin-arm64', 'linux-amd64', 'linux-arm64', 'windows-amd64'].includes(platform + '-' + arch)) {
    throw new Error('Unsupported bundled platform')
  }
  if (typeof root !== 'string' || !root) throw new Error('Plugin root is unavailable')
  if (root.startsWith('file:')) {
    const location = new URL(root)
    const path = decodeURIComponent(location.pathname)
    root = location.hostname && location.hostname !== 'localhost' ? '//' + location.hostname + path : path
    if (platform === 'windows' && /^\/[a-zA-Z]:\//.test(root)) root = root.slice(1)
  }
  root = root.replace(/\\/g, '/')
  if (!root.startsWith('/') && !/^[a-zA-Z]:\//.test(root)) throw new Error('Plugin root must be absolute')
  return root.replace(/\/+$/, '') + '/bin/' + platform + '-' + arch + '/agent-handoff' + (platform === 'windows' ? '.exe' : '')
}

async function localBinary($) {
  const configured = await $.env.get('AGENT_HANDOFF_BIN')
  if (configured) return configured
  if (await $.env.get('OS') === 'Windows_NT') {
    // The release ships a Windows amd64 executable, matching the existing
    // bundled-core platform contract (also used through Windows emulation).
    return bundledBinaryPath($.plugin.root, 'windows', 'amd64')
  }
  const detected = await $.process.run(['uname', '-s', '-m'])
  if (detected.exitCode !== 0) throw new Error('Platform detection failed')
  const [system, machine] = detected.stdout.trim().split(/\s+/)
  const platform = { Darwin: 'darwin', Linux: 'linux' }[system]
  const arch = { x86_64: 'amd64', amd64: 'amd64', aarch64: 'arm64', arm64: 'arm64' }[machine]
  return bundledBinaryPath($.plugin.root, platform, arch)
}

async function handoff($, args) {
  const words = args.trim().split(/\s+/).filter(Boolean)
  const target = words[0] || 'codex'
  if (!['codex', 'claude'].includes(target) || words.length > 2 || (words[1] && words[1] !== '--include-code')) {
    await $.ui.log('Usage: /handoff [codex|claude] [--include-code]')
    return {}
  }
  if (switching) {
    await $.ui.log('A local handoff is already running. Wait for its result before creating another task.')
    return {}
  }
  switching = true
  try {
    // Read these for every invocation: session.start does not fire after
    // /resume or /branch, so a captured startup ID would select the wrong task.
    const id = await $.session.id()
    const cwd = await $.session.cwd()
    if (typeof id !== 'string' || !id.trim() || id === 'current') {
      await $.ui.log('The current source session is not available. No other task was selected. Retry after the session is saved, or run agent-handoff switch locally with an exact --thread ID.')
      return {}
    }
    const binary = await localBinary($)
    const argv = [binary, 'switch', '--source', 'claude', '--target', target, '--thread', id, '--cwd', cwd]
    if (words[1] === '--include-code') argv.push('--include-code')
    const completed = await $.process.run(argv, { cwd, timeoutMs: 120000 })
    let result
    try {
      result = JSON.parse(completed.stdout)
    } catch {
      await $.ui.log('The local handoff did not return a usable result. Reinstall or update the plugin so its bundled core supports switch. No model request was made.')
      return {}
    }
    if (result.status === 'blocked') {
      await $.ui.log('Handoff was blocked by the secret scan. Review the source locally before retrying; no target task was created.')
      return {}
    }
    if (result.status === 'import_failed' && typeof result.path === 'string') {
      await $.ui.log('Import failed. The usable local zip is preserved at: ' + result.path + '\nRetry with agent-handoff import after fixing the target.')
      return {}
    }
    if (completed.exitCode !== 0 || result.status !== 'ok') {
      await $.ui.log('The current source session is not available for handoff. No other task was selected. Retry after the session is saved, or inspect it with agent-handoff tasks --source claude.')
      return {}
    }
    await $.ui.log('New ' + target + ' task ready: ' + result.target_thread_id + '\nWorkspace: ' + result.target_cwd + '\nResume: ' + result.fallback_command + '\nHistory is ready; send the next message in the target agent to continue.')
    return {}
  } catch {
    await $.ui.log('The bundled local handoff could not run. Reinstall the plugin or set AGENT_HANDOFF_BIN to the absolute path of a current core. No model request was made.')
    return {}
  } finally {
    switching = false
  }
}

export function register(on) {
  on('session.start', async ($, e, next) => {
    await $.command.register({
      name: 'handoff',
      description: 'Create a local task in Codex or Claude without model usage / 无需模型额度创建交接任务',
      argumentHint: '[codex|claude] [--include-code]',
      immediate: true,
    })
    return next(e)
  })
  on('command.run', { command: 'handoff' }, async ($, e) => handoff($, e.args || ''))
}
