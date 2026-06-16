// account_key persistence for the web client. v1 stores the unlocked
// 32-byte E2EE key in sessionStorage as base64 std; the key is wiped
// on tab close. Persisting to IndexedDB (so the service-worker push
// handler can decrypt notifications too) is a follow-up that needs an
// origin-isolation argument before it ships.

const STORAGE_KEY = 'atterm.account-key'

function bytesToB64(b: Uint8Array): string {
  return btoa(String.fromCharCode(...b))
}

function b64ToBytes(s: string): Uint8Array {
  const bin = atob(s)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

export function saveAccountKey(key: Uint8Array): void {
  if (typeof sessionStorage === 'undefined') return
  sessionStorage.setItem(STORAGE_KEY, bytesToB64(key))
}

export function loadAccountKey(): Uint8Array | null {
  if (typeof sessionStorage === 'undefined') return null
  const raw = sessionStorage.getItem(STORAGE_KEY)
  if (!raw) return null
  try {
    return b64ToBytes(raw)
  } catch {
    return null
  }
}

export function clearAccountKey(): void {
  if (typeof sessionStorage === 'undefined') return
  sessionStorage.removeItem(STORAGE_KEY)
}

export function hasAccountKey(): boolean {
  if (typeof sessionStorage === 'undefined') return false
  return sessionStorage.getItem(STORAGE_KEY) !== null
}
