// Mobile settings views (SettingsProfilesMobile.vue, SettingsSSHHostsMobile.vue)
// need the unlocked account_key before they can open the two sealed prefs-sync
// blobs. getCurrentAccountKey() (./account-key) is synchronous, but the value
// it returns depends on capacitor.ts's bootstrapCachedAccountKey — a one-shot
// async read from secure storage that runs at platform-creation time and can
// still be in flight the moment a settings view mounts (e.g. the user opens
// Settings within the first tick after app launch). Treat "not there yet" as
// a real, transient state rather than the same thing as "logged out":
// poll briefly after mount, then give up. A genuinely logged-out user (or a
// bootstrap that failed) settles into a stable null, which callers render as
// their own "locked" state — this module never distinguishes the two itself.
import { onBeforeUnmount, onMounted, ref, type Ref } from 'vue'
import { getCurrentAccountKey } from './account-key'

export function usePolledAccountKey(pollMs = 300, maxAttempts = 10): Ref<Uint8Array | null> {
  const accountKey = ref<Uint8Array | null>(getCurrentAccountKey()) as Ref<Uint8Array | null>
  let handle: ReturnType<typeof setInterval> | null = null

  onMounted(() => {
    if (accountKey.value) return
    let attempts = 0
    handle = setInterval(() => {
      attempts++
      const k = getCurrentAccountKey()
      if (k) accountKey.value = k
      if (k || attempts >= maxAttempts) {
        if (handle !== null) clearInterval(handle)
        handle = null
      }
    }, pollMs)
  })

  onBeforeUnmount(() => {
    if (handle !== null) clearInterval(handle)
    handle = null
  })

  return accountKey
}
