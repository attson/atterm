<script lang="ts" setup>
import { computed, onBeforeUnmount, ref } from 'vue'
import QRCode from 'qrcode'
import { createPairingToken, type PairingToken } from '../lib/api'
import { useI18n } from '../i18n/useI18n'

const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const current = ref<PairingToken | null>(null)
const qrDataUrl = ref('')
const now = ref(Math.floor(Date.now() / 1000))
let tick: ReturnType<typeof setInterval> | null = null

const remaining = computed(() => current.value ? Math.max(0, current.value.expires_at - now.value) : 0)
const countdownText = computed(() => {
  const s = remaining.value
  const m = Math.floor(s / 60)
  const r = s % 60
  return `${m}:${r.toString().padStart(2, '0')}`
})
const expired = computed(() => current.value !== null && remaining.value === 0)

async function generate() {
  loading.value = true
  error.value = ''
  try {
    const tok = await createPairingToken()
    qrDataUrl.value = await QRCode.toDataURL(tok.qr_url, { width: 240, margin: 1 })
    current.value = tok
    now.value = Math.floor(Date.now() / 1000)
    if (tick) clearInterval(tick)
    tick = setInterval(() => { now.value = Math.floor(Date.now() / 1000) }, 1000)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

onBeforeUnmount(() => { if (tick) clearInterval(tick) })
</script>

<template>
  <section class="pairing-panel" data-testid="pairing-panel">
    <div class="title">{{ t('settings.relay.pairing.title') }}</div>
    <p class="hint">{{ t('settings.relay.pairing.hint') }}</p>

    <button
      v-if="!current"
      type="button"
      data-testid="pair-generate"
      :disabled="loading"
      @click="generate"
    >
      {{ loading ? t('settings.relay.pairing.generating') : t('settings.relay.pairing.generate') }}
    </button>

    <div v-else class="qr-wrap">
      <img :src="qrDataUrl" alt="" class="qr" :class="{ dimmed: expired }" data-testid="pair-qr" />
      <div v-if="current.wrapped" class="wrap-badge" data-testid="pair-wrap-badge">
        {{ t('settings.relay.pairing.wrappedBadge') }}
      </div>
      <p v-else class="wrap-warning" data-testid="pair-wrap-warning">
        {{ t('settings.relay.pairing.unwrappedWarning') }}
      </p>
      <div v-if="!expired" class="countdown">
        <span>{{ t('settings.relay.pairing.expiresIn') }}</span>
        <span class="time">{{ countdownText }}</span>
      </div>
      <div v-else class="countdown expired" data-testid="pair-expired">
        {{ t('settings.relay.pairing.expired') }}
      </div>
      <code class="prefix">{{ current.token.slice(0, 12) }}…</code>
      <button type="button" data-testid="pair-regenerate" @click="generate">
        {{ t('settings.relay.pairing.regenerate') }}
      </button>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
  </section>
</template>

<style scoped>
.pairing-panel { display: flex; flex-direction: column; gap: 10px; padding: 12px; border: 1px solid var(--border); border-radius: 10px; }
.title { font-size: 13px; font-weight: 700; color: var(--fg); }
.hint { font-size: 12px; color: var(--fg-dim); margin: 0; line-height: 1.45; }
.qr-wrap { display: flex; flex-direction: column; align-items: center; gap: 8px; }
.qr { width: 240px; height: 240px; image-rendering: pixelated; background: #fff; padding: 6px; border-radius: 6px; }
.qr.dimmed { opacity: 0.35; }
.countdown { font-size: 12px; color: var(--fg-dim); }
.countdown.expired { color: var(--bad); }
.wrap-badge { font-size: 11px; color: var(--fg-dim); padding: 2px 8px; border: 1px solid var(--border); border-radius: 4px; }
.wrap-warning { font-size: 11px; color: var(--warn); background: rgba(255, 200, 0, 0.06); border-left: 2px solid var(--warn); padding: 6px 10px; margin: 0; line-height: 1.4; text-align: left; max-width: 240px; }
.prefix { font-family: ui-monospace, Menlo, monospace; font-size: 11px; color: var(--fg-dim); }
.error { color: var(--bad); font-size: 12px; margin: 0; }
button { height: 30px; border: 1px solid var(--accent); border-radius: 7px; background: var(--accent); color: var(--bg); padding: 0 12px; font-size: 12px; font-weight: 700; cursor: pointer; }
button:disabled { opacity: 0.55; }
</style>
