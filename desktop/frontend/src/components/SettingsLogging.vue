<script lang="ts" setup>
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  getLogPreview,
  getLoggingConfig,
  getPtyInputDebugEnabled,
  pickLogFilePath,
  setLoggingConfig,
  setPtyInputDebugEnabled,
  type LogPreview,
} from "../lib/api";
import { useI18n } from "../i18n/useI18n";
import LogLines from "./LogLines.vue";
import SelectDropdown from "./SelectDropdown.vue";
import {
  LEVEL_FILTER_OPTIONS,
  LEVEL_WRITE_OPTIONS,
  isLogLevel,
  type LogLevel,
} from "../lib/parseLogLine";

defineEmits<{
  (e: "open-log-viewer"): void;
}>();

const enabled = ref(true);
const path = ref("");
const effectivePath = ref("");
const loading = ref(true);
const error = ref("");
const ptyInputDebug = ref(false);
// writeLevel is the minimum severity persisted to the file. Not to be confused
// with tailMinLevel below, which only filters what is already there.
const writeLevel = ref<LogLevel>("INFO");
const { t } = useI18n();

// Inline log tail: refresh every 3 s while the panel is mounted so the
// user can watch new lines land without opening the full-screen viewer.
const tail = ref<LogPreview | null>(null);
const tailError = ref("");
const tailLoading = ref(false);
let tailTimer: number | null = null;
const tailEl = ref<any>(null);
const tailMinLevel = ref<LogLevel>("DEBUG");
// "following" tails the newest lines. It auto-pauses when the user scrolls
// up to read, so the 3 s refresh never yanks the viewport or swaps content
// out from under them; scrolling back to the bottom resumes the tail.
const following = ref(true);

function tailScrollEl(): HTMLElement | undefined {
  return (tailEl.value as any)?.$el as HTMLElement | undefined;
}

function atBottom(el: HTMLElement): boolean {
  return el.scrollHeight - el.scrollTop - el.clientHeight < 24;
}

async function refreshTail(opts?: { force?: boolean }) {
  if (!enabled.value) return;
  const el = tailScrollEl();
  // While the user has scrolled up to read, skip the periodic refresh so the
  // content under their cursor doesn't move or get replaced. A manual refresh
  // (force) and the already-at-bottom case always run.
  if (!opts?.force && el && !atBottom(el)) {
    following.value = false;
    return;
  }
  following.value = true;
  tailLoading.value = true;
  tailError.value = "";
  try {
    tail.value = await getLogPreview();
  } catch (e: any) {
    tailError.value = e?.message ?? String(e);
  } finally {
    tailLoading.value = false;
  }
  await nextTick();
  const after = tailScrollEl();
  if (after) after.scrollTop = after.scrollHeight;
}

function onTailScroll() {
  const el = tailScrollEl();
  if (!el) return;
  const pinned = atBottom(el);
  if (pinned && !following.value) {
    // Scrolled back to the bottom: resume following and catch up immediately.
    void refreshTail();
  } else {
    following.value = pinned;
  }
}

// Attach the scroll listener to the live LogLines element whenever it mounts
// or unmounts (it exists only while logging is enabled and content is present).
watch(tailEl, (cur, _prev, onCleanup) => {
  const el = (cur as any)?.$el as HTMLElement | undefined;
  if (!el) return;
  el.addEventListener("scroll", onTailScroll, { passive: true });
  onCleanup(() => el.removeEventListener("scroll", onTailScroll));
});

function applyConfig(cfg: Awaited<ReturnType<typeof getLoggingConfig>>) {
  enabled.value = cfg.enabled;
  path.value = cfg.path;
  effectivePath.value = cfg.effective_path;
  if (isLogLevel(cfg.level)) writeLevel.value = cfg.level;
}

onMounted(async () => {
  try {
    applyConfig(await getLoggingConfig());
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    loading.value = false;
  }
  try {
    ptyInputDebug.value = await getPtyInputDebugEnabled();
  } catch {
    /* leave default false */
  }
  await refreshTail();
  tailTimer = window.setInterval(refreshTail, 3000);
});

onBeforeUnmount(() => {
  if (tailTimer !== null) {
    window.clearInterval(tailTimer);
    tailTimer = null;
  }
});

async function onToggle(e: Event) {
  const target = e.target as HTMLInputElement;
  const previous = enabled.value;
  enabled.value = target.checked;
  error.value = "";
  try {
    await setLoggingConfig({ enabled: target.checked, path: path.value });
    applyConfig(await getLoggingConfig());
  } catch (e: any) {
    enabled.value = previous;
    error.value = e?.message ?? String(e);
  }
}

async function onWriteLevelChange(next: LogLevel) {
  const previous = writeLevel.value;
  writeLevel.value = next;
  error.value = "";
  try {
    await setLoggingConfig({ enabled: enabled.value, path: path.value, level: next });
    applyConfig(await getLoggingConfig());
    // A lowered write level means new records the tail hasn't seen yet.
    await refreshTail({ force: true });
  } catch (e: any) {
    writeLevel.value = previous;
    error.value = e?.message ?? String(e);
  }
}

async function onTogglePtyInputDebug(e: Event) {
  const target = e.target as HTMLInputElement;
  const previous = ptyInputDebug.value;
  ptyInputDebug.value = target.checked;
  try {
    await setPtyInputDebugEnabled(target.checked);
  } catch (err: any) {
    ptyInputDebug.value = previous;
    error.value = err?.message ?? String(err);
  }
}

async function onPickPath() {
  error.value = "";
  try {
    const picked = await pickLogFilePath();
    if (!picked) return;
    await setLoggingConfig({ enabled: enabled.value, path: picked });
    applyConfig(await getLoggingConfig());
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  }
}

