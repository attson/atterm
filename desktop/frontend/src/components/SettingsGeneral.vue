<script lang="ts" setup>
import { onMounted, ref } from "vue";
import {
  getNotificationsEnabled,
  setNotificationsEnabled,
  setTerminalThemePreference,
} from "../lib/api";
import { TERMINAL_THEMES, getTerminalTheme } from "../lib/terminalThemes";

const props = defineProps<{
  terminalThemeId: string;
}>();

const emit = defineEmits<{
  (e: "terminal-theme-changed", themeID: string): void;
}>();

const selected = ref(getTerminalTheme(props.terminalThemeId).id);
const persisted = ref(selected.value);
const saving = ref(false);
const error = ref("");

const notificationsEnabled = ref(true);
const notificationsLoading = ref(true);

onMounted(async () => {
  try {
    notificationsEnabled.value = await getNotificationsEnabled();
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    notificationsLoading.value = false;
  }
});

async function onNotificationsToggle(e: Event) {
  const target = e.target as HTMLInputElement;
  const previous = notificationsEnabled.value;
  notificationsEnabled.value = target.checked;
  error.value = "";
  try {
    await setNotificationsEnabled(target.checked);
  } catch (e: any) {
    notificationsEnabled.value = previous;
    error.value = e?.message ?? String(e);
  }
}

async function onChange() {
  const nextTheme = getTerminalTheme(selected.value).id;
  const previous = persisted.value;
  selected.value = nextTheme;
  saving.value = true;
  error.value = "";
  try {
    await setTerminalThemePreference(nextTheme);
    persisted.value = nextTheme;
    emit("terminal-theme-changed", nextTheme);
  } catch (e: any) {
    selected.value = previous;
    emit("terminal-theme-changed", previous);
    error.value = e?.message ?? String(e);
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <div class="tab-pane">
    <label class="field-label">terminal theme</label>
    <select v-model="selected" :disabled="saving" @change="onChange">
      <option
        v-for="theme in TERMINAL_THEMES"
        :key="theme.id"
        :value="theme.id"
      >
        {{ theme.label }} — {{ theme.description }}
      </option>
    </select>
    <p class="hint">
      Applies to all terminal panes immediately and is saved as your local desktop preference.
    </p>

    <label class="checkbox" v-if="!notificationsLoading">
      <input
        type="checkbox"
        :checked="notificationsEnabled"
        @change="onNotificationsToggle"
      />
      Show system notifications on terminal bell
    </label>
    <p class="hint" v-if="!notificationsLoading">
      Only fires when the AT Term window is not focused.
    </p>

    <p v-if="error" class="error">{{ error }}</p>
  </div>
</template>

<style scoped>
.tab-pane {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.field-label {
  font-size: 12px;
  color: var(--fg-dim);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.hint {
  font-size: 12px;
  color: var(--fg-dim);
  margin: 0;
  line-height: 1.5;
}
.error {
  color: var(--bad);
  font-size: 12px;
  margin: 0;
}
.checkbox {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--fg);
}
select {
  height: 32px;
  padding: 6px 10px;
  background: var(--bg);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 6px;
  font-size: 13px;
}
select:focus {
  outline: none;
  box-shadow: 0 0 0 2px var(--accent);
}
</style>
