<script lang="ts" setup>
import { ref, onMounted } from "vue";
import {
  listSSHHosts,
  addSSHHost,
  deleteSSHHost,
  newSshSessionByID,
  type SSHHost,
} from "../lib/api";

const emit = defineEmits<{
  (e: "connected", sessionId: string): void;
  (e: "close"): void;
}>();

const hosts = ref<SSHHost[]>([]);
const errorMsg = ref("");
const nAlias = ref("");
const nHost = ref("");
const nPort = ref("22");
const nUser = ref("");
const nPassword = ref("");

async function reload() {
  hosts.value = await listSSHHosts();
}
onMounted(reload);

async function connect(id: string) {
  errorMsg.value = "";
  try {
    const resp = await newSshSessionByID(id);
    emit("connected", resp.session_id);
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}

async function add() {
  errorMsg.value = "";
  try {
    await addSSHHost(
      {
        id: "",
        alias: nAlias.value,
        host: nHost.value,
        port: nPort.value || "22",
        user: nUser.value,
        auth_kind: "password",
      },
      { password: nPassword.value },
    );
    nAlias.value = "";
    nHost.value = "";
    nPort.value = "22";
    nUser.value = "";
    nPassword.value = "";
    await reload();
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}

async function remove(id: string) {
  errorMsg.value = "";
  try {
    await deleteSSHHost(id);
    await reload();
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}
</script>

<template>
  <div class="backdrop" @click.self="$emit('close')">
    <div class="panel">
      <h2>SSH Hosts</h2>

      <ul v-if="hosts.length">
        <li v-for="h in hosts" :key="h.id">
          <span class="name">{{ h.alias || h.user + "@" + h.host }}</span>
          <button :data-test="`ssh-connect-${h.id}`" @click="connect(h.id)">Connect</button>
          <button class="danger" :data-test="`ssh-delete-${h.id}`" @click="remove(h.id)">Delete</button>
        </li>
      </ul>
      <p v-else class="dim">No saved hosts yet.</p>

      <div class="add">
        <input data-test="ssh-add-alias" v-model="nAlias" placeholder="alias (optional)" />
        <input data-test="ssh-add-host" v-model="nHost" placeholder="host" />
        <input data-test="ssh-add-port" v-model="nPort" placeholder="port" />
        <input data-test="ssh-add-user" v-model="nUser" placeholder="user" />
        <input data-test="ssh-add-password" type="password" v-model="nPassword" placeholder="password" />
        <button class="primary" data-test="ssh-add-submit" @click="add">Add Host</button>
      </div>

      <p v-if="errorMsg" class="error" data-test="ssh-hosts-error">{{ errorMsg }}</p>

      <div class="row end">
        <button @click="$emit('close')">Close</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.6);
  display: flex; align-items: center; justify-content: center; z-index: 110;
}
.panel {
  background: var(--panel); border: 1px solid var(--border);
  border-radius: 8px; padding: 20px 24px; width: 520px;
  max-width: calc(100vw - 32px);
  display: flex; flex-direction: column; gap: 12px;
}
.panel h2 {
  margin: 0; font-size: 14px; font-weight: 600;
  letter-spacing: 0.05em; text-transform: uppercase; color: var(--fg-dim);
}
.panel ul { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.panel li { display: flex; align-items: center; gap: 8px; }
.panel .name { flex: 1; color: var(--fg); font-size: 13px; }
.panel .dim { color: var(--fg-dim); font-size: 12px; margin: 0; }
.add { display: flex; flex-wrap: wrap; gap: 6px; }
.add input {
  background: var(--bg); border: 1px solid var(--border); border-radius: 4px;
  padding: 5px 7px; color: var(--fg); font-size: 12px;
}
.error { color: var(--bad); font-size: 12px; margin: 0; }
.row.end { display: flex; justify-content: flex-end; }
.primary { background: var(--accent); color: #0d1117; border-color: var(--accent); font-weight: 600; }
.danger { color: var(--bad); }
</style>
