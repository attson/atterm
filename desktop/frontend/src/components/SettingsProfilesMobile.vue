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
import { computed, ref, watch } from "vue";
import { useI18n } from "../i18n/useI18n";
import { usePlatform } from "../platform";
import { usePolledAccountKey } from "../lib/accountKeyReady";
import { readSyncedRawValue } from "../lib/syncedBlobSource";
import { openProfilesBlob, type ProfileView } from "../lib/syncedBlobs";

const { t } = useI18n();
const platform = usePlatform();
const enabled = platform.caps.capacitor;

const accountKey = usePolledAccountKey();

const profiles = ref<ProfileView[]>([]);
const defaultProfileId = ref("");
const errorMessage = ref("");
const hasSyncedValue = ref(false);
const opened = ref(false);

function refresh(): void {
  if (!enabled) return;
  errorMessage.value = "";
  opened.value = false;
  const key = accountKey.value;
  if (!key) return;
  const raw = readSyncedRawValue("profiles_encrypted");
  hasSyncedValue.value = raw !== undefined;
  if (raw === undefined) return;
  try {
    const result = openProfilesBlob(key, raw);
    profiles.value = result.profiles;
    defaultProfileId.value = result.defaultProfileId;
    opened.value = true;
  } catch (e) {
    errorMessage.value = e instanceof Error ? e.message : String(e);
  }
}

watch(accountKey, refresh, { immediate: true });

type Status = "locked" | "noData" | "error" | "empty" | "ready";
const status = computed<Status>(() => {
  if (!accountKey.value) return "locked";
  if (errorMessage.value) return "error";
  if (!hasSyncedValue.value) return "noData";
  if (opened.value && profiles.value.length === 0) return "empty";
  return "ready";
});
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

    <ul v-else class="profile-list" data-testid="mobile-profiles-list">
      <li v-for="p in profiles" :key="p.id" class="profile-item" data-testid="mobile-profile-item">
        <div class="profile-header">
          <span class="profile-name">{{ p.name || t("common.unknown") }}</span>
          <span
            v-if="p.id === defaultProfileId"
            class="badge"
            data-testid="mobile-profile-default-badge"
          >{{ t("settings.mobileProfiles.defaultBadge") }}</span>
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
.profile-name { font-weight: 600; font-size: 13px; color: var(--fg); }
.badge { font-size: 11px; padding: 1px 6px; border-radius: 4px; background: rgba(88, 166, 255, 0.16); color: var(--accent); }
.profile-fields { margin: 0; display: grid; grid-template-columns: auto 1fr; gap: 2px 10px; font-size: 12px; }
.profile-fields dt { color: var(--fg-dim); }
.profile-fields dd { margin: 0; color: var(--fg); word-break: break-all; }
.env-list { list-style: none; margin: 0; padding: 0; }
</style>
