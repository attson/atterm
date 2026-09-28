<script lang="ts" setup>
import { computed, onMounted, ref } from 'vue'
import { Check, QrCode, Search, ShieldCheck } from 'lucide-vue-next'
import { useI18n } from '../i18n/useI18n'
import { usePlatform } from '../platform'
import { QRScanner } from '../platform/qrScanner'
import type { PeerConnectionPreview, PeerSpaceStatus } from '../platform/types'

const { t } = useI18n()
const platform = usePlatform()

const loading = ref(true)
const previewing = ref(false)
const joining = ref(false)
const scanning = ref(false)
const error = ref('')
const status = ref<PeerSpaceStatus | null>(null)
const bundleInput = ref('')
const previewedBundle = ref('')
const preview = ref<PeerConnectionPreview | null>(null)

const canPreview = computed(() => bundleInput.value.trim() !== '' && !previewing.value && !joining.value)
const fingerprint = computed(() => {
  const hash = status.value?.genesis_hash?.trim()
  if (!hash) return ''
  return hash.startsWith('SHA256:') ? hash : `SHA256:${hash}`
})

onMounted(async () => {
  if (!platform.peer) {
    error.value = t('settings.peer.errors.unavailable')
    loading.value = false
    return
  }
  try {
    status.value = await platform.peer.status()
  } catch {
    error.value = t('settings.peer.errors.status')
  } finally {
    loading.value = false
  }
})

function onBundleInput(): void {
  preview.value = null
  previewedBundle.value = ''
  error.value = ''
}

function describePeerError(value: unknown, phase: 'preview' | 'join'): string {
  const message = value instanceof Error ? value.message : String(value)
  if (/fingerprint.*does not match/i.test(message)) return t('settings.peer.errors.fingerprintMismatch')
  if (/timeout|timed out|connection refused|no such host|network|fetch|dial tcp|unreachable/i.test(message)) {
    return t('settings.peer.errors.hostUnavailable')
  }
  if (/expired/i.test(message)) return t('settings.peer.errors.expired')
  if (/join rejected/i.test(message)) {
    return t(phase === 'preview' ? 'settings.peer.errors.invalidBundle' : 'settings.peer.errors.joinRejected')
  }
  return t(phase === 'preview' ? 'settings.peer.errors.invalidBundle' : 'settings.peer.errors.joinFailed')
}

async function previewBundle(raw = bundleInput.value): Promise<void> {
  const peer = platform.peer
  const normalized = raw.trim()
  if (!peer || !normalized || previewing.value || joining.value) return
  error.value = ''
  preview.value = null
  previewedBundle.value = ''
  previewing.value = true
  try {
    const result = await peer.previewConnectionBundle(normalized)
    preview.value = result
    previewedBundle.value = normalized
  } catch (e) {
    error.value = describePeerError(e, 'preview')
  } finally {
    previewing.value = false
  }
}

async function joinSpace(): Promise<void> {
  const peer = platform.peer
  const confirmed = preview.value
  if (!peer || !confirmed || !previewedBundle.value || joining.value) return
  error.value = ''
  joining.value = true
  try {
    status.value = await peer.joinSpace({
      connection_bundle: previewedBundle.value,
      expected_fingerprint: confirmed.fingerprint,
    })
    bundleInput.value = ''
    previewedBundle.value = ''
    preview.value = null
  } catch (e) {
    error.value = describePeerError(e, 'join')
  } finally {
    joining.value = false
  }
}

async function scanBundle(): Promise<void> {
  if (scanning.value || previewing.value || joining.value) return
  error.value = ''
  scanning.value = true
  try {
    const { camera } = await QRScanner.requestPermissions()
    if (camera !== 'granted') {
      error.value = t('settings.peer.errors.cameraDenied')
      return
    }
    const result = await QRScanner.scan()
    if (result.cancelled) return
    if (!result.rawValue?.trim()) {
      error.value = t('settings.peer.errors.emptyQr')
      return
    }
    bundleInput.value = result.rawValue.trim()
    await previewBundle(bundleInput.value)
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e)
    error.value = /PLUGIN_NOT_AVAILABLE|not implemented/i.test(message)
      ? t('settings.peer.errors.scanUnavailable')
      : t('settings.peer.errors.scanFailed')
  } finally {
    scanning.value = false
  }
}

function formatTime(unixSeconds: number | undefined): string {
  if (!unixSeconds) return t('common.unknown')
  return new Date(unixSeconds * 1000).toLocaleString()
}

function permissionLabel(permission: string): string {
  if (permission === 'view') return t('settings.peer.permission.view')
  if (permission === 'control') return t('settings.peer.permission.control')
  if (permission === 'full') return t('settings.peer.permission.full')
  return t('common.unknown')
}
</script>

