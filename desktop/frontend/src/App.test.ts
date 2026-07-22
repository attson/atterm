import { describe, expect, test, it, beforeEach, afterEach, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { __setPlatformForTests } from "./platform";
import { createFakePlatform } from "./platform/__tests__/_fakePlatform";
import { __setBindingsForTest } from "./lib/api";
import { TYPE, NIL_SID, encodeFrame, encodeText } from "./lib/proto";
import type { SessionInfo } from "./lib/connection";
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

// useRecoverySnapshot subscribes via wailsjs/runtime EventsOn → window.runtime;
// jsdom has neither, so stub a no-op runtime once for the whole file.
if (typeof window !== "undefined" && !(window as any).runtime) {
  (window as any).runtime = {
    EventsOnMultiple: vi.fn(() => () => {}),
    EventsOn: vi.fn(() => () => {}),
    EventsOff: vi.fn(),
    EventsOffAll: vi.fn(),
    EventsEmit: vi.fn(),
  };
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

describe("notification click routing", () => {
  test("subscribes to notification:click and focuses the matching pane", () => {
    expect(source).toContain("platform.events.on('notification:click'");
    expect(source).toContain("function focusSessionFromNotification");
    expect(source).toContain("findPaneLocation(tabs.value, sessionId)");
    expect(source).toContain("t.activePaneIdx = loc.paneIdx");
    expect(source).toContain("gotoTab(loc.tabId)");
    expect(source).toContain("windowUnminimize?.()");
    expect(source).toContain("windowShow?.()");
  });
});

describe("remote session discovery", () => {
  test("remote list snapshots do not clear already-open panes", () => {
    const body = source.match(/function\s+applyRemoteSessions\s*\([^)]*\)\s*\{([\s\S]*?)\n\}/)?.[1] ?? "";
    expect(body).not.toContain("sweepMissingSessions");
  });

  test("TaskSidebar close closes the pane holding that session", () => {
    expect(source).toContain("function onSidebarClose");
    expect(source).toContain("findPaneLocation(tabs.value, s.session_id)");
    expect(source).toContain("closePaneAt(t, loc.paneIdx)");
    expect(source).toContain(':open-session-ids="openSessionIds"');
    expect(source).toContain('@close="onSidebarClose"');
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
    expect(source).toContain('@open-remote="openRemoteFromTitleBar"');
    expect(source).toContain('@open-settings="showSettings = true"');
  });

  test("remote titlebar action expands the task sidebar instead of opening a remote dialog", () => {
    expect(source).toContain("function openRemoteFromTitleBar");
    expect(source).toContain("setSidebarCollapsedAndPersist(false)");
    expect(source).not.toContain("showRemote");
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

describe("remote tab session retention", () => {
  class FakeWebSocket {
    static instances: FakeWebSocket[] = [];
    static CONNECTING = 0;
    static OPEN = 1;
    readyState = FakeWebSocket.CONNECTING;
    binaryType = "";
    onopen: (() => void) | null = null;
    onmessage: ((event: MessageEvent) => void) | null = null;
    onclose: (() => void) | null = null;
    onerror: (() => void) | null = null;

    constructor(public url: string) {
      FakeWebSocket.instances.push(this);
    }

    send() {}
    close() {
      this.readyState = FakeWebSocket.CONNECTING;
      this.onclose?.();
    }

    emitSessions(sessions: SessionInfo[]) {
      const bytes = encodeFrame(TYPE.LIST_RESP, NIL_SID, encodeText(JSON.stringify(sessions)));
      this.onmessage?.({ data: bytes.buffer } as MessageEvent);
    }
  }

  const remoteSession: SessionInfo = {
    id: "remote-1",
    command: "claude",
    cwd: "/tmp",
    title: "claude",
    cols: 120,
    rows: 40,
    started_at: 1,
    host_id: "host-remote",
    host: "remote-host",
    user: "attson",
  };

  let listRemoteSessionsMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    setActivePinia(createPinia());
    __setPlatformForTests(createFakePlatform());
    // Remote list now arrives through the Go backend (App.ListRemoteSessions)
    // — a JSON string of SessionInfo[] — rather than a webview WebSocket.
    listRemoteSessionsMock = vi.fn().mockResolvedValue(JSON.stringify([]));
    __setBindingsForTest({
      ListRemoteSessions: listRemoteSessionsMock,
      GetTaskSidebarCollapsed: vi.fn().mockResolvedValue(false),
      GetEndpoint: vi.fn().mockResolvedValue({ url: "ws://local", session_token: "" }),
      GetHostInfo: vi.fn().mockResolvedValue({ host_id: "local-host", host: "local", user: "attson" }),
      GetRelayConfig: vi.fn().mockResolvedValue({
        url: "ws://remote",
        token: "token",
        session_expires_at: 0,
        allow_insecure_relay: false,
        remote_permission: "full",
        connected: true,
      }),
      GetTerminalTheme: vi.fn().mockResolvedValue("classic"),
      GetCommandNotifyThresholdSeconds: vi.fn().mockResolvedValue(0),
      ListShells: vi.fn().mockResolvedValue(["/bin/zsh"]),
      NewSession: vi.fn().mockResolvedValue({ session_id: "local-1" }),
      CloseSession: vi.fn().mockResolvedValue(undefined),
      GetUpdateState: vi.fn().mockResolvedValue({ available: false, ready: false }),
      ConfirmQuit: vi.fn().mockResolvedValue(undefined),
      MarkSessionsSeen: vi.fn().mockResolvedValue(undefined),
      LoadRecoverySnapshot: vi.fn().mockResolvedValue({
        version: 1,
        host_id: "",
        clean_shutdown: true,
        saved_at_unix: 0,
        tabs: [],
      }),
      SaveRecoverySnapshot: vi.fn().mockResolvedValue(undefined),
      DiscardRecoverySnapshot: vi.fn().mockResolvedValue(undefined),
      GetRecoveryDialogEnabled: vi.fn().mockResolvedValue(true),
    } as any);
    FakeWebSocket.instances = [];
    vi.stubGlobal("WebSocket", FakeWebSocket);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    __setBindingsForTest(undefined);
    __setPlatformForTests(null);
  });

  it("keeps an opened remote pane attached when a transient list snapshot omits it", async () => {
    // The remote list arrives via the Go-backed poll (2s interval). Drive it
    // with real timers and a controllable snapshot rather than fake timers,
    // which deadlock @vue/test-utils' setTimeout-based flushPromises.
    let remoteSnapshot = JSON.stringify([remoteSession]);
    listRemoteSessionsMock.mockImplementation(() => Promise.resolve(remoteSnapshot));

    const wrapper = mount(App, {
      global: {
        stubs: {
          TitleBar: true,
          TabBar: true,
          PluginHost: true,
          TranslatePanelHost: true,
          ShortcutHints: true,
          TaskSidebar: {
            template: `<button data-testid="open-remote" @click="$emit('open', { session_id: 'remote-1' })">open</button>`,
          },
          PaneGrid: {
            props: ["tab"],
            template: `
              <div data-testid="pane-grid">
                <div
                  v-for="pane in tab.panes"
                  :key="pane.sessionId || 'empty'"
                  :data-testid="pane.sessionId ? 'pane-session' : 'pane-empty'"
                  :data-session-id="pane.sessionId || ''"
                >{{ pane.sessionId || 'empty' }}</div>
              </div>
            `,
          },
        },
      },
    });
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    await flushPromises();

    const visiblePaneSessionId = () => {
      const pane = wrapper.findAll('[data-testid="pane-session"]').find((p) => p.isVisible());
      return pane?.attributes("data-session-id");
    };

    // The remote list is delivered by polling App.ListRemoteSessions; the
    // first snapshot (from the immediate poll on connect) lists remote-1.
    expect(listRemoteSessionsMock).toHaveBeenCalled();

    await wrapper.get('[data-testid="open-remote"]').trigger("click");
    await flushPromises();
    expect(visiblePaneSessionId()).toBe("remote-1");

    // A later poll whose snapshot omits remote-1 must NOT tear down the
    // already-open pane (sweepMissingSessions skips remote panes).
    remoteSnapshot = JSON.stringify([]);
    await new Promise((resolve) => setTimeout(resolve, 2100));
    await flushPromises();

    expect(visiblePaneSessionId()).toBe("remote-1");
    expect(wrapper.find('[data-testid="pane-empty"]').exists()).toBe(false);
  }, 10000);

  test("successful remote polls prune stale single-pane remote tabs after a grace window", () => {
    expect(source).toContain("REMOTE_STALE_TAB_GRACE_MS = 60_000");
    expect(source).toContain("remoteMissingSince");
    expect(source).toContain("pruneStaleRemoteTabs({");
    expect(source).toContain("remoteSessions: remoteList.value");
    expect(source).toContain("tabs.value = pruned.tabs");
    expect(source).toContain("pruned.removedTabIds.includes(currentTabId.value)");
    expect(source).toContain('location.hash = ""');
  });
});

describe("App window title follows active AI session", () => {
  test("source declares a watcher that reads activeSession.type/title", () => {
    // Watcher must observe BOTH .type and .title so the title rewires when
    // the AI tool emits OSC 0/1/2 mid-session, not just on tab switch.
    expect(source).toContain("activeSession");
    expect(source).toMatch(/windowSetTitle\?/);
    expect(source).toMatch(/['"]AT Term['"]/);
    expect(source).toContain("'ai'");
  });

  test("source uses AT Term fallback for non-AI or empty-title cases", () => {
    // The fallback string must appear adjacent to the watcher logic so
    // shell tabs and AI-tabs-without-title both restore the default title.
    const body = source.match(/watch\([\s\S]*?windowSetTitle[\s\S]*?\)/)?.[0] ?? "";
    expect(body).toContain("'ai'");
    expect(body).toMatch(/['"]AT Term['"]/);
  });

  test("watcher initial call resolves to AT Term in the fake platform", async () => {
    setActivePinia(createPinia());
    const platform = createFakePlatform();
    const setTitle = platform.system.windowSetTitle as ReturnType<typeof vi.fn>;
    setTitle.mockClear();
    __setPlatformForTests(platform);
    try {
      mount(App, {
        global: {
          stubs: { TitleBar: true, TabBar: true, PluginHost: true, TranslatePanelHost: true, ShortcutHints: true, TaskSidebar: true, PaneGrid: true },
        },
      });
      await flushPromises();
      // No tabs yet → fallback to AT Term via immediate: true.
      expect(setTitle).toHaveBeenCalledWith("AT Term");
    } finally {
      __setPlatformForTests(null);
    }
  });
});
