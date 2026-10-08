import * as api from '../lib/api'
import {
  EventsOn,
  EventsEmit,
  WindowMinimise,
  WindowShow,
  WindowUnminimise,
  WindowToggleMaximise,
  WindowIsMaximised,
  WindowSetTitle,
  Quit,
  Environment,
  BrowserOpenURL,
  ClipboardSetText,
} from '../../wailsjs/runtime/runtime'
import {
  GetPluginConfig,
  SetPluginConfig,
  StartWidget,
  StopWidget,
  PushWidgetState,
  StartServicePreview,
  StopServicePreview,
  RebindServicePreview,
  StartNativeDirect,
  SendNativeDirectFrame,
  StopNativeDirect,
  GetPeerSpaceStatus,
  GetPeerConfigSyncStatus,
  AcceptPendingPeerConfig,
  DiscardPendingPeerConfig,
  CreatePeerSpace,
  CreatePeerInvitations,
  ListPeerInvitations,
  ListPeerMembers,
  RevokePeerInvitation,
  RevokePeerInvitationBatch,
  RevokePeerMember,
  PreviewPeerConnectionBundle,
  JoinPeerSpace,
  ImportPeerConnectionBundle,
  GetPeerQuickTunnelStatus,
  GetPeerRendezvousConfig,
  SetPeerRendezvousConfig,
  GetPeerRendezvousStatus,
  ReconnectPeerRendezvous,
  SyncPeerConfigNow,
  StartPeerQuickTunnel,
  StopPeerQuickTunnel,
  CreatePeerConnectionBundle,
  ListPeerSessions,
  GetPeerSessionRouteStatus,
  StartPeerNativeDirect,
  SendPeerNativeDirectFrame,
  StopPeerNativeDirect,
} from '../../wailsjs/go/main/App'
import {
  ListDir,
  WatchDir,
  UnwatchDir,
  ReadFile,
  FileMeta,
  OpenExternal,
  WriteFile,
  CreateFile,
  Rename,
  Remove,
  Mkdir,
  Trash,
} from '../../wailsjs/go/main/PluginFS'
import type { Platform, EnvironmentInfo, RemoteSession } from './types'
import { main as WailsModels } from '../../wailsjs/go/models'
import { setAccountKeyProvider } from '../lib/account-key'
import { NativeDirectClientTransport } from '../lib/nativeDirectClient'
import { PeerSessionConnection } from '../lib/peerSessionConnection'

// In-memory cache of the unlocked account_key. Mirrors the Capacitor
// platform's cache but reads from the Go App.GetAccountKey binding
// rather than secureStorage. Populated on platform init + after every
// login/register (api.loginRemoteRelay / api.registerRemoteRelay calls
// are wrapped to refetch). Wiped on logout.
let cachedAccountKey: Uint8Array | null = null

const peerNativeBridge = {
  on: (event: string, handler: (data: unknown) => void) => EventsOn(event, handler as (...data: unknown[]) => void),
  start: (req: { id: string; session_id: string; since_seq: number; client_instance_id: string; route?: 'direct' | 'quick_tunnel' }) => StartPeerNativeDirect(req),
  send: (id: string, frame: number[]) => SendPeerNativeDirectFrame(id, frame),
  stop: (id: string) => StopPeerNativeDirect(id),
}

async function listPeerRemoteSessions(): Promise<RemoteSession[]> {
  const parsed = JSON.parse(await ListPeerSessions()) as RemoteSession[] | null
  return (parsed ?? []).map((session) => ({
    ...session,
    session_id: session.session_id || (session as unknown as { id?: string }).id || '',
    peer_direct: true,
  }))
}

