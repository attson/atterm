// Wails Go bindings shim. The Wails runtime injects window.go.main.App.<Method>
// at startup; we expose typed wrappers so the rest of the app doesn't have to
// reach into globals directly.

import { t } from "../i18n";
import type { PresetId } from "./taskState";
import {
  InitializeNotifications,
  IsNotificationAvailable,
  CheckNotificationAuthorization,
  RequestNotificationAuthorization,
  SendNotification,
} from "../../wailsjs/runtime/runtime";

export type TaskGroupBy = "host" | "state";

export interface Endpoint {
  url: string;
  session_token: string;
}

export type LocalePreference = "system" | "en" | "zh-CN";

export interface NewSessionReq {
  command: string;
  args?: string[];
  cwd?: string;
  cols?: number;
  rows?: number;
  // Filled by classifyAIKind() in lib/aiKind.ts when the user-typed command
  // matches a known AI CLI. Empty value disables sniff + resume.
  ai_kind?: "claude" | "codex" | "aider" | "";
  // Round-tripped from the previous run's snapshot during executeRestore.
  // Not used by Go to spawn the child — only the frontend injects the
  // resume command after prompt-ready.
  initial_ai_session_id?: string;
}

export interface NewSessionResp {
  session_id: string;
}

export interface RecoveryAIInfo {
  kind: "claude" | "codex" | "aider";
  session_id?: string;
  captured_at_unix?: number;
}

export interface RecoveryPaneSnapshot {
  slot: number;
  shell: string;
  shell_args?: string[];
  last_cwd?: string;
  session_type?: string;
  last_command_line?: string;
  title?: string;
  ai?: RecoveryAIInfo;
}

export interface RecoveryTabSnapshot {
  id: string;
  layout: "single" | "vertical" | "horizontal" | "grid2x2";
  active_pane_idx: number;
  col_ratio: number;
  row_ratio: number;
  panes: RecoveryPaneSnapshot[];
}

export interface RecoverySnapshot {
  version: number;
  host_id: string;
  clean_shutdown: boolean;
  saved_at_unix: number;
  active_tab_id?: string;
  tabs: RecoveryTabSnapshot[];
}

export interface RelayConfig {
  url: string;
  token: string;
  // Unix-seconds expiry of `token` when it was minted as a relay session
  // token (e.g. via /api/pair/consume). 0 means "unknown" — treat `token`
  // as an opaque, long-lived credential. Always present on the wire so the
  // frontend can branch on `> 0` without optional-chaining.
  session_expires_at: number;
  allow_insecure_relay: boolean;
  // disable_e2ee, when true, turns off this desktop's agent-side sealing.
  // The account_key stays loaded so cross-desktop decrypt keeps working;
  // only outbound OUT / META / SessionInfo / CommandEventPayload sealing
  // is suppressed. Intended for testing the unsealed fallback path.
  // Optional in the type so iOS Capacitor's local RelayConfig fixtures
  // (which never run an agent) can omit it; Go always returns a
  // concrete bool over the wire, never undefined.
  disable_e2ee?: boolean;
  remote_permission: string;
  // Email cached from the most recent successful LoginRemoteRelay. Used
  // by Settings → Relay to prefill the email field on reopen. Read-only
  // from the frontend's perspective — setRelayConfig ignores it; only
  // loginRemoteRelay writes it.
  last_email: string;
  connected: boolean;
}

export interface RelayMe {
  user_id: string;
  email: string;
}

export type WebhookFormat = "feishu" | "generic";

export interface Webhook {
  id: string;
  name: string;
  url: string;
  format: WebhookFormat;
  created_at: string;
}

