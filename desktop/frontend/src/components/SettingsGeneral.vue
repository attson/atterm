<script lang="ts" setup>
import { computed, onMounted, ref } from "vue";
import {
  getCommandNotifyThresholdSeconds,
  getNotificationsEnabled,
  getShellIntegrationEnabled,
  getWebglRendererEnabled,
  setCommandNotifyThresholdSeconds,
  setNotificationsEnabled,
  setShellIntegrationEnabled,
  setWebglRendererEnabled,
  setTerminalThemePreference,
} from "../lib/api";
import { TERMINAL_THEMES, getTerminalTheme } from "../lib/terminalThemes";
import SelectDropdown from "./SelectDropdown.vue";

const props = defineProps<{
  terminalThemeId: string;
}>();

const emit = defineEmits<{
  (e: "terminal-theme-changed", themeID: string): void;
  (e: "command-notify-threshold-changed", seconds: number): void;
}>();

const selected = ref(getTerminalTheme(props.terminalThemeId).id);
const persisted = ref(selected.value);
const saving = ref(false);
const error = ref("");

const themeOptions = computed(() =>
  TERMINAL_THEMES.map((theme) => ({
    value: theme.id,
    label: theme.label,
    description: theme.description,
  })),
);

const notificationsEnabled = ref(true);
const notificationsLoading = ref(true);
const shellIntegrationEnabled = ref(true);
const shellIntegrationLoading = ref(true);
const webglRendererEnabled = ref(true);
const webglRendererLoading = ref(true);
const commandNotifyThresholdSec = ref(10);
const commandNotifyThresholdLoading = ref(true);

onMounted(async () => {
  try {
    notificationsEnabled.value = await getNotificationsEnabled();
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    notificationsLoading.value = false;
  }
  try {
    shellIntegrationEnabled.value = await getShellIntegrationEnabled();
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    shellIntegrationLoading.value = false;
  }
  try {
    webglRendererEnabled.value = await getWebglRendererEnabled();
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    webglRendererLoading.value = false;
  }
  try {
    commandNotifyThresholdSec.value = await getCommandNotifyThresholdSeconds();
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    commandNotifyThresholdLoading.value = false;
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

async function onShellIntegrationToggle(e: Event) {
  const target = e.target as HTMLInputElement;
  const previous = shellIntegrationEnabled.value;
  shellIntegrationEnabled.value = target.checked;
  error.value = "";
  try {
    await setShellIntegrationEnabled(target.checked);
  } catch (e: any) {
    shellIntegrationEnabled.value = previous;
    error.value = e?.message ?? String(e);
  }
}

async function onWebglRendererToggle(e: Event) {
  const target = e.target as HTMLInputElement;
  const previous = webglRendererEnabled.value;
  webglRendererEnabled.value = target.checked;
  error.value = "";
  try {
    await setWebglRendererEnabled(target.checked);
  } catch (e: any) {
    webglRendererEnabled.value = previous;
    error.value = e?.message ?? String(e);
  }
}

async function onCommandNotifyThresholdChange(e: Event) {
  const target = e.target as HTMLInputElement;
  const raw = Number(target.value);
  const next = Number.isFinite(raw) ? Math.max(1, Math.min(600, Math.round(raw))) : 10;
  const previous = commandNotifyThresholdSec.value;
  commandNotifyThresholdSec.value = next;
  target.value = String(next);
  error.value = "";
  try {
    await setCommandNotifyThresholdSeconds(next);
    emit("command-notify-threshold-changed", next);
  } catch (e: any) {
    commandNotifyThresholdSec.value = previous;
    target.value = String(previous);
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
    <SelectDropdown
      v-model="selected"
      :options="themeOptions"
      :disabled="saving"
      aria-label="terminal theme"
      @update:modelValue="onChange"
    />
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

    <label class="checkbox" v-if="!shellIntegrationLoading">
      <input
        type="checkbox"
        :checked="shellIntegrationEnabled"
        @change="onShellIntegrationToggle"
      />
      Enable shell integration
    </label>
    <p class="hint" v-if="!shellIntegrationLoading">
      Injects OSC 133 hooks into zsh / bash / fish / pwsh at session start so we can
      detect when a command finishes. Only affects new sessions.
    </p>

    <label class="checkbox" v-if="!webglRendererLoading">
      <input
        type="checkbox"
        :checked="webglRendererEnabled"
        @change="onWebglRendererToggle"
      />
      Use WebGL terminal renderer
    </label>
    <p class="hint" v-if="!webglRendererLoading">
      GPU-rasterized rendering keeps light themes free of cell ghosting on dense
      TUI output. Off by default on Linux because NVIDIA proprietary + X11 +
      WebKitGTK paint the cursor / last cell a frame late, which surfaces as
      visible typing lag. Affects new terminal sessions only.
    </p>

    <label class="field-label" v-if="!commandNotifyThresholdLoading">
      Command-finished notification threshold (seconds)
    </label>
    <input
      v-if="!commandNotifyThresholdLoading"
      class="number-input"
      type="number"
      min="1"
      max="600"
      :value="commandNotifyThresholdSec"
      @change="onCommandNotifyThresholdChange"
    />
    <p class="hint" v-if="!commandNotifyThresholdLoading">
      Commands shorter than this duration do not produce a notification. Requires
      shell integration to be enabled.
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
.number-input {
  width: 80px;
  padding: 4px 8px;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--fg);
  font: inherit;
}
</style>
