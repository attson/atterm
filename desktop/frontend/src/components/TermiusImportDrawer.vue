<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { FolderOpen, Search, KeyRound, LockKeyhole, X } from 'lucide-vue-next';
import SelectDropdown from './SelectDropdown.vue';
import type { MessageKey } from '../i18n';
import { useI18n } from '../i18n/useI18n';
import {
  cancelTermiusImport, discoverTermiusProfiles, importTermius,
  previewTermiusImport, selectTermiusDirectory,
  type TermiusImportPreview, type TermiusImportReport,
} from '../lib/api';

const emit = defineEmits<{ close: []; imported: [report: TermiusImportReport] }>();
const { t } = useI18n();
const path = ref('');
const localKey = ref('');
const profiles = ref<string[]>([]);
const preview = ref<TermiusImportPreview | null>(null);
const selectedHosts = ref<string[]>([]);
const selectedKeys = ref<string[]>([]);
const busy = ref(false);
const importing = ref(false);
const error = ref('');
const query = ref('');
const section = ref<'hosts' | 'keys'>('hosts');
type HostStatus = 'all' | 'available' | 'blocked' | 'existing';
const hostStatus = ref<HostStatus>('all');
function statusOf(host: TermiusImportPreview['hosts'][number]): Exclude<HostStatus, 'all'> {
  return host.existing ? 'existing' : host.blocked ? 'blocked' : 'available';
}
const statusFilters = computed(() => {
  const hosts = preview.value?.hosts ?? [];
  return ([['all', 'ssh.termius.filterAll'], ['available', 'ssh.termius.filterAvailable'], ['blocked', 'ssh.termius.filterBlocked'], ['existing', 'ssh.termius.filterExisting']] as const).map(([value, key]) => ({
    value, label: t(key), count: hosts.filter(h => value === 'all' || statusOf(h) === value).length,
  }));
});
const filteredHosts = computed(() => preview.value?.hosts.filter(h => (hostStatus.value === 'all' || statusOf(h) === hostStatus.value) && matches([h.name, h.host, h.user, ...h.tags])) ?? []);
const filteredKeys = computed(() => preview.value?.keys.filter(k => matches([k.name])) ?? []);
function matches(values: string[]) {
  const terms = query.value.toLocaleLowerCase().trim().split(/\s+/).filter(Boolean);
  const text = values.join(' ').toLocaleLowerCase();
  return terms.every(term => text.includes(term));
}
const selectableIDs = computed(() => section.value === 'hosts'
  ? filteredHosts.value.filter(h => !h.blocked && !h.existing).map(h => h.id)
  : filteredKeys.value.filter(k => k.valid).map(k => k.id));
