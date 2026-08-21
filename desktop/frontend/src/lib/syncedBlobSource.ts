// Reads a prefs-sync value straight out of the localStorage cache that
// lib/prefsSync.capacitor.ts's localStorageAdapter already maintains
// (VALUE_KEY(k) = `atterm.${k}.value`, JSON-stringified) — the same store
// PrefsSyncEngine.pull() writes into for EVERY key the relay returns,
// including profiles_encrypted and ssh_hosts_encrypted, which are not in
// SYNCED_KEYS and therefore never pushed from here, only pulled (see
// prefsSync.ts's module doc). Mobile settings views read through this
// instead of adding a second fetch to /api/me/preferences.
export function readSyncedRawValue(key: string): string | undefined {
  if (typeof localStorage === 'undefined') return undefined
  const raw = localStorage.getItem(`atterm.${key}.value`)
  if (raw === null) return undefined
  try {
    const parsed = JSON.parse(raw)
    return typeof parsed === 'string' ? parsed : undefined
  } catch {
    return undefined
  }
}
