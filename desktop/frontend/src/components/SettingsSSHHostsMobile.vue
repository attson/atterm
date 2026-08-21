<script lang="ts" setup>
// Mobile-only, read-only view of the desktop's SSH host list (design doc
// §2/§6). No SSH from mobile: this is reference material, not a dialer —
// a jump-chain host and a ProxyCommand host (which nothing can dial at all)
// are called out distinctly, but neither row is clickable. Never renders a
// credential: SSHHostView (lib/syncedBlobs.ts) cannot carry one — the
// sealed payload's sshCredential/sshKeySecret fields are dropped before the
// view is ever built, and syncedBlobs.test.ts pins that with planted
// CANARY strings — so there is nothing here to accidentally leak, only to
// not add back.
//
// Platform gate: `enabled` short-circuits before any localStorage/crypto
// call and the template's root v-if renders nothing when it's false — same
// shape as SettingsProfilesMobile.vue's gate (see that file's comment for
// why: the web build mounts this identical shell too).
import { computed, ref, watch } from "vue";
import { useI18n } from "../i18n/useI18n";
import { usePlatform } from "../platform";
import { usePolledAccountKey } from "../lib/accountKeyReady";
import { readSyncedRawValue } from "../lib/syncedBlobSource";
import { openSSHHostsBlob, type SSHHostView, type SSHKeyView } from "../lib/syncedBlobs";

const { t } = useI18n();
const platform = usePlatform();
const enabled = platform.caps.capacitor;

// Only start the account-key poll (a real setInterval) when this view can
// ever render — a non-capacitor mount (web tab, Wails desktop) must do
// nothing at all, matching the "short-circuits before any localStorage/
// crypto call" claim above.
const accountKey = enabled ? usePolledAccountKey() : ref<Uint8Array | null>(null);

const hosts = ref<SSHHostView[]>([]);
const keys = ref<SSHKeyView[]>([]);
const errorMessage = ref("");
const hasSyncedValue = ref(false);

function refresh(): void {
  if (!enabled) return;
  errorMessage.value = "";
  const key = accountKey.value;
  if (!key) return;
  const raw = readSyncedRawValue("ssh_hosts_encrypted");
  hasSyncedValue.value = raw !== undefined;
  if (raw === undefined) return;
  try {
    const result = openSSHHostsBlob(key, raw);
    hosts.value = result.hosts;
    keys.value = result.keys;
  } catch (e) {
    errorMessage.value = e instanceof Error ? e.message : String(e);
  }
}

watch(accountKey, refresh, { immediate: true });

// Once hasSyncedValue is true, the try/catch above always sets either
// errorMessage (handled above) or hosts/keys — "empty" and "ready" are the
// only remaining possibilities, so this doesn't need its own "did the
// reader actually run" flag.
type Status = "locked" | "noData" | "error" | "empty" | "ready";
const status = computed<Status>(() => {
  if (!accountKey.value) return "locked";
  if (errorMessage.value) return "error";
  if (!hasSyncedValue.value) return "noData";
  if (hosts.value.length === 0) return "empty";
  return "ready";
});

// keyId references a synced SSHKeyView by id — resolve to its display name
// (never the secret material, which SSHKeyView cannot carry) so a key-auth
// host shows something a human recognizes instead of a bare uuid.
function keyName(keyId: string): string {
  if (!keyId) return "";
  return keys.value.find((k) => k.id === keyId)?.name ?? keyId;
}
</script>

<template>
  <div v-if="enabled" class="mobile-hosts" data-testid="mobile-hosts-panel">
    <p class="hint">{{ t("settings.mobileHosts.intro") }}</p>

    <p v-if="status === 'locked'" class="status" data-testid="mobile-hosts-locked">
      {{ t("settings.mobileSync.locked") }}
    </p>
    <p v-else-if="status === 'noData'" class="status" data-testid="mobile-hosts-no-data">
      {{ t("settings.mobileSync.noData") }}
    </p>
    <p v-else-if="status === 'error'" class="status error" data-testid="mobile-hosts-error">
      {{ t("settings.mobileSync.error", { error: errorMessage }) }}
    </p>
    <p v-else-if="status === 'empty'" class="status" data-testid="mobile-hosts-empty">
      {{ t("settings.mobileHosts.empty") }}
    </p>

    <ul v-else class="host-list" data-testid="mobile-hosts-list">
      <li v-for="h in hosts" :key="h.id" class="host-item" data-testid="mobile-host-item">
        <div class="host-header">
          <span class="host-alias">{{ h.alias || h.host }}</span>
          <span class="host-target">{{ h.host }}</span>
          <span
            v-if="h.isProxyCommandHost"
            class="badge badge-warn"
            data-testid="mobile-host-proxy-command-badge"
          >{{ t("settings.mobileHosts.proxyCommand") }}</span>
          <span
            v-else-if="h.hasJumpChain"
            class="badge"
            data-testid="mobile-host-jump-chain-badge"
          >{{ t("settings.mobileHosts.jumpChain") }}</span>
        </div>
        <dl class="host-fields">
          <dt>{{ t("settings.mobileHosts.user") }}</dt>
          <dd data-testid="mobile-host-user">{{ h.user || t("common.unknown") }}</dd>
          <dt>{{ t("settings.mobileHosts.port") }}</dt>
          <dd data-testid="mobile-host-port">{{ h.port || t("common.unknown") }}</dd>
          <template v-if="h.keyId">
            <dt>{{ t("settings.mobileHosts.key") }}</dt>
            <dd data-testid="mobile-host-key">{{ keyName(h.keyId) }}</dd>
          </template>
          <template v-if="h.tags.length">
            <dt>{{ t("settings.mobileHosts.tags") }}</dt>
            <dd data-testid="mobile-host-tags">{{ h.tags.join(", ") }}</dd>
          </template>
          <template v-if="h.note">
            <dt>{{ t("settings.mobileHosts.note") }}</dt>
            <dd data-testid="mobile-host-note">{{ h.note }}</dd>
          </template>
        </dl>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.mobile-hosts { display: flex; flex-direction: column; gap: 12px; }
.hint { font-size: 12px; color: var(--fg-dim); margin: 0; line-height: 1.5; }
.status { font-size: 13px; color: var(--fg-dim); margin: 0; }
.status.error { color: var(--bad); }
.host-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 10px; }
.host-item { border: 1px solid var(--border); border-radius: 8px; padding: 10px 12px; background: var(--bg); }
.host-header { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; flex-wrap: wrap; }
.host-alias { font-weight: 600; font-size: 13px; color: var(--fg); }
.host-target { font-size: 12px; color: var(--fg-dim); }
.badge { font-size: 11px; padding: 1px 6px; border-radius: 4px; background: rgba(88, 166, 255, 0.16); color: var(--accent); }
.badge-warn { background: rgba(248, 81, 73, 0.16); color: var(--bad); }
.host-fields { margin: 0; display: grid; grid-template-columns: auto 1fr; gap: 2px 10px; font-size: 12px; }
.host-fields dt { color: var(--fg-dim); }
.host-fields dd { margin: 0; color: var(--fg); word-break: break-all; }
</style>
