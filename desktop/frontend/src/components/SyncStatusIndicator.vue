<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from '../i18n/useI18n'
import type { MessageKey } from '../i18n'
import { usePlatform } from '../platform'
import { getSyncStatus, syncNow, type SyncStatus, type PullResult } from '../lib/api'

// Desktop-only (design doc §6 "No mobile indicator" -- mobile reads
// preferences over HTTP from the relay and does not run this engine).
// Callers must gate mounting this component on `caps.wailsBindings`
// themselves (see SettingsDialog.vue's header), matching the pattern
// SettingsProfiles.vue documents: the whole component assumes
// GetSyncStatus/SyncNow exist rather than re-checking the cap internally,
// so a caller that forgets the gate fails loudly (bindings() throws) in
// dev/tests instead of silently degrading.
const { t } = useI18n()
const platform = usePlatform()

const status = ref<SyncStatus>({ state: 'idle', last_synced_at: 0, pending_keys: 0 })
const triggering = ref(false)
const triggerError = ref('')

// The most recent sync:pulled result still worth showing. Independent of
// `status`: a pull can adopt/conflict keys on a sync that otherwise reports
// idle. Cleared by the user dismissing it, or replaced wholesale by the
// next non-empty sync:pulled event.
const notice = ref<PullResult | null>(null)

// keys.* maps internal/prefssync.SyncedKeys() -- see sync.go -- to the
// human-readable setting name shown elsewhere in Settings. Kept as a literal
// table (not a template-string lookup) so every entry is a real, checked
// MessageKey.
const KEY_LABELS: Record<string, MessageKey> = {
  locale_preference: 'sync.keys.locale_preference',
  quick_templates: 'sync.keys.quick_templates',
  notifications_enabled: 'sync.keys.notifications_enabled',
  ai_notifications_only: 'sync.keys.ai_notifications_only',
  command_notify_threshold_seconds: 'sync.keys.command_notify_threshold_seconds',
  shell_integration_enabled: 'sync.keys.shell_integration_enabled',
  pinned_session_ids: 'sync.keys.pinned_session_ids',
  ssh_hosts_encrypted: 'sync.keys.ssh_hosts_encrypted',
  terminal_theme: 'sync.keys.terminal_theme',
  terminal_font_head: 'sync.keys.terminal_font_head',
  terminal_font_size: 'sync.keys.terminal_font_size',
  terminal_line_height: 'sync.keys.terminal_line_height',
  terminal_cursor_style: 'sync.keys.terminal_cursor_style',
  terminal_cursor_blink: 'sync.keys.terminal_cursor_blink',
  terminal_scrollback: 'sync.keys.terminal_scrollback',
  default_shell: 'sync.keys.default_shell',
  shortcut_bindings: 'sync.keys.shortcut_bindings',
  profiles_encrypted: 'sync.keys.profiles_encrypted',
}

// keyLabel turns a raw sync key into the name a user would recognize from
// Settings. A key with no entry above is one SyncedKeys() grew that this
// table hasn't caught up with yet -- fall back to a humanized form of the
// raw key (underscores to spaces, capitalized) rather than ever printing
// the literal snake_case identifier.
function keyLabel(key: string): string {
  const labelKey = KEY_LABELS[key]
  if (labelKey) return t(labelKey)
  return key.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase())
}

// last_synced_at: 0 means "never synced" (see SyncStatus in
// lib/api/_bindings.ts) and must read as "never", not as an epoch date.
const lastSyncedLabel = computed(() => {
  if (status.value.last_synced_at === 0) return t('sync.lastSyncedNever')
  const d = new Date(status.value.last_synced_at)
  const time = t('settings.devices.timeFormat', {
    y: d.getFullYear(),
    m: d.getMonth() + 1,
    d: d.getDate(),
    hh: String(d.getHours()).padStart(2, '0'),
    mm: String(d.getMinutes()).padStart(2, '0'),
  })
  return t('sync.lastSyncedAt', { time })
})

// "offline" (no relay configured / paused / logged out) is deliberately its
// own label and its own color -- design doc §2: "not being configured is
// not a failure and must not show a red indicator." Only "error" gets the
// bad-color treatment; see the template's state-* classes.
const stateLabel = computed(() => {
  switch (status.value.state) {
    case 'idle':
      return t('sync.stateIdle')
    case 'syncing':
      return t('sync.stateSyncing')
    case 'offline':
      return t('sync.stateOffline')
    case 'error':
      return t('sync.stateError')
  }
})

