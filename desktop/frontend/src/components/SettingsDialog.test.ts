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
    expect(source).toMatch(/activeTab\s*=\s*ref<["']general["']\s*\|\s*["']relay["']\s*\|\s*["']logging["']\s*\|\s*["']updates["']\s*\|\s*["']plugins["']\s*\|\s*["']shortcuts["']/);
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
}));

import { mount, flushPromises } from "@vue/test-utils";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform } from "../platform/__tests__/_fakePlatform";
import { createPinia } from "pinia";
import SettingsDialog from "./SettingsDialog.vue";

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

describe("SettingsDialog caps gating", () => {
  it("renders all 10 tabs with full desktop caps (Logging folded into Diagnostics)", () => {
    const w = mountDialog();
    expect(navLabels(w)).toEqual([
      en.settings.tabs.general,
      en.tasks.settings.section,
      en.settings.tabs.relay,
      en.settings.tabs.devices,
      en.settings.tabs.plugins,
      en.settings.tabs.shortcuts,
      en.settings.templates.tab,
      en.settings.tabs.updates,
      en.settings.diagnostics.tab,
      en.settings.feishu.title,
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

  it("with capacitor-style caps shows General + Task display + Relay + Devices + Templates + Diagnostics + Feishu", () => {
    platform.caps = { ...platform.caps, autoUpdate: false, pluginHost: false, fileDialog: false };
    __setPlatformForTests(platform);
    expect(navLabels(mountDialog())).toEqual([
      en.settings.tabs.general,
      en.tasks.settings.section,
      en.settings.tabs.relay,
      en.settings.tabs.devices,
      en.settings.templates.tab,
      en.settings.diagnostics.tab,
      en.settings.feishu.title,
    ]);
  });

  it("falls back to general when initialTab is hidden under current caps", () => {
    platform.caps = { ...platform.caps, autoUpdate: false };
    __setPlatformForTests(platform);
    const w = mountDialog({ ...baseProps, initialTab: "updates" });
    expect(w.find(".settings-nav-item.active").text()).toBe(en.settings.tabs.general);
  });
});

