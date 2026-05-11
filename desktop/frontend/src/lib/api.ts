// Wails Go bindings shim. The Wails runtime injects window.go.main.App.<Method>
// at startup; we expose typed wrappers so the rest of the app doesn't have to
// reach into globals directly.

export interface Endpoint {
  url: string;
  token: string;
}

export interface NewSessionReq {
  command: string;
  args?: string[];
  cwd?: string;
  cols?: number;
  rows?: number;
}

export interface NewSessionResp {
  session_id: string;
}

export interface RelayConfig {
  url: string;
  token: string;
  allow_insecure_relay: boolean;
  remote_permission: string;
  connected: boolean;
}

export interface HostInfo {
  host_id: string;
  host: string;
  user: string;
}

// Mirrors desktop/updater.go UpdateState. Field names are snake_case from
// Wails JSON marshaling; we match exactly.
export interface UpdateState {
  current: string;
  latest: string;
  available: boolean;
  notes: string;
  checking: boolean;
  last_check_at: number;
  downloading: boolean;
  download_pct: number;
  ready: boolean;
  error: string;
  asset_url: string;
  asset_size: number;
  download_dir: string;
}

interface AppBindings {
  GetEndpoint(): Promise<Endpoint>;
  GetHostInfo(): Promise<HostInfo>;
  NewSession(req: NewSessionReq): Promise<NewSessionResp>;
  CloseSession(sessionID: string): Promise<void>;
  ListShells(): Promise<string[]>;
  GetRelayConfig(): Promise<RelayConfig>;
  SetRelayConfig(cfg: RelayConfig): Promise<void>;
  GetUpdateState(): Promise<UpdateState>;
  CheckUpdate(): Promise<void>;
  StartDownload(): Promise<void>;
  InstallUpdate(): Promise<void>;
  GetAutoCheckUpdates(): Promise<boolean>;
  SetAutoCheckUpdates(enabled: boolean): Promise<void>;
}

declare global {
  interface Window {
    go?: {
      main?: {
        App?: AppBindings;
      };
    };
  }
}

function bindings(): AppBindings {
  const b = window.go?.main?.App;
  if (!b) throw new Error("Wails bindings not ready");
  return b;
}

export function getEndpoint(): Promise<Endpoint> {
  return bindings().GetEndpoint();
}

export function newSession(req: NewSessionReq): Promise<NewSessionResp> {
  return bindings().NewSession(req);
}

export function closeSession(sessionId: string): Promise<void> {
  return bindings().CloseSession(sessionId);
}

export function listShells(): Promise<string[]> {
  return bindings().ListShells();
}

export function getRelayConfig(): Promise<RelayConfig> {
  return bindings().GetRelayConfig();
}

export function setRelayConfig(cfg: {
  url: string;
  token: string;
  allow_insecure_relay?: boolean;
  remote_permission?: string;
}): Promise<void> {
  return bindings().SetRelayConfig({
    url: cfg.url,
    token: cfg.token,
    allow_insecure_relay: cfg.allow_insecure_relay ?? false,
    remote_permission: cfg.remote_permission ?? "full",
    connected: false,
  });
}

export function getHostInfo(): Promise<HostInfo> {
  return bindings().GetHostInfo();
}

export function getUpdateState(): Promise<UpdateState> {
  return bindings().GetUpdateState();
}

export function checkUpdate(): Promise<void> {
  return bindings().CheckUpdate();
}

export function startDownload(): Promise<void> {
  return bindings().StartDownload();
}

export function installUpdate(): Promise<void> {
  return bindings().InstallUpdate();
}

export function getAutoCheckUpdates(): Promise<boolean> {
  return bindings().GetAutoCheckUpdates();
}

export function setAutoCheckUpdates(enabled: boolean): Promise<void> {
  return bindings().SetAutoCheckUpdates(enabled);
}
