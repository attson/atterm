<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from '../i18n/useI18n'
import type { MessageKey } from '../i18n'
import { usePlatform } from '../platform'
import { getSyncStatus, syncNow, type SyncStatus, type PullResult } from '../lib/api'
import { formatAgo } from '../lib/formatAgo'

// Desktop-only (design doc §6 "No mobile indicator" -- mobile reads
// preferences over HTTP from the relay and does not run this engine).
// Gated internally on `caps.wailsBindings`: below, `enabled` short-circuits
// onMounted so it never calls GetSyncStatus/SyncNow, and the template's
// root v-if renders nothing at all on web/Capacitor. An earlier draft left
// this ungated and relied on the fetch/SyncNow rejections being caught and
// swallowed -- but "swallowed" there actually meant a confidently false UI
// ("Up to date - Never synced" with a live-looking, silently-broken
// button), which is worse than showing nothing. SettingsDialog.vue also
// gates mounting this component at all (`v-if="caps.wailsBindings"`,
// matching SettingsProfiles.vue's precedent) -- that external gate stays
// as defense in depth, not as the only gate.
const { t } = useI18n()
const platform = usePlatform()
const enabled = platform.caps.wailsBindings

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
// Design doc §5 asks for *relative* last-sync time; last_synced_at is
// milliseconds (per _bindings.ts) while formatAgo takes unix seconds, hence
// the /1000 below.
const lastSyncedLabel = computed(() => {
  if (status.value.last_synced_at === 0) return t('sync.lastSyncedNever')
  const ago = formatAgo(Math.floor(status.value.last_synced_at / 1000))
  return t('sync.lastSyncedAt', { time: ago })
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
  // Full replace, not a merge: Go's SyncStatus.last_error is `omitempty`, so
  // a success payload omits the field entirely rather than sending "". A
  // merge (`{ ...status.value, ...data }`) would leave a stale last_error
  // sitting in the reactive object forever once set, even though the
  // template's error paragraph now trusts last_error's mere presence (see
  // below) instead of re-checking state -- exactly the silent-failure mode
  // design doc §2 exists to prevent.
  status.value = data as SyncStatus
  // A prior "cannot start" trigger error is now stale: the engine has just
  // reported fresher, authoritative status, so any locally-generated error
  // text from an earlier click no longer applies.
  triggerError.value = ''
}

function onPulled(data: unknown) {
  const result = data as PullResult
  if ((result.adopted?.length ?? 0) === 0 && (result.conflict?.length ?? 0) === 0) return
  notice.value = result
}

let offStatus: (() => void) | null = null
let offPulled: (() => void) | null = null
onMounted(async () => {
  if (!enabled) return
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
  <div v-if="enabled" class="sync-indicator" data-testid="sync-indicator" :data-state="status.state">
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

    <!-- Gate on last_error's mere presence, not status.state === 'error':
         the Go side guarantees last_error is only ever populated when state
         is 'error' (see SyncStatus in lib/api/_bindings.ts), so this stays
         correct either way -- and gating on presence alone is what makes a
         merge-vs-replace bug in onStatus actually visible instead of
         accidentally masked by a redundant state check. -->
    <p v-if="status.last_error" class="sync-error" data-testid="sync-error">
      {{ status.last_error }}
    </p>
    <p v-if="triggerError" class="sync-error" data-testid="sync-trigger-error">{{ triggerError }}</p>

    <div v-if="notice" class="sync-notice" data-testid="sync-notice">
      <p
        v-if="notice.adopted && notice.adopted.length"
        class="sync-notice-adopted"
        data-testid="sync-notice-adopted"
      >{{ t('sync.pulledAdopted', { keys: notice.adopted.map(keyLabel).join(', ') }) }}</p>
      <p
        v-if="notice.conflict && notice.conflict.length"
        class="sync-notice-conflict"
        data-testid="sync-notice-conflict"
      >{{ t('sync.pulledConflict', { keys: notice.conflict.map(keyLabel).join(', ') }) }}</p>
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
