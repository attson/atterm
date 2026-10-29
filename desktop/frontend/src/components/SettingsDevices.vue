<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { RefreshCw } from "lucide-vue-next";
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
let refreshTimer: ReturnType<typeof setInterval> | null = null;

const onlineCount = computed(() => rows.value.filter((row) => row.is_online).length);
const presenceKnown = computed(() => rows.value.some((row) => !!row.last_seen_at));
const displayRows = computed(() => [...rows.value].sort((a, b) => {
  if (a.is_current !== b.is_current) return a.is_current ? -1 : 1;
  if (!!a.is_online !== !!b.is_online) return a.is_online ? -1 : 1;
  return (b.last_seen_at ?? 0) - (a.last_seen_at ?? 0);
}));

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
  refreshTimer = setInterval(() => {
    if (!refreshing.value) void reload(true);
  }, 30_000);
});

onBeforeUnmount(() => {
  if (refreshTimer !== null) clearInterval(refreshTimer);
});

async function onRefresh() {
  await reload(true);
}

async function onRevoke(row: RelaySessionRow) {
  const ua = deviceName(row.user_agent);
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

function deviceName(userAgent: string): string {
  if (!userAgent) return t("settings.devices.unknownUA");

  const desktop = /^AT-Term-Desktop\/([^\s]+) \(([^/]+)\/([^)]+)\)$/.exec(userAgent);
  if (desktop) {
    const os = ({ darwin: "macOS", windows: "Windows", linux: "Linux" } as Record<string, string>)[desktop[2]] ?? desktop[2];
    const version = desktop[1] === "dev" ? "" : ` · ${desktop[1]}`;
    return `AT Term Desktop · ${os}${version}`;
  }
  if (userAgent === "Go-http-client/1.1") return t("settings.devices.legacyDesktop");

  const device = /iPad/i.test(userAgent)
    ? "iPad"
    : /iPhone/i.test(userAgent)
      ? "iPhone"
      : /Android/i.test(userAgent)
        ? "Android"
        : /Windows/i.test(userAgent)
          ? "Windows"
          : /Macintosh|Mac OS X/i.test(userAgent)
            ? "macOS"
            : /Linux/i.test(userAgent)
              ? "Linux"
              : "";
  const browser = /Edg\//i.test(userAgent)
    ? "Edge"
    : /Firefox\//i.test(userAgent)
      ? "Firefox"
      : /CriOS\//i.test(userAgent) || /Chrome\//i.test(userAgent)
        ? "Chrome"
        : /Safari\//i.test(userAgent)
          ? "Safari"
          : "";
  if (device || browser) return [device, browser].filter(Boolean).join(" · ");
  return userAgent;
}

function activityText(row: RelaySessionRow): string {
  if (row.is_online) return t("settings.devices.online");
  if (!row.last_seen_at) return t("settings.devices.statusUnknown");
  const minutes = Math.max(1, Math.floor((Date.now() - row.last_seen_at) / 60_000));
  if (minutes < 60) return t("settings.devices.activeMinutesAgo", { count: minutes });
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return t("settings.devices.activeHoursAgo", { count: hours });
  return t("settings.devices.activeDaysAgo", { count: Math.floor(hours / 24) });
}
</script>

<template>
  <div class="tab-pane">
    <div v-if="loading" class="dim">{{ t("common.loading") }}</div>
    <template v-else>
      <div class="header-row">
        <div>
          <p class="hint">{{ t("settings.devices.hint") }}</p>
          <p v-if="!notAuthed && rows.length" class="presence-summary">
            <span class="presence-dot" :class="{ active: onlineCount > 0 }" aria-hidden="true" />
            {{ presenceKnown
              ? t("settings.devices.onlineSummary", { online: onlineCount, total: rows.length })
              : t("settings.devices.signedInSummary", { total: rows.length }) }}
          </p>
        </div>
        <div v-if="!notAuthed" class="header-actions">
          <button
            type="button"
            class="icon-btn"
            :disabled="refreshing"
            :aria-label="t('settings.devices.refresh')"
            @click="onRefresh"
          >
            <RefreshCw :size="14" :class="{ spinning: refreshing }" aria-hidden="true" />
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
        <div v-for="row in displayRows" :key="row.id_hash" class="device-row">
          <div class="device-info">
            <div class="ua" :title="row.user_agent">{{ deviceName(row.user_agent) }}</div>
            <div class="meta">
              {{
                t("settings.devices.loginLine", {
                  time: formatTime(row.created_at),
                  ip: row.ip_prefix,
                })
              }}
            </div>
          </div>
          <div class="device-actions">
            <span class="activity" :class="{ online: row.is_online }">
              <span v-if="row.is_online" class="activity-dot" aria-hidden="true" />
              {{ activityText(row) }}
            </span>
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
.presence-summary {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 7px 0 0;
  color: var(--fg);
  font-size: 12px;
}
.presence-dot,
.activity-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex: 0 0 auto;
}
.presence-dot {
  background: var(--fg-dim);
}
.presence-dot.active,
.activity-dot {
  background: var(--good);
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
.spinning {
  animation: devices-spin 0.8s linear infinite;
}
@keyframes devices-spin {
  to { transform: rotate(360deg); }
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
.device-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  flex-shrink: 0;
}
.activity {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--fg-dim);
  font-size: 12px;
  white-space: nowrap;
}
.activity.online {
  color: var(--good);
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

@media (max-width: 720px) {
  .device-row {
    align-items: flex-start;
    flex-wrap: wrap;
  }
  .device-info {
    flex-basis: 100%;
  }
  .device-actions {
    width: 100%;
    justify-content: space-between;
  }
}
.danger-btn:hover:not(:disabled) {
  background: color-mix(in srgb, var(--bad) 8%, transparent 92%);
}
.danger-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
