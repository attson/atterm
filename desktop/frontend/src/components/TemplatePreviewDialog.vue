<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from '../i18n/useI18n'
import type { QuickTemplate } from '../lib/templates'

const props = defineProps<{ template: QuickTemplate | null }>()
const emit = defineEmits<{
  (e: 'confirm', t: QuickTemplate): void
  (e: 'cancel'): void
}>()
const { t } = useI18n()

const open = computed(() => props.template !== null)

function onKeydown(e: KeyboardEvent) {
  if (!open.value) return
  if (e.key === 'Enter') {
    e.preventDefault()
    if (props.template) emit('confirm', props.template)
  } else if (e.key === 'Escape') {
    e.preventDefault()
    emit('cancel')
  }
}

function onConfirmClick() {
  if (props.template) emit('confirm', props.template)
}

onMounted(() => { window.addEventListener('keydown', onKeydown) })
onBeforeUnmount(() => { window.removeEventListener('keydown', onKeydown) })
</script>

<template>
  <div
    v-if="open"
    class="dialog-backdrop"
    data-testid="template-preview-backdrop"
    @click="emit('cancel')"
  >
    <div class="dialog" data-testid="template-preview" @click.stop>
      <h3>{{ t('settings.templates.preview.title') }}</h3>
      <pre class="preview">{{ template?.text }}</pre>
      <div class="actions">
        <button
          type="button"
          data-testid="template-preview-cancel"
          @click="emit('cancel')"
        >{{ t('common.cancel') }}</button>
        <button
          type="button"
          autofocus
          data-testid="template-preview-confirm"
          @click="onConfirmClick"
        >{{ t('settings.templates.preview.send') }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dialog-backdrop {
  position: fixed; inset: 0; z-index: 50;
  background: rgba(0, 0, 0, 0.55);
  display: flex; align-items: center; justify-content: center;
  padding: 1rem;
}
.dialog {
  width: 100%; max-width: 320px;
  background: #11182b; color: #e6e7ea;
  border: 1px solid #1e2638; border-radius: 11px;
  padding: 14px 16px;
  display: flex; flex-direction: column; gap: 10px;
}
.dialog h3 { margin: 0; font-size: 0.95rem; font-weight: 600; }
.preview {
  margin: 0; padding: 8px 10px;
  background: #020617; color: #e2e8f0;
  border: 1px solid #1e2638; border-radius: 8px;
  font-family: var(--font-mono);
  font-size: 0.82rem; line-height: 1.4;
  white-space: pre-wrap; word-break: break-all;
  max-height: 30vh; overflow-y: auto;
}
.actions {
  display: flex; gap: 8px; justify-content: flex-end;
}
.actions button {
  height: 32px; padding: 0 14px;
  border: 1px solid #1e2638; border-radius: 7px;
  background: #11182b; color: #cbd5e1; font-size: 0.82rem;
}
.actions button:last-child {
  background: #2563eb; border-color: #2563eb; color: #ffffff; font-weight: 600;
}
</style>
