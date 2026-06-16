// Module-level account_key provider registry. Platforms (capacitor /
// wails) register a function that returns the currently-unlocked
// account_key bytes (or null when locked / unavailable), and the
// session connection layer reads through it when it needs to decrypt
// MetaPayload.Sealed / SessionInfo.Sealed.
//
// Why a module-level singleton: connection.ts must stay platform-
// agnostic (used by both Wails desktop and Capacitor mobile bundles).
// Plumbing the key as a constructor argument would require touching
// every call site for what is functionally a process-global piece of
// state. The registry pattern keeps the integration points small.
//
// Why "provider function" not "raw bytes": the iOS SecureStorage
// plugin returns the key asynchronously and we don't want to await
// inside the WebSocket onmessage callback. Capacitor caches the key
// in memory after first login (it's already in the WebView heap when
// the user is logged in) and the provider just returns the cached
// reference synchronously.

let provider: (() => Uint8Array | null) | null = null

/** Register the provider. Called once during platform init. */
export function setAccountKeyProvider(fn: (() => Uint8Array | null) | null): void {
  provider = fn
}

/** Return the unlocked account_key, or null when no provider is
 * registered or the user is logged out. */
export function getCurrentAccountKey(): Uint8Array | null {
  if (!provider) return null
  try {
    return provider()
  } catch {
    return null
  }
}
