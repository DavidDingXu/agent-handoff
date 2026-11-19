import { expect, test, mock } from 'claude-code/testing'
import { bundledBinaryPath } from '../hooks/register.js'

test('keeps native package paths and file URLs literal across macOS and Windows', () => {
  expect(bundledBinaryPath('/Users/Test User/插件/', 'darwin', 'arm64')).toBe('/Users/Test User/插件/bin/darwin-arm64/agent-handoff')
  expect(bundledBinaryPath('file:///Users/Test%20User/%E6%8F%92%E4%BB%B6', 'darwin', 'amd64')).toBe('/Users/Test User/插件/bin/darwin-amd64/agent-handoff')
  expect(bundledBinaryPath('C:\\Users\\Test User\\插件', 'windows', 'amd64')).toBe('C:/Users/Test User/插件/bin/windows-amd64/agent-handoff.exe')
  expect(bundledBinaryPath('file:///C:/Users/Test%20User/%E6%8F%92%E4%BB%B6', 'windows', 'amd64')).toBe('C:/Users/Test User/插件/bin/windows-amd64/agent-handoff.exe')
  expect(bundledBinaryPath('file://server/shared%20folder/plugin', 'windows', 'amd64')).toBe('//server/shared folder/plugin/bin/windows-amd64/agent-handoff.exe')
})

test('registers an immediate local command before the first prompt', async ($, on) => {
  let registered
  on('session.start', () => ({ cwd: '/synthetic/project' }))
  on('command.register', (_, e) => {
    registered = e
    return { value: undefined }
  })
  await $.session.start({ surface: 'terminal', isInteractive: true, cwd: '/synthetic/project' })
  expect(registered?.name).toBe('handoff')
  expect(registered?.immediate).toBe(true)
})

test('binds every switch to the current native session without a model turn', async ($, on) => {
  const calls = []
  const logs = []
  let nextID = 0
  let modelCalls = 0
  let submittedPrompts = 0
  mock.env(on, { AGENT_HANDOFF_BIN: '/synthetic/bin/agent-handoff' })
  on('session.id', () => ({ value: ['session-first', 'session-after-resume'][nextID++] }))
  on('session.cwd', () => ({ value: '/synthetic/project' }))
  on('model.complete', () => { modelCalls++; return { deny: 'No model call is permitted.' } })
  on('prompt.submit', () => { submittedPrompts++; return { text: '' } })
  on('process.run', (_, e) => {
    calls.push(e.argv)
    return { value: { exitCode: 0, stdout: JSON.stringify({ status: 'ok', target_agent: 'codex', target_thread_id: 'new-task', fallback_command: 'codex resume new-task', target_cwd: '/synthetic/project' }), stderr: '' } }
  })
  on('ui.log', (_, e) => { logs.push(e.text); return { value: undefined } })
  const first = await $.command.run({ command: 'handoff', args: 'codex' })
  await $.command.run({ command: 'handoff', args: 'codex --include-code' })
  expect(calls).toEqual([
    ['/synthetic/bin/agent-handoff', 'switch', '--source', 'claude', '--target', 'codex', '--thread', 'session-first', '--cwd', '/synthetic/project'],
    ['/synthetic/bin/agent-handoff', 'switch', '--source', 'claude', '--target', 'codex', '--thread', 'session-after-resume', '--cwd', '/synthetic/project', '--include-code'],
  ])
  expect(first).toEqual({})
  expect(logs[0]).toContain('codex resume new-task')
  expect(modelCalls).toBe(0)
  expect(submittedPrompts).toBe(0)
})

