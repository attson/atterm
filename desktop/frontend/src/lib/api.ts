// Wails Go bindings shim. The Wails runtime injects window.go.main.App.<Method>
// at startup; we expose typed wrappers so the rest of the app doesn't have to
// reach into globals directly.
//
// This file used to hold every wrapper in one 1100-line module. It now
// re-exports six domain slices under lib/api/ (relay, ssh, feishu, tasks,
// updates, recovery) and keeps just the "everything else" surface —
// session lifecycle, startup, generic prefs, notifications, logging —
// alongside the shared bindings() plumbing.
//
// All existing consumers still `import { X } from "./lib/api"` unchanged;
// nothing in the tree has to know about the split.

import {
  InitializeNotifications,
  IsNotificationAvailable,
  CheckNotificationAuthorization,
  RequestNotificationAuthorization,
  SendNotification,
} from "../../wailsjs/runtime/runtime";

import { bindings } from "./api/_bindings";
import type {
  ClipboardPastePayload,
  Endpoint,
  HostInfo,
  LocalePreference,
  LoggingConfig,
  LogPreview,
  NewSessionReq,
  NewSessionResp,
  NotificationRouteData,
  StartupError,
} from "./api/_bindings";

// Public re-exports for consumers of ./lib/api.
export type {
  AppBindings,
  ClipboardPastePayload,
  ConnHealthSnapshot,
  DiagnosticsPayload,
  Endpoint,
  FeishuCredentials,
  FeishuRemoteTerminalSettings,
  FeishuStatusResp,
  HookInstallState,
  HostInfo,
  LocalePreference,
  LoggingConfig,
  LogPreview,
  MarkSessionsSeenOpts,
  NewSessionReq,
  NewSessionResp,
  NotificationRouteData,
  PairingToken,
  RecoveryAIInfo,
  RecoveryPaneSnapshot,
  RecoverySnapshot,
  RecoveryTabSnapshot,
  RelayConfig,
  RelayMe,
  RelaySessionRow,
  SSHConnectReq,
  SSHCredential,
  SSHHost,
  SSHKey,
  SignOutOthersResult,
  StartupError,
  TaskGroupBy,
  UpdateState,
  VersionLine,
} from "./api/_bindings";
import { errText, logDebug } from "./log";
export { __setBindingsForTest } from "./api/_bindings";

export * from "./api/relay";
export * from "./api/ssh";
export * from "./api/feishu";
export * from "./api/tasks";
export * from "./api/updates";
export * from "./api/recovery";

// ---- notification runtime (kept here alongside showNotification) ----

let notificationRuntimeReady: Promise<boolean> | undefined;
let notificationID = 0;

function resetNotificationRuntime(): void {
  notificationRuntimeReady = undefined;
  notificationID = 0;
}

export function __resetNotificationRuntimeForTest(): void {
  resetNotificationRuntime();
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

// ---- session lifecycle + startup ----

export function getClipboardPastePayload(): Promise<ClipboardPastePayload> {
  return bindings().GetClipboardPastePayload();
}

export function getStartupError(): Promise<StartupError> {
  const b = bindings();
  if (!b.GetStartupError) {
    return Promise.resolve({ fatal: false, message: "", log_path: "" });
  }
  return b.GetStartupError();
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

// Cached shell list. ListShells shells out to exec.LookPath on the Go side;
// the result is stable for a session and only changes when the user picks a
// different default shell, so cache it to keep new-tab spawns off the IPC +
// lookup path. Invalidated by setDefaultShell (the configured default reorders
// the output) and by __resetShellsCacheForTest.
let shellsCache: Promise<string[]> | null = null;

export function __resetShellsCacheForTest(): void {
  shellsCache = null;
}

export function listShells(): Promise<string[]> {
  if (!shellsCache) {
    shellsCache = bindings()
      .ListShells()
      .catch((e) => {
        shellsCache = null; // don't cache failures — allow retry
        throw e;
      });
  }
  return shellsCache;
}

export function getHostInfo(): Promise<HostInfo> {
  return bindings().GetHostInfo();
}

// ---- generic prefs (theme / locale / default shell / debug toggles) ----

export function getLoggingConfig(): Promise<LoggingConfig> {
  return bindings().GetLoggingConfig();
}

export function setLoggingConfig(cfg: {
  enabled: boolean;
  path?: string;
  level?: string;
}): Promise<void> {
  return bindings().SetLoggingConfig({
    enabled: cfg.enabled,
    path: cfg.path ?? "",
    effective_path: "",
    dev_dual_output: false,
    // Empty means "leave the stored level alone" on the Go side, so callers
    // that only change the path or the on/off switch don't reset it.
    level: cfg.level ?? "",
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

export async function setDefaultShell(shell: string): Promise<void> {
  await bindings().SetDefaultShell(shell);
  shellsCache = null; // the configured default reorders ListShells output
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

export function getAINotificationsOnly(): Promise<boolean> {
  return bindings().GetAINotificationsOnly();
}

export function setAINotificationsOnly(enabled: boolean): Promise<void> {
  return bindings().SetAINotificationsOnly(enabled);
}

export function getPtyInputDebugEnabled(): Promise<boolean> {
  return bindings().GetPtyInputDebugEnabled();
}

export function setPtyInputDebugEnabled(enabled: boolean): Promise<void> {
  return bindings().SetPtyInputDebugEnabled(enabled);
}

export async function showNotification(title: string, body: string, data?: NotificationRouteData): Promise<void> {
  // Honor the user's "Show system notifications" toggle BEFORE we touch any
  // notification backend. The Wails runtime's SendNotification (preferred
  // path below) talks to macOS UserNotifications directly and would
  // otherwise bypass NotificationsEnabledOrDefault — which only the Go
  // fallback checks. A binding error (e.g. boot race before window.go is
  // wired) defaults to "allow" so we don't silently drop notifications.
  try {
    if (!(await getNotificationsEnabled())) return;
  } catch (e) {
    logDebug("notify", "enabled-lookup failed; defaulting to allow", { error: errText(e) });
  }
  if (await ensureNotificationRuntimeReady()) {
    try {
      notificationID += 1;
      await SendNotification({
        id: `atterm-${Date.now()}-${notificationID}`,
        title,
        body,
        ...(data ? { data } : {}),
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

export function getUserHomeDir(): Promise<string> {
  return bindings().GetUserHomeDir();
}
