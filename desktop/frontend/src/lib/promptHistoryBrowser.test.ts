import { describe, expect, it, vi } from 'vitest'
import { createPromptHistoryBrowser, formatPromptHistoryTime, type PromptHistoryBrowserState } from './promptHistoryBrowser'

describe('prompt history browser', () => {
  it('formats timestamps as localized relative time', () => {
    const now = Date.UTC(2026, 9, 24, 10, 0, 0)
    expect(formatPromptHistoryTime(now / 1000, now, 'zh-CN')).toBe('现在')
    expect(formatPromptHistoryTime((now - 5 * 60_000) / 1000, now, 'zh-CN')).toBe('5分钟前')
    expect(formatPromptHistoryTime((now - 2 * 86_400_000) / 1000, now, 'en')).toBe('2 days ago')
  })

  it('opens an empty trusted draft with five timestamped recent entries', async () => {
    const queryPage = vi.fn().mockResolvedValue({
      items: [
        { text: 'first', last_used_at: 500 },
        { text: 'second', last_used_at: 400 },
        { text: 'third', last_used_at: 300 },
        { text: 'fourth', last_used_at: 200 },
        { text: 'fifth', last_used_at: 100 },
      ],
      next_cursor: 'older',
      total: 7,
    })
    let state: PromptHistoryBrowserState = {
      candidates: [],
      selectedIndex: 0,
      rangeStart: 0,
      total: 0,
    }
    const browser = createPromptHistoryBrowser({
      queryPage,
      onChange: (next) => { state = next },
    })

    expect(browser.open({ text: '', trusted: true }, true)).toBe(true)
    await vi.waitFor(() => expect(state.candidates).toHaveLength(5))

    expect(queryPage).toHaveBeenCalledWith('', '', 5)
    expect(state).toEqual({
      candidates: [
        { text: 'first', lastUsedAt: 500 },
        { text: 'second', lastUsedAt: 400 },
        { text: 'third', lastUsedAt: 300 },
        { text: 'fourth', lastUsedAt: 200 },
        { text: 'fifth', lastUsedAt: 100 },
      ],
      selectedIndex: 0,
      rangeStart: 0,
      total: 7,
    })
  })

  it('rejects unsafe and one-character shortcut drafts but accepts a two-character prefix as a suffix', async () => {
    const queryPage = vi.fn().mockResolvedValue({
      items: [{ text: 'publish latest', last_used_at: 500 }],
      next_cursor: '',
      total: 1,
    })
    let state = emptyState()
    const browser = createPromptHistoryBrowser({ queryPage, onChange: (next) => { state = next } })

    expect(browser.open({ text: 'p', trusted: true }, true)).toBe(false)
    expect(browser.open({ text: '', trusted: false }, true)).toBe(false)
    expect(queryPage).not.toHaveBeenCalled()

    expect(browser.open({ text: 'pu', trusted: true }, true)).toBe(true)
    await vi.waitFor(() => expect(state.candidates).toHaveLength(1))
    expect(browser.handleKey('Tab')).toEqual({ handled: true, suffix: 'blish latest' })
  })

  it('debounces automatic prefix queries and ignores stale pages', async () => {
    vi.useFakeTimers()
    let resolveFirst!: (result: ReturnType<typeof page>) => void
    const first = new Promise<ReturnType<typeof page>>((resolve) => { resolveFirst = resolve })
    const queryPage = vi.fn()
      .mockReturnValueOnce(first)
      .mockResolvedValueOnce(page('publish latest', 900))
    let state = emptyState()
    const browser = createPromptHistoryBrowser({
      queryPage,
      onChange: (next) => { state = next },
      debounceMs: 20,
    })

    browser.update({ text: 'pu', trusted: true }, true)
    await vi.advanceTimersByTimeAsync(20)
    browser.update({ text: 'pub', trusted: true }, true)
    await vi.advanceTimersByTimeAsync(20)
    await vi.advanceTimersByTimeAsync(0)
    expect(queryPage).toHaveBeenNthCalledWith(1, 'pu', '', 5)
    expect(queryPage).toHaveBeenNthCalledWith(2, 'pub', '', 5)
    expect(state.candidates.map((item) => item.text)).toEqual(['publish latest'])

    resolveFirst(page('pull stale', 100))
    await vi.advanceTimersByTimeAsync(0)
    expect(state.candidates.map((item) => item.text)).toEqual(['publish latest'])
    browser.dispose()
    vi.useRealTimers()
  })

  it('moves toward older records across five-row pages without wrapping', async () => {
    const queryPage = vi.fn()
      .mockResolvedValueOnce({
        items: ['one', 'two', 'three', 'four', 'five'].map((text, index) => ({ text, last_used_at: 500 - index })),
        next_cursor: 'older',
        total: 7,
      })
      .mockResolvedValueOnce({
        items: [
          { text: 'six', last_used_at: 100 },
          { text: 'seven', last_used_at: 99 },
        ],
        next_cursor: '',
        total: 7,
      })
    let state = emptyState()
    const browser = createPromptHistoryBrowser({ queryPage, onChange: (next) => { state = next } })
    browser.open({ text: '', trusted: true }, true)
    await vi.waitFor(() => expect(state.candidates).toHaveLength(5))

    expect(browser.handleKey('ArrowDown')).toEqual({ handled: true })
    for (let index = 0; index < 5; index++) browser.handleKey('ArrowUp')
    await vi.waitFor(() => expect(state.rangeStart).toBe(5))
    expect(queryPage).toHaveBeenLastCalledWith('', 'older', 5)
    expect(state.candidates.map((item) => item.text)).toEqual(['six', 'seven'])
    expect(state.selectedIndex).toBe(0)

    browser.handleKey('ArrowUp')
    browser.handleKey('ArrowUp')
    expect(state.selectedIndex).toBe(1)
    browser.handleKey('ArrowDown')
    browser.handleKey('ArrowDown')
    expect(state.rangeStart).toBe(0)
    expect(state.selectedIndex).toBe(4)
    expect(browser.handleKey('Tab')).toEqual({ handled: true, suffix: 'five' })
    expect(state.candidates).toEqual([])
  })
})

function emptyState(): PromptHistoryBrowserState {
  return { candidates: [], selectedIndex: 0, rangeStart: 0, total: 0 }
}

function page(text: string, lastUsedAt: number) {
  return {
    items: [{ text, last_used_at: lastUsedAt }],
    next_cursor: '',
    total: 1,
  }
}
