<script setup lang="ts">
import { ref } from 'vue'

// EditorRow is the generic shape both the template editor (value = text) and
// the aux-key editor (value = raw byte seq) map onto. The parent owns the
// real model and maps to/from this shape.
export interface EditorRow { id: string; label: string; value: string }

const props = defineProps<{
  rows: EditorRow[]
  labelText: string
  valueText: string
  addText: string
  editText: string
  deleteText: string
  resetText: string
  cancelText: string
  saveText: string
  resetConfirmText: string
  testid: string
  // displayValue/parseValue let the aux editor show "\x1b" while storing the
  // ESC byte; the template editor leaves them as identity.
  displayValue?: (v: string) => string
  parseValue?: (v: string) => string
}>()

const emit = defineEmits<{
  (e: 'update:rows', rows: EditorRow[]): void
  (e: 'reset'): void
}>()

const disp = (v: string) => (props.displayValue ? props.displayValue(v) : v)
const parse = (v: string) => (props.parseValue ? props.parseValue(v) : v)

const editing = ref<{ id: string; label: string; value: string; isNew: boolean } | null>(null)
const resetOpen = ref(false)

function commit(next: EditorRow[]) { emit('update:rows', next) }

function startAdd() { editing.value = { id: crypto.randomUUID(), label: '', value: '', isNew: true } }
function startEdit(r: EditorRow) { editing.value = { id: r.id, label: r.label, value: disp(r.value), isNew: false } }
function cancelEdit() { editing.value = null }
function saveEdit() {
  if (!editing.value) return
  const e = editing.value
  if (!e.label.trim() || !e.value) return
  const row: EditorRow = { id: e.id, label: e.label.trim(), value: parse(e.value) }
  if (e.isNew) commit([...props.rows, row])
  else commit(props.rows.map((x) => (x.id === e.id ? row : x)))
  editing.value = null
}
function del(id: string) { commit(props.rows.filter((x) => x.id !== id)) }
function moveUp(i: number) {
  if (i <= 0) return
  const next = [...props.rows]
  ;[next[i - 1], next[i]] = [next[i], next[i - 1]]
  commit(next)
}
function moveDown(i: number) {
  if (i >= props.rows.length - 1) return
  const next = [...props.rows]
  ;[next[i], next[i + 1]] = [next[i + 1], next[i]]
  commit(next)
}
function startReset() { resetOpen.value = true }
function confirmReset() { resetOpen.value = false; editing.value = null; emit('reset') }
function cancelReset() { resetOpen.value = false }
</script>

<template>
  <div class="list-editor" :data-testid="testid">
    <ul class="rows">
      <li
        v-for="(r, i) in rows"
        :key="r.id"
        class="row"
        :data-testid="`${testid}-row-${r.id}`"
      >
        <span class="label">{{ r.label }}</span>
        <code class="value">{{ disp(r.value) }}</code>
        <div class="actions">
          <button class="mini" :disabled="i === 0" @click="moveUp(i)" aria-label="up">↑</button>
          <button class="mini" :disabled="i === rows.length - 1" @click="moveDown(i)" aria-label="down">↓</button>
          <button class="mini" :data-testid="`${testid}-edit-${r.id}`" @click="startEdit(r)">{{ editText }}</button>
          <button class="mini del" :data-testid="`${testid}-delete-${r.id}`" @click="del(r.id)">{{ deleteText }}</button>
        </div>
      </li>
    </ul>

    <div v-if="editing" class="edit" :data-testid="`${testid}-edit-form`">
      <input v-model="editing.label" :placeholder="labelText" :data-testid="`${testid}-edit-label`" autocapitalize="off" spellcheck="false" />
      <input v-model="editing.value" :placeholder="valueText" :data-testid="`${testid}-edit-value`" autocapitalize="off" spellcheck="false" />
      <div class="edit-actions">
        <button @click="cancelEdit">{{ cancelText }}</button>
        <button class="primary" :data-testid="`${testid}-edit-save`" @click="saveEdit">{{ saveText }}</button>
      </div>
    </div>

    <div class="footer">
      <button :data-testid="`${testid}-add`" @click="startAdd">{{ addText }}</button>
      <button :data-testid="`${testid}-reset`" @click="startReset">{{ resetText }}</button>
    </div>

    <div v-if="resetOpen" class="backdrop" @click="cancelReset">
      <div class="dialog" @click.stop>
        <p>{{ resetConfirmText }}</p>
        <div class="edit-actions">
          <button @click="cancelReset">{{ cancelText }}</button>
          <button class="primary" :data-testid="`${testid}-reset-confirm`" @click="confirmReset">{{ resetText }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.list-editor { display: flex; flex-direction: column; gap: 8px; }
.rows { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.row { display: grid; grid-template-columns: minmax(0, 6rem) minmax(0, 1fr) auto; gap: 8px; align-items: center; padding: 8px 10px; background: #11182b; border: 1px solid #1e2638; border-radius: 9px; }
.label { font-weight: 600; font-family: var(--font-mono-strict); font-size: 0.85rem; color: #e6e7ea; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
.value { font-family: var(--font-mono-strict); font-size: 0.78rem; color: #8d93a3; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
.actions { display: flex; gap: 4px; }
.mini { height: 30px; min-width: 30px; padding: 0 8px; border-radius: 7px; background: #0b1020; border: 1px solid #1e2638; color: #cbd5e1; font-size: 0.74rem; }
.mini:disabled { opacity: 0.4; }
.mini.del { color: #f87171; border-color: rgba(248,113,113,.4); }
.edit { display: flex; flex-direction: column; gap: 8px; padding: 10px; background: #0b1020; border: 1px solid #3b82f6; border-radius: 9px; }
.edit input { height: 40px; border-radius: 9px; border: 1px solid #1e2638; background: #11182b; color: #e6e7ea; padding: 0 12px; font-size: 0.95rem; font-family: var(--font-mono-strict); }
.edit-actions { display: flex; gap: 8px; justify-content: flex-end; }
.edit-actions button, .footer button { height: 38px; padding: 0 14px; border-radius: 9px; border: 1px solid #1e2638; background: #11182b; color: #cbd5e1; font-size: 0.85rem; }
.edit-actions .primary { background: #3b82f6; color: #fff; border: none; }
.footer { display: flex; gap: 8px; }
.footer button { flex: 1; }
.backdrop { position: fixed; inset: 0; background: rgba(0,0,0,.6); display: flex; align-items: center; justify-content: center; z-index: 50; padding: 1.5rem; }
.dialog { background: #11182b; border: 1px solid #1e2638; border-radius: 12px; padding: 16px; max-width: 320px; display: flex; flex-direction: column; gap: 12px; color: #e6e7ea; font-size: 0.9rem; }
.dialog .primary { background: #3b82f6; color: #fff; border: none; }
</style>
