export interface SnippetSessionTarget {
  send(text: string): void
  canSend(): boolean
}

export type SnippetSessionTargetRegistry = Map<string, SnippetSessionTarget>