export interface CreateWebhookReq {
  name: string;
  url: string;
  format: WebhookFormat;
  allow_insecure: boolean;
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

export interface FeishuCredentials {
  AppID: string;
  AppSecret: string;
  EncryptKey: string;
  VerifyToken: string;
}

export interface FeishuStatusResp {
  enabled: boolean;
  mode: "local" | "relay";
  bound: boolean;
  open_id: string;
  disabled: boolean;
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
  SetRelayDisableE2EE(disabled: boolean): Promise<void>;
  SetUplinkPaused(paused: boolean): Promise<void>;
  GetUplinkHealth(): Promise<ConnHealthSnapshot>;
  LoginRemoteRelay(relayURL: string, email: string, password: string, allowInsecure: boolean): Promise<void>;
  RegisterRemoteRelay(relayURL: string, email: string, password: string, claimToken: string, allowInsecure: boolean): Promise<void>;
  HasAccountKey(): Promise<boolean>;
  GetAccountKey(): Promise<string>;
  ProbeRelayVersion(arg1: string): Promise<void>;
  FetchRelayMe(): Promise<RelayMe>;
  ListWebhooks(): Promise<Webhook[]>;
  CreateWebhook(req: CreateWebhookReq): Promise<Webhook>;
  DeleteWebhook(id: string): Promise<void>;
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
  LoadRecoverySnapshot(): Promise<RecoverySnapshot>;
  SaveRecoverySnapshot(payload: string): Promise<void>;
  DiscardRecoverySnapshot(): Promise<void>;
  GetRecoveryDialogEnabled(): Promise<boolean>;
  SetRecoveryDialogEnabled(enabled: boolean): Promise<void>;
  GetWebglRendererEnabled(): Promise<boolean>;
  SetWebglRendererEnabled(enabled: boolean): Promise<void>;
  GetCommandNotifyThresholdSeconds(): Promise<number>;
  SetCommandNotifyThresholdSeconds(seconds: number): Promise<void>;
  BroadcastCommandFinished(sessionId: string, exitCode: number, elapsedMs: number, label: string): Promise<void>;
  GetDiagnostics(userAgent: string): Promise<DiagnosticsPayload>;
  ExportDiagnostics(content: string): Promise<string>;
  GetFeishuStatus(): Promise<FeishuStatusResp>;
  SetFeishuCredentials(c: FeishuCredentials): Promise<void>;
  BeginFeishuPair(): Promise<string>;
  DeleteFeishuBinding(): Promise<void>;
  GetQuickTemplates(): Promise<import('./templates').QuickTemplate[]>;
  SetQuickTemplates(list: import('./templates').QuickTemplate[]): Promise<void>;
  GetTaskPreset(): Promise<string>;
  SetTaskPreset(preset: PresetId): Promise<void>;
  GetTaskGroupBy(): Promise<string>;
  SetTaskGroupBy(groupBy: TaskGroupBy): Promise<void>;
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
let notificationRuntimeReady: Promise<boolean> | undefined;
let notificationID = 0;

export function __setBindingsForTest(b: AppBindings | undefined): void {
  _bindingsOverride = b;
}

function resetNotificationRuntime(): void {
  notificationRuntimeReady = undefined;
  notificationID = 0;
}

export function __resetNotificationRuntimeForTest(): void {
  resetNotificationRuntime();
}

function bindings(): AppBindings {
  if (_bindingsOverride) return _bindingsOverride;
  const b = window.go?.main?.App;
  if (!b) throw new Error(t("app.wailsBindingsNotReady"));
  return b;
}

async function ensureNotificationRuntimeReady(): Promise<boolean> {
  if (!notificationRuntimeReady) {
    notificationRuntimeReady = (async () => {
      try {
        if (!(await IsNotificationAvailable())) return false;
        await InitializeNotifications();
        if (await CheckNotificationAuthorization()) return true;
        return await RequestNotificationAuthorization();
      } catch {
        return false;
      }
    })();
  }
  return notificationRuntimeReady;
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
  session_expires_at?: number;
  allow_insecure_relay?: boolean;
  disable_e2ee?: boolean;
  remote_permission?: string;
}): Promise<void> {
  return bindings().SetRelayConfig({
    url: cfg.url,
    token: cfg.token,
    session_expires_at: cfg.session_expires_at ?? 0,
    allow_insecure_relay: cfg.allow_insecure_relay ?? false,
    disable_e2ee: cfg.disable_e2ee ?? false,
    remote_permission: cfg.remote_permission ?? "full",
    last_email: "",
    connected: false,
  });
}

// setRelayDisableE2EE flips the agent-side seal toggle directly without
// touching URL / token / permission — used by Settings when the user
// wants the change to take effect immediately, not on the next "Save".
export function setRelayDisableE2EE(disabled: boolean): Promise<void> {
  return bindings().SetRelayDisableE2EE(disabled);
}

export function setUplinkPaused(paused: boolean): Promise<void> {
  return bindings().SetUplinkPaused(paused);
}

// loginRemoteRelay drives the OPAQUE login flow on the Go side. The Wails
// method completes the protocol round-trip, persists the returned session
// token via SetRelayConfig, unlocks the account_key into App memory, and
// restarts the uplink — callers only need to refresh GetRelayConfig()
// afterwards.
export function loginRemoteRelay(relayURL: string, email: string, password: string, allowInsecure: boolean): Promise<void> {
  return bindings().LoginRemoteRelay(relayURL, email, password, allowInsecure);
}

// registerRemoteRelay drives the OPAQUE registration flow on the Go side.
// Same persistence semantics as loginRemoteRelay; claimToken is optional
// (supply the plaintext token printed by `atterm-relay` bootstrap to also
// promote the new user to admin, otherwise pass "").
export function registerRemoteRelay(relayURL: string, email: string, password: string, claimToken: string, allowInsecure: boolean): Promise<void> {
  return bindings().RegisterRemoteRelay(relayURL, email, password, claimToken, allowInsecure);
}

// hasAccountKey reports whether the E2EE account_key is currently unlocked
// in App memory. False after app restart (key is in-memory only in v1) —
// the frontend uses this to decide between "unlock" and full-login prompts.
export function hasAccountKey(): Promise<boolean> {
  return bindings().HasAccountKey();
}

// getAccountKey returns the unlocked account_key as a base64 std string,
// or empty string when locked / no user. The Wails platform layer
// caches this in JS memory so MetaPayload.Sealed decrypt in the hot
// path stays synchronous. See platform/wails.ts.
export function getAccountKey(): Promise<string> {
  return bindings().GetAccountKey();
}

// probeRelayVersion calls the Wails ProbeRelayVersion method on the Go side
// to verify the URL points at an atterm relay. Throws on probe failure.
export function probeRelayVersion(url: string): Promise<void> {
  return bindings().ProbeRelayVersion(url);
}

// ConnHealthSnapshot mirrors internal/connhealth.Snapshot. Returned by the
// Wails GetUplinkHealth method and rendered by the ConnHealthPill / drawer.
export interface ConnHealthSnapshot {
  state: "closed" | "connecting" | "connected" | "reconnecting";
  rtt: {
    last_ms: number | null;
    p50_ms: number | null;
    p95_ms: number | null;
  };
  rtt_samples: Array<{ at_ms: number; rtt_ms: number }>;
  reconnect: {
    count_last_hour: number;
    last_at_ms: number | null;
    last_reason: string;
    history: Array<{ at_ms: number; reason: string; duration_ms: number }>;
  };
  bytes: {
    in_per_sec: number;
    out_per_sec: number;
  };
  seq_gaps: number;
}

export function getUplinkHealth(): Promise<ConnHealthSnapshot> {
  return bindings().GetUplinkHealth();
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

export async function showNotification(title: string, body: string): Promise<void> {
  if (await ensureNotificationRuntimeReady()) {
    try {
      notificationID += 1;
      await SendNotification({
        id: `atterm-${Date.now()}-${notificationID}`,
        title,
        body,
      });
      return;
    } catch {
      resetNotificationRuntime();
    }
  }
  return bindings().ShowNotification(title, body);
}

export function getShellIntegrationEnabled(): Promise<boolean> {
  return bindings().GetShellIntegrationEnabled();
}

export function setShellIntegrationEnabled(enabled: boolean): Promise<void> {
  return bindings().SetShellIntegrationEnabled(enabled);
}

export function loadRecoverySnapshot(): Promise<RecoverySnapshot> {
  return bindings().LoadRecoverySnapshot();
}

export function saveRecoverySnapshot(snap: RecoverySnapshot): Promise<void> {
  return bindings().SaveRecoverySnapshot(JSON.stringify(snap));
}

export function discardRecoverySnapshot(): Promise<void> {
  return bindings().DiscardRecoverySnapshot();
}

export function getRecoveryDialogEnabled(): Promise<boolean> {
  return bindings().GetRecoveryDialogEnabled();
}

export function setRecoveryDialogEnabled(enabled: boolean): Promise<void> {
  return bindings().SetRecoveryDialogEnabled(enabled);
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

export function listWebhooks(): Promise<Webhook[]> {
  return bindings().ListWebhooks();
}

export function createWebhook(req: CreateWebhookReq): Promise<Webhook> {
  return bindings().CreateWebhook(req);
}

export function deleteWebhook(id: string): Promise<void> {
  return bindings().DeleteWebhook(id);
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

export function getTaskGroupBy(): Promise<string> {
  return bindings().GetTaskGroupBy();
}

export function setTaskGroupBy(groupBy: TaskGroupBy): Promise<void> {
  return bindings().SetTaskGroupBy(groupBy);
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

export function getFeishuStatus(): Promise<FeishuStatusResp> {
  return bindings().GetFeishuStatus();
}

export function setFeishuCredentials(c: FeishuCredentials): Promise<void> {
  return bindings().SetFeishuCredentials(c);
}

export function beginFeishuPair(): Promise<string> {
  return bindings().BeginFeishuPair();
}

export function deleteFeishuBinding(): Promise<void> {
  return bindings().DeleteFeishuBinding();
}
