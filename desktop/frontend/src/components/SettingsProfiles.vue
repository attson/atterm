<script lang="ts" setup>
// Settings -> Session profiles (roadmap item 22). One component per settings
// section, same convention as SettingsTemplates.vue / SettingsRelay.vue: a
// standalone tab, not embedded inside another pane like
// SettingsTerminalAppearance.vue is. That's why this file does not gate its
// own template on caps.wailsBindings the way Appearance does per-field —
// SettingsDialog.vue already hides the entire "profiles" tab (and skips
// mounting this component at all) when !caps.wailsBindings, matching
// SettingsRelay.vue/SettingsFeishu.vue's precedent. Profiles only ever
// launch a *local* shell (desktop/relay_host.go NewSession), so there is no
// web/Capacitor variant to support here (design doc §2 non-goals).
import { onBeforeUnmount, onMounted, ref } from "vue";
import {
  getProfiles,
  setProfiles,
  getDefaultProfileID,
  setDefaultProfileID,
  type SessionProfile,
} from "../lib/api";
import { useI18n } from "../i18n/useI18n";
import { usePlatform } from "../platform";

const { t } = useI18n();
const platform = usePlatform();

// profiles-changed is the same-shape counterpart to
// SettingsTerminalAppearance's "appearance-changed" and SettingsShortcuts'
// "bindings-changed": a second, always-fires path so App.vue's picker stays
// live even when the relay is unreachable and prefs:changed (which only
// fires on a successful Push) never comes. Forwarded through
// SettingsDialog.vue exactly like those two.
const emit = defineEmits<{
  (e: "profiles-changed"): void;
}>();

const profiles = ref<SessionProfile[]>([]);
const defaultProfileId = ref("");
const loading = ref(true);
const error = ref("");

interface EditingState {
  id: string;
  name: string;
  shell: string;
  cwd: string;
  startupCmd: string;
  envText: string;
  syncEnv: boolean;
  isNew: boolean;
}
const editing = ref<EditingState | null>(null);

// Shared by onMounted and the prefs:changed listener below, mirroring
// SettingsTerminalAppearance.vue's loadAppearance(): read-only, never calls
// a set* API. A remote pull that changed profiles/default must be reflected
// here without writing anything back, or an open panel would ping-pong the
// value between devices (prefs-sync-l1 design doc §7.2, same rule as every
// other synced pref — this item's own design doc has no §7.2).
async function loadProfiles() {
  error.value = "";
  try {
    const [list, def] = await Promise.all([getProfiles(), getDefaultProfileID()]);
    profiles.value = list ?? [];
    defaultProfileId.value = def ?? "";
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    loading.value = false;
  }
}

let prefsChangedOff: (() => void) | null = null;
onMounted(() => {
  void loadProfiles();
  prefsChangedOff = platform.events.on("prefs:changed", () => {
    void loadProfiles();
  });
});
onBeforeUnmount(() => {
  prefsChangedOff?.();
  prefsChangedOff = null;
});

// envToText/textToEnv round-trip the profile's Env map through a plain
// "KEY=VALUE per line" textarea — no separate key/value row UI, matching how
// little space a settings tab pane has (SettingsTemplates.vue keeps its
// per-item editor similarly flat).
function envToText(env?: Record<string, string>): string {
  if (!env) return "";
  return Object.entries(env)
    .map(([k, v]) => `${k}=${v}`)
    .join("\n");
}

function textToEnv(text: string): Record<string, string> | undefined {
  const env: Record<string, string> = {};
  for (const rawLine of text.split("\n")) {
    const line = rawLine.trim();
    if (!line) continue;
    const idx = line.indexOf("=");
    if (idx <= 0) continue; // silently skip malformed lines (no "="/empty key)
    const key = line.slice(0, idx).trim();
    const value = line.slice(idx + 1);
    if (key) env[key] = value;
  }
  return Object.keys(env).length > 0 ? env : undefined;
}

// Returns whether the write actually succeeded. Callers that chain a second
// write onto a successful persist (deleteProfile's default-clearing, below)
// must check this — otherwise a failed persist() still reverts local state
// via loadProfiles(), and if that revert happens to leave defaultProfileId
// pointing at the id the caller just tried to delete, an unconditional
// follow-up write would silently clear the default even though the delete
// itself failed and nothing should have changed.
async function persist(): Promise<boolean> {
  error.value = "";
  try {
    await setProfiles(profiles.value);
    emit("profiles-changed");
    return true;
  } catch (e: any) {
    const message = e?.message ?? String(e);
    await loadProfiles(); // revert local state to whatever actually persisted
    // loadProfiles() always clears `error` at its own start (it's the
    // read-only reload, shared with the prefs:changed listener, and has no
    // reason to know a write just failed) and only re-sets it if the reload
    // itself throws. Left alone, that means the message set above gets wiped
    // out from under the caller a beat later and the user sees no error at
    // all — worse than the "generic error" a naive reading of this code
    // suggests, since silence reads as "nothing happened" even more directly
    // than a vague message would. Restore it unless the reload surfaced its
    // own (more relevant, since it's a live failure) error instead.
    if (!error.value) error.value = message;
    return false;
  }
}

