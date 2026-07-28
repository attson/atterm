<script lang="ts" setup>
import { onMounted, ref } from "vue";
import type { RelaySessionRow } from "../lib/api";
import { useI18n } from "../i18n/useI18n";
import { usePlatform } from "../platform";

const { t } = useI18n();
const platform = usePlatform();

const rows = ref<RelaySessionRow[]>([]);
const loading = ref(true);
const refreshing = ref(false);
const signingOutOthers = ref(false);
const revoking = ref<Record<string, boolean>>({});
const error = ref("");
const notAuthed = ref(false);

async function reload(silent = false) {
  if (!silent) loading.value = true;
  refreshing.value = true;
  error.value = "";
  notAuthed.value = false;
  try {
    if (!platform.sessions.listRelaySessions) {
      notAuthed.value = true;
      rows.value = [];
      return;
    }
    rows.value = await platform.sessions.listRelaySessions();
  } catch (e: any) {
    const msg = e?.message ?? String(e);
    if (msg.includes("not authenticated")) {
      notAuthed.value = true;
    } else {
      error.value = msg;
    }
  } finally {
    loading.value = false;
    refreshing.value = false;
  }
}

onMounted(() => {
  void reload();
});

async function onRefresh() {
  await reload(true);
}

async function onRevoke(row: RelaySessionRow) {
  const ua = row.user_agent || t("settings.devices.unknownUA");
  if (!window.confirm(t("settings.devices.revokeConfirm", { ua }))) return;
  revoking.value = { ...revoking.value, [row.id_hash]: true };
  try {
    await platform.sessions.revokeRelaySession?.(row.id_hash);
    await reload(true);
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    const next = { ...revoking.value };
    delete next[row.id_hash];
    revoking.value = next;
  }
}

async function onSignOutOthers() {
  if (!window.confirm(t("settings.devices.signOutOthersConfirm"))) return;
  signingOutOthers.value = true;
  try {
    await platform.sessions.signOutOtherRelaySessions?.();
    await reload(true);
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    signingOutOthers.value = false;
  }
}

function formatTime(unixMs: number): string {
  const d = new Date(unixMs);
  return t("settings.devices.timeFormat", {
    y: d.getFullYear(),
    m: d.getMonth() + 1,
    d: d.getDate(),
    hh: String(d.getHours()).padStart(2, "0"),
    mm: String(d.getMinutes()).padStart(2, "0"),
  });
}
</script>

<template>
  <div class="tab-pane">
    <div v-if="loading" class="dim">{{ t("common.loading") }}</div>
    <template v-else>
      <div class="header-row">
        <p class="hint">{{ t("settings.devices.hint") }}</p>
        <div v-if="!notAuthed" class="header-actions">
          <button
            type="button"
            class="icon-btn"
            :disabled="refreshing"
            :aria-label="t('settings.devices.refresh')"
            @click="onRefresh"
          >
            {{ refreshing ? "…" : "⟳" }}
          </button>
          <button
            type="button"
            class="secondary"
            :disabled="signingOutOthers || rows.filter((r) => !r.is_current).length === 0"
            @click="onSignOutOthers"
          >
            {{
              signingOutOthers
                ? t("settings.devices.signingOut")
                : t("settings.devices.signOutOthers")
            }}
          </button>
        </div>
      </div>

      <p v-if="notAuthed" class="hint">{{ t("settings.devices.notAuthenticated") }}</p>
      <p v-else-if="error" class="error">{{ error }}</p>
      <p v-else-if="rows.length === 0" class="dim">{{ t("settings.devices.empty") }}</p>

      <div v-else class="device-list">
        <div v-for="row in rows" :key="row.id_hash" class="device-row">
          <div class="device-info">
            <div class="ua">{{ row.user_agent || t("settings.devices.unknownUA") }}</div>
            <div class="meta">
              {{
                t("settings.devices.loginLine", {
                  time: formatTime(row.created_at),
                  ip: row.ip_prefix,
                })
              }}
            </div>
          </div>
          <span v-if="row.is_current" class="current-tag">
            {{ t("settings.devices.currentTag") }}
          </span>
          <button
            v-else
            type="button"
            class="danger-btn"
            :disabled="!!revoking[row.id_hash]"
            @click="onRevoke(row)"
          >
            {{
              revoking[row.id_hash]
                ? t("settings.devices.revoking")
                : t("settings.devices.revoke")
            }}
          </button>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.tab-pane {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.dim {
  color: var(--fg-dim);
  font-size: 13px;
}
.hint {
  margin: 0;
  color: var(--fg-dim);
  font-size: 12px;
  line-height: 1.5;
}
.error {
  color: var(--bad);
  font-size: 12px;
  margin: 0;
}
.header-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.icon-btn {
  width: 28px;
  height: 28px;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: transparent;
  color: var(--fg-dim);
  cursor: pointer;
  font-size: 14px;
}
.icon-btn:hover:not(:disabled) {
  color: var(--fg);
  background: rgba(255, 255, 255, 0.04);
}
.icon-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
button.secondary {
  background: transparent;
  color: var(--fg-dim);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 6px 12px;
  font-size: 13px;
  cursor: pointer;
}
button.secondary:hover:not(:disabled) {
  color: var(--fg);
  background: rgba(255, 255, 255, 0.04);
}
button.secondary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.device-list {
  display: flex;
  flex-direction: column;
  border-top: 1px solid var(--border);
}
.device-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 0;
  border-bottom: 1px solid var(--border);
}
.device-info {
  flex: 1;
  min-width: 0;
}
.ua {
  color: var(--fg);
  font-size: 13px;
  overflow-wrap: anywhere;
}
.meta {
  color: var(--fg-dim);
  font-size: 12px;
  margin-top: 2px;
}
.current-tag {
  color: var(--fg-dim);
  font-size: 12px;
  padding: 2px 8px;
  border: 1px solid var(--border);
  border-radius: 999px;
  flex-shrink: 0;
}
.danger-btn {
  background: transparent;
  color: var(--bad);
  border: 1px solid var(--bad);
  border-radius: 6px;
  padding: 6px 12px;
  font-size: 13px;
  cursor: pointer;
  flex-shrink: 0;
}
.danger-btn:hover:not(:disabled) {
  background: color-mix(in srgb, var(--bad) 8%, transparent 92%);
}
.danger-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
