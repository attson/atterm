// Shared data types re-exported so consumers don't need to know if they came
// from lib/api.ts (the existing Wails shim) or wailsjs/go/models (Wails-generated).
export type {
  Endpoint,
  NewSessionReq,
  NewSessionResp,
  RelayConfig,
  RelayMe,
  HostInfo,
  LoggingConfig,
  LogPreview,
  ClipboardPastePayload,
  UpdateState,
  MarkSessionsSeenOpts,
  NotificationRouteData,
} from '../lib/api'

// PluginConfig + sub-types live in wailsjs/go/models, re-export here.
export type { main as PluginModels } from '../../wailsjs/go/models'

// File system types re-exported from wailsjs/go/models so there's one
// source of truth — the Go side regenerates wailsjs/* on changes.
import type { main as _Models } from '../../wailsjs/go/models'
import type { SessionSummary } from '../lib/connection'
export type { SessionSummary } from '../lib/connection'
export type DirEntry = _Models.DirEntry
export type FileContent = _Models.FileContent
export type FileMetaInfo = _Models.FileMetaInfo
export type PeerSpaceStatus = _Models.PeerSpaceStatus
export type PeerConfigSyncStatus = _Models.PeerConfigSyncStatus
export type PeerInvitation = _Models.PeerInvitation
export type PeerMember = _Models.PeerMember
export type PeerConnectionPreview = _Models.PeerConnectionPreview
export type PeerRendezvousConfig = _Models.PeerRendezvousConfig
export type PeerRendezvousStatus = _Models.PeerRendezvousStatus
export type PeerLANConfig = _Models.PeerLANConfig
export type PeerRouteImportResult = _Models.PeerRouteImportResult
export type PeerSessionRouteStatus = _Models.PeerSessionRouteStatus

export interface EnvironmentInfo {
  buildType: string
  platform: string
  arch: string
}

// ----- Capabilities -----
export interface Capabilities {
  localPty: boolean
  autoUpdate: boolean
  pluginHost: boolean
  windowControls: boolean
  systemClipboard: boolean
  notifications: boolean
  fileDialog: boolean
  // True on Wails (where window.go.main.App is populated), false on Web
  // and Capacitor. Distinct from `localPty`: Capacitor also has
  // localPty=false but runs the shared App.vue shell with its own platform
  // bridge. App.vue uses `wailsBindings` to gate the
  // desktop-only boot chain (getEndpoint/getHostInfo/loadRecoverySnapshot
  // …) whose lib/api calls throw when the Wails runtime isn't there.
  wailsBindings: boolean
  // True only inside the Capacitor native wrapper. Web has the same
  // localPty=false/wailsBindings=false shape but lacks native plugins such
  // as Camera and Keyboard.
  capacitor: boolean
}

// ----- Bridges -----
import type { RelayConfig as _RelayConfig, RelayMe as _RelayMe, NewSessionReq as _Req, NewSessionResp as _Resp, ClipboardPastePayload as _Clip, UpdateState as _UpdateState, MarkSessionsSeenOpts as _MarkSessionsSeenOpts, NotificationRouteData as _NotificationRouteData, RelaySessionRow as _RelaySessionRow, SignOutOthersResult as _SignOutOthersResult } from '../lib/api'

export interface PairingConsumeResult {
  relay_url: string
  session_token: string
  expires_at: number
  user: { id: string; email: string }
  // realm_id / home_instance_url mirror the fields the OPAQUE login finalize
  // step already returns (see opaqueLogin in capacitor.ts) so pairing and
  // password login populate RelayConfig identically. '' when the relay
  // omitted them (older relay versions).
  realm_id: string
  home_instance_url: string
}

