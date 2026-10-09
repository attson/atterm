<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Cable, Check, Copy, Database, Download, Play, Plus, QrCode, RadioTower, RefreshCw, Save, Search, ShieldCheck, Square, Trash2, X } from 'lucide-vue-next'
import { useI18n } from '../i18n/useI18n'
import { copyTextToClipboard } from '../lib/terminalCopy'
import { usePlatform } from '../platform'
import { QRScanner } from '../platform/qrScanner'
import type { PeerConfigSyncStatus, PeerConnectionPreview, PeerInvitation, PeerLANConfig, PeerMember, PeerQuickTunnelStatus, PeerRendezvousConfig, PeerRendezvousStatus, PeerSpaceStatus } from '../platform/types'
import PeerMembersSection from './PeerMembersSection.vue'
import SelectDropdown, { type SelectOption } from './SelectDropdown.vue'

const { t } = useI18n()
const platform = usePlatform()

const loading = ref(true)
const previewing = ref(false)
const joining = ref(false)
const scanning = ref(false)
const tunnelBusy = ref(false)
const routeCopying = ref(false)
const routeCopied = ref(false)
const routeImporting = ref(false)
const routeBundleInput = ref('')
const routeImportResult = ref<'quick_tunnel' | 'manual_lan' | 'no_route' | ''>('')
const creatingSpace = ref(false)
const invitationsLoading = ref(false)
const invitationCreating = ref(false)
const invitationActionID = ref('')
const membersLoading = ref(false)
const memberActionID = ref('')
const configSyncLoading = ref(false)
const configSyncAction = ref<'accept' | 'discard' | 'sync' | ''>('')
const rendezvousBusy = ref<'save' | 'reconnect' | ''>('')
const lanBusy = ref(false)
const discardPendingConfirming = ref(false)
const copiedInvitationID = ref('')
const error = ref('')
const status = ref<PeerSpaceStatus | null>(null)
const tunnelStatus = ref<PeerQuickTunnelStatus | null>(null)
const invitations = ref<PeerInvitation[]>([])
const members = ref<PeerMember[]>([])
const configSyncStatus = ref<PeerConfigSyncStatus | null>(null)
const rendezvousStatus = ref<PeerRendezvousStatus | null>(null)
const rendezvousMode = ref<'disabled' | 'official' | 'custom'>('disabled')
const rendezvousURL = ref('')
const rendezvousSTUNMode = ref<'default' | 'custom' | 'disabled'>('default')
const rendezvousSTUNURLs = ref('')
const lanEnabled = ref(false)
const lanAutoDiscovery = ref(false)
const lanAdvertiseHost = ref('')
const lanPort = ref('8484')
const lanRunning = ref(false)
const lanDiscoveryRunning = ref(false)
const lanListenAddress = ref('')
const lanLastError = ref('')
const lanDiscoveryLastError = ref('')
const lanRoutes = ref<Array<{ host: string; port: number; fingerprint: string }>>([])
const lanRouteHost = ref('')
const lanRoutePort = ref('8484')
const lanRouteFingerprint = ref('')
let rendezvousStatusPoll: ReturnType<typeof setInterval> | undefined
let rendezvousStatusPolling = false
const bundleInput = ref('')
const previewedBundle = ref('')
const preview = ref<PeerConnectionPreview | null>(null)
const inviteCount = ref(1)
const inviteValidFor = ref('24')
const invitePermission = ref<'view' | 'control' | 'full'>('control')
const inviteCanInvite = ref(false)
const inviteCanSyncSecrets = ref(false)
const inviteSessionScope = ref('')

const canPreview = computed(() => bundleInput.value.trim() !== '' && !previewing.value && !joining.value)
const hasPublishedMemberRoute = computed(() => Boolean(
  tunnelStatus.value?.running || rendezvousStatus.value?.state === 'online' || lanRunning.value,
))
const hasPublishedBootstrapRoute = computed(() => Boolean(
  tunnelStatus.value?.running || lanRunning.value,
))
const hasQuickTunnelHost = computed(() => Boolean(
  platform.peer?.getQuickTunnelStatus
  && platform.peer.startQuickTunnel
  && platform.peer.stopQuickTunnel
  && platform.peer.createConnectionBundle,
))
const hasLANHost = computed(() => Boolean(platform.peer?.getLANConfig && platform.peer.setLANConfig))
const fingerprint = computed(() => {
  const hash = status.value?.genesis_hash?.trim()
  if (!hash) return ''
  return hash.startsWith('SHA256:') ? hash : `SHA256:${hash}`
})
const validityOptions = computed<SelectOption[]>(() => [
  { value: '24', label: t('settings.peer.invitations.validity24') },
  { value: '168', label: t('settings.peer.invitations.validity168') },
  { value: '720', label: t('settings.peer.invitations.validity720') },
])
const permissionOptions = computed<SelectOption[]>(() => [
  { value: 'view', label: t('settings.peer.permission.view') },
  { value: 'control', label: t('settings.peer.permission.control') },
  { value: 'full', label: t('settings.peer.permission.full') },
])
const rendezvousModeOptions = computed<SelectOption[]>(() => [
  { value: 'disabled', label: t('settings.peer.rendezvous.mode.disabled') },
  { value: 'official', label: t('settings.peer.rendezvous.mode.official') },
  { value: 'custom', label: t('settings.peer.rendezvous.mode.custom') },
])
const rendezvousSTUNOptions = computed<SelectOption[]>(() => [
  { value: 'default', label: t('settings.peer.rendezvous.stun.default') },
  { value: 'custom', label: t('settings.peer.rendezvous.stun.custom') },
  { value: 'disabled', label: t('settings.peer.rendezvous.stun.disabled') },
])

onMounted(async () => {
  if (!platform.peer) {
    error.value = t('settings.peer.errors.unavailable')
    loading.value = false
    return
  }
  try {
    status.value = await platform.peer.status()
    if (status.value.configured) {
      await loadConfiguredPeerData()
    }
  } catch {
    error.value = t('settings.peer.errors.status')
  } finally {
    loading.value = false
  }
})

onBeforeUnmount(() => {
  if (rendezvousStatusPoll) clearInterval(rendezvousStatusPoll)
  rendezvousStatusPoll = undefined
})

function ensureRendezvousStatusPolling(): void {
  if (rendezvousStatusPoll) return
  rendezvousStatusPoll = setInterval(() => void refreshRendezvousStatus(), 2000)
}

async function refreshRendezvousStatus(): Promise<void> {
  const peer = platform.peer
  if (!peer || rendezvousStatusPolling || rendezvousBusy.value) return
  rendezvousStatusPolling = true
  try {
    rendezvousStatus.value = await peer.getRendezvousStatus()
  } catch {
    error.value = t('settings.peer.errors.rendezvousStatus')
  } finally {
    rendezvousStatusPolling = false
  }
}

