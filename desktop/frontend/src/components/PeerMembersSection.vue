<script lang="ts" setup>
import { ref } from 'vue'
import { ShieldX, Trash2, X } from 'lucide-vue-next'
import { useI18n } from '../i18n/useI18n'
import type { PeerMember } from '../platform/types'

defineProps<{
  members: PeerMember[]
  loading: boolean
  busyPeerId: string
}>()

const emit = defineEmits<{
  (event: 'revoke', member: PeerMember): void
}>()

const { t } = useI18n()
const pendingPeerID = ref('')

function formatTime(unixSeconds: number | undefined): string {
  if (!unixSeconds) return t('settings.peer.members.never')
  return new Date(unixSeconds * 1000).toLocaleString()
}

function formatLastExchange(member: PeerMember): string {
  if (member.local) return t('settings.peer.members.thisDevice')
  if (!member.last_exchange_at) return t('settings.peer.members.neverExchanged')
  return new Date(member.last_exchange_at * 1000).toLocaleString()
}

function permissionLabel(permission: string): string {
  if (permission === 'view') return t('settings.peer.permission.view')
  if (permission === 'control') return t('settings.peer.permission.control')
  if (permission === 'full') return t('settings.peer.permission.full')
  return t('common.unknown')
}

function statusLabel(status: string): string {
  if (status === 'active') return t('settings.peer.members.status.active')
  if (status === 'revoked') return t('settings.peer.members.status.revoked')
  if (status === 'expired') return t('settings.peer.members.status.expired')
  return t('common.unknown')
}

function confirmRevocation(member: PeerMember): void {
  pendingPeerID.value = ''
  emit('revoke', member)
}
</script>

<template>
  <section class="peer-members" data-testid="peer-members-section">
    <div>
      <h3>{{ t('settings.peer.members.title') }}</h3>
      <p class="hint">{{ t('settings.peer.members.hint') }}</p>
    </div>

    <p v-if="loading" class="hint">{{ t('common.loading') }}</p>
    <p v-else-if="members.length === 0" class="hint">{{ t('settings.peer.members.empty') }}</p>
    <div v-else class="member-list">
      <article
        v-for="member in members"
        :key="member.peer_id"
        class="member-row"
        data-testid="peer-member-row"
      >
        <div class="member-heading">
          <code>{{ member.peer_id }}</code>
          <span v-if="member.local" class="badge local-badge">{{ t('settings.peer.members.thisDevice') }}</span>
          <span class="badge" :class="`status-${member.status}`">
            {{ statusLabel(member.status) }}
          </span>
          <button
            v-if="member.can_revoke && pendingPeerID !== member.peer_id"
            type="button"
            class="icon-action danger-action"
            data-testid="peer-member-revoke-open"
            :disabled="Boolean(busyPeerId)"
            :aria-label="t('settings.peer.members.revoke')"
            :title="t('settings.peer.members.revoke')"
            @click="pendingPeerID = member.peer_id"
          >
            <Trash2 :size="15" aria-hidden="true" />
          </button>
        </div>

        <dl class="member-details">
          <div>
            <dt>{{ t('settings.peer.permissionLabel') }}</dt>
            <dd>{{ permissionLabel(member.permission) }}</dd>
          </div>
          <div>
            <dt>{{ t('settings.peer.canInvite') }}</dt>
            <dd>{{ member.can_invite ? t('settings.peer.yes') : t('settings.peer.no') }}</dd>
          </div>
          <div>
            <dt>{{ t('settings.peer.canSyncSecrets') }}</dt>
            <dd>{{ member.can_sync_secrets ? t('settings.peer.yes') : t('settings.peer.no') }}</dd>
          </div>
          <div>
            <dt>{{ t('settings.peer.members.expires') }}</dt>
            <dd>{{ formatTime(member.expires_at) }}</dd>
          </div>
          <div>
            <dt>{{ t('settings.peer.members.lastDirectExchange') }}</dt>
            <dd>{{ formatLastExchange(member) }}</dd>
          </div>
          <div class="scope-detail">
            <dt>{{ t('settings.peer.sessionScope') }}</dt>
            <dd v-if="member.allowed_session_ids.length === 0">{{ t('settings.peer.scopeAll') }}</dd>
            <dd v-else class="scope-list">
              <code v-for="sessionID in member.allowed_session_ids" :key="sessionID">{{ sessionID }}</code>
            </dd>
          </div>
        </dl>

        <div
          v-if="pendingPeerID === member.peer_id"
          class="revoke-confirm"
          data-testid="peer-member-revoke-confirm"
        >
          <ShieldX :size="17" aria-hidden="true" />
          <p>{{ t('settings.peer.members.revokeConfirm') }}</p>
          <div class="confirm-actions">
            <button
              type="button"
              class="secondary-action"
              data-testid="peer-member-revoke-cancel"
              :disabled="busyPeerId === member.peer_id"
              @click="pendingPeerID = ''"
            >
              <X :size="14" aria-hidden="true" />
              {{ t('common.cancel') }}
            </button>
            <button
              type="button"
              class="danger-button"
              data-testid="peer-member-revoke-confirm-button"
              :disabled="Boolean(busyPeerId)"
              @click="confirmRevocation(member)"
            >
              <ShieldX :size="14" aria-hidden="true" />
              {{ busyPeerId === member.peer_id
                ? t('settings.peer.members.revoking')
                : t('settings.peer.members.confirmAction') }}
            </button>
          </div>
        </div>
      </article>
    </div>
  </section>
