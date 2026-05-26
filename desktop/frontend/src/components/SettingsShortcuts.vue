<script lang="ts" setup>
import { computed, onMounted, ref, watch } from "vue";
import { usePluginConfigStore } from "../plugins/configStore";
import {
  ACTIONS,
  ACTION_BY_ID,
  conflictsWith,
  resolvedBindings,
  type Mod,
  type ShortcutAction,
} from "../lib/shortcutBindings";
import HotkeyCaptureCell from "./HotkeyCaptureCell.vue";
import { useI18n } from "../i18n/useI18n";

function detectMod(): Mod {
  if (typeof navigator === "undefined") return "Control";
  return navigator.platform?.toLowerCase().includes("mac") ? "Meta" : "Control";
}

const props = defineProps<{
  // Allow tests to inject Control; production uses detectMod().
  mod?: Mod;
}>();

const mod = computed<Mod>(() => props.mod ?? detectMod());

const store = usePluginConfigStore();
const draft = ref<Record<string, string>>({});
const { t } = useI18n();

function loadDraft() {
  draft.value = JSON.parse(JSON.stringify(store.cfg?.shortcuts?.bindings ?? {}));
}

onMounted(async () => {
  if (!store.cfg) await store.load();
  loadDraft();
});

watch(
  () => store.cfg?.shortcuts?.bindings,
  () => { if (!dirty.value) loadDraft(); },
  { deep: true },
);

const dirty = computed(() => {
  const cur = store.cfg?.shortcuts?.bindings ?? {};
  return JSON.stringify(cur) !== JSON.stringify(draft.value);
});

// Fully-resolved bindings (defaults + draft overrides) for display and
// conflict detection.
const resolved = computed(() => resolvedBindings(draft.value));

function bindingFor(action: ShortcutAction): string {
  return resolved.value[action.id] ?? action.defaultBinding;
}

function conflictsFor(action: ShortcutAction): string[] {
  return conflictsWith(resolved.value, action.id);
}

const anyConflict = computed(() =>
  ACTIONS.some((a) => conflictsFor(a).length > 0),
);

function onCellUpdate(action: ShortcutAction, value: string) {
  if (value === action.defaultBinding) {
    // Equal to default — remove the override so the entry stays clean.
    const next = { ...draft.value };
    delete next[action.id];
    draft.value = next;
    return;
  }
  draft.value = { ...draft.value, [action.id]: value };
}

function resetRow(action: ShortcutAction) {
  const next = { ...draft.value };
  delete next[action.id];
  draft.value = next;
}

function resetAll() {
  draft.value = {};
}

async function save() {
  if (!store.cfg) return;
  const next = JSON.parse(JSON.stringify(store.cfg));
  // Strip entries equal to defaults (defensive — already handled in onCellUpdate
  // but a user could have reached this state via resetRow + manual save flow).
  const normalized: Record<string, string> = {};
  for (const [id, value] of Object.entries(draft.value)) {
    if (id in ACTION_BY_ID && value !== ACTION_BY_ID[id]!.defaultBinding) {
      normalized[id] = value;
    }
  }
  next.shortcuts.bindings = normalized;
  await store.save(next);
}

function discard() {
  loadDraft();
}

const paneActions = ACTIONS.filter((a) => a.group === "pane");
const tabActions = ACTIONS.filter((a) => a.group === "tab");

defineExpose({ dirty });
</script>

<template>
  <div class="shortcut-settings">
    <section class="shortcut-group">
      <h3>{{ t("settings.shortcuts.pane") }}</h3>
      <div v-for="action in paneActions" :key="action.id" class="shortcut-row">
        <div class="label">{{ t(action.labelKey) }}</div>
        <HotkeyCaptureCell
          :value="bindingFor(action)"
          :mod="mod"
          @update="(v) => onCellUpdate(action, v)"
        />
        <button class="reset-row" :title="t('settings.shortcuts.resetTo', { binding: action.defaultBinding })" @click="resetRow(action)">↺</button>
        <div class="conflict" v-if="conflictsFor(action).length">
          {{ t("settings.shortcuts.conflictsWith", { labels: conflictsFor(action).map((id) => ACTION_BY_ID[id] ? t(ACTION_BY_ID[id]!.labelKey) : id).join(", ") }) }}
        </div>
      </div>
    </section>

    <section class="shortcut-group">
      <h3>{{ t("settings.shortcuts.tab") }}</h3>
      <div v-for="action in tabActions" :key="action.id" class="shortcut-row">
        <div class="label">{{ t(action.labelKey) }}</div>
        <HotkeyCaptureCell
          :value="bindingFor(action)"
          :mod="mod"
          @update="(v) => onCellUpdate(action, v)"
        />
        <button class="reset-row" :title="t('settings.shortcuts.resetTo', { binding: action.defaultBinding })" @click="resetRow(action)">↺</button>
        <div class="conflict" v-if="conflictsFor(action).length">
          {{ t("settings.shortcuts.conflictsWith", { labels: conflictsFor(action).map((id) => ACTION_BY_ID[id] ? t(ACTION_BY_ID[id]!.labelKey) : id).join(", ") }) }}
        </div>
      </div>
    </section>

    <div class="actions-row">
      <button class="reset-all" @click="resetAll">{{ t("settings.shortcuts.resetAll") }}</button>
      <div class="spacer" />
      <button class="discard" :disabled="!dirty" @click="discard">{{ t("common.discard") }}</button>
      <button class="save" :disabled="!dirty || anyConflict" @click="save">{{ t("common.save") }}</button>
    </div>
  </div>
</template>

<style scoped>
.shortcut-settings { padding: 8px 4px; font-size: 12px; color: var(--fg); }
.shortcut-group { margin-bottom: 18px; }
.shortcut-group h3 {
  margin: 4px 0 8px;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--fg-dim);
}
.shortcut-row {
  display: grid;
  grid-template-columns: 1fr auto auto;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
}
.label { color: var(--fg); }
.reset-row {
  background: transparent;
  border: 1px solid #2d333b;
  color: var(--fg-dim);
  padding: 2px 8px;
  border-radius: 4px;
  cursor: pointer;
}
.reset-row:hover { background: rgba(255, 255, 255, 0.04); color: var(--fg); }
.conflict {
  grid-column: 1 / -1;
  color: #f85149;
  font-size: 11px;
  padding-left: 4px;
}
.actions-row {
  display: flex;
  gap: 8px;
  margin-top: 12px;
  align-items: center;
}
.spacer { flex: 1; }
.actions-row button {
  background: #21262d;
  border: 1px solid #2d333b;
  color: #c9d1d9;
  padding: 4px 12px;
  border-radius: 4px;
  cursor: pointer;
}
.actions-row button:disabled { opacity: 0.4; cursor: default; }
.actions-row .save { background: var(--accent); color: #0d1117; border-color: var(--accent); }
.actions-row .save:disabled { background: #21262d; color: #c9d1d9; border-color: #2d333b; }
</style>
