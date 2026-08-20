<script lang="ts" setup>
import { ref, computed, onMounted, onBeforeUnmount } from "vue";
import { Server, Plus, X, Pencil, Trash2, Search, Zap, KeyRound, Upload, FileUp, FileDown } from "lucide-vue-next";
import SelectDropdown, { type SelectOption } from "./SelectDropdown.vue";
import SessionRowMenu, { type MenuItem } from "./SessionRowMenu.vue";
import { readPrivateKeyFile } from "../lib/sshKeyFile";
import { sshCommandFor } from "../lib/sshCommand";
import { allHostTags, hostHasAllTags, normalizeTags, parseTagInput } from "../lib/hostTags";
import { fallbackCopyText } from "../lib/terminalCopy";
import { useI18n } from "../i18n/useI18n";
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
  previewSSHConfigImport,
  importSSHHosts,
  type SSHHost,
  type SSHKey,
  type SSHConfigImportPreview,
} from "../lib/api";

const emit = defineEmits<{
  (e: "connected", sessionId: string): void;
  (e: "close"): void;
}>();

const { t } = useI18n();

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
  const auth = h.auth_kind === "key" ? t("ssh.subtitleAuthKey") : t("ssh.subtitleAuthPassword");
  return `${h.user}@${h.host}${port} · ${auth}`;
}

// ---- Proxied hosts ----
// A host imported from ~/.ssh/config with ProxyJump or ProxyCommand is not
// directly connectable: NewSshSessionByID refuses to dial it (a ProxyJump
// host is usually only reachable through its bastion, and a ProxyCommand is
// an arbitrary command atterm never runs). The list must say so up front
// instead of letting the user click Connect and read it off an error.
type ProxyFields = Pick<SSHHost, "proxy_jump" | "proxy_command">;

function isProxied(h: ProxyFields): boolean {
  return !!(h.proxy_jump || h.proxy_command);
}
// Short pill text for the host row / preview row.
function proxyLabel(h: ProxyFields): string {
  return h.proxy_jump ? t("ssh.proxy.jumpBadge") : t("ssh.proxy.commandBadge");
}
// Full sentence for tooltips and the error line. Branches on which field is
// actually set — naming ProxyJump at a ProxyCommand-only host sends the user
// looking for a config line they do not have.
function proxyReason(h: ProxyFields): string {
  if (h.proxy_jump) {
    return t("ssh.proxy.jumpReason", { target: h.proxy_jump });
  }
  if (h.proxy_command) {
    return t("ssh.proxy.commandReason", { command: h.proxy_command });
  }
  return "";
}

