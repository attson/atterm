export function isLoopbackHost(host: string): boolean {
  const h = host.toLowerCase()
  if (h === 'localhost' || h === '127.0.0.1' || h === '::1') return true
  if (h.startsWith('127.')) return true
  if (h.startsWith('[') && h.endsWith(']') && h.slice(1, -1) === '::1') return true
  return false
}

export function validateRelayBase(base: string, allowInsecure: boolean): string | null {
  const trimmed = base.trim()
  if (!trimmed) return 'relay URL is required'
  let u: URL
  try {
    u = new URL(trimmed)
  } catch {
    return 'invalid or malformed relay URL'
  }
  if (u.protocol !== 'http:' && u.protocol !== 'https:') {
    return 'relay URL must start with http:// or https://'
  }
  if (u.pathname !== '/' || u.search !== '' || u.hash !== '') {
    return 'relay URL must not contain a path, query, or fragment'
  }
  if (u.protocol === 'http:' && !isLoopbackHost(u.hostname) && !allowInsecure) {
    return 'enable "Allow insecure HTTP/WS" to use http:// with a non-loopback host'
  }
  return null
}

// Convert an http(s) relay base to the ws(s) base SessionConnection expects
// (no trailing slash, no path — SessionConnection appends /client itself).
export function relayBaseToWsUrl(httpBase: string): string {
  const u = new URL(httpBase)
  const proto = u.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${u.host}`
}
