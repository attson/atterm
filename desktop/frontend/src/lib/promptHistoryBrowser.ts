import type { PromptDraftSnapshot } from './promptDraft'
import { promptCompletionSuffix } from './promptDraft'

export interface PromptHistoryPageItem {
  text: string
  last_used_at: number
}

export interface PromptHistoryPage {
  items: PromptHistoryPageItem[]
  next_cursor: string
  total: number
}

export interface PromptHistoryCandidate {
  text: string
  lastUsedAt: number
}

export interface PromptHistoryBrowserState {
  candidates: PromptHistoryCandidate[]
  selectedIndex: number
  rangeStart: number
  total: number
}

interface PromptHistoryBrowserOptions {
  queryPage(prefix: string, cursor: string, limit: number): Promise<PromptHistoryPage>
  onChange(state: PromptHistoryBrowserState): void
  debounceMs?: number
}

export interface PromptHistoryBrowser {
  update(draft: PromptDraftSnapshot, active: boolean): void
  open(draft: PromptDraftSnapshot, active: boolean): boolean
  handleKey(key: string): PromptHistoryBrowserAction
  accept(index: number): PromptHistoryBrowserAction
  dismiss(): void
  dispose(): void
}

export interface PromptHistoryBrowserAction {
  handled: boolean
  suffix?: string
}

const EMPTY_STATE: PromptHistoryBrowserState = {
  candidates: [],
  selectedIndex: 0,
  rangeStart: 0,
  total: 0,
}

export function formatPromptHistoryTime(lastUsedAt: number, nowMs: number, locale: string): string {
  const seconds = Math.max(0, Math.floor(nowMs / 1000 - lastUsedAt))
  const formatter = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' })
  if (seconds < 60) return formatter.format(0, 'second')
  if (seconds < 3600) return formatter.format(-Math.floor(seconds / 60), 'minute')
  if (seconds < 86_400) return formatter.format(-Math.floor(seconds / 3600), 'hour')
  if (seconds < 2_592_000) return formatter.format(-Math.floor(seconds / 86_400), 'day')
  return formatter.format(-Math.floor(seconds / 2_592_000), 'month')
}

export function createPromptHistoryBrowser(options: PromptHistoryBrowserOptions): PromptHistoryBrowser {
  let generation = 0
  let timer: ReturnType<typeof setTimeout> | null = null
  let currentDraft: PromptDraftSnapshot = { text: '', trusted: true }
  let prefix = ''
  let loaded: PromptHistoryCandidate[] = []
  let nextCursor = ''
  let total = 0
  let selected = 0
  let loadingMore = false
  let pendingOlderMoves = 0

  function publish(state: PromptHistoryBrowserState): void {
    options.onChange({
      ...state,
      candidates: state.candidates.map((candidate) => ({ ...candidate })),
    })
  }

  function publishView(): void {
    if (loaded.length === 0) {
      publish(EMPTY_STATE)
      return
    }
    selected = Math.min(selected, loaded.length - 1)
    const rangeStart = Math.floor(selected / 5) * 5
    publish({
      candidates: loaded.slice(rangeStart, rangeStart + 5),
      selectedIndex: selected - rangeStart,
      rangeStart,
      total,
    })
  }

  function dismiss(): void {
    generation++
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
    loaded = []
    nextCursor = ''
    total = 0
    selected = 0
    loadingMore = false
    pendingOlderMoves = 0
    publish(EMPTY_STATE)
  }

  function queryFirst(prefix: string): void {
    const requestGeneration = ++generation
    void options.queryPage(prefix, '', 5).then((page) => {
      if (requestGeneration !== generation) return
      loaded = page.items.map((item) => ({ text: item.text, lastUsedAt: item.last_used_at }))
      nextCursor = page.next_cursor
      total = page.total
      selected = 0
      publishView()
    }).catch(() => {
      if (requestGeneration === generation) publish(EMPTY_STATE)
    })
  }

  function open(draft: PromptDraftSnapshot, active: boolean): boolean {
    const nextPrefix = draft.text.replace(/^\s+/u, '')
    const prefixLength = Array.from(nextPrefix).length
    if (!active || !draft.trusted || (prefixLength > 0 && prefixLength < 2)) return false
    dismiss()
    currentDraft = { ...draft }
    prefix = nextPrefix
    queryFirst(prefix)
    return true
  }

  function update(draft: PromptDraftSnapshot, active: boolean): void {
    const nextPrefix = draft.text.replace(/^\s+/u, '')
    if (!active || !draft.trusted || Array.from(nextPrefix).length < 2) {
      dismiss()
      return
    }
    dismiss()
    currentDraft = { ...draft }
    prefix = nextPrefix
    const requestGeneration = generation
    timer = setTimeout(() => {
      timer = null
      if (requestGeneration !== generation) return
      queryFirst(prefix)
    }, options.debounceMs ?? 120)
  }

  function loadOlder(): void {
    if (loadingMore || nextCursor === '') return
    loadingMore = true
    const requestGeneration = generation
    const cursor = nextCursor
    void options.queryPage(prefix, cursor, 5).then((page) => {
      if (requestGeneration !== generation) return
      const seen = new Set(loaded.map((candidate) => candidate.text))
      for (const item of page.items) {
        if (seen.has(item.text)) continue
        seen.add(item.text)
        loaded.push({ text: item.text, lastUsedAt: item.last_used_at })
      }
      nextCursor = page.next_cursor
      total = page.total
      loadingMore = false
      const steps = Math.min(pendingOlderMoves, Math.max(0, loaded.length - 1 - selected))
      selected += steps
      pendingOlderMoves -= steps
      publishView()
      if (pendingOlderMoves > 0 && nextCursor !== '') loadOlder()
      else if (nextCursor === '') pendingOlderMoves = 0
    }).catch(() => {
      if (requestGeneration !== generation) return
      loadingMore = false
      pendingOlderMoves = 0
      publishView()
    })
  }

  function accept(index: number): PromptHistoryBrowserAction {
    const rangeStart = Math.floor(selected / 5) * 5
    const candidate = loaded[rangeStart + index]
    if (!candidate || !currentDraft.trusted) {
      dismiss()
      return { handled: false }
    }
    const suffix = promptCompletionSuffix(currentDraft.text, candidate.text)
    dismiss()
    return suffix ? { handled: true, suffix } : { handled: false }
  }

  function handleKey(key: string): PromptHistoryBrowserAction {
    if (loaded.length === 0) return { handled: false }
    if (key === 'ArrowUp') {
      if (selected + 1 < loaded.length) {
        selected++
        publishView()
      } else if (nextCursor !== '') {
        pendingOlderMoves++
        loadOlder()
      }
      return { handled: true }
    }
    if (key === 'ArrowDown') {
      if (pendingOlderMoves > 0) pendingOlderMoves--
      else if (selected > 0) {
        selected--
        publishView()
      }
      return { handled: true }
    }
    if (key === 'Tab') return accept(selected % 5)
    if (key === 'Escape') {
      dismiss()
      return { handled: true }
    }
    if (key === 'Enter') {
      dismiss()
      return { handled: false }
    }
    return { handled: false }
  }

  return { update, open, handleKey, accept, dismiss, dispose: dismiss }
}
