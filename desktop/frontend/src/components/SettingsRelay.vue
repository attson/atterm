<script lang="ts" setup>
import { computed, onMounted, ref, watch } from "vue";
import { getRelayConfig, setRelayConfig } from "../lib/api";

const emit = defineEmits<{
  (e: "relay-config-changed"): void;
  (e: "dirty", value: boolean): void;
}>();

const url = ref("");
const token = ref("");
const allowInsecureRelay = ref(false);
const remotePermission = ref("full");
const connected = ref(false);
const loading = ref(true);
const saving = ref(false);
const error = ref("");

const persistedUrl = ref("");
const persistedToken = ref("");
const persistedAllowInsecure = ref(false);
const persistedPermission = ref("full");

const dirty = computed(
  () =>
    url.value !== persistedUrl.value ||
    token.value !== persistedToken.value ||
    allowInsecureRelay.value !== persistedAllowInsecure.value ||
    remotePermission.value !== persistedPermission.value,
);

watch(dirty, (value) => emit("dirty", value));

onMounted(async () => {
  try {
    const cfg = await getRelayConfig();
    url.value = cfg.url;
    token.value = cfg.token;
    allowInsecureRelay.value = cfg.allow_insecure_relay;
    remotePermission.value = cfg.remote_permission || "full";
    connected.value = cfg.connected;
    snapshotPersisted();
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    loading.value = false;
  }
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
    connected.value = cfg.connected;
    snapshotPersisted();
    emit("relay-config-changed");
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    saving.value = false;
  }
}

async function disconnect() {
  saving.value = true;
  error.value = "";
  try {
    await setRelayConfig({ url: "", token: "", allow_insecure_relay: false });
    url.value = "";
    token.value = "";
    allowInsecureRelay.value = false;
    remotePermission.value = "full";
    connected.value = false;
    snapshotPersisted();
    emit("relay-config-changed");
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    saving.value = false;
  }
}

const canSave = computed(() => !saving.value && !!url.value.trim());
const saveLabel = computed(() => (saving.value ? "saving…" : "save & connect"));

defineExpose({
  save,
  disconnect,
  canSave,
  saveLabel,
  connected,
  saving,
});
</script>

<template>
  <div class="tab-pane">
    <div v-if="loading" class="dim">loading…</div>
    <template v-else>
      <div class="status-pill" :class="connected ? 'on' : 'off'">
        <span class="dot">●</span>
        {{ connected ? "uplink running" : "uplink stopped" }}
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

      <label class="field-label">token</label>
      <input
        v-model="token"
        type="password"
        placeholder="shared bearer token"
        :disabled="saving"
        @keyup.enter="save"
      />

      <label class="field-label">remote session permissions</label>
      <select v-model="remotePermission" :disabled="saving">
        <option value="view">view only — remote clients can watch output</option>
        <option value="control">control — allow input and resize</option>
        <option value="full">full — allow input, resize, and image paste</option>
      </select>
      <p class="hint">
        This is announced as the owner policy for sessions from this desktop.
        Relay-side read-only tokens can still reduce access to view only.
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
input[type="password"],
select {
  height: 32px;
  padding: 6px 10px;
  background: var(--bg);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 6px;
  font-size: 13px;
}
input:focus,
select:focus {
  outline: none;
  box-shadow: 0 0 0 2px var(--accent);
}
</style>
