import { describe, expect, it, vi } from 'vitest'

import { useSnippets } from './useSnippets'

describe('useSnippets', () => {
  it('reloads ordered multiline snippets without changing text', async () => {
    const list = [
      { id: 'one', label: 'One', text: 'echo one\necho two' },
      { id: 'two', label: 'Two', text: 'printf "done"' },
    ]
    const state = useSnippets({
      bridge: { load: vi.fn().mockResolvedValue(list), save: vi.fn(), clear: vi.fn() },
      canSend: () => true,
      paste: vi.fn().mockReturnValue(true),
      send: vi.fn(),
      sendError: () => 'not writable',
    })

    await state.reload()

    expect(state.snippets.value).toEqual(list)
  })

  it('Paste uses only the xterm paste path and Run appends a separate Enter', () => {
    const calls: string[] = []
    const state = useSnippets({
      bridge: { load: vi.fn(), save: vi.fn(), clear: vi.fn() },
      canSend: () => true,
      paste: (text) => { calls.push(`paste:${text}`); return true },
      send: (text) => { calls.push(`send:${JSON.stringify(text)}`) },
      sendError: () => 'not writable',
    })
    const snippet = { id: 'deploy', label: 'Deploy', text: 'make build\nmake deploy' }

    expect(state.pasteSnippet(snippet)).toBe(true)
    expect(calls).toEqual(['paste:make build\nmake deploy'])

    calls.length = 0
    expect(state.runSnippet(snippet)).toBe(true)
    expect(calls).toEqual(['paste:make build\nmake deploy', 'send:"\\r"'])
  })

  it('keeps the action blocked and reports an error when the terminal is not writable', () => {
    const paste = vi.fn().mockReturnValue(true)
    const send = vi.fn()
    const state = useSnippets({
      bridge: { load: vi.fn(), save: vi.fn(), clear: vi.fn() },
      canSend: () => false,
      paste,
      send,
      sendError: () => 'not writable',
    })

    expect(state.runSnippet({ id: 'x', label: 'X', text: 'echo x' })).toBe(false)
    expect(paste).not.toHaveBeenCalled()
    expect(send).not.toHaveBeenCalled()
    expect(state.error.value).toBe('not writable')
  })

  it('reports an Enter send failure after pasting and leaves Run unsuccessful', () => {
    const paste = vi.fn().mockReturnValue(true)
    const state = useSnippets({
      bridge: { load: vi.fn(), save: vi.fn(), clear: vi.fn() },
      canSend: () => true,
      paste,
      send: () => { throw new Error('connection closed') },
      sendError: () => 'not writable',
    })

    expect(state.runSnippet({ id: 'x', label: 'X', text: 'echo x' })).toBe(false)
    expect(paste).toHaveBeenCalledWith('echo x')
    expect(state.error.value).toBe('connection closed')
  })
})
