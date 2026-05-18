// sessionRoute.ts — pure helpers for the terminal-home hash router.
//
// We use #/s/<uuid> so navigating to the terminal doesn't touch the
// browser path (which the server gates by cookie). The session id is
// always a canonical lowercase UUID; uppercase forms are normalised.

const UUID_RE = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/
const HASH_RE = /^#\/s\/([0-9a-fA-F-]+)$/

export function parseSessionRoute(hash: string): string | null {
  const m = HASH_RE.exec(hash)
  if (!m) return null
  const uuid = m[1]!
  if (!UUID_RE.test(uuid)) return null
  return uuid.toLowerCase()
}

export function formatSessionRoute(uuid: string): string {
  return `#/s/${uuid}`
}