export interface RelayBridge {
  load(): Promise<_RelayConfig | null>
  save(cfg: _RelayConfig): Promise<void>
  clear(): Promise<void>
  fetchMe(): Promise<_RelayMe>
  setUplinkPaused?(paused: boolean): Promise<void>
  consumePairing?(relayBase: string, token: string, wrapKey?: Uint8Array): Promise<PairingConsumeResult>
  /** Mobile-only. POST {url}/api/auth/login with {email, password}; on success
   *  writes the returned session token + email into the persisted RelayConfig
   *  and stores the password under a separate Keychain key for one-tap
   *  re-login. Throws Error with one of these messages:
   *    'invalid_credentials' | 'rate_limited' | 'cannot_reach_relay' |
   *    'http_<status>'. */
  login?(url: string, email: string, password: string, allowInsecure: boolean): Promise<void>
  /** Web/Capacitor. POST /api/auth/logout best-effort and clear the local
   *  session token + unlocked account key. Capacitor preserves url +
   *  last_email + saved password so the next login is one tap. */
  logout?(): Promise<void>
  /** Mobile-only. Reads the saved password from Keychain. Returns '' when
   *  nothing is stored. */
  loadSavedPassword?(): Promise<string>
}

export interface RemoteSession {
  session_id: string
  host_id: string
  host: string
  user: string
  title: string
  cwd?: string
  cols: number
  rows: number
  /** Unix seconds when the session was first opened (PTY fork time).
   *  Stable for the lifetime of the session. Used as the primary sort key
   *  in the host group list so rows don't reshuffle as activity changes. */
  started_at?: number
  remote_permission?: string
  task_state?: 'idle' | 'running' | 'waiting_input' | 'completed' | 'failed' | 'disconnected' | 'closed'
  current_command?: string
  command_started_at?: number
  command_ended_at?: number
  command_duration_ms?: number
  command_exit_code?: number
  last_output_at?: number
  type?: string
  summary?: SessionSummary
  /** Per-user unread flag — computed by the relay from attention_at vs seen_at vs
   *  subscriberCount. Local-only sessions (not uplinked) leave this undefined. */
  unread?: boolean
  /** Unix seconds of the session's last attention-worthy transition
   *  (waiting_input, or non-shell completed/failed). 0/undefined = none pending. */
  attention_at?: number
  /** Base64 (std) AEAD envelope over {title, cwd, command,
   *  current_command} sealed by the agent under HKDF(account_key,
   *  session_uuid). Empty / absent for sessions whose agent did not
   *  have an unlocked account_key. See @lib/opaque openSessionFields
   *  for the decrypt path. */
  sealed?: string
  /** Desktop-only accountless route discovered through encrypted Rendezvous. */
  peer_direct?: boolean
}

export interface SessionBridge {
  newSession?(req: _Req): Promise<_Resp>
  closeSession(sessionID: string): Promise<void>
  listShells(): Promise<string[]>
  listRemoteSessions(): Promise<RemoteSession[]>
  /** Optional — mark the given sessions (or all owned sessions) as seen on
   *  the relay. Wails delegates to lib/api (which surfaces the raw HTTP
   *  status on failure). Capacitor posts directly to /api/sessions/seen
   *  with Bearer auth and throws `'relay_unauthorized'` on HTTP 401. */
  markSessionsSeen?(opts: _MarkSessionsSeenOpts): Promise<void>
  /** Read the persisted pin list. Wails reads from appConfig; Capacitor/
   *  Web from localStorage. */
  getPins(): Promise<string[]>
  /** Persist the pin list. Wails writes to appConfig (which triggers a
   *  prefs push server-side); Capacitor/Web write to localStorage and
   *  notify the prefsSync engine locally. */
  setPins(ids: string[]): Promise<void>
  /** Relay account sessions for Settings -> Signed-in devices. */
  listRelaySessions?(): Promise<_RelaySessionRow[]>
  revokeRelaySession?(idHash: string): Promise<void>
  signOutOtherRelaySessions?(): Promise<_SignOutOthersResult>
  /** Remote profile launch. This asks a specific connected desktop (by
   *  host_id, from RemoteSession.host_id) to fork a session from one of its
   *  own saved profiles (by profile_id, from ProfileView.id) and resolves
   *  with the new session_id on success. Capacitor uses the Relay account
   *  route; Wails can use an authenticated accountless Peer route.
   *
   *  One round trip, no request retry: rejects after a 30s timeout (matching the
   *  relay's own request_in_flight TTL headroom) with Error('timeout'), and
   *  the caller must not resend — a retried "start a shell" that actually
   *  succeeded the first time leaves an orphan process nobody asked for. On
   *  a desktop-refused request, rejects with an Error whose message is one
   *  of the closed set of wire error codes documented on
   *  SessionCreatedPayload.Error in internal/proto/frame.go (plus
   *  'relay_not_configured' when no relay session exists locally).
   *
   *  The Capacitor implementation opens a brand-new WebSocket to the relay's /client
   *  endpoint for every call, rather than reusing one connection across
   *  requests (there is no existing attached connection to ride when the
   *  phone isn't attached to anything, unlike the FS request path's
   *  pendingFSRequests). That has a real benefit: a late reply for a
   *  timed-out request cannot land on a newer request, because the socket
   *  and its closure die together. But it also means the relay's per-client
   *  in-flight bound (internal/relay/session_create_router.go's
   *  hasOutstandingRequest, keyed on the requesting client_conn.go
   *  connection's own targetedOut channel) CANNOT fire against this client
   *  — every call is a distinct connection, so two concurrent taps look
   *  like two different clients to the relay, and so does
   *  duplicate_request_id (each attempt mints its own fresh request_id).
   *  The in-app double-tap guard (the `creating` ref in
   *  SettingsProfilesMobile.vue) is therefore the ONLY defence against
   *  duplicate requests from this client, not a belt-and-suspenders layer
   *  on top of a relay-side one. Each open request also holds one of the
   *  account key's MaxConnectionsPerKey connection slots (shared with the
   *  sidebar's session list and any attached terminals) for up to 30s —
   *  bounded to one such slot at a time by that same JS guard, but worth
   *  knowing if connection-limit errors ever show up here. The Wails Peer
   *  implementation similarly owns one temporary terminal route, but sends
   *  no driver claim or terminal input and closes it after the response. */
  createSessionWithProfile?(hostID: string, profileID: string): Promise<string>
}

