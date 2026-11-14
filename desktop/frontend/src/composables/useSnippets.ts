import { ref, type Ref } from 'vue'

import type { Snippet } from '../lib/templates'
import type { SnippetBridge } from '../platform/types'

export interface UseSnippetsOptions {
  bridge: SnippetBridge
  canSend: () => boolean
  paste: (text: string) => boolean
  send: (text: string) => void
  sendError: () => string
}

export interface UseSnippets {
  snippets: Ref<readonly Snippet[]>
  error: Ref<string>
  reload(): Promise<void>
  pasteSnippet(snippet: Snippet): boolean
  runSnippet(snippet: Snippet): boolean
}

export function useSnippets(options: UseSnippetsOptions): UseSnippets {
  const snippets = ref<readonly Snippet[]>([])
  const error = ref('')

  async function reload(): Promise<void> {
    snippets.value = await options.bridge.load()
  }

  function pasteSnippet(snippet: Snippet): boolean {
    error.value = ''
    if (!options.canSend()) {
      error.value = options.sendError()
      return false
    }
    try {
      if (!options.paste(snippet.text)) {
        error.value = options.sendError()
        return false
      }
      return true
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : String(cause)
      return false
    }
  }

  function runSnippet(snippet: Snippet): boolean {
    if (!pasteSnippet(snippet)) return false
    try {
      options.send('\r')
      return true
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : String(cause)
      return false
    }
  }

  return { snippets, error, reload, pasteSnippet, runSnippet }
}
