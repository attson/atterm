<script lang="ts" setup>
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { usePlatform } from '../platform'
import { useI18n } from '../i18n/useI18n'

const props = defineProps<{ scannedUrl: string; allowInsecure?: boolean }>()
const emit = defineEmits<{ (e: 'connected'): void; (e: 'cancel'): void }>()

const platform = usePlatform()
const { t } = useI18n()

const status = ref<'pending' | 'error'>('pending')
const errorCode = ref('')
const elapsedMs = ref(0)
let elapsedTimer: ReturnType<typeof setInterval> | null = null

// Map internal error tokens to localized strings. Unknown codes fall
// through to the generic "Pairing failed: <code>" so we never lose the
// raw signal for debugging.
const errorMessage = computed(() => {
  const code = errorCode.value
  switch (code) {
    case 'pair_invalid_url':       return t('mobile.pairing.errInvalidUrl')
    case 'pair_invalid_scheme':    return t('mobile.pairing.errInvalidScheme')
    case 'platform_unsupported':   return t('mobile.pairing.errPlatformUnsupported')
    case 'cannot_reach_relay':     return t('mobile.pairing.errCannotReachRelay')
    case 'pair_timeout':           return t('mobile.pairing.errTimeout')
    case 'pair_invalid':           return t('mobile.pairing.errPairInvalid')
    default:                       return t('mobile.pairing.errGeneric', { message: code })
  }
})

const elapsedLabel = computed(() => (elapsedMs.value / 1000).toFixed(1) + 's')

function parseScanned(raw: string, allowInsecure: boolean): { origin: string; token: string } | string {
  let u: URL
  try {
    u = new URL(raw)
  } catch {
    return 'pair_invalid_url'
  }
  if (u.protocol !== 'https:' && !(u.protocol === 'http:' && allowInsecure)) {
    return 'pair_invalid_scheme'
  }
  const token = u.searchParams.get('t')
  if (!token || !u.host) return 'pair_invalid_url'
  return { origin: u.origin, token }
}

async function run() {
  const parsed = parseScanned(props.scannedUrl, !!props.allowInsecure)
  if (typeof parsed === 'string') {
    errorCode.value = parsed
    status.value = 'error'
    return
  }

  // Belt-and-suspenders timeout layer #2: even if platform.consumePairing's
  // own AbortController fails to cancel the in-flight fetch (some WKWebView
  // builds drop the abort signal mid-handshake), this Promise.race
  // guarantees we surface pair_timeout to the user.
  const started = Date.now()
  elapsedMs.value = 0
  elapsedTimer = setInterval(() => { elapsedMs.value = Date.now() - started }, 100)

  try {
    if (!platform.relay.consumePairing) throw new Error('platform_unsupported')
    const result = await Promise.race([
      platform.relay.consumePairing(parsed.origin, parsed.token),
      new Promise<never>((_, reject) =>
        setTimeout(() => reject(new Error('pair_timeout')), 15_500),
      ),
    ])
    await platform.relay.save({
      url: result.relay_url,
      token: result.session_token,
      session_expires_at: result.expires_at,
      allow_insecure_relay: !!props.allowInsecure,
      remote_permission: 'full',
      last_email: '',
      connected: false,
    })
    emit('connected')
  } catch (e) {
    errorCode.value = e instanceof Error ? e.message : String(e)
    status.value = 'error'
  } finally {
    if (elapsedTimer) { clearInterval(elapsedTimer); elapsedTimer = null }
  }
}

onMounted(run)
onBeforeUnmount(() => {
  if (elapsedTimer) { clearInterval(elapsedTimer); elapsedTimer = null }
})
</script>

<template>
  <div class="pair-consume">
    <div v-if="status === 'pending'" class="pending">
      <p>{{ t('mobile.pairing.connecting') }}</p>
      <p class="elapsed" data-testid="pair-elapsed">{{ elapsedLabel }}</p>
    </div>
    <div v-else class="error" data-testid="pair-error">
      <p>{{ t('mobile.pairing.failed') }}</p>
      <p class="code">{{ errorMessage }}</p>
      <button type="button" @click="emit('cancel')">{{ t('mobile.pairing.back') }}</button>
    </div>
  </div>
</template>

<style scoped>
.pair-consume { min-height: 100vh; display: flex; align-items: center; justify-content: center; flex-direction: column; gap: 12px; padding: 1.5rem; background: var(--bg, #05070d); color: var(--fg, #e6e7ea); }
.pending { display: flex; flex-direction: column; align-items: center; gap: 6px; font-size: 0.95rem; color: #8d93a3; }
.pending p { margin: 0; }
.pending .elapsed { font-family: var(--font-mono); font-size: 0.8rem; color: #5b6172; }
.error p { margin: 0; }
.error .code { font-family: var(--font-mono); color: #f87171; font-size: 0.8rem; }
.error button { margin-top: 12px; height: 42px; padding: 0 18px; border: none; border-radius: 9px; background: #3b82f6; color: #fff; font-weight: 600; }
</style>
