import type { SessionInputHistoryResult } from '@/lib/connection'

const MAX_ITEMS = 200
const historyBySession = new Map<string, SessionInputHistoryResult['items']>()

export function recordMockInputHistory(sessionID: string, text: string): void {
  const command = text.trim()
  if (!command) return
  const items = historyBySession.get(sessionID) ?? []
  items.unshift({ text: command, submitted_at_unix_ms: Date.now() })
  if (items.length > MAX_ITEMS) items.length = MAX_ITEMS
  historyBySession.set(sessionID, items)
}

export function readMockInputHistory(sessionID: string): SessionInputHistoryResult {
  return {
    items: (historyBySession.get(sessionID) ?? []).map((item) => ({ ...item })),
    truncated: false,
  }
}

export function clearMockInputHistory(sessionID: string): void {
  historyBySession.delete(sessionID)
}