const visibleSelected = computed(() => selectableIDs.value.filter(id => (section.value === 'hosts' ? selectedHosts.value : selectedKeys.value).includes(id)).length);
const allVisibleSelected = computed(() => selectableIDs.value.length > 0 && visibleSelected.value === selectableIDs.value.length);
const effectiveHosts = computed(() => {
  const included = new Set<string>();
  const hosts = new Map(preview.value?.hosts.map(h => [h.id, h]) ?? []);
  function add(id: string) {
    if (included.has(id)) return;
    const host = hosts.get(id);
    if (!host || host.existing || host.blocked) return;
    included.add(id);
    host.jump_ids?.forEach(add);
  }
  selectedHosts.value.forEach(add);
  return included;
});
const linkedKeys = computed(() => new Set(preview.value?.hosts.filter(h => effectiveHosts.value.has(h.id) && h.key_id).map(h => h.key_id) ?? []));
const hostNames = computed(() => new Map(preview.value?.hosts.map(h => [h.id, h.name]) ?? []));
const keyCount = computed(() => new Set([...selectedKeys.value, ...linkedKeys.value]).size);
const existingCount = computed(() => preview.value?.hosts.filter(h => h.existing).length ?? 0);
const blockedCount = computed(() => preview.value?.hosts.filter(h => h.blocked && !h.existing).length ?? 0);
function toggleVisible() {
  const target = section.value === 'hosts' ? selectedHosts : selectedKeys;
  target.value = allVisibleSelected.value
    ? target.value.filter(id => !selectableIDs.value.includes(id))
    : [...new Set([...target.value, ...selectableIDs.value])];
}
function changeSection(value: 'hosts' | 'keys') { section.value = value; query.value = ''; }
let alive = true;
let request = 0;
const options = computed(() => [
  { value: '', label: t('ssh.termius.chooseProfile') },
  ...profiles.value.map(value => ({ value, label: value })),
]);
const canImport = computed(() => !busy.value && selectedHosts.value.length + selectedKeys.value.length > 0);
const keyNames = computed(() => new Map(preview.value?.keys.map(k => [k.id, k.name]) ?? []));
const warningKeys: Record<string, MessageKey> = {
  invalid_address: 'ssh.termius.warnings.invalid_address',
  missing_key: 'ssh.termius.warnings.missing_key',
  missing_username: 'ssh.termius.warnings.missing_username',
  missing_credentials: 'ssh.termius.warnings.missing_credentials',
  unsupported_proxy: 'ssh.termius.warnings.unsupported_proxy',
  startup_skipped: 'ssh.termius.warnings.startup_skipped',
  invalid_group: 'ssh.termius.warnings.invalid_group',
  restore_groups: 'ssh.termius.warnings.restore_groups',
  restore_forward_names: 'ssh.termius.warnings.restore_forward_names',
  inherited_config: 'ssh.termius.warnings.inherited_config',
  forward_skipped: 'ssh.termius.warnings.forward_skipped',
  invalid_key: 'ssh.termius.warnings.invalid_key',
  invalid_jump: 'ssh.termius.warnings.invalid_jump',
};
function warningMessage(code: string): string {
  return warningKeys[code] ? t(warningKeys[code]) : code;
}
function message(e: unknown): string {
  const value = e instanceof Error ? e.message : String(e);
  switch (value) {
    case 'termius_preview_expired': return t('ssh.termius.expired');
    case 'termius_running': return t('ssh.termius.running');
    case 'termius_process_check_failed': return t('ssh.termius.processCheckFailed');
    case 'termius_local_key_unavailable': return t('ssh.termius.keyUnavailable');
    case 'termius_keychain_failed': return t('ssh.termius.keychainFailed');
    case 'termius_invalid_local_key': return t('ssh.termius.invalidLocalKey');
    case 'termius_dependency_conflict': return t('ssh.termius.dependencyConflict');
    case 'termius_invalid_key': return t('ssh.termius.invalidKey');
    case 'termius_rollback_failed': return t('ssh.termius.rollbackFailed');
    default: return value;
  }
}
function discard() {
  const token = preview.value?.token;
  preview.value = null;
  query.value = '';
  section.value = 'hosts';
  hostStatus.value = 'all';
  selectedHosts.value = [];
  selectedKeys.value = [];
  if (token) void cancelTermiusImport(token).catch(() => {});
}
function close() {
  if (importing.value) return;
  request++;
  discard();
  localKey.value = '';
  emit('close');
}
async function browse() {
  error.value = '';
  try {
    const chosen = await selectTermiusDirectory();
    if (alive && chosen) path.value = chosen;
  } catch (e) { if (alive) error.value = message(e); }
}
async function scan() {
  if (busy.value || !path.value.trim()) return;
  discard();
  const current = ++request;
  busy.value = true;
  error.value = '';
  try {
    const result = await previewTermiusImport(path.value.trim(), localKey.value);
    if (!alive || current !== request) {
      void cancelTermiusImport(result.token).catch(() => {});
      return;
    }
    preview.value = result;
  } catch (e) { if (alive && current === request) error.value = message(e); }
  finally {
    localKey.value = '';
    if (alive && current === request) busy.value = false;
  }
}
async function confirm() {
  if (!preview.value || !canImport.value) return;
  busy.value = true;
  importing.value = true;
  error.value = '';
  try {
    const report = await importTermius(preview.value.token, selectedHosts.value, selectedKeys.value);
    preview.value = null;
    emit('imported', report);
  } catch (e) { error.value = message(e); }
  finally { busy.value = false; importing.value = false; }
}
onMounted(async () => {
  try {
    const found = await discoverTermiusProfiles();
    if (!alive) return;
    profiles.value = found;
    if (found.length === 1 && !path.value) path.value = found[0];
  } catch (e) { if (alive) error.value = message(e); }
});
onBeforeUnmount(() => { alive = false; request++; discard(); localKey.value = ''; });
</script>

