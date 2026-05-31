<script lang="ts" setup>
import { onMounted, ref } from 'vue'
import { usePlatform } from '../platform'
import { useI18n } from '../i18n/useI18n'

const props = defineProps<{ scannedUrl: string; allowInsecure?: boolean }>()
const emit = defineEmits<{ (e: 'connected'): void; (e: 'cancel'): void }>()

const platform = usePlatform()
const { t } = useI18n()

const status = ref<'pending' | 'error'>('pending')
const errorCode = ref('')

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
  try {
    if (!platform.relay.consumePairing) throw new Error('platform_unsupported')
    const result = await platform.relay.consumePairing(parsed.origin, parsed.token)
    await platform.relay.save({
      url: result.relay_url,
      token: result.api_token,
      allow_insecure_relay: !!props.allowInsecure,
      remote_permission: 'full',
      connected: false,
    })
    emit('connected')
  } catch (e) {
    errorCode.value = e instanceof Error ? e.message : String(e)
    status.value = 'error'
  }
}

onMounted(run)
</script>

<template>
  <div class="pair-consume">
    <div v-if="status === 'pending'" class="pending">
      {{ t('mobile.pairing.connecting') }}
    </div>
    <div v-else class="error" data-testid="pair-error">
      <p>{{ t('mobile.pairing.failed') }}</p>
      <p class="code">{{ errorCode }}</p>
      <button type="button" @click="emit('cancel')">{{ t('mobile.pairing.back') }}</button>
    </div>
  </div>
</template>

<style scoped>
.pair-consume { min-height: 100vh; display: flex; align-items: center; justify-content: center; flex-direction: column; gap: 12px; padding: 1.5rem; background: var(--bg, #05070d); color: var(--fg, #e6e7ea); }
.pending { font-size: 0.95rem; color: #8d93a3; }
.error p { margin: 0; }
.error .code { font-family: ui-monospace, Menlo, monospace; color: #f87171; font-size: 0.8rem; }
.error button { margin-top: 12px; height: 42px; padding: 0 18px; border: none; border-radius: 9px; background: #3b82f6; color: #fff; font-weight: 600; }
</style>
