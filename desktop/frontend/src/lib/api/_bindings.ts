// Shared Wails-bindings plumbing used by every lib/api/ domain file.
// The AppBindings interface stays a single struct (not split by domain)
// because the Go side exposes them as one flat window.go.main.App object
// — splitting the interface would only serve appearances and would
// force every consumer to name multiple mixins.

import { t } from "../../i18n";
import type { PresetId } from "../taskState";

// ---- domain types re-exported here so AppBindings can name them ----
//
// These types are the wire contracts every domain shares. Domain modules
// re-export them with their function set for public consumption; the
// interface below is the internal binding surface.

export type TaskGroupBy = "host" | "state";

export interface Endpoint {
  url: string;
  session_token: string;
}

export interface StartupError {
  fatal: boolean;
  message: string;
  log_path: string;
}

export type LocalePreference = "system" | "en" | "zh-CN";

export interface NewSessionReq {
  command: string;
  args?: string[];
  cwd?: string;
  cols?: number;
  rows?: number;
  // Filled from the previous run's snapshot during executeRestore
  // (recoveryRestore.ts). Go-side sniff (desktop/ai_sid_sniff.go) also sets
  // this after the child prints its prompt so resume works. Empty value
  // disables sniff + resume.
  ai_kind?: "claude" | "codex" | "aider" | "";
  // Round-tripped from the previous run's snapshot during executeRestore.
  // Not used by Go to spawn the child — only the frontend injects the
  // resume command after prompt-ready.
  initial_ai_session_id?: string;
  // Original full command line the AI CLI was launched with (snapshot
  // last_command_line). Go merges claude's launch flags (e.g.
  // --permission-mode) into the injected `claude --resume <id>` so recovery
  // preserves them. Not passed as a spawn arg.
  initial_ai_command_line?: string;
  // Explicit session-profile override for this new session. Empty/absent
  // means "use the default profile if one is set, else the existing
  // default_shell + HOME behavior" — resolved entirely on the Go side
  // (desktop/relay_host.go resolveSessionProfile). The frontend only needs
  // to send a non-empty value when the user picked a specific profile.
  profile_id?: string;
}

// SessionProfile mirrors desktop/profiles.go SessionProfile — a named
// session-launch configuration (shell, cwd, startup command, env). See
// design doc §4 (docs/superpowers/specs/2026-08-17-session-profiles-design.md).
export interface SessionProfile {
  id: string;
  name: string;
  shell?: string;
  cwd?: string;
  startup_cmd?: string;
  env?: Record<string, string>;
  // sync_env defaults false/absent: Env never leaves this machine. Sealed
  // sync (SetProfiles) strips Env for every profile with sync_env !== true
  // before it ever reaches the relay (desktop/profiles.go stripUnsyncedEnv).
  sync_env?: boolean;
}

// SSHConnectReq mirrors desktop/app.go SSHConnectReq. Credentials are used for
// this connection only and are never persisted (slice 1).
export interface SSHConnectReq {
  host: string;
  port?: string;
  user: string;
  // "key" carries a pasted private_key for the ad-hoc dialog; saved hosts use
  // key_id via NewSshSessionByID instead.
  auth_kind: "password" | "key";
  password?: string;
  private_key?: string;
  passphrase?: string;
  cols?: number;
  rows?: number;
  // Set on a retry after the user confirmed an unknown host fingerprint in the
  // TOFU dialog: the Host and Fingerprint of the rejection that produced the
  // dialog, echoed back verbatim.
  //
  // They replaced an accept_host_key bool, and the difference is a security one
  // once a connection can run through a jump-host chain. The Go callback does
  // not merely let an accepted key through — it appends it to known_hosts — so
  // "accept the next unknown key" would record keys for machines the user was
  // never shown, and a substituted hop would never prompt again. Send both or
  // neither; a fingerprint without its host matches nothing.
  accepted_host_key_host?: string;
  accepted_host_key_fingerprint?: string;
}

