<script lang="ts" setup>
import { computed, onMounted, ref, watch } from "vue";
import { usePluginConfigStore, type QuickInputButton } from "../configStore";
import { conflictsWith } from "./hotkeyConflict";
import { useI18n } from "../../i18n/useI18n";

const store = usePluginConfigStore();
const draft = ref<QuickInputButton[]>([]);
const error = ref<string>("");
const { t } = useI18n();

function loadDraft() {
  draft.value = JSON.parse(JSON.stringify(store.cfg?.quickInput.buttons ?? []));
}

onMounted(async () => {
  if (!store.cfg) await store.load();
  loadDraft();
});

watch(() => store.cfg?.quickInput.buttons, () => {
  if (!dirty.value) loadDraft();
}, { deep: true });

const dirty = computed(() => {
  const cur = store.cfg?.quickInput.buttons ?? [];
  return JSON.stringify(cur) !== JSON.stringify(draft.value);
});

let counter = 0;
function newID() {
  return `qib-${Date.now()}-${counter++}`;
}

function addButton() {
  draft.value.push({ id: newID(), label: t("plugins.quickInput.newButtonLabel"), send: "new", appendNewline: true });
}

function deleteAt(i: number) {
  draft.value.splice(i, 1);
}

function validate(): string | null {
  for (const b of draft.value) {
    if (!b.label.trim()) return t("plugins.quickInput.labelEmpty");
    if (b.hotkey && conflictsWith(draft.value, b.hotkey, b.id)) {
      return t("plugins.quickInput.hotkeyConflict", { label: b.label });
    }
  }
  return null;
}

async function save() {
  const v = validate();
  if (v) {
    error.value = v;
    return;
  }
  error.value = "";
  const next = JSON.parse(JSON.stringify(store.cfg));
  next.quickInput.buttons = JSON.parse(JSON.stringify(draft.value));
  try {
    await store.save(next);
  } catch (err) {
    error.value = (err as Error).message;
  }
}

function discard() {
  loadDraft();
  error.value = "";
}

defineExpose({ dirty });
</script>

<template>
  <div class="quick-input-settings">
    <table>
      <thead>
        <tr>
          <th>{{ t("plugins.quickInput.label") }}</th>
          <th>{{ t("plugins.quickInput.send") }}</th>
          <th>{{ t("plugins.quickInput.newline") }}</th>
          <th>{{ t("plugins.quickInput.hotkey") }}</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(b, i) in draft" :key="b.id" class="button-row">
          <td><input class="label" v-model="b.label" /></td>
          <td><input class="send" v-model="b.send" /></td>
          <td><input class="newline" type="checkbox" v-model="b.appendNewline" /></td>
          <td><input class="hotkey" v-model="b.hotkey" placeholder="Alt+1" /></td>
          <td><button class="delete" @click="deleteAt(i)">×</button></td>
        </tr>
      </tbody>
    </table>
    <div class="row-actions">
      <button class="add" @click="addButton">{{ t("plugins.quickInput.addButton") }}</button>
      <div class="spacer" />
      <button class="discard" :disabled="!dirty" @click="discard">{{ t("common.discard") }}</button>
      <button class="save" :disabled="!dirty" @click="save">{{ t("common.save") }}</button>
    </div>
    <div v-if="error" class="error">{{ error }}</div>
  </div>
</template>

<style scoped>
.quick-input-settings { padding: 8px 4px; font-size: 12px; }
table { width: 100%; border-collapse: collapse; }
th { text-align: left; font-weight: 500; padding: 4px 6px; opacity: 0.7; }
td { padding: 4px 6px; }
input { background: #0d1117; border: 1px solid #2d333b; color: #c9d1d9; padding: 2px 6px; border-radius: 3px; }
input.label, input.send, input.hotkey { width: 110px; }
.row-actions { display: flex; gap: 8px; margin-top: 8px; align-items: center; }
.spacer { flex: 1; }
button { background: #21262d; border: 1px solid #2d333b; color: #c9d1d9; padding: 2px 10px; border-radius: 3px; cursor: pointer; }
button:disabled { opacity: 0.4; cursor: default; }
.error { margin-top: 6px; color: #f85149; }
</style>
