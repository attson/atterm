// secureStorage hides the iOS Keychain plugin behind an async key/value
// interface. On iOS the calls land in the native AttermSecureStorage plugin
// (mobile/ios/App/App/Plugins/AttermSecureStorage). On web and in vitest a
// closure-backed Map is used so tests don't touch real Keychain.
//
// The default export `secureStorage` is selected once at module load time
// based on plugin presence; subsequent calls share the same backend.

import { registerPlugin } from '@capacitor/core'

export interface SecureStorage {
  set(key: string, value: string): Promise<void>
  get(key: string): Promise<string | null>
  remove(key: string): Promise<void>
}

interface NativePlugin {
  set(opts: { key: string; value: string }): Promise<void>
  get(opts: { key: string }): Promise<{ value: string | null }>
  remove(opts: { key: string }): Promise<void>
}

// Capacitor's registerPlugin returns a typed proxy on every platform; the
// proxy throws PLUGIN_NOT_AVAILABLE when called in environments where the
// native side isn't linked (web, vitest).
const native = registerPlugin<NativePlugin>('AttermSecureStorage')

export function createInMemorySecureStorage(): SecureStorage {
  const store = new Map<string, string>()
  return {
    async set(key, value) { store.set(key, value) },
    async get(key) { return store.has(key) ? store.get(key)! : null },
    async remove(key) { store.delete(key) },
  }
}

function createNativeSecureStorage(): SecureStorage {
  return {
    async set(key, value) { await native.set({ key, value }) },
    async get(key) {
      const r = await native.get({ key })
      return r.value
    },
    async remove(key) { await native.remove({ key }) },
  }
}

// Detect whether the native plugin is reachable. Capacitor throws
// PLUGIN_NOT_AVAILABLE on web; treat anything else as "plugin is there".
let detected: Promise<SecureStorage> | null = null

async function selectBackend(): Promise<SecureStorage> {
  try {
    await native.get({ key: '__atterm_probe__' })
    return createNativeSecureStorage()
  } catch (e: any) {
    const msg = String(e?.message ?? e ?? '')
    if (msg.includes('PLUGIN_NOT_AVAILABLE') || msg.includes('not implemented')) {
      return createInMemorySecureStorage()
    }
    // Native plugin is present but failed for another reason — keep using it
    // so callers see real errors instead of silently falling back to memory.
    return createNativeSecureStorage()
  }
}

export const secureStorage: SecureStorage = {
  async set(key, value) { (await (detected ??= selectBackend())).set(key, value) },
  async get(key) { return (await (detected ??= selectBackend())).get(key) },
  async remove(key) { return (await (detected ??= selectBackend())).remove(key) },
}
