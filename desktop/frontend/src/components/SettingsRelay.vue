<script lang="ts" setup>
import { computed, onMounted, ref, watch } from "vue";
import { getRelayConfig, setRelayConfig, setUplinkPaused, fetchRelayMe } from "../lib/api";
import { BrowserOpenURL, EventsOn } from "../../wailsjs/runtime/runtime";
import SelectDropdown from "./SelectDropdown.vue";

const emit = defineEmits<{
  (e: "relay-config-changed"): void;
  (e: "dirty", value: boolean): void;
}>();

const url = ref("");
const token = ref("");
const allowInsecureRelay = ref(false);
const remotePermission = ref("full");
const paused = ref(false);
const loading = ref(true);
const saving = ref(false);
const togglingPause = ref(false);
const error = ref("");

// In-memory only (SEC-1): never persisted or logged.
const connectedUserID = ref("");
const connectedEmail = ref("");

const persistedUrl = ref("");
const persistedToken = ref("");
const persistedAllowInsecure = ref(false);
const persistedPermission = ref("full");

const permissionOptions = [
  { value: "view", label: "view only", description: "remote clients can watch output" },
  { value: "control", label: "control", description: "allow input and resize" },
  { value: "full", label: "full", description: "allow input, resize, and image paste" },
];

const dirty = computed(
  () =>
    url.value !== persistedUrl.value ||
    token.value !== persistedToken.value ||
    allowInsecureRelay.value !== persistedAllowInsecure.value ||
    remotePermission.value !== persistedPermission.value,
);

watch(dirty, (value) => emit("dirty", value));

// Token format warning: tokens must start with atk_
const tokenWarning = computed(() =>
  token.value && !token.value.startsWith("atk_")
    ? "This doesn't look like an API token. Old shared tokens were removed in v2."
    : "",
);

// Status pill matrix per spec §8.2
const statusPill = computed(() => {
  if (connectedEmail.value) {
    return { text: `connected as ${connectedEmail.value}`, cls: "on" };
  }
  if (connectedUserID.value) {
    return { text: `connected as ${connectedUserID.value.slice(0, 8)}`, cls: "on" };
  }
  if (!url.value) {
    return { text: "not configured", cls: "off" };
  }
  if (paused.value) {
    return { text: "paused (config kept)", cls: "off" };
  }
  return { text: "uplink running", cls: "on" };
});

onMounted(async () => {
  try {
    const cfg = await getRelayConfig();
    url.value = cfg.url;
    token.value = cfg.token;
    allowInsecureRelay.value = cfg.allow_insecure_relay;
    remotePermission.value = cfg.remote_permission || "full";
    paused.value = (cfg as any).paused ?? false;
    snapshotPersisted();
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    loading.value = false;
  }

  EventsOn("relay:auth-info", async (data: { user_id: string }) => {
    connectedUserID.value = data.user_id || "";
    try {
      const me = await fetchRelayMe();
      connectedEmail.value = me.email || "";
    } catch {
      // Ignore; status row falls back to showing the short user_id.
    }
  });
});

function snapshotPersisted() {
  persistedUrl.value = url.value;
  persistedToken.value = token.value;
  persistedAllowInsecure.value = allowInsecureRelay.value;
  persistedPermission.value = remotePermission.value;
}

async function save() {
  saving.value = true;
  error.value = "";
  try {
    await setRelayConfig({
      url: url.value.trim(),
      token: token.value.trim(),
      allow_insecure_relay: allowInsecureRelay.value,
      remote_permission: remotePermission.value,
    });
    const cfg = await getRelayConfig();
    paused.value = (cfg as any).paused ?? false;
    snapshotPersisted();
    emit("relay-config-changed");
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    saving.value = false;
  }
}

async function handleTogglePaused() {
  togglingPause.value = true;
  error.value = "";
  try {
    await setUplinkPaused(paused.value);
    emit("relay-config-changed");
  } catch (e: any) {
    error.value = e?.message ?? String(e);
    // Revert toggle on failure
    paused.value = !paused.value;
  } finally {
    togglingPause.value = false;
  }
}

function openInBrowser() {
  if (!url.value) return;
  BrowserOpenURL(`${url.value}/settings.html`);
}

const canSave = computed(() => !saving.value && !!url.value.trim());
const saveLabel = computed(() => (saving.value ? "saving…" : "save & connect"));

defineExpose({
  save,
  canSave,
  saveLabel,
  paused,
  saving,
});
</script>