// AcceptedHostKey mirrors desktop/ssh_host.go AcceptedHostKey — the second
// argument of NewSshSessionByID, and the saved-host equivalent of the two
// SSHConnectReq fields above. Both members are required so the pair cannot be
// split on the way back; { host: "", fingerprint: "" } accepts nothing and is
// what a first attempt sends.
export interface AcceptedHostKey {
  host: string;
  fingerprint: string;
}

// SSHHost mirrors desktop SSHHost — the non-secret part of a saved host.
// Key auth references an SSHKey by id rather than inlining a private key.
export interface SSHHost {
  id: string;
  alias?: string;
  host: string;
  port?: string;
  user: string;
  auth_kind: "password" | "key";
  key_id?: string;
  tags?: string[];
  note?: string;
  // identity_file / proxy_jump / proxy_command are populated by ssh_config
  // import (desktop/ssh_config_import.go) and are otherwise empty for
  // manually-added hosts. identity_file is a path only — atterm never reads
  // the file as part of import.
  //
  // proxy_jump is connectable as of roadmap item 27: the backend dials the
  // chain, one hop at a time, and every hop must itself be a saved host in
  // atterm (it uses that host's own credential). The frontend still shows the
  // route — which machines a connection passes through is worth seeing before
  // it happens — but no longer refuses it.
  //
  // proxy_command is still refused, and always will be: atterm never runs an
  // arbitrary command to open a connection. That marker has to stay visible
  // rather than turning into a failed dial.
  identity_file?: string;
  proxy_jump?: string;
  proxy_command?: string;
  // forwards are this host's port-forwarding rules (desktop/ssh_tunnels.go).
  // Configuration only — nothing starts on connect; StartForward is the only
  // thing that acts on them. UpdateSSHHost lets the caller's value win, so a
  // save payload that drops this field deletes every rule on every device.
  forwards?: ForwardRule[];
}

// ForwardRule mirrors desktop/ssh_tunnels.go ForwardRule — one saved
// port-forwarding rule on an SSHHost.
//
// bind_addr empty means 127.0.0.1 (defaultForwardBindAddr). Anything else is a
// deliberate, dangerous choice the UI has to warn about: the listener stops
// being loopback-only and the rule syncs to every device.
//
// target_host / target_port are the far side of a local or remote forward and
// are unused by "dynamic", where the SOCKS5 client names a destination per
// connection.
export interface ForwardRule {
  id: string;
  kind: "local" | "remote" | "dynamic";
  bind_addr?: string;
  bind_port: string;
  target_host?: string;
  target_port?: string;
  note?: string;
}

// ActiveForward mirrors desktop/ssh_tunnels.go ActiveForward — the view of one
// tunnel this app is holding.
//
// It also carries tunnels that stopped *on their own* (running false, error
// set) so a lost SSH connection stays visible. Branch on `running`: rendering
// one of those as live claims a listener that no longer exists.
export interface ActiveForward {
  host_id: string;
  rule_id: string;
  kind: string;
  // listen_addr is the listener's real resolved address, so an ephemeral or
  // defaulted bind reports what it actually became.
  listen_addr: string;
  target?: string;
  conns: number;
  started_at: number;
  running: boolean;
  error?: string;
}

// ---- the file explorer's SSH data source (desktop/sftp_source.go) ----
//
// These mirror the Go types by hand, like everything else in this file. The
// entry / meta / content shapes are deliberately identical to the local
// source's (desktop/fsaccess.go DirEntry, FileMetaInfo, FileContent) so one
// FileSystemBridge implementation can serve either.

// SFTPDirEntry mirrors desktop DirEntry.
export interface SFTPDirEntry {
  name: string;
  isDir: boolean;
  size?: number;
  modTime?: number;
}

// SFTPFileMetaInfo mirrors desktop FileMetaInfo.
export interface SFTPFileMetaInfo {
  path: string;
  size: number;
  modTime: number;
  isBinary: boolean;
}

