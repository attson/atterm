<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from '../i18n/useI18n'
import { usePlatform } from '../platform'
import { DEFAULT_TEMPLATES, type QuickTemplate } from '../lib/templates'

const { t } = useI18n()
const platform = usePlatform()

const items = ref<QuickTemplate[]>([])
const editing = ref<{ id: string; label: string; text: string; isNew: boolean } | null>(null)
const resetOpen = ref(false)
const error = ref('')

// Editor shows the raw stored list verbatim (no defaults injection). An empty
// stored list shows an empty editor — the "Reset to defaults" button is the
// explicit way to seed the 10 starters. This keeps the editor and the
// runtime bar/dialog in sync: bar/dialog falls back to defaults via
// effectiveTemplates when the stored list is empty.
async function reload() {
  const list = await platform.templates.load()
  items.value = [...list]
}

onMounted(reload)

async function persist() {
  error.value = ''
  try {
    await platform.templates.save(items.value)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

function startAdd() {
  editing.value = { id: crypto.randomUUID(), label: '', text: '', isNew: true }
}
function startEdit(tpl: QuickTemplate) {
  editing.value = { id: tpl.id, label: tpl.label, text: tpl.text, isNew: false }
}
function cancelEdit() { editing.value = null }
async function saveEdit() {
  if (!editing.value) return
  const e = editing.value
  if (!e.label.trim() || !e.text) return
  if (e.isNew) {
    items.value.push({ id: e.id, label: e.label.trim(), text: e.text })
  } else {
    const idx = items.value.findIndex((x) => x.id === e.id)
    if (idx >= 0) items.value[idx] = { id: e.id, label: e.label.trim(), text: e.text }
  }
  editing.value = null
  await persist()
}
async function deleteItem(id: string) {
  items.value = items.value.filter((x) => x.id !== id)
  await persist()
}
async function moveUp(id: string) {
  const idx = items.value.findIndex((x) => x.id === id)
  if (idx <= 0) return
  ;[items.value[idx - 1], items.value[idx]] = [items.value[idx], items.value[idx - 1]]
  await persist()
}
async function moveDown(id: string) {
  const idx = items.value.findIndex((x) => x.id === id)
  if (idx < 0 || idx >= items.value.length - 1) return
  ;[items.value[idx], items.value[idx + 1]] = [items.value[idx + 1], items.value[idx]]
  await persist()
}
function startReset() { resetOpen.value = true }
async function confirmReset() {
  resetOpen.value = false
  await platform.templates.clear()
  items.value = [...DEFAULT_TEMPLATES]
}
function cancelReset() { resetOpen.value = false }
</script>

<template>
  <div class="tab-pane">
    <p class="hint">{{ t('settings.templates.intro') }}</p>

    <ul class="list">
      <li
        v-for="(it, idx) in items"
        :key="it.id"
        class="row"
        :data-testid="`template-row-${it.id}`"
      >
        <span class="label">{{ it.label }}</span>
        <code class="text">{{ it.text }}</code>
        <div class="actions">
          <button :disabled="idx === 0" @click="moveUp(it.id)">↑</button>
          <button :disabled="idx === items.length - 1" @click="moveDown(it.id)">↓</button>
          <button @click="startEdit(it)">{{ t('settings.templates.edit') }}</button>
          <button class="del" :data-testid="`template-delete-${it.id}`" @click="deleteItem(it.id)">
            {{ t('settings.templates.delete') }}
          </button>
        </div>
      </li>
    </ul>

    <div v-if="editing" class="edit-row">
      <input v-model="editing.label" :placeholder="t('settings.templates.label')" data-testid="template-edit-label" />
      <input v-model="editing.text" :placeholder="t('settings.templates.text')" data-testid="template-edit-text" />
      <button data-testid="template-edit-save" @click="saveEdit">{{ t('settings.templates.save') }}</button>
      <button @click="cancelEdit">{{ t('common.cancel') }}</button>
    </div>

    <div class="footer">
      <button data-testid="template-add" @click="startAdd">{{ t('settings.templates.add') }}</button>
      <button data-testid="template-reset" @click="startReset">{{ t('settings.templates.reset') }}</button>
      <span v-if="error" class="error">{{ error }}</span>
    </div>

    <div v-if="resetOpen" class="dialog-backdrop" @click="cancelReset">
      <div class="dialog" @click.stop>
        <p>{{ t('settings.templates.resetConfirm') }}</p>
        <div class="actions">
          <button @click="cancelReset">{{ t('common.cancel') }}</button>
          <button data-testid="template-reset-confirm" @click="confirmReset">{{ t('settings.templates.reset') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tab-pane { display: flex; flex-direction: column; gap: 10px; }
.hint { font-size: 12px; color: var(--fg-dim); margin: 0; line-height: 1.5; }
.list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.row { display: grid; grid-template-columns: 8rem 1fr auto; gap: 8px; align-items: center; padding: 8px 10px; background: var(--bg); border: 1px solid var(--border); border-radius: 6px; }
.label { font-weight: 600; font-family: var(--font-mono); font-size: 0.85rem; }
.text { font-family: var(--font-mono); font-size: 0.78rem; color: var(--fg-dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.actions { display: flex; gap: 4px; }
.actions button { height: 24px; padding: 0 8px; font-size: 0.74rem; }
.actions .del { color: var(--bad); border-color: rgba(248, 81, 73, 0.4); }
.edit-row { display: grid; grid-template-columns: 8rem 1fr auto auto; gap: 8px; padding: 8px 10px; background: var(--panel); border: 1px solid var(--accent); border-radius: 6px; }
.edit-row input { height: 28px; padding: 0 8px; }
.footer { display: flex; gap: 8px; align-items: center; }
.error { color: var(--bad); font-size: 0.75rem; }
.dialog-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.55); display: flex; align-items: center; justify-content: center; z-index: 50; }
.dialog { background: #11182b; padding: 14px 18px; border: 1px solid #1e2638; border-radius: 11px; max-width: 320px; display: flex; flex-direction: column; gap: 10px; color: #e6e7ea; }
</style>
