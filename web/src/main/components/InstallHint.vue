<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from '@shared/i18n/useI18n'

const STORAGE_KEY = 'at-term-install-hint-dismissed'
const visible = ref(false)
const { t } = useI18n()

function isIos(ua: string): boolean {
  return /iPad|iPhone|iPod/i.test(ua) && !/Macintosh|Windows|Android/i.test(ua)
}

function isStandalone(): boolean {
  if (typeof window === 'undefined') return false
  return Boolean(
    window.matchMedia?.('(display-mode: standalone)').matches ||
      (window.navigator as Navigator & { standalone?: boolean }).standalone === true,
  )
}

function onDismiss() {
  localStorage.setItem(STORAGE_KEY, '1')
  visible.value = false
}

onMounted(() => {
  if (typeof navigator === 'undefined' || typeof localStorage === 'undefined') return
  const dismissed = localStorage.getItem(STORAGE_KEY) === '1'
  visible.value = isIos(navigator.userAgent) && !isStandalone() && !dismissed
})
</script>

<template>
  <section v-if="visible" class="install-hint">
    <div>
      <strong>{{ t('main.install.title') }}</strong>
      <span>{{ t('main.install.text') }}</span>
    </div>
    <button type="button" :aria-label="t('main.install.dismiss')" @click="onDismiss">×</button>
  </section>
</template>

<style scoped>
.install-hint {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.75rem 1rem;
  background: var(--panel);
  border-bottom: 1px solid var(--border);
  color: var(--fg-dim);
  font-size: 0.875rem;
}
.install-hint strong { display: block; color: var(--fg); }
.install-hint button { background: transparent; color: var(--fg-dim); border: none; font-size: 1.25rem; cursor: pointer; }
.install-hint button:hover { color: var(--fg); }
</style>