async function loadConfiguredPeerData(): Promise<void> {
  const peer = platform.peer
  if (!peer) return
  ensureRendezvousStatusPolling()
  invitationsLoading.value = true
  membersLoading.value = true
  configSyncLoading.value = true
  const [tunnelResult, invitationResult, memberResult, configSyncResult, rendezvousConfigResult, rendezvousStatusResult, lanConfigResult] = await Promise.allSettled([
    peer.getQuickTunnelStatus?.() ?? Promise.resolve(null),
    peer.listInvitations(),
    peer.listMembers(),
    peer.configSyncStatus(),
    peer.getRendezvousConfig(),
    peer.getRendezvousStatus(),
    peer.getLANConfig?.() ?? Promise.resolve(null),
  ])
  if (tunnelResult.status === 'fulfilled') {
    tunnelStatus.value = tunnelResult.value
  } else {
    error.value = t('settings.peer.errors.status')
  }
  if (invitationResult.status === 'fulfilled') {
    invitations.value = invitationResult.value
  } else {
    error.value = t('settings.peer.errors.invitationLoad')
  }
  invitationsLoading.value = false
  if (memberResult.status === 'fulfilled') {
    members.value = memberResult.value
  } else {
    error.value = t('settings.peer.errors.memberLoad')
  }
  membersLoading.value = false
  if (configSyncResult.status === 'fulfilled') {
    configSyncStatus.value = configSyncResult.value
  } else {
    error.value = t('settings.peer.errors.syncStatus')
  }
  configSyncLoading.value = false
  if (rendezvousConfigResult.status === 'fulfilled') {
    applyRendezvousConfig(rendezvousConfigResult.value)
  } else {
    error.value = t('settings.peer.errors.rendezvousStatus')
  }
  if (rendezvousStatusResult.status === 'fulfilled') {
    rendezvousStatus.value = rendezvousStatusResult.value
  } else {
    error.value = t('settings.peer.errors.rendezvousStatus')
  }
  if (lanConfigResult.status === 'fulfilled' && lanConfigResult.value) {
    applyLANConfig(lanConfigResult.value)
  } else if (peer.getLANConfig) {
    error.value = t('settings.peer.errors.lanStatus')
  }
}

function applyLANConfig(config: PeerLANConfig): void {
  lanEnabled.value = config.enabled
  lanAutoDiscovery.value = config.auto_discovery
  lanAdvertiseHost.value = config.advertise_host || ''
  lanPort.value = String(config.port || 8484)
  lanRunning.value = config.running
  lanDiscoveryRunning.value = config.discovery_running
  lanListenAddress.value = config.listen_address || ''
  lanLastError.value = config.last_error || ''
  lanDiscoveryLastError.value = config.discovery_last_error || ''
  lanRoutes.value = (config.routes || []).map(route => ({
    host: route.host,
    port: route.port,
    fingerprint: route.fingerprint,
  }))
}

async function persistLANConfig(routes = lanRoutes.value): Promise<boolean> {
  const peer = platform.peer
  const save = peer?.setLANConfig
  const load = peer?.getLANConfig
  if (!save || !load || lanBusy.value) return false
  error.value = ''
  lanBusy.value = true
  try {
    await save({
      enabled: lanEnabled.value,
      auto_discovery: lanAutoDiscovery.value,
      advertise_host: lanAdvertiseHost.value.trim(),
      port: Math.trunc(Number(lanPort.value)),
      routes,
    })
    applyLANConfig(await load())
    return true
  } catch {
    error.value = t('settings.peer.errors.lanSave')
    return false
  } finally {
    lanBusy.value = false
  }
}

async function addLANRoute(): Promise<void> {
  const route = {
    host: lanRouteHost.value.trim(),
    port: Math.trunc(Number(lanRoutePort.value)),
    fingerprint: lanRouteFingerprint.value.trim(),
  }
  if (!route.host || !route.fingerprint) return
  if (await persistLANConfig([...lanRoutes.value, route])) {
    lanRouteHost.value = ''
    lanRouteFingerprint.value = ''
  }
}

async function removeLANRoute(index: number): Promise<void> {
  await persistLANConfig(lanRoutes.value.filter((_, routeIndex) => routeIndex !== index))
}

function applyRendezvousConfig(config: PeerRendezvousConfig): void {
  rendezvousMode.value = config.mode as typeof rendezvousMode.value
  rendezvousURL.value = config.url || ''
  rendezvousSTUNMode.value = config.stun_mode as typeof rendezvousSTUNMode.value
  rendezvousSTUNURLs.value = (config.stun_urls || []).join('\n')
}

function parseSTUNURLs(value: string): string[] {
  return value.split(/[\s,]+/).map(item => item.trim()).filter(Boolean)
}

async function saveRendezvousConfig(): Promise<void> {
  const peer = platform.peer
  if (!peer || rendezvousBusy.value) return
  error.value = ''
  rendezvousBusy.value = 'save'
  try {
    await peer.setRendezvousConfig({
      mode: rendezvousMode.value,
      url: rendezvousURL.value.trim(),
      stun_mode: rendezvousSTUNMode.value,
      stun_urls: rendezvousSTUNMode.value === 'custom' ? parseSTUNURLs(rendezvousSTUNURLs.value) : [],
    })
    const [config, nextStatus] = await Promise.all([
      peer.getRendezvousConfig(),
      peer.getRendezvousStatus(),
    ])
    applyRendezvousConfig(config)
    rendezvousStatus.value = nextStatus
    routeCopied.value = false
  } catch {
    error.value = t('settings.peer.errors.rendezvousSave')
  } finally {
    rendezvousBusy.value = ''
  }
}

async function reconnectRendezvous(): Promise<void> {
  const peer = platform.peer
  if (!peer || rendezvousBusy.value || rendezvousMode.value === 'disabled') return
  error.value = ''
  rendezvousBusy.value = 'reconnect'
  try {
    rendezvousStatus.value = await peer.reconnectRendezvous()
    routeCopied.value = false
  } catch {
    error.value = t('settings.peer.errors.rendezvousReconnect')
  } finally {
    rendezvousBusy.value = ''
  }
}

async function syncConfigNow(): Promise<void> {
  const peer = platform.peer
  if (!peer || configSyncAction.value || configSyncLoading.value) return
  error.value = ''
  configSyncAction.value = 'sync'
  try {
    configSyncStatus.value = await peer.syncConfigNow()
  } catch {
    error.value = t('settings.peer.errors.syncNow')
  } finally {
    configSyncAction.value = ''
  }
}

function rendezvousStateLabel(state: string | undefined): string {
  if (state === 'online') return t('settings.peer.rendezvous.state.online')
  if (state === 'connecting') return t('settings.peer.rendezvous.state.connecting')
  if (state === 'waiting') return t('settings.peer.rendezvous.state.waiting')
  if (state === 'error') return t('settings.peer.rendezvous.state.error')
  return t('settings.peer.rendezvous.state.disabled')
}

