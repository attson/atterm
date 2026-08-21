import { describe, expect, test, it, vi, beforeEach, afterEach } from "vitest";
import source from "./SettingsDialog.vue?raw";
import { en } from "../i18n/messages/en";

function styleBlockFor(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = source.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`));
  return match?.[1] ?? "";
}

describe("SettingsDialog shell", () => {
  test("imports the five tab subcomponents", () => {
    expect(source).toContain('import SettingsGeneral from "./SettingsGeneral.vue"');
    expect(source).toContain('import SettingsRelay from "./SettingsRelay.vue"');
    expect(source).toContain('import SettingsLogging from "./SettingsLogging.vue"');
    expect(source).toContain('import SettingsUpdates from "./SettingsUpdates.vue"');
    expect(source).toContain('import SettingsPlugins from "./SettingsPlugins.vue"');
  });

  test("renders sidebar nav with the five category labels", () => {
    expect(source).toContain('class="settings-nav"');
    expect(source).toContain('t("settings.tabs.general")');
    expect(source).toContain('t("settings.tabs.relay")');
    expect(source).toContain('t("settings.tabs.logging")');
    expect(source).toContain('t("settings.tabs.updates")');
    expect(source).toContain('t("settings.tabs.plugins")');
  });

  test("tracks the active tab and switches via sidebar clicks", () => {
    expect(source).toMatch(/activeTab\s*=\s*ref<["']general["']\s*\|\s*["']account["']\s*\|\s*["']relay["']\s*\|\s*["']logging["']\s*\|\s*["']updates["']\s*\|\s*["']plugins["']\s*\|\s*["']shortcuts["']/);
    expect(source).toContain('@click="switchTab(\'general\')"');
    expect(source).toContain('@click="switchTab(\'relay\')"');
    expect(source).toContain('@click="switchTab(\'updates\')"');
    expect(source).toContain('@click="switchTab(\'plugins\')"');
  });

  test("renders the pinned footer only for the relay tab", () => {
    expect(source).toContain('v-if="activeTab === \'relay\'"');
    expect(source).toContain('class="settings-footer"');
    expect(source).toContain('t("common.cancel")');
    expect(source).toContain("relayRef?.canSave");
    expect(source).toContain("relayRef?.saveLabel");
  });

  test("only reads the Wails terminal theme preference when Wails bindings exist", () => {
    expect(source).toContain("if (!caps.wailsBindings) return;");
    expect(source).toMatch(/if\s*\(!caps\.wailsBindings\)\s*return;[\s\S]*?getTerminalThemePreference\(\)/);
  });

  test("hosts sub-dialogs and listens for tab events", () => {
    expect(source).toContain('@open-log-viewer="openLogViewer"');
    expect(source).toContain('@request-install="onForceInstallClick"');
    expect(source).toContain("ConfirmInstallDialog");
    expect(source).toContain("LogViewerDialog");
  });

  test("confirms before discarding unsaved relay changes on tab switch", () => {
    expect(source).toContain("relayDirty");
    expect(source).toContain("pendingTab");
    expect(source).toMatch(/showDiscardConfirm\s*=/);
  });

  test("dialog uses fixed wider size and pinned column layout", () => {
    const dialogStyle = styleBlockFor(".settings-dialog");
    expect(dialogStyle).toMatch(/width\s*:\s*720px/);
    expect(dialogStyle).toMatch(/height\s*:\s*540px/);
    const navStyle = styleBlockFor(".settings-nav");
    expect(navStyle).toMatch(/width\s*:\s*160px/);
  });

  test("imports the SettingsShortcuts subcomponent", () => {
    expect(source).toContain('import SettingsShortcuts from "./SettingsShortcuts.vue"');
  });

  test("renders the Shortcuts label in the sidebar", () => {
    expect(source).toContain('t("settings.tabs.shortcuts")');
  });

  test("activeTab union includes 'shortcuts'", () => {
    expect(source).toMatch(/activeTab[\s\S]*?["']shortcuts["']/);
  });

  test("clicking the Shortcuts nav switches to that tab", () => {
    expect(source).toContain("@click=\"switchTab('shortcuts')\"");
  });
});

// ——— mount-based caps gating tests ———

vi.mock("../lib/api", () => ({
  getTerminalThemePreference: vi.fn().mockResolvedValue("default"),
  getLogPreview: vi.fn().mockResolvedValue({ path: "", exists: false, truncated: false, content: "" }),
  getLoggingConfig: vi.fn().mockResolvedValue({ enabled: false, path: "", effective_path: "" }),
  getUpdateState: vi.fn().mockResolvedValue({
    current: "0.0.0", latest: "0.0.0", available: false, notes: "",
    checking: false, last_check_at: 0, downloading: false, download_pct: 0,
    ready: false, error: "", asset_url: "", asset_size: 0, download_dir: "", download_path: "",
  }),
  getAutoCheckUpdates: vi.fn().mockResolvedValue(true),
  getNotificationsEnabled: vi.fn().mockResolvedValue(true),
  getShellIntegrationEnabled: vi.fn().mockResolvedValue(true),
  getDefaultShell: vi.fn().mockResolvedValue("auto"),
  listShells: vi.fn().mockResolvedValue(["/bin/zsh"]),
  getWebglRendererEnabled: vi.fn().mockResolvedValue(true),
  getCommandNotifyThresholdSeconds: vi.fn().mockResolvedValue(10),
  getRelayConfig: vi.fn().mockResolvedValue({
    url: "", token: "", session_expires_at: 0, allow_insecure_relay: false, remote_permission: "full",
  }),
  fetchRelayMe: vi.fn().mockResolvedValue({ user_id: "u", email: "e" }),
  setRelayConfig: vi.fn().mockResolvedValue(undefined),
  setUplinkPaused: vi.fn().mockResolvedValue(undefined),
  installUpdate: vi.fn().mockResolvedValue(undefined),
  checkUpdate: vi.fn().mockResolvedValue(undefined),
  startDownload: vi.fn().mockResolvedValue(undefined),
  setAutoCheckUpdates: vi.fn().mockResolvedValue(undefined),
  setNotificationsEnabled: vi.fn().mockResolvedValue(undefined),
  setShellIntegrationEnabled: vi.fn().mockResolvedValue(undefined),
  setDefaultShell: vi.fn().mockResolvedValue(undefined),
  setWebglRendererEnabled: vi.fn().mockResolvedValue(undefined),
  setTerminalThemePreference: vi.fn().mockResolvedValue(undefined),
  setCommandNotifyThresholdSeconds: vi.fn().mockResolvedValue(undefined),
  pickLogFilePath: vi.fn().mockResolvedValue("/tmp/log"),
  setLoggingConfig: vi.fn().mockResolvedValue(undefined),
  getHostInfo: vi.fn().mockResolvedValue({ platform: "darwin", arch: "arm64", buildType: "production" }),
  getSyncStatus: vi.fn().mockResolvedValue({ state: "idle", last_synced_at: 0, pending_keys: 0 }),
  syncNow: vi.fn().mockResolvedValue(undefined),
  exportConfig: vi.fn().mockResolvedValue(""),
  previewConfigImport: vi.fn().mockResolvedValue({ changes: [], skipped: [] }),
  applyConfigImport: vi.fn().mockResolvedValue({ applied: [], skipped: [] }),
  getDiagnostics: vi.fn().mockResolvedValue({
    generated_at: "", app_version: "", os: "", arch: "", os_version: "",
    webview_summary: "", user_agent: "", relay_url: "", relay_status: "",
    relay_token_redacted: "", allow_insecure_relay: false, remote_permission: "full",
    uplink_paused: false, recent_relay_errors: [],
    config: {
      default_shell: "", locale: "", terminal_theme: "", notifications_enabled: false,
      shell_integration_enabled: false, webgl_renderer_enabled: false, logging_enabled: false,
      log_file_path: "", auto_check_updates: false, command_notify_threshold_seconds: 0,
    },
  }),
}));

import { mount, flushPromises } from "@vue/test-utils";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform } from "../platform/__tests__/_fakePlatform";
import { createPinia } from "pinia";
import SettingsDialog from "./SettingsDialog.vue";
import SettingsGeneral from "./SettingsGeneral.vue";

const baseProps: { localSessionCount: number; remoteSessionCount: number; terminalThemeId: string; initialTab?: "general" | "relay" | "logging" | "updates" | "shortcuts" } = { localSessionCount: 0, remoteSessionCount: 0, terminalThemeId: "default" };
let platform: ReturnType<typeof createFakePlatform>;

beforeEach(() => {
  vi.clearAllMocks();
  platform = createFakePlatform();
  __setPlatformForTests(platform);
});
afterEach(() => { __setPlatformForTests(null); });

function mountDialog(props: typeof baseProps = baseProps) {
  return mount(SettingsDialog, {
    props,
    global: { plugins: [createPinia()] },
  });
}

function navLabels(w: ReturnType<typeof mount>) {
  return w.findAll(".settings-nav-item").map((b) => b.text());
}

async function switchToTab(w: ReturnType<typeof mount>, label: string) {
  const btn = w.findAll(".settings-nav-item").find((b) => b.text() === label);
  if (!btn) throw new Error(`no nav item labeled ${label}`);
  await btn.trigger("click");
  await flushPromises();
}

describe("SettingsDialog caps gating", () => {
  it("renders all 12 tabs with full desktop caps (Logging folded into Diagnostics)", () => {
    const w = mountDialog();
    expect(navLabels(w)).toEqual([
      en.settings.tabs.general,
      en.tasks.settings.section,
      en.settings.tabs.relay,
      en.settings.tabs.devices,
      en.settings.tabs.plugins,
      en.settings.tabs.shortcuts,
      en.settings.templates.tab,
      en.settings.profiles.tab,
      en.settings.tabs.updates,
      en.settings.diagnostics.tab,
      en.settings.feishu.title,
      en.settings.tabs.receivedFiles,
    ]);
    // Logging is no longer a standalone nav item.
    expect(navLabels(w)).not.toContain(en.settings.tabs.logging);
  });

  it("hides Updates when autoUpdate=false", () => {
    platform.caps = { ...platform.caps, autoUpdate: false };
    __setPlatformForTests(platform);
    expect(navLabels(mountDialog())).not.toContain(en.settings.tabs.updates);
  });

  it("hides Plugins + Shortcuts when pluginHost=false", () => {
    platform.caps = { ...platform.caps, pluginHost: false };
    __setPlatformForTests(platform);
    const labels = navLabels(mountDialog());
    expect(labels).not.toContain(en.settings.tabs.plugins);
    expect(labels).not.toContain(en.settings.tabs.shortcuts);
  });

  it("folds Logging into the Diagnostics pane instead of a standalone tab", () => {
    // The merged pane renders SettingsLogging above SettingsDiagnostics.
    expect(source).toContain('class="diag-merged"');
    expect(source).toContain('@open-log-viewer="openLogViewer"');
    expect(source).toContain("<SettingsDiagnostics />");
    // Diagnostics stays a top-level tab; Logging never is.
    const labels = navLabels(mountDialog());
    expect(labels).toContain(en.settings.diagnostics.tab);
    expect(labels).not.toContain(en.settings.tabs.logging);
  });

  it("keeps the logging section out of the Diagnostics pane when fileDialog=false", () => {
    // Section is gated on caps.fileDialog inside the merged pane.
    expect(source).toContain('<section v-if="caps.fileDialog" class="merged-section">');
  });

  // The config export/import panel (SettingsConfigIO.vue) has a strong test
  // suite of its own, but nothing before this pinned that it's actually
  // *mounted* anywhere -- deleting its line from the diag-merged block would
  // leave every one of its own tests green (they mount the component
  // directly) while the feature became unreachable from the app. This
  // mounts the real SettingsDialog, switches to the Diagnostics tab exactly
  // as a user would, and looks for the panel by its own data-testid.
  it("mounts the config export/import panel in the Diagnostics pane on Wails", async () => {
    const w = mountDialog();
    await switchToTab(w, en.settings.diagnostics.tab);
    expect(w.find('[data-testid="configio-panel"]').exists()).toBe(true);
  });

  it("never reaches the config export/import panel on non-wails platforms (Diagnostics tab itself is hidden)", () => {
    platform.caps = { ...platform.caps, wailsBindings: false };
    __setPlatformForTests(platform);
    const w = mountDialog();
    expect(w.find('[data-testid="configio-panel"]').exists()).toBe(false);
  });

  it("with capacitor-style caps shows General + Task display + Relay + Devices + Templates + Profiles + Diagnostics + Feishu + Received files", () => {
    platform.caps = { ...platform.caps, autoUpdate: false, pluginHost: false, fileDialog: false };
    __setPlatformForTests(platform);
    expect(navLabels(mountDialog())).toEqual([
      en.settings.tabs.general,
      en.tasks.settings.section,
      en.settings.tabs.relay,
      en.settings.tabs.devices,
      en.settings.templates.tab,
      en.settings.profiles.tab,
      en.settings.diagnostics.tab,
      en.settings.feishu.title,
      en.settings.tabs.receivedFiles,
    ]);
  });

  it("falls back to general when initialTab is hidden under current caps", () => {
    platform.caps = { ...platform.caps, autoUpdate: false };
    __setPlatformForTests(platform);
    const w = mountDialog({ ...baseProps, initialTab: "updates" });
    expect(w.find(".settings-nav-item.active").text()).toBe(en.settings.tabs.general);
  });

  it("shows the Account tab on non-wails platforms (web/capacitor)", () => {
    platform.caps = { ...platform.caps, wailsBindings: false };
    __setPlatformForTests(platform);
    expect(navLabels(mountDialog())).toContain(en.settings.account.title);
  });

  it("hides the Account tab on Wails (apiFetch would 401 against the desktop session)", () => {
    platform.caps = { ...platform.caps, wailsBindings: true };
    __setPlatformForTests(platform);
    expect(navLabels(mountDialog())).not.toContain(en.settings.account.title);
  });

  it("hides Relay / Diagnostics / Received files / Feishu / Profiles on non-wails platforms", () => {
    platform.caps = { ...platform.caps, wailsBindings: false };
    __setPlatformForTests(platform);
    const labels = navLabels(mountDialog());
    expect(labels).not.toContain(en.settings.tabs.relay);
    expect(labels).not.toContain(en.settings.diagnostics.tab);
    expect(labels).not.toContain(en.settings.tabs.receivedFiles);
    expect(labels).not.toContain(en.settings.feishu.title);
    expect(labels).not.toContain(en.settings.profiles.tab);
  });

  it("falls back to general when initialTab is 'relay' but wailsBindings=false", () => {
    platform.caps = { ...platform.caps, wailsBindings: false };
    __setPlatformForTests(platform);
    const w = mountDialog({ ...baseProps, initialTab: "relay" });
    expect(w.find(".settings-nav-item.active").text()).toBe(en.settings.tabs.general);
  });

  // The sync status indicator is desktop-only (design doc §6 "No mobile
  // indicator"): GetSyncStatus/SyncNow are Wails-only, and mobile syncs
  // prefs over HTTP via a wholly separate path (prefsSync.capacitor.ts).
  // The two mobile read-only views (SettingsProfilesMobile.vue,
  // SettingsSSHHostsMobile.vue) each have their own strong test suites, but
  // nothing before this pinned that they're actually *mounted and reachable*
  // from SettingsDialog.vue -- deleting their nav buttons and/or their pane
  // mounts would leave every one of their own tests green (they mount the
  // component directly) while the feature became unreachable from the phone.
  // Same shape as the config-export/import pin above. Mounts the real
  // dialog under capacitor caps, switches to each new tab exactly as a user
  // would, and looks for each panel by its own data-testid.
  it("mounts the mobile profiles and mobile SSH hosts panels under capacitor caps", async () => {
    platform.caps = { ...platform.caps, capacitor: true };
    __setPlatformForTests(platform);
    const w = mountDialog();
    await switchToTab(w, en.settings.tabs.mobileProfiles);
    expect(w.find('[data-testid="mobile-profiles-panel"]').exists()).toBe(true);
    await switchToTab(w, en.settings.tabs.mobileHosts);
    expect(w.find('[data-testid="mobile-hosts-panel"]').exists()).toBe(true);
    w.unmount();
  });

  it("never reaches the mobile profiles/SSH hosts panels when capacitor=false (nav items themselves are hidden)", () => {
    platform.caps = { ...platform.caps, capacitor: false };
    __setPlatformForTests(platform);
    const labels = navLabels(mountDialog());
    expect(labels).not.toContain(en.settings.tabs.mobileProfiles);
    expect(labels).not.toContain(en.settings.tabs.mobileHosts);
  });

  it("shows the sync status indicator in the header on Wails (wailsBindings=true)", async () => {
    platform.caps = { ...platform.caps, wailsBindings: true };
    __setPlatformForTests(platform);
    const w = mountDialog();
    await flushPromises();
    expect(w.find('[data-testid="sync-indicator"]').exists()).toBe(true);
  });

  it("hides the sync status indicator on non-wails platforms (web/capacitor)", async () => {
    platform.caps = { ...platform.caps, wailsBindings: false };
    __setPlatformForTests(platform);
    const w = mountDialog();
    await flushPromises();
    expect(w.find('[data-testid="sync-indicator"]').exists()).toBe(false);
  });
});

describe("SettingsDialog version footer", () => {
  it("renders 'AT Term v<version>' once getAppVersion resolves", async () => {
    platform.system.getAppVersion = vi.fn().mockResolvedValue("0.3.20");
    __setPlatformForTests(platform);
    const w = mountDialog();
    await flushPromises();
    expect(w.find('[data-testid="settings-version-footer"]').text()).toBe("AT Term v0.3.20");
  });

  it("renders 'AT Term (dev)' when getAppVersion resolves empty/dev", async () => {
    platform.system.getAppVersion = vi.fn().mockResolvedValue("dev");
    __setPlatformForTests(platform);
    const w = mountDialog();
    await flushPromises();
    expect(w.find('[data-testid="settings-version-footer"]').text()).toBe("AT Term (dev)");
  });
});

// SettingsDialog.vue's forwarding of SettingsGeneral's appearance-changed
// event once shipped dead (never wired at all) with the full unit suite
// green, because every other hop in the Appearance -> General -> parent ->
// App -> PaneGrid -> TerminalView chain had its own test but nothing pinned
// this one. Guards both ends: the dialog is listening on the child, and it
// re-emits exactly what it received.
describe("SettingsDialog appearance forwarding", () => {
  it("forwards appearance-changed from SettingsGeneral", () => {
    const w = mountDialog();
    const payload = {
      fontHead: "",
      fontSize: 16,
      lineHeight: 1.2,
      cursorStyle: "bar",
      cursorBlink: false,
      scrollback: 8000,
    };
    w.findComponent(SettingsGeneral).vm.$emit("appearance-changed", payload);
    expect(w.emitted("appearance-changed")).toBeTruthy();
    expect(w.emitted("appearance-changed")!.at(-1)![0]).toEqual(payload);
  });
});
