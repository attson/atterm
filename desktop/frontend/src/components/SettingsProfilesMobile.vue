<script lang="ts" setup>
// Mobile-only, read-only view of the desktop's session profiles (design doc
// §2/§6). Desktop already has an editable "Session profiles" tab
// (SettingsProfiles.vue, gated on caps.wailsBindings) that forks local PTYs;
// this component never does that — mobile cannot fork a PTY at all (see
// platform/capacitor.ts's own comment on newSession), so there is nothing
// here to edit, only to display.
//
// Platform gate: `enabled` short-circuits before any localStorage/crypto call
// and the template's root v-if renders nothing at all when it's false. This
// matters because web/vite.config.ts aliases the web build's `@` to this same
// desktop/frontend/src tree and Capacitor mounts the identical shell, so a
// plain browser tab (caps.capacitor === false) would otherwise reach this
// component too — same shape of gate as SyncStatusIndicator.vue and
// SettingsConfigIO.vue, which each cite their own prior fix rounds for this
// exact mistake.
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "../i18n/useI18n";
import type { MessageKey } from "../i18n";
import { usePlatform } from "../platform";
import type { RemoteSession } from "../platform/types";
import { usePolledAccountKey } from "../lib/accountKeyReady";
import { readSyncedRawValue } from "../lib/syncedBlobSource";
import { openProfilesBlob, type ProfileView } from "../lib/syncedBlobs";

const { t } = useI18n();
const platform = usePlatform();
const enabled = platform.caps.capacitor;

const emit = defineEmits<{
  (e: "session-created", sessionId: string): void;
}>();

// Only start the account-key poll (a real setInterval) when this view can
// ever render — a non-capacitor mount (web tab, Wails desktop) must do
// nothing at all, matching the "short-circuits before any localStorage/
// crypto call" claim above.
const accountKey = enabled ? usePolledAccountKey() : ref<Uint8Array | null>(null);

const profiles = ref<ProfileView[]>([]);
const defaultProfileId = ref("");
const errorMessage = ref("");
const hasSyncedValue = ref(false);

function refresh(): void {
  if (!enabled) return;
  errorMessage.value = "";
  const key = accountKey.value;
  if (!key) return;
  const raw = readSyncedRawValue("profiles_encrypted");
  hasSyncedValue.value = raw !== undefined;
  if (raw === undefined) return;
  try {
    const result = openProfilesBlob(key, raw);
    profiles.value = result.profiles;
    defaultProfileId.value = result.defaultProfileId;
  } catch (e) {
    errorMessage.value = e instanceof Error ? e.message : String(e);
  }
}

watch(accountKey, refresh, { immediate: true });

// Once hasSyncedValue is true, the try/catch above always sets either
// errorMessage (handled above) or profiles — "empty" and "ready" are the
// only remaining possibilities, so this doesn't need its own "did the
// reader actually run" flag.
type Status = "locked" | "noData" | "error" | "empty" | "ready";
const status = computed<Status>(() => {
  if (!accountKey.value) return "locked";
  if (errorMessage.value) return "error";
  if (!hasSyncedValue.value) return "noData";
  if (profiles.value.length === 0) return "empty";
  return "ready";
});

// Host picker: there is no dedicated "list my desktops" API. The mobile
// session list (/api/sessions, already fetched for the sidebar) is the only
// source the brief allows inventing from — see listRemoteSessions. That means
// a desktop with zero open sessions has no SessionInfo row and cannot be
// named here at all, even though the relay itself would route a
// SESSION_CREATE to it fine (it learns host_id from AnnouncePayload on
// uplink connect, independent of session count). This is a real gap: until
// that desktop opens at least one session (from itself), a phone cannot pick
// it as an "open with profile" target.
const remoteSessions = ref<RemoteSession[]>([]);

interface HostOption { id: string; name: string }
const hosts = computed<HostOption[]>(() => {
  const seen = new Map<string, string>();
  for (const s of remoteSessions.value) {
    if (!s.host_id || seen.has(s.host_id)) continue;
    seen.set(s.host_id, s.host || s.host_id);
  }
  return [...seen.entries()]
    .map(([id, name]) => ({ id, name }))
    .sort((a, b) => a.name.localeCompare(b.name));
});

