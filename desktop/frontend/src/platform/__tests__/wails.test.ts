import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock lib/api before the impl import so createWailsPlatform sees the mocks.
vi.mock('../../lib/api', () => ({
  getRelayConfig: vi.fn().mockResolvedValue({ url: 'https://r', token: 'atk_x', session_expires_at: 0, allow_insecure_relay: false, remote_permission: 'full', connected: false }),
  setRelayConfig: vi.fn().mockResolvedValue(undefined),
  setUplinkPaused: vi.fn().mockResolvedValue(undefined),
  fetchRelayMe: vi.fn().mockResolvedValue({ user_id: 'u', email: 'e' }),
  getClipboardPastePayload: vi.fn().mockResolvedValue({ kind: 'none' }),
  showNotification: vi.fn().mockResolvedValue(undefined),
  pickLogFilePath: vi.fn().mockResolvedValue('/tmp/log'),
  newSession: vi.fn().mockResolvedValue({ session_id: 's1' }),
  closeSession: vi.fn().mockResolvedValue(undefined),
  listShells: vi.fn().mockResolvedValue(['/bin/zsh']),
  getUpdateState: vi.fn().mockResolvedValue({ state: 'idle' }),
  checkUpdate: vi.fn().mockResolvedValue(undefined),
  startDownload: vi.fn().mockResolvedValue(undefined),
  downloadVersion: vi.fn().mockResolvedValue(undefined),
  installUpdate: vi.fn().mockResolvedValue(undefined),
  markSessionsSeen: vi.fn().mockResolvedValue(undefined),
  getPinnedSessionIds: vi.fn().mockResolvedValue(['s1', 's2']),
  setPinnedSessionIds: vi.fn().mockResolvedValue(undefined),
  listRelaySessions: vi.fn().mockResolvedValue([{ id_hash: 'h1', user_agent: 'UA', ip_prefix: '1.2.3', created_at: 1, expires_at: 2, is_current: true }]),
  revokeRelaySession: vi.fn().mockResolvedValue(undefined),
  signOutOtherRelaySessions: vi.fn().mockResolvedValue({ deleted: 1 }),
}))