async function createPeerSessionWithProfile(hostID: string, profileID: string): Promise<string> {
  const candidates = (await listPeerRemoteSessions())
    .filter((session) => session.host_id === hostID &&
      (session.remote_permission === 'control' || session.remote_permission === 'full') && session.session_id)
    .sort((left, right) => left.session_id.localeCompare(right.session_id))
  if (candidates.length === 0) throw new Error('unknown_host_id')

  let selected: { anchor: RemoteSession; route: 'direct' | 'quick_tunnel' } | null = null
  let quickTunnelCandidate: RemoteSession | null = null
  for (const anchor of candidates) {
    let routeStatus: Awaited<ReturnType<typeof GetPeerSessionRouteStatus>>
    try {
      routeStatus = await GetPeerSessionRouteStatus(anchor.session_id)
    } catch {
      continue
    }
    if (routeStatus.direct) {
      selected = { anchor, route: 'direct' }
      break
    }
    if (!quickTunnelCandidate && routeStatus.quick_tunnel) quickTunnelCandidate = anchor
  }
  if (!selected && quickTunnelCandidate) selected = { anchor: quickTunnelCandidate, route: 'quick_tunnel' }
  if (!selected) throw new Error('upstream_unavailable')
  const { anchor, route } = selected

  return new Promise<string>((resolve, reject) => {
    const startedAt = Date.now()
    let requestStarted = false
    let settled = false
    let connection: PeerSessionConnection | null = null
    const finish = (complete: () => void) => {
      if (settled) return
      settled = true
      window.clearTimeout(timer)
      connection?.detach()
      complete()
    }
    const timer = window.setTimeout(() => finish(() => reject(new Error('timeout'))), 30_000)
    connection = new PeerSessionConnection(anchor.session_id, {
      onStatus: (status) => {
        if (status !== 'attached' || requestStarted || settled) return
        requestStarted = true
        const remaining = Math.max(1, 30_000 - (Date.now() - startedAt))
        void connection!.createSessionWithProfile(hostID, profileID, remaining)
          .then((sessionID) => finish(() => resolve(sessionID)))
          .catch((error) => finish(() => reject(error)))
      },
      onClose: () => finish(() => reject(new Error('upstream_unavailable'))),
    }, {
      route,
      transportFactory: (options) => new NativeDirectClientTransport(options, peerNativeBridge, 'peer-native-direct:event:'),
      resolveQuickTunnelFallback: async () => (await GetPeerSessionRouteStatus(anchor.session_id)).quick_tunnel,
    })
    connection.attach()
  })
}

