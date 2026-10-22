import { describe, expect, it } from 'vitest'
import { createPromptDraftTracker, isCodexOrClaudeCommand, isPromptDraftInputData, promptCompletionSuffix } from './promptDraft'

describe('prompt draft tracker', () => {
  it('tracks text, Unicode backspace, bracketed paste, clear line, and delete word', () => {
    const tracker = createPromptDraftTracker()
    tracker.feed('  push并')
    expect(tracker.snapshot()).toEqual({ text: '  push并', trusted: true })

    tracker.feed('\x7f')
    expect(tracker.snapshot().text).toBe('  push')
    tracker.feed('\x1b[200~ and create mr\x1b[201~')
    expect(tracker.snapshot().text).toBe('  push and create mr')
    tracker.feed('\x17')
    expect(tracker.snapshot().text).toBe('  push and create ')
    tracker.feed('\x15')
    expect(tracker.snapshot()).toEqual({ text: '', trusted: true })
  })

  it('becomes untrusted on cursor, history, tab, or unknown controls until Enter', () => {
    const tracker = createPromptDraftTracker()
    tracker.feed('review')
    tracker.feed('\x1b[D')
    expect(tracker.snapshot()).toEqual({ text: 'review', trusted: false })
    tracker.feed(' ignored while uncertain')
    expect(tracker.snapshot().trusted).toBe(false)
    tracker.feed('\r')
    expect(tracker.snapshot()).toEqual({ text: '', trusted: true })
    tracker.feed('next')
    expect(tracker.snapshot()).toEqual({ text: 'next', trusted: true })

    tracker.feed('\t')
    expect(tracker.snapshot().trusted).toBe(false)
  })

  it('resets after submitted input and rejects malformed bracketed paste', () => {
    const tracker = createPromptDraftTracker()
    tracker.feed('first\rsecond')
    expect(tracker.snapshot()).toEqual({ text: 'second', trusted: true })
    tracker.feed('\x1b[200~unfinished')
    expect(tracker.snapshot().trusted).toBe(false)
  })

  it('tracks cursor edits and becomes completable again at end of line', () => {
    const tracker = createPromptDraftTracker()
    tracker.feed('hello')
    tracker.feed('\x1b[D\x1b[D')
    expect(tracker.snapshot()).toEqual({ text: 'hello', trusted: false })
    tracker.feed('\x7f')
    expect(tracker.snapshot()).toEqual({ text: 'helo', trusted: false })
    tracker.feed('\x1b[F')
    expect(tracker.snapshot()).toEqual({ text: 'helo', trusted: true })
    tracker.feed('\x1b[H\x1b[3~\x1b[F')
    expect(tracker.snapshot()).toEqual({ text: 'elo', trusted: true })
  })

  it('recognizes IME text and editing data but rejects terminal replies', () => {
    expect(isPromptDraftInputData('你好')).toBe(true)
    expect(isPromptDraftInputData('\x7f')).toBe(true)
    expect(isPromptDraftInputData('\x1b[D')).toBe(true)
    expect(isPromptDraftInputData('\x1b[200~你好\x1b[201~')).toBe(true)
    expect(isPromptDraftInputData('\x1b[?1;2c')).toBe(false)
  })
})

describe('prompt suggestion helpers', () => {
  it('only recognizes Codex and Claude launch commands through common wrappers', () => {
    expect(isCodexOrClaudeCommand('codex --full-auto')).toBe(true)
    expect(isCodexOrClaudeCommand('env FOO=1 sudo /opt/bin/claude --permission-mode plan')).toBe(true)
    expect(isCodexOrClaudeCommand('aider')).toBe(false)
    expect(isCodexOrClaudeCommand('echo codex')).toBe(false)
  })

  it('computes a suffix without adding Enter and tolerates ASCII case differences', () => {
    expect(promptCompletionSuffix('  PuSh并', 'push并创建mr到test')).toBe('创建mr到test')
    expect(promptCompletionSuffix('push并创建mr到test', 'push并创建mr到test')).toBe('')
    expect(promptCompletionSuffix('other', 'push并创建mr到test')).toBeNull()
  })
})