vi.mock('../../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn(() => () => {}),
  EventsEmit: vi.fn(),
  WindowMinimise: vi.fn(),
  WindowShow: vi.fn(),
  WindowUnminimise: vi.fn(),
  WindowToggleMaximise: vi.fn(),
  WindowIsMaximised: vi.fn().mockResolvedValue(true),
  Quit: vi.fn(),
  Environment: vi.fn().mockResolvedValue({ platform: 'darwin', arch: 'arm64', buildType: 'production' }),
  BrowserOpenURL: vi.fn(),
  ClipboardSetText: vi.fn().mockResolvedValue(true),
}))

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetPluginConfig: vi.fn().mockResolvedValue({ enabled_plugins: [] }),
  SetPluginConfig: vi.fn().mockResolvedValue(undefined),
  GetAppVersion: vi.fn().mockResolvedValue('v0.3.19'),
  StartServicePreview: vi.fn().mockResolvedValue({ id: 'preview-1', url: 'http://127.0.0.1:49000/' }),
  RebindServicePreview: vi.fn().mockResolvedValue(undefined),
  StopServicePreview: vi.fn().mockResolvedValue(undefined),
  GetPeerSpaceStatus: vi.fn().mockResolvedValue({ configured: false }),
  GetPeerTraffic: vi.fn().mockResolvedValue([
    { day: '2026-09-23', route: 'direct', bytes_sent: 10, bytes_received: 20, records_sent: 1, records_received: 2 },
    { day: '2026-09-23', route: 'future_route', bytes_sent: 30, bytes_received: 40, records_sent: 3, records_received: 4 },
  ]),
  GetPeerConfigSyncStatus: vi.fn().mockResolvedValue({ configured: true, pending_operations: 2 }),
  AcceptPendingPeerConfig: vi.fn().mockResolvedValue({ configured: true, pending_operations: 3, pending_import_records: 0 }),
  DiscardPendingPeerConfig: vi.fn().mockResolvedValue({ configured: true, pending_operations: 2, pending_import_records: 0 }),
  CreatePeerSpace: vi.fn().mockResolvedValue({ configured: true, peer_id: 'peer-1', space_id: 'space-1' }),
  CreatePeerInvitations: vi.fn().mockResolvedValue([{ invite_id: 'invite-1', batch_id: 'batch-1', token: 'atp1.x.y', expires_at: 10 }]),
  ListPeerInvitations: vi.fn().mockResolvedValue([]),
  ListPeerMembers: vi.fn().mockResolvedValue([{ peer_id: 'peer-1', local: true, status: 'active' }]),
  RevokePeerInvitation: vi.fn().mockResolvedValue(undefined),
  RevokePeerInvitationBatch: vi.fn().mockResolvedValue(undefined),
  RevokePeerMember: vi.fn().mockResolvedValue(undefined),
  PreviewPeerConnectionBundle: vi.fn().mockResolvedValue({ fingerprint: 'SHA256:space' }),
  JoinPeerSpace: vi.fn().mockResolvedValue({ configured: true, peer_id: 'peer-2', space_id: 'space-1' }),
  ImportPeerConnectionBundle: vi.fn().mockResolvedValue({ issuer_peer_id: 'peer-2', quick_tunnel: true, expires_at: 20 }),
  GetPeerRendezvousConfig: vi.fn().mockResolvedValue({
    mode: 'disabled', url: '', websocket_url: '', health_url: '', stun_mode: 'default',
    stun_urls: ['stun:stun.cloudflare.com:3478'], turn_enabled: false, turn_urls: [],
    turn_username: '', turn_credential_configured: false,
  }),
  SetPeerRendezvousConfig: vi.fn().mockResolvedValue(undefined),
  GetPeerRendezvousStatus: vi.fn().mockResolvedValue({ mode: 'disabled', state: 'disabled', reachable_peers: 0 }),
  ReconnectPeerRendezvous: vi.fn().mockResolvedValue({ mode: 'official', state: 'connecting', reachable_peers: 0 }),
  SyncPeerConfigNow: vi.fn().mockResolvedValue({ configured: true, pending_operations: 1 }),
  GetPeerQuickTunnelStatus: vi.fn().mockResolvedValue({ running: false, starting: false }),
  StartPeerQuickTunnel: vi.fn().mockResolvedValue({ running: true, starting: false, public_url: 'https://route.trycloudflare.com' }),
  StopPeerQuickTunnel: vi.fn().mockResolvedValue(undefined),
  CreatePeerConnectionBundle: vi.fn().mockResolvedValue('atc1.member-route.signature'),
  ListPeerSessions: vi.fn().mockResolvedValue(JSON.stringify([{
    id: 'peer-session-1', host_id: 'peer-host', remote_permission: 'full',
  }])),
  ListPeerHosts: vi.fn().mockResolvedValue([{
    id: 'peer-host', name: 'Peer Desktop', permission: 'control',
  }]),
  CreatePeerSessionWithProfile: vi.fn().mockResolvedValue('aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee'),
  GetPeerSessionRouteStatus: vi.fn().mockResolvedValue({ direct: true, quick_tunnel: true }),
  StartPeerNativeDirect: vi.fn().mockResolvedValue(undefined),
  SendPeerNativeDirectFrame: vi.fn().mockResolvedValue(undefined),
  StopPeerNativeDirect: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('../../../wailsjs/go/main/PluginFS', () => ({
  ListDir: vi.fn().mockResolvedValue([]),
  WatchDir: vi.fn().mockResolvedValue(undefined),
  UnwatchDir: vi.fn().mockResolvedValue(undefined),
  ReadFile: vi.fn().mockResolvedValue({ path: '/x', data: [], isBinary: false }),
  FileMeta: vi.fn().mockResolvedValue({ path: '/x', size: 0, modTime: 0, isDir: false, exists: true }),
}))

import { createWailsPlatform } from '../wails'
import { WindowMinimise, WindowShow, WindowUnminimise, Environment, BrowserOpenURL, ClipboardSetText, EventsOn, EventsEmit } from '../../../wailsjs/runtime/runtime'
import {
  AcceptPendingPeerConfig,
  CreatePeerConnectionBundle,
  CreatePeerInvitations,
  GetPeerTraffic,
  GetPeerRendezvousConfig,
  GetPeerRendezvousStatus,
  GetPeerQuickTunnelStatus,
  GetPeerConfigSyncStatus,
  GetPluginConfig,
  JoinPeerSpace,
  ImportPeerConnectionBundle,
  ListPeerSessions,
  ListPeerHosts,
  CreatePeerSessionWithProfile,
  GetPeerSessionRouteStatus,
  ListPeerMembers,
  PreviewPeerConnectionBundle,
  RevokePeerMember,
  SetPeerRendezvousConfig,
  ReconnectPeerRendezvous,
  SyncPeerConfigNow,
  DiscardPendingPeerConfig,
  SetPluginConfig,
  GetAppVersion,
  StartPeerQuickTunnel,
  StartPeerNativeDirect,
  RebindServicePreview,
  StartServicePreview,
  StopPeerNativeDirect,
  StopPeerQuickTunnel,
} from '../../../wailsjs/go/main/App'
import { ListDir, ReadFile } from '../../../wailsjs/go/main/PluginFS'
import {
  fetchRelayMe,
  showNotification,
  setUplinkPaused,
  markSessionsSeen,
  getPinnedSessionIds,
  setPinnedSessionIds,
  listRelaySessions,
  revokeRelaySession,
  signOutOtherRelaySessions,
} from '../../lib/api'

