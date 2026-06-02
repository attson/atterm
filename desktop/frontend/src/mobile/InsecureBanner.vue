<script lang="ts" setup>
import { computed, ref } from 'vue'
import { useI18n } from '../i18n/useI18n'

const props = defineProps<{ relayUrl: string }>()
const emit = defineEmits<{ (e: 'dismiss'): void }>()
const { t } = useI18n()

const isInsecure = computed(() => props.relayUrl.startsWith('http://'))
const expanded = ref(false)
const dismissed = ref(false)
const visible = computed(() => isInsecure.value && !dismissed.value)

function toggle(): void {
  expanded.value = !expanded.value
}

function dismiss(): void {
  dismissed.value = true
  expanded.value = false
  emit('dismiss')
}
</script>

<template>
  <div v-if="visible" class="banner" data-testid="insecure-banner" @click="toggle">
    <div class="head">
      <svg class="icon" viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z"/>
        <line x1="12" y1="9" x2="12" y2="13"/>
        <line x1="12" y1="17" x2="12.01" y2="17"/>
      </svg>
      <span class="title">{{ t('mobile.insecure.warning.title') }}</span>
    </div>
    <div v-if="expanded" class="body" data-testid="insecure-body" @click.stop>
      <p>{{ t('mobile.insecure.warning.body') }}</p>
      <button type="button" data-testid="insecure-dismiss" @click="dismiss">
        {{ t('mobile.insecure.warning.dismiss') }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.banner { background: rgba(245,158,11,.13); border-bottom: 1px solid rgba(245,158,11,.4); color: #f5c451; padding: 8px 12px; font-size: 0.8rem; cursor: pointer; }
.head { display: flex; align-items: center; gap: 6px; }
.icon { flex: 0 0 auto; }
.title { font-weight: 600; }
.body { margin-top: 6px; }
.body p { margin: 0 0 8px; color: #f5c451; }
.body button { background: rgba(245,158,11,.25); border: 1px solid rgba(245,158,11,.5); color: #f5c451; padding: 4px 10px; border-radius: 6px; font-size: 0.78rem; cursor: pointer; }
</style>