export interface SystemBridge {
  showNotification(title: string, body: string, data?: _NotificationRouteData): Promise<void>
  getClipboardPaste(): Promise<_Clip>
  // Native shells should provide this so clipboard writes are not subject to
  // an embedded WebView's focus and transient-user-activation policy.
  setClipboardText?(text: string): Promise<void>
  pickLogFilePath?(): Promise<string>
  openExternalURL(url: string): Promise<void>
  getEnvironment(): Promise<EnvironmentInfo | null>
  // Version string like "v0.3.19", or "dev" for unbuilt / non-desktop runs.
  getAppVersion(): Promise<string>
  // Mobile/browser-only helper for dismissing the on-screen keyboard.
  // TerminalView also blurs xterm locally; this hook lets native shells
  // such as Capacitor call their keyboard API when available.
  hideSoftKeyboard?(): Promise<void>
  // Window control surface — optional, gated by caps.windowControls.
  windowMinimize?(): Promise<void>
  windowShow?(): Promise<void>
  windowUnminimize?(): Promise<void>
  windowToggleMaximize?(): Promise<void>
  windowIsMaximized?(): Promise<boolean>
  // windowSetTitle updates the OS window title (macOS title bar etc).
  // Undefined on non-desktop platforms; callers must null-check.
  windowSetTitle?(title: string): Promise<void>
  quit?(): Promise<void>
}

export interface EventBus {
  on(event: string, handler: (data: unknown) => void): () => void
  emit(event: string, data: unknown): void
}

export interface UpdaterBridge {
  getState(): Promise<_UpdateState>
  checkUpdate(): Promise<void>
  startDownload(): Promise<void>
  downloadVersion(tag: string): Promise<void>
  installUpdate(): Promise<void>
}

export interface PluginHostBridge {
  getPluginConfig(): Promise<_Models.PluginConfig>
  setPluginConfig(cfg: _Models.PluginConfig): Promise<void>
  fs: {
    listDir(path: string): Promise<DirEntry[]>
    watchDir(path: string): Promise<number>      // returns watch id
    unwatchDir(id: number): Promise<void>         // takes watch id
    readFile(path: string, maxBytes?: number): Promise<FileContent>
    fileMeta(path: string): Promise<FileMetaInfo>
    openExternal(path: string): Promise<void>
    /** Returns a same-origin URL that resolves to the file via the Wails
     *  AssetServer.Handler at /pluginfs/<base64.URLEncoding(path)>. The URL
     *  is stable for the lifetime of the file path; no expiry. */
    assetUrlFor(path: string): string
    /** Atomic write via tmp+rename. expectedModTime=0 disables CAS;
     *  createIfMissing=true allows creating a non-existent target. */
    writeFile(path: string, data: number[] | Uint8Array, expectedModTime: number, createIfMissing: boolean): Promise<FileMetaInfo>
    createFile(path: string): Promise<FileMetaInfo>
    rename(from: string, to: string): Promise<FileMetaInfo>
    remove(path: string, recursive: boolean): Promise<void>
    mkdir(path: string): Promise<FileMetaInfo>
    trash(path: string): Promise<void>
  }
}

