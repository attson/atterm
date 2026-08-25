<script lang="ts">
// Exported so SettingsGeneral.vue can type its re-emit of the same event
// without redeclaring the payload shape.
export interface TerminalAppearanceState {
  fontHead: string;
  fontSize: number;
  lineHeight: number;
  cursorStyle: "block" | "underline" | "bar";
  cursorBlink: boolean;
  scrollback: number;
}
</script>

<script lang="ts" setup>
// Font / size / line height / cursor / scrollback controls for Settings ->
// General. Split out of SettingsGeneral.vue (which had grown past 900 lines)
// because this block has no coupling to the rest of that file and roadmap
// item 22 (per-profile appearance overrides) will want it already isolated.
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  getTerminalFontHead,
  setTerminalFontHead,
  getTerminalFontSize,
  setTerminalFontSize,
  getTerminalLineHeight,
  setTerminalLineHeight,
  getTerminalCursorStyle,
  setTerminalCursorStyle,
  getTerminalCursorBlink,
  setTerminalCursorBlink,
  getTerminalScrollback,
  setTerminalScrollback,
} from "../lib/api";
import { useI18n } from "../i18n/useI18n";
import { TERMINAL_FONT_PRESETS } from "../lib/terminalFont";
import { usePlatform } from "../platform";
import SelectDropdown from "./SelectDropdown.vue";

const emit = defineEmits<{
  (e: "appearance-changed", state: TerminalAppearanceState): void;
}>();

const { t } = useI18n();
const platform = usePlatform();
const caps = platform.caps;
const error = ref("");

// Loaded together in one Promise.all below since they're one logical unit —
// Task 4's App.vue reads them the same way and expects them to arrive as a
// single "appearance" snapshot.
const fontHead = ref("");
const persistedFontHead = ref("");
const fontSize = ref(13);
const lineHeight = ref(1.0);
const cursorStyle = ref<"block" | "underline" | "bar">("block");
const persistedCursorStyle = ref<"block" | "underline" | "bar">("block");
const cursorBlink = ref(true);
const scrollback = ref(5000);
const persistedScrollback = ref(5000);
const appearanceLoading = ref(true);

const fontHeadOptions = computed(() =>
  TERMINAL_FONT_PRESETS.map((preset) => ({ value: preset.id, label: preset.label })),
);

const cursorStyleOptions = computed(() => [
  { value: "block", label: t("settings.general.terminalCursorStyleBlock") },
  { value: "underline", label: t("settings.general.terminalCursorStyleUnderline") },
  { value: "bar", label: t("settings.general.terminalCursorStyleBar") },
]);

// #343 dropped scrollback to 5000 because ~2.75 KB/line at 200 columns adds
// up fast across panes (see TerminalView.vue's comment on the `scrollback`
// default). Exposing the setting again means the user needs to see that cost
// before they raise it — this estimate feeds the {mb} placeholder below.
const scrollbackEstimateMb = computed(() => Math.round((scrollback.value * 2.75) / 1000));

// Shared by onMounted and the prefs:changed listener below (Task 4) so a
// remote pull and a fresh mount observe Go the exact same way. Read-only:
// assigns refs from what Go returns and nothing else. Must never call a
// set* API — a remote change reaching this panel and getting written back
// out would ping-pong the value between devices forever (design §7.2).
async function loadAppearance() {
  if (!caps.wailsBindings) return;
  try {
    const [head, size, lh, style, blink, sb] = await Promise.all([
      getTerminalFontHead(),
      getTerminalFontSize(),
      getTerminalLineHeight(),
      getTerminalCursorStyle(),
      getTerminalCursorBlink(),
      getTerminalScrollback(),
    ]);
    fontHead.value = head;
    persistedFontHead.value = head;
    fontSize.value = size;
    lineHeight.value = lh;
    cursorStyle.value = style;
    persistedCursorStyle.value = cursorStyle.value;
    cursorBlink.value = blink;
    scrollback.value = sb;
    persistedScrollback.value = sb;
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    appearanceLoading.value = false;
  }
}