function startAdd() {
  editing.value = {
    id: crypto.randomUUID(),
    name: "",
    shell: "",
    cwd: "",
    startupCmd: "",
    envText: "",
    syncEnv: false,
    isNew: true,
  };
}

function startEdit(p: SessionProfile) {
  editing.value = {
    id: p.id,
    name: p.name,
    shell: p.shell ?? "",
    cwd: p.cwd ?? "",
    startupCmd: p.startup_cmd ?? "",
    envText: envToText(p.env),
    syncEnv: !!p.sync_env,
    isNew: false,
  };
}

function cancelEdit() {
  editing.value = null;
}

async function saveEdit() {
  if (!editing.value) return;
  const e = editing.value;
  if (!e.name.trim()) {
    // Whitespace-only name previously no-opped silently, leaving the editor
    // open with no feedback — a user clicking Save twice would conclude the
    // panel was broken. Surface it the same way a failed persist() does.
    error.value = t("settings.profiles.nameRequired");
    return;
  }
  const next: SessionProfile = { id: e.id, name: e.name.trim() };
  if (e.shell.trim()) next.shell = e.shell.trim();
  if (e.cwd.trim()) next.cwd = e.cwd.trim();
  if (e.startupCmd.trim()) next.startup_cmd = e.startupCmd.trim();
  const env = textToEnv(e.envText);
  if (env) next.env = env;
  if (e.syncEnv) next.sync_env = true;

  if (e.isNew) {
    profiles.value = [...profiles.value, next];
  } else {
    profiles.value = profiles.value.map((p) => (p.id === e.id ? next : p));
  }
  editing.value = null;
  await persist();
}

async function deleteProfile(id: string) {
  const wasDefault = defaultProfileId.value === id;
  profiles.value = profiles.value.filter((p) => p.id !== id);
  const deleted = await persist();
  // Dangling-default judgment call (task-4 brief): if the profile that was
  // just deleted was the default, clear the default too rather than leaving
  // DefaultProfileID pointing at nothing. The Go side already does the
  // equivalent for an *inbound* sync (resolveDefaultProfileID in
  // desktop/profiles.go) — this mirrors that same rule for a *local* delete,
  // so "the default always names a real profile, or is empty" holds
  // regardless of which machine the delete happened on.
  //
  // Gated on `deleted` (persist()'s actual result), not on re-reading
  // defaultProfileId.value after the fact: if setProfiles rejected, persist()
  // already called loadProfiles() to revert — and since the delete never
  // reached the server, that revert reloads defaultProfileId back to `id`,
  // which would make the naive `defaultProfileId.value === id` check here
  // look true again and fire a second, unrelated, *successful* write that
  // wipes the user's default even though the delete itself failed. A failed
  // delete must leave everything untouched, not silently succeed at a
  // different write instead.
  if (deleted && wasDefault) {
    await clearDefault();
  }
}

async function setDefault(id: string) {
  const previous = defaultProfileId.value;
  defaultProfileId.value = id;
  error.value = "";
  try {
    await setDefaultProfileID(id);
    emit("profiles-changed");
  } catch (e: any) {
    defaultProfileId.value = previous;
    error.value = e?.message ?? String(e);
  }
}

async function clearDefault() {
  await setDefault("");
}
</script>

