import { describe, expect, test, it, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import source from "./SettingsGeneral.vue?raw";
import SettingsGeneral from "./SettingsGeneral.vue";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform } from "../platform/__tests__/_fakePlatform";

vi.mock("@shared/api/push-flow", () => ({
  enablePushFlow: vi.fn(),
  disablePushFlow: vi.fn(),
}));
vi.mock("@shared/api/push", () => ({
  testPush: vi.fn(),
}));

import { enablePushFlow, disablePushFlow } from "@shared/api/push-flow";
import { testPush } from "@shared/api/push";

const enablePushFlowMock = enablePushFlow as unknown as ReturnType<typeof vi.fn>;
const disablePushFlowMock = disablePushFlow as unknown as ReturnType<typeof vi.fn>;
const testPushMock = testPush as unknown as ReturnType<typeof vi.fn>;

function stubServiceWorker(opts: { controller?: object | null; subscription?: object | null } = {}) {
  const controller = opts.controller === undefined ? {} : opts.controller;
  const subscription = opts.subscription === undefined ? null : opts.subscription;
  const registration = {
    pushManager: {
      getSubscription: vi.fn().mockResolvedValue(subscription),
    },
  };
  Object.defineProperty(navigator, "serviceWorker", {
    configurable: true,
    value: {
      controller,
      ready: Promise.resolve(registration),
    },
  });
  // Real browsers that expose navigator.serviceWorker also expose the
  // Notification API; jsdom has neither, so stub both together.
  Object.defineProperty(globalThis, "Notification", {
    configurable: true,
    value: { permission: "granted", requestPermission: vi.fn().mockResolvedValue("granted") },
  });
  return registration;
}

function removeServiceWorker() {
  delete (navigator as { serviceWorker?: unknown }).serviceWorker;
  delete (globalThis as { Notification?: unknown }).Notification;
}

describe("SettingsGeneral", () => {
  test("declares terminalThemeId prop and terminal-theme-changed emit", () => {
    expect(source).toContain("terminalThemeId: string");
    expect(source).toContain('(e: "terminal-theme-changed", themeID: string): void');
  });

  test("uses terminal theme registry and saves changes via setTerminalThemePreference", () => {
    expect(source).toContain("TERMINAL_THEMES");
    expect(source).toContain("setTerminalThemePreference");
    expect(source).toMatch(/async\s+function\s+onChange\s*\(\s*\)/);
    expect(source).toContain('emit("terminal-theme-changed", nextTheme)');
  });

  test("renders a SelectDropdown bound to the local selected ref", () => {
    expect(source).toContain("import SelectDropdown");
    expect(source).toContain("<SelectDropdown");
    expect(source).toContain('v-model="selected"');
    expect(source).toContain('@update:modelValue="onChange"');
    expect(source).toContain("settings.general.terminalTheme");
  });

  test("reverts to previous theme on save error and surfaces it", () => {
    expect(source).toContain('selected.value = previous');
    expect(source).toContain('emit("terminal-theme-changed", previous)');
    expect(source).toMatch(/error\.value\s*=/);
  });
});

