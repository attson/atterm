import { describe, it, expect } from 'vitest'
import {
  aiTitleOrCommand,
  commandLabel,
  fullCommand,
  rowTitle,
  hostName,
  coResidentIndex,
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

describe('sessionLabel.aiTitleOrCommand', () => {
  it('returns AI title when session is ai and title is non-empty', () => {
    expect(aiTitleOrCommand({
      session_id: 'x',
      current_command: 'claude --foo',
      title: 'Remove token auth from relay login',
      type: 'ai',
    })).toBe('Remove token auth from relay login')
  })

  it('falls back to commandLabel when AI session has empty title', () => {
    expect(aiTitleOrCommand({
      session_id: 'x',
      current_command: '/usr/local/bin/claude --bar',
      title: '',
      type: 'ai',
    })).toBe('claude')
  })

  it('ignores title for non-AI sessions even when set', () => {
    expect(aiTitleOrCommand({
      session_id: 'x',
      current_command: 'zsh',
      title: 'user@host: ~/proj',
      type: 'shell',
    })).toBe('zsh')
  })

  it('treats undefined type as non-AI', () => {
    expect(aiTitleOrCommand({
      session_id: 'x',
      current_command: 'zsh',
      title: 'something',
    })).toBe('zsh')
  })
})

describe('sessionLabel.coResidentIndex', () => {
  it('returns an empty map when localHost is empty', () => {
    const byHost = { 'h-a': [{ host: 'mac' }], 'h-b': [{ host: 'mac' }] }
    expect(coResidentIndex(byHost, '').size).toBe(0)
  })
  it('returns an empty map when only one local host_id is present', () => {
    const byHost = { 'h-a': [{ host: 'mac' }], 'remote': [{ host: 'other' }] }
    const out = coResidentIndex(byHost, 'mac')
    expect(out.size).toBe(0)
  })
  it('numbers two local host_ids in lexicographic order', () => {
    const byHost = {
      'h-b': [{ host: 'mac' }],
      'h-a': [{ host: 'mac' }],
    }
    const out = coResidentIndex(byHost, 'mac')
    expect(out.get('h-a')).toBe(1)
    expect(out.get('h-b')).toBe(2)
  })
  it('numbers three local host_ids and excludes remote ones', () => {
    const byHost = {
      'h-c': [{ host: 'mac' }],
      'h-a': [{ host: 'mac' }],
      'h-b': [{ host: 'mac' }],
      'remote-1': [{ host: 'other' }],
    }
    const out = coResidentIndex(byHost, 'mac')
    expect(out.size).toBe(3)
    expect(out.get('h-a')).toBe(1)
    expect(out.get('h-b')).toBe(2)
    expect(out.get('h-c')).toBe(3)
    expect(out.has('remote-1')).toBe(false)
  })
  it('ignores entries with empty session list', () => {
    const byHost: Record<string, { host?: string }[]> = {
      'h-a': [],
      'h-b': [{ host: 'mac' }],
    }
    expect(coResidentIndex(byHost, 'mac').size).toBe(0)
  })
})
