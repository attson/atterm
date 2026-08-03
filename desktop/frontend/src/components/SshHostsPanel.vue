<script lang="ts" setup>
import { ref, computed, onMounted } from "vue";
import { Server, Plus, X, Pencil, Trash2, Search, Zap, Folder, KeyRound } from "lucide-vue-next";
import {
  listSSHHosts,
  addSSHHost,
  updateSSHHost,
  deleteSSHHost,
  newSshSessionByID,
  listSSHKeys,
  addSSHKey,
  updateSSHKey,
  deleteSSHKey,
  type SSHHost,
  type SSHKey,
} from "../lib/api";

const emit = defineEmits<{
  (e: "connected", sessionId: string): void;
  (e: "close"): void;
}>();

type Tab = "hosts" | "keys";
const activeTab = ref<Tab>("hosts");

const hosts = ref<SSHHost[]>([]);
const keys = ref<SSHKey[]>([]);
const errorMsg = ref("");
const query = ref("");
const connectingId = ref("");

async function reload() {
  [hosts.value, keys.value] = await Promise.all([listSSHHosts(), listSSHKeys()]);
}
onMounted(reload);

// ---- Hosts ----
const filteredHosts = computed(() => {
  const q = query.value.trim().toLowerCase();
  const list = q
    ? hosts.value.filter((h) =>
        `${h.alias ?? ""} ${h.host} ${h.user} ${h.group ?? ""}`.toLowerCase().includes(q),
      )
    : hosts.value;
  return list;
});

const hostGroups = computed(() => {
  const map = new Map<string, SSHHost[]>();
  for (const h of filteredHosts.value) {
    const g = h.group?.trim() || "";
    if (!map.has(g)) map.set(g, []);
    map.get(g)!.push(h);
  }
  return [...map.entries()].sort(([a], [b]) => {
    if (a === "") return 1;
    if (b === "") return -1;
    return a.localeCompare(b);
  });
});

function hostLabel(h: SSHHost): string {
  return h.alias?.trim() || `${h.user}@${h.host}`;
}
function hostSubtitle(h: SSHHost): string {
  const port = h.port && h.port !== "22" ? `:${h.port}` : "";
  const auth = h.auth_kind === "key" ? "key" : "password";
  return `${h.user}@${h.host}${port} · ${auth}`;
}

async function connect(id: string) {
  if (connectingId.value) return;
  errorMsg.value = "";
  connectingId.value = id;
  try {
    const resp = await newSshSessionByID(id);
    emit("connected", resp.session_id);
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  } finally {
    connectingId.value = "";
  }
}

// ---- Host drawer ----
const hostDrawer = ref(false);
const hostEditId = ref<string | null>(null);
const fAlias = ref("");
const fHost = ref("");
const fPort = ref("22");
const fUser = ref("");
const fGroup = ref("");
const fAuthKind = ref<"password" | "key">("password");
const fPassword = ref("");
const fKeyID = ref("");