<template>
  <div class="tab-pane">
    <p class="hint">{{ t("settings.profiles.intro") }}</p>
    <p class="hint">{{ t("settings.profiles.precedenceHint") }}</p>
    <p class="hint">{{ t("settings.profiles.envPrivacyIntro") }}</p>

    <template v-if="!loading">
      <ul class="list">
        <li v-for="p in profiles" :key="p.id" class="row" :data-testid="`profile-row-${p.id}`">
          <span class="name" :title="p.name">
            {{ p.name }}
            <span v-if="p.id === defaultProfileId" class="badge" data-testid="profile-default-badge">{{
              t("settings.profiles.defaultBadge")
            }}</span>
          </span>
          <code class="detail">{{ p.shell || t("settings.profiles.shellUnset") }}</code>
          <code class="detail">{{ p.cwd || t("settings.profiles.cwdUnset") }}</code>
          <div class="actions">
            <button
              v-if="p.id !== defaultProfileId"
              :data-testid="`profile-set-default-${p.id}`"
              @click="setDefault(p.id)"
            >
              {{ t("settings.profiles.setDefault") }}
            </button>
            <button v-else :data-testid="`profile-clear-default-${p.id}`" @click="clearDefault()">
              {{ t("settings.profiles.clearDefault") }}
            </button>
            <button :data-testid="`profile-edit-${p.id}`" @click="startEdit(p)">
              {{ t("settings.profiles.edit") }}
            </button>
            <button class="del" :data-testid="`profile-delete-${p.id}`" @click="deleteProfile(p.id)">
              {{ t("settings.profiles.delete") }}
            </button>
          </div>
        </li>
      </ul>
      <p v-if="profiles.length === 0" class="hint">{{ t("settings.profiles.empty") }}</p>
    </template>

    <div v-if="editing" class="edit-row" data-testid="profile-editor">
      <div
        class="settings-field-grid settings-field-grid--two"
        data-testid="profile-primary-grid"
      >
        <input
          v-model="editing.name"
          class="edit-input"
          :placeholder="t('settings.profiles.name')"
          data-testid="profile-edit-name"
        />
        <input
          v-model="editing.shell"
          class="edit-input"
          :placeholder="t('settings.profiles.shellPlaceholder')"
          data-testid="profile-edit-shell"
        />
      </div>
      <input
        v-model="editing.cwd"
        class="edit-input"
        :placeholder="t('settings.profiles.cwdPlaceholder')"
        data-testid="profile-edit-cwd"
      />
      <input
        v-model="editing.startupCmd"
        class="edit-input"
        :placeholder="t('settings.profiles.startupCmdPlaceholder')"
        data-testid="profile-edit-startup-cmd"
      />
      <p class="hint">{{ t("settings.profiles.startupCmdHint") }}</p>
      <textarea
        v-model="editing.envText"
        class="edit-input edit-textarea"
        rows="3"
        :placeholder="t('settings.profiles.envPlaceholder')"
        data-testid="profile-edit-env"
      />
      <p class="hint">{{ t("settings.profiles.envHint") }}</p>
      <label class="checkbox">
        <input type="checkbox" v-model="editing.syncEnv" data-testid="profile-edit-sync-env" />
        {{ t("settings.profiles.syncEnv") }}
      </label>
      <p class="hint">
        {{ editing.syncEnv ? t("settings.profiles.syncEnvOnHint") : t("settings.profiles.syncEnvOffHint") }}
      </p>
      <div class="edit-actions">
        <button @click="cancelEdit">{{ t("common.cancel") }}</button>
        <button class="primary" data-testid="profile-edit-save" @click="saveEdit">
          {{ t("settings.profiles.save") }}
        </button>
      </div>
    </div>

    <div class="footer">
      <button data-testid="profile-add" @click="startAdd">{{ t("settings.profiles.add") }}</button>
      <span v-if="error" class="error">{{ error }}</span>
    </div>
  </div>
</template>

<style scoped>
.tab-pane {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.hint {
  font-size: 12px;
  color: var(--fg-dim);
  margin: 0;
  line-height: 1.5;
}
.list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.row {
  display: grid;
  grid-template-columns: 9rem 1fr 1fr auto;
  gap: 8px;
  align-items: center;
  padding: 8px 10px;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 6px;
}
.name {
  font-weight: 600;
  font-size: 0.85rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: flex;
  align-items: center;
  gap: 6px;
}
.badge {
  font-size: 0.65rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--accent);
  border: 1px solid var(--accent);
  border-radius: 3px;
  padding: 1px 4px;
}
.detail {
  font-family: var(--font-mono);
  font-size: 0.78rem;
  color: var(--fg-dim);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.actions {
  display: flex;
  gap: 4px;
}
.actions button {
  height: 24px;
  padding: 0 8px;
  font-size: 0.74rem;
}
.actions .del {
  color: var(--bad);
  border-color: rgba(248, 81, 73, 0.4);
}
.edit-row {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px;
  background: var(--panel);
  border: 1px solid var(--accent);
  border-radius: 6px;
}
.edit-input {
  padding: 6px 8px;
  background: var(--bg);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 4px;
  font-size: 0.85rem;
  font-family: var(--font-mono);
  width: 100%;
  box-sizing: border-box;
  height: 28px;
}
.edit-textarea {
  height: auto;
  resize: vertical;
  min-height: 60px;
  line-height: 1.4;
}
.checkbox {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--fg);
}
.edit-actions {
  display: flex;
  gap: 6px;
  justify-content: flex-end;
}
.edit-actions button {
  height: 28px;
  padding: 0 14px;
  font-size: 0.78rem;
}
.edit-actions .primary {
  background: var(--accent);
  color: #0d1117;
  border-color: var(--accent);
  font-weight: 600;
}
.footer {
  display: flex;
  gap: 8px;
  align-items: center;
}
.error {
  color: var(--bad);
  font-size: 0.75rem;
}

@media (max-width: 640px) {
  .row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .name {
    grid-column: 1 / -1;
  }
  .actions {
    grid-column: 1 / -1;
    flex-wrap: wrap;
    justify-content: flex-end;
  }
}
</style>
