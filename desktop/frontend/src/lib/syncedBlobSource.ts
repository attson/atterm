// Reads a prefs-sync value straight out of the localStorage cache that
// lib/prefsSync.capacitor.ts's localStorageAdapter already maintains — the
// same store PrefsSyncEngine.pull() writes into for EVERY key the relay
// returns, including profiles_encrypted and ssh_hosts_encrypted, which are
// not in SYNCED_KEYS and therefore never pushed from here, only pulled (see
// prefsSync.ts's module doc). Mobile settings views read through this
// instead of adding a second fetch to /api/me/preferences.
//
// Built on localStorageAdapter().readValue() rather than re-deriving the
// `atterm.<key>.value` storage-key convention here: that convention lives
// in exactly one place (prefsSync.capacitor.ts). Duplicating it means
// renaming it there would silently strand this file — prefsSync.capacitor.test.ts
// only exercises its own literal, and the mobile views would just settle
// into a permanent "nothing has synced" with the whole suite still green.
import { localStorageAdapter } from './prefsSync.capacitor'

export function readSyncedRawValue(key: string): string | undefined {
  const value: unknown = localStorageAdapter().readValue(key)
  return typeof value === 'string' ? value : undefined
}