function openNewHost() {
  hostEditId.value = "";
  fAlias.value = "";
  fHost.value = "";
  fPort.value = "22";
  fUser.value = "";
  fGroup.value = "";
  fAuthKind.value = "password";
  fPassword.value = "";
  fKeyID.value = keys.value[0]?.id ?? "";
  hostDrawer.value = true;
}
function openEditHost(h: SSHHost) {
  hostEditId.value = h.id;
  fAlias.value = h.alias ?? "";
  fHost.value = h.host;
  fPort.value = h.port || "22";
  fUser.value = h.user;
  fGroup.value = h.group ?? "";
  fAuthKind.value = h.auth_kind;
  fPassword.value = "";
  fKeyID.value = h.key_id ?? keys.value[0]?.id ?? "";
  hostDrawer.value = true;
}
function closeHostDrawer() {
  hostDrawer.value = false;
  hostEditId.value = null;
}
const canSaveHost = computed(() => {
  if (fHost.value.trim() === "" || fUser.value.trim() === "") return false;
  if (fAuthKind.value === "key") return fKeyID.value !== "";
  return true;
});
async function saveHost() {
  if (!canSaveHost.value) return;
  errorMsg.value = "";
  const base = {
    alias: fAlias.value.trim(),
    host: fHost.value.trim(),
    port: fPort.value.trim() || "22",
    user: fUser.value.trim(),
    group: fGroup.value.trim(),
    auth_kind: fAuthKind.value,
    key_id: fAuthKind.value === "key" ? fKeyID.value : undefined,
  };
  const cred = { password: fAuthKind.value === "password" ? fPassword.value : undefined };
  const hasNewCred = fAuthKind.value === "password" && fPassword.value !== "";
  try {
    if (hostEditId.value) {
      await updateSSHHost({ id: hostEditId.value, ...base }, hasNewCred ? cred : null);
    } else {
      await addSSHHost({ id: "", ...base }, cred);
    }
    closeHostDrawer();
    await reload();
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}
async function removeHost(id: string) {
  errorMsg.value = "";
  try {
    await deleteSSHHost(id);
    await reload();
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}

// ---- Key drawer ----
const keyDrawer = ref(false);
const keyEditId = ref<string | null>(null);
const kName = ref("");
const kPem = ref("");
const kPassphrase = ref("");

function openNewKey() {
  keyEditId.value = "";
  kName.value = "";
  kPem.value = "";
  kPassphrase.value = "";
  keyDrawer.value = true;
}
// Jump from the host form's empty-vault hint straight into adding a key:
// close the host drawer, switch to the Keys tab, open the New Key drawer.
function jumpToNewKey() {
  closeHostDrawer();
  activeTab.value = "keys";
  openNewKey();
}
function openEditKey(k: SSHKey) {
  keyEditId.value = k.id;
  kName.value = k.name;
  kPem.value = "";
  kPassphrase.value = "";
  keyDrawer.value = true;
}
function closeKeyDrawer() {
  keyDrawer.value = false;
  keyEditId.value = null;
}
const canSaveKey = computed(() => kName.value.trim() !== "" && (keyEditId.value ? true : kPem.value.trim() !== ""));
async function saveKey() {
  if (!canSaveKey.value) return;
  errorMsg.value = "";
  try {
    if (keyEditId.value) {
      await updateSSHKey(keyEditId.value, kName.value.trim(), kPem.value, kPassphrase.value);
    } else {
      await addSSHKey(kName.value.trim(), kPem.value, kPassphrase.value);
    }
    closeKeyDrawer();
    await reload();
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}
async function removeKey(id: string) {
  errorMsg.value = "";
  try {
    await deleteSSHKey(id);
    await reload();
  } catch (e) {
    // Reference guard errors ("key in use by: ...") surface here.
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}
</script>

<template>
  <div class="ssh-overlay" @click.self="$emit('close')">
    <div class="ssh-shell">
      <!-- Header -->
      <header class="ssh-header">
        <div class="tabs">
          <button
            class="tab" :class="{ on: activeTab === 'hosts' }"
            data-test="ssh-tab-hosts" @click="activeTab = 'hosts'"
          ><Server :size="14" /> Hosts <span class="tab-count">{{ hosts.length }}</span></button>
          <button
            class="tab" :class="{ on: activeTab === 'keys' }"
            data-test="ssh-tab-keys" @click="activeTab = 'keys'"
          ><KeyRound :size="14" /> Keys <span class="tab-count">{{ keys.length }}</span></button>
        </div>
        <div v-if="activeTab === 'hosts'" class="search">
          <Search :size="13" class="search-icon" />
          <input v-model="query" data-test="ssh-search" placeholder="Filter hosts…" spellcheck="false" autocomplete="off" />
        </div>
        <div v-else class="search-spacer" />
        <button v-if="activeTab === 'hosts'" class="new-btn" data-test="ssh-new-host" @click="openNewHost">
          <Plus :size="14" /> New Host
        </button>
        <button v-else class="new-btn" data-test="ssh-key-new" @click="openNewKey">
          <Plus :size="14" /> New Key
        </button>
        <button class="close-x" title="Close" @click="$emit('close')"><X :size="16" /></button>
      </header>

      <p v-if="errorMsg" class="ssh-error" data-test="ssh-hosts-error">{{ errorMsg }}</p>

      <!-- HOSTS TAB -->
      <div v-show="activeTab === 'hosts'" class="ssh-body">
        <div v-if="hosts.length === 0" class="empty">
          <Server :size="40" class="empty-icon" />
          <p class="empty-title">No saved hosts</p>
          <p class="empty-sub">Add a host to connect and take it over from any device.</p>
          <button class="new-btn ghost" @click="openNewHost"><Plus :size="14" /> New Host</button>
        </div>
        <template v-else>
          <section v-for="[gname, ghosts] in hostGroups" :key="gname || '__ungrouped'" class="group">
            <div class="group-head">
              <Folder :size="12" class="group-icon" />
              <span class="group-name">{{ gname || "Ungrouped" }}</span>
              <span class="group-count">{{ ghosts.length }}</span>
            </div>
            <div class="card-grid">
              <article
                v-for="h in ghosts" :key="h.id" class="card"
                :class="{ busy: connectingId === h.id }" @dblclick="connect(h.id)"
              >
                <div class="card-glyph">
                  <KeyRound v-if="h.auth_kind === 'key'" :size="16" />
                  <Server v-else :size="16" />
                </div>
                <div class="card-main">
                  <div class="card-label">{{ hostLabel(h) }}</div>
                  <div class="card-sub">{{ hostSubtitle(h) }}</div>
                </div>
                <div class="card-actions">
                  <button class="act connect" :data-test="`ssh-connect-${h.id}`" :disabled="connectingId === h.id" title="Connect" @click.stop="connect(h.id)"><Zap :size="13" /></button>
                  <button class="act" title="Edit" @click.stop="openEditHost(h)"><Pencil :size="13" /></button>
                  <button class="act danger" :data-test="`ssh-delete-${h.id}`" title="Delete" @click.stop="removeHost(h.id)"><Trash2 :size="13" /></button>
                </div>
              </article>
            </div>
          </section>
        </template>
      </div>

      <!-- KEYS TAB -->
      <div v-show="activeTab === 'keys'" class="ssh-body">
        <div v-if="keys.length === 0" class="empty">
          <KeyRound :size="40" class="empty-icon" />
          <p class="empty-title">No keys yet</p>
          <p class="empty-sub">Add a private key here, then reference it from a host.</p>
          <button class="new-btn ghost" @click="openNewKey"><Plus :size="14" /> New Key</button>
        </div>
        <div v-else class="card-grid">
          <article v-for="k in keys" :key="k.id" class="card">
            <div class="card-glyph"><KeyRound :size="16" /></div>
            <div class="card-main">
              <div class="card-label">{{ k.name }}</div>
              <div class="card-sub">{{ k.key_type ? "Type " + k.key_type : "SSH key" }}</div>
            </div>
            <div class="card-actions">
              <button class="act" title="Edit" @click.stop="openEditKey(k)"><Pencil :size="13" /></button>
              <button class="act danger" :data-test="`ssh-key-delete-${k.id}`" title="Delete" @click.stop="removeKey(k.id)"><Trash2 :size="13" /></button>
            </div>
          </article>
        </div>
      </div>

      <!-- HOST DRAWER -->
      <transition name="drawer">
        <aside v-if="hostDrawer" class="drawer">
          <div class="drawer-head"><span>{{ hostEditId ? "Edit Host" : "New Host" }}</span><button class="close-x" @click="closeHostDrawer"><X :size="15" /></button></div>
          <div class="drawer-body">
            <label class="field"><span class="fl">Address</span><input data-test="ssh-add-host" v-model="fHost" placeholder="IP or hostname" spellcheck="false" autocomplete="off" /></label>
            <div class="field-row">
              <label class="field grow"><span class="fl">Label</span><input data-test="ssh-add-alias" v-model="fAlias" placeholder="optional" autocomplete="off" /></label>
              <label class="field port"><span class="fl">Port</span><input data-test="ssh-add-port" v-model="fPort" autocomplete="off" /></label>
            </div>
            <label class="field"><span class="fl">Group</span><input data-test="ssh-add-group" v-model="fGroup" placeholder="optional" autocomplete="off" /></label>
            <label class="field"><span class="fl">Username</span><input data-test="ssh-add-user" v-model="fUser" placeholder="user" autocomplete="off" /></label>
            <div class="seg">
              <button :class="{ on: fAuthKind === 'password' }" data-test="ssh-auth-password" @click="fAuthKind = 'password'">Password</button>
              <button :class="{ on: fAuthKind === 'key' }" data-test="ssh-auth-key" @click="fAuthKind = 'key'">Key</button>
            </div>
            <label v-if="fAuthKind === 'password'" class="field">
              <span class="fl">Password<template v-if="hostEditId"> <em>(leave blank to keep)</em></template></span>
              <input data-test="ssh-add-password" type="password" v-model="fPassword" autocomplete="off" />
            </label>
            <template v-else>
              <label v-if="keys.length" class="field">
                <span class="fl">Key</span>
                <select data-test="ssh-add-keyid" v-model="fKeyID" class="select">
                  <option v-for="k in keys" :key="k.id" :value="k.id">{{ k.name }}{{ k.key_type ? " (" + k.key_type + ")" : "" }}</option>
                </select>
              </label>
              <div v-else class="empty-keys-hint">
                <p class="hint">No keys yet.</p>
                <button class="btn primary sm" data-test="ssh-host-add-key" @click="jumpToNewKey">
                  <Plus :size="13" /> Add a key
                </button>
              </div>
            </template>
          </div>
          <div class="drawer-foot">
            <button class="btn ghost" @click="closeHostDrawer">Cancel</button>
            <button class="btn primary" data-test="ssh-add-submit" :disabled="!canSaveHost" @click="saveHost">{{ hostEditId ? "Save" : "Add Host" }}</button>
          </div>
        </aside>
      </transition>

      <!-- KEY DRAWER -->
      <transition name="drawer">
        <aside v-if="keyDrawer" class="drawer">
          <div class="drawer-head"><span>{{ keyEditId ? "Edit Key" : "New Key" }}</span><button class="close-x" @click="closeKeyDrawer"><X :size="15" /></button></div>
          <div class="drawer-body">
            <label class="field"><span class="fl">Name</span><input data-test="ssh-key-name" v-model="kName" placeholder="e.g. aws" autocomplete="off" /></label>
            <label class="field">
              <span class="fl">Private key (PEM)<template v-if="keyEditId"> <em>(leave blank to keep)</em></template></span>
              <textarea data-test="ssh-key-pem" v-model="kPem" rows="6" spellcheck="false" placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"></textarea>
            </label>
            <label class="field"><span class="fl">Passphrase</span><input data-test="ssh-key-passphrase" type="password" v-model="kPassphrase" autocomplete="off" /></label>
          </div>
          <div class="drawer-foot">
            <button class="btn ghost" @click="closeKeyDrawer">Cancel</button>
            <button class="btn primary" data-test="ssh-key-submit" :disabled="!canSaveKey" @click="saveKey">{{ keyEditId ? "Save" : "Add Key" }}</button>
          </div>
        </aside>
      </transition>
    </div>
  </div>
</template>

<style scoped>
.ssh-overlay {
  position: fixed; inset: 0; z-index: 120;
  background: rgba(1, 4, 9, 0.72); backdrop-filter: blur(3px);
  display: flex; align-items: center; justify-content: center;
}
.ssh-shell {
  position: relative;
  width: min(1040px, calc(100vw - 48px)); height: min(720px, calc(100vh - 48px));
  background: radial-gradient(120% 80% at 100% 0%, rgba(88, 166, 255, 0.06), transparent 55%), var(--bg);
  border: 1px solid var(--border); border-radius: 12px;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.55); overflow: hidden;
  display: flex; flex-direction: column;
}
.ssh-header {
  display: flex; align-items: center; gap: 12px;
  padding: 10px 16px; border-bottom: 1px solid var(--border); background: var(--panel);
}
.tabs { display: flex; gap: 4px; }
.tab {
  display: inline-flex; align-items: center; gap: 6px;
  background: transparent; border: 1px solid transparent; color: var(--fg-dim);
  font-size: 12px; font-weight: 600; letter-spacing: 0.04em;
  padding: 6px 12px; border-radius: 7px; cursor: pointer; transition: all 120ms;
}
.tab:hover { color: var(--fg); }
.tab.on { color: var(--fg); background: var(--bg); border-color: var(--border); }
.tab-count { font-size: 10px; color: var(--fg-dim); background: rgba(139,148,158,0.16); padding: 1px 6px; border-radius: 999px; }
.search { flex: 1; max-width: 360px; display: flex; align-items: center; gap: 7px; background: var(--bg); border: 1px solid var(--border); border-radius: 7px; padding: 0 10px; }
.search-spacer { flex: 1; }
.search-icon { color: var(--fg-dim); flex: none; }
.search input { flex: 1; background: transparent; border: none; outline: none; color: var(--fg); font-size: 13px; padding: 7px 0; }
.new-btn { display: inline-flex; align-items: center; gap: 5px; background: var(--accent); color: #04101f; border: none; font-size: 12px; font-weight: 600; padding: 7px 12px; border-radius: 7px; cursor: pointer; transition: filter 120ms; }
.new-btn:hover { filter: brightness(1.1); }
.new-btn.ghost { background: transparent; color: var(--accent); border: 1px solid var(--accent); }
.close-x { display: inline-flex; align-items: center; justify-content: center; background: transparent; border: none; color: var(--fg-dim); cursor: pointer; padding: 4px; border-radius: 6px; transition: color 120ms, background 120ms; }
.close-x:hover { color: var(--fg); background: rgba(139, 148, 158, 0.12); }
.ssh-error { margin: 0; padding: 8px 16px; font-size: 12px; color: var(--bad); background: rgba(248, 81, 73, 0.08); border-bottom: 1px solid rgba(248, 81, 73, 0.2); }
.ssh-body { flex: 1; overflow-y: auto; padding: 18px 16px 24px; }
.group { margin-bottom: 22px; }
.group-head { display: flex; align-items: center; gap: 7px; margin: 0 2px 10px; color: var(--fg-dim); }
.group-icon { color: var(--neutral); }
.group-name { font-size: 11px; font-weight: 600; letter-spacing: 0.1em; text-transform: uppercase; }
.group-count { font-size: 10px; color: var(--neutral); }
.card-grid { display: grid; gap: 10px; grid-template-columns: repeat(auto-fill, minmax(288px, 1fr)); }
.card { position: relative; display: flex; align-items: center; gap: 11px; padding: 12px; background: var(--panel); border: 1px solid var(--border); border-radius: 10px; transition: border-color 140ms, transform 140ms, box-shadow 140ms; }
.card:hover { border-color: var(--accent); transform: translateY(-1px); box-shadow: 0 6px 18px rgba(0, 0, 0, 0.35); }
.card.busy { opacity: 0.6; }
.card-glyph { flex: none; width: 34px; height: 34px; border-radius: 8px; display: flex; align-items: center; justify-content: center; color: var(--accent); background: rgba(88, 166, 255, 0.1); border: 1px solid rgba(88, 166, 255, 0.18); }
.card-main { flex: 1; min-width: 0; }
.card-label { font-size: 13px; font-weight: 600; color: var(--fg); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.card-sub { font-size: 11px; color: var(--fg-dim); font-family: var(--font-mono-strict); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; margin-top: 2px; }
.card-actions { flex: none; display: flex; gap: 4px; align-items: center; }
.act { display: inline-flex; align-items: center; justify-content: center; width: 28px; height: 28px; border-radius: 6px; background: var(--bg); border: 1px solid var(--border); color: var(--fg); cursor: pointer; transition: color 120ms, background 120ms, border-color 120ms; }
.act svg { display: block; flex: none; width: 15px; height: 15px; }
.act:hover { color: #fff; border-color: var(--neutral); background: rgba(139, 148, 158, 0.18); }
.act.connect { background: var(--accent); border-color: var(--accent); color: #04101f; }
.act.connect:hover:not(:disabled) { filter: brightness(1.12); }
.act.danger { color: var(--fg-dim); }
.act.danger:hover { color: #fff; border-color: var(--bad); background: rgba(248, 81, 73, 0.2); }
.act:disabled { opacity: 0.5; cursor: default; }
.empty { height: 100%; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 6px; color: var(--fg-dim); }
.empty-icon { color: var(--neutral); opacity: 0.5; margin-bottom: 6px; }
.empty-title { margin: 0; font-size: 14px; color: var(--fg); font-weight: 600; }
.empty-sub { margin: 0 0 12px; font-size: 12px; }
.drawer { position: absolute; top: 0; right: 0; bottom: 0; width: 360px; max-width: 84%; background: var(--panel); border-left: 1px solid var(--border); box-shadow: -20px 0 48px rgba(0, 0, 0, 0.45); display: flex; flex-direction: column; }
.drawer-head { display: flex; align-items: center; justify-content: space-between; padding: 14px 16px; border-bottom: 1px solid var(--border); font-size: 12px; font-weight: 700; letter-spacing: 0.1em; text-transform: uppercase; color: var(--fg-dim); }
.drawer-body { flex: 1; overflow-y: auto; padding: 16px; display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field-row { display: flex; gap: 10px; }
.field.grow { flex: 1; }
.field.port { width: 78px; flex: none; }
.fl { font-size: 11px; color: var(--fg-dim); }
.fl em { color: var(--neutral); font-style: normal; }
.field input, .field textarea, .select { background: var(--bg); border: 1px solid var(--border); border-radius: 6px; padding: 7px 9px; color: var(--fg); font-size: 13px; outline: none; transition: border-color 120ms; }
.field input:focus, .field textarea:focus, .select:focus { border-color: var(--accent); }
.field textarea { resize: vertical; font-family: var(--font-mono-strict); font-size: 12px; }
.select { cursor: pointer; }
.hint { font-size: 12px; color: var(--fg-dim); margin: 0; }
.empty-keys-hint { display: flex; flex-direction: column; align-items: flex-start; gap: 8px; }
.btn.sm { display: inline-flex; align-items: center; gap: 5px; padding: 6px 11px; font-size: 12px; }
.btn.sm svg { display: block; }
.seg { display: flex; background: var(--bg); border: 1px solid var(--border); border-radius: 7px; padding: 3px; gap: 3px; }
.seg button { flex: 1; background: transparent; border: none; color: var(--fg-dim); font-size: 12px; padding: 6px 0; border-radius: 5px; cursor: pointer; transition: all 120ms; }
.seg button.on { background: var(--accent); color: #04101f; font-weight: 600; }
.drawer-foot { display: flex; justify-content: flex-end; gap: 8px; padding: 12px 16px; border-top: 1px solid var(--border); }
.btn { font-size: 12px; padding: 8px 14px; border-radius: 7px; cursor: pointer; border: 1px solid var(--border); background: transparent; color: var(--fg); transition: all 120ms; }
.btn.ghost:hover { background: rgba(139, 148, 158, 0.1); }
.btn.primary { background: var(--accent); color: #04101f; border-color: var(--accent); font-weight: 600; }
.btn.primary:hover:not(:disabled) { filter: brightness(1.1); }
.btn.primary:disabled { opacity: 0.4; cursor: default; }
.drawer-enter-active, .drawer-leave-active { transition: transform 180ms ease, opacity 180ms ease; }
.drawer-enter-from, .drawer-leave-to { transform: translateX(24px); opacity: 0; }
</style>
