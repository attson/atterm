<script lang="ts" setup>
import { computed, onMounted, ref } from "vue";
import { getShortcutBindings, setShortcutBindings } from "../lib/api";
import {
  ACTIONS,
  ACTION_BY_ID,
  conflictsWith,
  resolvedBindings,
  type Mod,
  type ShortcutAction,
} from "../lib/shortcutBindings";
import HotkeyCaptureCell from "./HotkeyCaptureCell.vue";
import { modKey } from "../lib/modKey";
import { useI18n } from "../i18n/useI18n";

function detectMod(): Mod {
  return modKey();
}

const props = defineProps<{
  // Allow tests to inject Control; production uses detectMod().
  mod?: Mod;
}>();

// bindings-changed lets App.vue's useTerminalShortcuts pick up a save
// immediately, without the two of them sharing a reactive store the way
// usePluginConfigStore used to provide for free (see the App.vue side of
// this same swap, onBindingsChanged).
const emit = defineEmits<{
  (e: "bindings-changed", bindings: Record<string, string>): void;
}>();

const mod = computed<Mod>(() => props.mod ?? detectMod());

const { t } = useI18n();
// persisted mirrors what Go last confirmed (via GetShortcutBindings on load,
// or the payload of a successful SetShortcutBindings); draft is the editable
// working copy. There is no reactive store to watch anymore — the panel
// re-reads on every mount, matching SettingsTerminalAppearance.vue.
const persisted = ref<Record<string, string>>({});
const draft = ref<Record<string, string>>({});
const error = ref("");

function loadDraft() {
  draft.value = JSON.parse(JSON.stringify(persisted.value));
}

onMounted(async () => {
  try {
    persisted.value = await getShortcutBindings();
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  }
  loadDraft();
});

const dirty = computed(() => {
  return JSON.stringify(persisted.value) !== JSON.stringify(draft.value);
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
  // Strip entries equal to defaults (defensive — already handled in onCellUpdate
  // but a user could have reached this state via resetRow + manual save flow).
  const normalized: Record<string, string> = {};
  for (const [id, value] of Object.entries(draft.value)) {
    if (id in ACTION_BY_ID && value !== ACTION_BY_ID[id]!.defaultBinding) {
      normalized[id] = value;
    }
  }
  error.value = "";
  try {
    // setShortcutBindings only — this must never round-trip through the
    // plugin config (App.SetPluginConfig), or it would resurrect the legacy
    // Plugins.Shortcuts.Bindings slot that the Go-side migration just
    // stopped clearing on every load (desktop/config.go migrateShortcutBindings).
    await setShortcutBindings(normalized);
    persisted.value = normalized;
    emit("bindings-changed", normalized);
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  }
}

function discard() {
  loadDraft();
}

const paneActions = ACTIONS.filter((a) => a.group === "pane");
const tabActions = ACTIONS.filter((a) => a.group === "tab");
const sidebarActions = ACTIONS.filter((a) => a.group === "sidebar");

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

    <section class="shortcut-group" v-if="sidebarActions.length > 0">
      <h3>{{ t("settings.shortcuts.sidebar") }}</h3>
      <div v-for="action in sidebarActions" :key="action.id" class="shortcut-row">
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

    <p v-if="error" class="error">{{ error }}</p>
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
.error {
  color: #f85149;
  font-size: 12px;
  margin: 8px 0 0;
}

@media (max-width: 640px) {
  .shortcut-row {
    grid-template-columns: minmax(0, 1fr) minmax(0, 140px) auto;
  }
  .shortcut-row :deep(.hotkey-cell) {
    min-width: 0;
    width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .actions-row {
    flex-wrap: wrap;
  }
}
</style>
