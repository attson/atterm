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
const step = ref<'parsing' | 'requesting' | 'saving'>('parsing')
let rafHandle = 0
let started = 0
let cancelled = false

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
    case 'cancelled':              return t('mobile.pairing.errCancelled')
    default:                       return t('mobile.pairing.errGeneric', { message: code })
  }
})

const elapsedLabel = computed(() => (elapsedMs.value / 1000).toFixed(1) + 's')
const stepLabel = computed(() => {
  switch (step.value) {
    case 'parsing':    return t('mobile.pairing.stepParsing')
    case 'requesting': return t('mobile.pairing.stepRequesting')
    case 'saving':     return t('mobile.pairing.stepSaving')
    default:           return ''
  }
})

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

// RAF-driven elapsed counter. requestAnimationFrame is tied to the
// display refresh signal and survives the throttling that drops
// setInterval to ~0 Hz when WKWebView is mid-fetch on iOS.
function tickElapsed(): void {
  if (cancelled || status.value !== 'pending') return
  elapsedMs.value = Date.now() - started
  rafHandle = requestAnimationFrame(tickElapsed)
}

function onCancelClicked(): void {
  cancelled = true
  errorCode.value = 'cancelled'
  status.value = 'error'
  if (rafHandle) { cancelAnimationFrame(rafHandle); rafHandle = 0 }
}

async function run() {
  const parsed = parseScanned(props.scannedUrl, !!props.allowInsecure)
  if (typeof parsed === 'string') {
    errorCode.value = parsed
    status.value = 'error'
    return
  }

  started = Date.now()
  elapsedMs.value = 0
  rafHandle = requestAnimationFrame(tickElapsed)

  try {
    if (!platform.relay.consumePairing) throw new Error('platform_unsupported')
    // Two-layer timeout: platform.consumePairing has its own native timeout
    // via CapacitorHttp; this Promise.race adds a JS-level 16s reject in
    // case JS-side timers are still alive even when the network is hung.
    step.value = 'requesting'
    const result = await Promise.race([
      platform.relay.consumePairing(parsed.origin, parsed.token),
      new Promise<never>((_, reject) =>
        setTimeout(() => reject(new Error('pair_timeout')), 16_000),
      ),
    ])
    if (cancelled) return
    step.value = 'saving'
    await platform.relay.save({
      url: result.relay_url,
      token: result.session_token,
      session_expires_at: result.expires_at,
      allow_insecure_relay: !!props.allowInsecure,
      remote_permission: 'full',
      last_email: '',
      connected: false,
    })
    if (cancelled) return
    emit('connected')
  } catch (e) {
    if (cancelled) return
    errorCode.value = e instanceof Error ? e.message : String(e)
    status.value = 'error'
  } finally {
    if (rafHandle) { cancelAnimationFrame(rafHandle); rafHandle = 0 }
  }
}

onMounted(run)
onBeforeUnmount(() => {
  cancelled = true
  if (rafHandle) { cancelAnimationFrame(rafHandle); rafHandle = 0 }
})
</script>

<template>
  <div class="pair-consume">
    <div v-if="status === 'pending'" class="pending">
      <p>{{ t('mobile.pairing.connecting') }}</p>
      <p class="step" data-testid="pair-step">{{ stepLabel }}</p>
      <p class="elapsed" data-testid="pair-elapsed">{{ elapsedLabel }}</p>
      <button type="button" class="cancel" data-testid="pair-cancel" @click="onCancelClicked">
        {{ t('common.cancel') }}
      </button>
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
.pending .step { font-size: 0.8rem; color: #6b7280; }
.pending .elapsed { font-family: var(--font-mono); font-size: 0.8rem; color: #5b6172; }
.pending .cancel { margin-top: 18px; height: 36px; padding: 0 18px; border: 1px solid #2e3340; border-radius: 8px; background: transparent; color: #8d93a3; font-size: 0.85rem; }
.error p { margin: 0; }
.error .code { font-family: var(--font-mono); color: #f87171; font-size: 0.8rem; }
.error button { margin-top: 12px; height: 42px; padding: 0 18px; border: none; border-radius: 9px; background: #3b82f6; color: #fff; font-weight: 600; }
</style>
