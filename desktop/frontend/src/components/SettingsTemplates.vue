<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from '../i18n/useI18n'
import { usePlatform } from '../platform'
import { DEFAULT_TEMPLATES, snippetCollectionJSONBytes, type QuickTemplate, type Snippet } from '../lib/templates'

const { t } = useI18n()
const platform = usePlatform()

type TemplateKind = 'quick' | 'snippet'
type TemplateItem = {
  id: string
  kind: TemplateKind
  label: string
  text: string
  hotkey?: string
}

const items = ref<TemplateItem[]>([])
const activeKind = ref<TemplateKind>('quick')
const visibleItems = computed(() => items.value.filter((item) => item.kind === activeKind.value))
const editing = ref<{
  id: string
  kind: TemplateKind
  originalKind: TemplateKind
  label: string
  text: string
  hotkey: string
  isNew: boolean
} | null>(null)
const hasQuickSnippetCollision = ref(false)
const resetOpen = ref(false)
const hidden = ref(false)
const error = ref('')
const historyStatus = ref({ enabled: false, count: 0 })
const historyClearOpen = ref(false)
const canManagePromptHistory = platform.caps.wailsBindings && Boolean(platform.promptHistory)
// Editor mirrors what the runtime template bar shows: the stored list if
// non-empty, otherwise the bundled DEFAULT_TEMPLATES (via effectiveTemplates).
// What you see in Settings = what you see in the bar. The first customization
// (edit / add / reorder / delete) calls persist() which writes the visible
// list, naturally locking the defaults into the user's stored set.
async function reload() {
  const [storedQuick, snippets] = await Promise.all([
    platform.templates.load(),
    platform.snippets.load(),
  ])
  const snippetIDs = new Set(snippets.map((item) => item.id))
  hasQuickSnippetCollision.value = storedQuick.some((item) => snippetIDs.has(item.id))
  const quick = (storedQuick.length > 0 ? storedQuick : DEFAULT_TEMPLATES)
    .filter((item) => !snippetIDs.has(item.id))
    .map<TemplateItem>((item) => ({ ...item, kind: 'quick' }))
  items.value = [
    ...quick,
    ...snippets.map<TemplateItem>((item) => ({ ...item, kind: 'snippet' })),
  ]
  hidden.value = await platform.templates.loadHidden()
  await reloadPromptHistory()
}

