import { describe, it, expect } from 'vitest'
import {
  commandLabel,
  fullCommand,
  rowTitle,
  hostName,
  taskStateLabel,
} from './sessionLabel'

describe('sessionLabel.fullCommand', () => {
  it('prefers current_command over title', () => {
    expect(fullCommand({ current_command: 'claude --foo', title: 'fallback', session_id: 'abcd1234' }))
      .toBe('claude --foo')
  })
  it('falls back to title when current_command is empty', () => {
    expect(fullCommand({ current_command: '', title: 'zsh', session_id: 'abcd1234' })).toBe('zsh')
  })
  it('falls back to the first 8 chars of session_id when both are empty', () => {
    expect(fullCommand({ session_id: 'abcd12345678' })).toBe('abcd1234')
  })
})

describe('sessionLabel.commandLabel', () => {
  it('strips arguments and path prefix', () => {
    expect(commandLabel({ current_command: '/usr/local/bin/claude --permission-mode bypassPermissions', session_id: 'x' }))
      .toBe('claude')
  })
  it('returns the bare command when there is no slash and no arg', () => {
    expect(commandLabel({ current_command: 'codex', session_id: 'x' })).toBe('codex')
  })
})

describe('sessionLabel.rowTitle', () => {
  it('appends cwd on its own line when present', () => {
    expect(rowTitle({ current_command: 'ls', cwd: '/tmp', session_id: 'x' })).toBe('ls\n/tmp')
  })
  it('returns just the command when cwd is missing', () => {
    expect(rowTitle({ current_command: 'ls', session_id: 'x' })).toBe('ls')
  })
})

describe('sessionLabel.hostName', () => {
  it('returns the first session\'s host when present', () => {
    expect(hostName('h-1', [{ host: 'macbook' }], 'unknown')).toBe('macbook')
  })
  it('falls back to hostId when the list is empty', () => {
    expect(hostName('h-1', [], 'unknown')).toBe('h-1')
  })
  it('falls back to the provided unknown label when hostId is empty', () => {
    expect(hostName('', undefined, 'unknown host')).toBe('unknown host')
  })
})

describe('sessionLabel.taskStateLabel', () => {
  const fakeT = (k: string) => k  // identity-of-key: easiest to assert
  it('maps each TaskState to its mobile.taskStates.* key', () => {
    expect(taskStateLabel('running', fakeT as any)).toBe('mobile.taskStates.running')
    expect(taskStateLabel('waiting_input', fakeT as any)).toBe('mobile.taskStates.waiting_input')
    expect(taskStateLabel('completed', fakeT as any)).toBe('mobile.taskStates.completed')
    expect(taskStateLabel('failed', fakeT as any)).toBe('mobile.taskStates.failed')
    expect(taskStateLabel('disconnected', fakeT as any)).toBe('mobile.taskStates.disconnected')
    expect(taskStateLabel('closed', fakeT as any)).toBe('mobile.taskStates.closed')
  })
  it('falls back to mobile.taskStates.idle for unknown / undefined', () => {
    expect(taskStateLabel(undefined, fakeT as any)).toBe('mobile.taskStates.idle')
    expect(taskStateLabel('made_up_state', fakeT as any)).toBe('mobile.taskStates.idle')
    expect(taskStateLabel('idle', fakeT as any)).toBe('mobile.taskStates.idle')
  })
})