import type { QuickTemplate } from '../lib/templates'

export interface TemplateBridge {
  load(): Promise<QuickTemplate[]>
  save(list: QuickTemplate[]): Promise<void>
  clear(): Promise<void>
  // The user can hide the whole template bar in Settings → Templates. Each
  // platform persists the flag in whatever store fits (capacitor uses
  // localStorage, wails uses appConfig).
  loadHidden(): Promise<boolean>
  saveHidden(hidden: boolean): Promise<void>
}

import type { AuxKey } from '../lib/auxKeys'
import type { DirectClientOptions } from '../lib/directClient'
import type { NativeDirectClientOptions } from '../lib/nativeDirectClient'
import type { DirectTransport } from '../lib/connection'

export interface AuxKeyBridge {
  load(): Promise<AuxKey[]>
  save(list: AuxKey[]): Promise<void>
  clear(): Promise<void>
}

export interface DirectConnectionBridge {
  load(): Promise<boolean>
  save(enabled: boolean): Promise<void>
  /** Desktop injects Go/Pion; browser/mobile omit this and use WebRTC. */
  createTransport?: (options: DirectClientOptions) => DirectTransport
}

export interface CreatePeerInvitationsRequest {
  count: number
  valid_for_hours: number
  permission: 'view' | 'control' | 'full'
  allowed_session_ids: string[]
  can_invite: boolean
  can_sync_secrets: boolean
}

export interface PeerQuickTunnelStatus {
  running: boolean
  starting: boolean
  public_url?: string
  local_origin?: string
  managed_supported?: boolean
  managed_installed?: boolean
  system_available?: boolean
  managed_version?: string
  managed_asset?: string
  managed_sha256?: string
}

export interface PeerTrafficRow {
  day: string
  route: 'direct' | 'quick_tunnel' | 'lan' | 'gateway'
  bytes_sent: number
  bytes_received: number
  records_sent: number
  records_received: number
}