async function triggerSync() {
  if (status.value.state === 'syncing' || triggering.value) return
  triggering.value = true
  triggerError.value = ''
  try {
    await syncNow()
  } catch (e) {
    // SyncNow only ever rejects for "cannot start" (offline); the outcome
    // of a sync that did start arrives later via sync:status/sync:pulled.
    triggerError.value = e instanceof Error ? e.message : String(e)
  } finally {
    triggering.value = false
  }
}

function dismissNotice() {
  notice.value = null
}

function onStatus(data: unknown) {
  status.value = data as SyncStatus
}

function onPulled(data: unknown) {
  const result = data as PullResult
  if ((result.Adopted?.length ?? 0) === 0 && (result.Conflict?.length ?? 0) === 0) return
  notice.value = result
}

let offStatus: (() => void) | null = null
let offPulled: (() => void) | null = null
onMounted(async () => {
  offStatus = platform.events.on('sync:status', onStatus)
  offPulled = platform.events.on('sync:pulled', onPulled)
  try {
    status.value = await getSyncStatus()
  } catch {
    // Keep the idle default for first paint; a later sync:status event (or
    // the next mount) will correct it.
  }
})
onBeforeUnmount(() => {
  offStatus?.()
  offStatus = null
  offPulled?.()
  offPulled = null
})
</script>

<template>
  <div class="sync-indicator" data-testid="sync-indicator" :data-state="status.state">
    <div class="sync-row">
      <span class="sync-dot" :class="`state-${status.state}`" aria-hidden="true"></span>
      <span class="sync-state" data-testid="sync-state" :data-state="status.state">{{ stateLabel }}</span>
      <span class="sync-last" data-testid="sync-last-synced">{{ lastSyncedLabel }}</span>
      <span v-if="status.pending_keys > 0" class="sync-pending" data-testid="sync-pending">{{
        t('sync.pendingKeys', { count: status.pending_keys })
      }}</span>
      <button
        class="sync-now-btn"
        data-testid="sync-now-button"
        :disabled="status.state === 'syncing' || triggering"
        @click="triggerSync"
      >{{ t('sync.syncNow') }}</button>
    </div>

    <p v-if="status.state === 'error' && status.last_error" class="sync-error" data-testid="sync-error">
      {{ status.last_error }}
    </p>
    <p v-if="triggerError" class="sync-error" data-testid="sync-trigger-error">{{ triggerError }}</p>

    <div v-if="notice" class="sync-notice" data-testid="sync-notice">
      <p
        v-if="notice.Adopted && notice.Adopted.length"
        class="sync-notice-adopted"
        data-testid="sync-notice-adopted"
      >{{ t('sync.pulledAdopted', { keys: notice.Adopted.map(keyLabel).join(', ') }) }}</p>
      <p
        v-if="notice.Conflict && notice.Conflict.length"
        class="sync-notice-conflict"
        data-testid="sync-notice-conflict"
      >{{ t('sync.pulledConflict', { keys: notice.Conflict.map(keyLabel).join(', ') }) }}</p>
      <button class="sync-notice-dismiss" data-testid="sync-notice-dismiss" @click="dismissNotice">{{
        t('app.dismiss')
      }}</button>
    </div>
  </div>
</template>

<style scoped>
.sync-indicator { display: flex; flex-direction: column; gap: 4px; font-size: 0.78rem; }
.sync-row { display: flex; align-items: center; gap: 8px; }
.sync-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; background: var(--fg-dim); }
.sync-dot.state-idle { background: var(--good, #3fb950); }
.sync-dot.state-syncing { background: var(--accent); }
.sync-dot.state-offline { background: var(--fg-dim); }
.sync-dot.state-error { background: var(--bad); }
.sync-state { color: var(--fg); }
.sync-state[data-state="error"] { color: var(--bad); }
/* offline is deliberately NOT colored like error -- see design doc §2. */
.sync-state[data-state="offline"] { color: var(--fg-dim); }
.sync-last, .sync-pending { color: var(--fg-dim); }
.sync-pending { font-weight: 600; }
.sync-now-btn { margin-left: auto; font-size: 0.76rem; padding: 2px 8px; }
.sync-error { color: var(--bad); margin: 0; }
.sync-notice { display: flex; flex-direction: column; gap: 4px; border: 1px solid var(--border); border-radius: 6px; padding: 6px 8px; background: var(--bg); }
.sync-notice-adopted, .sync-notice-conflict { margin: 0; }
.sync-notice-dismiss { align-self: flex-end; font-size: 0.72rem; padding: 2px 8px; }
</style>
