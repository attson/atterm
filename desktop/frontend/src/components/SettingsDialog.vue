<script lang="ts" setup>
import { onMounted, ref } from "vue";
import { getRelayConfig, setRelayConfig } from "../lib/api";

const emit = defineEmits<{ (e: "close"): void }>();

const url = ref("");
const token = ref("");
const connected = ref(false);
const loading = ref(true);
const saving = ref(false);
const error = ref("");

onMounted(async () => {
  try {
    const cfg = await getRelayConfig();
    url.value = cfg.url;
    token.value = cfg.token;
    connected.value = cfg.connected;
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    loading.value = false;
  }
});

async function save() {
  saving.value = true;
  error.value = "";
  try {
    await setRelayConfig({ url: url.value.trim(), token: token.value.trim() });
    const cfg = await getRelayConfig();
    connected.value = cfg.connected;
    emit("close");
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
    await setRelayConfig({ url: "", token: "" });
    url.value = "";
    token.value = "";
    connected.value = false;
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    saving.value = false;
  }
}

function close() {
  if (!saving.value) emit("close");
}
</script>

<template>
  <div class="backdrop" @click.self="close">
    <div class="dialog">
      <h2>relay settings</h2>

      <div v-if="loading" class="dim">loading…</div>
      <template v-else>
        <p class="hint">
          configure a remote atterm-relay so this machine's sessions can be
          attached from other devices. when no one is attached, no bytes leave
          this machine.
        </p>

        <label>relay url</label>
        <input
          v-model="url"
          type="text"
          placeholder="ws://relay.example.com:8080"
          :disabled="saving"
          @keyup.enter="save"
        />

        <label>token</label>
        <input
          v-model="token"
          type="password"
          placeholder="shared bearer token"
          :disabled="saving"
          @keyup.enter="save"
        />

        <div class="status">
          <span :class="connected ? 'on' : 'off'">●</span>
          {{ connected ? "uplink running" : "uplink stopped" }}
        </div>

        <div v-if="error" class="error">{{ error }}</div>

        <div class="row">
          <button @click="close" :disabled="saving">cancel</button>
          <button
            v-if="connected"
            @click="disconnect"
            :disabled="saving"
            class="danger"
          >disconnect</button>
          <button
            class="primary"
            @click="save"
            :disabled="saving || !url.trim()"
          >
            {{ saving ? "saving…" : "save & connect" }}
          </button>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
}
.dialog {
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 20px 24px;
  width: 460px;
  max-width: calc(100vw - 32px);
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.dialog h2 {
  margin: 0 0 12px;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--fg-dim);
}
.hint {
  font-size: 12px;
  color: var(--fg-dim);
  margin: 0 0 8px;
  line-height: 1.5;
}
label {
  font-size: 12px;
  color: var(--fg-dim);
  margin-top: 6px;
}
.status {
  font-size: 12px;
  color: var(--fg-dim);
  margin-top: 10px;
  display: flex;
  align-items: center;
  gap: 6px;
}
.status .on { color: var(--good); }
.status .off { color: var(--fg-dim); }
.dim { color: var(--fg-dim); font-size: 13px; padding: 8px 0; }
.error {
  color: var(--bad);
  font-size: 12px;
  margin-top: 6px;
}
.row {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 16px;
}
button.danger {
  border-color: var(--bad);
  color: var(--bad);
}
button.danger:hover { background: rgba(248, 81, 73, 0.1); }
</style>
