import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { __setPlatformForTests } from "./platform";
import { createFakePlatform } from "./platform/__tests__/_fakePlatform";
import { __setBindingsForTest } from "./lib/api";
import { __resetForTests as resetSel, useSessionSelection } from "./composables/useSessionSelection";
import App from "./App.vue";

// jsdom does not implement matchMedia; stub it so xterm's ScreenDprMonitor
// does not throw unhandled rejections when mounting App. (mirrors App.test.ts)
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
// (mirrors App.test.ts)
if (typeof window !== "undefined" && !(window as any).runtime) {
  (window as any).runtime = {
    EventsOnMultiple: vi.fn(() => () => {}),
    EventsOn: vi.fn(() => () => {}),
    EventsOff: vi.fn(),
    EventsOffAll: vi.fn(),
    EventsEmit: vi.fn(),
  };
}

// Same fake-transport / bindings setup as the "remote tab session retention"
// describe block in App.test.ts — App.vue opens a real WebSocket for the
// local session-list connection on boot, so it must be stubbed even though
// these tests never touch it directly.
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
}

// Selection stubs for TaskSidebar: App.vue reads sel.selectedIds via the
// same module-scoped useSessionSelection() singleton, so tests can seed
// selection directly instead of driving the real sidebar's row UI.
const sel = useSessionSelection();

function mergeCloseStub() {
  return {
    template: `<div>
      <button data-testid="merge-selected" @click="$emit('merge-selected')">merge</button>
      <button data-testid="close-selected" @click="$emit('close-selected')">close-selected</button>
    </div>`,
  };
}

function paneGridStub() {
  return {
    props: ["tab"],
    template: `
      <div data-testid="pane-grid" :data-tab-id="tab.id" :data-layout="tab.layout">
        <div
          v-for="(pane, i) in tab.panes"
          :key="i"
          :data-testid="pane.sessionId ? 'pane-session' : 'pane-empty'"
          :data-session-id="pane.sessionId || ''"
          :data-remote="pane.remote ? '1' : '0'"
        >{{ pane.sessionId || 'empty' }}</div>
      </div>
    `,
  };
}

let closeSessionMock: ReturnType<typeof vi.fn>;

async function mountApp() {
  const wrapper = mount(App, {
    global: {
      stubs: {
        TitleBar: true,
        TabBar: true,
        PluginHost: true,
        TranslatePanelHost: true,
        ShortcutHints: true,
        TaskSidebar: mergeCloseStub(),
        PaneGrid: paneGridStub(),
      },
    },
  });
  await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
  await flushPromises();
  return wrapper;
}

function visiblePaneGrid(wrapper: Awaited<ReturnType<typeof mountApp>>) {
  return wrapper.findAll('[data-testid="pane-grid"]').find((g) => g.isVisible());
}

