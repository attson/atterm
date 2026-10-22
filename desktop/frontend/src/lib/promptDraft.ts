export interface PromptDraftSnapshot {
  text: string
  trusted: boolean
}

export interface PromptDraftTracker {
  feed(data: string): PromptDraftSnapshot
  reset(): PromptDraftSnapshot
  snapshot(): PromptDraftSnapshot
}

const BRACKETED_PASTE_START = '\x1b[200~'
const BRACKETED_PASTE_END = '\x1b[201~'
const EDIT_SEQUENCES = [
  ['\x1b[D', 'left'],
  ['\x1b[C', 'right'],
  ['\x1b[H', 'home'],
  ['\x1b[1~', 'home'],
  ['\x1b[F', 'end'],
  ['\x1b[4~', 'end'],
  ['\x1b[3~', 'delete'],
] as const

type EditAction = typeof EDIT_SEQUENCES[number][1]

export function createPromptDraftTracker(): PromptDraftTracker {
  let chars: string[] = []
  let cursor = 0
  let trusted = true

  const snapshot = (): PromptDraftSnapshot => ({
    text: chars.join(''),
    // Completion suffixes are only safe at the end of the editable line.
    trusted: trusted && cursor === chars.length,
  })
  const reset = (): PromptDraftSnapshot => {
    chars = []
    cursor = 0
    trusted = true
    return snapshot()
  }

  const insert = (value: string) => {
    const inserted = Array.from(value)
    chars.splice(cursor, 0, ...inserted)
    cursor += inserted.length
  }

  const applyEdit = (action: EditAction) => {
    if (action === 'left') cursor = Math.max(0, cursor - 1)
    else if (action === 'right') cursor = Math.min(chars.length, cursor + 1)
    else if (action === 'home') cursor = 0
    else if (action === 'end') cursor = chars.length
    else if (action === 'delete' && cursor < chars.length) chars.splice(cursor, 1)
  }

  function feed(data: string): PromptDraftSnapshot {
    if (!data) return snapshot()
    if (!trusted) {
      const lastEnter = Math.max(data.lastIndexOf('\r'), data.lastIndexOf('\n'))
      if (lastEnter < 0) return snapshot()
      reset()
      data = data.slice(lastEnter + 1)
    }

    if (data.startsWith(BRACKETED_PASTE_START)) {
      if (!data.endsWith(BRACKETED_PASTE_END)) {
        trusted = false
        return snapshot()
      }
      insert(data.slice(BRACKETED_PASTE_START.length, -BRACKETED_PASTE_END.length))
      return snapshot()
    }

    for (let offset = 0; offset < data.length;) {
      const edit = EDIT_SEQUENCES.find(([sequence]) => data.startsWith(sequence, offset))
      if (edit) {
        applyEdit(edit[1])
        offset += edit[0].length
        continue
      }
      const char = Array.from(data.slice(offset))[0]
      offset += char.length
      if (char === '\r' || char === '\n') {
        reset()
        continue
      }
      if (char === '\x7f' || char === '\b') {
        if (cursor > 0) {
          chars.splice(cursor - 1, 1)
          cursor--
        }
        continue
      }
      if (char === '\x15') {
        chars.splice(0, cursor)
        cursor = 0
        continue
      }
      if (char === '\x17') {
        cursor = deletePreviousWord(chars, cursor)
        continue
      }
      const code = char.codePointAt(0) ?? 0
      if (code < 0x20 || code === 0x7f) {
        trusted = false
        return snapshot()
      }
      insert(char)
    }
    return snapshot()
  }

  return { feed, reset, snapshot }
}

function deletePreviousWord(chars: string[], cursor: number): number {
  const end = cursor
  while (cursor > 0 && /\s/u.test(chars[cursor - 1])) cursor--
  while (cursor > 0 && !/\s/u.test(chars[cursor - 1])) cursor--
  chars.splice(cursor, end - cursor)
  return cursor
}

// xterm emits IME commits without a preceding keydown, while its automatic
// terminal replies are escape sequences. Recognizing only editable prompt
// data lets the replay guard admit Chinese commits and edits without treating
// device-status/cursor reports as human input.
export function isPromptDraftInputData(data: string): boolean {
  if (!data) return false
  if (data.startsWith(BRACKETED_PASTE_START)) return data.endsWith(BRACKETED_PASTE_END)
  for (let offset = 0; offset < data.length;) {
    const edit = EDIT_SEQUENCES.find(([sequence]) => data.startsWith(sequence, offset))
    if (edit) {
      offset += edit[0].length
      continue
    }
    const char = Array.from(data.slice(offset))[0]
    offset += char.length
    const code = char.codePointAt(0) ?? 0
    if (code >= 0x20 && code !== 0x7f) continue
    if (char === '\r' || char === '\n' || char === '\x7f' || char === '\b' || char === '\x15' || char === '\x17' || char === '\t') continue
    return false
  }
  return true
}

function asciiFold(value: string): string {
  return value.replace(/[A-Z]/g, (char) => char.toLowerCase())
}

export function promptCompletionSuffix(draft: string, candidate: string): string | null {
  const prefix = draft.replace(/^\s+/u, '')
  if (!asciiFold(candidate).startsWith(asciiFold(prefix))) return null
  return candidate.slice(prefix.length)
}

export function isCodexOrClaudeCommand(command: string): boolean {
  const tokens = command.trim().split(/\s+/).filter(Boolean)
  while (tokens.length > 0) {
    const token = tokens[0]
    if (['sudo', 'time', 'nice', 'env'].includes(token) || /^[A-Z_][A-Z0-9_]*=/.test(token)) {
      tokens.shift()
      continue
    }
    break
  }
  const executable = (tokens[0] ?? '').split(/[\\/]/).pop()
  return executable === 'codex' || executable === 'claude'
}
