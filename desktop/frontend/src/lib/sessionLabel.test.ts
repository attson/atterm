import { describe, it, expect } from 'vitest'
import {
  titleOrCommand,
  commandLabel,
  fullCommand,
  rowTitle,
  hostName,
  hostNameWithIndex,
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

describe('sessionLabel.titleOrCommand', () => {
  it('returns AI title when session is ai and title is non-empty', () => {
    expect(titleOrCommand({
      session_id: 'x',
      current_command: 'claude --foo',
      title: 'Remove token auth from relay login',
      type: 'ai',
    })).toBe('Remove token auth from relay login')
  })

  it('strips codex animated cwd title prefix and keeps the cwd basename', () => {
    expect(titleOrCommand({
      session_id: 'x',
      current_command: 'codex',
      cwd: '/Users/attson/code/github.com.attson/worktrees/material-tag-front',
      title: '∷ material-tag-front',
      type: 'ai',
    })).toBe('material-tag-front')
  })

  it('falls back to commandLabel when AI session has empty title', () => {
    expect(titleOrCommand({
      session_id: 'x',
      current_command: '/usr/local/bin/claude --bar',
      title: '',
      type: 'ai',
    })).toBe('claude')
  })

  it('shows the title for non-AI sessions when it carries signal', () => {
    // Real remote shell sessions typically drive OSC 2 to the project
    // directory or a user label; the sidebar row should show that
    // instead of every zsh row collapsing to "zsh".
    expect(titleOrCommand({
      session_id: 'x',
      current_command: 'zsh',
      title: 'user@host: ~/proj',
      type: 'shell',
    })).toBe('user@host: ~/proj')
  })

  it('suppresses title == command basename to avoid duplicated "zsh"', () => {
    // Fresh shells that emit `\e]2;zsh\a` echo the executable name as
    // their title; that duplicates commandLabel and adds no info, so
    // fall through to commandLabel.
    expect(titleOrCommand({
      session_id: 'x',
      current_command: 'zsh',
      title: 'zsh',
      type: 'shell',
    })).toBe('zsh')
  })

  it('suppresses shell-path title (e.g. /usr/bin/zsh) — falls back to commandLabel basename', () => {
    // /etc/zshrc on many systems runs `echo -ne "\e]2;$SHELL\a"` on
    // startup, which lands as SessionInfo.Title = "/usr/bin/zsh". Basename
    // "zsh" matches commandLabel → row shows the clean "zsh".
    expect(titleOrCommand({
      session_id: 'x',
      current_command: '/usr/bin/zsh',
      title: '/usr/bin/zsh',
      type: 'shell',
    })).toBe('zsh')
  })

  it('suppresses shell-path title even when current_command is empty (relay stripped)', () => {
    // On mobile the sealed current_command may not have decrypted yet;
    // title alone carries "/usr/bin/zsh". commandLabel walks through title
    // to derive "zsh", and usableTitle compares against that.
    expect(titleOrCommand({
      session_id: 'abcd1234',
      current_command: '',
      title: '/bin/zsh',
      type: 'shell',
    })).toBe('zsh')
  })

  it('treats undefined type as a generic session (title still surfaces)', () => {
    expect(titleOrCommand({
      session_id: 'x',
      current_command: 'zsh',
      title: 'something',
    })).toBe('something')
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

describe('sessionLabel.hostNameWithIndex', () => {
  it('returns the base name when index is undefined', () => {
    expect(hostNameWithIndex('h-1', [{ host: 'mac' }], 'unknown', undefined))
      .toBe('mac')
  })
  it('returns the base name when index is 0 (treated as absent)', () => {
    expect(hostNameWithIndex('h-1', [{ host: 'mac' }], 'unknown', 0))
      .toBe('mac')
  })
  it('appends "#N" suffix when index >= 1', () => {
    expect(hostNameWithIndex('h-1', [{ host: 'mac' }], 'unknown', 1))
      .toBe('mac #1')
    expect(hostNameWithIndex('h-2', [{ host: 'mac' }], 'unknown', 2))
      .toBe('mac #2')
  })
  it('appends suffix even when falling back to host_id', () => {
    expect(hostNameWithIndex('h-1', [], 'unknown', 1)).toBe('h-1 #1')
  })
})