describe('createWailsPlatform', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.removeItem('atterm.peer.quick-tunnel-fallback-consent')
  })

  it('caps has all desktop flags true', () => {
    const p = createWailsPlatform()
    expect(p.caps).toEqual({
      localPty: true, autoUpdate: true, pluginHost: true, windowControls: true,
      systemClipboard: true, notifications: true, fileDialog: true,
      wailsBindings: true, capacitor: false,
    })
  })

  it('relay.fetchMe delegates to lib/api fetchRelayMe', async () => {
    const p = createWailsPlatform()
    const me = await p.relay.fetchMe()
    expect(fetchRelayMe).toHaveBeenCalledOnce()
    expect(me).toEqual({ user_id: 'u', email: 'e' })
  })

  it('relay.setUplinkPaused delegates', async () => {
    const p = createWailsPlatform()
    await p.relay.setUplinkPaused!(true)
    expect(setUplinkPaused).toHaveBeenCalledWith(true)
  })

  it('sessions.markSessionsSeen delegates to api.markSessionsSeen', async () => {
    const p = createWailsPlatform()
    await p.sessions.markSessionsSeen!({ ids: ['s1'] })
    expect(markSessionsSeen).toHaveBeenCalledWith({ ids: ['s1'] })
  })

  it('sessions.getPins delegates to api.getPinnedSessionIds', async () => {
    const p = createWailsPlatform()
    const pins = await p.sessions.getPins()
    expect(getPinnedSessionIds).toHaveBeenCalledOnce()
    expect(pins).toEqual(['s1', 's2'])
  })

  it('sessions.setPins delegates to api.setPinnedSessionIds', async () => {
    const p = createWailsPlatform()
    await p.sessions.setPins(['s3'])
    expect(setPinnedSessionIds).toHaveBeenCalledWith(['s3'])
  })

  it('sessions.listRelaySessions delegates to api.listRelaySessions', async () => {
    const p = createWailsPlatform()
    const rows = await p.sessions.listRelaySessions!()
    expect(listRelaySessions).toHaveBeenCalledOnce()
    expect(rows).toEqual([{ id_hash: 'h1', user_agent: 'UA', ip_prefix: '1.2.3', created_at: 1, expires_at: 2, is_current: true }])
  })

  it('sessions.revokeRelaySession delegates to api.revokeRelaySession', async () => {
    const p = createWailsPlatform()
    await p.sessions.revokeRelaySession!('h1')
    expect(revokeRelaySession).toHaveBeenCalledWith('h1')
  })

  it('sessions.signOutOtherRelaySessions delegates to api.signOutOtherRelaySessions', async () => {
    const p = createWailsPlatform()
    const result = await p.sessions.signOutOtherRelaySessions!()
    expect(signOutOtherRelaySessions).toHaveBeenCalledOnce()
    expect(result).toEqual({ deleted: 1 })
  })

  it('system.showNotification delegates', async () => {
    const p = createWailsPlatform()
    await p.system.showNotification('t', 'b')
    expect(showNotification).toHaveBeenCalledWith('t', 'b')
  })

  it('system.setClipboardText delegates to native Wails clipboard', async () => {
    const p = createWailsPlatform()
    await p.system.setClipboardText!('selected output')
    expect(ClipboardSetText).toHaveBeenCalledWith('selected output')
  })

  it('system.windowMinimize delegates to runtime WindowMinimise', async () => {
    const p = createWailsPlatform()
    await p.system.windowMinimize!()
    expect(WindowMinimise).toHaveBeenCalledOnce()
  })

  it('system.windowShow delegates to runtime WindowShow', async () => {
    const p = createWailsPlatform()
    await p.system.windowShow!()
    expect(WindowShow).toHaveBeenCalledOnce()
  })

  it('system.windowUnminimize delegates to runtime WindowUnminimise', async () => {
    const p = createWailsPlatform()
    await p.system.windowUnminimize!()
    expect(WindowUnminimise).toHaveBeenCalledOnce()
  })

  it('system.getEnvironment returns the EnvironmentInfo from runtime', async () => {
    const p = createWailsPlatform()
    const env = await p.system.getEnvironment()
    expect(Environment).toHaveBeenCalledOnce()
    expect(env).toEqual({ platform: 'darwin', arch: 'arm64', buildType: 'production' })
  })

  it('system.openExternalURL delegates to BrowserOpenURL', async () => {
    const p = createWailsPlatform()
    await p.system.openExternalURL('https://example.com')
    expect(BrowserOpenURL).toHaveBeenCalledWith('https://example.com')
  })

  it('system.getAppVersion delegates to wailsjs GetAppVersion', async () => {
    const p = createWailsPlatform()
    const version = await p.system.getAppVersion()
    expect(GetAppVersion).toHaveBeenCalledOnce()
    expect(version).toBe('v0.3.19')
  })

  it('servicePreview converts multiple mappings for the desktop gateway', async () => {
    const p = createWailsPlatform()
    const firstKey = new Uint8Array(32).fill(1)
    const secondKey = new Uint8Array(32).fill(2)
    await p.servicePreview!.start({
      mappings: [
        { serviceId: 'service-root', clientTicket: 'ticket-root', clientToHostKey: firstKey, hostToClientKey: secondKey, port: 3000 },
        { serviceId: 'service-api', clientTicket: 'ticket-api', clientToHostKey: secondKey, hostToClientKey: firstKey, port: 8080, pathPrefix: '/api' },
      ],
    })
    expect(StartServicePreview).toHaveBeenCalledWith(expect.objectContaining({
      mappings: [
        expect.objectContaining({ service_id: 'service-root', port: 3000, path_prefix: '' }),
        expect.objectContaining({ service_id: 'service-api', port: 8080, path_prefix: '/api' }),
      ],
    }))
  })

  it('servicePreview forwards only the opaque Peer attempt handle for Peer mappings', async () => {
    const p = createWailsPlatform()
    const emptyKey = new Uint8Array()
    await p.servicePreview!.start({
      mappings: [{
        serviceId: 'peer-service', clientTicket: '',
        clientToHostKey: emptyKey, hostToClientKey: emptyKey,
        port: 3000, peerAttemptId: 'native-attempt-1',
      }],
    })
    expect(StartServicePreview).toHaveBeenCalledWith(expect.objectContaining({
      mappings: [expect.objectContaining({
        service_id: 'peer-service', peer_attempt_id: 'native-attempt-1',
        client_ticket: '', client_to_host_key: [], host_to_client_key: [],
      })],
    }))

    await p.servicePreview!.rebind!({
      gatewayId: 'gateway-1', mappingIndex: 0, serviceId: 'peer-service-2',
      clientTicket: '', clientToHostKey: emptyKey, hostToClientKey: emptyKey,
      peerAttemptId: 'native-attempt-2',
    })
    expect(RebindServicePreview).toHaveBeenCalledWith(expect.objectContaining({
      gateway_id: 'gateway-1', service_id: 'peer-service-2',
      peer_attempt_id: 'native-attempt-2', client_ticket: '',
      client_to_host_key: [], host_to_client_key: [],
    }))
  })

  it('peer bridge wraps invitation requests in the generated model', async () => {
    const p = createWailsPlatform()
    const result = await p.peer!.createInvitations({
      count: 5,
      valid_for_hours: 24,
      permission: 'control',
      allowed_session_ids: [],
      can_invite: false,
      can_sync_secrets: false,
    })
    expect(CreatePeerInvitations).toHaveBeenCalledWith(expect.objectContaining({
      count: 5,
      valid_for_hours: 24,
      permission: 'control',
    }))
    expect(result[0].invite_id).toBe('invite-1')
  })

  it('peer bridge exposes known local traffic routes and drops unknown routes', async () => {
    const p = createWailsPlatform()

    expect(await p.peer!.getTraffic!('2026-09-01', '2026-09-23')).toEqual([
      { day: '2026-09-23', route: 'direct', bytes_sent: 10, bytes_received: 20, records_sent: 1, records_received: 2 },
    ])
    expect(GetPeerTraffic).toHaveBeenCalledWith('2026-09-01', '2026-09-23')
  })

  it('peer bridge previews and joins through typed Wails bindings', async () => {
    const p = createWailsPlatform()
    const preview = await p.peer!.previewConnectionBundle('atc1.bundle.signature')
    expect(PreviewPeerConnectionBundle).toHaveBeenCalledWith('atc1.bundle.signature')
    expect(preview.fingerprint).toBe('SHA256:space')

    await p.peer!.joinSpace({
      connection_bundle: 'atc1.bundle.signature',
      expected_fingerprint: 'SHA256:space',
    })
    expect(JoinPeerSpace).toHaveBeenCalledWith(expect.objectContaining({
      connection_bundle: 'atc1.bundle.signature',
      expected_fingerprint: 'SHA256:space',
    }))
  })

  it('peer bridge exposes Quick Tunnel host lifecycle and member route bundles', async () => {
    const p = createWailsPlatform()

    expect(await p.peer!.getQuickTunnelStatus!()).toEqual({ running: false, starting: false })
    expect(GetPeerQuickTunnelStatus).toHaveBeenCalledOnce()

    expect(await p.peer!.startQuickTunnel!()).toEqual(expect.objectContaining({
      running: true,
      public_url: 'https://route.trycloudflare.com',
    }))
    expect(StartPeerQuickTunnel).toHaveBeenCalledOnce()

    expect(await p.peer!.createConnectionBundle!('')).toBe('atc1.member-route.signature')
    expect(CreatePeerConnectionBundle).toHaveBeenCalledWith('')

    await p.peer!.stopQuickTunnel!()
    expect(StopPeerQuickTunnel).toHaveBeenCalledOnce()
  })

  it('peer bridge imports member routes and exposes token-free route status', async () => {
    const p = createWailsPlatform()

    expect(await p.peer!.importConnectionBundle('atc1.member-route.signature')).toEqual({
      issuer_peer_id: 'peer-2', quick_tunnel: true, expires_at: 20,
    })
    expect(ImportPeerConnectionBundle).toHaveBeenCalledWith('atc1.member-route.signature')

    expect(await p.peer!.getSessionRouteStatus!('peer-session-1')).toEqual({
      direct: true, quick_tunnel: true,
    })
    expect(GetPeerSessionRouteStatus).toHaveBeenCalledWith('peer-session-1')
  })

  it('peer bridge persists local Rendezvous configuration through typed Wails bindings', async () => {
    const p = createWailsPlatform()

    expect(await p.peer!.getRendezvousConfig()).toEqual(expect.objectContaining({ mode: 'disabled' }))
    expect(GetPeerRendezvousConfig).toHaveBeenCalledOnce()

    await p.peer!.setRendezvousConfig({
      mode: 'official', url: '', stun_mode: 'default', stun_urls: [],
      turn_enabled: false, turn_urls: [], turn_username: '', turn_credential: '',
    })
    expect(SetPeerRendezvousConfig).toHaveBeenCalledWith(expect.objectContaining({ mode: 'official' }))

    expect(await p.peer!.getRendezvousStatus()).toEqual(expect.objectContaining({ state: 'disabled' }))
    expect(GetPeerRendezvousStatus).toHaveBeenCalledOnce()
    expect(await p.peer!.reconnectRendezvous()).toEqual(expect.objectContaining({ state: 'connecting' }))
    expect(ReconnectPeerRendezvous).toHaveBeenCalledOnce()
    expect(await p.peer!.syncConfigNow()).toEqual(expect.objectContaining({ pending_operations: 1 }))
    expect(SyncPeerConfigNow).toHaveBeenCalledOnce()
  })

  it('peer bridge adapts encrypted catalog entries for the shared sidebar model', async () => {
    const p = createWailsPlatform()

    expect(await p.peer!.listSessions!()).toEqual([
      expect.objectContaining({
        id: 'peer-session-1',
        session_id: 'peer-session-1',
        peer_direct: true,
      }),
    ])
    expect(ListPeerSessions).toHaveBeenCalledOnce()
  })

  it('peer transport uses an isolated native event namespace', () => {
    const p = createWailsPlatform()
    const transport = p.peer!.createSessionTransport!({
      signalURL: '',
      sessionId: '11111111-2222-3333-4444-555555555555',
      sinceSeq: 7,
      clientInstanceId: 'peer-client',
      callbacks: { onFrame: vi.fn(), onReady: vi.fn(), onFailure: vi.fn() },
    })

    transport.start()
    expect(EventsOn).toHaveBeenCalledWith(
      expect.stringMatching(/^peer-native-direct:event:/),
      expect.any(Function),
    )
    expect(StartPeerNativeDirect).toHaveBeenCalledWith(expect.objectContaining({
      session_id: '11111111-2222-3333-4444-555555555555',
      since_seq: 7,
      client_instance_id: 'peer-client',
    }))

    transport.close()
    expect(StopPeerNativeDirect).toHaveBeenCalledOnce()
  })

  it('lists zero-session Peer hosts and delegates host-control creation to Go', async () => {
    const targetHostID = 'peer-host'
    const p = createWailsPlatform()

    await expect(p.sessions.listRemoteHosts!()).resolves.toEqual([
      { id: targetHostID, name: 'Peer Desktop', permission: 'control' },
    ])
    expect(ListPeerHosts).toHaveBeenCalledOnce()

    await expect(p.sessions.createSessionWithProfile!(targetHostID, 'profile-a'))
      .resolves.toBe('aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee')
    expect(CreatePeerSessionWithProfile).toHaveBeenLastCalledWith(targetHostID, 'profile-a', false)

    localStorage.setItem('atterm.peer.quick-tunnel-fallback-consent', '1')
    await p.sessions.createSessionWithProfile!(targetHostID, 'profile-b')
    expect(CreatePeerSessionWithProfile).toHaveBeenLastCalledWith(targetHostID, 'profile-b', true)
  })

  it('peer bridge lists and revokes Peer members independently of Relay sessions', async () => {
    const p = createWailsPlatform()

    expect(await p.peer!.listMembers()).toEqual([
      expect.objectContaining({ peer_id: 'peer-1', local: true, status: 'active' }),
    ])
    expect(ListPeerMembers).toHaveBeenCalledOnce()

    await p.peer!.revokeMember('peer-2')
    expect(RevokePeerMember).toHaveBeenCalledWith('peer-2')
  })

  it('peer bridge exposes token-free config sync status and pending import actions', async () => {
    const p = createWailsPlatform()

    expect(await p.peer!.configSyncStatus()).toEqual(expect.objectContaining({ pending_operations: 2 }))
    expect(GetPeerConfigSyncStatus).toHaveBeenCalledOnce()
    expect(await p.peer!.acceptPendingConfig()).toEqual(expect.objectContaining({ pending_import_records: 0 }))
    expect(AcceptPendingPeerConfig).toHaveBeenCalledOnce()
    expect(await p.peer!.discardPendingConfig()).toEqual(expect.objectContaining({ pending_import_records: 0 }))
    expect(DiscardPendingPeerConfig).toHaveBeenCalledOnce()
  })

  it('events.on subscribes via EventsOn and returns the unsubscribe', () => {
    // createWailsPlatform calls EventsOn once for 'account-key:changed'
    // during construction (M5-meta-wails). Construct first, then prime
    // the next EventsOn return so the assertion lands on the test's
    // own subscription, not the platform's.
    const p = createWailsPlatform()
    const off = vi.fn()
    ;(EventsOn as ReturnType<typeof vi.fn>).mockReturnValueOnce(off)
    const handler = vi.fn()
    const u = p.events.on('relay:auth-error', handler)
    expect(EventsOn).toHaveBeenCalledWith('relay:auth-error', handler)
    expect(u).toBe(off)
  })

  it('events.emit delegates to EventsEmit', () => {
    const p = createWailsPlatform()
    p.events.emit('foo', { x: 1 })
    expect(EventsEmit).toHaveBeenCalledWith('foo', { x: 1 })
  })

  it('pluginHost.getPluginConfig delegates to App.GetPluginConfig', async () => {
    const p = createWailsPlatform()
    await p.pluginHost!.getPluginConfig()
    expect(GetPluginConfig).toHaveBeenCalledOnce()
  })

  it('pluginHost.fs.listDir delegates to PluginFS.ListDir', async () => {
    const p = createWailsPlatform()
    await p.pluginHost!.fs.listDir('/tmp')
    expect(ListDir).toHaveBeenCalledWith('/tmp')
  })

  it('pluginHost.fs.readFile delegates to PluginFS.ReadFile', async () => {
    const p = createWailsPlatform()
    await p.pluginHost!.fs.readFile('/tmp/x')
    expect(ReadFile).toHaveBeenCalledWith('/tmp/x')
  })
})
