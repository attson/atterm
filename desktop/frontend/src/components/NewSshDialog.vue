<script lang="ts" setup>
import { ref } from "vue";
import { newSshSession, type SSHConnectReq } from "../lib/api";
import { parseHostKeyPrompt, type HostKeyPrompt } from "../lib/sshHostKey";

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
// When set, the last attempt failed with an unknown host key: show the
// fingerprint and a confirm button that retries with exactly this (host,
// fingerprint) pair echoed back.
//
// This dialog is the ad-hoc path — it never names a saved host, so the backend
// has no ProxyJump chain to build and hopIndex is always 0. The chain wording
// lives in SshHostsPanel, which is the only place a chain can be reached from.
const pending = ref<HostKeyPrompt | null>(null);

// buildReq takes the acceptance rather than a bool: the retry has to name the
// one key the user was shown, on the one machine they were shown it for.
// "Accept the next unknown key" is what this replaced, and it was not a
// stylistic difference — the Go callback *writes* an accepted key into
// known_hosts, so a blanket accept records keys for machines nobody saw.
//
// Both halves come from the rejection untouched. Rebuilding either one from the
// form would be the same bug: host.value is what the user typed, while the key
// is scoped to the known_hosts name the backend reported ("[h]:2222"), and a
// value that no longer matches means the acceptance is silently ignored.
function buildReq(accepted: HostKeyPrompt | null): SSHConnectReq {
  return {
    host: host.value,
    port: port.value || "22",
    user: user.value,
    auth_kind: authKind.value,
    password: authKind.value === "password" ? password.value : undefined,
    private_key: authKind.value === "key" ? privateKey.value : undefined,
    passphrase: authKind.value === "key" ? passphrase.value : undefined,
    accepted_host_key_host: accepted?.host,
    accepted_host_key_fingerprint: accepted?.fingerprint,
  };
}

async function connect(accepted: HostKeyPrompt | null) {
  if (busy.value) return;
  busy.value = true;
  errorMsg.value = "";
  try {
    const resp = await newSshSession(buildReq(accepted));
    pending.value = null;
    emit("connected", resp.session_id);
  } catch (err) {
    // A prompt after an acceptance means a *different* key was refused, not
    // the one just agreed to — show it as the next question rather than
    // dead-ending on the raw errCodeHostKeyUnknown sentinel, which is all
    // Error() carries.
    const prompt = parseHostKeyPrompt(err);
    if (prompt) {
      pending.value = prompt;
    } else {
      pending.value = null;
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

      <div v-if="pending" class="tofu" data-test="ssh-tofu">
        <p class="warn">Unknown host key. Verify this fingerprint before trusting it:</p>
        <code>{{ pending.fingerprint }}</code>
        <button class="primary" data-test="ssh-accept-hostkey" @click="connect(pending)">
          Accept & Connect
        </button>
      </div>

      <p v-if="errorMsg" class="error" data-test="ssh-error">{{ errorMsg }}</p>

      <div class="row end">
        <button @click="$emit('cancel')">Cancel</button>
        <button class="primary" data-test="ssh-connect" :disabled="busy" @click="connect(null)">
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
