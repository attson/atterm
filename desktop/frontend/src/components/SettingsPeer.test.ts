import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../i18n/useI18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => {
      if (!params) return key
      return `${key}[${Object.entries(params).map(([name, value]) => `${name}=${value}`).join(',')}]`
    },
  }),
}))

const { fakePlatform, qrScanner } = vi.hoisted(() => ({
  fakePlatform: {
    caps: { capacitor: false, wailsBindings: true },
    system: {
      setClipboardText: vi.fn(),
    },
    peer: {
      status: vi.fn(),
      configSyncStatus: vi.fn(),
      acceptPendingConfig: vi.fn(),
      discardPendingConfig: vi.fn(),
      createSpace: vi.fn(),
      exportTrustBackup: vi.fn(),
      importTrustBackup: vi.fn(),
      previewConnectionBundle: vi.fn(),
      joinSpace: vi.fn(),
      importConnectionBundle: vi.fn(),
      createInvitations: vi.fn(),
      listInvitations: vi.fn(),
      listMembers: vi.fn(),
      revokeInvitation: vi.fn(),
      revokeMember: vi.fn(),
      getRendezvousConfig: vi.fn(),
      setRendezvousConfig: vi.fn(),
      getRendezvousStatus: vi.fn(),
      reconnectRendezvous: vi.fn(),
      syncConfigNow: vi.fn(),
      getQuickTunnelStatus: vi.fn(),
      installCloudflared: vi.fn(),
      startQuickTunnel: vi.fn(),
      stopQuickTunnel: vi.fn(),
      getLANConfig: vi.fn(),
      setLANConfig: vi.fn(),
      createConnectionBundle: vi.fn(),
      getQuickTunnelFallbackConsent: vi.fn(),
      setQuickTunnelFallbackConsent: vi.fn(),
    },
  },
  qrScanner: {
    requestPermissions: vi.fn(),
    scan: vi.fn(),
  },
}))

vi.mock('../platform', () => ({
  usePlatform: () => fakePlatform,
}))

vi.mock('../platform/qrScanner', () => ({
  QRScanner: qrScanner,
}))

import SettingsPeer from './SettingsPeer.vue'

const emptyStatus = {
  configured: false,
  open_invitations: 0,
  used_invitations: 0,
  revoked_invitations: 0,
  expired_invitations: 0,
}

const configuredStatus = {
  configured: true,
  peer_id: 'peer_local_123',
  space_id: 'space_family_456',
  genesis_hash: 'abcdef0123456789',
  created_at: 1_797_897_600,
  open_invitations: 3,
  used_invitations: 2,
  revoked_invitations: 1,
  expired_invitations: 4,
}

const pendingSyncStatus = {
  configured: true,
  local_operations: 7,
  pending_operations: 3,
  replica_devices: 2,
  active_remote_members: 1,
  acknowledging_peers: 1,
  last_exchange_at: 1_797_900_000,
  pending_import_records: 0,
}

const preview = {
  space_id: 'space_family_456',
  fingerprint: 'SHA256:ABCDEF0123456789',
  issuer_peer_id: 'peer_host_789',
  permission: 'control',
  allowed_session_ids: ['session-one', 'session-two'],
  can_invite: true,
  can_sync_secrets: false,
  invitation_expires_at: 1_798_156_800,
  bundle_expires_at: 1_797_984_000,
  route_kind: 'quick_tunnel',
  route_url: 'https://sample.trycloudflare.com',
}