describe("SettingsGeneral notification toggle", () => {
  test("desktop-only settings are hidden behind caps.wailsBindings on web", () => {
    expect(source).toContain("if (caps.wailsBindings)");
    expect(source).toContain('v-if="caps.wailsBindings && !notificationsLoading"');
    expect(source).toContain('v-if="caps.wailsBindings && !shellIntegrationLoading"');
    expect(source).toContain('v-if="caps.wailsBindings && !recoveryEnabledLoading"');
    expect(source).toContain('v-if="caps.wailsBindings && !defaultShellLoading"');
    expect(source).toContain('v-if="caps.wailsBindings && !webglRendererLoading"');
    expect(source).toContain('v-if="caps.wailsBindings && !commandNotifyThresholdLoading"');
  });

  test("imports notification getter and setter", () => {
    expect(source).toContain("getNotificationsEnabled");
    expect(source).toContain("setNotificationsEnabled");
  });

  test("renders the checkbox with label and focus-only hint", () => {
    expect(source).toContain("settings.general.notificationsBell");
    expect(source).toContain("settings.general.notificationsHint");
  });

  test("wires checkbox change handler to setNotificationsEnabled", () => {
    expect(source).toMatch(/onNotificationsToggle/);
    expect(source).toContain('@change="onNotificationsToggle"');
  });

  test("renders shell integration toggle wired to setShellIntegrationEnabled", () => {
    expect(source).toContain("settings.general.shellIntegration");
    expect(source).toMatch(/setShellIntegrationEnabled\(/);
  });

  test("renders command-notify threshold number input wired to setCommandNotifyThresholdSeconds", () => {
    expect(source).toContain("settings.general.commandNotifyThreshold");
    expect(source).toMatch(/setCommandNotifyThresholdSeconds\(/);
    expect(source).toContain('min="1"');
    expect(source).toContain('max="600"');
  });

  test("loads shell integration and threshold on mount", () => {
    expect(source).toMatch(/getShellIntegrationEnabled\(\)/);
    expect(source).toMatch(/getCommandNotifyThresholdSeconds\(\)/);
  });

  test("emits command-notify-threshold-changed when threshold saves", () => {
    expect(source).toContain('"command-notify-threshold-changed"');
  });
});

describe("SettingsGeneral default shell preference", () => {
  test("imports default shell bindings and listShells", () => {
    expect(source).toContain("getDefaultShell");
    expect(source).toContain("setDefaultShell");
    expect(source).toContain("listShells");
  });

  test("renders automatic and custom shell controls", () => {
    expect(source).toContain("settings.general.defaultShell");
    expect(source).toContain("defaultShellOptions");
    expect(source).toContain('v-model="selectedDefaultShell"');
    expect(source).not.toContain("<select");
    expect(source).toContain("settings.general.customShellPath");
    expect(source).toMatch(/onDefaultShellChange/);
    expect(source).toMatch(/onCustomShellSave/);
  });
});

describe("SettingsGeneral WebGL renderer toggle", () => {
  test("imports WebGL getter and setter", () => {
    expect(source).toContain("getWebglRendererEnabled");
    expect(source).toContain("setWebglRendererEnabled");
  });

  test("renders the WebGL renderer toggle wired to setWebglRendererEnabled", () => {
    expect(source).toContain("settings.general.webglRenderer");
    expect(source).toMatch(/onWebglRendererToggle/);
    expect(source).toContain('@change="onWebglRendererToggle"');
  });

  test("hint mentions the typing-lag trade-off so Linux users can find it", () => {
    expect(source).toContain("settings.general.webglHint");
  });

  test("loads WebGL preference on mount", () => {
    expect(source).toMatch(/getWebglRendererEnabled\(\)/);
  });
});

describe("SettingsGeneral language preference", () => {
  test("imports i18n and locale preference bindings", () => {
    expect(source).toContain("useI18n");
    expect(source).toContain("getLocalePreference");
    expect(source).toContain("setRuntimeLocalePreference");
    expect(source).not.toContain("setLocalePreference as saveLocalePreference");
  });

  test("renders language selector with translated label and hint", () => {
    expect(source).toContain("settings.general.languageLabel");
    expect(source).toContain("settings.general.languageHint");
    expect(source).toContain("languageOptions");
  });
});

describe("SettingsGeneral push notification section", () => {
  let platform: ReturnType<typeof createFakePlatform>;

  beforeEach(() => {
    vi.clearAllMocks();
    platform = createFakePlatform();
    __setPlatformForTests(platform);
  });

  afterEach(() => {
    __setPlatformForTests(null);
    removeServiceWorker();
  });

  it("does not render when caps.notifications is false", async () => {
    platform.caps.notifications = false;
    stubServiceWorker();
    const w = mount(SettingsGeneral, { props: { terminalThemeId: "classic" } });
    await flushPromises();
    expect(w.find('[data-testid="push-section"]').exists()).toBe(false);
  });

  it("does not render when caps.notifications is true but there is no navigator.serviceWorker (e.g. Wails)", async () => {
    platform.caps.notifications = true;
    removeServiceWorker();
    const w = mount(SettingsGeneral, { props: { terminalThemeId: "classic" } });
    await flushPromises();
    expect(w.find('[data-testid="push-section"]').exists()).toBe(false);
  });

  it("renders when caps.notifications is true and navigator.serviceWorker is present", async () => {
    platform.caps.notifications = true;
    stubServiceWorker();
    const w = mount(SettingsGeneral, { props: { terminalThemeId: "classic" } });
    await flushPromises();
    expect(w.find('[data-testid="push-section"]').exists()).toBe(true);
  });

  it("checkbox starts unchecked and the test button is hidden when there is no existing subscription", async () => {
    platform.caps.notifications = true;
    stubServiceWorker({ subscription: null });
    const w = mount(SettingsGeneral, { props: { terminalThemeId: "classic" } });
    await flushPromises();
    expect(w.get<HTMLInputElement>('[data-testid="push-toggle"]').element.checked).toBe(false);
    expect(w.find('[data-testid="push-test"]').exists()).toBe(false);
  });

  it("checkbox starts checked and the test button is shown when a subscription already exists", async () => {
    platform.caps.notifications = true;
    stubServiceWorker({ subscription: { endpoint: "https://example/1" } });
    const w = mount(SettingsGeneral, { props: { terminalThemeId: "classic" } });
    await flushPromises();
    expect(w.get<HTMLInputElement>('[data-testid="push-toggle"]').element.checked).toBe(true);
    expect(w.find('[data-testid="push-test"]').exists()).toBe(true);
  });

  it("toggling the checkbox on calls enablePushFlow and reflects success", async () => {
    platform.caps.notifications = true;
    stubServiceWorker({ subscription: null });
    enablePushFlowMock.mockResolvedValue({ ok: true });
    const w = mount(SettingsGeneral, { props: { terminalThemeId: "classic" } });
    await flushPromises();

    await w.get('[data-testid="push-toggle"]').setValue(true);
    await flushPromises();

    expect(enablePushFlowMock).toHaveBeenCalledTimes(1);
    expect(w.find('[data-testid="push-test"]').exists()).toBe(true);
  });

  it("toggling the checkbox on surfaces the mapped error and reverts the checkbox when enablePushFlow fails", async () => {
    platform.caps.notifications = true;
    stubServiceWorker({ subscription: null });
    enablePushFlowMock.mockResolvedValue({ ok: false, reason: "denied" });
    const w = mount(SettingsGeneral, { props: { terminalThemeId: "classic" } });
    await flushPromises();

    await w.get('[data-testid="push-toggle"]').setValue(true);
    await flushPromises();

    expect(w.get<HTMLInputElement>('[data-testid="push-toggle"]').element.checked).toBe(false);
    expect(w.get('[data-testid="push-error"]').text()).toContain(
      "Browser denied the notification permission.",
    );
  });

  it("toggling the checkbox off calls disablePushFlow and hides the test button", async () => {
    platform.caps.notifications = true;
    stubServiceWorker({ subscription: { endpoint: "https://example/1" } });
    disablePushFlowMock.mockResolvedValue({ ok: true });
    const w = mount(SettingsGeneral, { props: { terminalThemeId: "classic" } });
    await flushPromises();

    await w.get('[data-testid="push-toggle"]').setValue(false);
    await flushPromises();

    expect(disablePushFlowMock).toHaveBeenCalledTimes(1);
    expect(w.find('[data-testid="push-test"]').exists()).toBe(false);
  });

  it("clicking the test button calls testPush", async () => {
    platform.caps.notifications = true;
    stubServiceWorker({ subscription: { endpoint: "https://example/1" } });
    testPushMock.mockResolvedValue(1);
    const w = mount(SettingsGeneral, { props: { terminalThemeId: "classic" } });
    await flushPromises();

    await w.get('[data-testid="push-test"]').trigger("click");
    await flushPromises();

    expect(testPushMock).toHaveBeenCalledTimes(1);
  });
});