async function connect(id: string) {
  if (connectingId.value) return;
  errorMsg.value = "";
  // Never dial a proxied host, whichever affordance got us here (button,
  // double-click, context menu). The backend refuses too; stopping here just
  // makes the reason arrive without a round trip.
  const target = hosts.value.find((h) => h.id === id);
  if (target && isProxied(target)) {
    errorMsg.value = proxyReason(target);
    return;
  }
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

// Computed, not a module-level const: the labels have to re-resolve when the
// user switches language while the panel is open.
const hostMenuItems = computed<MenuItem[]>(() => [
  { key: "connect", label: t("ssh.hosts.menuConnect") },
  { key: "edit", label: t("ssh.hosts.menuEdit") },
  { key: "duplicate", label: t("ssh.hosts.menuDuplicate") },
  { key: "copy-ssh", label: t("ssh.hosts.menuCopySsh") },
  { key: "remove", label: t("ssh.hosts.menuRemove"), separatorBefore: true },
]);

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
//
// The " copy" suffix is deliberately not translated: it becomes the stored
// alias, which syncs to every other device. Localizing it would mean the same
// host reads differently depending on which machine duplicated it, and the
// user can rename it in the drawer anyway.
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
// The record the drawer was opened on, kept whole. The form refs below only
// cover the fields the drawer edits; identity_file / proxy_jump /
// proxy_command have no control and would be dropped from the save payload
// if it were built from the refs alone — silently stripping the
// not-directly-connectable gate off an imported host at the exact moment the
// user has to open this drawer to give it a credential. Same shape as
// duplicateHost's {...h}.
const editingHost = ref<SSHHost | null>(null);
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
  editingHost.value = null;
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
  editingHost.value = { ...h };
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
  editingHost.value = null;
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
      // Spread the record we opened on first so fields the form does not own
      // survive the round trip; base then overrides everything it does own.
      await updateSSHHost(
        { ...(editingHost.value ?? {}), id: hostEditId.value, ...base },
        hasNewCred ? cred : null,
      );
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

// ---- ssh_config import ----
// Two-step preview/import (design doc §5.1): PreviewSSHConfigImport only
// reads ~/.ssh/config, ImportSSHHosts only writes what the user explicitly
// checked. Rows default to unchecked — the host list syncs to other devices,
// so undoing a bad bulk import means unchecking rows one at a time, not one
// big "select all" the user has to partially undo afterwards.
const configImportDrawer = ref(false);
const configImportLoading = ref(false);
// Two separate errors on purpose. A failed *preview* means there is nothing
// to show, so the body shows the message instead of a list. A failed
// *confirm* leaves the preview intact and belongs in the footer next to the
// button that failed — reusing one ref made a confirm error blank out the
// entry list the user was still working with.
const configPreviewError = ref("");
const configConfirmError = ref("");
const configPreview = ref<SSHConfigImportPreview | null>(null);
const configSelected = ref<Set<number>>(new Set());
const configImporting = ref(false);
const configImportResult = ref<number | null>(null);

// The Go side make()s both slices so they marshal to [] rather than null, but
// the drawer reads .length on every render — normalise here so one nil slice
// crossing the boundary can never take the whole panel down with a TypeError.
const previewEntries = computed<SSHHost[]>(() => configPreview.value?.entries ?? []);
const previewSkipped = computed(() => configPreview.value?.skipped ?? []);

// Aliases already saved locally. ImportSSHHosts matches incoming entries
// against stored hosts by exact Alias, so this mirrors it exactly (empty
// aliases never match — they always append). Computed in the frontend rather
// than returned by the preview because the backend knows nothing the panel
// doesn't: the saved host list is already loaded here, and "will this
// overwrite?" is a fact about the *local* store at the moment the user is
// looking at the list, not about ~/.ssh/config.
const savedAliases = computed(() => new Set(hosts.value.map((h) => h.alias).filter(Boolean)));
function willOverwrite(e: SSHHost): boolean {
  return !!e.alias && savedAliases.value.has(e.alias);
}

function hostFromConfigEntry(e: SSHHost): SSHHost {
  return { ...e };
}

async function openConfigImport() {
  configImportDrawer.value = true;
  configPreviewError.value = "";
  configConfirmError.value = "";
  configImportResult.value = null;
  configPreview.value = null;
  configSelected.value = new Set();
  configImportLoading.value = true;
  try {
    configPreview.value = await previewSSHConfigImport();
  } catch (e) {
    configPreviewError.value = e instanceof Error ? e.message : String(e);
  } finally {
    configImportLoading.value = false;
  }
}
function closeConfigImportDrawer() {
  configImportDrawer.value = false;
}
function toggleConfigEntry(index: number) {
  const next = new Set(configSelected.value);
  if (next.has(index)) next.delete(index);
  else next.add(index);
  configSelected.value = next;
}
const canImportConfigSelection = computed(() => configSelected.value.size > 0);

async function confirmConfigImport() {
  if (!configPreview.value || configSelected.value.size === 0) return;
  configImporting.value = true;
  configConfirmError.value = "";
  try {
    const chosen = previewEntries.value
      .filter((_, i) => configSelected.value.has(i))
      .map(hostFromConfigEntry);
    const count = await importSSHHosts(chosen);
    configImportResult.value = count;
    configImportDrawer.value = false;
    await reload();
  } catch (e) {
    configConfirmError.value = e instanceof Error ? e.message : String(e);
  } finally {
    configImporting.value = false;
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
          ><Server :size="14" /> {{ t("ssh.tabHosts") }} <span class="tab-count">{{ hosts.length }}</span></button>
          <button
            class="tab" :class="{ on: activeTab === 'keys' }"
            data-test="ssh-tab-keys" @click="activeTab = 'keys'"
          ><KeyRound :size="14" /> {{ t("ssh.tabKeys") }} <span class="tab-count">{{ keys.length }}</span></button>
        </div>
        <div v-if="activeTab === 'hosts'" class="search">
          <Search :size="13" class="search-icon" />
          <input v-model="query" data-test="ssh-search" :placeholder="t('ssh.filterPlaceholder')" spellcheck="false" autocomplete="off" />
        </div>
        <div v-else class="search-spacer" />
        <button
          v-if="activeTab === 'hosts'" class="new-btn ghost"
          data-test="ssh-config-import-open" @click="openConfigImport"
        >
          <FileDown :size="14" /> {{ t("ssh.configImport.open") }}
        </button>
        <button v-if="activeTab === 'hosts'" class="new-btn" data-test="ssh-new-host" @click="openNewHost">
          <Plus :size="14" /> {{ t("ssh.newHost") }}
        </button>
        <button v-else class="new-btn" data-test="ssh-key-new" @click="openNewKey">
          <Plus :size="14" /> {{ t("ssh.newKey") }}
        </button>
        <button class="close-x" :title="t('common.close')" @click="$emit('close')"><X :size="16" /></button>
      </header>

      <p v-if="errorMsg" class="ssh-error" data-test="ssh-hosts-error">{{ errorMsg }}</p>
      <p v-if="configImportResult !== null" class="ssh-success" data-test="ssh-config-import-result">
        {{ t("ssh.importedHosts", { count: configImportResult }) }}
      </p>

      <!-- HOSTS TAB -->
      <div v-show="activeTab === 'hosts'" class="ssh-body">
        <div v-if="hosts.length === 0" class="empty">
          <Server :size="40" class="empty-icon" />
          <p class="empty-title">{{ t("ssh.hosts.emptyTitle") }}</p>
          <p class="empty-sub">{{ t("ssh.hosts.emptySub") }}</p>
          <button class="new-btn ghost" @click="openNewHost"><Plus :size="14" /> {{ t("ssh.newHost") }}</button>
        </div>
        <template v-else>
          <div v-if="availableTags.length" class="tag-bar" data-test="ssh-tag-filter-bar">
            <button
              v-for="tag in availableTags" :key="tag"
              class="tag-pill" :class="{ on: selectedTags.includes(tag) }"
              :data-test="`ssh-tag-filter-${tag}`" @click="toggleTagFilter(tag)"
            >{{ tag }}</button>
            <button
              v-if="selectedTags.length" class="tag-clear"
              data-test="ssh-tag-filter-clear" @click="clearTagFilters"
            ><X :size="11" /> {{ t("ssh.hosts.clearTags") }}</button>
          </div>
          <div v-if="filteredHosts.length === 0" class="empty" data-test="ssh-hosts-no-match">
            <Server :size="40" class="empty-icon" />
            <p class="empty-title">{{ t("ssh.hosts.noMatchTitle") }}</p>
            <p class="empty-sub">{{ t("ssh.hosts.noMatchSub") }}</p>
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
                  <div v-if="isProxied(h)" class="card-proxy">
                    <span
                      class="proxy-badge" :data-test="`ssh-host-proxy-${h.id}`"
                      :title="proxyReason(h)"
                    >{{ proxyLabel(h) }}</span>
                  </div>
                  <div v-if="h.tags?.length" class="card-tags">
                    <span
                      v-for="tag in h.tags" :key="tag" class="card-tag"
                      :data-test="`ssh-host-tag-${h.id}-${tag}`"
                    >{{ tag }}</span>
                  </div>
                </div>
                <div class="card-actions">
                  <button
                    class="act connect" :data-test="`ssh-connect-${h.id}`"
                    :disabled="connectingId === h.id || isProxied(h)"
                    :title="isProxied(h) ? proxyReason(h) : t('common.connect')"
                    @click.stop="connect(h.id)"
                  ><Zap :size="13" /></button>
                  <button class="act" :title="t('ssh.edit')" @click.stop="openEditHost(h)"><Pencil :size="13" /></button>
                  <button class="act danger" :data-test="`ssh-delete-${h.id}`" :title="t('common.delete')" @click.stop="removeHost(h.id)"><Trash2 :size="13" /></button>
                </div>
              </article>
            </div>
        </template>
      </div>

      <!-- KEYS TAB -->
      <div v-show="activeTab === 'keys'" class="ssh-body">
        <div v-if="keys.length === 0" class="empty">
          <KeyRound :size="40" class="empty-icon" />
          <p class="empty-title">{{ t("ssh.keys.emptyTitle") }}</p>
          <p class="empty-sub">{{ t("ssh.keys.emptySub") }}</p>
          <button class="new-btn ghost" @click="openNewKey"><Plus :size="14" /> {{ t("ssh.newKey") }}</button>
        </div>
        <div v-else class="card-grid">
          <article v-for="k in keys" :key="k.id" class="card">
            <div class="card-glyph"><KeyRound :size="16" /></div>
            <div class="card-main">
              <div class="card-label">{{ k.name }}</div>
              <div class="card-sub">{{ k.key_type ? t("ssh.keys.typeLabel", { type: k.key_type }) : t("ssh.keys.genericSubtitle") }}</div>
            </div>
            <div class="card-actions">
              <button class="act" :title="t('ssh.edit')" @click.stop="openEditKey(k)"><Pencil :size="13" /></button>
              <button class="act danger" :data-test="`ssh-key-delete-${k.id}`" :title="t('common.delete')" @click.stop="removeKey(k.id)"><Trash2 :size="13" /></button>
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
          <div class="drawer-head"><span>{{ hostEditId ? t("ssh.hostDrawer.titleEdit") : t("ssh.hostDrawer.titleNew") }}</span><button class="close-x" @click="closeHostDrawer"><X :size="15" /></button></div>
          <div class="drawer-body">
            <label class="field"><span class="fl">{{ t("ssh.hostDrawer.address") }}</span><input data-test="ssh-add-host" v-model="fHost" :placeholder="t('ssh.hostDrawer.addressPlaceholder')" spellcheck="false" autocomplete="off" /></label>
            <div class="field-row">
              <label class="field grow"><span class="fl">{{ t("ssh.hostDrawer.label") }}</span><input data-test="ssh-add-alias" v-model="fAlias" :placeholder="t('ssh.hostDrawer.labelPlaceholder')" autocomplete="off" /></label>
              <label class="field port"><span class="fl">{{ t("ssh.hostDrawer.port") }}</span><input data-test="ssh-add-port" v-model="fPort" autocomplete="off" /></label>
            </div>
            <label class="field tag-field">
              <span class="fl">{{ t("ssh.hostDrawer.tags") }}</span>
              <div class="tag-editor">
                <span
                  v-for="tag in fTags" :key="tag" class="chip"
                  :data-test="`ssh-add-tag-chip-${tag}`"
                >
                  {{ tag }}
                  <button
                    type="button" class="chip-x" :data-test="`ssh-add-tag-remove-${tag}`"
                    :title="t('ssh.hostDrawer.removeTag', { tag })" @click.prevent="removeFormTag(tag)"
                  ><X :size="10" /></button>
                </span>
                <input
                  class="tag-input" data-test="ssh-add-tag-input" v-model="fTagInput"
                  :placeholder="fTags.length ? '' : t('ssh.hostDrawer.tagsPlaceholder')"
                  autocomplete="off" spellcheck="false"
                  @focus="tagMenuOpen = true" @input="tagMenuOpen = true" @blur="onTagBlur"
                  @keydown.enter.prevent="commitTagInput"
                  @keydown.,.prevent="commitTagInput"
                  @keydown.backspace="onTagBackspace"
                />
                <ul v-if="tagMenuOpen && tagSuggestions.length" class="combo-menu" data-test="ssh-tag-menu">
                  <li
                    v-for="tag in tagSuggestions" :key="tag"
                    class="combo-opt" :data-test="`ssh-tag-opt-${tag}`"
                    @mousedown.prevent="pickTag(tag)"
                  >{{ tag }}</li>
                </ul>
              </div>
            </label>
            <label class="field"><span class="fl">{{ t("ssh.hostDrawer.username") }}</span><input data-test="ssh-add-user" v-model="fUser" :placeholder="t('ssh.hostDrawer.usernamePlaceholder')" autocomplete="off" /></label>
            <div class="seg">
              <button :class="{ on: fAuthKind === 'password' }" data-test="ssh-auth-password" @click="fAuthKind = 'password'">{{ t("ssh.hostDrawer.authPassword") }}</button>
              <button :class="{ on: fAuthKind === 'key' }" data-test="ssh-auth-key" @click="fAuthKind = 'key'">{{ t("ssh.hostDrawer.authKey") }}</button>
            </div>
            <!-- Read-only: ssh_config's IdentityFile is recorded as a path,
                 never read. Showing it is the whole point of recording it —
                 it tells the user which key to go import. -->
            <p v-if="editingHost?.identity_file" class="identity-hint" data-test="ssh-host-identity-file">
              <span class="fl">{{ t("ssh.hostDrawer.identityFileLabel") }}</span>
              <code>{{ editingHost.identity_file }}</code>
              <span class="hint">{{ t("ssh.hostDrawer.identityFileHint") }}</span>
            </p>
            <p v-if="editingHost && isProxied(editingHost)" class="identity-hint proxy" data-test="ssh-host-drawer-proxy">
              {{ proxyReason(editingHost) }}
            </p>
            <label v-if="fAuthKind === 'password'" class="field">
              <span class="fl">{{ t("ssh.hostDrawer.password") }}<template v-if="hostEditId"> <em>{{ t("ssh.hostDrawer.keepBlank") }}</em></template></span>
              <input data-test="ssh-add-password" type="password" v-model="fPassword" autocomplete="off" />
            </label>
            <template v-else>
              <label v-if="keys.length" class="field">
                <span class="fl">{{ t("ssh.hostDrawer.keyPicker") }}</span>
                <div data-test="ssh-add-keyid">
                  <SelectDropdown v-model="fKeyID" :options="keyOptions" :aria-label="t('ssh.hostDrawer.keyPickerAria')" />
                </div>
              </label>
              <div v-else class="empty-keys-hint">
                <p class="hint">{{ t("ssh.hostDrawer.noKeys") }}</p>
                <button class="btn primary sm" data-test="ssh-host-add-key" @click="jumpToNewKey">
                  <Plus :size="13" /> {{ t("ssh.hostDrawer.addKey") }}
                </button>
              </div>
            </template>
          </div>
          <div class="drawer-foot">
            <button class="btn ghost" @click="closeHostDrawer">{{ t("common.cancel") }}</button>
            <button class="btn primary" data-test="ssh-add-submit" :disabled="!canSaveHost" @click="saveHost">{{ hostEditId ? t("common.save") : t("ssh.hostDrawer.submitNew") }}</button>
          </div>
        </aside>
      </transition>

      <!-- KEY DRAWER -->
      <transition name="drawer">
        <aside v-if="keyDrawer" class="drawer">
          <div class="drawer-head"><span>{{ keyEditId ? t("ssh.keyDrawer.titleEdit") : t("ssh.keyDrawer.titleNew") }}</span><button class="close-x" @click="closeKeyDrawer"><X :size="15" /></button></div>
          <div class="drawer-body">
            <label class="field"><span class="fl">{{ t("ssh.keyDrawer.name") }}</span><input data-test="ssh-key-name" v-model="kName" :placeholder="t('ssh.keyDrawer.namePlaceholder')" autocomplete="off" /></label>
            <label class="field">
              <span class="fl">{{ t("ssh.keyDrawer.pem") }}<template v-if="keyEditId"> <em>{{ t("ssh.keyDrawer.keepBlank") }}</em></template></span>
              <!-- The placeholder is the literal PEM header the user is
                   expected to paste, not prose — it stays untranslated. -->
              <textarea data-test="ssh-key-pem" v-model="kPem" rows="6" spellcheck="false" placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"></textarea>
            </label>
            <label class="field">
              <span class="fl">{{ t("ssh.keyDrawer.passphrase") }}</span>
              <input data-test="ssh-key-passphrase" type="password" v-model="kPassphrase" autocomplete="off" />
            </label>
            <p v-if="kNeedsPassphrase" class="enc-hint" data-test="ssh-key-encrypted-hint">
              {{ t("ssh.keyDrawer.encryptedHint") }}
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
                <span>{{ t("ssh.keyDrawer.dropzone") }}</span>
              </div>
              <button class="btn import" type="button" data-test="ssh-key-import-btn" @click="pickKeyFile">
                <Upload :size="13" /> {{ t("ssh.keyDrawer.importFromFile") }}
              </button>
            </div>
          </div>
          <div class="drawer-foot">
            <button class="btn ghost" @click="closeKeyDrawer">{{ t("common.cancel") }}</button>
            <button class="btn primary" data-test="ssh-key-submit" :disabled="!canSaveKey" @click="saveKey">{{ keyEditId ? t("common.save") : t("ssh.keyDrawer.submitNew") }}</button>
          </div>
        </aside>
      </transition>

      <!-- SSH CONFIG IMPORT DRAWER -->
      <transition name="drawer">
        <aside v-if="configImportDrawer" class="drawer wide" data-test="ssh-config-import-drawer">
          <div class="drawer-head">
            <span>{{ t("ssh.configImport.title") }}</span>
            <button class="close-x" @click="closeConfigImportDrawer"><X :size="15" /></button>
          </div>
          <div class="drawer-body">
            <div v-if="configImportLoading" class="empty" data-test="ssh-config-import-loading">
              <p class="empty-sub">{{ t("ssh.configImport.loading") }}</p>
            </div>
            <p v-else-if="configPreviewError" class="ssh-error inline" data-test="ssh-config-import-error">
              {{ configPreviewError }}
            </p>
            <template v-else-if="configPreview">
              <div
                v-if="previewEntries.length === 0 && previewSkipped.length === 0"
                class="empty" data-test="ssh-config-import-empty"
              >
                <FileDown :size="36" class="empty-icon" />
                <p class="empty-title">{{ t("ssh.configImport.emptyTitle") }}</p>
                <p class="empty-sub">{{ t("ssh.configImport.emptySub") }}</p>
              </div>
              <template v-else>
                <p v-if="previewEntries.length" class="config-section-title">
                  {{ t("ssh.configImport.importableTitle", { count: previewEntries.length }) }}
                </p>
                <ul v-if="previewEntries.length" class="config-entry-list">
                  <li
                    v-for="(e, i) in previewEntries" :key="`${e.alias}-${i}`"
                    class="config-entry"
                  >
                    <label class="config-entry-label">
                      <input
                        type="checkbox" :data-test="`ssh-config-entry-check-${i}`"
                        :checked="configSelected.has(i)" @change="toggleConfigEntry(i)"
                      />
                      <span class="config-entry-main">
                        <span class="config-entry-alias">{{ e.alias || `${e.user}@${e.host}` }}</span>
                        <span class="config-entry-sub">{{ e.user }}@{{ e.host }}{{ e.port && e.port !== '22' ? `:${e.port}` : '' }}</span>
                      </span>
                      <span class="config-entry-badges">
                        <span
                          v-if="willOverwrite(e)" class="overwrite-badge"
                          :data-test="`ssh-config-entry-overwrite-${i}`"
                          :title="t('ssh.configImport.overwriteTitle')"
                        >{{ t("ssh.configImport.overwriteBadge") }}</span>
                        <span
                          v-if="isProxied(e)" class="proxy-badge"
                          :data-test="`ssh-config-entry-proxy-${i}`"
                          :title="proxyReason(e)"
                        >{{ t("ssh.configImport.proxyBadge", { label: proxyLabel(e) }) }}</span>
                      </span>
                    </label>
                  </li>
                </ul>
                <template v-if="previewSkipped.length">
                  <p class="config-section-title">{{ t("ssh.configImport.skippedTitle", { count: previewSkipped.length }) }}</p>
                  <ul class="config-skipped" data-test="ssh-config-import-skipped">
                    <li v-for="(s, i) in previewSkipped" :key="`${s.alias}-${i}`" class="skip-row">
                      <span class="skip-alias">{{ s.alias }}</span>
                      <span class="skip-reason">{{ s.reason }}</span>
                    </li>
                  </ul>
                </template>
              </template>
              <!-- Backend-authored: PreviewSSHConfigImport returns this note
                   already worded, so it is rendered verbatim, not keyed. -->
              <p class="config-note">{{ configPreview.note }}</p>
            </template>
          </div>
          <div class="drawer-foot config-foot">
            <p v-if="configConfirmError" class="ssh-error inline" data-test="ssh-config-import-confirm-error">
              {{ configConfirmError }}
            </p>
            <div class="foot-actions">
              <button class="btn ghost" @click="closeConfigImportDrawer">{{ t("common.cancel") }}</button>
              <button
                class="btn primary" data-test="ssh-config-import-confirm"
                :disabled="!canImportConfigSelection || configImporting"
                @click="confirmConfigImport"
              >{{ configImporting ? t("ssh.configImport.importing") : t("ssh.configImport.confirm", { count: configSelected.size }) }}</button>
            </div>
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
.ssh-error.inline { border-bottom: none; border-radius: 6px; }
.ssh-success { margin: 0; padding: 8px 16px; font-size: 12px; color: var(--good, #3fb950); background: rgba(63, 185, 80, 0.08); border-bottom: 1px solid rgba(63, 185, 80, 0.2); }
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
.card-proxy { display: flex; margin-top: 5px; }
.card-proxy .proxy-badge { max-width: 100%; text-align: left; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
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
.drawer.wide { width: 480px; }
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
.identity-hint {
  margin: -4px 0 0; display: flex; flex-direction: column; gap: 3px;
  font-size: 11px; color: var(--fg-dim); line-height: 1.4;
}
.identity-hint code {
  font-family: var(--font-mono-strict); font-size: 11px; color: var(--fg);
  background: var(--bg); border: 1px solid var(--border); border-radius: 5px;
  padding: 4px 7px; word-break: break-all;
}
.identity-hint.proxy { color: var(--warn, #d29922); }
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
.config-section-title { margin: 4px 0 2px; font-size: 11px; font-weight: 700; letter-spacing: 0.06em; text-transform: uppercase; color: var(--fg-dim); }
.config-entry-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.config-entry { background: var(--bg); border: 1px solid var(--border); border-radius: 8px; }
.config-entry-label { display: flex; align-items: center; gap: 10px; padding: 9px 10px; cursor: pointer; }
.config-entry-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.config-entry-alias { font-size: 13px; font-weight: 600; color: var(--fg); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.config-entry-sub { font-size: 11px; color: var(--fg-dim); font-family: var(--font-mono-strict); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.config-entry-badges { flex: none; display: flex; flex-direction: column; align-items: flex-end; gap: 4px; }
.proxy-badge { flex: none; font-size: 10px; color: var(--warn, #d29922); background: rgba(210, 153, 34, 0.12); border: 1px solid rgba(210, 153, 34, 0.3); padding: 3px 8px; border-radius: 999px; max-width: 150px; line-height: 1.3; text-align: right; }
.overwrite-badge { flex: none; font-size: 10px; color: var(--fg-dim); background: rgba(139, 148, 158, 0.16); border: 1px solid var(--border); padding: 3px 8px; border-radius: 999px; line-height: 1.3; white-space: nowrap; }
.config-skipped { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
.skip-row { display: flex; align-items: baseline; gap: 8px; padding: 6px 10px; background: rgba(139, 148, 158, 0.08); border-radius: 6px; }
.skip-alias { font-size: 12px; font-weight: 600; color: var(--fg); flex: none; }
.skip-reason { font-size: 11px; color: var(--fg-dim); }
.config-note { margin: 6px 0 0; font-size: 11px; color: var(--fg-dim); line-height: 1.5; }
.config-foot { flex-direction: column; align-items: stretch; gap: 8px; }
.foot-actions { display: flex; justify-content: flex-end; gap: 8px; }
</style>