<template>
  <div class="tab-pane" data-testid="settings-peer">
    <p v-if="loading" class="hint">{{ t('common.loading') }}</p>

    <template v-else-if="status?.configured">
      <section class="peer-section" data-testid="peer-configured">
        <div class="section-heading">
          <ShieldCheck :size="17" aria-hidden="true" />
          <div>
            <h3>{{ t('settings.peer.connectedTitle') }}</h3>
            <p class="hint">{{ t('settings.peer.connectedHint') }}</p>
          </div>
        </div>
        <dl class="detail-list">
          <div>
            <dt>{{ t('settings.peer.peerId') }}</dt>
            <dd class="mono">{{ status.peer_id }}</dd>
          </div>
          <div>
            <dt>{{ t('settings.peer.spaceId') }}</dt>
            <dd class="mono">{{ status.space_id }}</dd>
          </div>
          <div>
            <dt>{{ t('settings.peer.fingerprint') }}</dt>
            <dd class="mono">{{ fingerprint }}</dd>
          </div>
          <div>
            <dt>{{ t('settings.peer.createdAt') }}</dt>
            <dd>{{ formatTime(status.created_at) }}</dd>
          </div>
        </dl>
      </section>

      <section class="peer-section">
        <h3>{{ t('settings.peer.invitationSummary') }}</h3>
        <div class="metrics" data-testid="peer-invitation-summary">
          <div><strong>{{ status.open_invitations }}</strong><span>{{ t('settings.peer.invitationOpen') }}</span></div>
          <div><strong>{{ status.used_invitations }}</strong><span>{{ t('settings.peer.invitationUsed') }}</span></div>
          <div><strong>{{ status.revoked_invitations }}</strong><span>{{ t('settings.peer.invitationRevoked') }}</span></div>
          <div><strong>{{ status.expired_invitations }}</strong><span>{{ t('settings.peer.invitationExpired') }}</span></div>
        </div>
      </section>
    </template>

    <template v-else>
      <section class="peer-section">
        <h3>{{ t('settings.peer.joinTitle') }}</h3>
        <p class="hint">{{ t('settings.peer.joinHint') }}</p>
        <label class="field-label" for="peer-bundle-input">{{ t('settings.peer.bundleLabel') }}</label>
        <textarea
          id="peer-bundle-input"
          v-model="bundleInput"
          data-testid="peer-bundle-input"
          rows="4"
          :placeholder="t('settings.peer.bundlePlaceholder')"
          :disabled="previewing || joining"
          autocomplete="off"
          spellcheck="false"
          @input="onBundleInput"
        />
        <div class="actions">
          <button
            type="button"
            class="primary-action"
            data-testid="peer-preview-button"
            :disabled="!canPreview"
            @click="previewBundle()"
          >
            <Search :size="15" aria-hidden="true" />
            {{ previewing ? t('settings.peer.previewing') : t('settings.peer.previewAction') }}
          </button>
          <button
            v-if="platform.caps.capacitor"
            type="button"
            class="secondary-action"
            data-testid="peer-scan-button"
            :disabled="scanning || previewing || joining"
            @click="scanBundle"
          >
            <QrCode :size="15" aria-hidden="true" />
            {{ scanning ? t('settings.peer.scanning') : t('settings.peer.scanAction') }}
          </button>
        </div>
      </section>

      <section v-if="preview" class="peer-section preview" data-testid="peer-preview">
        <div class="section-heading">
          <ShieldCheck :size="17" aria-hidden="true" />
          <div>
            <h3>{{ t('settings.peer.confirmTitle') }}</h3>
            <p class="hint">{{ t('settings.peer.confirmHint') }}</p>
          </div>
        </div>
        <dl class="detail-list">
          <div>
            <dt>{{ t('settings.peer.fingerprint') }}</dt>
            <dd class="mono fingerprint">{{ preview.fingerprint }}</dd>
          </div>
          <div>
            <dt>{{ t('settings.peer.spaceId') }}</dt>
            <dd class="mono">{{ preview.space_id }}</dd>
          </div>
          <div>
            <dt>{{ t('settings.peer.issuerPeerId') }}</dt>
            <dd class="mono">{{ preview.issuer_peer_id }}</dd>
          </div>
          <div>
            <dt>{{ t('settings.peer.permissionLabel') }}</dt>
            <dd>{{ permissionLabel(preview.permission) }}</dd>
          </div>
          <div>
            <dt>{{ t('settings.peer.sessionScope') }}</dt>
            <dd data-testid="peer-session-scope">
              <span v-if="preview.allowed_session_ids.length === 0">{{ t('settings.peer.scopeAll') }}</span>
              <span v-else class="session-list">
                <code v-for="sessionId in preview.allowed_session_ids" :key="sessionId">{{ sessionId }}</code>
              </span>
            </dd>
          </div>
          <div>
            <dt>{{ t('settings.peer.canInvite') }}</dt>
            <dd>{{ preview.can_invite ? t('settings.peer.yes') : t('settings.peer.no') }}</dd>
          </div>
          <div>
            <dt>{{ t('settings.peer.canSyncSecrets') }}</dt>
            <dd>{{ preview.can_sync_secrets ? t('settings.peer.yes') : t('settings.peer.no') }}</dd>
          </div>
          <div>
            <dt>{{ t('settings.peer.invitationExpires') }}</dt>
            <dd>{{ formatTime(preview.invitation_expires_at) }}</dd>
          </div>
          <div>
            <dt>{{ t('settings.peer.bundleExpires') }}</dt>
            <dd>{{ formatTime(preview.bundle_expires_at) }}</dd>
          </div>
          <div>
            <dt>{{ t('settings.peer.route') }}</dt>
            <dd>
              <span>{{ preview.route_kind === 'quick_tunnel' ? t('settings.peer.routeQuickTunnel') : preview.route_kind }}</span>
              <code class="route-url">{{ preview.route_url }}</code>
            </dd>
          </div>
        </dl>
        <button
          type="button"
          class="primary-action join-action"
          data-testid="peer-join-button"
          :disabled="joining"
          @click="joinSpace"
        >
          <Check :size="15" aria-hidden="true" />
          {{ joining ? t('settings.peer.joining') : t('settings.peer.joinAction') }}
        </button>
      </section>
    </template>

    <p v-if="error" class="error" role="alert" data-testid="peer-error">{{ error }}</p>
  </div>