// SFTPFileContent mirrors desktop FileContent. `data` arrives as a base64
// string over the Wails JSON boundary even though Go declares []byte — the
// preview components already accept either form.
export interface SFTPFileContent {
  path: string;
  data: string | number[];
  isBinary: boolean;
  truncatedAt?: number;
}

// SFTPListing mirrors desktop/sftp_source.go SFTPListing.
//
// It is not a bare entry array because a remote listing is capped
// (internal/sftpfs MaxEntries) and the cap has to be visible: a user shown 12
// of 3000 files with no indication is the failure the cap exists to prevent.
// `total` is the real count, so the panel can say "showing 2000 of 3000"
// rather than only that something is missing.
export interface SFTPListing {
  path: string;
  entries: SFTPDirEntry[];
  truncated: boolean;
  total: number;
}

// SSHCredential mirrors desktop sshCredential — only a password now; private
// keys live in the key vault (SSHKey), not on the host.
export interface SSHCredential {
  password?: string;
}

// SSHKey mirrors desktop SSHKey — a vault key's non-secret fields. The private
// key + passphrase live in the keyring on the Go side.
export interface SSHKey {
  id: string;
  name: string;
  key_type?: string;
}

// SSHConfigImportSkipped mirrors desktop SSHConfigImportSkipped, one entry of
// SSHConfigImportPreview.skipped — a host ~/.ssh/config named that the parser
// deliberately did not import (Match block, unreadable/cyclic Include, …),
// with a user-facing reason string (English, backend-owned; shown verbatim).
// Must stay visible in the import drawer: silently dropping these is the
// exact failure the preview step exists to prevent (design doc §5.4).
export interface SSHConfigImportSkipped {
  alias: string;
  reason: string;
}

// SSHConfigImportPreview mirrors desktop SSHConfigImportPreview, the result
// of PreviewSSHConfigImport. note is a fixed, backend-owned footnote about
// the parser's field coverage (design doc §7.3) and must be shown, not
// discarded.
export interface SSHConfigImportPreview {
  entries: SSHHost[];
  skipped: SSHConfigImportSkipped[];
  note: string;
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
  // remote=true panes skip the spawn path on restore; the original session_id
  // is re-bound to the pane so the existing remote session resumes instead of
  // being replaced by a freshly forked local shell. host_id is informational
  // (lets the recovery dialog show which host a pane came from).
  remote?: boolean;
  host_id?: string;
  session_id?: string;
  shell: string;
  shell_args?: string[];
  last_cwd?: string;
  session_type?: string;
  last_command_line?: string;
  title?: string;
  ai?: RecoveryAIInfo;
  // ssh_host_id, when non-empty, marks this pane as an SSH session connected
  // from a saved host. On restore it is reconnected via NewSshSessionByID
  // instead of being forked as a local shell. Empty for local shells and
  // ad-hoc SSH sessions.
  ssh_host_id?: string;
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
  // Loopback ws:// base the frontend attaches remote sessions through (the Go
  // remoteProxy). Read-only; empty when unavailable. Remote /client attaches
  // tunnel through Go because the WebView can't TLS-dial the relay directly on
  // networks that fingerprint-filter its handshake. Optional so Capacitor
  // fixtures may omit it.
  remote_proxy_url?: string;
  // realmId is the relay realm this session belongs to (from login finalize).
  // Written by mobile on login; consumed by subproject C for node selection.
  // Not present on desktop (Go manages realm identity there).
  realmId?: string;
  // homeInstanceURL is the user's home relay node for this realm (from login
  // finalize `home_instance_url`). Written by mobile on login; consumed by
  // subproject C for node selection. Empty/absent falls back to `url`.
  homeInstanceURL?: string;
}

export interface RelayMe {
  user_id: string;
  email: string;
  // Optional: older relays / cached objects may omit it. App.vue's isAdmin
  // computed treats a missing field as non-admin (`=== true` check).
  is_admin?: boolean;
}

