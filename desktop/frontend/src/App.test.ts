import { describe, expect, test, it, beforeEach, afterEach, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { __setPlatformForTests } from "./platform";
import { createFakePlatform } from "./platform/__tests__/_fakePlatform";
import App from "./App.vue";

// jsdom does not implement matchMedia; stub it so xterm's ScreenDprMonitor
// does not throw unhandled rejections when mounting App.
if (typeof window !== "undefined" && !window.matchMedia) {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  });
}
import source from "./App.vue?raw";
import settingsSource from "./components/SettingsDialog.vue?raw";

describe("tab activation", () => {
  test("gotoTab sets currentTabId before mutating the hash", () => {
    const body = source.match(/function\s+gotoTab\s*\(id:\s*string\)\s*\{([\s\S]*?)\n\}/)?.[1] ?? "";
    const currentIdx = body.indexOf("currentTabId.value = id");
    const hashIdx = body.indexOf("location.hash =");
    expect(currentIdx).toBeGreaterThanOrEqual(0);
    expect(hashIdx).toBeGreaterThanOrEqual(0);
    expect(currentIdx).toBeLessThan(hashIdx);
  });
});

describe("terminal toasts", () => {
  test("wires pane-grid toast events to the existing toast surface", () => {
    expect(source).toContain('@toast="showToast"');
  });
});

describe("auth-error banner", () => {
  test("subscribes to relay:auth-error event on mount", () => {
    expect(source).toContain("platform.events.on('relay:auth-error'");
  });

  test("banner section is gated on authError being non-null", () => {
    expect(source).toContain('v-if="authError"');
    expect(source).toContain("auth-error-banner");
  });

  test("banner references auth_invalid_token reason string", () => {
    expect(source).toContain("auth_invalid_token");
  });

  test("banner references auth_user_disabled reason string", () => {
    expect(source).toContain("auth_user_disabled");
  });

  test("banner has an Open settings action", () => {
    expect(source).toContain("app.openSettings");
    expect(source).toContain("openSettingsRelay");
  });

  test("SettingsDialog receives initial-tab prop for relay navigation", () => {
    expect(source).toContain(":initial-tab=");
    expect(source).toContain("settingsInitialTab");
  });

  test("SettingsDialog supports initialTab prop", () => {
    expect(settingsSource).toContain("initialTab");
  });
});

describe("quit confirmation", () => {
  test("registers the before-close listener and imports confirmQuit", () => {
    expect(source).toContain("platform.events.on('before-close'");
    expect(source).toContain("confirmQuit");
    expect(source).toContain("ConfirmQuitDialog");
  });

  test("renders ConfirmQuitDialog wired to existing session counts", () => {
    expect(source).toContain("<ConfirmQuitDialog");
    expect(source).toContain(':local-count="localSessionCount"');
    expect(source).toContain(':remote-count="remoteSessionCount"');
    expect(source).toContain('@confirm="onConfirmQuit"');
    expect(source).toContain('@cancel="onCancelQuit"');
  });

  test("gates the dialog: zero counts call confirmQuit, otherwise open dialog", () => {
    expect(source).toMatch(/localSessionCount\.value\s*===\s*0[^\n]*remoteSessionCount\.value\s*===\s*0/);
    expect(source).toContain("quitDialogOpen.value = true");
  });
});

describe("merged title bar", () => {
  test("uses TitleBar component instead of inline topbar markup", () => {
    expect(source).toContain("<TitleBar");
    expect(source).toContain('import TitleBar from "./components/TitleBar.vue"');
    expect(source).not.toContain('class="topbar"');
    expect(source).not.toContain('class="brand"');
  });

  test("passes status, errorMsg, sessionCount, remoteEndpoint, availableRemoteCount, updateBadge props", () => {
    expect(source).toContain(':status="status"');
    expect(source).toContain(':error-msg="errorMsg"');
    expect(source).toContain(':session-count="sessionCount"');
    expect(source).toContain(':remote-endpoint="remoteEndpoint"');
    expect(source).toContain(':available-remote-count="availableRemote.length"');
    expect(source).toContain(':update-badge="updateBadge"');
  });

  test("wires open-remote and open-settings events", () => {
    expect(source).toContain('@open-remote="showRemote = true"');
    expect(source).toContain('@open-settings="showSettings = true"');
  });
});

describe("TitleBar caps gate", () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    __setPlatformForTests(createFakePlatform())
  })

  afterEach(() => {
    __setPlatformForTests(null)
  })

  it('renders TitleBar by default (caps.windowControls=true)', async () => {
    const w = mount(App)
    await flushPromises()
    expect(w.find('[data-testid="titlebar-root"]').exists()).toBe(true)
  })

  it('hides TitleBar when caps.windowControls is false', async () => {
    const platform = createFakePlatform()
    platform.caps = { ...platform.caps, windowControls: false }
    __setPlatformForTests(platform)
    const w = mount(App)
    await flushPromises()
    expect(w.find('[data-testid="titlebar-root"]').exists()).toBe(false)
  })
})