// prefsSync fires the same "prefs:changed" event on a successful Pull as it
// does after a local Push; if this panel is already open when a remote
// change lands, reload so its six controls don't sit stale until the panel
// is closed and reopened. SettingsDialog mounts this fresh every time
// Settings opens (App.vue: `v-if="showSettings"`), so the listener must be
// torn down on unmount or every open leaves one more copy running forever —
// mirrors SettingsTemplates.vue's prefsChangedOff.
let prefsChangedOff: (() => void) | null = null;
onMounted(() => {
  void loadAppearance();
  prefsChangedOff = platform.events.on("prefs:changed", () => {
    void loadAppearance();
  });
});
onBeforeUnmount(() => {
  prefsChangedOff?.();
  prefsChangedOff = null;
});

// Task 4's App.vue replaces its appearance state wholesale on every event
// rather than patching one field, so every commit re-sends all six current
// values (not just the one that changed).
//
// scrollback reads persistedScrollback, not scrollback.value: unlike the
// other five fields, scrollback has an `input` handler (onScrollbackInput)
// that updates scrollback.value unclamped on every keystroke so the memory
// hint tracks typing, before onScrollbackChange commits/clamps on blur. If
// some other control's change handler fired emitAppearanceChanged() while a
// scrollback edit was mid-keystroke, reading scrollback.value here could
// emit a value that was never actually persisted. Reading
// persistedScrollback keeps "what we emit" == "what we persisted" true
// unconditionally rather than depending on tab-order/blur timing.
function emitAppearanceChanged() {
  emit("appearance-changed", {
    fontHead: fontHead.value,
    fontSize: fontSize.value,
    lineHeight: lineHeight.value,
    cursorStyle: cursorStyle.value,
    cursorBlink: cursorBlink.value,
    scrollback: persistedScrollback.value,
  });
}

async function onFontHeadChange() {
  const next = fontHead.value;
  const previous = persistedFontHead.value;
  error.value = "";
  try {
    await setTerminalFontHead(next);
    persistedFontHead.value = next;
    emitAppearanceChanged();
  } catch (e: any) {
    fontHead.value = previous;
    error.value = e?.message ?? String(e);
  }
}

// Committed on `change`, not `input`: font size/line height changes trigger a
// terminal re-fit + SIGWINCH in Task 4, and dragging a number input fires
// `input` continuously (spec §6 risk 2).
async function onFontSizeChange(e: Event) {
  const target = e.target as HTMLInputElement;
  const raw = Number(target.value);
  const next = Number.isFinite(raw) ? Math.max(8, Math.min(32, Math.round(raw))) : fontSize.value;
  const previous = fontSize.value;
  fontSize.value = next;
  target.value = String(next);
  error.value = "";
  try {
    await setTerminalFontSize(next);
    emitAppearanceChanged();
  } catch (e: any) {
    fontSize.value = previous;
    target.value = String(previous);
    error.value = e?.message ?? String(e);
  }
}

async function onLineHeightChange(e: Event) {
  const target = e.target as HTMLInputElement;
  const raw = Number(target.value);
  const next = Number.isFinite(raw) ? Math.max(1.0, Math.min(2.0, Math.round(raw * 10) / 10)) : lineHeight.value;
  const previous = lineHeight.value;
  lineHeight.value = next;
  target.value = String(next);
  error.value = "";
  try {
    await setTerminalLineHeight(next);
    emitAppearanceChanged();
  } catch (e: any) {
    lineHeight.value = previous;
    target.value = String(previous);
    error.value = e?.message ?? String(e);
  }
}

async function onCursorStyleChange() {
  const next = cursorStyle.value;
  const previous = persistedCursorStyle.value;
  error.value = "";
  try {
    await setTerminalCursorStyle(next);
    persistedCursorStyle.value = next;
    emitAppearanceChanged();
  } catch (e: any) {
    cursorStyle.value = previous;
    error.value = e?.message ?? String(e);
  }
}

async function onCursorBlinkToggle(e: Event) {
  const target = e.target as HTMLInputElement;
  const previous = cursorBlink.value;
  cursorBlink.value = target.checked;
  error.value = "";
  try {
    await setTerminalCursorBlink(target.checked);
    emitAppearanceChanged();
  } catch (e: any) {
    cursorBlink.value = previous;
    target.checked = previous;
    error.value = e?.message ?? String(e);
  }
}