test('a missing persisted source session is reported without selecting another task', async ($, on) => {
  const logs = []
  mock.env(on, {})
  on('session.id', () => ({ value: 'not-persisted-yet' }))
  on('session.cwd', () => ({ value: '/synthetic/project' }))
  on('process.run', (_, e) => {
    if (e.argv[0] === 'uname') return { value: { exitCode: 0, stdout: 'Darwin arm64\n', stderr: '' } }
    return { value: { exitCode: 1, stdout: JSON.stringify({ status: 'error', error_code: 'export_failed' }), stderr: 'source session not found' } }
  })
  on('ui.log', (_, e) => { logs.push(e.text); return { value: undefined } })
  const answer = await $.command.run({ command: 'handoff', args: 'codex' })
  expect(answer).toEqual({})
  expect(logs[0]).toContain('not available')
  expect(logs[0]).toContain('No other task was selected')
})

test('invalid command arguments never reach a local process', async ($, on) => {
  let processes = 0
  const logs = []
  on('process.run', () => { processes++; return { deny: 'Unexpected process.' } })
  on('ui.log', (_, e) => { logs.push(e.text); return { value: undefined } })
  await $.command.run({ command: 'handoff', args: 'codex; echo unsafe' })
  expect(processes).toBe(0)
  expect(logs[0]).toContain('/handoff')
})

test('uses the bundled Windows executable without a PATH install', async ($, on) => {
  const calls = []
  mock.env(on, { OS: 'Windows_NT' })
  on('session.id', () => ({ value: 'source-session' }))
  on('session.cwd', () => ({ value: 'C:\\synthetic project' }))
  on('process.run', (_, e) => {
    calls.push(e.argv)
    return { value: { exitCode: 0, stdout: JSON.stringify({ status: 'ok', target_thread_id: 'new-task', fallback_command: 'codex resume new-task' }), stderr: '' } }
  })
  on('ui.log', () => ({ value: undefined }))
  await $.command.run({ command: 'handoff', args: 'codex' })
  expect(calls).toHaveLength(1)
  expect(calls[0][0]).toMatch(/\/bin\/windows-amd64\/agent-handoff\.exe$/)
})

test('detects the bundled macOS architecture without a shell', async ($, on) => {
  const calls = []
  mock.env(on, {})
  on('session.id', () => ({ value: 'source-session' }))
  on('session.cwd', () => ({ value: '/synthetic/project' }))
  on('process.run', (_, e) => {
    calls.push(e.argv)
    if (e.argv[0] === 'uname') return { value: { exitCode: 0, stdout: 'Darwin arm64\n', stderr: '' } }
    return { value: { exitCode: 0, stdout: JSON.stringify({ status: 'ok', target_thread_id: 'new-task', fallback_command: 'codex resume new-task' }), stderr: '' } }
  })
  on('ui.log', () => ({ value: undefined }))
  await $.command.run({ command: 'handoff', args: 'codex' })
  expect(calls[0]).toEqual(['uname', '-s', '-m'])
  expect(calls[1][0]).toMatch(/\/bin\/darwin-arm64\/agent-handoff$/)
})

test('retains an import-failure zip and hides secret-scan evidence from command output', async ($, on) => {
  const logs = []
  let invocation = 0
  mock.env(on, { AGENT_HANDOFF_BIN: '/synthetic/bin/agent-handoff' })
  on('session.id', () => ({ value: 'source-session' }))
  on('session.cwd', () => ({ value: '/synthetic/project' }))
  on('process.run', () => {
    const result = invocation++ === 0
      ? { status: 'import_failed', path: '/synthetic/local/task.agent-handoff.zip' }
      : { status: 'blocked', safety: { findings: [{ hint: 'SYNTHETIC-PRIVATE-MATERIAL' }] } }
    return { value: { exitCode: 1, stdout: JSON.stringify(result), stderr: '' } }
  })
  on('ui.log', (_, e) => { logs.push(e.text); return { value: undefined } })
  await $.command.run({ command: 'handoff', args: 'codex' })
  await $.command.run({ command: 'handoff', args: 'codex' })
  expect(logs[0]).toContain('/synthetic/local/task.agent-handoff.zip')
  expect(logs[1]).toContain('blocked by the secret scan')
  expect(logs[1]).not.toContain('SYNTHETIC-PRIVATE-MATERIAL')
})