</template>

<style scoped>
.peer-members {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--border);
}
h3,
.hint,
.revoke-confirm p {
  margin: 0;
}
h3 {
  color: var(--fg);
  font-size: 13px;
}
.hint {
  color: var(--fg-dim);
  font-size: 12px;
  line-height: 1.5;
}
.member-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.member-row {
  min-width: 0;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: color-mix(in srgb, var(--panel) 72%, transparent);
}
.member-heading {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
}
.member-heading > code {
  overflow-wrap: anywhere;
  color: var(--fg);
  font-size: 11px;
}
.badge {
  flex: 0 0 auto;
  padding: 2px 6px;
  border: 1px solid var(--border);
  border-radius: 999px;
  color: var(--fg-dim);
  font-size: 10px;
}
.local-badge,
.status-active {
  border-color: color-mix(in srgb, var(--good) 45%, var(--border));
  color: var(--good);
}
.status-revoked,
.status-expired {
  border-color: color-mix(in srgb, var(--bad) 40%, var(--border));
  color: var(--bad);
}
.icon-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  min-width: 30px;
  height: 30px;
  margin-left: auto;
  padding: 0;
  border: 1px solid transparent;
  border-radius: 5px;
  background: transparent;
  color: var(--fg-dim);
}
.danger-action:hover:not(:disabled) {
  border-color: var(--bad);
  color: var(--bad);
}
.member-details {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px 12px;
  margin: 10px 0 0;
}
.member-details div {
  min-width: 0;
}
.member-details dt {
  color: var(--fg-dim);
  font-size: 10px;
}
.member-details dd {
  margin: 2px 0 0;
  overflow-wrap: anywhere;
  color: var(--fg);
  font-size: 11px;
}
.scope-detail {
  grid-column: 1 / -1;
}
.scope-list {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.scope-list code {
  padding: 2px 4px;
  border: 1px solid var(--border);
  border-radius: 3px;
}
.revoke-confirm {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px solid var(--border);
  color: var(--bad);
}
.revoke-confirm p {
  color: var(--fg);
  font-size: 11px;
  line-height: 1.45;
}
.confirm-actions {
  display: flex;
  gap: 6px;
}
.secondary-action,
.danger-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  min-height: 30px;
  padding: 5px 9px;
  border: 1px solid var(--border);
  border-radius: 5px;
  font-size: 11px;
}
.secondary-action {
  background: transparent;
  color: var(--fg);
}
.danger-button {
  border-color: var(--bad);
  background: var(--bad);
  color: white;
}
button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}
@media (max-width: 640px) {
  .member-details {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .revoke-confirm {
    grid-template-columns: auto minmax(0, 1fr);
  }
  .confirm-actions {
    grid-column: 1 / -1;
  }
  .confirm-actions button {
    flex: 1 1 0;
  }
}
</style>
