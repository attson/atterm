<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { type LocalePreference } from '../i18n'
import { useI18n } from '../i18n/useI18n'
import { usePlatform } from '../platform'
import { validateRelayBase } from './relay'
import PairingConsume from './PairingConsume.vue'
import { BarcodeScanner } from '@capacitor-mlkit/barcode-scanning'

const props = defineProps<{ reason?: 'token_invalid' | null }>()
const emit = defineEmits<{ (e: 'connected'): void }>()

const platform = usePlatform()
const { t, languageOptions, localePreference, setLocalePreference } = useI18n()
const scheme = ref<'https://' | 'http://'>('https://')
const host = ref('')
const url = computed(() => scheme.value + host.value.trim())
const token = ref('')
const allowInsecure = ref(false)
const error = ref<string | null>(null)
const submitting = ref(false)
const scannedUrl = ref<string | null>(null)

// Split a raw relay URL into the scheme dropdown + host input. Pasting a full
// URL into the host field (e.g. from a copied address) lifts its scheme into
// the dropdown instead of producing https://https://… on connect.
function applyUrl(raw: string): void {
  const m = /^\s*(https?:\/\/)(.*)$/i.exec(raw)
  if (m) {
    scheme.value = m[1].toLowerCase() as 'https://' | 'http://'
    host.value = m[2].trim()
  } else {
    host.value = raw.trim()
  }
}

function normalizeHost(): void {
  applyUrl(host.value)
}

// The insecure-transport opt-in only applies to http:// relays; over https the
// transport is already encrypted, so the toggle is hidden and force-cleared.
watch(scheme, (s) => {
  if (s === 'https://') allowInsecure.value = false
})

const banner = computed(() =>
  props.reason === 'token_invalid'
    ? t('mobile.tokenInvalidBanner')
    : null,
)

const localizedLanguageOptions = computed(() =>
  languageOptions.map((option) => ({
    value: option.value,
    label: t(option.labelKey),
  })),
)

onMounted(async () => {
  const cfg = await platform.relay.load()
  if (cfg) {
    applyUrl(cfg.url || '')
    token.value = cfg.token || ''
    allowInsecure.value = !!cfg.allow_insecure_relay
  }
})

