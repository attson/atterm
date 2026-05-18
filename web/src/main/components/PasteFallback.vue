<script setup lang="ts">
import { ref, watch } from 'vue'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{
  (e: 'paste-text', text: string): void
  (e: 'paste-image', file: File): void
  (e: 'close'): void
}>()

const text = ref('')

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

function onFileChange(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  if (!file.type.startsWith('image/')) return
  emit('paste-image', file)
}
</script>

<template>
  <form v-if="open" class="paste-fallback" @submit="onSubmit">
    <textarea v-model="text" rows="3" placeholder="paste text here" data-testid="paste-text"></textarea>
    <div class="actions">
      <label class="paste-image-pick" for="paste-image-file">pick image</label>
      <input
        id="paste-image-file"
        type="file"
        accept="image/*"
        hidden
        data-testid="paste-image-file"
        @change="onFileChange"
      />
      <button type="button" data-testid="paste-cancel" @click="onCancel">cancel</button>
      <button type="submit">send</button>
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
