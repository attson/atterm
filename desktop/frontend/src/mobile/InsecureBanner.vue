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
      <span class="icon">⚠</span>
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
.icon { font-size: 0.95rem; }
.title { font-weight: 600; }
.body { margin-top: 6px; }
.body p { margin: 0 0 8px; color: #f5c451; }
.body button { background: rgba(245,158,11,.25); border: 1px solid rgba(245,158,11,.5); color: #f5c451; padding: 4px 10px; border-radius: 6px; font-size: 0.78rem; cursor: pointer; }
</style>
