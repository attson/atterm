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
}

// ----- Bridges -----
import type { RelayConfig as _RelayConfig, RelayMe as _RelayMe, NewSessionReq as _Req, NewSessionResp as _Resp, ClipboardPastePayload as _Clip, UpdateState as _UpdateState } from '../lib/api'

export interface PairingConsumeResult {
  relay_url: string
  api_token: string
  user: { id: string; email: string }
}

export interface RelayBridge {
  load(): Promise<_RelayConfig | null>
  save(cfg: _RelayConfig): Promise<void>
  clear(): Promise<void>
  fetchMe(): Promise<_RelayMe>
  setUplinkPaused?(paused: boolean): Promise<void>
  consumePairing?(relayBase: string, token: string): Promise<PairingConsumeResult>
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
}

export interface SessionBridge {
  newSession?(req: _Req): Promise<_Resp>
  closeSession(sessionID: string): Promise<void>
  listShells(): Promise<string[]>
  listRemoteSessions(): Promise<RemoteSession[]>
}

export interface SystemBridge {
  showNotification(title: string, body: string): Promise<void>
  getClipboardPaste(): Promise<_Clip>
  pickLogFilePath?(): Promise<string>
  openExternalURL(url: string): Promise<void>
  getEnvironment(): Promise<EnvironmentInfo | null>
  // Window control surface — optional, gated by caps.windowControls.
  windowMinimize?(): Promise<void>
  windowToggleMaximize?(): Promise<void>
  windowIsMaximized?(): Promise<boolean>
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
  }
}

import type { QuickTemplate } from '../lib/templates'

export interface TemplateBridge {
  load(): Promise<QuickTemplate[]>
  save(list: QuickTemplate[]): Promise<void>
  clear(): Promise<void>
}

import type { AuxKey } from '../lib/auxKeys'

export interface AuxKeyBridge {
  load(): Promise<AuxKey[]>
  save(list: AuxKey[]): Promise<void>
  clear(): Promise<void>
}

export interface Platform {
  caps: Capabilities
  relay: RelayBridge
  sessions: SessionBridge
  system: SystemBridge
  events: EventBus
  templates: TemplateBridge
  auxKeys: AuxKeyBridge
  updater?: UpdaterBridge
  pluginHost?: PluginHostBridge
}
