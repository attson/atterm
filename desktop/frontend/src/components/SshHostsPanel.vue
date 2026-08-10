<script lang="ts" setup>
import { ref, computed, onMounted, onBeforeUnmount } from "vue";
import { Server, Plus, X, Pencil, Trash2, Search, Zap, KeyRound, Upload, FileUp } from "lucide-vue-next";
import SelectDropdown, { type SelectOption } from "./SelectDropdown.vue";
import SessionRowMenu, { type MenuItem } from "./SessionRowMenu.vue";
import { readPrivateKeyFile } from "../lib/sshKeyFile";
import { sshCommandFor } from "../lib/sshCommand";
import { allHostTags, hostHasAllTags, normalizeTags, parseTagInput } from "../lib/hostTags";
import { fallbackCopyText } from "../lib/terminalCopy";
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

// The key drawer invites the user to drag a file in. A near-miss drop would
// otherwise reach the webview, which navigates to the dropped file and wipes
// the app view. Swallow stray drags for as long as this modal is up; the drop
// zone's own handler has already run by the time these fire.
function swallowStrayDrag(e: Event) {
  e.preventDefault();
}
onMounted(() => {
  window.addEventListener("dragover", swallowStrayDrag);
  window.addEventListener("drop", swallowStrayDrag);
  void reload();
});
onBeforeUnmount(() => {
  window.removeEventListener("dragover", swallowStrayDrag);
  window.removeEventListener("drop", swallowStrayDrag);
});

// ---- Hosts ----
// Tag filter narrows first (AND across the selected tags), then the free-text
// query, which also searches tag names.
const selectedTags = ref<string[]>([]);
const availableTags = computed(() => allHostTags(hosts.value));

const filteredHosts = computed(() => {
  const q = query.value.trim().toLowerCase();
  return hosts.value.filter((h) => {
    if (!hostHasAllTags(h.tags, selectedTags.value)) return false;
    if (q === "") return true;
    const haystack = `${h.alias ?? ""} ${h.host} ${h.user} ${(h.tags ?? []).join(" ")}`;
    return haystack.toLowerCase().includes(q);
  });
});

function toggleTagFilter(tag: string) {
  const at = selectedTags.value.indexOf(tag);
  selectedTags.value = at >= 0
    ? selectedTags.value.filter((t) => t !== tag)
    : [...selectedTags.value, tag];
}
function clearTagFilters() {
  selectedTags.value = [];
}

const keyOptions = computed<SelectOption[]>(() =>
  keys.value.map((k) => ({
    value: k.id,
    label: k.key_type ? `${k.name} (${k.key_type})` : k.name,
  })),
);

// Tags already in use elsewhere, minus the ones this host already carries —
// offered as suggestions in the host form. Typing a brand-new tag also works.
const tagMenuOpen = ref(false);
const tagSuggestions = computed<string[]>(() => {
  const q = fTagInput.value.trim().toLowerCase();
  const owned = new Set(fTags.value.map((t) => t.toLowerCase()));
  return availableTags.value.filter(
    (t) => !owned.has(t.toLowerCase()) && (q === "" || t.toLowerCase().includes(q)),
  );
});
function addFormTags(raw: string[]) {
  fTags.value = normalizeTags([...fTags.value, ...raw]);
}
function commitTagInput() {
  addFormTags(parseTagInput(fTagInput.value));
  fTagInput.value = "";
}
function pickTag(t: string) {
  addFormTags([t]);
  fTagInput.value = "";
  tagMenuOpen.value = false;
}
function removeFormTag(t: string) {
  fTags.value = fTags.value.filter((x) => x !== t);
}
// Backspace on an empty input peels off the last chip, the usual tag-field feel.
function onTagBackspace() {
  if (fTagInput.value === "") fTags.value = fTags.value.slice(0, -1);
}
function onTagBlur() {
  // Delay so a mousedown on a suggestion registers before the menu closes.
  window.setTimeout(() => (tagMenuOpen.value = false), 120);
}

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

// ---- Host context menu ----
const hostMenu = ref<{ open: boolean; x: number; y: number; host: SSHHost | null }>({
  open: false,
  x: 0,
  y: 0,
  host: null,
});

const hostMenuItems: MenuItem[] = [
  { key: "connect", label: "Connect" },
  { key: "edit", label: "Edit Host Details" },
  { key: "duplicate", label: "Duplicate" },
  { key: "copy-ssh", label: "Copy SSH command" },
  { key: "remove", label: "Remove", separatorBefore: true },
];

function openHostMenu(e: MouseEvent, h: SSHHost) {
  hostMenu.value = { open: true, x: e.clientX, y: e.clientY, host: h };
}
function closeHostMenu() {
  hostMenu.value = { ...hostMenu.value, open: false, host: null };
}

