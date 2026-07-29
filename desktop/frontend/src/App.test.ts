import { describe, expect, test, it, beforeEach, afterEach, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { __setPlatformForTests } from "./platform";
import { createFakePlatform } from "./platform/__tests__/_fakePlatform";
import { __setBindingsForTest } from "./lib/api";
import { TYPE, NIL_SID, encodeFrame, encodeText } from "./lib/proto";
import type { SessionInfo } from "./lib/connection";
import { __resetForTests as __resetPinsForTests } from "./composables/useSessionPins";
import * as api from "./lib/api";
import { saveSnapshot } from "./lib/webTabsSnapshot";
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
import capacitorMainSource from "./main.capacitor.ts?raw";
import zhCNSource from "./i18n/messages/zh-CN.ts?raw";

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

describe("web capability gates", () => {
  test("does not mount plugin hosts when caps.pluginHost=false", () => {
    expect(source).toContain('<template v-if="caps.pluginHost && rightPanelHasPlugin">');
    expect(source).toContain('<PluginHost\n      v-if="caps.pluginHost"');
    expect(source).toContain('<TranslatePanelHost v-if="caps.pluginHost"');
  });

  test("settings close only refreshes desktop relay config when Wails bindings exist", () => {
    expect(source).toContain("function refreshDesktopRelayConfig()");
    expect(source).toContain("if (caps.wailsBindings)");
    expect(source).toContain('@relay-config-changed="refreshDesktopRelayConfig"');
    expect(source).toContain('@close="onSettingsClose"');
    expect(source).not.toContain('@close="showSettings = false; settingsInitialTab = undefined; refreshRelayConfig()"');
  });
});

describe("capacitor uses the shared web shell", () => {
  test("Capacitor boots the shared App.vue entry instead of the legacy mobile shell", () => {
    expect(capacitorMainSource).toContain("import App from './App.vue'");
    expect(capacitorMainSource).toContain("const app = createApp(App)");
    expect(capacitorMainSource).not.toContain("import MobileApp");
    expect(capacitorMainSource).not.toContain("createApp(MobileApp)");
  });

  test("non-Wails app can open Settings directly to the Account tab when no attach endpoint exists", () => {
    expect(source).toContain("function openSettingsAccount()");
    expect(source).toContain('settingsInitialTab.value = "account"');
    expect(source).toContain("if (!hasRelayConfig && !caps.localPty)");
    expect(source).toContain("openSettingsAccount()");
    expect(source).toContain("const endpoint = relayCfg ? buildWebRemoteEndpoint(relayCfg) : null");
    expect(source).toContain("if (!endpoint)");
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

describe("startup fatal error", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    __setPlatformForTests(createFakePlatform());
  });

  afterEach(() => {
    __setBindingsForTest(undefined);
    __setPlatformForTests(null);
  });

  it("renders a startup failure panel and stops before opening the local endpoint", async () => {
    const getEndpoint = vi.fn().mockResolvedValue({ url: "ws://local", session_token: "" });
    __setBindingsForTest({
      GetStartupError: vi.fn().mockResolvedValue({
        fatal: true,
        message: "start relay host: open userstore",
        log_path: "/tmp/atterm/desktop.log",
      }),
      GetEndpoint: getEndpoint,
      GetCommandNotifyThresholdSeconds: vi.fn().mockResolvedValue(10),
      ConfirmQuit: vi.fn().mockResolvedValue(undefined),
    } as any);

    const wrapper = mount(App, {
      global: {
        stubs: {
          TitleBar: true,
          TabBar: true,
          TaskSidebar: true,
          PluginHost: true,
          TranslatePanelHost: true,
          ShortcutHints: true,
        },
      },
    });
    await flushPromises();

    expect(wrapper.find('[data-testid="startup-fatal"]').exists()).toBe(true);
    expect(wrapper.text()).toContain("start relay host: open userstore");
    expect(wrapper.text()).toContain("/tmp/atterm/desktop.log");
    expect(getEndpoint).not.toHaveBeenCalled();
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

describe("settings button stays reachable when caps.windowControls is false (web/mobile)", () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  afterEach(() => {
    __setPlatformForTests(null)
  })

  it('opens SettingsDialog via the TabBar settings button even with no TitleBar', async () => {
    const platform = createFakePlatform()
    platform.caps = { ...platform.caps, windowControls: false }
    __setPlatformForTests(platform)
    const w = mount(App)
    await flushPromises()

    // TitleBar (and its old settings button) is gone under this cap set.
    expect(w.find('[data-testid="titlebar-root"]').exists()).toBe(false)
    expect(w.find('.settings-dialog').exists()).toBe(false)

    const settingsBtn = w.get('[data-test="tabbar-settings"]')
    await settingsBtn.trigger("click")
    await flushPromises()

    expect(w.find('.settings-dialog').exists()).toBe(true)
  })
})

describe("local shell entrypoint caps gate", () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    __setPlatformForTests(createFakePlatform())
  })

  afterEach(() => {
    __setPlatformForTests(null)
  })

  it('shows the new-local-shell button by default (caps.localPty=true)', async () => {
    const w = mount(App)
    await flushPromises()
    expect(w.find('[data-test="new-local-shell"]').exists()).toBe(true)
  })

  it('hides the new-local-shell button when caps.localPty is false', async () => {
    const platform = createFakePlatform()
    platform.caps = { ...platform.caps, localPty: false }
    __setPlatformForTests(platform)
    const w = mount(App)
    await flushPromises()
    expect(w.find('[data-test="new-local-shell"]').exists()).toBe(false)
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

describe("recovery pin migration integration", () => {
  // Integration coverage for executeRestore's pin-migration slice: drives the
  // real useSessionPins module (not mocked) through a real recovery restore,
  // unlike useSessionPins.test.ts (which exercises rename/flushNow directly
  // with synthetic ids) or recoveryRestore.test.ts (which never touches
  // pins). This is the one path that proves the two wire together correctly.
  class NoopWebSocket {
    static CONNECTING = 0;
    static OPEN = 1;
    readyState = NoopWebSocket.CONNECTING;
    binaryType = "";
    onopen: (() => void) | null = null;
    onmessage: ((event: MessageEvent) => void) | null = null;
    onclose: (() => void) | null = null;
    onerror: (() => void) | null = null;
    constructor(_url: string) {}
    send() {}
    close() {}
  }

  let platform: ReturnType<typeof createFakePlatform>;
  beforeEach(() => {
    setActivePinia(createPinia());
    platform = createFakePlatform();
    __setPlatformForTests(platform);
    __resetPinsForTests();
    vi.stubGlobal("WebSocket", NoopWebSocket);
  });

  afterEach(() => {
    __setBindingsForTest(undefined);
    __setPlatformForTests(null);
    __resetPinsForTests();
    vi.unstubAllGlobals();
  });

  it("renames a pinned local session's id across executeRestore's respawn", async () => {
    const setPinned = vi.fn().mockResolvedValue(undefined);
    platform.sessions.getPins = vi.fn().mockResolvedValue(["old-sid"]);
    platform.sessions.setPins = setPinned;
    __setBindingsForTest({
      GetTaskSidebarCollapsed: vi.fn().mockResolvedValue(false),
      GetEndpoint: vi.fn().mockResolvedValue({ url: "ws://local", session_token: "" }),
      GetHostInfo: vi.fn().mockResolvedValue({ host_id: "local-host", host: "local", user: "attson" }),
      GetRelayConfig: vi.fn().mockResolvedValue({
        url: "ws://remote", token: "", session_expires_at: 0,
        allow_insecure_relay: false, remote_permission: "full", connected: false,
      }),
      ListRemoteSessions: vi.fn().mockResolvedValue(JSON.stringify([])),
      GetTerminalTheme: vi.fn().mockResolvedValue("classic"),
      GetCommandNotifyThresholdSeconds: vi.fn().mockResolvedValue(0),
      ListShells: vi.fn().mockResolvedValue(["/bin/zsh"]),
      NewSession: vi.fn().mockResolvedValue({ session_id: "new-sid" }),
      CloseSession: vi.fn().mockResolvedValue(undefined),
      GetUpdateState: vi.fn().mockResolvedValue({ available: false, ready: false }),
      ConfirmQuit: vi.fn().mockResolvedValue(undefined),
      MarkSessionsSeen: vi.fn().mockResolvedValue(undefined),
      LoadRecoverySnapshot: vi.fn().mockResolvedValue({
        version: 1,
        host_id: "",
        clean_shutdown: false,
        saved_at_unix: 0,
        active_tab_id: "t1",
        tabs: [
          {
            id: "t1",
            layout: "single",
            active_pane_idx: 0,
            col_ratio: 0.5,
            row_ratio: 0.5,
            panes: [{ slot: 0, remote: false, session_id: "old-sid", shell: "/bin/zsh", last_cwd: "/tmp" }],
          },
        ],
      }),
      SaveRecoverySnapshot: vi.fn().mockResolvedValue(undefined),
      DiscardRecoverySnapshot: vi.fn().mockResolvedValue(undefined),
      GetRecoveryDialogEnabled: vi.fn().mockResolvedValue(true),
    } as any);

    const wrapper = mount(App, {
      global: {
        stubs: {
          TitleBar: true,
          TabBar: true,
          PluginHost: true,
          TranslatePanelHost: true,
          ShortcutHints: true,
          TaskSidebar: true,
          PaneGrid: true,
          RecoveryDialog: {
            props: ["snapshot"],
            template: `<button data-testid="btn-restore-all" @click="$emit('restore', snapshot.tabs)">restore</button>`,
          },
        },
      },
    });
    // setupMeasureProbe() (called from boot()) resolves off a real
    // requestAnimationFrame — a bare flushPromises() only drains microtasks,
    // so without this the RecoveryDialog stub never appears in time. Matches
    // the "remote tab session retention" describe block above.
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    await flushPromises();

    await wrapper.get('[data-testid="btn-restore-all"]').trigger("click");
    await flushPromises();

    expect(setPinned).toHaveBeenCalled();
    const persisted = setPinned.mock.calls[setPinned.mock.calls.length - 1][0];
    expect(persisted).toContain("new-sid");
    expect(persisted).not.toContain("old-sid");
  });
});

describe("local-shell paths gated on caps.localPty (recovery + boot auto-start)", () => {
  // Covers the two non-UI callers of newSession that Task 3.1's TabBar "+"
  // gate doesn't reach: executeRestore's local-fork branch, and the
  // boot-time auto-start fallback (App.vue's onMounted, no-recovery path).
  // Both must no-op instead of forking a local shell when the platform has
  // no local PTY (the future web build — Phase 4).
  class NoopWebSocket {
    static CONNECTING = 0;
    static OPEN = 1;
    readyState = NoopWebSocket.CONNECTING;
    binaryType = "";
    onopen: (() => void) | null = null;
    onmessage: ((event: MessageEvent) => void) | null = null;
    onclose: (() => void) | null = null;
    onerror: (() => void) | null = null;
    constructor(_url: string) {}
    send() {}
    close() {}
  }

  let platform: ReturnType<typeof createFakePlatform>;
  beforeEach(() => {
    setActivePinia(createPinia());
    platform = createFakePlatform();
    __setPlatformForTests(platform);
    vi.stubGlobal("WebSocket", NoopWebSocket);
  });

  afterEach(() => {
    __setBindingsForTest(undefined);
    __setPlatformForTests(null);
    vi.unstubAllGlobals();
  });

  it("recovery skips local-fork panes when caps.localPty=false", async () => {
    platform.caps = { ...platform.caps, localPty: false };
    const newSessionMock = vi.fn().mockResolvedValue({ session_id: "new-sid" });
    __setBindingsForTest({
      GetTaskSidebarCollapsed: vi.fn().mockResolvedValue(false),
      GetEndpoint: vi.fn().mockResolvedValue({ url: "ws://local", session_token: "" }),
      GetHostInfo: vi.fn().mockResolvedValue({ host_id: "local-host", host: "local", user: "attson" }),
      GetRelayConfig: vi.fn().mockResolvedValue({
        url: "ws://remote", token: "", session_expires_at: 0,
        allow_insecure_relay: false, remote_permission: "full", connected: false,
      }),
      ListRemoteSessions: vi.fn().mockResolvedValue(JSON.stringify([])),
      GetTerminalTheme: vi.fn().mockResolvedValue("classic"),
      GetCommandNotifyThresholdSeconds: vi.fn().mockResolvedValue(0),
      ListShells: vi.fn().mockResolvedValue(["/bin/zsh"]),
      NewSession: newSessionMock,
      CloseSession: vi.fn().mockResolvedValue(undefined),
      GetUpdateState: vi.fn().mockResolvedValue({ available: false, ready: false }),
      ConfirmQuit: vi.fn().mockResolvedValue(undefined),
      MarkSessionsSeen: vi.fn().mockResolvedValue(undefined),
      LoadRecoverySnapshot: vi.fn().mockResolvedValue({
        version: 1,
        host_id: "",
        clean_shutdown: false,
        saved_at_unix: 0,
        active_tab_id: "t1",
        tabs: [
          {
            id: "t1",
            layout: "single",
            active_pane_idx: 0,
            col_ratio: 0.5,
            row_ratio: 0.5,
            panes: [{ slot: 0, remote: false, session_id: "old-sid", shell: "/bin/zsh", last_cwd: "/tmp" }],
          },
        ],
      }),
      SaveRecoverySnapshot: vi.fn().mockResolvedValue(undefined),
      DiscardRecoverySnapshot: vi.fn().mockResolvedValue(undefined),
      GetRecoveryDialogEnabled: vi.fn().mockResolvedValue(true),
    } as any);

    const wrapper = mount(App, {
      global: {
        stubs: {
          TitleBar: true,
          TabBar: true,
          PluginHost: true,
          TranslatePanelHost: true,
          ShortcutHints: true,
          TaskSidebar: true,
          RecoveryDialog: {
            props: ["snapshot"],
            template: `<button data-testid="btn-restore-all" @click="$emit('restore', snapshot.tabs)">restore</button>`,
          },
          PaneGrid: {
            props: ["tab"],
            template: `
              <div data-testid="pane-grid">
                <div
                  v-for="pane in tab.panes"
                  :key="pane.sessionId || 'empty'"
                  :data-testid="pane.sessionId ? 'pane-session' : 'pane-empty'"
                >{{ pane.sessionId || 'empty' }}</div>
              </div>
            `,
          },
        },
      },
    });
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    await flushPromises();

    await wrapper.get('[data-testid="btn-restore-all"]').trigger("click");
    await flushPromises();

    expect(newSessionMock).not.toHaveBeenCalled();
    expect(wrapper.find('[data-testid="pane-empty"]').exists()).toBe(true);
  });

  it("boot auto-start is skipped when caps.localPty=false (no recovery snapshot)", async () => {
    platform.caps = { ...platform.caps, localPty: false };
    const newSessionMock = vi.fn().mockResolvedValue({ session_id: "new-sid" });
    __setBindingsForTest({
      GetTaskSidebarCollapsed: vi.fn().mockResolvedValue(false),
      GetEndpoint: vi.fn().mockResolvedValue({ url: "ws://local", session_token: "" }),
      GetHostInfo: vi.fn().mockResolvedValue({ host_id: "local-host", host: "local", user: "attson" }),
      GetRelayConfig: vi.fn().mockResolvedValue({
        url: "ws://remote", token: "", session_expires_at: 0,
        allow_insecure_relay: false, remote_permission: "full", connected: false,
      }),
      ListRemoteSessions: vi.fn().mockResolvedValue(JSON.stringify([])),
      GetTerminalTheme: vi.fn().mockResolvedValue("classic"),
      GetCommandNotifyThresholdSeconds: vi.fn().mockResolvedValue(0),
      ListShells: vi.fn().mockResolvedValue(["/bin/zsh"]),
      NewSession: newSessionMock,
      CloseSession: vi.fn().mockResolvedValue(undefined),
      GetUpdateState: vi.fn().mockResolvedValue({ available: false, ready: false }),
      ConfirmQuit: vi.fn().mockResolvedValue(undefined),
      MarkSessionsSeen: vi.fn().mockResolvedValue(undefined),
      // No recovery snapshot — this is the boot path that used to fall
      // straight through to startNewTab().
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

    const wrapper = mount(App, {
      global: {
        stubs: {
          TitleBar: true,
          TabBar: {
            props: ["tabs"],
            template: `<div data-testid="tabbar-stub" :data-tab-count="tabs.length"></div>`,
          },
          PluginHost: true,
          TranslatePanelHost: true,
          ShortcutHints: true,
          TaskSidebar: true,
          PaneGrid: true,
        },
      },
    });
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    await flushPromises();

    expect(newSessionMock).not.toHaveBeenCalled();
    expect(wrapper.find('[data-testid="tabbar-stub"]').attributes("data-tab-count")).toBe("0");
    expect(wrapper.text()).toContain("waiting for session selection...");
    expect(wrapper.text()).not.toContain("starting first session...");
    expect(zhCNSource).toContain('waitingForSessionSelection: "等待会话选择..."');
  });

  it("recovery discard does not spawn a local shell when caps.localPty=false", async () => {
    platform.caps = { ...platform.caps, localPty: false };
    const newSessionMock = vi.fn().mockResolvedValue({ session_id: "new-sid" });
    const discardMock = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest({
      GetTaskSidebarCollapsed: vi.fn().mockResolvedValue(false),
      GetEndpoint: vi.fn().mockResolvedValue({ url: "ws://local", session_token: "" }),
      GetHostInfo: vi.fn().mockResolvedValue({ host_id: "local-host", host: "local", user: "attson" }),
      GetRelayConfig: vi.fn().mockResolvedValue({
        url: "ws://remote", token: "", session_expires_at: 0,
        allow_insecure_relay: false, remote_permission: "full", connected: false,
      }),
      ListRemoteSessions: vi.fn().mockResolvedValue(JSON.stringify([])),
      GetTerminalTheme: vi.fn().mockResolvedValue("classic"),
      GetCommandNotifyThresholdSeconds: vi.fn().mockResolvedValue(0),
      ListShells: vi.fn().mockResolvedValue(["/bin/zsh"]),
      NewSession: newSessionMock,
      CloseSession: vi.fn().mockResolvedValue(undefined),
      GetUpdateState: vi.fn().mockResolvedValue({ available: false, ready: false }),
      ConfirmQuit: vi.fn().mockResolvedValue(undefined),
      MarkSessionsSeen: vi.fn().mockResolvedValue(undefined),
      LoadRecoverySnapshot: vi.fn().mockResolvedValue({
        version: 1,
        host_id: "",
        clean_shutdown: false,
        saved_at_unix: 0,
        active_tab_id: "t1",
        tabs: [
          {
            id: "t1",
            layout: "single",
            active_pane_idx: 0,
            col_ratio: 0.5,
            row_ratio: 0.5,
            panes: [{ slot: 0, remote: false, session_id: "old-sid", shell: "/bin/zsh", last_cwd: "/tmp" }],
          },
        ],
      }),
      SaveRecoverySnapshot: vi.fn().mockResolvedValue(undefined),
      DiscardRecoverySnapshot: discardMock,
      GetRecoveryDialogEnabled: vi.fn().mockResolvedValue(true),
    } as any);

    const wrapper = mount(App, {
      global: {
        stubs: {
          TitleBar: true,
          TabBar: {
            props: ["tabs"],
            template: `<div data-testid="tabbar-stub" :data-tab-count="tabs.length"></div>`,
          },
          PluginHost: true,
          TranslatePanelHost: true,
          ShortcutHints: true,
          TaskSidebar: true,
          RecoveryDialog: {
            props: ["snapshot"],
            template: `<button data-testid="btn-discard" @click="$emit('discard')">discard</button>`,
          },
          PaneGrid: true,
        },
      },
    });
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    await flushPromises();

    await wrapper.get('[data-testid="btn-discard"]').trigger("click");
    await flushPromises();

    expect(discardMock).toHaveBeenCalled();
    expect(newSessionMock).not.toHaveBeenCalled();
    expect(wrapper.find('[data-testid="tabbar-stub"]').attributes("data-tab-count")).toBe("0");
  });

  it("keyboard onNewTab shortcut is a no-op when caps.localPty=false", async () => {
    platform.caps = { ...platform.caps, localPty: false };
    const newSessionMock = vi.fn().mockResolvedValue({ session_id: "new-sid" });
    __setBindingsForTest({
      GetTaskSidebarCollapsed: vi.fn().mockResolvedValue(false),
      GetEndpoint: vi.fn().mockResolvedValue({ url: "ws://local", session_token: "" }),
      GetHostInfo: vi.fn().mockResolvedValue({ host_id: "local-host", host: "local", user: "attson" }),
      GetRelayConfig: vi.fn().mockResolvedValue({
        url: "ws://remote", token: "", session_expires_at: 0,
        allow_insecure_relay: false, remote_permission: "full", connected: false,
      }),
      ListRemoteSessions: vi.fn().mockResolvedValue(JSON.stringify([])),
      GetTerminalTheme: vi.fn().mockResolvedValue("classic"),
      GetCommandNotifyThresholdSeconds: vi.fn().mockResolvedValue(0),
      ListShells: vi.fn().mockResolvedValue(["/bin/zsh"]),
      NewSession: newSessionMock,
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

    const wrapper = mount(App, {
      global: {
        stubs: {
          TitleBar: true,
          TabBar: {
            props: ["tabs"],
            template: `<div data-testid="tabbar-stub" :data-tab-count="tabs.length"></div>`,
          },
          PluginHost: true,
          TranslatePanelHost: true,
          ShortcutHints: true,
          TaskSidebar: true,
          PaneGrid: true,
        },
      },
    });
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    await flushPromises();
    // Boot auto-start already skipped (Task 3.2); reset the mock so this
    // test only observes the keyboard shortcut's own behavior.
    newSessionMock.mockClear();

    // Mirror useTerminalShortcuts' own platform detection (Meta on mac,
    // Control elsewhere) so the synthetic event matches whichever modifier
    // this test run resolves to — setting both would trip its
    // wrong-modifier rejection.
    const isMac = navigator.platform?.toLowerCase().includes("mac") ?? false;
    document.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "t",
        code: "KeyT",
        metaKey: isMac,
        ctrlKey: !isMac,
        bubbles: true,
        cancelable: true,
      }),
    );
    await flushPromises();

    expect(newSessionMock).not.toHaveBeenCalled();
    expect(wrapper.find('[data-testid="tabbar-stub"]').attributes("data-tab-count")).toBe("0");
  });
});

describe("admin view (main-area swap)", () => {
  // Minimal TabBar stub: reflects isAdmin/adminOpen props back onto a
  // data-test button (so a test can assert App wired them through) and
  // exposes two triggers — toggle-admin (mirrors the real admin button) and
  // activate (mirrors clicking a session tab, i.e. gotoTab).
  const tabBarStub = {
    props: ["isAdmin", "adminOpen"],
    template: `
      <div>
        <button v-if="isAdmin" data-testid="toggle-admin" :class="{ active: adminOpen }" @click="$emit('toggle-admin')">admin</button>
        <button data-testid="activate-session-tab" @click="$emit('activate', 'tab-1')">tab</button>
      </div>
    `,
  };

  function mountAdminApp() {
    return mount(App, {
      global: {
        stubs: {
          TitleBar: true,
          TaskSidebar: true,
          PluginHost: true,
          TranslatePanelHost: true,
          ShortcutHints: true,
          PaneGrid: { template: `<div data-testid="pane-grid-stub" />` },
          AdminPanel: { template: `<div data-testid="admin-panel-stub" />` },
          TabBar: tabBarStub,
        },
      },
    });
  }

  beforeEach(() => {
    setActivePinia(createPinia());
  });

  afterEach(() => {
    __setBindingsForTest(undefined);
    __setPlatformForTests(null);
  });

  it("isAdmin=true + adminViewOpen=true renders AdminPanel and hides PaneGrid", async () => {
    const platform = createFakePlatform();
    platform.relay.fetchMe = vi.fn().mockResolvedValue({ user_id: "u1", email: "a@b.com", is_admin: true });
    __setPlatformForTests(platform);

    const wrapper = mountAdminApp();
    await flushPromises();

    // Closed by default — PaneGrid's absence here is coincidental (no
    // localEndpoint in this test), the real assertion is AdminPanel's gate.
    expect(wrapper.find('[data-testid="admin-panel-stub"]').exists()).toBe(false);

    await wrapper.get('[data-testid="toggle-admin"]').trigger("click");
    await flushPromises();

    expect(wrapper.find('[data-testid="admin-panel-stub"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="pane-grid-stub"]').exists()).toBe(false);
  });

  it("clicking a session tab (gotoTab) closes the admin view", async () => {
    const platform = createFakePlatform();
    platform.relay.fetchMe = vi.fn().mockResolvedValue({ user_id: "u1", email: "a@b.com", is_admin: true });
    __setPlatformForTests(platform);

    const wrapper = mountAdminApp();
    await flushPromises();

    await wrapper.get('[data-testid="toggle-admin"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="admin-panel-stub"]').exists()).toBe(true);

    await wrapper.get('[data-testid="activate-session-tab"]').trigger("click");
    await flushPromises();

    expect(wrapper.find('[data-testid="admin-panel-stub"]').exists()).toBe(false);
  });

  it("isAdmin transitioning true -> false auto-resets adminViewOpen", async () => {
    const platform = createFakePlatform();
    platform.relay.fetchMe = vi.fn().mockResolvedValue({ user_id: "u1", email: "a@b.com", is_admin: true });
    __setPlatformForTests(platform);

    const wrapper = mountAdminApp();
    await flushPromises();

    await wrapper.get('[data-testid="toggle-admin"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="admin-panel-stub"]').exists()).toBe(true);

    // Simulate admin rights being revoked mid-session: App.vue exposes `me`
    // (see defineExpose at the bottom of App.vue) purely so this transition
    // is testable without a second live fetchMe() round-trip.
    (wrapper.vm as any).me = { user_id: "u1", email: "a@b.com", is_admin: false };
    await flushPromises();

    expect(wrapper.find('[data-testid="admin-panel-stub"]').exists()).toBe(false);
  });
});

describe("recovery snapshot gated on caps.wailsBindings (web)", () => {
  // Regression for the "Wails 绑定尚未就绪" uncaught error on web: App.vue used
  // to construct useRecoverySnapshot unconditionally, whose structural watcher
  // called saveRecoverySnapshot() -> lib/api's bindings() -> throws
  // synchronously because the web build has no window.go. The throw happened
  // 500ms later inside a setTimeout callback, so it escaped as uncaught.
  //
  // This test deliberately leaves bindings unstubbed (no __setBindingsForTest
  // call) so window.go stays absent, exactly reproducing the web environment.
  // It restores a web tabs snapshot (lib/webTabsSnapshot) at boot — the same
  // kind of post-mount tabs mutation that used to trigger the composable's
  // deep watcher — and asserts saveRecoverySnapshot is never invoked and
  // nothing throws once the 500ms structural debounce window has passed.
  beforeEach(() => {
    setActivePinia(createPinia());
    localStorage.clear();
    sessionStorage.clear();
    saveSnapshot({
      tabs: [
        {
          id: "web-t1",
          layout: "single",
          active_pane_idx: 0,
          panes: [{ slot: 0, session_id: "remote-1", host_id: "host-remote" }],
        },
      ],
      active_tab_id: "web-t1",
    });
  });

  afterEach(() => {
    __setPlatformForTests(null);
    localStorage.clear();
    sessionStorage.clear();
  });

  it("never calls saveRecoverySnapshot and does not throw when restoring a web tabs snapshot", async () => {
    const saveSpy = vi.spyOn(api, "saveRecoverySnapshot");

    const platform = createFakePlatform();
    platform.caps = { ...platform.caps, wailsBindings: false, localPty: false, windowControls: false };
    platform.sessions.listRemoteSessions = vi.fn().mockResolvedValue([]);
    __setPlatformForTests(platform);

    const wrapper = mount(App, {
      global: {
        stubs: {
          TitleBar: true,
          TabBar: true,
          PluginHost: true,
          TranslatePanelHost: true,
          ShortcutHints: true,
          TaskSidebar: true,
          PaneGrid: true,
        },
      },
    });
    await flushPromises();

    // The web snapshot restore above pushes a tab post-mount — exactly the
    // kind of structural tabs mutation that used to schedule a 500ms flush
    // through the (unconditionally-constructed) recovery composable.
    await new Promise((resolve) => setTimeout(resolve, 600));
    await flushPromises();

    expect(saveSpy).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});