async function onScanQR(): Promise<void> {
  error.value = null
  try {
    const { camera } = await BarcodeScanner.requestPermissions()
    if (camera !== 'granted' && camera !== 'limited') {
      error.value = t('mobile.pairing.cameraDenied')
      return
    }
    const { barcodes } = await BarcodeScanner.scan({ formats: ['QR_CODE' as any] })
    const first = barcodes[0]
    if (!first?.rawValue) {
      error.value = t('mobile.pairing.noQrDetected')
      return
    }
    scannedUrl.value = first.rawValue
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

function onConsumeCancelled(): void {
  scannedUrl.value = null
}

async function onConnect(): Promise<void> {
  error.value = null
  const v = validateRelayBase(url.value, allowInsecure.value)
  if (v) { error.value = v; return }
  if (!token.value.trim()) { error.value = t('mobile.apiTokenRequired'); return }
  submitting.value = true
  try {
    await platform.relay.save({
      url: url.value.replace(/\/$/, ''),
      token: token.value.trim(),
      session_expires_at: 0,
      allow_insecure_relay: allowInsecure.value,
      remote_permission: 'full',
      last_email: '',
      connected: false,
    })
    await platform.relay.fetchMe()
    emit('connected')
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    error.value = /401/.test(msg)
      ? t('mobile.apiTokenInvalid')
      : /403|origin/i.test(msg)
        ? t('mobile.originRejected')
        : t('mobile.cannotReachRelay', { message: msg })
  } finally {
    submitting.value = false
  }
}

async function onLanguageChange(e: Event): Promise<void> {
  const target = e.target as HTMLSelectElement
  await setLocalePreference(target.value as LocalePreference)
}
</script>

<template>
  <div class="setup">
    <PairingConsume
      v-if="scannedUrl"
      :scanned-url="scannedUrl"
      :allow-insecure="allowInsecure"
      @connected="emit('connected')"
      @cancel="onConsumeCancelled"
    />
    <template v-else>
      <h1>AT Term</h1>
      <p class="sub">{{ t('mobile.setupSubtitle') }}</p>
      <div v-if="banner" class="banner">{{ banner }}</div>

      <button data-testid="scan-qr" class="btn btn-primary" :disabled="submitting" @click="onScanQR">
        {{ t('mobile.pairing.scan') }}
      </button>
      <p class="or">{{ t('mobile.pairing.orManual') }}</p>

      <label class="field">
        <span>{{ t('settings.general.languageLabel') }}</span>
        <select data-testid="mobile-language" :value="localePreference" :disabled="submitting" @change="onLanguageChange">
          <option v-for="option in localizedLanguageOptions" :key="option.value" :value="option.value">
            {{ option.label }}
          </option>
        </select>
      </label>
      <label class="field">
        <span>{{ t('mobile.relayUrl') }}</span>
        <div class="url-input">
          <select data-testid="relay-scheme" v-model="scheme" :disabled="submitting" class="scheme-select" :aria-label="t('mobile.relayUrl')">
            <option value="https://">https://</option>
            <option value="http://">http://</option>
          </select>
          <input data-testid="relay-url" v-model="host" :disabled="submitting" placeholder="relay.example.com" autocomplete="off" autocapitalize="off" spellcheck="false" @input="normalizeHost" />
        </div>
      </label>
      <label class="field">
        <span>{{ t('mobile.apiToken') }}</span>
        <input data-testid="relay-token" v-model="token" :disabled="submitting" type="password" placeholder="atk_…" autocomplete="off" />
      </label>
      <label v-if="scheme === 'http://'" class="row">
        <span>{{ t('mobile.allowInsecure') }}</span>
        <input data-testid="allow-insecure" v-model="allowInsecure" :disabled="submitting" type="checkbox" />
      </label>
      <aside v-if="scheme === 'http://' && allowInsecure" class="warn-hint" data-state="warn" data-testid="insecure-hint">
        <svg class="warn-icon" viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z"/>
          <line x1="12" y1="9" x2="12" y2="13"/>
          <line x1="12" y1="17" x2="12.01" y2="17"/>
        </svg>
        <span>{{ t('mobile.insecure.setupHint') }}</span>
      </aside>
      <p v-if="error" class="error">{{ error }}</p>
      <button data-testid="connect" class="btn" :disabled="submitting" @click="onConnect">{{ t('common.connect') }}</button>
    </template>
  </div>
</template>

<style scoped>
.setup { min-height: 100vh; box-sizing: border-box; display: flex; flex-direction: column; justify-content: center; padding: calc(2rem + env(safe-area-inset-top)) 1.25rem calc(2rem + env(safe-area-inset-bottom)); background: var(--bg, #05070d); color: var(--fg, #e6e7ea); font-family: var(--font-sans); }
h1 { text-align: center; margin: 0 0 4px; font-size: 1.6rem; }
.sub { text-align: center; color: #8d93a3; margin: 0 0 1.5rem; font-size: 0.9rem; }
.banner { background: rgba(245,158,11,.13); border: 1px solid rgba(245,158,11,.4); color: #f5c451; padding: 9px 11px; border-radius: 9px; margin-bottom: 1rem; font-size: 0.8rem; }
.field { display: block; margin-bottom: 0.9rem; }
.field span { display: block; font-size: 0.75rem; color: #8d93a3; margin-bottom: 0.35rem; }
.field input { width: 100%; height: 42px; border-radius: 9px; border: 1px solid #1e2638; background: #11182b; color: #e6e7ea; padding: 0 12px; font-size: 0.95rem; font-family: var(--font-mono-strict); }
.field select { width: 100%; height: 42px; border-radius: 9px; border: 1px solid #1e2638; background: #11182b; color: #e6e7ea; padding: 0 12px; font-size: 0.95rem; font-family: var(--font-sans); }
.url-input { display: flex; gap: 8px; }
.url-input .scheme-select { flex: 0 0 auto; width: auto; height: 42px; border-radius: 9px; border: 1px solid #1e2638; background: #11182b; color: #e6e7ea; padding: 0 8px; font-size: 0.9rem; font-family: var(--font-mono-strict); }
.url-input input { flex: 1 1 auto; min-width: 0; }
.row { display: flex; align-items: center; justify-content: space-between; margin: 1rem 0; font-size: 0.85rem; }
.error { color: #f87171; font-size: 0.8rem; margin: 0 0 0.75rem; }
.btn { width: 100%; height: 46px; border: none; border-radius: 10px; background: #3b82f6; color: #fff; font-size: 1rem; font-weight: 600; }
.btn:disabled { opacity: 0.6; }
.btn-primary { background: #2563eb; margin-bottom: 0.75rem; }
.or { text-align: center; color: #8d93a3; font-size: 0.8rem; margin: 0 0 1rem; }
.warn-hint { display: flex; gap: 8px; padding: 9px 11px; margin: 0 0 1rem; border: 1px solid rgba(245,158,11,.4); border-left-width: 3px; border-radius: 9px; background: rgba(245,158,11,.13); color: #f5c451; font-size: 0.78rem; line-height: 1.4; }
.warn-icon { flex: 0 0 auto; margin-top: 1px; }
</style>