<template>
  <aside class="termius-drawer" role="dialog" aria-modal="true" :aria-label="t('ssh.termius.title')" data-test="termius-drawer" @keydown.esc.stop="close">
    <header>
      <span>{{ t('ssh.termius.title') }}</span>
      <button class="icon-button" :aria-label="t('common.close')" :disabled="importing" @click="close"><X :size="16" /></button>
    </header>
    <div v-if="preview" class="preview-tools">
      <div class="tabs" role="tablist" :aria-label="t('ssh.termius.items')">
        <button id="termius-hosts-tab" class="tab" role="tab" :aria-selected="section === 'hosts'" aria-controls="termius-items" data-test="termius-hosts-tab" @click="changeSection('hosts')">{{ t('ssh.termius.hosts', { count: preview.hosts.length }) }}</button>
        <button id="termius-keys-tab" class="tab" role="tab" :aria-selected="section === 'keys'" aria-controls="termius-items" data-test="termius-keys-tab" @click="changeSection('keys')">{{ t('ssh.termius.keys', { count: preview.keys.length }) }}</button>
      </div>
      <div class="search-row"><Search :size="15" aria-hidden="true" /><input v-model="query" type="search" :placeholder="t('ssh.termius.search')" :aria-label="t('ssh.termius.search')" data-test="termius-search" /></div>
      <div v-if="section === 'hosts'" class="status-filters" role="group" :aria-label="t('ssh.termius.filterStatus')">
        <button v-for="filter in statusFilters" :key="filter.value" class="status-filter" :aria-pressed="hostStatus === filter.value" :data-test="`termius-filter-${filter.value}`" @click="hostStatus = filter.value">{{ filter.label }} <span>{{ filter.count }}</span></button>
      </div>
      <div class="selection-bar">
        <label class="bulk"><input type="checkbox" :checked="allVisibleSelected" :indeterminate="visibleSelected > 0 && !allVisibleSelected" :disabled="busy || !selectableIDs.length" data-test="termius-select-visible" @change="toggleVisible" />{{ t('ssh.termius.selectVisible', { count: selectableIDs.length }) }}</label>
        <span v-if="existingCount || blockedCount" class="hint">{{ t('ssh.termius.unavailable', { existing: existingCount, blocked: blockedCount }) }}</span>
      </div>
      <p class="hint">{{ section === 'keys' ? t('ssh.termius.keysHint') : t('ssh.termius.selectionHint') }}</p>
    </div>
    <div class="body" :class="{ 'preview-body': preview }">
      <p v-if="!preview" class="hint">{{ t('ssh.termius.description') }}</p>
      <p v-if="!preview" class="hint">{{ t('ssh.termius.closeSource') }}</p>
      <template v-if="!preview">
        <label v-if="profiles.length > 1" class="field">
          <span>{{ t('ssh.termius.profile') }}</span>
          <SelectDropdown v-model="path" :options="options" :aria-label="t('ssh.termius.profile')" :disabled="busy" />
        </label>
        <label class="field">
          <span>{{ t('ssh.termius.directory') }}</span>
          <div class="path-row">
            <input v-model="path" :disabled="busy" :placeholder="t('ssh.termius.directoryPlaceholder')" data-test="termius-path" spellcheck="false" autocomplete="off" />
            <button class="button" :disabled="busy" :aria-label="t('ssh.termius.browse')" @click="browse"><FolderOpen :size="15" /></button>
          </div>
        </label>
        <label class="field">
          <span>{{ t('ssh.termius.localKey') }}</span>
          <input v-model="localKey" type="password" :disabled="busy" :placeholder="t('ssh.termius.keyPlaceholder')" data-test="termius-local-key" autocomplete="off" spellcheck="false" />
          <span class="hint">{{ t('ssh.termius.keyHint') }}</span>
        </label>
        <button class="button primary" :disabled="busy || !path.trim()" data-test="termius-preview" @click="scan">{{ busy ? t('ssh.termius.scanning') : t('ssh.termius.preview') }}</button>
      </template>
      <template v-else>
        <div id="termius-items" class="items" role="tabpanel" :aria-labelledby="section === 'hosts' ? 'termius-hosts-tab' : 'termius-keys-tab'">
          <template v-if="section === 'hosts'">
            <p v-if="!filteredHosts.length" class="empty hint">{{ query.trim() || hostStatus !== 'all' ? t('ssh.termius.noMatches') : t('ssh.termius.noHosts') }}</p>
            <label v-for="host in filteredHosts" :key="host.id" class="entry" :class="{ blocked: host.blocked || host.existing, selected: effectiveHosts.has(host.id) }">
              <input v-model="selectedHosts" type="checkbox" :value="host.id" :disabled="busy || host.blocked || host.existing" :data-test="`termius-host-${host.id}`" />
              <span class="entry-body">
                <span class="entry-title"><strong>{{ host.name || host.host }}</strong><span v-if="effectiveHosts.has(host.id) && !selectedHosts.includes(host.id)" class="badge">{{ t('ssh.termius.autoHop') }}</span><span v-if="host.existing" class="badge">{{ t('ssh.termius.existing') }}</span><span v-else-if="host.blocked" class="badge warning">{{ t('ssh.termius.blocked') }}</span></span>
                <span class="address">{{ host.user }}@{{ host.host }}:{{ host.port }}</span>
                <span v-if="host.tags.length || host.key_id || host.has_password || host.forward_count" class="metadata">
                  <span v-for="tag in host.tags" :key="tag" class="badge">{{ tag }}</span>
                  <span v-if="host.key_id" class="credential"><KeyRound :size="12" aria-hidden="true" />{{ t('ssh.termius.linkedKey', { name: keyNames.get(host.key_id) || host.key_id }) }}</span>
                  <span v-else-if="host.has_password" class="credential"><LockKeyhole :size="12" aria-hidden="true" />{{ t('ssh.termius.passwordFound') }}</span>
                  <span v-if="host.forward_count" class="hint">{{ t('ssh.termius.forwards', { count: host.forward_count }) }}</span>
                </span>
                <span v-if="host.proxy_label" class="hint">{{ t('ssh.termius.proxyRoute', { route: host.proxy_label }) }}</span>
                <span v-if="host.jump_ids?.length" class="hint">{{ t('ssh.termius.jumpRoute', { route: host.jump_ids.map(id => hostNames.get(id) || id).join(' → ') }) }}</span>
                <span v-if="host.group_path?.length" class="warning">{{ host.group_path.join(" / ") }}</span>
                  <span v-for="warning in host.warnings" :key="warning" class="warning">{{ warningMessage(warning) }}</span>
              </span>
            </label>
          </template>
          <template v-else>
            <p v-if="!filteredKeys.length" class="empty hint">{{ t('ssh.termius.noMatches') }}</p>
            <label v-for="key in filteredKeys" :key="key.id" class="entry" :class="{ blocked: !key.valid, selected: selectedKeys.includes(key.id) || linkedKeys.has(key.id) }">
              <input v-model="selectedKeys" type="checkbox" :value="key.id" :disabled="busy || !key.valid" :data-test="`termius-key-${key.id}`" />
              <span class="entry-body"><span class="entry-title"><KeyRound :size="14" aria-hidden="true" /><strong>{{ key.name || 'Termius' }}</strong><span v-if="linkedKeys.has(key.id)" class="badge">{{ t('ssh.termius.autoIncluded') }}</span></span><span v-if="!key.valid" class="warning">{{ t('ssh.termius.invalidKey') }}</span></span>
            </label>
          </template>
        </div>
      </template>
      <p v-if="error" class="warning" role="alert" data-test="termius-error">{{ error }}</p>
    </div>
    <footer>
      <span v-if="preview" class="selection-count" aria-live="polite" data-test="termius-selection-count">{{ t('ssh.termius.selectedCount', { hosts: effectiveHosts.size, keys: keyCount }) }}</span>
      <button class="button" :disabled="importing" @click="close">{{ t('common.cancel') }}</button>
      <button v-if="preview" class="button" :disabled="busy" data-test="termius-back" @click="discard">{{ t('ssh.termius.rescan') }}</button>
      <button v-if="preview" class="button primary" :disabled="!canImport" data-test="termius-confirm" @click="confirm">{{ importing ? t('ssh.configImport.importing') : t('ssh.termius.confirm') }}</button>
    </footer>
  </aside>