async function onResetPath() {
  error.value = "";
  try {
    await setLoggingConfig({ enabled: enabled.value, path: "" });
    applyConfig(await getLoggingConfig());
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  }
}
</script>

<template>
  <div class="tab-pane">
    <div v-if="loading" class="dim">{{ t("common.loading") }}</div>
    <template v-else>
      <label class="checkbox">
        <input
          type="checkbox"
          :checked="enabled"
          @change="onToggle"
        />
        {{ t("settings.logging.writeLogs") }}
      </label>

      <div class="level-row">
        <span class="level-label">{{ t("settings.logging.writeLevel") }}</span>
        <div class="level-select">
          <SelectDropdown
            :modelValue="writeLevel"
            :options="LEVEL_WRITE_OPTIONS"
            :ariaLabel="t('settings.logging.writeLevel')"
            @update:modelValue="(v) => onWriteLevelChange(v as LogLevel)"
          />
        </div>
        <span
          class="info-icon"
          role="img"
          :aria-label="t('settings.logging.writeLevelHint')"
          :title="t('settings.logging.writeLevelHint')"
        >i</span>
      </div>

      <div class="checkbox-row">
        <label class="checkbox">
          <input type="checkbox" :checked="ptyInputDebug" @change="onTogglePtyInputDebug" />
          {{ t("settings.logging.ptyInputDebug") }}
        </label>
        <span
          class="info-icon"
          role="img"
          :aria-label="t('settings.logging.ptyInputDebugHint')"
          :title="t('settings.logging.ptyInputDebugHint')"
        >i</span>
      </div>

      <div class="kv">
        <span class="k">{{ t("settings.logging.currentFile") }}</span>
        <span class="v path" :title="effectivePath">{{ effectivePath }}</span>
      </div>

      <div class="actions">
        <button @click="onPickPath">{{ t("settings.logging.changeLocation") }}</button>
        <button @click="onResetPath">{{ t("settings.logging.resetDefault") }}</button>
        <button @click="$emit('open-log-viewer')">{{ t("settings.logging.viewLogs") }}</button>
      </div>

      <p v-if="error" class="error">{{ error }}</p>

      <section v-if="enabled" class="tail-wrap">
        <header class="tail-header">
          <span class="tail-label">{{ t("settings.logging.liveTail") }}</span>
          <button
            v-if="!following"
            class="tail-paused"
            :title="t('settings.logging.tailPausedHint')"
            @click="refreshTail({ force: true })"
          >
            {{ t("settings.logging.tailPaused") }}
          </button>
          <div class="tail-level">
            <SelectDropdown
              :modelValue="tailMinLevel"
              :options="LEVEL_FILTER_OPTIONS"
              :ariaLabel="t('settings.logging.levelFilter')"
              @update:modelValue="(v) => (tailMinLevel = v as LogLevel)"
            />
          </div>
          <button class="tail-refresh" :disabled="tailLoading" @click="refreshTail({ force: true })">
            {{ t("common.refresh") }}
          </button>
        </header>
        <p v-if="tailError" class="tail-error">{{ tailError }}</p>
        <p v-else-if="!tail || !tail.exists" class="tail-empty">
          {{ t("settings.logging.noContent") }}
        </p>
        <LogLines v-else ref="tailEl" class="tail-content" :content="tail.content" :minLevel="tailMinLevel" />
      </section>
    </template>
  </div>
</template>

<style scoped>
.tab-pane {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.dim {
  color: var(--fg-dim);
  font-size: 13px;
}
.checkbox {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--fg);
}
.checkbox-row {
  display: flex;
  align-items: center;
  gap: 6px;
}
.level-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.level-label {
  font-size: 13px;
  color: var(--fg);
}
.level-select {
  width: 104px;
}
.info-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 14px;
  border: 1px solid var(--fg-dim);
  border-radius: 50%;
  color: var(--fg-dim);
  font-size: 10px;
  font-style: italic;
  line-height: 1;
  cursor: help;
  user-select: none;
}
.info-icon:hover {
  color: var(--fg);
  border-color: var(--fg);
}
.kv {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  font-size: 12px;
}
.kv .k {
  color: var(--fg-dim);
  width: 130px;
}
.kv .v {
  color: var(--fg);
}
.kv .path {
  flex: 1;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  overflow-wrap: anywhere;
}
.actions {
  display: flex;
  gap: 8px;
}
.error {
  color: var(--bad);
  font-size: 12px;
  margin: 0;
}
button {
  height: 32px;
  padding: 6px 14px;
  background: var(--bg);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}
button:hover {
  background: rgba(255, 255, 255, 0.04);
}
.tail-wrap {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 4px;
}
.tail-header {
  display: flex;
  align-items: center;
  gap: 8px;
}
.tail-label {
  font-size: 12px;
  color: var(--fg-dim);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  flex: 1 1 auto;
}
.tail-refresh {
  height: 24px;
  padding: 2px 10px;
  font-size: 12px;
}
.tail-paused {
  height: 24px;
  padding: 2px 10px;
  font-size: 12px;
  flex: 0 0 auto;
  color: var(--accent);
  border-color: var(--accent);
}
.tail-level { width: 104px; flex: 0 0 auto; }
.tail-empty {
  color: var(--fg-dim);
  font-size: 12px;
  margin: 0;
}
.tail-error {
  color: var(--bad);
  font-size: 12px;
  margin: 0;
}
.tail-content {
  margin: 0;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 10px;
  color: var(--fg);
  font-size: 11px;
  line-height: 1.45;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  max-height: 280px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