async function onHostMenuSelect(key: string) {
  const h = hostMenu.value.host;
  if (!h) return;
  switch (key) {
    case "connect":
      await connect(h.id);
      break;
    case "edit":
      openEditHost(h);
      break;
    case "duplicate":
      await duplicateHost(h);
      break;
    case "copy-ssh":
      await copySshCommand(h);
      break;
    case "remove":
      await removeHost(h.id);
      break;
  }
}

// duplicateHost copies every non-secret field. The password lives in the
// keyring and cannot be read back, so a password host's copy starts without
// one — open the drawer in that case rather than leaving a host that silently
// fails to connect.
async function duplicateHost(h: SSHHost) {
  errorMsg.value = "";
  const copy: SSHHost = {
    ...h,
    id: "",
    alias: h.alias?.trim() ? `${h.alias.trim()} copy` : "",
  };
  try {
    const created = await addSSHHost(copy, {});
    await reload();
    if (h.auth_kind === "password") openEditHost(created ?? copy);
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}

async function copySshCommand(h: SSHHost) {
  const text = sshCommandFor({ user: h.user, host: h.host, port: h.port });
  const clipboard = typeof navigator === "undefined" ? undefined : navigator.clipboard;
  if (clipboard?.writeText) {
    await clipboard.writeText(text);
    return;
  }
  fallbackCopyText(text);
}

// ---- Host drawer ----
const hostDrawer = ref(false);
const hostEditId = ref<string | null>(null);
const fAlias = ref("");
const fHost = ref("");
const fPort = ref("22");
const fUser = ref("");
const fTags = ref<string[]>([]);
const fTagInput = ref("");
const fAuthKind = ref<"password" | "key">("password");
const fPassword = ref("");
const fKeyID = ref("");

function openNewHost() {
  hostEditId.value = "";
  fAlias.value = "";
  fHost.value = "";
  fPort.value = "22";
  fUser.value = "";
  fTags.value = [];
  fTagInput.value = "";
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
  fTags.value = normalizeTags(h.tags ?? []);
  fTagInput.value = "";
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
    tags: fTags.value,
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
// Set when an imported key's PEM is passphrase-protected, so the drawer can say
// so instead of letting the user hit a backend parse error on save.
const kNeedsPassphrase = ref(false);

function resetKeyForm() {
  kName.value = "";
  kPem.value = "";
  kPassphrase.value = "";
  kNeedsPassphrase.value = false;
}

function openNewKey() {
  keyEditId.value = "";
  resetKeyForm();
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
  resetKeyForm();
  kName.value = k.name;
  keyDrawer.value = true;
}
function closeKeyDrawer() {
  keyDrawer.value = false;
  keyEditId.value = null;
}
// ---- Key file import ----
// Dragging is pointer-only; on touch builds (iOS) the drop zone would be dead
// UI, so only the picker button is offered there. Evaluated per mount so tests
// (and a device switching input modes) get an honest answer.
const supportsFileDrag = ref(
  window.matchMedia?.("(hover: hover) and (pointer: fine)")?.matches ?? true,
);
const keyFileInput = ref<HTMLInputElement | null>(null);
const keyDragOver = ref(false);

function pickKeyFile() {
  keyFileInput.value?.click();
}

async function onKeyFilePicked(e: Event) {
  const input = e.target as HTMLInputElement;
  const file = input.files?.[0];
  await importKeyFile(file);
  // Clear so re-picking the same path fires change again.
  input.value = "";
}

async function onKeyFileDropped(e: DragEvent) {
  keyDragOver.value = false;
  await importKeyFile(e.dataTransfer?.files?.[0]);
}

// importKeyFile fills the drawer from a local file. The name is only filled in
// when the user has not typed one, so an explicit label always wins.
async function importKeyFile(file: File | undefined | null) {
  if (!file) return;
  errorMsg.value = "";
  try {
    const imported = await readPrivateKeyFile(file);
    kPem.value = imported.pem;
    kNeedsPassphrase.value = imported.encrypted;
    if (kName.value.trim() === "") kName.value = imported.suggestedName;
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
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
          <div v-if="availableTags.length" class="tag-bar" data-test="ssh-tag-filter-bar">
            <button
              v-for="t in availableTags" :key="t"
              class="tag-pill" :class="{ on: selectedTags.includes(t) }"
              :data-test="`ssh-tag-filter-${t}`" @click="toggleTagFilter(t)"
            >{{ t }}</button>
            <button
              v-if="selectedTags.length" class="tag-clear"
              data-test="ssh-tag-filter-clear" @click="clearTagFilters"
            ><X :size="11" /> Clear</button>
          </div>
          <div v-if="filteredHosts.length === 0" class="empty" data-test="ssh-hosts-no-match">
            <Server :size="40" class="empty-icon" />
            <p class="empty-title">No hosts match</p>
            <p class="empty-sub">Loosen the filter or clear the selected tags.</p>
          </div>
            <div v-else class="card-grid">
              <article
                v-for="h in filteredHosts" :key="h.id" class="card"
                :data-test="`ssh-host-card-${h.id}`"
                :class="{ busy: connectingId === h.id }" @dblclick="connect(h.id)"
                @contextmenu.prevent="openHostMenu($event, h)"
              >
                <div class="card-glyph">
                  <KeyRound v-if="h.auth_kind === 'key'" :size="16" />
                  <Server v-else :size="16" />
                </div>
                <div class="card-main">
                  <div class="card-label">{{ hostLabel(h) }}</div>
                  <div class="card-sub">{{ hostSubtitle(h) }}</div>
                  <div v-if="h.tags?.length" class="card-tags">
                    <span
                      v-for="t in h.tags" :key="t" class="card-tag"
                      :data-test="`ssh-host-tag-${h.id}-${t}`"
                    >{{ t }}</span>
                  </div>
                </div>
                <div class="card-actions">
                  <button class="act connect" :data-test="`ssh-connect-${h.id}`" :disabled="connectingId === h.id" title="Connect" @click.stop="connect(h.id)"><Zap :size="13" /></button>
                  <button class="act" title="Edit" @click.stop="openEditHost(h)"><Pencil :size="13" /></button>
                  <button class="act danger" :data-test="`ssh-delete-${h.id}`" title="Delete" @click.stop="removeHost(h.id)"><Trash2 :size="13" /></button>
                </div>
              </article>
            </div>
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

      <SessionRowMenu
        :open="hostMenu.open" :x="hostMenu.x" :y="hostMenu.y" :items="hostMenuItems"
        @close="closeHostMenu" @select="onHostMenuSelect"
      />

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
            <label class="field tag-field">
              <span class="fl">Tags</span>
              <div class="tag-editor">
                <span
                  v-for="t in fTags" :key="t" class="chip"
                  :data-test="`ssh-add-tag-chip-${t}`"
                >
                  {{ t }}
                  <button
                    type="button" class="chip-x" :data-test="`ssh-add-tag-remove-${t}`"
                    :title="`Remove ${t}`" @click.prevent="removeFormTag(t)"
                  ><X :size="10" /></button>
                </span>
                <input
                  class="tag-input" data-test="ssh-add-tag-input" v-model="fTagInput"
                  :placeholder="fTags.length ? '' : 'comma separated'"
                  autocomplete="off" spellcheck="false"
                  @focus="tagMenuOpen = true" @input="tagMenuOpen = true" @blur="onTagBlur"
                  @keydown.enter.prevent="commitTagInput"
                  @keydown.,.prevent="commitTagInput"
                  @keydown.backspace="onTagBackspace"
                />
                <ul v-if="tagMenuOpen && tagSuggestions.length" class="combo-menu" data-test="ssh-tag-menu">
                  <li
                    v-for="t in tagSuggestions" :key="t"
                    class="combo-opt" :data-test="`ssh-tag-opt-${t}`"
                    @mousedown.prevent="pickTag(t)"
                  >{{ t }}</li>
                </ul>
              </div>
            </label>
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
                <div data-test="ssh-add-keyid">
                  <SelectDropdown v-model="fKeyID" :options="keyOptions" aria-label="SSH key" />
                </div>
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
            <label class="field">
              <span class="fl">Passphrase</span>
              <input data-test="ssh-key-passphrase" type="password" v-model="kPassphrase" autocomplete="off" />
            </label>
            <p v-if="kNeedsPassphrase" class="enc-hint" data-test="ssh-key-encrypted-hint">
              This key is encrypted — enter its passphrase above.
            </p>

            <div class="import-block">
              <input
                ref="keyFileInput" class="file-input" data-test="ssh-key-file-input"
                type="file" @change="onKeyFilePicked"
              />
              <div
                v-if="supportsFileDrag" class="dropzone" :class="{ over: keyDragOver }"
                data-test="ssh-key-dropzone"
                @dragover.prevent="keyDragOver = true"
                @dragenter.prevent="keyDragOver = true"
                @dragleave="keyDragOver = false"
                @drop.prevent="onKeyFileDropped"
              >
                <FileUp :size="20" class="dropzone-icon" />
                <span>Drag and drop a private key file to import</span>
              </div>
              <button class="btn import" type="button" data-test="ssh-key-import-btn" @click="pickKeyFile">
                <Upload :size="13" /> Import from key file
              </button>
            </div>
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
.tag-bar { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin: 0 2px 14px; }
.tag-pill {
  background: var(--panel); border: 1px solid var(--border); color: var(--fg-dim);
  font-size: 11px; padding: 4px 10px; border-radius: 999px; cursor: pointer;
  transition: color 120ms, border-color 120ms, background 120ms;
}
.tag-pill:hover { color: var(--fg); border-color: var(--neutral); }
.tag-pill.on { color: #04101f; background: var(--accent); border-color: var(--accent); font-weight: 600; }
.tag-clear {
  display: inline-flex; align-items: center; gap: 3px;
  background: transparent; border: none; color: var(--fg-dim);
  font-size: 11px; padding: 4px 6px; cursor: pointer;
}
.tag-clear:hover { color: var(--fg); }
.card-tags { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 5px; }
.card-tag {
  font-size: 10px; color: var(--fg-dim); background: rgba(139, 148, 158, 0.16);
  padding: 1px 7px; border-radius: 999px; white-space: nowrap;
}
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
.field input, .field textarea { background: var(--bg); border: 1px solid var(--border); border-radius: 6px; padding: 7px 9px; color: var(--fg); font-size: 13px; outline: none; transition: border-color 120ms; }
.field input:focus, .field textarea:focus { border-color: var(--accent); }
.field textarea { resize: vertical; font-family: var(--font-mono-strict); font-size: 12px; }
.hint { font-size: 12px; color: var(--fg-dim); margin: 0; }
.tag-editor {
  position: relative; display: flex; flex-wrap: wrap; align-items: center; gap: 4px;
  background: var(--bg); border: 1px solid var(--border); border-radius: 6px;
  padding: 5px 6px; transition: border-color 120ms;
}
.tag-editor:focus-within { border-color: var(--accent); }
.tag-editor .chip {
  display: inline-flex; align-items: center; gap: 3px;
  font-size: 11px; color: var(--fg); background: rgba(88, 166, 255, 0.14);
  border: 1px solid rgba(88, 166, 255, 0.28); padding: 1px 4px 1px 8px; border-radius: 999px;
}
.chip-x {
  display: inline-flex; align-items: center; justify-content: center;
  background: transparent; border: none; color: var(--fg-dim); cursor: pointer;
  padding: 1px; border-radius: 999px; line-height: 1;
}
.chip-x:hover { color: #fff; background: rgba(248, 81, 73, 0.35); }
.tag-input {
  flex: 1; min-width: 90px;
  background: transparent; border: none; outline: none;
  color: var(--fg); font-size: 13px; padding: 2px 3px;
}
.combo { position: relative; display: flex; }
.combo input { flex: 1; padding-right: 26px; width: 100%; }
.combo-caret {
  position: absolute; right: 6px; top: 50%; transform: translateY(-50%);
  background: transparent; border: none; color: var(--fg-dim); cursor: pointer;
  font-size: 11px; padding: 2px 4px; line-height: 1;
}
.combo-caret:hover { color: var(--fg); }
.combo-menu {
  position: absolute; top: calc(100% + 4px); left: 0; right: 0; z-index: 10;
  margin: 0; padding: 4px 0; list-style: none; max-height: 180px; overflow-y: auto;
  background: var(--bg); border: 1px solid var(--border); border-radius: 6px;
  box-shadow: 0 6px 16px rgba(0, 0, 0, 0.35);
}
.combo-opt { padding: 7px 10px; font-size: 13px; color: var(--fg); cursor: pointer; }
.combo-opt:hover { background: rgba(255, 255, 255, 0.06); }
.empty-keys-hint { display: flex; flex-direction: column; align-items: flex-start; gap: 8px; }
.enc-hint { margin: -4px 0 0; font-size: 11px; color: var(--warn, #d29922); }
.import-block { margin-top: 4px; display: flex; flex-direction: column; gap: 8px; }
.file-input { position: absolute; width: 1px; height: 1px; opacity: 0; pointer-events: none; }
.dropzone {
  display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 6px;
  padding: 18px 12px; text-align: center;
  border: 1px dashed var(--border); border-radius: 8px;
  color: var(--fg-dim); font-size: 11px; line-height: 1.4;
  transition: border-color 120ms, background 120ms, color 120ms;
}
.dropzone-icon { color: var(--neutral); }
.dropzone.over { border-color: var(--accent); color: var(--fg); background: rgba(88, 166, 255, 0.08); }
.dropzone.over .dropzone-icon { color: var(--accent); }
.btn.import { display: inline-flex; align-items: center; justify-content: center; gap: 6px; width: 100%; }
.btn.import svg { display: block; }
.btn.import:hover { background: rgba(139, 148, 158, 0.1); }
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