beforeEach(() => {
  vi.clearAllMocks()
  fakePlatform.caps.capacitor = false
  fakePlatform.caps.wailsBindings = true
  fakePlatform.peer.status.mockResolvedValue(emptyStatus)
  fakePlatform.peer.configSyncStatus.mockResolvedValue(pendingSyncStatus)
  fakePlatform.peer.acceptPendingConfig.mockResolvedValue({ ...pendingSyncStatus, pending_import_records: 0 })
  fakePlatform.peer.discardPendingConfig.mockResolvedValue({ ...pendingSyncStatus, pending_import_records: 0 })
  fakePlatform.peer.createSpace.mockResolvedValue(configuredStatus)
  fakePlatform.peer.exportTrustBackup.mockResolvedValue('/tmp/atterm-peer-trust.json')
  fakePlatform.peer.importTrustBackup.mockResolvedValue(configuredStatus)
  fakePlatform.peer.previewConnectionBundle.mockResolvedValue(preview)
  fakePlatform.peer.joinSpace.mockResolvedValue(configuredStatus)
  fakePlatform.peer.importConnectionBundle.mockResolvedValue({
    issuer_peer_id: 'peer_host_789', quick_tunnel: true, expires_at: 1_797_984_000,
  })
  fakePlatform.peer.getQuickTunnelStatus.mockResolvedValue({
    running: false, starting: false, managed_supported: true, managed_installed: false, system_available: true,
    managed_version: '2026.10.0', managed_asset: 'cloudflared-test', managed_sha256: 'abc123',
  })
  fakePlatform.peer.installCloudflared.mockResolvedValue({
    running: false, starting: false, managed_supported: true, managed_installed: true, system_available: false,
    managed_version: '2026.10.0', managed_asset: 'cloudflared-test', managed_sha256: 'abc123',
  })
  fakePlatform.peer.startQuickTunnel.mockResolvedValue({
    running: true,
    starting: false,
    public_url: 'https://route.trycloudflare.com',
  })
  fakePlatform.peer.stopQuickTunnel.mockResolvedValue(undefined)
  fakePlatform.peer.getLANConfig.mockResolvedValue({
    lan_only: false, enabled: false, auto_discovery: false, advertise_host: '', port: 8484, routes: [], running: false, discovery_running: false,
  })
  fakePlatform.peer.setLANConfig.mockResolvedValue(undefined)
  fakePlatform.peer.createConnectionBundle.mockResolvedValue('atc1.member-route.signature')
  fakePlatform.peer.createInvitations.mockResolvedValue([])
  fakePlatform.peer.listInvitations.mockResolvedValue([])
  fakePlatform.peer.listMembers.mockResolvedValue([])
  fakePlatform.peer.revokeInvitation.mockResolvedValue(undefined)
  fakePlatform.peer.revokeMember.mockResolvedValue(undefined)
  fakePlatform.peer.getRendezvousConfig.mockResolvedValue({
    mode: 'disabled', url: '', websocket_url: '', health_url: '',
    stun_mode: 'default', stun_urls: ['stun:stun.cloudflare.com:3478'],
    turn_enabled: false, turn_urls: [], turn_username: '', turn_credential_configured: false,
  })
  fakePlatform.peer.setRendezvousConfig.mockResolvedValue(undefined)
  fakePlatform.peer.getRendezvousStatus.mockResolvedValue({
    mode: 'disabled', state: 'disabled', reachable_peers: 0,
  })
  fakePlatform.peer.reconnectRendezvous.mockResolvedValue({
    mode: 'official', state: 'connecting', url: 'https://rendezvous.atterm.dev', reachable_peers: 0,
  })
  fakePlatform.peer.syncConfigNow.mockResolvedValue(pendingSyncStatus)
  fakePlatform.peer.getQuickTunnelFallbackConsent.mockResolvedValue(false)
  fakePlatform.peer.setQuickTunnelFallbackConsent.mockResolvedValue(undefined)
  fakePlatform.system.setClipboardText.mockResolvedValue(undefined)
  qrScanner.requestPermissions.mockResolvedValue({ camera: 'granted' })
  qrScanner.scan.mockResolvedValue({ cancelled: false, rawValue: 'atc1.scanned-token' })
})

afterEach(() => {
  vi.restoreAllMocks()
})

async function mountReady() {
  const wrapper = mount(SettingsPeer)
  await flushPromises()
  return wrapper
}

