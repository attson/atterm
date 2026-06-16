<script lang="ts" setup>
import { computed, ref, watchEffect } from "vue";
import type { RecoverySnapshot, RecoveryTabSnapshot } from "../lib/api";
import { useI18n } from "../i18n/useI18n";

const props = defineProps<{ snapshot: RecoverySnapshot; open: boolean }>();
const emit = defineEmits<{
  (e: "restore", picks: RecoveryTabSnapshot[]): void;
  (e: "discard"): void;
}>();

const { t } = useI18n();

const picked = ref<Record<string, boolean>>({});
const expanded = ref<Record<string, boolean>>({});

watchEffect(() => {
  if (!props.snapshot) return;
  for (const tab of props.snapshot.tabs) {
    if (picked.value[tab.id] === undefined) picked.value[tab.id] = true;
  }
});

const minutesAgo = computed(() => {
  const now = Math.floor(Date.now() / 1000);
  const dt = Math.max(0, now - props.snapshot.saved_at_unix);
  return Math.floor(dt / 60);
});

const subtitle = computed(() =>
  props.snapshot.clean_shutdown
    ? t("recovery.dialog.subtitleClean", { minutes: minutesAgo.value })
    : t("recovery.dialog.subtitleUnclean", { minutes: minutesAgo.value }),
);

const pickedCount = computed(
  () => props.snapshot.tabs.filter((tab) => picked.value[tab.id]).length,
);
const totalCount = computed(() => props.snapshot.tabs.length);

const restoreLabel = computed(() =>
  pickedCount.value === totalCount.value
    ? t("recovery.dialog.btnRestoreAll", { count: totalCount.value })
    : t("recovery.dialog.btnRestoreSelected", { count: pickedCount.value }),
);

function paneBadge(p: RecoveryTabSnapshot["panes"][number]): string {
  if (p.session_type !== "ai" && p.last_command_line) {
    const tok = p.last_command_line.split(/\s+/)[0];
    if (tok && !["claude", "codex", "aider"].includes(tok)) {
      return t("recovery.dialog.badgeUnclassified");
    }
  }
  if (p.session_type === "ai") {
    if (p.ai?.kind === "aider") return t("recovery.dialog.badgeResumable");
    if (p.ai?.session_id) return t("recovery.dialog.badgeResumable");
    return t("recovery.dialog.badgeFresh");
  }
  return t("recovery.dialog.badgeShell");
}

function tabTitle(i: number, tab: RecoveryTabSnapshot): string {
  const head = tab.panes[0];
  const cwd = head?.last_cwd?.split("/").filter(Boolean).pop() ?? "";
  return `Tab ${i + 1}` + (cwd ? ` · ${cwd}` : "");
}

function emitRestore() {
  const out = props.snapshot.tabs.filter((tab) => picked.value[tab.id]);
  emit("restore", out);
}
</script>

<template>
  <div v-if="open" class="recovery-dialog-backdrop" role="dialog" aria-modal="true">
    <div class="recovery-dialog">
      <header>
        <h2>{{ t("recovery.dialog.title") }}</h2>
        <p class="subtitle">{{ subtitle }}</p>
      </header>
      <ul class="tab-list">
        <li v-for="(tab, i) in snapshot.tabs" :key="tab.id">
          <label class="tab-row">
            <input type="checkbox" v-model="picked[tab.id]" />
            <button class="caret" type="button" @click="expanded[tab.id] = !expanded[tab.id]">
              {{ expanded[tab.id] ? "▾" : "▸" }}
            </button>
            <span class="tab-title">{{ tabTitle(i, tab) }}</span>
            <span class="tab-meta">
              {{ tab.panes.length }} {{ tab.panes.length > 1 ? "panes" : "pane" }}
            </span>
          </label>
          <ul v-if="expanded[tab.id]" class="pane-list">
            <li v-for="p in tab.panes" :key="p.slot">
              <span class="pane-shell">{{ p.shell.split("/").pop() || p.shell }}</span>
              <span class="pane-cwd">{{ p.last_cwd }}</span>
              <span class="pane-badge">{{ paneBadge(p) }}</span>
            </li>
          </ul>
        </li>
      </ul>
      <footer>
        <button data-testid="btn-discard" class="btn-secondary" @click="emit('discard')">
          {{ t("recovery.dialog.btnDiscard") }}
        </button>
        <button
          data-testid="btn-restore"
          class="btn-primary"
          :disabled="pickedCount === 0"
          @click="emitRestore"
        >
          {{ restoreLabel }}
        </button>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.recovery-dialog-backdrop {
  position: fixed; inset: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex; align-items: center; justify-content: center;
  z-index: 200;
}
.recovery-dialog {
  background: var(--bg, #0d1117);
  color: var(--fg, #d1d5db);
  border-radius: 8px;
  min-width: 480px; max-width: 720px; max-height: 80vh;
  overflow: hidden; display: flex; flex-direction: column;
}
.recovery-dialog header { padding: 16px 20px 0; }
.recovery-dialog h2 { font-size: 1.1rem; margin: 0; }
.subtitle { font-size: 0.85rem; opacity: 0.7; margin: 4px 0 12px; }
.tab-list { list-style: none; padding: 0 20px; margin: 0; overflow: auto; }
.tab-row { display: flex; align-items: center; gap: 8px; padding: 6px 0; cursor: default; }
.caret { background: transparent; border: 0; color: inherit; cursor: pointer; padding: 0 4px; }
.tab-title { flex: 1; }
.tab-meta { opacity: 0.6; font-size: 0.8rem; }
.pane-list { list-style: none; padding-left: 36px; margin: 4px 0 8px; font-size: 0.85rem; }
.pane-list li { display: flex; gap: 8px; padding: 2px 0; }
.pane-shell { font-family: monospace; }
.pane-cwd { opacity: 0.7; flex: 1; }
.pane-badge { opacity: 0.85; font-size: 0.75rem; }
footer { display: flex; justify-content: flex-end; gap: 8px; padding: 12px 20px 16px; }
.btn-primary {
  background: #2563eb; color: white; border: 0;
  padding: 6px 12px; border-radius: 4px; cursor: pointer;
}
.btn-primary:disabled { opacity: 0.4; cursor: not-allowed; }
.btn-secondary {
  background: transparent; color: inherit; border: 1px solid #444;
  padding: 6px 12px; border-radius: 4px; cursor: pointer;
}
</style>