<template>
  <div class="tab-pane">
    <div v-if="loading" class="dim">loading…</div>
    <template v-else>
      <div class="uplink-toggle-row">
        <span class="field-label">Uplink</span>
        <label class="toggle-switch" :class="{ disabled: togglingPause }">
          <input
            v-model="paused"
            type="checkbox"
            :true-value="false"
            :false-value="true"
            :disabled="togglingPause || !url"
            @change="handleTogglePaused"
          />
          <span class="toggle-track">
            <span class="toggle-thumb" />
          </span>
          <span class="toggle-label">{{ paused ? "OFF" : "ON" }}</span>
        </label>
      </div>

      <div class="status-pill" :class="statusPill.cls">
        <span class="dot">●</span>
        {{ statusPill.text }}
      </div>

      <p class="hint">
        configure a remote atterm-relay so this machine's sessions can be
        attached from other devices. when no one is attached, no bytes leave
        this machine.
      </p>

      <label class="field-label">relay url</label>
      <input
        v-model="url"
        type="text"
        placeholder="wss://relay.example.com"
        :disabled="saving"
        @keyup.enter="save"
      />

      <label class="field-label">API token</label>
      <input
        v-model="token"
        type="password"
        placeholder="atk_xxxxxxxx…"
        :disabled="saving"
        @keyup.enter="save"
      />
      <p v-if="tokenWarning" class="token-warning">{{ tokenWarning }}</p>
      <div class="token-actions">
        <button
          v-if="url"
          class="btn-link"
          type="button"
          @click="openInBrowser"
        >Open in browser</button>
      </div>

      <label class="field-label">remote session permissions</label>
      <SelectDropdown
        v-model="remotePermission"
        :options="permissionOptions"
        :disabled="saving"
        aria-label="remote session permissions"
      />
      <p class="hint">
        This is announced as the owner policy for sessions from this desktop.
        Relay-side permissions can still reduce access to view only.
      </p>

      <label class="checkbox">
        <input
          v-model="allowInsecureRelay"
          type="checkbox"
          :disabled="saving"
        />
        enable insecure mode (allow ws:// cleartext relay)
      </label>
      <p v-if="allowInsecureRelay" class="warning">
        ws:// sends the relay token, terminal output, and your input in
        clear text. Use this only on trusted private networks.
      </p>

      <p v-if="error" class="error">{{ error }}</p>
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
.uplink-toggle-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.toggle-switch {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  user-select: none;
}
.toggle-switch.disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.toggle-switch input[type="checkbox"] {
  position: absolute;
  opacity: 0;
  width: 0;
  height: 0;
}
.toggle-track {
  position: relative;
  display: inline-block;
  width: 32px;
  height: 18px;
  background: var(--fg-dim);
  border-radius: 9px;
  transition: background 0.15s;
}
.toggle-switch input:checked + .toggle-track {
  background: var(--good);
}
.toggle-thumb {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 14px;
  height: 14px;
  background: var(--fg);
  border-radius: 50%;
  transition: transform 0.15s;
}
.toggle-switch input:checked + .toggle-track .toggle-thumb {
  transform: translateX(14px);
}
.toggle-label {
  font-size: 12px;
  color: var(--fg-dim);
  min-width: 2.2em;
}
.status-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border: 1px solid var(--border);
  border-radius: 999px;
  font-size: 12px;
  width: max-content;
}
.status-pill .dot {
  font-size: 10px;
  line-height: 1;
}
.status-pill.on .dot { color: var(--good); }
.status-pill.off .dot { color: var(--fg-dim); }
.status-pill.on { color: var(--good); }
.status-pill.off { color: var(--fg-dim); }
.hint {
  font-size: 12px;
  color: var(--fg-dim);
  margin: 0;
  line-height: 1.5;
}
.field-label {
  font-size: 12px;
  color: var(--fg-dim);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.token-warning {
  color: var(--bad);
  font-size: 12px;
  margin: 0;
}
.token-actions {
  display: flex;
  gap: 8px;
}
.btn-link {
  background: none;
  border: none;
  color: var(--accent);
  font-size: 12px;
  padding: 0;
  cursor: pointer;
  text-decoration: underline;
}
.btn-link:hover {
  opacity: 0.8;
}
.checkbox {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--fg);
}
.error {
  color: var(--bad);
  font-size: 12px;
  margin: 0;
}
.warning {
  color: var(--bad);
  font-size: 12px;
  line-height: 1.45;
  margin: 0;
}
input[type="text"],
input[type="password"] {
  height: 32px;
  padding: 6px 10px;
  background: var(--bg);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 6px;
  font-size: 13px;
}
input:focus {
  outline: none;
  box-shadow: 0 0 0 2px var(--accent);
}
</style>
