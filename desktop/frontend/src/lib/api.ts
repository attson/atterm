// Wails Go bindings shim. The Wails runtime injects window.go.main.App.<Method>
// at startup; we expose typed wrappers so the rest of the app doesn't have to
// reach into globals directly.

import { t } from "../i18n";
import type { PresetId } from "./taskState";

export interface Endpoint {
  url: string;
  token: string;
}

export type LocalePreference = "system" | "en" | "zh-CN";

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

export interface RelayMe {
  user_id: string;
  email: string;
}

export interface DiagnosticsPayload {
  generated_at: string;
  app_version: string;
  os: string;
  arch: string;
  os_version: string;
  webview_summary: string;
  user_agent: string;
  relay_url: string;
  relay_status: string;
  relay_token_redacted: string;
  allow_insecure_relay: boolean;
  remote_permission: string;
  uplink_paused: boolean;
  recent_relay_errors: { timestamp: string; message: string }[];
  config: {
    default_shell: string;
    locale: string;
    terminal_theme: string;
    notifications_enabled: boolean;
    shell_integration_enabled: boolean;
    webgl_renderer_enabled: boolean;
    logging_enabled: boolean;
    log_file_path: string;
    auto_check_updates: boolean;
    command_notify_threshold_seconds: number;
  };
}

export interface PairingToken {
  token: string;
  expires_at: number;
  qr_url: string;
}

export interface HostInfo {
  host_id: string;
  host: string;
  user: string;
}

export interface LoggingConfig {
  enabled: boolean;
  path: string;
  effective_path: string;
  dev_dual_output: boolean;
}

export interface LogPreview {
  path: string;
  exists: boolean;
  truncated: boolean;
  content: string;
}

export interface ClipboardPastePayload {
  kind: "none" | "text" | "image";
  text?: string;
  filename?: string;
  content_type?: string;
  data_base64?: string;
  reason?: string;
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
  download_path: string;
}

