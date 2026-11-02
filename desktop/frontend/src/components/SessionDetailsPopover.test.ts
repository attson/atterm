import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { mount } from "@vue/test-utils";
import SessionDetailsPopover from "./SessionDetailsPopover.vue";
import source from "./SessionDetailsPopover.vue?raw";
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
  aiSessionId?: string;
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
      aiSessionId: over.aiSessionId ?? "",
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

  test("renders a confirmed AI session id for AI sessions", async () => {
    const w = factory({
      session: mkSession({ type: "ai" }),
      aiSessionId: "019faea7-292e-7ad3-a408-4faf2bb8a848",
    });
    const row = w.find("[data-test=details-field-aiSessionId]");
    expect(row.find(".value").text()).toBe("019faea7-292e-7ad3-a408-4faf2bb8a848");

    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    await row.find(".copy").trigger("click");
    expect(writeText).toHaveBeenCalledWith("019faea7-292e-7ad3-a408-4faf2bb8a848");
  });

  test("hides the AI session id until it is confirmed and for non-AI sessions", async () => {
    const w = factory({ session: mkSession({ type: "ai" }) });
    expect(w.find("[data-test=details-field-aiSessionId]").exists()).toBe(false);

    await w.setProps({ aiSessionId: "ai-sid", session: mkSession({ type: "shell" }) });
    expect(w.find("[data-test=details-field-aiSessionId]").exists()).toBe(false);
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

  test("drags by the title and keeps the card inside the viewport", async () => {
    const w = factory();
    const popover = w.find<HTMLElement>("[data-test=session-details-popover]");
    const handle = w.find("[data-test=session-details-drag-handle]");
    vi.spyOn(popover.element, "getBoundingClientRect").mockReturnValue({
      x: 200,
      y: 200,
      left: 200,
      top: 200,
      right: 620,
      bottom: 500,
      width: 420,
      height: 300,
      toJSON: () => ({}),
    });
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 800 });
    Object.defineProperty(window, "innerHeight", { configurable: true, value: 600 });

    await handle.trigger("pointerdown", { button: 0, pointerId: 7, clientX: 240, clientY: 220 });
    expect(handle.classes()).toContain("dragging");
    await handle.trigger("pointermove", { pointerId: 7, clientX: 790, clientY: 590 });
    expect(popover.attributes("style")).toContain("left: 380px");
    expect(popover.attributes("style")).toContain("top: 300px");

    await handle.trigger("pointerup", { pointerId: 7 });
    expect(handle.classes()).not.toContain("dragging");
  });

  test("ignores non-primary pointer drags", async () => {
    const w = factory();
    const handle = w.find("[data-test=session-details-drag-handle]");
    await handle.trigger("pointerdown", { button: 2, pointerId: 9, clientX: 240, clientY: 220 });
    expect(handle.classes()).not.toContain("dragging");
  });

  test("details rows allow horizontal scrolling for long values on narrow screens", () => {
    const rowsRule = source.match(/\.rows\s*\{[^}]*\}/)?.[0] ?? "";
    const rowRule = source.match(/\.row\s*\{[^}]*\}/)?.[0] ?? "";
    const valueRule = source.match(/\.value\s*\{[^}]*\}/)?.[0] ?? "";
    expect(rowsRule).toContain("overflow-x: auto");
    expect(rowsRule).toContain("-webkit-overflow-scrolling: touch");
    expect(rowRule).toContain("min-width: max-content");
    expect(valueRule).toContain("overflow: visible");
    expect(valueRule).toContain("text-overflow: clip");
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