describe('SettingsPeer', () => {
  it('persists explicit Quick Tunnel fallback consent per device', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    const wrapper = await mountReady()
    const checkbox = wrapper.get<HTMLInputElement>('[data-testid="peer-fallback-consent"]')
    expect(checkbox.element.checked).toBe(false)

    await checkbox.setValue(true)
    await flushPromises()

    expect(fakePlatform.peer.setQuickTunnelFallbackConsent).toHaveBeenCalledWith(true)
    expect(checkbox.element.checked).toBe(true)
  })

  it('previews pasted deep links and shows the authenticated trust details', async () => {
    const wrapper = await mountReady()
    const deepLink = 'https://app.example/connect#atc1.bundle-token'

    await wrapper.get('[data-testid="peer-bundle-input"]').setValue(deepLink)
    await wrapper.get('[data-testid="peer-preview-button"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.previewConnectionBundle).toHaveBeenCalledWith(deepLink)
    expect(wrapper.get('[data-testid="peer-preview"]').text()).toContain(preview.fingerprint)
    expect(wrapper.get('[data-testid="peer-preview"]').text()).toContain(preview.route_url)
    expect(wrapper.get('[data-testid="peer-session-scope"]').text()).toContain('session-one')
    expect(wrapper.get('[data-testid="peer-session-scope"]').text()).toContain('session-two')
    expect(wrapper.text()).toContain('settings.peer.permission.control')
    expect(wrapper.text()).toContain('settings.peer.yes')
    expect(wrapper.text()).toContain('settings.peer.no')
    expect(fakePlatform.peer.joinSpace).not.toHaveBeenCalled()
  })

  it('requires an explicit confirmation and binds it to the previewed fingerprint', async () => {
    const wrapper = await mountReady()
    await wrapper.get('[data-testid="peer-bundle-input"]').setValue('atc1.raw-token')
    await wrapper.get('[data-testid="peer-preview-button"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.joinSpace).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="peer-join-button"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.joinSpace).toHaveBeenCalledWith({
      connection_bundle: 'atc1.raw-token',
      expected_fingerprint: preview.fingerprint,
    })
    expect(wrapper.find('[data-testid="peer-bundle-input"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="peer-configured"]').text()).toContain(configuredStatus.peer_id)
    expect(wrapper.get('[data-testid="peer-configured"]').text()).toContain(`SHA256:${configuredStatus.genesis_hash}`)
  })

  it('shows all-session scope when the invitation does not restrict session ids', async () => {
    fakePlatform.peer.previewConnectionBundle.mockResolvedValue({ ...preview, allowed_session_ids: [] })
    const wrapper = await mountReady()
    await wrapper.get('[data-testid="peer-bundle-input"]').setValue('atc1.all-sessions')
    await wrapper.get('[data-testid="peer-preview-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="peer-session-scope"]').text()).toContain('settings.peer.scopeAll')
  })

  it('maps rejected and unavailable joins to safe messages without rendering the token', async () => {
    fakePlatform.peer.joinSpace.mockRejectedValue(new Error('Peer Space join rejected: dial tcp: connection refused atc1.secret'))
    const wrapper = await mountReady()
    await wrapper.get('[data-testid="peer-bundle-input"]').setValue('atc1.secret')
    await wrapper.get('[data-testid="peer-preview-button"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="peer-join-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="peer-error"]').text()).toBe('settings.peer.errors.hostUnavailable')
    expect(wrapper.text()).not.toContain('atc1.secret')
  })

  it('shows QR scanning only on Capacitor and previews the scanned bundle', async () => {
    const desktop = await mountReady()
    expect(desktop.find('[data-testid="peer-scan-button"]').exists()).toBe(false)
    desktop.unmount()

    fakePlatform.caps.capacitor = true
    const mobile = await mountReady()
    await mobile.get('[data-testid="peer-scan-button"]').trigger('click')
    await flushPromises()

    expect(qrScanner.requestPermissions).toHaveBeenCalledTimes(1)
    expect(qrScanner.scan).toHaveBeenCalledTimes(1)
    expect(fakePlatform.peer.previewConnectionBundle).toHaveBeenCalledWith('atc1.scanned-token')
    expect(mobile.find('[data-testid="peer-preview"]').exists()).toBe(true)
  })

  it('shows an existing Space without offering a replacement join form', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    const wrapper = await mountReady()

    expect(wrapper.get('[data-testid="peer-configured"]').text()).toContain(configuredStatus.space_id)
    expect(wrapper.find('[data-testid="peer-bundle-input"]').exists()).toBe(false)
  })

  it('exports encrypted trust only after matching recovery passphrases', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    const wrapper = await mountReady()

    await wrapper.get('[data-testid="peer-backup-passphrase"]').setValue('correct horse battery staple')
    await wrapper.get('[data-testid="peer-backup-confirmation"]').setValue('does not match this value')
    expect(wrapper.get('[data-testid="peer-backup-export"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="peer-backup-confirmation"]').setValue('correct horse battery staple')
    await wrapper.get('[data-testid="peer-backup-export"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.exportTrustBackup).toHaveBeenCalledWith('correct horse battery staple')
    expect(wrapper.get('[data-testid="peer-backup-success"]').text()).toContain('/tmp/atterm-peer-trust.json')
  })

  it('restores an encrypted trust package only on an unconfigured desktop', async () => {
    const wrapper = await mountReady()
    const input = wrapper.get('[data-testid="peer-backup-file"]')
    const file = new File(['encrypted-peer-backup'], 'peer-backup.json', { type: 'application/json' })
    Object.defineProperty(file, 'text', { configurable: true, value: vi.fn().mockResolvedValue('encrypted-peer-backup') })
    Object.defineProperty(input.element, 'files', { configurable: true, value: [file] })
    await input.trigger('change')
    await wrapper.get('[data-testid="peer-backup-import-passphrase"]').setValue('correct horse battery staple')
    await wrapper.get('[data-testid="peer-backup-import"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.importTrustBackup).toHaveBeenCalledWith('encrypted-peer-backup', 'correct horse battery staple')
    expect(wrapper.get('[data-testid="peer-configured"]').text()).toContain(configuredStatus.space_id)
    expect(wrapper.find('[data-testid="peer-backup-import"]').exists()).toBe(false)
  })

  it('does not expose identity recovery on platforms with non-exportable browser keys', async () => {
    fakePlatform.caps.wailsBindings = false
    const wrapper = await mountReady()
    expect(wrapper.find('[data-testid="peer-trust-backup"]').exists()).toBe(false)
  })

  it('imports a ticketless member route without re-entering the join flow', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    const wrapper = await mountReady()

    await wrapper.get('[data-testid="peer-route-import-input"]').setValue('atterm://peer/connect#atc1.member-route')
    await wrapper.get('[data-testid="peer-route-import-button"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.importConnectionBundle).toHaveBeenCalledWith('atterm://peer/connect#atc1.member-route')
    expect(fakePlatform.peer.joinSpace).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="peer-route-import-success"]').text()).toContain('settings.peer.routeImport.quickTunnelReady')
    expect(wrapper.get('[data-testid="peer-route-import-input"]').element).toHaveProperty('value', '')
  })

  it('creates a new Peer Space and loads host controls', async () => {
    const setIntervalSpy = vi.spyOn(globalThis, 'setInterval')
    const wrapper = await mountReady()

    await wrapper.get('[data-testid="peer-create-space"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.createSpace).toHaveBeenCalledOnce()
    expect(fakePlatform.peer.getQuickTunnelStatus).toHaveBeenCalledOnce()
    expect(fakePlatform.peer.listInvitations).toHaveBeenCalledOnce()
    expect(fakePlatform.peer.listMembers).toHaveBeenCalledOnce()
    expect(fakePlatform.peer.configSyncStatus).toHaveBeenCalledOnce()
    expect(setIntervalSpy).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-testid="peer-configured"]').text()).toContain(configuredStatus.space_id)
    wrapper.unmount()
  })

  it('shows durable pending sync without implying central cloud storage', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    const wrapper = await mountReady()

    expect(wrapper.get('[data-testid="peer-sync-state"]').text()).toContain('settings.peer.sync.pending[count=3]')
    expect(wrapper.get('[data-testid="peer-sync-storage-note"]').text()).toContain('settings.peer.sync.pendingHint')
    expect(wrapper.get('[data-testid="peer-config-sync"]').text()).toContain('7')
    expect(wrapper.get('[data-testid="peer-config-sync"]').text()).toContain('settings.peer.sync.lastExchange')

    fakePlatform.peer.configSyncStatus.mockResolvedValue({ ...pendingSyncStatus, pending_operations: 0 })
    await wrapper.get('[data-testid="peer-sync-refresh"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="peer-sync-state"]').text()).toContain('settings.peer.sync.synced')
    expect(fakePlatform.peer.configSyncStatus).toHaveBeenCalledTimes(2)
  })

  it('loads Rendezvous operational status and exposes only aggregate reachability', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    fakePlatform.peer.getRendezvousConfig.mockResolvedValue({
      mode: 'official', url: 'https://rendezvous.atterm.dev',
      websocket_url: 'wss://rendezvous.atterm.dev/v1/connect',
      health_url: 'https://rendezvous.atterm.dev/healthz',
      stun_mode: 'default', stun_urls: ['stun:stun.cloudflare.com:3478'],
      turn_enabled: false, turn_urls: [], turn_username: '', turn_credential_configured: false,
    })
    fakePlatform.peer.getRendezvousStatus.mockResolvedValue({
      mode: 'official', state: 'online', url: 'https://rendezvous.atterm.dev',
      last_registered_at: 1_797_900_000, registration_ms: 38, reachable_peers: 2,
    })

    const wrapper = await mountReady()

    expect(fakePlatform.peer.getRendezvousConfig).toHaveBeenCalledOnce()
    expect(fakePlatform.peer.getRendezvousStatus).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-testid="peer-rendezvous-status"]').text()).toContain('settings.peer.rendezvous.state.online')
    expect(wrapper.get('[data-testid="peer-rendezvous-metrics"]').text()).toContain('38 ms')
    expect(wrapper.get('[data-testid="peer-rendezvous-metrics"]').text()).toContain('2')
  })

  it('validates and saves a custom Rendezvous origin with custom STUN servers', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    const wrapper = await mountReady()

    const mode = wrapper.get('[data-testid="peer-rendezvous-mode"]')
    await mode.get('[data-testid="select-trigger"]').trigger('click')
    await mode.findAll('[data-testid="select-option"]')[2].trigger('click')
    await wrapper.get('[data-testid="peer-rendezvous-url"]').setValue('https://rv.example.com')
    const stun = wrapper.get('[data-testid="peer-rendezvous-stun-mode"]')
    await stun.get('[data-testid="select-trigger"]').trigger('click')
    await stun.findAll('[data-testid="select-option"]')[1].trigger('click')
    await wrapper.get('[data-testid="peer-rendezvous-stun-urls"]').setValue('stun:one.example.com:3478\nstuns:two.example.com:5349')
    await wrapper.get('[data-testid="peer-rendezvous-save"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.setRendezvousConfig).toHaveBeenCalledWith({
      mode: 'custom', url: 'https://rv.example.com', stun_mode: 'custom',
      stun_urls: ['stun:one.example.com:3478', 'stuns:two.example.com:5349'],
      turn_enabled: false, turn_urls: [], turn_username: '', turn_credential: '',
    })
  })

  it('configures TURN without reading back or overwriting a saved credential', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    fakePlatform.peer.getRendezvousConfig.mockResolvedValue({
      mode: 'official', url: 'https://rendezvous.atterm.dev', websocket_url: '', health_url: '',
      stun_mode: 'default', stun_urls: ['stun:stun.cloudflare.com:3478'],
      turn_enabled: true, turn_urls: ['turn:turn.example.com:3478?transport=tcp'],
      turn_username: 'turn-user', turn_credential_configured: true,
    })
    const wrapper = await mountReady()

    const credential = wrapper.get('[data-testid="peer-rendezvous-turn-credential"]')
    expect((credential.element as HTMLInputElement).value).toBe('')
    expect(credential.attributes('placeholder')).toBe('settings.peer.rendezvous.turn.keepCredential')
    expect(wrapper.get('[data-testid="peer-rendezvous-turn-privacy"]').text()).toContain('settings.peer.rendezvous.turn.privacy')

    await wrapper.get('[data-testid="peer-rendezvous-turn-username"]').setValue('updated-user')
    await wrapper.get('[data-testid="peer-rendezvous-save"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.setRendezvousConfig).toHaveBeenCalledWith(expect.objectContaining({
      turn_enabled: true,
      turn_urls: ['turn:turn.example.com:3478?transport=tcp'],
      turn_username: 'updated-user',
      turn_credential: '',
    }))
  })

  it('reconnects Rendezvous and requests config sync through authenticated peers', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    fakePlatform.peer.getRendezvousConfig.mockResolvedValue({
      mode: 'official', url: 'https://rendezvous.atterm.dev', websocket_url: '', health_url: '',
      stun_mode: 'default', stun_urls: ['stun:stun.cloudflare.com:3478'],
      turn_enabled: false, turn_urls: [], turn_username: '', turn_credential_configured: false,
    })
    fakePlatform.peer.getRendezvousStatus.mockResolvedValue({
      mode: 'official', state: 'error', url: 'https://rendezvous.atterm.dev',
      reachable_peers: 0, last_error_code: 'service_unavailable',
    })
    const wrapper = await mountReady()

    await wrapper.get('[data-testid="peer-rendezvous-reconnect"]').trigger('click')
    await flushPromises()
    expect(fakePlatform.peer.reconnectRendezvous).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-testid="peer-rendezvous-status"]').text()).toContain('settings.peer.rendezvous.state.connecting')

    await wrapper.get('[data-testid="peer-sync-now"]').trigger('click')
    await flushPromises()
    expect(fakePlatform.peer.syncConfigNow).toHaveBeenCalledOnce()
  })

  it('merges or explicitly discards preserved pre-join settings', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    fakePlatform.peer.configSyncStatus.mockResolvedValue({
      ...pendingSyncStatus,
      pending_import_records: 4,
      pending_import_captured_at: 1_797_899_000,
    })
    const wrapper = await mountReady()

    expect(wrapper.get('[data-testid="peer-pending-import"]').text()).toContain(
      'settings.peer.sync.pendingImportTitle[count=4]',
    )
    await wrapper.get('[data-testid="peer-pending-discard-open"]').trigger('click')
    expect(wrapper.find('[data-testid="peer-pending-discard-confirm"]').exists()).toBe(true)
    expect(fakePlatform.peer.discardPendingConfig).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="peer-pending-discard-confirm-button"]').trigger('click')
    await flushPromises()
    expect(fakePlatform.peer.discardPendingConfig).toHaveBeenCalledOnce()
    expect(wrapper.find('[data-testid="peer-pending-import"]').exists()).toBe(false)

    fakePlatform.peer.configSyncStatus.mockResolvedValue({ ...pendingSyncStatus, pending_import_records: 2 })
    fakePlatform.peer.acceptPendingConfig.mockResolvedValue({ ...pendingSyncStatus, pending_import_records: 0 })
    await wrapper.get('[data-testid="peer-sync-refresh"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="peer-pending-accept"]').trigger('click')
    await flushPromises()
    expect(fakePlatform.peer.acceptPendingConfig).toHaveBeenCalledOnce()
    expect(wrapper.find('[data-testid="peer-pending-import"]').exists()).toBe(false)
  })

  it('lists Peer members separately and requires confirmation before revocation', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    const local = {
      peer_id: 'peer_local_123', grant_serial: 'grant-local', issuer_peer_id: 'peer_local_123',
      permission: 'full', allowed_session_ids: [], can_invite: true, can_sync_secrets: true,
      issued_at: 1_797_897_600, status: 'active', local: true, can_revoke: false,
    }
    const remote = {
      peer_id: 'peer_remote_789', grant_serial: 'grant-remote', issuer_peer_id: 'peer_local_123',
      permission: 'control', allowed_session_ids: ['session-one'], can_invite: false, can_sync_secrets: false,
      issued_at: 1_797_897_700, expires_at: 1_898_156_800, status: 'active', local: false, can_revoke: true,
      last_exchange_at: 1_797_900_000,
    }
    fakePlatform.peer.listMembers
      .mockResolvedValueOnce([local, remote])
      .mockResolvedValueOnce([local, { ...remote, status: 'revoked', revoked_at: 1_797_900_000, can_revoke: false }])
    const wrapper = await mountReady()

    expect(wrapper.findAll('[data-testid="peer-member-row"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('peer_remote_789')
    expect(wrapper.text()).toContain('settings.peer.permission.control')
    expect(wrapper.text()).toContain('settings.peer.members.lastDirectExchange')
    expect(fakePlatform.peer.revokeMember).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="peer-member-revoke-open"]').trigger('click')
    expect(wrapper.find('[data-testid="peer-member-revoke-confirm"]').exists()).toBe(true)
    expect(fakePlatform.peer.revokeMember).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="peer-member-revoke-confirm-button"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.revokeMember).toHaveBeenCalledWith('peer_remote_789')
    expect(wrapper.text()).toContain('settings.peer.members.status.revoked')
    expect(wrapper.find('[data-testid="peer-member-revoke-open"]').exists()).toBe(false)
  })

  it('pre-signs invitation batches with the selected constraints', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    const created = [
      { invite_id: 'invite-1', batch_id: 'batch-1', token: 'atp1.one', expires_at: 1_798_156_800 },
      { invite_id: 'invite-2', batch_id: 'batch-1', token: 'atp1.two', expires_at: 1_798_156_800 },
    ]
    fakePlatform.peer.createInvitations.mockResolvedValue(created)
    fakePlatform.peer.listInvitations
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce(created)
    fakePlatform.peer.status
      .mockResolvedValueOnce(configuredStatus)
      .mockResolvedValueOnce({ ...configuredStatus, open_invitations: 2 })
    const wrapper = await mountReady()

    await wrapper.get('[data-testid="peer-invite-count"]').setValue('2')
    await wrapper.get('[data-testid="peer-session-scope-input"]').setValue('session-a, session-b\nsession-a')
    await wrapper.get('[data-testid="peer-can-invite"]').setValue(true)
    await wrapper.get('[data-testid="peer-create-invitations"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.createInvitations).toHaveBeenCalledWith({
      count: 2,
      valid_for_hours: 24,
      permission: 'control',
      allowed_session_ids: ['session-a', 'session-b'],
      can_invite: true,
      can_sync_secrets: false,
    })
    expect(wrapper.findAll('[data-testid="peer-invitation-row"]')).toHaveLength(2)
    expect(wrapper.text()).not.toContain('atp1.one')
  })

  it('reports a refresh failure separately after invitations were created', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    fakePlatform.peer.listInvitations
      .mockResolvedValueOnce([])
      .mockRejectedValueOnce(new Error('store unavailable'))
    const wrapper = await mountReady()

    await wrapper.get('[data-testid="peer-create-invitations"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.createInvitations).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-testid="peer-error"]').text()).toBe('settings.peer.errors.invitationLoad')
  })

  it('copies an open first-join route and revokes the invitation', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    const invitation = {
      invite_id: 'invite-1',
      batch_id: 'batch-1',
      token: 'atp1.secret-ticket',
      expires_at: 1_898_156_800,
    }
    fakePlatform.peer.getQuickTunnelStatus.mockResolvedValue({
      running: true,
      starting: false,
      public_url: 'https://route.trycloudflare.com',
    })
    fakePlatform.peer.listInvitations
      .mockResolvedValueOnce([invitation])
      .mockResolvedValueOnce([{ ...invitation, token: '', revoked_at: 1_797_900_000 }])
    fakePlatform.peer.createConnectionBundle.mockResolvedValue('atc1.first-join.signature')
    const wrapper = await mountReady()

    await wrapper.get('[data-testid="peer-copy-invitation"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.createConnectionBundle).toHaveBeenCalledWith('atp1.secret-ticket')
    expect(fakePlatform.system.setClipboardText).toHaveBeenCalledWith('atc1.first-join.signature')
    expect(wrapper.text()).not.toContain('atp1.secret-ticket')

    await wrapper.get('[data-testid="peer-revoke-invitation"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.revokeInvitation).toHaveBeenCalledWith('invite-1')
    expect(wrapper.find('[data-testid="peer-copy-invitation"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="peer-invitation-row"]').text()).toContain('settings.peer.invitations.revoked')
  })

  it('starts Quick Tunnel and copies a ticketless member reconnect bundle', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    const wrapper = await mountReady()

    expect(fakePlatform.peer.getQuickTunnelStatus).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-testid="peer-tunnel-status"]').text()).toContain('settings.peer.tunnel.stopped')

    await wrapper.get('[data-testid="peer-tunnel-start"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.startQuickTunnel).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-testid="peer-tunnel-status"]').text()).toContain('settings.peer.tunnel.running')
    expect(wrapper.get('[data-testid="peer-tunnel-url"]').text()).toContain('https://route.trycloudflare.com')

    await wrapper.get('[data-testid="peer-tunnel-copy-route"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.createConnectionBundle).toHaveBeenCalledWith('')
    expect(fakePlatform.system.setClipboardText).toHaveBeenCalledWith('atc1.member-route.signature')
    expect(wrapper.get('[data-testid="peer-tunnel-copy-route"]').text()).toContain('settings.peer.tunnel.copied')
  })

  it('requires explicit confirmation before installing pinned cloudflared', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    fakePlatform.peer.getQuickTunnelStatus.mockResolvedValue({
      running: false, starting: false, managed_supported: true, managed_installed: false, system_available: false,
      managed_version: '2026.10.0', managed_asset: 'cloudflared-linux-amd64', managed_sha256: 'pinned-sha',
    })
    const wrapper = await mountReady()

    await wrapper.get('[data-testid="peer-tunnel-start"]').trigger('click')
    expect(fakePlatform.peer.startQuickTunnel).not.toHaveBeenCalled()
    expect(fakePlatform.peer.installCloudflared).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="peer-tunnel-install-confirm"]').text()).toContain('version=2026.10.0')
    expect(wrapper.get('[data-testid="peer-tunnel-install-confirm"]').text()).toContain('sha256=pinned-sha')

    await wrapper.get('[data-testid="peer-tunnel-install"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.installCloudflared).toHaveBeenCalledOnce()
    expect(fakePlatform.peer.startQuickTunnel).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="peer-tunnel-managed-installed"]').text()).toContain('version=2026.10.0')
    expect(wrapper.find('[data-testid="peer-tunnel-install-confirm"]').exists()).toBe(false)
  })

  it('keeps first-join invitation bundles disabled when only Rendezvous is online', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    fakePlatform.peer.getRendezvousStatus.mockResolvedValue({
      mode: 'official', state: 'online', url: 'https://rendezvous.atterm.dev', reachable_peers: 1,
    })
    fakePlatform.peer.listInvitations.mockResolvedValue([{
      invite_id: 'invite-1', batch_id: 'batch-1', token: 'atp1.secret-ticket', expires_at: 1_898_156_800,
    }])
    const wrapper = await mountReady()
    const copy = wrapper.get('[data-testid="peer-copy-invitation"]')

    expect(copy.attributes('disabled')).toBeDefined()
    expect(copy.attributes('title')).toBe('settings.peer.invitations.startTunnelFirst')
    await copy.trigger('click')
    await flushPromises()
    expect(fakePlatform.peer.createConnectionBundle).not.toHaveBeenCalled()
  })

  it('copies a first-join invitation when Manual LAN is the published bootstrap route', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    fakePlatform.peer.getLANConfig.mockResolvedValue({
      enabled: true,
      auto_discovery: false,
      advertise_host: '192.168.1.24',
      port: 8484,
      routes: [],
      running: true,
      discovery_running: false,
      listen_address: '0.0.0.0:8484',
    })
    fakePlatform.peer.listInvitations.mockResolvedValue([{
      invite_id: 'invite-lan', batch_id: 'batch-lan', token: 'atp1.lan-ticket', expires_at: 1_898_156_800,
    }])
    fakePlatform.peer.createConnectionBundle.mockResolvedValue('atc1.lan-first-join.signature')
    const wrapper = await mountReady()
    const copy = wrapper.get('[data-testid="peer-copy-invitation"]')

    expect(copy.attributes('disabled')).toBeUndefined()
    await copy.trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.createConnectionBundle).toHaveBeenCalledWith('atp1.lan-ticket')
    expect(fakePlatform.system.setClipboardText).toHaveBeenCalledWith('atc1.lan-first-join.signature')
  })

  it('saves the Manual LAN listener and adds then removes a fingerprint route', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    const disabled = { lan_only: false, enabled: false, auto_discovery: false, advertise_host: '', port: 8484, routes: [], running: false, discovery_running: false }
    const enabled = {
      enabled: true,
      auto_discovery: true,
      advertise_host: '192.168.1.24',
      port: 9444,
      routes: [],
      running: true,
      discovery_running: true,
      listen_address: '0.0.0.0:9444',
    }
    const withRoute = {
      ...enabled,
      routes: [{ host: '192.168.1.25', port: 8484, fingerprint: 'SHA256:remote-peer' }],
    }
    fakePlatform.peer.getLANConfig
      .mockResolvedValueOnce(disabled)
      .mockResolvedValueOnce(enabled)
      .mockResolvedValueOnce(withRoute)
      .mockResolvedValueOnce(enabled)
    const wrapper = await mountReady()

    expect(wrapper.get('[data-testid="peer-lan-advertise-host"]').attributes('placeholder')).toBe('settings.peer.lan.advertiseHostPlaceholder')
    expect(wrapper.get('[data-testid="peer-lan-route-host"]').attributes('placeholder')).toBe('settings.peer.lan.routeHostPlaceholder')

    await wrapper.get('[data-testid="peer-lan-enabled"]').setValue(true)
    await wrapper.get('[data-testid="peer-lan-auto-discovery"]').setValue(true)
    await wrapper.get('[data-testid="peer-lan-advertise-host"]').setValue('192.168.1.24')
    await wrapper.get('[data-testid="peer-lan-port"]').setValue('9444')
    await wrapper.get('[data-testid="peer-lan-save"]').trigger('click')
    await flushPromises()
    expect(fakePlatform.peer.setLANConfig).toHaveBeenLastCalledWith({
      lan_only: false, enabled: true, auto_discovery: true, advertise_host: '192.168.1.24', port: 9444, routes: [],
    })
    expect(wrapper.get('[data-testid="peer-lan-discovery-status"]').text()).toContain('settings.peer.lan.discoveryRunning')

    await wrapper.get('[data-testid="peer-lan-route-host"]').setValue('192.168.1.25')
    await wrapper.get('[data-testid="peer-lan-route-port"]').setValue('8484')
    await wrapper.get('[data-testid="peer-lan-route-fingerprint"]').setValue('SHA256:remote-peer')
    await wrapper.get('[data-testid="peer-lan-route-add"]').trigger('click')
    await flushPromises()
    expect(fakePlatform.peer.setLANConfig).toHaveBeenLastCalledWith({
      enabled: true,
      lan_only: false,
      auto_discovery: true,
      advertise_host: '192.168.1.24',
      port: 9444,
      routes: [{ host: '192.168.1.25', port: 8484, fingerprint: 'SHA256:remote-peer' }],
    })

    await wrapper.get('[data-testid="peer-lan-route-remove"]').trigger('click')
    await flushPromises()
    expect(fakePlatform.peer.setLANConfig).toHaveBeenLastCalledWith({
      lan_only: false, enabled: true, auto_discovery: true, advertise_host: '192.168.1.24', port: 9444, routes: [],
    })
  })

  it('enforces LAN-only controls while keeping Manual LAN available', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    fakePlatform.peer.getLANConfig.mockResolvedValue({
      lan_only: true,
      enabled: true,
      auto_discovery: true,
      advertise_host: '192.168.1.24',
      port: 8484,
      routes: [],
      running: true,
      discovery_running: true,
      listen_address: '0.0.0.0:8484',
    })
    fakePlatform.peer.getRendezvousConfig.mockResolvedValue({
      mode: 'official', url: 'https://rendezvous.atterm.dev', websocket_url: '', health_url: '',
      stun_mode: 'default', stun_urls: ['stun:stun.cloudflare.com:3478'],
      turn_enabled: false, turn_urls: [], turn_username: '', turn_credential_configured: false,
    })
    fakePlatform.peer.getRendezvousStatus.mockResolvedValue({
      mode: 'official', state: 'suppressed', reachable_peers: 0,
    })
    const wrapper = await mountReady()

    expect((wrapper.get('[data-testid="peer-lan-only"]').element as HTMLInputElement).checked).toBe(true)
    expect(wrapper.get('[data-testid="peer-rendezvous-save"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="peer-rendezvous-reconnect"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="peer-tunnel-start"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="peer-lan-enabled"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="peer-lan-route-host"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.text()).toContain('settings.peer.lan.onlyRelayHint')

    await wrapper.get('[data-testid="peer-lan-save"]').trigger('click')
    await flushPromises()
    expect(fakePlatform.peer.setLANConfig).toHaveBeenLastCalledWith(expect.objectContaining({
      lan_only: true,
      enabled: true,
    }))
  })

  it('copies a member reconnect bundle when Rendezvous is online without Quick Tunnel', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    fakePlatform.peer.getRendezvousStatus.mockResolvedValue({
      mode: 'official', state: 'online', url: 'https://rendezvous.atterm.dev', reachable_peers: 1,
    })
    const wrapper = await mountReady()

    expect(wrapper.get('[data-testid="peer-tunnel-status"]').text()).toContain('settings.peer.tunnel.stopped')
    await wrapper.get('[data-testid="peer-tunnel-copy-route"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.createConnectionBundle).toHaveBeenCalledWith('')
    expect(fakePlatform.system.setClipboardText).toHaveBeenCalledWith('atc1.member-route.signature')
  })

  it('stops Quick Tunnel and removes the share action', async () => {
    fakePlatform.peer.status.mockResolvedValue(configuredStatus)
    fakePlatform.peer.getQuickTunnelStatus.mockResolvedValue({
      running: true,
      starting: false,
      public_url: 'https://route.trycloudflare.com',
    })
    const wrapper = await mountReady()

    await wrapper.get('[data-testid="peer-tunnel-stop"]').trigger('click')
    await flushPromises()

    expect(fakePlatform.peer.stopQuickTunnel).toHaveBeenCalledOnce()
    expect(wrapper.find('[data-testid="peer-tunnel-copy-route"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="peer-tunnel-status"]').text()).toContain('settings.peer.tunnel.stopped')
  })
})
