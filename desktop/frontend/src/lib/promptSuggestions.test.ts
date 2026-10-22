import { describe, expect, it, vi } from 'vitest'
import { createPromptDraftTracker } from './promptDraft'
import { createPromptSuggestionController, promptSuggestionBottomOffset, type PromptSuggestionState } from './promptSuggestions'

describe('prompt suggestion positioning', () => {
  it('leaves one full row above the cursor and any TUI status rows below it', () => {
    expect(promptSuggestionBottomOffset(24, 23, 16)).toBe(44)
    expect(promptSuggestionBottomOffset(24, 21, 16)).toBe(76)
  })

  it('clamps incomplete terminal geometry to stable defaults', () => {
    expect(promptSuggestionBottomOffset(0, 99, 0)).toBe(44)
  })
})

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => { resolve = r })
  return { promise, resolve }
}

describe('prompt suggestion controller', () => {
  it('debounces trusted two-character prefixes and drops stale responses', async () => {
    vi.useFakeTimers()
    const first = deferred<string[]>()
    const second = deferred<string[]>()
    const query = vi.fn()
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise)
    let state: PromptSuggestionState = { candidates: [], selectedIndex: 0 }
    const controller = createPromptSuggestionController({ query, onChange: (next) => { state = next }, debounceMs: 50 })

    controller.update({ text: 'p', trusted: true }, true)
    await vi.advanceTimersByTimeAsync(60)
    expect(query).not.toHaveBeenCalled()

    controller.update({ text: 'pu', trusted: true }, true)
    await vi.advanceTimersByTimeAsync(50)
    controller.update({ text: 'pus', trusted: true }, true)
    await vi.advanceTimersByTimeAsync(50)
    expect(query).toHaveBeenNthCalledWith(1, 'pu', 3)
    expect(query).toHaveBeenNthCalledWith(2, 'pus', 3)

    second.resolve(['push and create mr'])
    await vi.advanceTimersByTimeAsync(0)
    expect(state.candidates).toEqual(['push and create mr'])
    first.resolve(['pull stale result'])
    await vi.advanceTimersByTimeAsync(0)
    expect(state.candidates).toEqual(['push and create mr'])

    controller.dispose()
    vi.useRealTimers()
  })

  it('cycles arrows and accepts Tab or click as suffix only', async () => {
    vi.useFakeTimers()
    let state: PromptSuggestionState = { candidates: [], selectedIndex: 0 }
    const controller = createPromptSuggestionController({
      query: async () => ['push alpha', 'push beta', 'push gamma', 'push ignored'],
      onChange: (next) => { state = next },
      debounceMs: 10,
    })
    controller.update({ text: 'push ', trusted: true }, true)
    await vi.advanceTimersByTimeAsync(10)
    await Promise.resolve()
    expect(state).toEqual({ candidates: ['push alpha', 'push beta', 'push gamma'], selectedIndex: 0 })

    expect(controller.handleKey('ArrowUp')).toEqual({ handled: true })
    expect(state.selectedIndex).toBe(2)
    expect(controller.handleKey('ArrowDown')).toEqual({ handled: true })
    expect(state.selectedIndex).toBe(0)
    expect(controller.handleKey('Tab')).toEqual({ handled: true, suffix: 'alpha' })
    expect(state.candidates).toEqual([])

    controller.update({ text: 'push ', trusted: true }, true)
    await vi.advanceTimersByTimeAsync(10)
    await Promise.resolve()
    expect(controller.accept(1)).toEqual({ handled: true, suffix: 'beta' })
    expect(state.candidates).toEqual([])
    vi.useRealTimers()
  })

  it('lets Enter through, while Esc closes until the draft changes', async () => {
    vi.useFakeTimers()
    const query = vi.fn().mockResolvedValue(['review this diff'])
    let state: PromptSuggestionState = { candidates: [], selectedIndex: 0 }
    const controller = createPromptSuggestionController({ query, onChange: (next) => { state = next }, debounceMs: 10 })
    controller.update({ text: 're', trusted: true }, true)
    await vi.advanceTimersByTimeAsync(10)
    await Promise.resolve()

    expect(controller.handleKey('Enter')).toEqual({ handled: false })
    expect(state.candidates).toEqual([])
    controller.update({ text: 're', trusted: true }, true)
    await vi.advanceTimersByTimeAsync(10)
    await Promise.resolve()
    expect(query).toHaveBeenCalledTimes(2)
    expect(controller.handleKey('Escape')).toEqual({ handled: true })
    controller.update({ text: 're', trusted: true }, true)
    await vi.advanceTimersByTimeAsync(20)
    expect(query).toHaveBeenCalledTimes(2)

    controller.update({ text: 'rev', trusted: true }, true)
    await vi.advanceTimersByTimeAsync(10)
    expect(query).toHaveBeenCalledTimes(3)
    vi.useRealTimers()
  })

  it('clears immediately when disabled or draft becomes untrusted', async () => {
    vi.useFakeTimers()
    let state: PromptSuggestionState = { candidates: [], selectedIndex: 0 }
    const controller = createPromptSuggestionController({
      query: async () => ['review this diff'],
      onChange: (next) => { state = next },
      debounceMs: 10,
    })
    controller.update({ text: 're', trusted: true }, true)
    await vi.advanceTimersByTimeAsync(10)
    await Promise.resolve()
    expect(state.candidates).toHaveLength(1)
    controller.update({ text: 're', trusted: false }, true)
    expect(state.candidates).toEqual([])
    controller.update({ text: 'review', trusted: true }, false)
    await vi.advanceTimersByTimeAsync(20)
    expect(state.candidates).toEqual([])
    vi.useRealTimers()
  })

  it('queries again after backspace edits and accepts a Chinese IME prefix', async () => {
    vi.useFakeTimers()
    const query = vi.fn(async (prefix: string) => prefix.startsWith('你')
      ? ['你好，请帮我检查项目']
      : ['hello 你可以做什么呢'])
    let state: PromptSuggestionState = { candidates: [], selectedIndex: 0 }
    const controller = createPromptSuggestionController({ query, onChange: (next) => { state = next }, debounceMs: 10 })
    const tracker = createPromptDraftTracker()

    tracker.feed('hell')
    controller.update(tracker.snapshot(), true)
    await vi.advanceTimersByTimeAsync(10)
    await Promise.resolve()
    expect(state.candidates).toEqual(['hello 你可以做什么呢'])

    tracker.feed('\x7f\x7f')
    controller.update(tracker.snapshot(), true)
    await vi.advanceTimersByTimeAsync(10)
    await Promise.resolve()
    expect(query).toHaveBeenLastCalledWith('he', 3)
    expect(state.candidates).toEqual(['hello 你可以做什么呢'])

    tracker.reset()
    tracker.feed('你好')
    controller.update(tracker.snapshot(), true)
    await vi.advanceTimersByTimeAsync(10)
    await Promise.resolve()
    expect(query).toHaveBeenLastCalledWith('你好', 3)
    expect(state.candidates).toEqual(['你好，请帮我检查项目'])
    vi.useRealTimers()
  })
})
