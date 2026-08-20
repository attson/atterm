<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from '../i18n/useI18n'
import { usePlatform } from '../platform'
import { effectiveTemplates, type QuickTemplate } from '../lib/templates'
import SnippetRunPanel from './SnippetRunPanel.vue'

const { t } = useI18n()
const platform = usePlatform()

const items = ref<QuickTemplate[]>([])
const editing = ref<{ id: string; label: string; text: string; hotkey: string; isNew: boolean } | null>(null)
const resetOpen = ref(false)
const hidden = ref(false)
const error = ref('')
// The template being run across hosts, or null when the run panel is closed.
// Only one at a time: SnippetRunPanel is a modal, so opening it for a second
// row while one is already open is not a state this list can reach.
const runningTemplateId = ref<string | null>(null)
function openRunPanel(id: string) { runningTemplateId.value = id }
function closeRunPanel() { runningTemplateId.value = null }

// Editor mirrors what the runtime template bar shows: the stored list if
// non-empty, otherwise the bundled DEFAULT_TEMPLATES (via effectiveTemplates).
// What you see in Settings = what you see in the bar. The first customization
// (edit / add / reorder / delete) calls persist() which writes the visible
// list, naturally locking the defaults into the user's stored set.
async function reload() {
  const list = await effectiveTemplates(platform.templates)
  items.value = [...list]
  hidden.value = await platform.templates.loadHidden()
}

// prefsSync (Go / Capacitor / Web) writes synced values straight into local
// storage and fires prefs:changed; if this pane is already open when a pull
// lands, refresh so the list doesn't stay stuck at the pre-sync snapshot.
let prefsChangedOff: (() => void) | null = null
onMounted(async () => {
  await reload()
  prefsChangedOff = platform.events.on('prefs:changed', () => { void reload() })
})
onBeforeUnmount(() => {
  prefsChangedOff?.()
  prefsChangedOff = null
})

// notifyChanged tells any open terminal to live-reload its template bar +
// hidden flag, instead of waiting for a remount. Mirrors the mobile
// settings-page event from PR #108.
function notifyChanged() {
  platform.events.emit('quickTemplates:changed', null)
}