describe("App — merge & batch close (integration)", () => {
  beforeEach(() => {
    resetSel();
    setActivePinia(createPinia());
    __setPlatformForTests(createFakePlatform());
    closeSessionMock = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest({
      ListRemoteSessions: vi.fn().mockResolvedValue(JSON.stringify([])),
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
      GetShortcutBindings: vi.fn().mockResolvedValue({}),
      GetTerminalFontHead: vi.fn().mockResolvedValue(""),
      GetTerminalFontSize: vi.fn().mockResolvedValue(13),
      GetTerminalLineHeight: vi.fn().mockResolvedValue(1.0),
      GetTerminalCursorStyle: vi.fn().mockResolvedValue("block"),
      GetTerminalCursorBlink: vi.fn().mockResolvedValue(true),
      GetTerminalScrollback: vi.fn().mockResolvedValue(5000),
      GetCommandNotifyThresholdSeconds: vi.fn().mockResolvedValue(0),
      ListShells: vi.fn().mockResolvedValue(["/bin/zsh"]),
      NewSession: vi.fn().mockResolvedValue({ session_id: "local-1" }),
      CloseSession: closeSessionMock,
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
    resetSel();
  });

  it("mergeSelectedIntoTab creates a grid2x2 tab from 3 selected sessions", async () => {
    const wrapper = await mountApp();
    // Boot auto-starts one local single-pane tab (session "local-1"); none
    // of the merged ids below are open, so that tab is left untouched.
    expect(wrapper.findAll('[data-testid="pane-grid"]')).toHaveLength(1);

    sel.selectOnly("remote-a");
    sel.toggle("remote-b");
    sel.toggle("remote-c");

    await wrapper.get('[data-testid="merge-selected"]').trigger("click");
    await flushPromises();

    const grids = wrapper.findAll('[data-testid="pane-grid"]');
    expect(grids).toHaveLength(2); // untouched local-1 tab + new merged tab

    const visible = visiblePaneGrid(wrapper);
    expect(visible?.attributes("data-layout")).toBe("grid2x2");
    const sessions = visible?.findAll('[data-testid="pane-session"]') ?? [];
    expect(sessions.map((s) => s.attributes("data-session-id")).sort()).toEqual([
      "remote-a",
      "remote-b",
      "remote-c",
    ]);
    for (const s of sessions) expect(s.attributes("data-remote")).toBe("1");
    expect(visible?.findAll('[data-testid="pane-empty"]')).toHaveLength(1);

    expect(closeSessionMock).not.toHaveBeenCalled();
    expect(sel.selectedIds.value.size).toBe(0);
  });

  it("mergeSelectedIntoTab with 1 open + 2 unopened detaches the open pane and closes empty source tab", async () => {
    const wrapper = await mountApp();
    expect(wrapper.findAll('[data-testid="pane-grid"]')).toHaveLength(1);

    // "local-1" is the boot-started local session, alone in its own
    // single-layout tab — the common merge case that exercises the
    // closePaneAt -> closeTab(detachOnly) cascade.
    sel.selectOnly("local-1");
    sel.toggle("remote-x");
    sel.toggle("remote-y");

    await wrapper.get('[data-testid="merge-selected"]').trigger("click");
    await flushPromises();

    // The load-bearing assertion: detaching "local-1" into the merged tab
    // must NOT kill the local shell, even though removing it emptied (and
    // therefore closed) its original single-pane tab.
    expect(closeSessionMock).not.toHaveBeenCalledWith("local-1");

    const grids = wrapper.findAll('[data-testid="pane-grid"]');
    expect(grids).toHaveLength(1); // original tab closed once emptied

    const visible = visiblePaneGrid(wrapper);
    expect(visible?.attributes("data-layout")).toBe("grid2x2");
    const localPane = visible?.find('[data-session-id="local-1"]');
    expect(localPane?.exists()).toBe(true);
    // Spec: merged panes are always remote:true, matching the
    // sidebar-driven openRemoteAsTab pattern.
    expect(localPane?.attributes("data-remote")).toBe("1");
  });

  it("closeSelectedOpen closes only the open subset among the selection", async () => {
    const wrapper = await mountApp();
    expect(wrapper.findAll('[data-testid="pane-grid"]')).toHaveLength(1);

    // "local-1" is open (boot-started tab); "remote-never-opened" has no
    // pane anywhere and must be silently skipped.
    sel.selectOnly("local-1");
    sel.toggle("remote-never-opened");

    await wrapper.get('[data-testid="close-selected"]').trigger("click");
    await flushPromises();

    expect(closeSessionMock).toHaveBeenCalledWith("local-1");
    expect(closeSessionMock).not.toHaveBeenCalledWith("remote-never-opened");
    expect(wrapper.findAll('[data-testid="pane-grid"]')).toHaveLength(0);
    // closeSelectedOpen clears the whole selection unconditionally (mirrors
    // mergeSelectedIntoTab) — the unopened id is left untouched, not left
    // selected.
    expect(sel.selectedIds.value.size).toBe(0);
  });

  it("mergeSelectedIntoTab is a no-op when count > 4", async () => {
    const wrapper = await mountApp();
    expect(wrapper.findAll('[data-testid="pane-grid"]')).toHaveLength(1);

    sel.selectOnly("s1");
    for (const id of ["s2", "s3", "s4", "s5"]) sel.toggle(id);
    expect(sel.selectedIds.value.size).toBe(5);

    await wrapper.get('[data-testid="merge-selected"]').trigger("click");
    await flushPromises();

    expect(wrapper.findAll('[data-testid="pane-grid"]')).toHaveLength(1);
    // Early-return guard bails before sel.clear() — selection survives so
    // the UI can show the user why nothing happened.
    expect(sel.selectedIds.value.size).toBe(5);
  });
});