export interface RelaySessionRow {
  id_hash: string;
  user_agent: string;
  ip_prefix: string;
  created_at: number;   // unix ms
  expires_at: number;   // unix ms
  is_current: boolean;
}

export interface SignOutOthersResult {
  deleted: number;
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
  wrapped: boolean;
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
  // relay_disabled: relay reachable but the admin turned Feishu off server-side.
  relay_disabled?: boolean;
  // error: the status fetch failed; the real state is unknown. When set, the UI
  // must not claim the integration is disabled.
  error?: string;
  // configured: app credentials are stored (regardless of bind state). Drives
  // the "configured" view instead of an empty form — secrets are never echoed
  // back, so without this the form looks blank on reopen.
  configured?: boolean;
  // app_id: stored (non-secret) App ID, echoed so the UI can show which app is
  // configured. Present in local mode; empty in relay mode.
  app_id?: string;
  // app_id_hash: sha256(app_id) — suffix of the event callback URL.
  app_id_hash?: string;
  // callback_url: relay event endpoint to paste into the Feishu console. Set
  // only in relay mode; empty in local mode (long-conn, no public URL).
  callback_url?: string;
}

// HookInstallState mirrors desktop/hookinstall.State (json tags). Returned by
// GetHookInstallState; rendered by SettingsFeishu's status row.
export interface HookInstallState {
  enabled: boolean;
  binary_path: string;
  binary_ok: boolean;
  binary_version: string;
  settings_path: string;
  settings_ok: boolean;
  last_error: string;
  last_check: string; // ISO timestamp (Go time.Time -> JSON string)
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
  /**
   * Minimum severity written to the file. Distinct from the viewer's level
   * filter, which only decides what is rendered from what was already written.
   */
  level: string;
}

