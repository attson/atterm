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
  connected: boolean;
}

export interface HostInfo {
  host_id: string;
  host: string;
  user: string;
}

interface AppBindings {
  GetEndpoint(): Promise<Endpoint>;
  GetHostInfo(): Promise<HostInfo>;
  NewSession(req: NewSessionReq): Promise<NewSessionResp>;
  CloseSession(sessionID: string): Promise<void>;
  ListShells(): Promise<string[]>;
  GetRelayConfig(): Promise<RelayConfig>;
  SetRelayConfig(cfg: RelayConfig): Promise<void>;
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

export function setRelayConfig(cfg: { url: string; token: string }): Promise<void> {
  return bindings().SetRelayConfig({ ...cfg, connected: false });
}

export function getHostInfo(): Promise<HostInfo> {
  return bindings().GetHostInfo();
}