interface AppBindings {
  GetClipboardPastePayload(): Promise<ClipboardPastePayload>;
  GetEndpoint(): Promise<Endpoint>;
  GetHostInfo(): Promise<HostInfo>;
  NewSession(req: NewSessionReq): Promise<NewSessionResp>;
  CloseSession(sessionID: string): Promise<void>;
  ListShells(): Promise<string[]>;
  GetRelayConfig(): Promise<RelayConfig>;
  SetRelayConfig(cfg: RelayConfig): Promise<void>;
  SetUplinkPaused(paused: boolean): Promise<void>;
  FetchRelayMe(): Promise<RelayMe>;
  CreatePairingToken(): Promise<PairingToken>;
  GetLoggingConfig(): Promise<LoggingConfig>;
  SetLoggingConfig(cfg: LoggingConfig): Promise<void>;
  PickLogFilePath(): Promise<string>;
  GetLogPreview(): Promise<LogPreview>;
  GetTerminalTheme(): Promise<string>;
  SetTerminalTheme(themeID: string): Promise<void>;
  GetLocalePreference(): Promise<LocalePreference>;
  SetLocalePreference(preference: LocalePreference): Promise<void>;
  GetDefaultShell(): Promise<string>;
  SetDefaultShell(shell: string): Promise<void>;
  GetUpdateState(): Promise<UpdateState>;
  CheckUpdate(): Promise<void>;
  StartDownload(): Promise<void>;
  InstallUpdate(): Promise<void>;
  GetAutoCheckUpdates(): Promise<boolean>;
  SetAutoCheckUpdates(enabled: boolean): Promise<void>;
  GetUpdateGHProxyURL(): Promise<string>;
  SetUpdateGHProxyURL(proxyURL: string): Promise<void>;
  ConfirmQuit(): Promise<void>;
  GetNotificationsEnabled(): Promise<boolean>;
  SetNotificationsEnabled(enabled: boolean): Promise<void>;
  ShowNotification(title: string, body: string): Promise<void>;
  GetShellIntegrationEnabled(): Promise<boolean>;
  SetShellIntegrationEnabled(enabled: boolean): Promise<void>;
  GetWebglRendererEnabled(): Promise<boolean>;
  SetWebglRendererEnabled(enabled: boolean): Promise<void>;
  GetCommandNotifyThresholdSeconds(): Promise<number>;
  SetCommandNotifyThresholdSeconds(seconds: number): Promise<void>;
  BroadcastCommandFinished(sessionId: string, exitCode: number, elapsedMs: number, label: string): Promise<void>;
  GetDiagnostics(userAgent: string): Promise<DiagnosticsPayload>;
  ExportDiagnostics(content: string): Promise<string>;
  GetQuickTemplates(): Promise<import('./templates').QuickTemplate[]>;
  SetQuickTemplates(list: import('./templates').QuickTemplate[]): Promise<void>;
  GetTaskPreset(): Promise<string>;
  SetTaskPreset(preset: PresetId): Promise<void>;
  GetTaskSidebarCollapsed(): Promise<boolean>;
  SetTaskSidebarCollapsed(collapsed: boolean): Promise<void>;
  GetTaskSidebarWidth(): Promise<number>;
  SetTaskSidebarWidth(px: number): Promise<void>;
  GetUserHomeDir(): Promise<string>;
  MarkSessionsSeen(ids: string[], all: boolean): Promise<void>;
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

let _bindingsOverride: AppBindings | undefined;

export function __setBindingsForTest(b: AppBindings | undefined): void {
  _bindingsOverride = b;
}

function bindings(): AppBindings {
  if (_bindingsOverride) return _bindingsOverride;
  const b = window.go?.main?.App;
  if (!b) throw new Error(t("app.wailsBindingsNotReady"));
  return b;
}

export function getClipboardPastePayload(): Promise<ClipboardPastePayload> {
  return bindings().GetClipboardPastePayload();
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

export function setUplinkPaused(paused: boolean): Promise<void> {
  return bindings().SetUplinkPaused(paused);
}

export function getLoggingConfig(): Promise<LoggingConfig> {
  return bindings().GetLoggingConfig();
}

export function setLoggingConfig(cfg: {
  enabled: boolean;
  path?: string;
}): Promise<void> {
  return bindings().SetLoggingConfig({
    enabled: cfg.enabled,
    path: cfg.path ?? "",
    effective_path: "",
    dev_dual_output: false,
  });
}

export function pickLogFilePath(): Promise<string> {
  return bindings().PickLogFilePath();
}

export function getLogPreview(): Promise<LogPreview> {
  return bindings().GetLogPreview();
}

export function getTerminalThemePreference(): Promise<string> {
  return bindings().GetTerminalTheme();
}

export function setTerminalThemePreference(themeID: string): Promise<void> {
  return bindings().SetTerminalTheme(themeID);
}

export function getLocalePreference(): Promise<LocalePreference> {
  return bindings().GetLocalePreference();
}

export function setLocalePreference(preference: LocalePreference): Promise<void> {
  return bindings().SetLocalePreference(preference);
}

export function getDefaultShell(): Promise<string> {
  return bindings().GetDefaultShell();
}

export function setDefaultShell(shell: string): Promise<void> {
  return bindings().SetDefaultShell(shell);
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

export function getUpdateGHProxyURL(): Promise<string> {
  return bindings().GetUpdateGHProxyURL();
}

export function setUpdateGHProxyURL(proxyURL: string): Promise<void> {
  return bindings().SetUpdateGHProxyURL(proxyURL);
}

export function confirmQuit(): Promise<void> {
  return bindings().ConfirmQuit();
}

export function getNotificationsEnabled(): Promise<boolean> {
  return bindings().GetNotificationsEnabled();
}

export function setNotificationsEnabled(enabled: boolean): Promise<void> {
  return bindings().SetNotificationsEnabled(enabled);
}

export function showNotification(title: string, body: string): Promise<void> {
  return bindings().ShowNotification(title, body);
}

export function getShellIntegrationEnabled(): Promise<boolean> {
  return bindings().GetShellIntegrationEnabled();
}

export function setShellIntegrationEnabled(enabled: boolean): Promise<void> {
  return bindings().SetShellIntegrationEnabled(enabled);
}

export function getWebglRendererEnabled(): Promise<boolean> {
  return bindings().GetWebglRendererEnabled();
}

export function setWebglRendererEnabled(enabled: boolean): Promise<void> {
  return bindings().SetWebglRendererEnabled(enabled);
}

export function getCommandNotifyThresholdSeconds(): Promise<number> {
  return bindings().GetCommandNotifyThresholdSeconds();
}

export function setCommandNotifyThresholdSeconds(seconds: number): Promise<void> {
  return bindings().SetCommandNotifyThresholdSeconds(seconds);
}

export function broadcastCommandFinished(
  sessionId: string,
  exitCode: number,
  elapsedMs: number,
  label: string,
): Promise<void> {
  return bindings().BroadcastCommandFinished(sessionId, exitCode, elapsedMs, label);
}

// fetchRelayMe calls the Go backend (FetchRelayMe Wails binding) which makes
// an HTTP GET to the configured relay's /api/me endpoint using the stored API
// token. The returned email is held in memory only (SEC-1 — not persisted).
export function fetchRelayMe(): Promise<RelayMe> {
  return bindings().FetchRelayMe();
}

// createPairingToken asks the relay to mint a 5-minute single-use pairing
// token via the desktop's existing API-token-authenticated channel. The
// returned qr_url is the value to encode into the QR image.
export function createPairingToken(): Promise<PairingToken> {
  return bindings().CreatePairingToken();
}

export function getDiagnostics(userAgent: string): Promise<DiagnosticsPayload> {
  return bindings().GetDiagnostics(userAgent);
}

export function exportDiagnostics(content: string): Promise<string> {
  return bindings().ExportDiagnostics(content);
}

export function getQuickTemplates(): Promise<import('./templates').QuickTemplate[]> {
  return bindings().GetQuickTemplates();
}

export function setQuickTemplates(list: import('./templates').QuickTemplate[]): Promise<void> {
  return bindings().SetQuickTemplates(list);
}

export function getTaskPreset(): Promise<string> {
  return bindings().GetTaskPreset();
}

export function setTaskPreset(preset: PresetId): Promise<void> {
  return bindings().SetTaskPreset(preset);
}

export function getTaskSidebarCollapsed(): Promise<boolean> {
  return bindings().GetTaskSidebarCollapsed();
}

export function setTaskSidebarCollapsed(collapsed: boolean): Promise<void> {
  return bindings().SetTaskSidebarCollapsed(collapsed);
}

export function getTaskSidebarWidth(): Promise<number> {
  return bindings().GetTaskSidebarWidth();
}
export function setTaskSidebarWidth(px: number): Promise<void> {
  return bindings().SetTaskSidebarWidth(px);
}

export function getUserHomeDir(): Promise<string> {
  return bindings().GetUserHomeDir();
}

export type MarkSessionsSeenOpts = { ids: string[] } | { all: true };

export function markSessionsSeen(opts: MarkSessionsSeenOpts): Promise<void> {
  if ("all" in opts && opts.all) {
    return bindings().MarkSessionsSeen([], true);
  }
  return bindings().MarkSessionsSeen((opts as { ids: string[] }).ids, false);
}