// Peer trust is optional until each platform has its required secure identity
// backend. Components must gate on platform.peer rather than importing Wails
// bindings or silently falling back to Relay credentials.
export interface PeerBridge {
  status(): Promise<PeerSpaceStatus>
  configSyncStatus(): Promise<PeerConfigSyncStatus>
  acceptPendingConfig(): Promise<PeerConfigSyncStatus>
  discardPendingConfig(): Promise<PeerConfigSyncStatus>
  createSpace(): Promise<PeerSpaceStatus>
  /** Desktop-only encrypted identity recovery. Browser keys are non-exportable. */
  exportTrustBackup?(passphrase: string): Promise<string>
  importTrustBackup?(encoded: string, passphrase: string): Promise<PeerSpaceStatus>
  previewConnectionBundle(raw: string): Promise<PeerConnectionPreview>
  joinSpace(req: { connection_bundle: string; expected_fingerprint: string }): Promise<PeerSpaceStatus>
  importConnectionBundle(raw: string): Promise<PeerRouteImportResult>
  createInvitations(req: CreatePeerInvitationsRequest): Promise<PeerInvitation[]>
  listInvitations(): Promise<PeerInvitation[]>
  revokeInvitation(inviteID: string): Promise<void>
  revokeInvitationBatch(batchID: string): Promise<void>
  listMembers(): Promise<PeerMember[]>
  revokeMember(peerID: string): Promise<void>
  getRendezvousConfig(): Promise<PeerRendezvousConfig>
  setRendezvousConfig(req: {
    mode: 'disabled' | 'official' | 'custom'
    url: string
    stun_mode: 'default' | 'custom' | 'disabled'
    stun_urls: string[]
    turn_enabled: boolean
    turn_urls: string[]
    turn_username: string
    turn_credential: string
  }): Promise<void>
  getRendezvousStatus(): Promise<PeerRendezvousStatus>
  reconnectRendezvous(): Promise<PeerRendezvousStatus>
  getLANConfig?(): Promise<PeerLANConfig>
  setLANConfig?(req: {
    lan_only: boolean
    enabled: boolean
    auto_discovery: boolean
    advertise_host: string
    port: number
    routes: Array<{ host: string; port: number; fingerprint: string }>
  }): Promise<void>
  syncConfigNow(): Promise<PeerConfigSyncStatus>
  listSessions?(): Promise<RemoteSession[]>
  getSessionRouteStatus?(sessionID: string): Promise<PeerSessionRouteStatus>
  createSessionTransport?: (options: NativeDirectClientOptions) => DirectTransport
  /** Desktop-only, device-local encrypted-record aggregates. */
  getTraffic?(from: string, to: string): Promise<PeerTrafficRow[]>
  /** Desktop host controls. Clients without a local gateway leave these absent. */
  getQuickTunnelStatus?(): Promise<PeerQuickTunnelStatus>
  installCloudflared?(): Promise<PeerQuickTunnelStatus>
  startQuickTunnel?(): Promise<PeerQuickTunnelStatus>
  stopQuickTunnel?(): Promise<void>
  createConnectionBundle?(invitationToken: string): Promise<string>
  /** Quick Tunnel carries terminal bytes through Cloudflare. Automatic
   * fallback stays disabled until this device records explicit consent. */
  getQuickTunnelFallbackConsent(): Promise<boolean>
  setQuickTunnelFallbackConsent(allowed: boolean): Promise<void>
}

// WidgetBridge drives the companion window ("桌面挂件" / Desk Widget): a second process of the
// same executable that owns a frameless always-on-top window. Only the Wails
// platform implements it — web and Capacitor leave it undefined.
export interface WidgetBridge {
  /** Spawns the companion window if it is not already running. Idempotent. */
  start(): Promise<void>
  /** Terminates the companion window. Idempotent. */
  stop(): Promise<void>
  /** Pushes a serialized WidgetState snapshot. No-op when the widget is stopped. */
  pushState(json: string): Promise<void>
}

export interface ServicePreviewMapping {
  serviceId: string
  clientTicket: string
  clientToHostKey: Uint8Array
  hostToClientKey: Uint8Array
  port: number
  pathPrefix?: string
  peerAttemptId?: string
}

export interface ServicePreviewStartRequest {
  mappings?: ServicePreviewMapping[]
  /** Legacy single-mapping fields kept for native plugin compatibility. */
  serviceId?: string
  clientTicket?: string
  clientToHostKey?: Uint8Array
  hostToClientKey?: Uint8Array
}

export interface ServicePreviewStartResult {
  id: string
  url: string
}

export interface ServicePreviewRebindRequest {
  gatewayId: string
  mappingIndex: number
  serviceId: string
  clientTicket: string
  clientToHostKey: Uint8Array
  hostToClientKey: Uint8Array
  peerAttemptId?: string
}

export interface ServicePreviewBridge {
  /** Native desktop gateway supports multiple path-to-port mappings. */
  supportsMappings?: boolean
  start(req: ServicePreviewStartRequest): Promise<ServicePreviewStartResult>
  stop(id: string): Promise<void>
  /**
   * Reattach a dead mapping's pipe using a freshly opened lease, keeping the
   * gateway URL alive. Present only on the Wails desktop bridge; absent on
   * native mobile (self-heal there is a native-plugin concern) and web.
   */
  rebind?(req: ServicePreviewRebindRequest): Promise<void>
}

export interface Platform {
  caps: Capabilities
  relay: RelayBridge
  sessions: SessionBridge
  system: SystemBridge
  events: EventBus
  templates: TemplateBridge
  auxKeys: AuxKeyBridge
  directConnection: DirectConnectionBridge
  peer?: PeerBridge
  updater?: UpdaterBridge
  pluginHost?: PluginHostBridge
  deskWidget?: WidgetBridge
  servicePreview?: ServicePreviewBridge
}
