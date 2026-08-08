<script lang="ts" setup>
import { errText, logWarn } from "../lib/log";
import { computed } from 'vue'
import { usePlatform } from '../platform'
import { useWindowMaximized, setMaximized } from '../composables/useWindowMaximized'
import { useI18n } from '../i18n/useI18n'

const platform = usePlatform()
const { t } = useI18n()
const isMaximized = useWindowMaximized()
const maxLabel = computed(() => (isMaximized.value ? t('common.restore') : t('common.maximize')))

function safe(fn: () => void) {
  try {
    fn()
  } catch (e) {
    logWarn("window", "runtime call failed", { error: errText(e) })
  }
}

function onMin() {
  safe(() => { void platform.system.windowMinimize?.() })
}

function onMax() {
  safe(() => {
    void platform.system.windowToggleMaximize?.()
    setMaximized(!isMaximized.value)
  })
}

function onClose() {
  safe(() => { void platform.system.quit?.() })
}
</script>

<template>
  <div class="window-controls" style="--wails-draggable: no-drag">
    <button
      class="wc-btn"
      type="button"
      data-testid="window-min"
      :aria-label="t('common.minimize')"
      @click="onMin"
    >
      <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
        <rect x="1" y="4.5" width="8" height="1" fill="currentColor" />
      </svg>
    </button>
    <button
      class="wc-btn"
      type="button"
      data-testid="window-max"
      :aria-label="maxLabel"
      @click="onMax"
    >
      <svg v-if="!isMaximized" width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
        <rect x="1" y="1" width="8" height="8" fill="none" stroke="currentColor" />
      </svg>
      <svg v-else width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
        <rect x="1" y="3" width="6" height="6" fill="none" stroke="currentColor" />
        <rect x="3" y="1" width="6" height="6" fill="none" stroke="currentColor" />
      </svg>
    </button>
    <button
      class="wc-btn wc-close"
      type="button"
      data-testid="window-close"
      :aria-label="t('common.close')"
      @click="onClose"
    >
      <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
        <path d="M1 1 L9 9 M9 1 L1 9" stroke="currentColor" stroke-width="1" fill="none" />
      </svg>
    </button>
  </div>
</template>

<style scoped>
.window-controls {
  display: inline-flex;
  align-items: stretch;
  height: 100%;
  margin-left: 8px;
}
.wc-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  border: none;
  background: transparent;
  color: var(--fg-dim);
  cursor: pointer;
  transition: background 120ms, color 120ms;
}
.wc-btn:hover {
  background: rgba(255, 255, 255, 0.08);
  color: var(--fg);
}
.wc-close:hover {
  background: #e81123;
  color: #fff;
}
</style>