</template>

<style scoped>
.tab-pane {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
}
.peer-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--border);
}
.peer-section:last-of-type {
  border-bottom: 0;
  padding-bottom: 0;
}
.peer-section h3 {
  margin: 0;
  color: var(--fg);
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0;
}
.section-heading {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  color: var(--good);
}
.section-heading > div {
  min-width: 0;
}
.hint {
  margin: 0;
  color: var(--fg-dim);
  font-size: 12px;
  line-height: 1.5;
}
.field-label,
.detail-list dt {
  color: var(--fg-dim);
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0.05em;
  text-transform: uppercase;
}
textarea {
  box-sizing: border-box;
  width: 100%;
  min-height: 88px;
  resize: vertical;
  padding: 9px 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
  outline: none;
  background: var(--bg);
  color: var(--fg);
  font: 12px/1.5 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  overflow-wrap: anywhere;
}
textarea:focus {
  box-shadow: 0 0 0 2px var(--accent);
}
textarea:disabled {
  opacity: 0.6;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-height: 32px;
  padding: 6px 12px;
  border: 1px solid var(--border);
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}
button:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}
.primary-action {
  border-color: var(--accent);
  background: var(--accent);
  color: #0d1117;
  font-weight: 600;
}
.secondary-action {
  background: transparent;
  color: var(--fg);
}
.secondary-action:hover:not(:disabled) {
  background: color-mix(in srgb, var(--fg) 6%, transparent);
}
.preview {
  padding-top: 4px;
}
.detail-list {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 12px 20px;
  margin: 0;
}
.detail-list > div {
  min-width: 0;
}
.detail-list dt {
  margin-bottom: 4px;
}
.detail-list dd {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
  margin: 0;
  color: var(--fg);
  font-size: 13px;
  line-height: 1.45;
  overflow-wrap: anywhere;
}
.mono,
.route-url,
.session-list code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
.fingerprint {
  color: var(--good) !important;
  font-weight: 600;
}
.route-url {
  color: var(--fg-dim);
  font-size: 11px;
  overflow-wrap: anywhere;
}
.session-list {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.session-list code {
  overflow-wrap: anywhere;
}
.join-action {
  align-self: flex-start;
}
.metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
}
.metrics > div {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  padding: 8px 10px;
  border-left: 2px solid var(--border);
}
.metrics strong {
  color: var(--fg);
  font-size: 15px;
}
.metrics span {
  color: var(--fg-dim);
  font-size: 11px;
}
.error {
  margin: 0;
  color: var(--bad);
  font-size: 12px;
  line-height: 1.5;
}
@media (max-width: 640px) {
  .detail-list {
    grid-template-columns: minmax(0, 1fr);
  }
  .metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .actions button,
  .join-action {
    width: 100%;
  }
}
</style>