async function persist() {
  error.value = ''
  try {
    await platform.templates.save(items.value)
    notifyChanged()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

async function onHiddenToggle(v: boolean) {
  hidden.value = v
  try {
    await platform.templates.saveHidden(v)
    notifyChanged()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

function startAdd() {
  editing.value = { id: crypto.randomUUID(), label: '', text: '', hotkey: '', isNew: true }
}
function startEdit(tpl: QuickTemplate) {
  editing.value = { id: tpl.id, label: tpl.label, text: tpl.text, hotkey: tpl.hotkey || '', isNew: false }
}
function cancelEdit() { editing.value = null }
async function saveEdit() {
  if (!editing.value) return
  const e = editing.value
  if (!e.label.trim() || !e.text) return
  const next: QuickTemplate = { id: e.id, label: e.label.trim(), text: e.text }
  if (e.hotkey.trim()) next.hotkey = e.hotkey.trim()
  if (e.isNew) {
    items.value.push(next)
  } else {
    const idx = items.value.findIndex((x) => x.id === e.id)
    if (idx >= 0) items.value[idx] = next
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
  await reload()
  notifyChanged()
}
function cancelReset() { resetOpen.value = false }
</script>

<template>
  <div class="tab-pane">
    <p class="hint">{{ t('settings.templates.intro') }}</p>

    <label class="show-toggle">
      <input
        type="checkbox"
        data-testid="template-show-toggle"
        :checked="!hidden"
        @change="onHiddenToggle(!($event.target as HTMLInputElement).checked)"
      />
      <span>{{ t('settings.templates.showBar') }}</span>
    </label>

    <ul class="list">
      <li
        v-for="(it, idx) in items"
        :key="it.id"
        class="row"
        :data-testid="`template-row-${it.id}`"
      >
        <span class="label" :title="it.label">{{ it.label }}</span>
        <code class="text" :title="it.text">{{ it.text }}</code>
        <code class="hotkey">{{ it.hotkey || '—' }}</code>
        <div class="actions">
          <button :disabled="idx === 0" @click="moveUp(it.id)">↑</button>
          <button :disabled="idx === items.length - 1" @click="moveDown(it.id)">↓</button>
          <button @click="startEdit(it)">{{ t('settings.templates.edit') }}</button>
          <button :data-testid="`template-run-${it.id}`" @click="openRunPanel(it.id)">
            {{ t('snippets.runOnHosts') }}
          </button>
          <button class="del" :data-testid="`template-delete-${it.id}`" @click="deleteItem(it.id)">
            {{ t('settings.templates.delete') }}
          </button>
        </div>
      </li>
    </ul>

    <div v-if="editing" class="edit-row">
      <div class="edit-top">
        <input
          v-model="editing.label"
          class="edit-label-input"
          :placeholder="t('settings.templates.label')"
          data-testid="template-edit-label"
        />
        <input
          v-model="editing.hotkey"
          class="edit-hotkey-input"
          :placeholder="t('settings.templates.hotkey')"
          data-testid="template-edit-hotkey"
        />
      </div>
      <textarea
        v-model="editing.text"
        class="edit-text-input"
        rows="3"
        :placeholder="t('settings.templates.text')"
        data-testid="template-edit-text"
      />
      <div class="edit-actions">
        <button @click="cancelEdit">{{ t('common.cancel') }}</button>
        <button class="primary" data-testid="template-edit-save" @click="saveEdit">{{ t('settings.templates.save') }}</button>
      </div>
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

    <SnippetRunPanel
      v-if="runningTemplateId"
      :snippet-id="runningTemplateId"
      @close="closeRunPanel"
    />
  </div>
</template>

<style scoped>
.tab-pane { display: flex; flex-direction: column; gap: 10px; }
.hint { font-size: 12px; color: var(--fg-dim); margin: 0; line-height: 1.5; }
.show-toggle { display: inline-flex; align-items: center; gap: 8px; font-size: 13px; color: var(--fg); }
.list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.row { display: grid; grid-template-columns: 7rem 1fr 5rem auto; gap: 8px; align-items: center; padding: 8px 10px; background: var(--bg); border: 1px solid var(--border); border-radius: 6px; }
.label { font-weight: 600; font-family: var(--font-mono); font-size: 0.85rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.text { font-family: var(--font-mono); font-size: 0.78rem; color: var(--fg-dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.hotkey { font-family: var(--font-mono); font-size: 0.74rem; color: var(--fg-dim); text-align: center; }
.actions { display: flex; gap: 4px; }
.actions button { height: 24px; padding: 0 8px; font-size: 0.74rem; }
.actions .del { color: var(--bad); border-color: rgba(248, 81, 73, 0.4); }
.edit-row { display: flex; flex-direction: column; gap: 8px; padding: 10px; background: var(--panel); border: 1px solid var(--accent); border-radius: 6px; }
.edit-top { display: grid; grid-template-columns: 1fr 8rem; gap: 8px; }
.edit-row input, .edit-row textarea { padding: 6px 8px; background: var(--bg); color: var(--fg); border: 1px solid var(--border); border-radius: 4px; font-size: 0.85rem; font-family: var(--font-mono); width: 100%; box-sizing: border-box; }
.edit-row input { height: 28px; }
.edit-row textarea { resize: vertical; min-height: 60px; line-height: 1.4; }
.edit-actions { display: flex; gap: 6px; justify-content: flex-end; }
.edit-actions button { height: 28px; padding: 0 14px; font-size: 0.78rem; }
.edit-actions .primary { background: var(--accent); color: #0d1117; border-color: var(--accent); font-weight: 600; }
.footer { display: flex; gap: 8px; align-items: center; }
.error { color: var(--bad); font-size: 0.75rem; }
.dialog-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.55); display: flex; align-items: center; justify-content: center; z-index: 50; }
.dialog { background: #11182b; padding: 14px 18px; border: 1px solid #1e2638; border-radius: 11px; max-width: 320px; display: flex; flex-direction: column; gap: 10px; color: #e6e7ea; }
</style>
