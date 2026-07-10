<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from '@shared/i18n/useI18n'

const MAX_ATTACH_BYTES = 10 * 1024 * 1024

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{
  (e: 'paste-text', text: string): void
  (e: 'paste-image', file: File): void
  (e: 'paste-file', file: File): void
  (e: 'close'): void
}>()

const text = ref('')
const { t } = useI18n()

watch(
  () => props.open,
  (next) => {
    if (!next) text.value = ''
  },
)

function onSubmit(e: Event) {
  e.preventDefault()
  if (text.value) emit('paste-text', text.value)
  emit('close')
}

function onCancel() {
  emit('close')
}

function dispatchFile(file: File) {
  if (file.size > MAX_ATTACH_BYTES) {
    // Keep the failure visible in devtools; App.vue's paste-image handler
    // already covers user-facing feedback for image paste, and file toasts
    // come from pasteFileBus after a successful send. Silent oversize drop
    // is acceptable for now.
    console.warn('[PasteFallback] file too large, dropping', file.name, file.size)
    return
  }
  if (file.type.startsWith('image/')) {
    emit('paste-image', file)
  } else {
    emit('paste-file', file)
  }
}

function onImageChange(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  dispatchFile(file)
}

function onAnyFileChange(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  dispatchFile(file)
}
</script>

<template>
  <form v-if="open" class="paste-fallback" @submit="onSubmit">
    <textarea v-model="text" rows="3" :placeholder="t('terminal.paste')" data-testid="paste-text"></textarea>
    <div class="actions">
      <label class="paste-image-pick" for="paste-image-file">{{ t('terminal.pickImage') }}</label>
      <input
        id="paste-image-file"
        type="file"
        accept="image/*"
        hidden
        data-testid="paste-image-file"
        @change="onImageChange"
      />
      <label class="paste-image-pick" for="paste-file-file">{{ t('terminal.pickFile') }}</label>
      <input
        id="paste-file-file"
        type="file"
        accept="*/*"
        hidden
        data-testid="paste-file-file"
        @change="onAnyFileChange"
      />
      <button type="button" data-testid="paste-cancel" @click="onCancel">{{ t('common.cancel') }}</button>
      <button type="submit">{{ t('common.send') }}</button>
    </div>
  </form>
</template>

<style scoped>
.paste-fallback { display: flex; flex-direction: column; gap: 0.5rem; padding: 0.5rem; background: var(--panel); border-top: 1px solid var(--border); }
.paste-fallback textarea { background: var(--bg); color: var(--fg); border: 1px solid var(--border); border-radius: 4px; padding: 0.5rem; font: inherit; }
.actions { display: flex; gap: 0.5rem; align-items: center; }
.paste-image-pick { color: var(--fg-dim); font-size: 0.875rem; cursor: pointer; text-decoration: underline; }
.paste-fallback button { background: var(--bg); color: var(--fg); border: 1px solid var(--border); border-radius: 4px; padding: 0.4rem 0.8rem; font: inherit; cursor: pointer; }
.paste-fallback button[type="submit"] { background: var(--accent); color: var(--bg); border-color: var(--accent); }
</style>