</template>

<style scoped>
.termius-drawer { position: absolute; inset: 0 0 0 auto; z-index: 3; width: min(680px, 100%); display: flex; flex-direction: column; background: var(--panel); color: var(--fg); border-left: 1px solid var(--border); border-radius: 0 12px 12px 0; }
header, footer { display: flex; align-items: center; gap: 8px; padding: 14px 16px; flex-shrink: 0; }
header { justify-content: space-between; border-bottom: 1px solid var(--border); font-size: 14px; font-weight: 600; color: var(--fg-dim); }
footer { justify-content: flex-end; border-top: 1px solid var(--border); flex-wrap: wrap; }
.body { padding: 16px; overflow-y: auto; min-height: 0; display: flex; flex-direction: column; gap: 14px; flex: 1; }
.field { display: flex; flex-direction: column; gap: 6px; font-size: 12px; color: var(--fg-dim); }
.path-row { display: flex; gap: 6px; }
.path-row input { min-width: 0; flex: 1; }
input:not([type="checkbox"]) { box-sizing: border-box; width: 100%; height: 32px; padding: 6px 10px; background: var(--bg); color: var(--fg); border: 1px solid var(--border); border-radius: 6px; font: inherit; }
input:focus-visible, button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.button, .icon-button { min-height: 32px; padding: 6px 10px; background: var(--bg); color: var(--fg); border: 1px solid var(--border); border-radius: 6px; cursor: pointer; font: inherit; font-size: 13px; display: inline-flex; align-items: center; justify-content: center; }
.icon-button { padding: 4px; background: transparent; }
button:hover:not(:disabled) { border-color: var(--accent); }
.primary { background: var(--accent); color: var(--bg); }
button:disabled, input:disabled { opacity: .5; cursor: not-allowed; }
.hint, .warning { margin: 0; font-size: 12px; line-height: 1.5; overflow-wrap: anywhere; }
.hint { color: var(--fg-dim); }
.warning { color: var(--bad); }
h3 { margin: 4px 0 0; font-size: 13px; font-weight: 600; }
.entry { cursor: pointer; display: flex; gap: 10px; align-items: flex-start; border: 1px solid transparent; border-bottom-color: var(--border); padding: 10px 8px; border-radius: 4px; font-size: 13px; }
.entry input { margin-top: 3px; accent-color: var(--accent); }
.entry-body { display: flex; flex-direction: column; gap: 3px; min-width: 0; overflow-wrap: anywhere; }
.blocked { opacity: .65; cursor: default; }
.preview-tools { flex-shrink: 0; padding: 12px 16px; display: flex; flex-direction: column; gap: 10px; border-bottom: 1px solid var(--border); }
.tabs { display: flex; gap: 4px; }
.tab { border: 1px solid transparent; border-radius: 6px; padding: 6px 12px; font: inherit; font-size: 13px; color: var(--fg-dim); background: transparent; cursor: pointer; }
.tab[aria-selected="true"] { color: var(--fg); background: var(--bg); border-color: var(--border); }
.status-filters { display: flex; flex-wrap: wrap; gap: 6px; }
.status-filter { display: inline-flex; align-items: center; gap: 6px; padding: 4px 8px; min-height: 28px; border: 1px solid var(--border); border-radius: 6px; color: var(--fg-dim); background: transparent; font: inherit; font-size: 12px; cursor: pointer; }
.status-filter[aria-pressed="true"] { color: var(--accent); border-color: var(--accent); background: var(--bg); }
.status-filter span { font-size: 11px; font-variant-numeric: tabular-nums; }
.search-row { display: flex; align-items: center; gap: 8px; color: var(--fg-dim); }
.search-row input { min-width: 0; }
.selection-bar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; }
.bulk { display: flex; align-items: center; gap: 8px; font-size: 12px; cursor: pointer; }
input[type="checkbox"] { accent-color: var(--accent); width: 14px; height: 14px; flex-shrink: 0; }
.preview-body { padding: 0 8px 12px; gap: 8px; }
.preview-body > .warning { padding: 8px; }
.items { min-width: 0; }
.entry:hover:not(.blocked), .entry.selected { background: var(--bg); }
.entry.selected { border-left-color: var(--accent); }
.entry-title, .metadata, .credential { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.entry-title strong { font-size: 13px; font-weight: 500; }
.address { color: var(--fg-dim); font-size: 12px; font-family: var(--font-mono, monospace); }
.metadata { gap: 4px 10px; font-size: 11px; color: var(--fg-dim); }
.badge { display: inline-flex; max-width: 100%; padding: 1px 5px; border: 1px solid var(--border); border-radius: 4px; font-size: 11px; font-weight: 400; color: var(--fg-dim); }
.credential { gap: 4px; }
.empty { padding: 32px 12px; text-align: center; }
.selection-count { margin-right: auto; color: var(--fg-dim); font-size: 12px; }
@media (max-width: 480px) { footer { padding: 10px; gap: 6px; } .selection-count { flex-basis: 100%; } }
</style>