const selectedHostId = ref("");
watch(hosts, (list) => {
  if (!list.some((h) => h.id === selectedHostId.value)) {
    selectedHostId.value = list[0]?.id ?? "";
  }
}, { immediate: true });

async function loadHosts(): Promise<void> {
  if (!enabled) return;
  try {
    remoteSessions.value = await platform.sessions.listRemoteSessions();
  } catch {
    // Sidebar's own session list poll surfaces connectivity errors; this
    // picker just falls back to "no hosts found" rather than duplicating
    // that error UI.
    remoteSessions.value = [];
  }
}
onMounted(loadHosts);

// creating holds the id of the profile currently being opened, or null. It
// is both the "show a spinner on this button" state AND the double-tap
// guard: openProfile's very first line reads it, and since everything up to
// the first `await` in an async function runs synchronously, a second tap
// that lands before the network call starts sees it already set and bails.
//
// This is the ONLY live double-tap defence for this client — not "primary
// with the relay as backstop". capacitor.ts's createSessionWithProfile
// opens a brand-new WebSocket per call (see its own doc comment in
// platform/types.ts for why), so two concurrent taps look to the relay like
// two different clients on two different /client connections. The relay's
// per-client in-flight bound (internal/relay/session_create_router.go's
// hasOutstandingRequest, keyed on the requesting client_conn.go connection's
// own targetedOut channel — see task 5) never sees two taps as the same
// client and cannot fire. If this guard is ever removed, nothing else in
// this path stops a double-tap.
const creating = ref<string | null>(null);
const createError = ref("");

// The closed set of wire error codes (SessionCreatedPayload.Error in
// internal/proto/frame.go) plus the platform-bridge-level codes
// capacitor.ts's createSessionWithProfile can reject with ('timeout',
// 'relay_not_configured'). Anything else is either the desktop's raw
// NewSession failure message passed through unchanged, or an unexpected
// JS error — both fall through to the generic bucket rather than being
// dumped raw at the user.
const KNOWN_ERROR_CODES = new Set([
  "unknown_profile",
  "permission_denied",
  "request_in_flight",
  "duplicate_request_id",
  "unknown_host_id",
  "invalid_request",
  "upstream_unavailable",
  "session_create_busy",
  "timeout",
  "relay_not_configured",
]);

function mapCreateError(message: string): string {
  if (KNOWN_ERROR_CODES.has(message)) {
    return t(`settings.mobileProfiles.openErrors.${message}` as MessageKey);
  }
  return t("settings.mobileProfiles.openErrors.generic", { error: message });
}

async function openProfile(profileId: string): Promise<void> {
  if (creating.value !== null) return;
  const hostId = selectedHostId.value;
  if (!hostId) return;
  const create = platform.sessions.createSessionWithProfile;
  if (!create) {
    createError.value = mapCreateError("relay_not_configured");
    return;
  }
  creating.value = profileId;
  createError.value = "";
  try {
    const sessionId = await create(hostId, profileId);
    // No retry on any outcome, success included — the caller (App.vue) is
    // responsible for what happens next; this component's job ends at the
    // emit.
    emit("session-created", sessionId);
  } catch (e) {
    createError.value = mapCreateError(e instanceof Error ? e.message : String(e));
  } finally {
    creating.value = null;
  }
}
</script>