/** One log line produced in the renderer, flushed in batches to the Go side. */
export interface FrontendLogRecord {
  timestamp_ms: number;
  level: string;
  tag: string;
  message: string;
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

// Mirrors desktop/updater.go VersionLine. One available minor-version line.
export interface VersionLine {
  minor: string;
  latest: string;
  notes: string;
  asset_url: string;
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
  lines: VersionLine[];
  // downloaded_exists is true when the most recent DownloadVersion /
  // StartDownload call short-circuited to Ready because the archive was
  // already on disk. The frontend watches false→true to prompt the user
  // whether to redownload.
  downloaded_exists: boolean;
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

export interface FeishuRemoteTerminalSettings {
  enabled: boolean;
  auto_attach: string;
}

export interface NotificationRouteData {
  session_id?: string;
  kind?: string;
}

export type MarkSessionsSeenOpts = { ids: string[] } | { all: true };

// SnippetHostState mirrors the literal strings desktop/snippet_run.go emits
// as SnippetHostResult.State. "failed" means the command ran and exited
// non-zero (ExitCode/Output are meaningful); "error" means it never ran at
// all — unreachable, auth rejected, an untrusted host key, or a timeout.
// Both are terminal, alongside "ok"; "pending"/"running" are not.
export type SnippetHostState = "pending" | "running" | "ok" | "failed" | "error";

// SnippetHostResult mirrors desktop/snippet_run.go SnippetHostResult — one
// host's outcome, as of the last state change. exit_code/output/truncated
// are only meaningful once state has left "pending"/"running".
export interface SnippetHostResult {
  host_id: string;
  host_label: string;
  state: SnippetHostState;
  exit_code: number;
  output: string;
  truncated: boolean;
  error?: string;
}

// SnippetRunProgress mirrors desktop/snippet_run.go SnippetRunProgress — the
// payload of the "snippet:run:progress" event, pushed once a host enters
// "running" and again when it reaches a terminal state. There is no separate
// "list results" call: this event stream is the only way a client learns
// anything about a run in progress. Consumers key a row on run_id +
// result.host_id.
export interface SnippetRunProgress {
  run_id: string;
  result: SnippetHostResult;
}

// SyncStatus mirrors desktop/prefs_sync_loop.go SyncStatus -- the payload of
// GetSyncStatus and of the "sync:status" event, which fires on every state
// transition. "offline" means no relay configured / paused / logged out and
// is distinct from "error" on purpose: not being configured is not a
// failure and must not render as one. last_error is only ever non-empty
// when state is "error": GetSyncStatus suppresses it whenever state is
// "offline", even if a stale error was recorded before the relay was logged
// out of or paused, so a UI that renders last_error whenever it is present
// (rather than gating on state === "error") stays correct too -- see the
// Go-side GetSyncStatus and recordSyncOutcome.
export interface SyncStatus {
  state: "idle" | "syncing" | "offline" | "error";
  last_synced_at: number; // ms, 0 = never
  pending_keys: number;
  last_error?: string;
}

// PullResult mirrors internal/prefssync.PullResult -- the payload of the
// "sync:pulled" event, fired after a pull that actually adopted or
// conflicted on at least one key (a no-op pull fires nothing). Both slices
// are pref keys, sorted.
export interface PullResult {
  Adopted: string[] | null;
  Conflict: string[] | null;
}

// AppBindings mirrors Go's exposed App.<Method> surface, one entry per
// wails.MethodDescription in wailsjs/go/main/App.d.ts. Kept as a single
// interface so the runtime lookup (bindings()) returns a typed value
// without domain-specific casts.
export interface AppBindings {
  GetClipboardPastePayload(): Promise<ClipboardPastePayload>;
  GetStartupError?(): Promise<StartupError>;
  GetEndpoint(): Promise<Endpoint>;
  GetHostInfo(): Promise<HostInfo>;
  NewSession(req: NewSessionReq): Promise<NewSessionResp>;
  NewSshSession(req: SSHConnectReq): Promise<NewSessionResp>;
  NewSshSessionByID(id: string, accepted: AcceptedHostKey): Promise<NewSessionResp>;
  ListSSHHosts(): Promise<SSHHost[]>;
  AddSSHHost(h: SSHHost, cred: SSHCredential): Promise<SSHHost>;
  UpdateSSHHost(h: SSHHost, cred: SSHCredential | null): Promise<void>;
  DeleteSSHHost(id: string): Promise<void>;
  StartForward(hostID: string, ruleID: string): Promise<void>;
  StopForward(hostID: string, ruleID: string): Promise<void>;
  ListActiveForwards(): Promise<ActiveForward[]>;
  // The file explorer's SSH data source. ListSFTPHosts is the source list:
  // the saved hosts atterm will actually dial, so a ProxyCommand host is
  // absent rather than present-and-broken.
  ListSFTPHosts(): Promise<SSHHost[]>;
  SFTPListDir(hostID: string, path: string): Promise<SFTPListing>;
  SFTPFileMeta(hostID: string, path: string): Promise<SFTPFileMetaInfo>;
  SFTPReadFile(hostID: string, path: string, maxBytes: number): Promise<SFTPFileContent>;
  SFTPWriteFile(
    hostID: string,
    path: string,
    data: number[],
    expectedModTime: number,
    createIfMissing: boolean,
  ): Promise<SFTPFileMetaInfo>;
  SFTPCreateFile(hostID: string, path: string): Promise<SFTPFileMetaInfo>;
  SFTPMkdir(hostID: string, path: string): Promise<SFTPFileMetaInfo>;
  SFTPRename(hostID: string, from: string, to: string): Promise<SFTPFileMetaInfo>;
  SFTPRemove(hostID: string, path: string, recursive: boolean): Promise<void>;
  SFTPDisconnect(hostID: string): Promise<void>;
  PreviewSSHConfigImport(): Promise<SSHConfigImportPreview>;
  ImportSSHHosts(hosts: SSHHost[]): Promise<number>;
  ListSSHKeys(): Promise<SSHKey[]>;
  AddSSHKey(name: string, privateKeyPEM: string, passphrase: string): Promise<SSHKey>;
  UpdateSSHKey(id: string, name: string, privateKeyPEM: string, passphrase: string): Promise<void>;
  DeleteSSHKey(id: string): Promise<void>;
  CloseSession(sessionID: string): Promise<void>;
  ListShells(): Promise<string[]>;
  GetRelayConfig(): Promise<RelayConfig>;
  SetRelayConfig(cfg: RelayConfig): Promise<void>;
  ClearRelayConfig(): Promise<void>;
  SetRelayDisableE2EE(disabled: boolean): Promise<void>;
  SetUplinkPaused(paused: boolean): Promise<void>;
  GetUplinkHealth(): Promise<ConnHealthSnapshot>;
  LoginRemoteRelay(relayURL: string, email: string, password: string, allowInsecure: boolean): Promise<void>;
  RegisterRemoteRelay(relayURL: string, email: string, password: string, claimToken: string, allowInsecure: boolean): Promise<void>;
  GetAccountKey(): Promise<string>;
  LoadSavedRelayPassword(): Promise<string>;
  RememberRelayPassword(password: string): Promise<void>;
  ProbeRelayVersion(arg1: string, arg2: boolean): Promise<void>;
  FetchRelayMe(): Promise<RelayMe>;
  ListRelaySessions(): Promise<RelaySessionRow[]>;
  RevokeRelaySession(idHash: string): Promise<void>;
  SignOutOtherRelaySessions(): Promise<SignOutOthersResult>;
  CreatePairingToken(): Promise<PairingToken>;
  GetLoggingConfig(): Promise<LoggingConfig>;
  SetLoggingConfig(cfg: LoggingConfig): Promise<void>;
  PickLogFilePath(): Promise<string>;
  GetLogPreview(): Promise<LogPreview>;
  AppendFrontendLogs(records: FrontendLogRecord[]): Promise<number>;
  GetTerminalTheme(): Promise<string>;
  SetTerminalTheme(themeID: string): Promise<void>;
  GetShortcutBindings(): Promise<Record<string, string>>;
  SetShortcutBindings(bindings: Record<string, string>): Promise<void>;
  GetTerminalFontHead(): Promise<string>;
  SetTerminalFontHead(head: string): Promise<void>;
  GetTerminalFontSize(): Promise<number>;
  SetTerminalFontSize(size: number): Promise<void>;
  GetTerminalLineHeight(): Promise<number>;
  SetTerminalLineHeight(lineHeight: number): Promise<void>;
  GetTerminalCursorStyle(): Promise<string>;
  SetTerminalCursorStyle(style: string): Promise<void>;
  GetTerminalCursorBlink(): Promise<boolean>;
  SetTerminalCursorBlink(blink: boolean): Promise<void>;
  GetTerminalScrollback(): Promise<number>;
  SetTerminalScrollback(lines: number): Promise<void>;
  GetLocalePreference(): Promise<LocalePreference>;
  SetLocalePreference(preference: LocalePreference): Promise<void>;
  GetDefaultShell(): Promise<string>;
  SetDefaultShell(shell: string): Promise<void>;
  GetUpdateState(): Promise<UpdateState>;
  CheckUpdate(): Promise<void>;
  StartDownload(): Promise<void>;
  DownloadVersion(tag: string): Promise<void>;
  CancelDownload(): Promise<void>;
  ForceRedownload(tag: string): Promise<void>;
  InstallUpdate(): Promise<void>;
  GetAutoCheckUpdates(): Promise<boolean>;
  SetAutoCheckUpdates(enabled: boolean): Promise<void>;
  GetUpdateGHProxyURL(): Promise<string>;
  SetUpdateGHProxyURL(proxyURL: string): Promise<void>;
  ConfirmQuit(): Promise<void>;
  GetNotificationsEnabled(): Promise<boolean>;
  SetNotificationsEnabled(enabled: boolean): Promise<void>;
  GetAINotificationsOnly(): Promise<boolean>;
  SetAINotificationsOnly(enabled: boolean): Promise<void>;
  GetFeishuModePref(): Promise<string>;
  SetFeishuModePref(pref: string): Promise<void>;
  GetFeishuEffectiveMode(): Promise<string>;
  GetFeishuRemoteTerminalSettings(): Promise<FeishuRemoteTerminalSettings>;
  SetFeishuRemoteTerminalSettings(enabled: boolean, autoAttach: string): Promise<void>;
  GetPtyInputDebugEnabled(): Promise<boolean>;
  SetPtyInputDebugEnabled(enabled: boolean): Promise<void>;
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
  SendFeishuTestCard(scenario: string): Promise<void>;
  GetHookInstallState(): Promise<HookInstallState>;
  SetHookInstallEnabled(on: boolean): Promise<void>;
  GetQuickTemplates(): Promise<import('../templates').QuickTemplate[]>;
  SetQuickTemplates(list: import('../templates').QuickTemplate[]): Promise<void>;
  // RunSnippetOnHosts starts a batch run of snippetText (labelled
  // snippetLabel for anything that needs a human-readable name) across
  // several saved hosts and returns immediately with a run id; results only
  // ever arrive through the "snippet:run:progress" event (SnippetRunProgress
  // above). CancelSnippetRun moves every host still queued or in flight to
  // "error" — hosts already at a terminal state are left alone.
  //
  // Takes the snippet's label + text directly rather than an id: templates
  // that still fall back to DEFAULT_TEMPLATES (never customized) have no
  // Go-side existence, so an id-only lookup would fail on a fresh install.
  RunSnippetOnHosts(snippetLabel: string, snippetText: string, hostIDs: string[]): Promise<string>;
  CancelSnippetRun(runID: string): Promise<void>;
  // GetSyncStatus reads the current cross-device preference sync state for
  // first paint; "sync:status" (SyncStatus) then pushes every subsequent
  // transition. SyncNow enqueues an immediate pull-then-push and only
  // rejects for "cannot start" (offline) -- the sync's own outcome still
  // arrives via "sync:status", not this call's resolution. See
  // desktop/prefs_sync_loop.go.
  GetSyncStatus(): Promise<SyncStatus>;
  SyncNow(): Promise<void>;
  GetTaskPreset(): Promise<string>;
  SetTaskPreset(preset: PresetId): Promise<void>;
  GetTaskGroupBy(): Promise<string>;
  SetTaskGroupBy(groupBy: TaskGroupBy): Promise<void>;
  GetTaskSidebarCollapsed(): Promise<boolean>;
  SetTaskSidebarCollapsed(collapsed: boolean): Promise<void>;
  GetTaskSidebarWidth(): Promise<number>;
  SetTaskSidebarWidth(px: number): Promise<void>;
  GetPinnedSessionIds(): Promise<string[]>;
  SetPinnedSessionIds(ids: string[]): Promise<void>;
  GetUserHomeDir(): Promise<string>;
  GetAppVersion(): Promise<string>;
  MarkSessionsSeen(ids: string[], all: boolean): Promise<void>;
  ListRemoteSessions(): Promise<string>;
  // Optional (like GetStartupError above): session profiles landed after
  // most of App.test.ts's __setBindingsForTest mocks were written. Marking
  // these `?` lets api.ts fall back to empty defaults when a mock doesn't
  // define them, instead of every existing test mock needing to grow these
  // two methods just to keep mounting App.
  GetProfiles?(): Promise<SessionProfile[]>;
  SetProfiles?(profiles: SessionProfile[]): Promise<void>;
  GetDefaultProfileID?(): Promise<string>;
  SetDefaultProfileID?(id: string): Promise<void>;
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

/** bindings() returns the injected Wails App surface. Throws with the
 *  i18n-keyed "wailsBindingsNotReady" message when called on web builds
 *  or before window.go is populated. */
export function bindings(): AppBindings {
  if (_bindingsOverride) return _bindingsOverride;
  const b = window.go?.main?.App;
  if (!b) throw new Error(t("app.wailsBindingsNotReady"));
  return b;
}