// `input` updates the local ref so the memory-estimate hint tracks what the
// user is typing; the Wails call (and the resize it triggers in Task 4) only
// fires on `change`, same as onFontSizeChange above.
function onScrollbackInput(e: Event) {
  const raw = Number((e.target as HTMLInputElement).value);
  if (Number.isFinite(raw)) scrollback.value = raw;
}

async function onScrollbackChange(e: Event) {
  const target = e.target as HTMLInputElement;
  const raw = Number(target.value);
  const next = Number.isFinite(raw)
    ? Math.max(500, Math.min(20000, Math.round(raw)))
    : persistedScrollback.value;
  const previous = persistedScrollback.value;
  scrollback.value = next;
  target.value = String(next);
  error.value = "";
  try {
    await setTerminalScrollback(next);
    persistedScrollback.value = next;
    emitAppearanceChanged();
  } catch (e: any) {
    scrollback.value = previous;
    target.value = String(previous);
    error.value = e?.message ?? String(e);
  }
}
</script>

<template>
  <div v-if="caps.wailsBindings && !appearanceLoading" class="terminal-appearance">
    <div class="settings-field">
      <label class="field-label">{{ t("settings.general.terminalFont") }}</label>
      <SelectDropdown
        v-model="fontHead"
        :options="fontHeadOptions"
        data-test="terminal-font"
        :aria-label="t('settings.general.terminalFont')"
        @update:modelValue="onFontHeadChange"
      />
      <p class="hint">{{ t("settings.general.terminalFontHint") }}</p>
    </div>

    <div class="settings-field-grid settings-field-grid--compact" data-test="terminal-compact-grid">
      <div class="settings-field">
        <label class="field-label" for="terminal-font-size">{{ t("settings.general.terminalFontSize") }}</label>
        <input
          id="terminal-font-size"
          class="number-input"
          type="number"
          data-test="terminal-font-size"
          min="8"
          max="32"
          step="1"
          :value="fontSize"
          @change="onFontSizeChange"
        />
      </div>

      <div class="settings-field">
        <label class="field-label" for="terminal-line-height">{{ t("settings.general.terminalLineHeight") }}</label>
        <input
          id="terminal-line-height"
          class="number-input"
          type="number"
          data-test="terminal-line-height"
          min="1.0"
          max="2.0"
          step="0.1"
          :value="lineHeight"
          @change="onLineHeightChange"
        />
      </div>

      <div class="settings-field scrollback-field">
        <label class="field-label" for="terminal-scrollback">{{ t("settings.general.terminalScrollback") }}</label>
        <input
          id="terminal-scrollback"
          class="number-input"
          type="number"
          data-test="terminal-scrollback"
          min="500"
          max="20000"
          step="500"
          :value="scrollback"
          @input="onScrollbackInput"
          @change="onScrollbackChange"
        />
      </div>

      <div class="settings-field">
        <label class="field-label">{{ t("settings.general.terminalCursorStyle") }}</label>
        <SelectDropdown
          v-model="cursorStyle"
          :options="cursorStyleOptions"
          data-test="terminal-cursor-style"
          :aria-label="t('settings.general.terminalCursorStyle')"
          @update:modelValue="onCursorStyleChange"
        />
        <label class="checkbox cursor-blink">
          <input
            type="checkbox"
            data-test="terminal-cursor-blink"
            :checked="cursorBlink"
            @change="onCursorBlinkToggle"
          />
          {{ t("settings.general.terminalCursorBlink") }}
        </label>
      </div>
    </div>

    <p class="hint" data-test="terminal-scrollback-hint">
      {{ t("settings.general.terminalScrollbackHint", { mb: scrollbackEstimateMb }) }}
    </p>

    <p v-if="error" class="error">{{ error }}</p>
  </div>
</template>

<style scoped>
.terminal-appearance {
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
.settings-field-grid .number-input {
  width: 100%;
}
.cursor-blink {
  min-height: 18px;
}
.scrollback-field {
  align-items: flex-start;
}
</style>
