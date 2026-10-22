import type { PromptDraftSnapshot } from './promptDraft'
import { promptCompletionSuffix } from './promptDraft'

export interface PromptSuggestionState {
  candidates: string[]
  selectedIndex: number
}

export interface PromptSuggestionAction {
  handled: boolean
  suffix?: string
}

export interface PromptSuggestionController {
  update(draft: PromptDraftSnapshot, active: boolean): void
  handleKey(key: string): PromptSuggestionAction
  accept(index: number): PromptSuggestionAction
  dismiss(): void
  dispose(): void
}

export function promptSuggestionBottomOffset(rows: number, cursorY: number, cellHeight: number): number {
  const safeRows = Math.max(1, Math.floor(rows))
  const safeCursorY = Math.min(safeRows - 1, Math.max(0, Math.floor(cursorY)))
  const safeCellHeight = Number.isFinite(cellHeight) && cellHeight > 0 ? cellHeight : 16
  // Keep one complete terminal row between the popup and the cursor row.
  // Merely aligning the popup edge with the row boundary still covers the
  // glyph ascenders in xterm because the popup border and line box straddle
  // that boundary, especially in Codex's compact editor/status layout.
  return Math.ceil(12 + (safeRows - safeCursorY + 1) * safeCellHeight)
}

interface PromptSuggestionOptions {
  query(prefix: string, limit: number): Promise<string[]>
  onChange(state: PromptSuggestionState): void
  debounceMs?: number
}

const EMPTY_STATE: PromptSuggestionState = { candidates: [], selectedIndex: 0 }

export function createPromptSuggestionController(options: PromptSuggestionOptions): PromptSuggestionController {
  const debounceMs = options.debounceMs ?? 120
  let state: PromptSuggestionState = EMPTY_STATE
  let draft: PromptDraftSnapshot = { text: '', trusted: true }
  let active = false
  let generation = 0
  let timer: ReturnType<typeof setTimeout> | null = null
  let dismissedText: string | null = null

  function publish(next: PromptSuggestionState): void {
    state = { candidates: [...next.candidates], selectedIndex: next.selectedIndex }
    options.onChange(state)
  }

  function cancelPending(): void {
    generation++
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
  }

  function clear(): void {
    cancelPending()
    if (state.candidates.length > 0 || state.selectedIndex !== 0) publish(EMPTY_STATE)
  }

  function normalizedPrefix(): string {
    return draft.text.replace(/^\s+/u, '')
  }

  function update(nextDraft: PromptDraftSnapshot, nextActive: boolean): void {
    const changed = nextDraft.text !== draft.text || nextDraft.trusted !== draft.trusted
    draft = nextDraft
    active = nextActive
    if (changed && dismissedText !== null && draft.text !== dismissedText) dismissedText = null
    clear()
    const prefix = normalizedPrefix()
    if (!active || !draft.trusted || Array.from(prefix).length < 2 || dismissedText === draft.text) return

    const requestGeneration = generation
    timer = setTimeout(async () => {
      timer = null
      try {
        const result = await options.query(prefix, 3)
        if (requestGeneration !== generation || !active || !draft.trusted || normalizedPrefix() !== prefix) return
        const candidates = (Array.isArray(result) ? result : [])
          .filter((candidate) => {
            const suffix = promptCompletionSuffix(draft.text, candidate)
            return suffix !== null && suffix.length > 0
          })
          .slice(0, 3)
        publish({ candidates, selectedIndex: 0 })
      } catch {
        if (requestGeneration === generation) publish(EMPTY_STATE)
      }
    }, debounceMs)
  }

  function accept(index: number): PromptSuggestionAction {
    const candidate = state.candidates[index]
    if (!candidate || !active || !draft.trusted) {
      clear()
      return { handled: false }
    }
    const suffix = promptCompletionSuffix(draft.text, candidate)
    clear()
    if (!suffix) return { handled: false }
    return { handled: true, suffix }
  }

  function dismiss(): void {
    dismissedText = draft.text
    clear()
  }

  function handleKey(key: string): PromptSuggestionAction {
    if (state.candidates.length === 0) return { handled: false }
    if (key === 'ArrowDown' || key === 'ArrowUp') {
      const delta = key === 'ArrowDown' ? 1 : -1
      const selectedIndex = (state.selectedIndex + delta + state.candidates.length) % state.candidates.length
      publish({ candidates: state.candidates, selectedIndex })
      return { handled: true }
    }
    if (key === 'Tab') return accept(state.selectedIndex)
    if (key === 'Escape') {
      dismiss()
      return { handled: true }
    }
    if (key === 'Enter') {
      clear()
      return { handled: false }
    }
    return { handled: false }
  }

  return {
    update,
    handleKey,
    accept,
    dismiss,
    dispose: clear,
  }
}
