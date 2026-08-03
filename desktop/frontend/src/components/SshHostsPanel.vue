<script lang="ts" setup>
import { ref, computed, onMounted } from "vue";
import { Server, Plus, X, Pencil, Trash2, Search, Zap, Folder, KeyRound } from "lucide-vue-next";
import {
  listSSHHosts,
  addSSHHost,
  updateSSHHost,
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
const query = ref("");
const connectingId = ref("");

// Right-side form drawer state. editingId="" means "new host".
const drawerOpen = ref(false);
const editingId = ref<string | null>(null);
const fAlias = ref("");
const fHost = ref("");
const fPort = ref("22");
const fUser = ref("");
const fGroup = ref("");
const fAuthKind = ref<"password" | "privateKey">("password");
const fPassword = ref("");
const fPrivateKey = ref("");
const fPassphrase = ref("");

async function reload() {
  hosts.value = await listSSHHosts();
}
onMounted(reload);

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase();
  if (!q) return hosts.value;
  return hosts.value.filter((h) => {
    const hay = `${h.alias ?? ""} ${h.host} ${h.user} ${h.group ?? ""}`.toLowerCase();
    return hay.includes(q);
  });
});

// Group hosts by their group label; ungrouped fall under "".
const groups = computed(() => {
  const map = new Map<string, SSHHost[]>();
  for (const h of filtered.value) {
    const g = h.group?.trim() || "";
    if (!map.has(g)) map.set(g, []);
    map.get(g)!.push(h);
  }
  // Named groups first (alphabetical), ungrouped last.
  return [...map.entries()].sort(([a], [b]) => {
    if (a === "") return 1;
    if (b === "") return -1;
    return a.localeCompare(b);
  });
});

function label(h: SSHHost): string {
  return h.alias?.trim() || `${h.user}@${h.host}`;
}