<template>
  <div v-if="enabled" class="mobile-profiles" data-testid="mobile-profiles-panel">
    <p class="hint">{{ t("settings.mobileProfiles.intro") }}</p>

    <p v-if="status === 'locked'" class="status" data-testid="mobile-profiles-locked">
      {{ t("settings.mobileSync.locked") }}
    </p>
    <p v-else-if="status === 'noData'" class="status" data-testid="mobile-profiles-no-data">
      {{ t("settings.mobileSync.noData") }}
    </p>
    <p v-else-if="status === 'error'" class="status error" data-testid="mobile-profiles-error">
      {{ t("settings.mobileSync.error", { error: errorMessage }) }}
    </p>
    <p v-else-if="status === 'empty'" class="status" data-testid="mobile-profiles-empty">
      {{ t("settings.mobileProfiles.empty") }}
    </p>

    <template v-else>
      <label v-if="hosts.length > 0" class="host-picker">
        <span>{{ t("settings.mobileProfiles.hostLabel") }}</span>
        <select v-model="selectedHostId" data-testid="mobile-profiles-host-select">
          <option v-for="h in hosts" :key="h.id" :value="h.id">{{ h.name }}</option>
        </select>
      </label>
      <p v-else class="status" data-testid="mobile-profiles-no-hosts">
        {{ t("settings.mobileProfiles.noHosts") }}
      </p>

      <p v-if="createError" class="status error" data-testid="mobile-profiles-create-error">
        {{ createError }}
      </p>

      <ul class="profile-list" data-testid="mobile-profiles-list">
        <li v-for="p in profiles" :key="p.id" class="profile-item" data-testid="mobile-profile-item">
          <div class="profile-header">
            <span class="profile-name">{{ p.name || t("common.unknown") }}</span>
            <span
              v-if="p.id === defaultProfileId"
              class="badge"
              data-testid="mobile-profile-default-badge"
            >{{ t("settings.mobileProfiles.defaultBadge") }}</span>
            <button
              type="button"
              class="open-btn"
              data-testid="mobile-profile-open"
              :disabled="!selectedHostId || creating !== null"
              @click="openProfile(p.id)"
            >{{ creating === p.id ? t("settings.mobileProfiles.opening") : t("settings.mobileProfiles.openButton") }}</button>
          </div>
          <dl class="profile-fields">
            <dt>{{ t("settings.mobileProfiles.shell") }}</dt>
            <dd data-testid="mobile-profile-shell">{{ p.shell || t("common.unknown") }}</dd>
            <dt>{{ t("settings.mobileProfiles.cwd") }}</dt>
            <dd data-testid="mobile-profile-cwd">{{ p.cwd || t("common.unknown") }}</dd>
            <dt>{{ t("settings.mobileProfiles.startupCmd") }}</dt>
            <dd data-testid="mobile-profile-startup-cmd">{{ p.startupCmd || t("common.unknown") }}</dd>
            <dt>{{ t("settings.mobileProfiles.env") }}</dt>
            <dd data-testid="mobile-profile-env">
              <template v-if="!p.syncEnv">{{ t("settings.mobileProfiles.envNotSynced") }}</template>
              <template v-else-if="!p.env || Object.keys(p.env).length === 0">{{ t("settings.mobileProfiles.envNone") }}</template>
              <ul v-else class="env-list">
                <li v-for="(v, k) in p.env" :key="k">{{ k }}={{ v }}</li>
              </ul>
            </dd>
          </dl>
        </li>
      </ul>
    </template>
  </div>
</template>

<style scoped>
.mobile-profiles { display: flex; flex-direction: column; gap: 12px; }
.hint { font-size: 12px; color: var(--fg-dim); margin: 0; line-height: 1.5; }
.status { font-size: 13px; color: var(--fg-dim); margin: 0; }
.status.error { color: var(--bad); }
.profile-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 10px; }
.profile-item { border: 1px solid var(--border); border-radius: 8px; padding: 10px 12px; background: var(--bg); }
.profile-header { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; }
.profile-name { font-weight: 600; font-size: 13px; color: var(--fg); flex: 1; }
.badge { font-size: 11px; padding: 1px 6px; border-radius: 4px; background: rgba(88, 166, 255, 0.16); color: var(--accent); }
.host-picker { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--fg-dim); }
.host-picker select { font-size: 12px; padding: 2px 6px; }
.open-btn { font-size: 12px; padding: 3px 10px; border-radius: 6px; border: 1px solid var(--border); background: var(--bg); color: var(--fg); cursor: pointer; }
.open-btn:disabled { opacity: 0.5; cursor: default; }
.profile-fields { margin: 0; display: grid; grid-template-columns: auto 1fr; gap: 2px 10px; font-size: 12px; }
.profile-fields dt { color: var(--fg-dim); }
.profile-fields dd { margin: 0; color: var(--fg); word-break: break-all; }
.env-list { list-style: none; margin: 0; padding: 0; }
</style>