function rendezvousErrorLabel(code: string | undefined): string {
  if (code === 'registration_timeout') return t('settings.peer.rendezvous.error.registrationTimeout')
  if (code === 'authentication_failed') return t('settings.peer.rendezvous.error.authenticationFailed')
  if (code === 'invalid_config') return t('settings.peer.rendezvous.error.invalidConfig')
  return t('settings.peer.rendezvous.error.serviceUnavailable')
}

async function refreshConfigSyncStatus(): Promise<void> {
  const peer = platform.peer
  if (!peer || configSyncLoading.value || configSyncAction.value) return
  error.value = ''
  configSyncLoading.value = true
  try {
    configSyncStatus.value = await peer.configSyncStatus()
  } catch {
    error.value = t('settings.peer.errors.syncStatus')
  } finally {
    configSyncLoading.value = false
  }
}

async function acceptPendingConfig(): Promise<void> {
  const peer = platform.peer
  if (!peer || configSyncAction.value) return
  error.value = ''
  configSyncAction.value = 'accept'
  try {
    configSyncStatus.value = await peer.acceptPendingConfig()
    discardPendingConfirming.value = false
  } catch {
    error.value = t('settings.peer.errors.pendingAccept')
  } finally {
    configSyncAction.value = ''
  }
}

async function discardPendingConfig(): Promise<void> {
  const peer = platform.peer
  if (!peer || configSyncAction.value) return
  error.value = ''
  configSyncAction.value = 'discard'
  try {
    configSyncStatus.value = await peer.discardPendingConfig()
    discardPendingConfirming.value = false
  } catch {
    error.value = t('settings.peer.errors.pendingDiscard')
  } finally {
    configSyncAction.value = ''
  }
}

async function writeClipboard(value: string): Promise<boolean> {
  const nativeClipboard = platform.system.setClipboardText
    ? { writeText: (text: string) => platform.system.setClipboardText!(text) }
    : undefined
  return copyTextToClipboard(value, nativeClipboard)
}

function onBundleInput(): void {
  preview.value = null
  previewedBundle.value = ''
  error.value = ''
}

async function startQuickTunnel(): Promise<void> {
  const start = platform.peer?.startQuickTunnel
  if (!start || tunnelBusy.value) return
  error.value = ''
  routeCopied.value = false
  copiedInvitationID.value = ''
  tunnelBusy.value = true
  try {
    tunnelStatus.value = await start()
  } catch {
    error.value = t('settings.peer.errors.tunnelStart')
  } finally {
    tunnelBusy.value = false
  }
}

async function stopQuickTunnel(): Promise<void> {
  const stop = platform.peer?.stopQuickTunnel
  if (!stop || tunnelBusy.value) return
  error.value = ''
  routeCopied.value = false
  copiedInvitationID.value = ''
  tunnelBusy.value = true
  try {
    await stop()
    tunnelStatus.value = { running: false, starting: false }
  } catch {
    error.value = t('settings.peer.errors.tunnelStop')
  } finally {
    tunnelBusy.value = false
  }
}

async function copyMemberRoute(): Promise<void> {
  const createBundle = platform.peer?.createConnectionBundle
  if (!createBundle || !hasPublishedMemberRoute.value || routeCopying.value || invitationActionID.value) return
  error.value = ''
  routeCopied.value = false
  routeCopying.value = true
  try {
    const bundle = await createBundle('')
    if (!await writeClipboard(bundle)) throw new Error('clipboard unavailable')
    routeCopied.value = true
  } catch {
    error.value = t('settings.peer.errors.routeCopy')
  } finally {
    routeCopying.value = false
  }
}

async function importMemberRoute(): Promise<void> {
  const peer = platform.peer
  const raw = routeBundleInput.value.trim()
  if (!peer || !raw || routeImporting.value) return
  error.value = ''
  routeImportResult.value = ''
  routeImporting.value = true
  try {
    const result = await peer.importConnectionBundle(raw)
    routeBundleInput.value = ''
    routeImportResult.value = result.quick_tunnel ? 'quick_tunnel' : result.manual_lan ? 'manual_lan' : 'no_route'
  } catch {
    error.value = t('settings.peer.errors.routeImport')
  } finally {
    routeImporting.value = false
  }
}

async function createPeerSpace(): Promise<void> {
  const peer = platform.peer
  if (!peer || creatingSpace.value) return
  error.value = ''
  creatingSpace.value = true
  try {
    status.value = await peer.createSpace()
    bundleInput.value = ''
    previewedBundle.value = ''
    preview.value = null
    await loadConfiguredPeerData()
  } catch {
    error.value = t('settings.peer.errors.createSpace')
  } finally {
    creatingSpace.value = false
  }
}

function parseSessionScope(value: string): string[] {
  const seen = new Set<string>()
  return value.split(/[\s,]+/).filter((sessionID) => {
    if (!sessionID || seen.has(sessionID)) return false
    seen.add(sessionID)
    return true
  })
}

async function refreshInvitationState(): Promise<void> {
  const peer = platform.peer
  if (!peer) return
  invitationsLoading.value = true
  try {
    const [nextStatus, nextInvitations] = await Promise.all([
      peer.status(),
      peer.listInvitations(),
    ])
    status.value = nextStatus
    invitations.value = nextInvitations
  } catch {
    error.value = t('settings.peer.errors.invitationLoad')
  } finally {
    invitationsLoading.value = false
  }
}

async function createInvitationBatch(): Promise<void> {
  const peer = platform.peer
  if (!peer || invitationCreating.value || invitationActionID.value) return
  error.value = ''
  copiedInvitationID.value = ''
  invitationCreating.value = true
  const count = Math.min(100, Math.max(1, Math.trunc(Number(inviteCount.value) || 1)))
  inviteCount.value = count
  try {
    try {
      await peer.createInvitations({
        count,
        valid_for_hours: Number(inviteValidFor.value),
        permission: invitePermission.value,
        allowed_session_ids: parseSessionScope(inviteSessionScope.value),
        can_invite: inviteCanInvite.value,
        can_sync_secrets: inviteCanSyncSecrets.value,
      })
    } catch {
      error.value = t('settings.peer.errors.invitationCreate')
      return
    }
    await refreshInvitationState()
  } finally {
    invitationCreating.value = false
  }
}

function invitationState(invitation: PeerInvitation): 'open' | 'used' | 'revoked' | 'expired' {
  if (invitation.revoked_at) return 'revoked'
  if (invitation.consumed_at) return 'used'
  if (invitation.expires_at <= Date.now() / 1000) return 'expired'
  return 'open'
}

async function copyInvitationRoute(invitation: PeerInvitation): Promise<void> {
  const createBundle = platform.peer?.createConnectionBundle
  if (
    !createBundle
    || !hasPublishedBootstrapRoute.value
    || tunnelBusy.value
    || !invitation.token
    || invitationState(invitation) !== 'open'
    || invitationActionID.value
  ) return
  error.value = ''
  copiedInvitationID.value = ''
  invitationActionID.value = invitation.invite_id
  try {
    const bundle = await createBundle(invitation.token)
    if (!await writeClipboard(bundle)) throw new Error('clipboard unavailable')
    copiedInvitationID.value = invitation.invite_id
  } catch {
    error.value = t('settings.peer.errors.invitationCopy')
  } finally {
    invitationActionID.value = ''
  }
}

