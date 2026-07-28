import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { mount } from "@vue/test-utils";
import SessionDetailsPopover from "./SessionDetailsPopover.vue";
import type { RemoteSession } from "../platform/types";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform } from "../platform/__tests__/_fakePlatform";

// SessionDetailsPopover mounts useSessionPins() unconditionally on setup —
// pins now go through platform.sessions.getPins/setPins.
beforeEach(() => {
  __setPlatformForTests(createFakePlatform());
});
afterEach(() => {
  __setPlatformForTests(null);
});

function mkSession(over: Partial<RemoteSession> = {}): RemoteSession {
  return {
    session_id: "sess-abc",
    host_id: "h1",
    host: "laptop",
    user: "attson",
    title: "shell",
    cwd: "/Users/attson/proj",
    cols: 80,
    rows: 24,
    started_at: 1_700_000_000,
    task_state: "running",
    current_command: "npm run dev",
    unread: false,
    ...over,
  } as RemoteSession;
}

function factory(over: {
  session?: RemoteSession | null;
  paneLocation?: { tabId: string; paneIdx: number } | null;
} = {}) {
  const session = over.session === undefined ? mkSession() : over.session;
  const paneLocation = over.paneLocation === undefined ? null : over.paneLocation;
  return mount(SessionDetailsPopover, {
    attachTo: document.body,
    props: {
      open: true,
      x: 200,
      y: 200,
      session,
      paneLocation,
      tabIndexById: (id: string) => (id === "t1" ? 1 : 0),
    },
  });
}

describe("SessionDetailsPopover", () => {
  test("does not render when open=false", async () => {
    const w = factory();
    await w.setProps({ open: false });
    expect(w.find("[data-test=session-details-popover]").exists()).toBe(false);
  });

  test("does not render when session is null", () => {
    const w = factory({ session: null });
    expect(w.find("[data-test=session-details-popover]").exists()).toBe(false);
  });

  test("renders session_id row", () => {
    const w = factory();
    expect(w.find("[data-test=details-field-sessionId] .value").text()).toBe("sess-abc");
  });

  test("skips optional rows when value is empty", () => {
    const w = factory({
      session: mkSession({
        current_command: undefined,
        command_started_at: undefined,
      }),
    });
    expect(w.find("[data-test=details-field-command]").exists()).toBe(true); // title fallback
    expect(w.find("[data-test=details-field-commandStartedAt]").exists()).toBe(false);
  });

  test("renders pane location row when provided", () => {
    const w = factory({
      paneLocation: { tabId: "t1", paneIdx: 2 },
    });
    const row = w.find("[data-test=details-field-paneLocation]");
    expect(row.exists()).toBe(true);
    expect(row.find(".value").text()).toBe("Tab 1 · Pane 3");
  });

  test("renders 'not open' when paneLocation is null", () => {
    const w = factory({ paneLocation: null });
    expect(w.find("[data-test=details-field-paneLocation]").exists()).toBe(true);
  });

  test("copy button writes to clipboard", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    const w = factory();
    await w.find("[data-test=details-field-sessionId] .copy").trigger("click");
    expect(writeText).toHaveBeenCalledWith("sess-abc");
  });

  test("Escape emits close", async () => {
    const w = factory();
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    expect(w.emitted("close")).toHaveLength(1);
    w.unmount();
  });

  test("outside mousedown emits close", async () => {
    const w = factory();
    document.body.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    expect(w.emitted("close")).toHaveLength(1);
    w.unmount();
  });
});