async function reloadPromptHistory() {
  if (!canManagePromptHistory || !platform.promptHistory) return
  try {
    historyStatus.value = await platform.promptHistory.status()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

function notifyPromptHistoryChanged() {
  platform.events.emit('promptHistory:changed', null)
}

async function onPromptHistoryToggle(enabled: boolean) {
  if (!platform.promptHistory) return
  const previous = historyStatus.value.enabled
  historyStatus.value.enabled = enabled
  error.value = ''
  try {
    await platform.promptHistory.setEnabled(enabled)
    await reloadPromptHistory()
    notifyPromptHistoryChanged()
  } catch (e) {
    historyStatus.value.enabled = previous
    error.value = e instanceof Error ? e.message : String(e)
  }
}

function startPromptHistoryClear() { historyClearOpen.value = true }
function cancelPromptHistoryClear() { historyClearOpen.value = false }
async function confirmPromptHistoryClear() {
  if (!platform.promptHistory) return
  historyClearOpen.value = false
  error.value = ''
  try {
    await platform.promptHistory.clear()
    await reloadPromptHistory()
    notifyPromptHistoryChanged()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
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

function quickValues(nextItems: TemplateItem[]): QuickTemplate[] {
  return nextItems
    .filter((item) => item.kind === 'quick')
    .map(({ id, label, text, hotkey }) => ({
      id,
      label,
      text,
      ...(hotkey ? { hotkey } : {}),
    }))
}

function snippetValues(nextItems: TemplateItem[]): Snippet[] {
  return nextItems
    .filter((item) => item.kind === 'snippet')
    .map(({ id, label, text }) => ({ id, label, text }))
}

function validateSnippetList(snippets: Snippet[]): string {
  if (snippets.length > 200) return t('settings.templates.validation.tooMany')
  for (const snippet of snippets) {
    if (Array.from(snippet.label).length > 80) return t('settings.templates.validation.labelTooLong')
    if (new TextEncoder().encode(snippet.text).byteLength > 64 * 1024) return t('settings.templates.validation.textTooLong')
  }
  if (snippetCollectionJSONBytes(snippets) > 1024 * 1024) {
    return t('settings.templates.validation.collectionTooLarge')
  }
  return ''
}

async function persist(nextItems: TemplateItem[], kinds: readonly TemplateKind[]): Promise<boolean> {
  error.value = ''
  try {
    const snippets = snippetValues(nextItems)
    const validationError = validateSnippetList(snippets)
    if (validationError) {
      error.value = validationError
      return false
    }
    const orderedKinds = [...new Set(kinds)]
    if (hasQuickSnippetCollision.value && !orderedKinds.includes('quick')) {
      orderedKinds.push('quick')
    }
    for (const kind of orderedKinds) {
      if (kind === 'snippet') {
        await platform.snippets.save(snippets)
        continue
      }
      await platform.templates.save(quickValues(nextItems))
      hasQuickSnippetCollision.value = false
    }
    items.value = nextItems
    notifyChanged()
    return true
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
    return false
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
  const kind = activeKind.value
  editing.value = { id: crypto.randomUUID(), kind, originalKind: kind, label: '', text: '', hotkey: '', isNew: true }
}
function startEdit(tpl: TemplateItem) {
  editing.value = {
    id: tpl.id,
    kind: tpl.kind,
    originalKind: tpl.kind,
    label: tpl.label,
    text: tpl.text,
    hotkey: tpl.hotkey || '',
    isNew: false,
  }
}
function cancelEdit() { editing.value = null }
function selectKind(kind: TemplateKind) {
  activeKind.value = kind
  editing.value = null
  error.value = ''
  resetOpen.value = false
}
function setEditingKind(kind: TemplateKind) {
  if (!editing.value) return
  editing.value.kind = kind
}

function insertAtKindEnd(list: TemplateItem[], item: TemplateItem): TemplateItem[] {
  const next = list.filter((entry) => entry.id !== item.id)
  let target = -1
  for (let i = 0; i < next.length; i += 1) {
    if (next[i].kind === item.kind) target = i
  }
  next.splice(target + 1, 0, item)
  return next
}

async function saveEdit() {
  if (!editing.value) return
  const e = editing.value
  if (!e.label.trim() || !e.text.trim()) {
    error.value = t('settings.templates.validation.required')
    return
  }
  const next: TemplateItem = { id: e.id, kind: e.kind, label: e.label.trim(), text: e.text }
  if (e.kind === 'quick' && e.hotkey.trim()) next.hotkey = e.hotkey.trim()
  let nextItems: TemplateItem[]
  if (e.isNew || e.originalKind !== e.kind) {
    nextItems = insertAtKindEnd(items.value, next)
  } else {
    nextItems = items.value.map((item) => item.id === e.id ? next : item)
  }
  const kinds: TemplateKind[] = [e.kind]
  if (!e.isNew && e.originalKind !== e.kind) kinds.push(e.originalKind)
  if (await persist(nextItems, kinds)) {
    activeKind.value = e.kind
    editing.value = null
  }
}
async function deleteItem(id: string) {
  const item = items.value.find((entry) => entry.id === id)
  if (!item) return
  await persist(items.value.filter((entry) => entry.id !== id), [item.kind])
}
function siblingIndex(id: string, direction: -1 | 1): number {
  const index = items.value.findIndex((item) => item.id === id)
  if (index < 0) return -1
  for (let i = index + direction; i >= 0 && i < items.value.length; i += direction) {
    if (items.value[i].kind === items.value[index].kind) return i
  }
  return -1
}
function canMove(id: string, direction: -1 | 1) { return siblingIndex(id, direction) >= 0 }
async function moveUp(id: string) {
  const idx = items.value.findIndex((x) => x.id === id)
  const other = siblingIndex(id, -1)
  if (idx < 0 || other < 0) return
  const next = [...items.value]
  ;[next[other], next[idx]] = [next[idx], next[other]]
  await persist(next, [next[other].kind])
}
async function moveDown(id: string) {
  const idx = items.value.findIndex((x) => x.id === id)
  const other = siblingIndex(id, 1)
  if (idx < 0 || other < 0) return
  const next = [...items.value]
  ;[next[idx], next[other]] = [next[other], next[idx]]
  await persist(next, [next[idx].kind])
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

    <section v-if="canManagePromptHistory" class="history-settings">
      <div class="history-heading">{{ t('settings.templates.history.title') }}</div>
      <label class="show-toggle">
        <input
          type="checkbox"
          data-testid="prompt-history-toggle"
          :checked="historyStatus.enabled"
          @change="onPromptHistoryToggle(($event.target as HTMLInputElement).checked)"
        />
        <span>{{ t('settings.templates.history.enabled') }}</span>
      </label>
      <div class="history-meta">
        <span data-testid="prompt-history-count">{{ t('settings.templates.history.count', { count: historyStatus.count }) }}</span>
        <button
          type="button"
          class="del"
          data-testid="prompt-history-clear"
          :disabled="historyStatus.count === 0"
          @click="startPromptHistoryClear"
        >{{ t('settings.templates.history.clear') }}</button>
      </div>
    </section>

    <div
      class="template-kind-tabs"
      role="tablist"
      :aria-label="t('settings.templates.kind.label')"
      data-testid="template-kind-tabs"
    >
      <button
        type="button"
        role="tab"
        data-testid="template-tab-quick"
        :aria-selected="activeKind === 'quick'"
        :class="{ active: activeKind === 'quick' }"
        @click="selectKind('quick')"
      >{{ t('settings.templates.kind.quick') }}</button>
      <button
        type="button"
        role="tab"
        data-testid="template-tab-snippet"
        :aria-selected="activeKind === 'snippet'"
        :class="{ active: activeKind === 'snippet' }"
        @click="selectKind('snippet')"
      >{{ t('settings.templates.kind.snippets') }}</button>
    </div>

    <ul v-if="visibleItems.length > 0" class="list">
      <li
        v-for="it in visibleItems"
        :key="it.id"
        class="row"
        :data-testid="`template-row-${it.id}`"
        :data-kind="it.kind"
      >
        <span class="label" v-tooltip="it.label">{{ it.label }}</span>
        <code class="text" v-tooltip="it.text">{{ it.text }}</code>
        <code class="hotkey">{{ it.kind === 'quick' ? (it.hotkey || '—') : '' }}</code>
        <div class="actions">
          <button :disabled="!canMove(it.id, -1)" @click="moveUp(it.id)">↑</button>
          <button :disabled="!canMove(it.id, 1)" @click="moveDown(it.id)">↓</button>
          <button :data-testid="`template-edit-${it.id}`" @click="startEdit(it)">{{ t('settings.templates.edit') }}</button>
          <button class="del" :data-testid="`template-delete-${it.id}`" @click="deleteItem(it.id)">
            {{ t('settings.templates.delete') }}
          </button>
        </div>
      </li>
    </ul>
    <p v-else class="template-empty" data-testid="template-empty">
      {{ t(activeKind === 'quick' ? 'settings.templates.emptyQuick' : 'settings.templates.emptySnippet') }}
    </p>

    <div v-if="editing" class="edit-row">
      <div class="kind-switch" role="group" :aria-label="t('settings.templates.kind.label')">
        <button
          type="button"
          data-testid="template-kind-quick"
          :class="{ active: editing.kind === 'quick' }"
          @click="setEditingKind('quick')"
        >{{ t('settings.templates.kind.quick') }}</button>
        <button
          type="button"
          data-testid="template-kind-snippet"
          :class="{ active: editing.kind === 'snippet' }"
          @click="setEditingKind('snippet')"
        >{{ t('settings.templates.kind.snippet') }}</button>
      </div>
      <div class="edit-top">
        <input
          v-model="editing.label"
          class="edit-label-input"
          :placeholder="t('settings.templates.label')"
          data-testid="template-edit-label"
        />
        <input
          v-if="editing.kind === 'quick'"
          v-model="editing.hotkey"
          class="edit-hotkey-input"
          :placeholder="t('settings.templates.hotkey')"
          data-testid="template-edit-hotkey"
        />
      </div>
      <textarea
        v-model="editing.text"
        class="edit-text-input"
        :rows="editing.kind === 'snippet' ? 7 : 3"
        :placeholder="t('settings.templates.text')"
        data-testid="template-edit-text"
      />
      <div class="edit-actions">
        <button @click="cancelEdit">{{ t('common.cancel') }}</button>
        <button class="primary" data-testid="template-edit-save" @click="saveEdit">{{ t('settings.templates.save') }}</button>
      </div>
    </div>

    <div class="footer">
      <button data-testid="template-add" @click="startAdd">
        {{ t(activeKind === 'quick' ? 'settings.templates.addQuick' : 'settings.templates.addSnippet') }}
      </button>
      <button v-if="activeKind === 'quick'" data-testid="template-reset" @click="startReset">
        {{ t('settings.templates.reset') }}
      </button>
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

    <div v-if="historyClearOpen" class="dialog-backdrop" @click="cancelPromptHistoryClear">
      <div class="dialog" @click.stop>
        <p>{{ t('settings.templates.history.clearConfirm') }}</p>
        <div class="actions">
          <button @click="cancelPromptHistoryClear">{{ t('common.cancel') }}</button>
          <button class="del" data-testid="prompt-history-clear-confirm" @click="confirmPromptHistoryClear">{{ t('settings.templates.history.clear') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tab-pane { display: flex; flex-direction: column; gap: 10px; }
.hint { font-size: 12px; color: var(--fg-dim); margin: 0; line-height: 1.5; }
.show-toggle { display: inline-flex; align-items: center; gap: 8px; font-size: 13px; color: var(--fg); }
.history-settings { display: flex; flex-direction: column; gap: 8px; padding: 10px 0; border-top: 1px solid var(--border); border-bottom: 1px solid var(--border); }
.history-heading { font-size: 13px; font-weight: 600; color: var(--fg); }
.history-meta { display: flex; align-items: center; justify-content: space-between; gap: 8px; color: var(--fg-dim); font-size: 12px; }
.history-meta button { height: 26px; padding: 0 9px; }
.del { color: var(--bad); border-color: rgba(248, 81, 73, 0.4); }
.template-kind-tabs { display: flex; gap: 2px; border-bottom: 1px solid var(--border); }
.template-kind-tabs button { min-width: 7rem; height: 32px; padding: 0 12px; border: 0; border-bottom: 2px solid transparent; border-radius: 0; color: var(--fg-dim); background: transparent; }
.template-kind-tabs button.active { color: var(--fg); border-bottom-color: var(--accent); }
.template-kind-tabs button:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
.list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.template-empty { margin: 0; padding: 28px 12px; border: 1px dashed var(--border); border-radius: 6px; color: var(--fg-dim); text-align: center; font-size: 0.78rem; }
.row { display: grid; grid-template-columns: 7rem minmax(8rem, 1fr) 5rem auto; gap: 8px; align-items: center; padding: 8px 10px; background: var(--bg); border: 1px solid var(--border); border-radius: 6px; }
.label { font-weight: 600; font-family: var(--font-mono); font-size: 0.85rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.text { font-family: var(--font-mono); font-size: 0.78rem; color: var(--fg-dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.hotkey { font-family: var(--font-mono); font-size: 0.74rem; color: var(--fg-dim); text-align: center; }
.actions { display: flex; gap: 4px; }
.actions button { height: 24px; padding: 0 8px; font-size: 0.74rem; }
.actions .del { color: var(--bad); border-color: rgba(248, 81, 73, 0.4); }
.edit-row { display: flex; flex-direction: column; gap: 8px; padding: 10px; background: var(--panel); border: 1px solid var(--accent); border-radius: 6px; }
.kind-switch { display: inline-flex; align-self: flex-start; border: 1px solid var(--border); border-radius: 5px; overflow: hidden; }
.kind-switch button { height: 28px; padding: 0 12px; border: 0; border-radius: 0; }
.kind-switch button + button { border-left: 1px solid var(--border); }
.kind-switch button.active { background: var(--accent); color: #0d1117; }
.edit-top { display: grid; grid-template-columns: 1fr 8rem; gap: 8px; }
.edit-row input, .edit-row textarea { padding: 6px 8px; background: var(--bg); color: var(--fg); border: 1px solid var(--border); border-radius: 4px; font-size: 0.85rem; font-family: var(--font-mono); width: 100%; box-sizing: border-box; }
.edit-row input { height: 28px; }
.edit-row textarea { resize: vertical; min-height: 60px; line-height: 1.4; }
.edit-actions { display: flex; gap: 6px; justify-content: flex-end; }
.edit-actions button { height: 28px; padding: 0 14px; font-size: 0.78rem; }
.edit-actions .primary { background: var(--accent); color: #0d1117; border-color: var(--accent); font-weight: 600; }
.footer { display: flex; gap: 8px; align-items: center; }
.error { color: var(--bad); font-size: 0.75rem; }
@media (max-width: 760px) {
  .row { grid-template-columns: minmax(7rem, 0.35fr) minmax(0, 1fr); }
  .text, .hotkey, .actions { grid-column: 1 / -1; }
  .actions { flex-wrap: wrap; }
  .edit-top { grid-template-columns: 1fr; }
}
.dialog-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.55); display: flex; align-items: center; justify-content: center; z-index: 50; }
.dialog { background: #11182b; padding: 14px 18px; border: 1px solid #1e2638; border-radius: 11px; max-width: 320px; display: flex; flex-direction: column; gap: 10px; color: #e6e7ea; }

@media (max-width: 640px) {
  .row {
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .text {
    grid-column: 1 / -1;
    grid-row: 2;
  }
  .hotkey {
    grid-column: 2;
    grid-row: 1;
  }
  .actions {
    grid-column: 1 / -1;
    flex-wrap: wrap;
  }
  .edit-top {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