async function revokeInvitation(invitation: PeerInvitation): Promise<void> {
  const peer = platform.peer
  if (!peer || invitationState(invitation) !== 'open' || invitationActionID.value) return
  error.value = ''
  copiedInvitationID.value = ''
  invitationActionID.value = invitation.invite_id
  try {
    try {
      await peer.revokeInvitation(invitation.invite_id)
    } catch {
      error.value = t('settings.peer.errors.invitationRevoke')
      return
    }
    await refreshInvitationState()
  } finally {
    invitationActionID.value = ''
  }
}

async function revokeMember(member: PeerMember): Promise<void> {
  const peer = platform.peer
  if (!peer || !member.can_revoke || memberActionID.value) return
  error.value = ''
  memberActionID.value = member.peer_id
  try {
    await peer.revokeMember(member.peer_id)
    const [nextMembers, nextSyncStatus] = await Promise.all([
      peer.listMembers(),
      peer.configSyncStatus(),
    ])
    members.value = nextMembers
    configSyncStatus.value = nextSyncStatus
  } catch {
    error.value = t('settings.peer.errors.memberRevoke')
  } finally {
    memberActionID.value = ''
  }
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
    await loadConfiguredPeerData()
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

      <section class="peer-section" data-testid="peer-rendezvous-section">
        <div class="section-heading">
          <RadioTower :size="17" aria-hidden="true" />
          <div>
            <h3>{{ t('settings.peer.rendezvous.title') }}</h3>
            <p class="hint">{{ t('settings.peer.rendezvous.hint') }}</p>
          </div>
        </div>
        <div class="rendezvous-form-grid">
          <div class="form-field" data-testid="peer-rendezvous-mode">
            <label class="field-label">{{ t('settings.peer.rendezvous.modeLabel') }}</label>
            <SelectDropdown
              v-model="rendezvousMode"
              :options="rendezvousModeOptions"
              :disabled="Boolean(rendezvousBusy)"
              :aria-label="t('settings.peer.rendezvous.modeLabel')"
            />
          </div>
          <div class="form-field" data-testid="peer-rendezvous-stun-mode">
            <label class="field-label">{{ t('settings.peer.rendezvous.stunLabel') }}</label>
            <SelectDropdown
              v-model="rendezvousSTUNMode"
              :options="rendezvousSTUNOptions"
              :disabled="Boolean(rendezvousBusy)"
              :aria-label="t('settings.peer.rendezvous.stunLabel')"
            />
          </div>
        </div>
        <div v-if="rendezvousMode === 'custom'" class="form-field">
          <label class="field-label" for="peer-rendezvous-url">{{ t('settings.peer.rendezvous.customURL') }}</label>
          <input
            id="peer-rendezvous-url"
            v-model="rendezvousURL"
            data-testid="peer-rendezvous-url"
            type="url"
            placeholder="https://rendezvous.example.com"
            :disabled="Boolean(rendezvousBusy)"
            autocomplete="off"
            spellcheck="false"
          />
          <p class="hint">{{ t('settings.peer.rendezvous.customURLHint') }}</p>
        </div>
        <div v-if="rendezvousSTUNMode === 'custom'" class="form-field">
          <label class="field-label" for="peer-rendezvous-stun-urls">{{ t('settings.peer.rendezvous.customSTUN') }}</label>
          <textarea
            id="peer-rendezvous-stun-urls"
            v-model="rendezvousSTUNURLs"
            data-testid="peer-rendezvous-stun-urls"
            rows="2"
            placeholder="stun:stun.example.com:3478"
            :disabled="Boolean(rendezvousBusy)"
            autocomplete="off"
            spellcheck="false"
          />
        </div>
        <div class="actions rendezvous-actions">
          <button
            type="button"
            class="primary-action"
            data-testid="peer-rendezvous-save"
            :disabled="Boolean(rendezvousBusy)"
            @click="saveRendezvousConfig"
          >
            <Save :size="15" aria-hidden="true" />
            {{ rendezvousBusy === 'save' ? t('settings.peer.rendezvous.saving') : t('settings.peer.rendezvous.save') }}
          </button>
          <button
            v-if="rendezvousMode !== 'disabled'"
            type="button"
            class="icon-action"
            data-testid="peer-rendezvous-reconnect"
            :disabled="Boolean(rendezvousBusy)"
            :aria-label="t('settings.peer.rendezvous.reconnect')"
            :title="t('settings.peer.rendezvous.reconnect')"
            @click="reconnectRendezvous"
          >
            <RefreshCw :size="15" :class="{ spinning: rendezvousBusy === 'reconnect' }" aria-hidden="true" />
          </button>
        </div>
        <div class="tunnel-status-row" data-testid="peer-rendezvous-status">
          <span
            class="status-dot"
            :class="{
              active: rendezvousStatus?.state === 'online',
              starting: rendezvousStatus?.state === 'connecting' || rendezvousStatus?.state === 'waiting',
              failed: rendezvousStatus?.state === 'error',
            }"
            aria-hidden="true"
          />
          <span>{{ rendezvousStateLabel(rendezvousStatus?.state) }}</span>
        </div>
        <code v-if="rendezvousStatus?.url" class="route-url published-route">{{ rendezvousStatus.url }}</code>
        <div v-if="rendezvousMode !== 'disabled'" class="sync-metrics" data-testid="peer-rendezvous-metrics">
          <div>
            <span>{{ t('settings.peer.rendezvous.registrationLatency') }}</span>
            <strong>{{ rendezvousStatus?.registration_ms ? `${rendezvousStatus.registration_ms} ms` : t('common.unknown') }}</strong>
          </div>
          <div>
            <span>{{ t('settings.peer.rendezvous.lastRegistration') }}</span>
            <strong>{{ formatTime(rendezvousStatus?.last_registered_at) }}</strong>
          </div>
          <div>
            <span>{{ t('settings.peer.rendezvous.reachablePeers') }}</span>
            <strong>{{ rendezvousStatus?.reachable_peers ?? 0 }}</strong>
          </div>
        </div>
        <p v-if="rendezvousStatus?.last_error_code" class="inline-error">
          {{ rendezvousErrorLabel(rendezvousStatus.last_error_code) }}
        </p>
        <p class="hint privacy-note">{{ t('settings.peer.rendezvous.privacy') }}</p>
      </section>

      <section v-if="hasLANHost" class="peer-section" data-testid="peer-lan-section">
        <div class="section-heading">
          <Cable :size="17" aria-hidden="true" />
          <div>
            <h3>{{ t('settings.peer.lan.title') }}</h3>
            <p class="hint">{{ t('settings.peer.lan.hint') }}</p>
          </div>
        </div>
        <label class="checkbox-row">
          <input v-model="lanEnabled" data-testid="peer-lan-enabled" type="checkbox" :disabled="lanBusy" />
          <span>
            <strong>{{ t('settings.peer.lan.enable') }}</strong>
            <small>{{ t('settings.peer.lan.enableHint') }}</small>
          </span>
        </label>
        <label class="checkbox-row">
          <input v-model="lanAutoDiscovery" data-testid="peer-lan-auto-discovery" type="checkbox" :disabled="lanBusy || !lanEnabled" />
          <span>
            <strong>{{ t('settings.peer.lan.autoDiscovery') }}</strong>
            <small>{{ t('settings.peer.lan.autoDiscoveryHint') }}</small>
          </span>
        </label>
        <div class="rendezvous-form-grid">
          <div class="form-field">
            <label class="field-label" for="peer-lan-advertise-host">{{ t('settings.peer.lan.advertiseHost') }}</label>
            <input
              id="peer-lan-advertise-host"
              v-model="lanAdvertiseHost"
              data-testid="peer-lan-advertise-host"
              type="text"
              inputmode="url"
              autocomplete="off"
              spellcheck="false"
              :placeholder="t('settings.peer.lan.advertiseHostPlaceholder')"
              :disabled="lanBusy"
            />
          </div>
          <div class="form-field">
            <label class="field-label" for="peer-lan-port">{{ t('settings.peer.lan.port') }}</label>
            <input id="peer-lan-port" v-model="lanPort" data-testid="peer-lan-port" type="number" min="1" max="65535" :disabled="lanBusy" />
          </div>
        </div>
        <div class="actions rendezvous-actions">
          <button type="button" class="primary-action" data-testid="peer-lan-save" :disabled="lanBusy" @click="persistLANConfig()">
            <Save :size="14" aria-hidden="true" />
            {{ lanBusy ? t('settings.peer.lan.saving') : t('settings.peer.lan.save') }}
          </button>
          <div class="tunnel-status-row" data-testid="peer-lan-status">
            <span class="status-dot" :class="{ active: lanRunning, failed: lanEnabled && Boolean(lanLastError) }" aria-hidden="true" />
            <span>{{ lanRunning ? t('settings.peer.lan.running') : lanEnabled ? t('settings.peer.lan.stopped') : t('common.disabled') }}</span>
          </div>
        </div>
        <code v-if="lanListenAddress" class="route-url published-route">{{ lanListenAddress }}</code>
        <p v-if="lanLastError" class="inline-error">{{ t('settings.peer.lan.error') }}</p>
        <div v-if="lanEnabled && lanAutoDiscovery" class="tunnel-status-row" data-testid="peer-lan-discovery-status">
          <span class="status-dot" :class="{ active: lanDiscoveryRunning, failed: Boolean(lanDiscoveryLastError) }" aria-hidden="true" />
          <span>{{ lanDiscoveryRunning ? t('settings.peer.lan.discoveryRunning') : t('settings.peer.lan.discoveryStopped') }}</span>
        </div>
        <p v-if="lanDiscoveryLastError" class="inline-error">{{ t('settings.peer.lan.discoveryError') }}</p>
        <p class="hint privacy-note">{{ t('settings.peer.lan.privacy') }}</p>

        <div class="lan-route-editor">
          <h4>{{ t('settings.peer.lan.routesTitle') }}</h4>
          <div class="lan-route-grid">
            <div class="form-field">
              <label class="field-label" for="peer-lan-route-host">{{ t('settings.peer.lan.routeHost') }}</label>
              <input id="peer-lan-route-host" v-model="lanRouteHost" data-testid="peer-lan-route-host" type="text" autocomplete="off" spellcheck="false" :placeholder="t('settings.peer.lan.routeHostPlaceholder')" :disabled="lanBusy" />
            </div>
            <div class="form-field">
              <label class="field-label" for="peer-lan-route-port">{{ t('settings.peer.lan.port') }}</label>
              <input id="peer-lan-route-port" v-model="lanRoutePort" data-testid="peer-lan-route-port" type="number" min="1" max="65535" :disabled="lanBusy" />
            </div>
            <div class="form-field">
              <label class="field-label" for="peer-lan-route-fingerprint">{{ t('settings.peer.lan.fingerprint') }}</label>
              <input id="peer-lan-route-fingerprint" v-model="lanRouteFingerprint" data-testid="peer-lan-route-fingerprint" type="text" autocomplete="off" spellcheck="false" placeholder="SHA256:..." :disabled="lanBusy" />
            </div>
            <button
              type="button"
              class="icon-action lan-route-add"
              data-testid="peer-lan-route-add"
              :disabled="lanBusy || !lanRouteHost.trim() || !lanRouteFingerprint.trim()"
              :aria-label="t('settings.peer.lan.addRoute')"
              :title="t('settings.peer.lan.addRoute')"
              @click="addLANRoute"
            >
              <Plus :size="15" aria-hidden="true" />
            </button>
          </div>
          <div v-if="lanRoutes.length" class="lan-route-list">
            <div v-for="(route, index) in lanRoutes" :key="route.fingerprint" class="lan-route-row">
              <div>
                <code>{{ route.host }}:{{ route.port }}</code>
                <small>{{ route.fingerprint }}</small>
              </div>
              <button
                type="button"
                class="icon-action"
                data-testid="peer-lan-route-remove"
                :disabled="lanBusy"
                :aria-label="t('settings.peer.lan.removeRoute')"
                :title="t('settings.peer.lan.removeRoute')"
                @click="removeLANRoute(index)"
              >
                <Trash2 :size="14" aria-hidden="true" />
              </button>
            </div>
          </div>
          <p v-else class="hint">{{ t('settings.peer.lan.routesEmpty') }}</p>
        </div>
      </section>

      <section class="peer-section" data-testid="peer-config-sync">
        <div class="sync-heading">
          <div class="section-heading sync-title">
            <Database :size="17" aria-hidden="true" />
            <div>
              <h3>{{ t('settings.peer.sync.title') }}</h3>
              <p class="hint">{{ t('settings.peer.sync.hint') }}</p>
            </div>
          </div>
          <button
            type="button"
            class="icon-action"
            data-testid="peer-sync-now"
            :disabled="configSyncLoading || Boolean(configSyncAction)"
            :aria-label="t('settings.peer.sync.syncNow')"
            :title="t('settings.peer.sync.syncNow')"
            @click="syncConfigNow"
          >
            <Database :size="15" :class="{ spinning: configSyncAction === 'sync' }" aria-hidden="true" />
          </button>
          <button
            type="button"
            class="icon-action"
            data-testid="peer-sync-refresh"
            :disabled="configSyncLoading || Boolean(configSyncAction)"
            :aria-label="t('settings.peer.sync.refresh')"
            :title="t('settings.peer.sync.refresh')"
            @click="refreshConfigSyncStatus"
          >
            <RefreshCw :size="15" :class="{ spinning: configSyncLoading }" aria-hidden="true" />
          </button>
        </div>
        <p v-if="configSyncLoading && !configSyncStatus" class="hint">{{ t('common.loading') }}</p>
        <template v-else-if="configSyncStatus">
          <div
            class="sync-state"
            :class="{ pending: configSyncStatus.pending_operations > 0 || configSyncStatus.pending_import_records > 0 }"
            data-testid="peer-sync-state"
          >
            <span
              class="status-dot"
              :class="{ active: configSyncStatus.pending_operations === 0 && configSyncStatus.last_exchange_at }"
              aria-hidden="true"
            />
            <span v-if="configSyncStatus.pending_operations > 0">
              {{ t('settings.peer.sync.pending', { count: configSyncStatus.pending_operations }) }}
            </span>
            <span v-else-if="configSyncStatus.last_exchange_at">{{ t('settings.peer.sync.synced') }}</span>
            <span v-else>{{ t('settings.peer.sync.localOnly') }}</span>
          </div>
          <div class="sync-metrics">
            <div>
              <span>{{ t('settings.peer.sync.localChanges') }}</span>
              <strong>{{ configSyncStatus.local_operations }}</strong>
            </div>
            <div>
              <span>{{ t('settings.peer.sync.replicaDevices') }}</span>
              <strong>{{ configSyncStatus.replica_devices }}</strong>
            </div>
            <div>
              <span>{{ t('settings.peer.sync.activePeers') }}</span>
              <strong>{{ configSyncStatus.active_remote_members }}</strong>
            </div>
            <div>
              <span>{{ t('settings.peer.sync.lastExchange') }}</span>
              <strong>{{ formatTime(configSyncStatus.last_exchange_at) }}</strong>
            </div>
          </div>
          <p class="hint" data-testid="peer-sync-storage-note">
            {{ configSyncStatus.pending_operations > 0
              ? t('settings.peer.sync.pendingHint')
              : t('settings.peer.sync.storageHint') }}
          </p>
          <div
            v-if="configSyncStatus.pending_import_records > 0"
            class="pending-import"
            data-testid="peer-pending-import"
          >
            <div>
              <strong>{{ t('settings.peer.sync.pendingImportTitle', { count: configSyncStatus.pending_import_records }) }}</strong>
              <p class="hint">
                {{ t('settings.peer.sync.pendingImportHint', { time: formatTime(configSyncStatus.pending_import_captured_at) }) }}
              </p>
            </div>
            <div v-if="!discardPendingConfirming" class="actions">
              <button
                type="button"
                class="primary-action"
                data-testid="peer-pending-accept"
                :disabled="Boolean(configSyncAction)"
                @click="acceptPendingConfig"
              >
                <Check :size="15" aria-hidden="true" />
                {{ configSyncAction === 'accept' ? t('settings.peer.sync.accepting') : t('settings.peer.sync.accept') }}
              </button>
              <button
                type="button"
                class="secondary-action"
                data-testid="peer-pending-discard-open"
                :disabled="Boolean(configSyncAction)"
                @click="discardPendingConfirming = true"
              >
                <Trash2 :size="14" aria-hidden="true" />
                {{ t('settings.peer.sync.discard') }}
              </button>
            </div>
            <div v-else class="discard-confirm" data-testid="peer-pending-discard-confirm">
              <p>{{ t('settings.peer.sync.discardConfirm') }}</p>
              <div class="actions">
                <button
                  type="button"
                  class="danger-confirm"
                  data-testid="peer-pending-discard-confirm-button"
                  :disabled="Boolean(configSyncAction)"
                  @click="discardPendingConfig"
                >
                  <Trash2 :size="14" aria-hidden="true" />
                  {{ configSyncAction === 'discard' ? t('settings.peer.sync.discarding') : t('settings.peer.sync.confirmDiscard') }}
                </button>
                <button
                  type="button"
                  class="icon-action"
                  :disabled="Boolean(configSyncAction)"
                  :aria-label="t('common.cancel')"
                  :title="t('common.cancel')"
                  @click="discardPendingConfirming = false"
                >
                  <X :size="15" aria-hidden="true" />
                </button>
              </div>
            </div>
          </div>
        </template>
      </section>

      <PeerMembersSection
        :members="members"
        :loading="membersLoading"
        :busy-peer-id="memberActionID"
        @revoke="revokeMember"
      />

      <section class="peer-section" data-testid="peer-route-import-section">
        <div class="section-heading">
          <Download :size="17" aria-hidden="true" />
          <div>
            <h3>{{ t('settings.peer.routeImport.title') }}</h3>
            <p class="hint">{{ t('settings.peer.routeImport.hint') }}</p>
          </div>
        </div>
        <div class="form-field">
          <label class="field-label" for="peer-route-bundle">{{ t('settings.peer.routeImport.label') }}</label>
          <textarea
            id="peer-route-bundle"
            v-model="routeBundleInput"
            data-testid="peer-route-import-input"
            rows="3"
            :placeholder="t('settings.peer.routeImport.placeholder')"
            :disabled="routeImporting"
            autocomplete="off"
            spellcheck="false"
            @input="routeImportResult = ''"
          />
        </div>
        <div class="actions">
          <button
            type="button"
            class="primary-action"
            data-testid="peer-route-import-button"
            :disabled="routeImporting || !routeBundleInput.trim()"
            @click="importMemberRoute"
          >
            <Download :size="15" aria-hidden="true" />
            {{ routeImporting ? t('settings.peer.routeImport.importing') : t('settings.peer.routeImport.action') }}
          </button>
        </div>
        <p v-if="routeImportResult" class="success-note" data-testid="peer-route-import-success">
          {{ routeImportResult === 'quick_tunnel'
            ? t('settings.peer.routeImport.quickTunnelReady')
            : routeImportResult === 'manual_lan'
              ? t('settings.peer.routeImport.lanReady')
              : t('settings.peer.routeImport.routeRemoved') }}
        </p>
      </section>

      <section v-if="hasQuickTunnelHost" class="peer-section" data-testid="peer-tunnel-section">
        <div>
          <h3>{{ t('settings.peer.tunnel.title') }}</h3>
          <p class="hint">{{ t('settings.peer.tunnel.hint') }}</p>
        </div>
        <div class="tunnel-status-row" data-testid="peer-tunnel-status">
          <span
            class="status-dot"
            :class="{ active: tunnelStatus?.running, starting: tunnelStatus?.starting }"
            aria-hidden="true"
          />
          <span>
            {{ tunnelStatus?.running
              ? t('settings.peer.tunnel.running')
              : tunnelStatus?.starting
                ? t('settings.peer.tunnel.starting')
                : t('settings.peer.tunnel.stopped') }}
          </span>
        </div>
        <code
          v-if="tunnelStatus?.running && tunnelStatus.public_url"
          class="route-url published-route"
          data-testid="peer-tunnel-url"
        >{{ tunnelStatus.public_url }}</code>
        <div class="actions">
          <button
            v-if="!tunnelStatus?.running"
            type="button"
            class="primary-action"
            data-testid="peer-tunnel-start"
            :disabled="tunnelBusy"
            @click="startQuickTunnel"
          >
            <Play :size="15" aria-hidden="true" />
            {{ tunnelBusy ? t('settings.peer.tunnel.starting') : t('settings.peer.tunnel.start') }}
          </button>
          <button
            v-if="hasPublishedMemberRoute"
            type="button"
            class="primary-action"
            data-testid="peer-tunnel-copy-route"
            :disabled="routeCopying || tunnelBusy || Boolean(invitationActionID)"
            @click="copyMemberRoute"
          >
            <Copy :size="15" aria-hidden="true" />
            {{ routeCopied
              ? t('settings.peer.tunnel.copied')
              : routeCopying
                ? t('settings.peer.tunnel.copying')
                : t('settings.peer.tunnel.copyRoute') }}
          </button>
          <template v-if="tunnelStatus?.running">
            <button
              type="button"
              class="secondary-action"
              data-testid="peer-tunnel-stop"
              :disabled="tunnelBusy || routeCopying || Boolean(invitationActionID)"
              @click="stopQuickTunnel"
            >
              <Square :size="14" aria-hidden="true" />
              {{ tunnelBusy ? t('settings.peer.tunnel.stopping') : t('settings.peer.tunnel.stop') }}
            </button>
          </template>
        </div>
        <p v-if="tunnelStatus?.running" class="hint">{{ t('settings.peer.tunnel.shareHint') }}</p>
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

      <section class="peer-section" data-testid="peer-invitations-section">
        <div>
          <h3>{{ t('settings.peer.invitations.title') }}</h3>
          <p class="hint">{{ t('settings.peer.invitations.hint') }}</p>
        </div>
        <div class="invitation-form-grid">
          <div class="form-field compact-field">
            <label class="field-label" for="peer-invite-count">{{ t('settings.peer.invitations.count') }}</label>
            <input
              id="peer-invite-count"
              v-model.number="inviteCount"
              data-testid="peer-invite-count"
              type="number"
              min="1"
              max="100"
              step="1"
              :disabled="invitationCreating"
            />
          </div>
          <div class="form-field">
            <label class="field-label">{{ t('settings.peer.invitations.validity') }}</label>
            <SelectDropdown
              v-model="inviteValidFor"
              :options="validityOptions"
              :disabled="invitationCreating"
              :aria-label="t('settings.peer.invitations.validity')"
            />
          </div>
          <div class="form-field">
            <label class="field-label">{{ t('settings.peer.permissionLabel') }}</label>
            <SelectDropdown
              v-model="invitePermission"
              :options="permissionOptions"
              :disabled="invitationCreating"
              :aria-label="t('settings.peer.permissionLabel')"
            />
          </div>
        </div>
        <div class="form-field">
          <label class="field-label" for="peer-session-scope-input">{{ t('settings.peer.invitations.sessionScope') }}</label>
          <textarea
            id="peer-session-scope-input"
            v-model="inviteSessionScope"
            data-testid="peer-session-scope-input"
            rows="2"
            :placeholder="t('settings.peer.invitations.sessionScopePlaceholder')"
            :disabled="invitationCreating"
            autocomplete="off"
            spellcheck="false"
          />
          <p class="hint">{{ t('settings.peer.invitations.sessionScopeHint') }}</p>
        </div>
        <div class="capability-options">
          <label class="checkbox-row">
            <input
              v-model="inviteCanInvite"
              data-testid="peer-can-invite"
              type="checkbox"
              :disabled="invitationCreating"
            />
            <span>
              <strong>{{ t('settings.peer.canInvite') }}</strong>
              <small>{{ t('settings.peer.invitations.canInviteHint') }}</small>
            </span>
          </label>
          <label class="checkbox-row">
            <input
              v-model="inviteCanSyncSecrets"
              data-testid="peer-can-sync-secrets"
              type="checkbox"
              :disabled="invitationCreating"
            />
            <span>
              <strong>{{ t('settings.peer.canSyncSecrets') }}</strong>
              <small>{{ t('settings.peer.invitations.canSyncSecretsHint') }}</small>
            </span>
          </label>
        </div>
        <button
          type="button"
          class="primary-action create-invitations"
          data-testid="peer-create-invitations"
          :disabled="invitationCreating || Boolean(invitationActionID)"
          @click="createInvitationBatch"
        >
          <Plus :size="15" aria-hidden="true" />
          {{ invitationCreating
            ? t('settings.peer.invitations.creating')
            : t('settings.peer.invitations.create') }}
        </button>

        <p v-if="invitationsLoading" class="hint">{{ t('common.loading') }}</p>
        <p v-else-if="invitations.length === 0" class="hint">{{ t('settings.peer.invitations.empty') }}</p>
        <div v-else class="invitation-list">
          <div
            v-for="invitation in invitations"
            :key="invitation.invite_id"
            class="invitation-row"
            data-testid="peer-invitation-row"
          >
            <div class="invitation-info">
              <div class="invitation-title-row">
                <code>{{ invitation.invite_id }}</code>
                <span class="invitation-state" :class="`state-${invitationState(invitation)}`">
                  {{ t(`settings.peer.invitations.${invitationState(invitation)}`) }}
                </span>
              </div>
              <span class="hint">
                {{ t('settings.peer.invitations.expiresAt', { time: formatTime(invitation.expires_at) }) }}
              </span>
            </div>
            <div v-if="invitationState(invitation) === 'open'" class="invitation-actions">
              <button
                type="button"
                class="secondary-action"
                data-testid="peer-copy-invitation"
                :disabled="!hasPublishedBootstrapRoute || !invitation.token || tunnelBusy || Boolean(invitationActionID)"
                :title="!hasPublishedBootstrapRoute ? t('settings.peer.invitations.startTunnelFirst') : undefined"
                @click="copyInvitationRoute(invitation)"
              >
                <Copy :size="14" aria-hidden="true" />
                {{ copiedInvitationID === invitation.invite_id
                  ? t('settings.peer.invitations.copied')
                  : invitationActionID === invitation.invite_id
                    ? t('settings.peer.invitations.copying')
                    : t('settings.peer.invitations.copy') }}
              </button>
              <button
                type="button"
                class="icon-action danger-action"
                data-testid="peer-revoke-invitation"
                :disabled="Boolean(invitationActionID)"
                :aria-label="t('settings.peer.invitations.revoke')"
                :title="t('settings.peer.invitations.revoke')"
                @click="revokeInvitation(invitation)"
              >
                <Trash2 :size="15" aria-hidden="true" />
              </button>
            </div>
          </div>
        </div>
      </section>
    </template>

    <template v-else>
      <section class="peer-section create-space-section">
        <h3>{{ t('settings.peer.create.title') }}</h3>
        <p class="hint">{{ t('settings.peer.create.hint') }}</p>
        <button
          type="button"
          class="secondary-action create-space-action"
          data-testid="peer-create-space"
          :disabled="creatingSpace || joining"
          @click="createPeerSpace"
        >
          <Plus :size="15" aria-hidden="true" />
          {{ creatingSpace ? t('settings.peer.create.creating') : t('settings.peer.create.action') }}
        </button>
      </section>

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
.sync-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.sync-title {
  color: var(--fg);
}
.sync-state {
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--fg);
  font-size: 13px;
}
.sync-state.pending .status-dot {
  background: var(--warn);
}
.sync-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px 16px;
}
.sync-metrics > div {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}
.sync-metrics span {
  color: var(--fg-dim);
  font-size: 11px;
}
.sync-metrics strong {
  color: var(--fg);
  font-size: 13px;
  font-weight: 500;
  overflow-wrap: anywhere;
}
.pending-import {
  display: flex;
  flex-direction: column;
  gap: 9px;
  padding: 10px;
  border-left: 2px solid var(--warn);
  background: color-mix(in srgb, var(--warn) 7%, transparent);
}
.pending-import > div:first-child > strong {
  color: var(--fg);
  font-size: 12px;
}
.discard-confirm {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.discard-confirm p {
  margin: 0;
  color: var(--bad);
  font-size: 12px;
  line-height: 1.45;
}
.danger-confirm {
  border-color: var(--bad);
  background: var(--bad);
  color: #fff;
}
.spinning {
  animation: peer-spin 0.8s linear infinite;
}
@keyframes peer-spin {
  to { transform: rotate(360deg); }
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
textarea,
input[type="number"],
input[type="text"],
input[type="url"] {
  box-sizing: border-box;
  width: 100%;
  padding: 9px 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
  outline: none;
  background: var(--bg);
  color: var(--fg);
  font: 12px/1.5 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  overflow-wrap: anywhere;
}
textarea {
  min-height: 88px;
  resize: vertical;
}
input[type="number"],
input[type="text"],
input[type="url"] {
  height: 32px;
  padding-block: 6px;
}
textarea:focus,
input[type="number"]:focus,
input[type="text"]:focus,
input[type="url"]:focus {
  box-shadow: 0 0 0 2px var(--accent);
}
textarea:disabled,
input[type="number"]:disabled,
input[type="text"]:disabled,
input[type="url"]:disabled {
  opacity: 0.6;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.tunnel-status-row {
  display: flex;
  align-items: center;
  gap: 7px;
  min-height: 20px;
  color: var(--fg);
  font-size: 13px;
}
.status-dot {
  width: 7px;
  height: 7px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: var(--fg-dim);
}
.status-dot.active {
  background: var(--good);
}
.status-dot.starting {
  background: var(--warn);
}
.status-dot.failed {
  background: var(--bad);
}
.rendezvous-form-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 10px;
}
.rendezvous-actions {
  align-items: center;
}
.privacy-note {
  padding-left: 9px;
  border-left: 2px solid var(--border);
}
.lan-route-editor {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-top: 4px;
}
.lan-route-editor h4 {
  margin: 0;
  color: var(--fg);
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0;
}
.lan-route-grid {
  display: grid;
  grid-template-columns: minmax(120px, 1fr) 92px minmax(180px, 1.5fr) 32px;
  align-items: end;
  gap: 8px;
}
.lan-route-add {
  width: 32px;
  padding: 0;
}
.lan-route-list {
  display: flex;
  flex-direction: column;
  border-top: 1px solid var(--border);
}
.lan-route-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  min-width: 0;
  padding: 8px 0;
  border-bottom: 1px solid var(--border);
}
.lan-route-row > div {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}
.lan-route-row code,
.lan-route-row small {
  overflow-wrap: anywhere;
}
.lan-route-row code {
  color: var(--fg);
  font-size: 12px;
}
.lan-route-row small {
  color: var(--fg-dim);
  font: 10px/1.4 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
.published-route {
  display: block;
  padding: 7px 9px;
  border: 1px solid var(--border);
  border-radius: 5px;
  background: var(--bg);
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
.create-space-action,
.create-invitations {
  align-self: flex-start;
}
.invitation-form-grid {
  display: grid;
  grid-template-columns: 92px minmax(0, 1fr) minmax(0, 1fr);
  gap: 10px;
}
.form-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.peer-section .form-field textarea {
  min-height: 58px;
}
.capability-options {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 8px 16px;
}
.checkbox-row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  min-width: 0;
  color: var(--fg);
  font-size: 12px;
  cursor: pointer;
}
.checkbox-row input {
  flex: 0 0 auto;
  margin: 2px 0 0;
}
.checkbox-row span {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.checkbox-row strong {
  font-weight: 500;
}
.checkbox-row small {
  color: var(--fg-dim);
  font-size: 11px;
  line-height: 1.4;
}
.invitation-list {
  display: flex;
  flex-direction: column;
  border-top: 1px solid var(--border);
}
.invitation-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
  padding: 10px 0;
  border-bottom: 1px solid var(--border);
}
.invitation-row:last-child {
  border-bottom: 0;
}
.invitation-info {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
.invitation-title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.invitation-title-row code {
  min-width: 0;
  color: var(--fg);
  font-size: 12px;
  overflow-wrap: anywhere;
}
.invitation-state {
  flex: 0 0 auto;
  padding: 2px 6px;
  border: 1px solid var(--border);
  border-radius: 999px;
  color: var(--fg-dim);
  font-size: 10px;
  line-height: 1.3;
}
.invitation-state.state-open {
  border-color: color-mix(in srgb, var(--good) 45%, var(--border));
  color: var(--good);
}
.invitation-state.state-revoked,
.invitation-state.state-expired {
  color: var(--bad);
}
.invitation-actions {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 6px;
}
.icon-action {
  width: 32px;
  min-width: 32px;
  padding: 0;
  background: transparent;
  color: var(--fg-dim);
}
.danger-action:hover:not(:disabled) {
  border-color: var(--bad);
  color: var(--bad);
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
.success-note {
  margin: 0;
  color: var(--good);
  font-size: 12px;
  line-height: 1.5;
}
@media (max-width: 640px) {
  .detail-list {
    grid-template-columns: minmax(0, 1fr);
  }
  .invitation-form-grid,
  .rendezvous-form-grid,
  .lan-route-grid,
  .capability-options {
    grid-template-columns: minmax(0, 1fr);
  }
  .metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .sync-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .discard-confirm {
    align-items: flex-start;
    flex-direction: column;
  }
  .actions button,
  .join-action,
  .create-space-action,
  .create-invitations {
    width: 100%;
  }
  .invitation-row {
    align-items: stretch;
    flex-direction: column;
  }
  .invitation-actions .secondary-action {
    flex: 1 1 auto;
  }
}
</style>