function subtitle(h: SSHHost): string {
  const port = h.port && h.port !== "22" ? `:${h.port}` : "";
  const auth = h.auth_kind === "privateKey" ? "key" : "password";
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

function openNew() {
  editingId.value = "";
  fAlias.value = "";
  fHost.value = "";
  fPort.value = "22";
  fUser.value = "";
  fGroup.value = "";
  fAuthKind.value = "password";
  fPassword.value = "";
  fPrivateKey.value = "";
  fPassphrase.value = "";
  drawerOpen.value = true;
}

function openEdit(h: SSHHost) {
  editingId.value = h.id;
  fAlias.value = h.alias ?? "";
  fHost.value = h.host;
  fPort.value = h.port || "22";
  fUser.value = h.user;
  fGroup.value = h.group ?? "";
  fAuthKind.value = h.auth_kind;
  // Credentials are write-only from the keyring; leave blank on edit and
  // only overwrite when the user types a new one.
  fPassword.value = "";
  fPrivateKey.value = "";
  fPassphrase.value = "";
  drawerOpen.value = true;
}

function closeDrawer() {
  drawerOpen.value = false;
  editingId.value = null;
}

const canSave = computed(() => fHost.value.trim() !== "" && fUser.value.trim() !== "");

async function save() {
  if (!canSave.value) return;
  errorMsg.value = "";
  const base = {
    alias: fAlias.value.trim(),
    host: fHost.value.trim(),
    port: fPort.value.trim() || "22",
    user: fUser.value.trim(),
    group: fGroup.value.trim(),
    auth_kind: fAuthKind.value,
  };
  const cred = {
    password: fAuthKind.value === "password" ? fPassword.value : undefined,
    private_key: fAuthKind.value === "privateKey" ? fPrivateKey.value : undefined,
    passphrase: fAuthKind.value === "privateKey" ? fPassphrase.value : undefined,
  };
  const hasNewCred =
    (fAuthKind.value === "password" && fPassword.value !== "") ||
    (fAuthKind.value === "privateKey" && fPrivateKey.value !== "");
  try {
    if (editingId.value) {
      await updateSSHHost({ id: editingId.value, ...base }, hasNewCred ? cred : null);
    } else {
      await addSSHHost({ id: "", ...base }, cred);
    }
    closeDrawer();
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
  <div class="ssh-overlay" @click.self="$emit('close')">
    <div class="ssh-shell">
      <!-- Header -->
      <header class="ssh-header">
        <div class="brand">
          <Server :size="16" class="brand-icon" />
          <span class="brand-title">SSH HOSTS</span>
          <span class="brand-count">{{ hosts.length }}</span>
        </div>
        <div class="search">
          <Search :size="13" class="search-icon" />
          <input
            v-model="query"
            data-test="ssh-search"
            placeholder="Filter hosts…"
            spellcheck="false"
            autocomplete="off"
          />
        </div>
        <button class="new-host-btn" data-test="ssh-new-host" @click="openNew">
          <Plus :size="14" /> New Host
        </button>
        <button class="close-x" title="Close" @click="$emit('close')">
          <X :size="16" />
        </button>
      </header>

      <p v-if="errorMsg" class="ssh-error" data-test="ssh-hosts-error">{{ errorMsg }}</p>

      <!-- Body: grouped card grid -->
      <div class="ssh-body">
        <div v-if="hosts.length === 0" class="empty">
          <Server :size="40" class="empty-icon" />
          <p class="empty-title">No saved hosts</p>
          <p class="empty-sub">Add a host to connect and take it over from any device.</p>
          <button class="new-host-btn ghost" @click="openNew"><Plus :size="14" /> New Host</button>
        </div>

        <template v-else>
          <section v-for="[gname, ghosts] in groups" :key="gname || '__ungrouped'" class="group">
            <div class="group-head">
              <Folder :size="12" class="group-icon" />
              <span class="group-name">{{ gname || "Ungrouped" }}</span>
              <span class="group-count">{{ ghosts.length }}</span>
            </div>
            <div class="card-grid">
              <article
                v-for="h in ghosts"
                :key="h.id"
                class="host-card"
                :class="{ busy: connectingId === h.id }"
                @dblclick="connect(h.id)"
              >
                <div class="card-glyph">
                  <KeyRound v-if="h.auth_kind === 'privateKey'" :size="16" />
                  <Server v-else :size="16" />
                </div>
                <div class="card-main">
                  <div class="card-label">{{ label(h) }}</div>
                  <div class="card-sub">{{ subtitle(h) }}</div>
                </div>
                <div class="card-actions">
                  <button
                    class="act connect"
                    :data-test="`ssh-connect-${h.id}`"
                    :disabled="connectingId === h.id"
                    title="Connect"
                    @click.stop="connect(h.id)"
                  >
                    <Zap :size="13" />
                  </button>
                  <button class="act" title="Edit" @click.stop="openEdit(h)">
                    <Pencil :size="13" />
                  </button>
                  <button
                    class="act danger"
                    :data-test="`ssh-delete-${h.id}`"
                    title="Delete"
                    @click.stop="remove(h.id)"
                  >
                    <Trash2 :size="13" />
                  </button>
                </div>
              </article>
            </div>
          </section>
        </template>
      </div>

      <!-- Right-side form drawer -->
      <transition name="drawer">
        <aside v-if="drawerOpen" class="drawer">
          <div class="drawer-head">
            <span>{{ editingId ? "Edit Host" : "New Host" }}</span>
            <button class="close-x" @click="closeDrawer"><X :size="15" /></button>
          </div>
          <div class="drawer-body">
            <label class="field">
              <span class="fl">Address</span>
              <input data-test="ssh-add-host" v-model="fHost" placeholder="IP or hostname" spellcheck="false" autocomplete="off" />
            </label>
            <div class="field-row">
              <label class="field grow">
                <span class="fl">Label</span>
                <input data-test="ssh-add-alias" v-model="fAlias" placeholder="optional" autocomplete="off" />
              </label>
              <label class="field port">
                <span class="fl">Port</span>
                <input data-test="ssh-add-port" v-model="fPort" autocomplete="off" />
              </label>
            </div>
            <label class="field">
              <span class="fl">Group</span>
              <input data-test="ssh-add-group" v-model="fGroup" placeholder="optional" autocomplete="off" />
            </label>
            <label class="field">
              <span class="fl">Username</span>
              <input data-test="ssh-add-user" v-model="fUser" placeholder="user" autocomplete="off" />
            </label>

            <div class="seg">
              <button :class="{ on: fAuthKind === 'password' }" @click="fAuthKind = 'password'">Password</button>
              <button :class="{ on: fAuthKind === 'privateKey' }" @click="fAuthKind = 'privateKey'">Private key</button>
            </div>

            <label v-if="fAuthKind === 'password'" class="field">
              <span class="fl">Password<template v-if="editingId"> <em>(leave blank to keep)</em></template></span>
              <input data-test="ssh-add-password" type="password" v-model="fPassword" autocomplete="off" />
            </label>
            <template v-else>
              <label class="field">
                <span class="fl">Private key (PEM)<template v-if="editingId"> <em>(leave blank to keep)</em></template></span>
                <textarea data-test="ssh-add-private-key" v-model="fPrivateKey" rows="4" spellcheck="false"></textarea>
              </label>
              <label class="field">
                <span class="fl">Passphrase</span>
                <input data-test="ssh-add-passphrase" type="password" v-model="fPassphrase" autocomplete="off" />
              </label>
            </template>
          </div>
          <div class="drawer-foot">
            <button class="btn ghost" @click="closeDrawer">Cancel</button>
            <button class="btn primary" data-test="ssh-add-submit" :disabled="!canSave" @click="save">
              {{ editingId ? "Save" : "Add Host" }}
            </button>
          </div>
        </aside>
      </transition>
    </div>
  </div>
</template>

<style scoped>
.ssh-overlay {
  position: fixed; inset: 0; z-index: 120;
  background: rgba(1, 4, 9, 0.72);
  backdrop-filter: blur(3px);
  display: flex; align-items: center; justify-content: center;
}
.ssh-shell {
  position: relative;
  width: min(1040px, calc(100vw - 48px));
  height: min(720px, calc(100vh - 48px));
  background:
    radial-gradient(120% 80% at 100% 0%, rgba(88, 166, 255, 0.06), transparent 55%),
    var(--bg);
  border: 1px solid var(--border);
  border-radius: 12px;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.55);
  overflow: hidden;
  display: flex; flex-direction: column;
}

/* Header */
.ssh-header {
  display: flex; align-items: center; gap: 12px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border);
  background: var(--panel);
}
.brand { display: flex; align-items: center; gap: 8px; }
.brand-icon { color: var(--accent); }
.brand-title {
  font-size: 12px; font-weight: 700; letter-spacing: 0.14em; color: var(--fg);
}
.brand-count {
  font-size: 11px; color: var(--fg-dim);
  background: rgba(139, 148, 158, 0.14);
  padding: 1px 7px; border-radius: 999px;
}
.search {
  flex: 1; max-width: 380px;
  display: flex; align-items: center; gap: 7px;
  background: var(--bg); border: 1px solid var(--border); border-radius: 7px;
  padding: 0 10px;
}
.search-icon { color: var(--fg-dim); flex: none; }
.search input {
  flex: 1; background: transparent; border: none; outline: none;
  color: var(--fg); font-size: 13px; padding: 7px 0;
}
.new-host-btn {
  display: inline-flex; align-items: center; gap: 5px;
  background: var(--accent); color: #04101f; border: none;
  font-size: 12px; font-weight: 600; padding: 7px 12px; border-radius: 7px;
  cursor: pointer; transition: filter 120ms;
}
.new-host-btn:hover { filter: brightness(1.1); }
.new-host-btn.ghost {
  background: transparent; color: var(--accent);
  border: 1px solid var(--accent);
}
.close-x {
  display: inline-flex; align-items: center; justify-content: center;
  background: transparent; border: none; color: var(--fg-dim);
  cursor: pointer; padding: 4px; border-radius: 6px; transition: color 120ms, background 120ms;
}
.close-x:hover { color: var(--fg); background: rgba(139, 148, 158, 0.12); }

.ssh-error {
  margin: 0; padding: 8px 16px; font-size: 12px; color: var(--bad);
  background: rgba(248, 81, 73, 0.08); border-bottom: 1px solid rgba(248, 81, 73, 0.2);
}

/* Body */
.ssh-body { flex: 1; overflow-y: auto; padding: 18px 16px 24px; }
.group { margin-bottom: 22px; }
.group-head {
  display: flex; align-items: center; gap: 7px;
  margin: 0 2px 10px; color: var(--fg-dim);
}
.group-icon { color: var(--neutral); }
.group-name { font-size: 11px; font-weight: 600; letter-spacing: 0.1em; text-transform: uppercase; }
.group-count { font-size: 10px; color: var(--neutral); }

.card-grid {
  display: grid; gap: 10px;
  grid-template-columns: repeat(auto-fill, minmax(232px, 1fr));
}
.host-card {
  position: relative;
  display: flex; align-items: center; gap: 11px;
  padding: 12px 12px;
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 10px;
  cursor: default;
  transition: border-color 140ms, transform 140ms, box-shadow 140ms;
}
.host-card:hover {
  border-color: var(--accent);
  transform: translateY(-1px);
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.35);
}
.host-card.busy { opacity: 0.6; }
.card-glyph {
  flex: none;
  width: 34px; height: 34px; border-radius: 8px;
  display: flex; align-items: center; justify-content: center;
  color: var(--accent);
  background: rgba(88, 166, 255, 0.1);
  border: 1px solid rgba(88, 166, 255, 0.18);
}
.card-main { flex: 1; min-width: 0; }
.card-label {
  font-size: 13px; font-weight: 600; color: var(--fg);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.card-sub {
  font-size: 11px; color: var(--fg-dim);
  font-family: var(--font-mono-strict);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  margin-top: 2px;
}
.card-actions {
  flex: none; display: flex; gap: 2px;
  opacity: 0; transition: opacity 140ms;
}
.host-card:hover .card-actions { opacity: 1; }
.act {
  display: inline-flex; align-items: center; justify-content: center;
  width: 26px; height: 26px; border-radius: 6px;
  background: transparent; border: none; color: var(--fg-dim);
  cursor: pointer; transition: color 120ms, background 120ms;
}
.act:hover { color: var(--fg); background: rgba(139, 148, 158, 0.14); }
.act.connect:hover { color: var(--good); background: rgba(63, 185, 80, 0.14); }
.act.danger:hover { color: var(--bad); background: rgba(248, 81, 73, 0.14); }
.act:disabled { opacity: 0.4; cursor: default; }

/* Empty state */
.empty {
  height: 100%;
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  gap: 6px; color: var(--fg-dim);
}
.empty-icon { color: var(--neutral); opacity: 0.5; margin-bottom: 6px; }
.empty-title { margin: 0; font-size: 14px; color: var(--fg); font-weight: 600; }
.empty-sub { margin: 0 0 12px; font-size: 12px; }

/* Drawer */
.drawer {
  position: absolute; top: 0; right: 0; bottom: 0;
  width: 340px; max-width: 82%;
  background: var(--panel);
  border-left: 1px solid var(--border);
  box-shadow: -20px 0 48px rgba(0, 0, 0, 0.45);
  display: flex; flex-direction: column;
}
.drawer-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 16px; border-bottom: 1px solid var(--border);
  font-size: 12px; font-weight: 700; letter-spacing: 0.1em; text-transform: uppercase; color: var(--fg-dim);
}
.drawer-body { flex: 1; overflow-y: auto; padding: 16px; display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field-row { display: flex; gap: 10px; }
.field.grow { flex: 1; }
.field.port { width: 78px; flex: none; }
.fl { font-size: 11px; color: var(--fg-dim); }
.fl em { color: var(--neutral); font-style: normal; }
.field input, .field textarea {
  background: var(--bg); border: 1px solid var(--border); border-radius: 6px;
  padding: 7px 9px; color: var(--fg); font-size: 13px; outline: none;
  transition: border-color 120ms;
}
.field input:focus, .field textarea:focus { border-color: var(--accent); }
.field textarea { resize: vertical; font-family: var(--font-mono-strict); font-size: 12px; }
.seg {
  display: flex; background: var(--bg); border: 1px solid var(--border);
  border-radius: 7px; padding: 3px; gap: 3px;
}
.seg button {
  flex: 1; background: transparent; border: none; color: var(--fg-dim);
  font-size: 12px; padding: 6px 0; border-radius: 5px; cursor: pointer; transition: all 120ms;
}
.seg button.on { background: var(--accent); color: #04101f; font-weight: 600; }
.drawer-foot {
  display: flex; justify-content: flex-end; gap: 8px;
  padding: 12px 16px; border-top: 1px solid var(--border);
}
.btn {
  font-size: 12px; padding: 8px 14px; border-radius: 7px; cursor: pointer;
  border: 1px solid var(--border); background: transparent; color: var(--fg);
  transition: all 120ms;
}
.btn.ghost:hover { background: rgba(139, 148, 158, 0.1); }
.btn.primary {
  background: var(--accent); color: #04101f; border-color: var(--accent); font-weight: 600;
}
.btn.primary:hover:not(:disabled) { filter: brightness(1.1); }
.btn.primary:disabled { opacity: 0.4; cursor: default; }

.drawer-enter-active, .drawer-leave-active { transition: transform 180ms ease, opacity 180ms ease; }
.drawer-enter-from, .drawer-leave-to { transform: translateX(24px); opacity: 0; }
</style>
