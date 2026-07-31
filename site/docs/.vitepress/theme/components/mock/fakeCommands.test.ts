import { describe, it, expect } from 'vitest'
import { runFakeCommand } from './fakeCommands'

describe('runFakeCommand', () => {
  it('returns output for known commands', () => {
    expect(runFakeCommand('pwd').output).toContain('~/srv/atterm')
    expect(runFakeCommand('whoami').output).toContain('you')
    expect(runFakeCommand('ls').output).toContain('README.md')
    expect(runFakeCommand('echo hi').output).toBe('hi\r\n')
    expect(runFakeCommand('help').output.length).toBeGreaterThan(0)
  })

  it('reports not-found for unknown commands', () => {
    const r = runFakeCommand('frobnicate')
    expect(r.output).toContain('command not found')
    expect(r.longRunning).toBeFalsy()
  })

  it('flags a long-running AI command that drives task state', () => {
    const r = runFakeCommand('codex exec "add feature"')
    expect(r.longRunning).toBe(true)
    expect(r.finalState).toBe('completed')
    expect(r.steps?.length ?? 0).toBeGreaterThan(0)
  })

  it('ignores empty input', () => {
    expect(runFakeCommand('   ').output).toBe('')
  })
})
