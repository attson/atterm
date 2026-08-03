<script lang="ts" setup>
import { ref } from "vue";
import { newSshSession, type SSHConnectReq } from "../lib/api";

const emit = defineEmits<{
  (e: "connected", sessionId: string): void;
  (e: "cancel"): void;
}>();

const host = ref("");
const port = ref("22");
const user = ref("");
const authKind = ref<"password" | "key">("password");
const password = ref("");
const privateKey = ref("");
const passphrase = ref("");

const busy = ref(false);
const errorMsg = ref("");
// When set, the last attempt failed with an unknown host key; show the
// fingerprint and a confirm button that retries with accept_host_key.
const pendingFingerprint = ref("");

// A rejected NewSshSession whose message is the errCodeHostKeyUnknown sentinel
// carries a Fingerprint field (Go HostKeyUnknownError). Detect it structurally.
function unknownHostFingerprint(err: unknown): string {
  if (err && typeof err === "object" && "Fingerprint" in err) {
    const fp = (err as { Fingerprint?: unknown }).Fingerprint;
    if (typeof fp === "string" && fp) return fp;
  }
  return "";
}

function buildReq(acceptHostKey: boolean): SSHConnectReq {
  return {
    host: host.value,
    port: port.value || "22",
    user: user.value,
    auth_kind: authKind.value,
    password: authKind.value === "password" ? password.value : undefined,
    private_key: authKind.value === "key" ? privateKey.value : undefined,
    passphrase: authKind.value === "key" ? passphrase.value : undefined,
    accept_host_key: acceptHostKey,
  };
}

async function connect(acceptHostKey: boolean) {
  if (busy.value) return;
  busy.value = true;
  errorMsg.value = "";
  try {
    const resp = await newSshSession(buildReq(acceptHostKey));
    pendingFingerprint.value = "";
    emit("connected", resp.session_id);
  } catch (err) {
    const fp = unknownHostFingerprint(err);
    if (fp && !acceptHostKey) {
      pendingFingerprint.value = fp;
    } else {
      pendingFingerprint.value = "";
      errorMsg.value = err instanceof Error ? err.message : String(err);
    }
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="backdrop" @click.self="$emit('cancel')">
    <div class="dialog">
      <h2>New SSH Connection</h2>

      <label>Host
        <input data-test="ssh-host" v-model="host" autocomplete="off" />
      </label>
      <label>Port
        <input data-test="ssh-port" v-model="port" autocomplete="off" />
      </label>
      <label>User
        <input data-test="ssh-user" v-model="user" autocomplete="off" />
      </label>

      <div class="row">
        <label class="inline">
          <input type="radio" value="password" v-model="authKind" data-test="ssh-auth-password" />
          Password
        </label>
        <label class="inline">
          <input type="radio" value="key" v-model="authKind" data-test="ssh-auth-key" />
          Private key
        </label>
      </div>

      <label v-if="authKind === 'password'">Password
        <input data-test="ssh-password" type="password" v-model="password" autocomplete="off" />
      </label>

      <template v-else>
        <label>Private key (PEM)
          <textarea data-test="ssh-private-key" v-model="privateKey" rows="4"></textarea>
        </label>
        <label>Passphrase (optional)
          <input data-test="ssh-passphrase" type="password" v-model="passphrase" autocomplete="off" />
        </label>
      </template>

      <div v-if="pendingFingerprint" class="tofu" data-test="ssh-tofu">
        <p class="warn">Unknown host key. Verify this fingerprint before trusting it:</p>
        <code>{{ pendingFingerprint }}</code>
        <button class="primary" data-test="ssh-accept-hostkey" @click="connect(true)">
          Accept & Connect
        </button>
      </div>

      <p v-if="errorMsg" class="error" data-test="ssh-error">{{ errorMsg }}</p>

      <div class="row end">
        <button @click="$emit('cancel')">Cancel</button>
        <button class="primary" data-test="ssh-connect" :disabled="busy" @click="connect(false)">
          Connect
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.6);
  display: flex; align-items: center; justify-content: center; z-index: 110;
}
.dialog {
  background: var(--panel); border: 1px solid var(--border);
  border-radius: 8px; padding: 20px 24px; width: 460px;
  max-width: calc(100vw - 32px);
  display: flex; flex-direction: column; gap: 10px;
}
.dialog h2 {
  margin: 0; font-size: 14px; font-weight: 600;
  letter-spacing: 0.05em; text-transform: uppercase; color: var(--fg-dim);
}
.dialog label {
  display: flex; flex-direction: column; gap: 4px;
  font-size: 12px; color: var(--fg-dim);
}
.dialog label.inline {
  flex-direction: row; align-items: center; gap: 6px; color: var(--fg);
}
.dialog input, .dialog textarea {
  background: var(--bg); border: 1px solid var(--border); border-radius: 4px;
  padding: 6px 8px; color: var(--fg); font-size: 13px;
}
.row { display: flex; gap: 12px; }
.row.end { justify-content: flex-end; margin-top: 4px; }
.tofu {
  display: flex; flex-direction: column; gap: 6px;
  border: 1px solid #d29922; border-radius: 6px; padding: 10px;
}
.tofu code { font-size: 12px; word-break: break-all; color: var(--fg); }
.warn { color: #d29922; font-size: 12px; margin: 0; }
.error { color: var(--bad); font-size: 12px; margin: 0; }
.primary {
  background: var(--accent); color: #0d1117; border-color: var(--accent);
  font-weight: 600;
}
</style>