function b64StdToBytes(s: string): Uint8Array {
  if (!s) return new Uint8Array(0)
  const norm = s.replace(/-/g, '+').replace(/_/g, '/')
  const pad = norm.length % 4 === 0 ? '' : '='.repeat(4 - (norm.length % 4))
  const bin = atob(norm + pad)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

async function refreshCachedAccountKey(): Promise<void> {
  try {
    const b64 = await api.getAccountKey()
    cachedAccountKey = b64 ? b64StdToBytes(b64) : null
  } catch {
    cachedAccountKey = null
  }
}

export function createWailsPlatform(): Platform {
  setAccountKeyProvider(() => cachedAccountKey)
  void refreshCachedAccountKey()
  // Refresh whenever the Go side mutates the unlocked key (login,
  // register, logout). No polling, no race window.
  EventsOn('account-key:changed', () => {
    void refreshCachedAccountKey()
  })
  // Event names currently being dispatched by emit() below. Unlike the web /
  // Capacitor platforms — whose buses are private Maps — `on` and `emit` here
  // are both backed by the *same* Wails bus, and wails/runtime.js EventsEmit
  // synchronously calls every EventsOn listener for the name before shipping
  // the event to Go. So a handler that re-emits the event it is handling
  // re-enters itself without bound: one 'prefs:changed' recursed ~1286 deep,
  // threw "Maximum call stack size exceeded" (aborting the dispatch, so the
  // real listeners never ran) and fired ~1286 WailsInvoke messages that
  // flooded the main thread and froze the UI for seconds. Guarding here makes
  // that shape impossible for every event, not just the one that hit it.
  const dispatching = new Set<string>()
  return {
    caps: {
      localPty: true,
      autoUpdate: true,
      pluginHost: true,
      windowControls: true,
      systemClipboard: true,
      notifications: true,
      fileDialog: true,
      wailsBindings: true,
      capacitor: false,
    },
    relay: {
      load: () => api.getRelayConfig().then((c) => c ?? null),
      save: async (cfg) => {
        await api.setRelayConfig({
          url: cfg.url,
          token: cfg.token,
          session_expires_at: cfg.session_expires_at,
          allow_insecure_relay: cfg.allow_insecure_relay,
          disable_e2ee: cfg.disable_e2ee,
          remote_permission: cfg.remote_permission,
        })
      },
      clear: () => api.clearRelayConfig(),
      fetchMe: () => api.fetchRelayMe(),
      setUplinkPaused: (paused: boolean) => api.setUplinkPaused(paused),
    },
    sessions: {
      newSession: api.newSession,
      closeSession: api.closeSession,
      listShells: api.listShells,
      listRemoteSessions: listPeerRemoteSessions,
      markSessionsSeen: api.markSessionsSeen,
      getPins: () => api.getPinnedSessionIds(),
      setPins: (ids) => api.setPinnedSessionIds(ids),
      listRelaySessions: api.listRelaySessions,
      revokeRelaySession: api.revokeRelaySession,
      signOutOtherRelaySessions: api.signOutOtherRelaySessions,
      createSessionWithProfile: createPeerSessionWithProfile,
    },
    servicePreview: {
      supportsMappings: true,
      start: async (req) => {
        const mappings = req.mappings ?? (req.serviceId && req.clientTicket && req.clientToHostKey && req.hostToClientKey ? [{
          serviceId: req.serviceId,
          clientTicket: req.clientTicket,
          clientToHostKey: req.clientToHostKey,
          hostToClientKey: req.hostToClientKey,
          port: 0,
        }] : [])
        if (mappings.length === 0) throw new Error("service preview requires at least one mapping")
        const first = mappings[0]
        const request = new WailsModels.ServicePreviewStartRequest({
          mappings: mappings.map((mapping) => ({
            service_id: mapping.serviceId,
            client_ticket: mapping.clientTicket,
            client_to_host_key: Array.from(mapping.clientToHostKey),
            host_to_client_key: Array.from(mapping.hostToClientKey),
            port: mapping.port,
            path_prefix: mapping.pathPrefix || "",
          })),
          service_id: first.serviceId,
          client_ticket: first.clientTicket,
          client_to_host_key: Array.from(first.clientToHostKey),
          host_to_client_key: Array.from(first.hostToClientKey),
        })
        const result = await StartServicePreview(request)
        return { id: result.id, url: result.url }
      },
      stop: async (id) => StopServicePreview(id),
      rebind: async (req) => {
        await RebindServicePreview(new WailsModels.ServicePreviewRebindRequest({
          gateway_id: req.gatewayId,
          mapping_index: req.mappingIndex,
          service_id: req.serviceId,
          client_ticket: req.clientTicket,
          client_to_host_key: Array.from(req.clientToHostKey),
          host_to_client_key: Array.from(req.hostToClientKey),
        }))
      },
    },
    system: {
      showNotification: api.showNotification,
      getClipboardPaste: api.getClipboardPastePayload,
      setClipboardText: async (text: string) => {
        await ClipboardSetText(text)
      },
      pickLogFilePath: api.pickLogFilePath,
      openExternalURL: async (url: string) => {
        BrowserOpenURL(url)
      },
      getEnvironment: async (): Promise<EnvironmentInfo | null> => {
        try {
          const info = await Environment()
          return {
            platform: info.platform,
            arch: info.arch,
            buildType: info.buildType,
          }
        } catch {
          return null
        }
      },
      getAppVersion: async () => {
        const { GetAppVersion } = await import('../../wailsjs/go/main/App')
        return GetAppVersion()
      },
      windowMinimize: async () => WindowMinimise(),
      windowShow: async () => WindowShow(),
      windowUnminimize: async () => WindowUnminimise(),
      windowToggleMaximize: async () => WindowToggleMaximise(),
      windowIsMaximized: () => WindowIsMaximised(),
      windowSetTitle: async (title: string) => WindowSetTitle(title),
      quit: async () => Quit(),
    },
    events: {
      on: (event, handler) => EventsOn(event, handler as (...data: unknown[]) => void),
      emit: (event, data) => {
        // Re-entrant emit of an in-flight event is the self-recursion case
        // described above; drop it rather than let it stack.
        if (dispatching.has(event)) return
        dispatching.add(event)
        try {
          EventsEmit(event, data)
        } finally {
          dispatching.delete(event)
        }
      },
    },
    templates: {
      load: async () => {
        const raw = await api.getQuickTemplates()
        return Array.isArray(raw) ? raw : []
      },
      save: async (list) => {
        await api.setQuickTemplates(list)
      },
      clear: async () => {
        await api.setQuickTemplates([])
      },
      // Hidden flag is a per-device UI preference, not a session secret —
      // localStorage in the WebView is fine and avoids a new Wails binding.
      loadHidden: async () => {
        return typeof localStorage !== 'undefined' && localStorage.getItem('atterm.templates.hidden') === '1'
      },
      saveHidden: async (hidden: boolean) => {
        if (typeof localStorage === 'undefined') return
        if (hidden) localStorage.setItem('atterm.templates.hidden', '1')
        else localStorage.removeItem('atterm.templates.hidden')
      },
    },
    // Desktop has a real keyboard and never renders the mobile aux-key bar,
    // so there is nothing to persist — a no-op bridge satisfies the Platform
    // type without a Go binding.
    auxKeys: {
      load: async () => [],
      save: async () => {},
      clear: async () => {},
    },
    directConnection: {
      load: () => api.getDirectP2PEnabled(),
      save: (enabled) => api.setDirectP2PEnabled(enabled),
      createTransport: (options) => new NativeDirectClientTransport(options, {
        on: (event, handler) => EventsOn(event, handler as (...data: unknown[]) => void),
        start: (req) => StartNativeDirect(req),
        send: (id, frame) => SendNativeDirectFrame(id, frame),
        stop: (id) => StopNativeDirect(id),
      }),
    },
    peer: {
      status: () => GetPeerSpaceStatus(),
      configSyncStatus: () => GetPeerConfigSyncStatus(),
      acceptPendingConfig: () => AcceptPendingPeerConfig(),
      discardPendingConfig: () => DiscardPendingPeerConfig(),
      createSpace: () => CreatePeerSpace(),
      previewConnectionBundle: (raw) => PreviewPeerConnectionBundle(raw),
      joinSpace: (req) => JoinPeerSpace(new WailsModels.JoinPeerSpaceReq(req)),
      importConnectionBundle: (raw) => ImportPeerConnectionBundle(raw),
      createInvitations: (req) => CreatePeerInvitations(new WailsModels.CreatePeerInvitationsReq(req)),
      listInvitations: () => ListPeerInvitations(),
      revokeInvitation: (inviteID) => RevokePeerInvitation(inviteID),
      revokeInvitationBatch: (batchID) => RevokePeerInvitationBatch(batchID),
      listMembers: () => ListPeerMembers(),
      revokeMember: (peerID) => RevokePeerMember(peerID),
      getRendezvousConfig: () => GetPeerRendezvousConfig(),
      setRendezvousConfig: (req) => SetPeerRendezvousConfig(new WailsModels.SetPeerRendezvousConfigReq(req)),
      getRendezvousStatus: () => GetPeerRendezvousStatus(),
      reconnectRendezvous: () => ReconnectPeerRendezvous(),
      syncConfigNow: () => SyncPeerConfigNow(),
      getQuickTunnelStatus: () => GetPeerQuickTunnelStatus(),
      startQuickTunnel: () => StartPeerQuickTunnel(),
      stopQuickTunnel: () => StopPeerQuickTunnel(),
      createConnectionBundle: (invitationToken) => CreatePeerConnectionBundle(invitationToken),
      listSessions: listPeerRemoteSessions,
      getSessionRouteStatus: (sessionID) => GetPeerSessionRouteStatus(sessionID),
      createSessionTransport: (options) => new NativeDirectClientTransport(options, peerNativeBridge, 'peer-native-direct:event:'),
    },
    updater: {
      getState: api.getUpdateState,
      checkUpdate: api.checkUpdate,
      startDownload: api.startDownload,
      downloadVersion: api.downloadVersion,
      installUpdate: api.installUpdate,
    },
    pluginHost: {
      getPluginConfig: GetPluginConfig as () => Promise<import('../../wailsjs/go/models').main.PluginConfig>,
      setPluginConfig: SetPluginConfig as (cfg: import('../../wailsjs/go/models').main.PluginConfig) => Promise<void>,
      fs: {
        listDir: ListDir as (path: string) => Promise<import('./types').DirEntry[]>,
        watchDir: WatchDir,
        unwatchDir: UnwatchDir,
        readFile: ReadFile as (path: string, maxBytes?: number) => Promise<import('./types').FileContent>,
        fileMeta: FileMeta as (path: string) => Promise<import('./types').FileMetaInfo>,
        openExternal: (path: string) => OpenExternal(path),
        assetUrlFor: (path: string) => {
          // base64 URL encoding (RFC 4648 §5), matches Go's base64.URLEncoding.
          const bytes = new TextEncoder().encode(path);
          let bin = "";
          for (const b of bytes) bin += String.fromCharCode(b);
          const b64 = btoa(bin).replace(/\+/g, "-").replace(/\//g, "_");
          return "/pluginfs/" + b64;
        },
        writeFile: (path: string, data: number[] | Uint8Array, expectedModTime: number, createIfMissing: boolean) =>
          WriteFile(path, Array.from(data), expectedModTime, createIfMissing) as Promise<import('./types').FileMetaInfo>,
        createFile: (path: string) => CreateFile(path) as Promise<import('./types').FileMetaInfo>,
        rename: (from: string, to: string) => Rename(from, to) as Promise<import('./types').FileMetaInfo>,
        remove: (path: string, recursive: boolean) => Remove(path, recursive),
        mkdir: (path: string) => Mkdir(path) as Promise<import('./types').FileMetaInfo>,
        trash: (path: string) => Trash(path),
      },
    },
    deskWidget: {
      start: () => StartWidget(),
      stop: () => StopWidget(),
      pushState: (json: string) => PushWidgetState(json),
    },
  }
}
