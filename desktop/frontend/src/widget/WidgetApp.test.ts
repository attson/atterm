import { mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import WidgetApp from "./WidgetApp.vue";
import { widgetBridge } from "./bridge";
import type { WidgetState } from "../lib/widgetState";

let stateHandler: ((state: WidgetState) => void) | null = null;
let bootstrapHandler: ((boot: { collapsed: boolean; x: number; y: number; locale: string }) => void) | null = null;

vi.mock("./bridge", () => ({
  onWidgetBootstrap: vi.fn((cb) => {
    bootstrapHandler = cb;
  }),
  onWidgetState: vi.fn((cb) => {
    stateHandler = cb;
  }),
  widgetBridge: {
    activate: vi.fn(),
    hide: vi.fn(),
    mute: vi.fn(),
    ready: vi.fn(),
    reportPosition: vi.fn(),
    resize: vi.fn(),
    setAiOnly: vi.fn(),
    setCollapsed: vi.fn(),
  },
}));

class ResizeObserverStub {
  observe() {}
  disconnect() {}
}

let animationFrames: FrameRequestCallback[] = [];

async function flushAnimationFrames() {
  // scheduleCardResize first waits for Vue's DOM flush, then requests a frame.
  await Promise.resolve();
  await Promise.resolve();
  const callbacks = animationFrames;
  animationFrames = [];
  callbacks.forEach((cb) => cb(performance.now()));
  await Promise.resolve();
}

describe("WidgetApp", () => {
  beforeEach(() => {
    stateHandler = null;
    bootstrapHandler = null;
    animationFrames = [];
    vi.stubGlobal("ResizeObserver", ResizeObserverStub);
    vi.stubGlobal("requestAnimationFrame", vi.fn((cb: FrameRequestCallback) => {
      animationFrames.push(cb);
      return animationFrames.length;
    }));
    vi.stubGlobal("cancelAnimationFrame", vi.fn());
    vi.mocked(widgetBridge.resize).mockClear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("actively shrinks the native window after restoring collapsed state", async () => {
    const w = mount(WidgetApp);
    const card = w.get<HTMLElement>(".widget-window").element;
    vi.spyOn(card, "getBoundingClientRect").mockReturnValue({
      width: 252,
      height: 60,
      top: 0,
      right: 252,
      bottom: 60,
      left: 0,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    });

    // The ResizeObserver stub deliberately never invokes its callback. The
    // state transition itself must still resize the OS window, otherwise its
    // transparent 172px startup area keeps intercepting clicks below the card.
    bootstrapHandler?.({ collapsed: true, x: 0, y: 0, locale: "zh-CN" });
    await w.vm.$nextTick();
    await flushAnimationFrames();

    expect(w.find(".rows").exists()).toBe(false);
    expect(widgetBridge.resize).toHaveBeenLastCalledWith(60);
  });

  it("remeasures the rendered card after manual collapse and expansion", async () => {
    const w = mount(WidgetApp);
    const card = w.get<HTMLElement>(".widget-window").element;
    vi.spyOn(card, "getBoundingClientRect").mockImplementation(() => {
      const height = w.find(".rows").exists() ? 140 : 60;
      return {
        width: 252,
        height,
        top: 0,
        right: 252,
        bottom: height,
        left: 0,
        x: 0,
        y: 0,
        toJSON: () => ({}),
      };
    });
    await flushAnimationFrames();
    vi.mocked(widgetBridge.resize).mockClear();

    await w.get(".widget-header").trigger("click");
    await flushAnimationFrames();

    expect(w.find(".rows").exists()).toBe(false);
    expect(widgetBridge.setCollapsed).toHaveBeenLastCalledWith(true);
    expect(widgetBridge.resize).toHaveBeenLastCalledWith(60);

    await w.get(".widget-header").trigger("click");
    await flushAnimationFrames();

    expect(w.find(".rows").exists()).toBe(true);
    expect(widgetBridge.setCollapsed).toHaveBeenLastCalledWith(false);
    expect(widgetBridge.resize).toHaveBeenLastCalledWith(140);
  });

  it("renders the shared running state icon for running rows", async () => {
    const w = mount(WidgetApp);
    bootstrapHandler?.({ collapsed: false, x: 0, y: 0, locale: "zh-CN" });
    stateHandler?.({
      mood: "running",
      unreadCount: 0,
      waitingCount: 0,
      runningCount: 1,
      failedCount: 0,
      completedCount: 0,
      idleCount: 0,
      headline: "1 个在跑",
      subline: "",
      rows: [
        {
          sessionId: "run-1",
          title: "codex",
          subtitle: "~/proj",
          state: "running",
          taskState: "running",
          kind: "codex",
          remoteHost: "",
          ageMs: 12_000,
          lastOutputAt: 0,
          unread: false,
        },
      ],
      overflowCount: 0,
      aiOnly: false,
    });
    await w.vm.$nextTick();

    expect(w.find('[data-state="running"]').exists()).toBe(true);
    expect(w.find("path.task-spinner").exists()).toBe(true);
  });

  it("renders unread in the main state icon for unread completed rows", async () => {
    const w = mount(WidgetApp);
    bootstrapHandler?.({ collapsed: false, x: 0, y: 0, locale: "zh-CN" });
    stateHandler?.({
      mood: "idle",
      unreadCount: 1,
      waitingCount: 0,
      runningCount: 0,
      failedCount: 0,
      completedCount: 1,
      idleCount: 0,
      headline: "都跑完了",
      subline: "1 个已完成",
      rows: [
        {
          sessionId: "done-1",
          title: "codex",
          subtitle: "~/proj",
          state: "idle",
          taskState: "completed",
          kind: "codex",
          remoteHost: "",
          ageMs: 0,
          lastOutputAt: 0,
          unread: true,
        },
      ],
      overflowCount: 0,
      aiOnly: false,
    });
    await w.vm.$nextTick();

    expect(w.find('.task-state-icon[data-state="completed"][data-unread="true"]').exists()).toBe(true);
    expect(w.find("path.task-unread-star").exists()).toBe(true);
    expect(w.find("path.task-completed-check").exists()).toBe(false);
  });

  it("uses unread sessions for the sprite badge instead of waiting sessions", async () => {
    const w = mount(WidgetApp);
    stateHandler?.({
      mood: "waiting",
      unreadCount: 12,
      waitingCount: 1,
      runningCount: 2,
      failedCount: 1,
      completedCount: 0,
      idleCount: 4,
      headline: "12 个会话有最新内容",
      subline: "1 个失败 · 2 个在跑 · 1 个等待输入",
      rows: [],
      overflowCount: 0,
      aiOnly: false,
    });
    await w.vm.$nextTick();

    expect(w.get('[data-test="widget-unread-badge"]').text()).toBe("12");
  });

  it("hides the sprite badge when there are no unread sessions", async () => {
    const w = mount(WidgetApp);
    stateHandler?.({
      mood: "waiting",
      unreadCount: 0,
      waitingCount: 1,
      runningCount: 0,
      failedCount: 0,
      completedCount: 0,
      idleCount: 0,
      headline: "暂无最新内容",
      subline: "1 个等待输入",
      rows: [],
      overflowCount: 0,
      aiOnly: false,
    });
    await w.vm.$nextTick();

    expect(w.find('[data-test="widget-unread-badge"]').exists()).toBe(false);
  });

  it("stacks AI kind above live output in one fixed right tail", async () => {
    const now = 1_800_000_000_000;
    vi.useFakeTimers();
    vi.setSystemTime(now);
    const w = mount(WidgetApp);
    stateHandler?.({
      mood: "running",
      unreadCount: 0,
      waitingCount: 0,
      runningCount: 1,
      failedCount: 0,
      completedCount: 0,
      idleCount: 0,
      headline: "1 个在跑",
      subline: "",
      rows: [{
        sessionId: "ai",
        title: "A very long Codex session title",
        subtitle: "~/code/atterm",
        state: "running",
        taskState: "running",
        kind: "codex",
        remoteHost: "",
        ageMs: 12_000,
        lastOutputAt: now / 1000 - 1,
        unread: false,
      }],
      overflowCount: 0,
      aiOnly: false,
    });
    await w.vm.$nextTick();

    const tail = w.get('[data-test="widget-row-tail"]');
    expect(tail.get(".kind").text()).toBe("codex");
    expect(tail.get('[data-test="last-output"]').text()).toBe("live");
    expect(tail.get('[data-test="last-output"]').attributes("aria-label")).toBe("Output active");
  });

  it("shows a time-only tail for non-AI sessions and no empty tail without a timestamp", async () => {
    const now = 1_800_000_000_000;
    vi.useFakeTimers();
    vi.setSystemTime(now);
    const w = mount(WidgetApp);
    stateHandler?.({
      mood: "idle",
      unreadCount: 0,
      waitingCount: 0,
      runningCount: 0,
      failedCount: 0,
      completedCount: 0,
      idleCount: 2,
      headline: "2 个空闲",
      subline: "",
      rows: [
        {
          sessionId: "timed-shell", title: "shell", subtitle: "/tmp", state: "idle",
          taskState: "idle", kind: "", remoteHost: "", ageMs: 0,
          lastOutputAt: now / 1000 - 60, unread: false,
        },
        {
          sessionId: "silent-shell", title: "shell", subtitle: "/tmp", state: "idle",
          taskState: "idle", kind: "", remoteHost: "", ageMs: 0,
          lastOutputAt: 0, unread: false,
        },
      ],
      overflowCount: 0,
      aiOnly: false,
    });
    await w.vm.$nextTick();

    const rows = w.findAll(".row");
    expect(rows[0].get('[data-test="widget-row-tail"]').classes()).toContain("time-only");
    expect(rows[0].get('[data-test="last-output"]').text()).toBe("1m");
    expect(rows[1].find('[data-test="widget-row-tail"]').exists()).toBe(false);
  });

  it("falls from live to now using the existing local clock without a new snapshot", async () => {
    const now = 1_800_000_000_000;
    vi.useFakeTimers();
    vi.setSystemTime(now);
    const w = mount(WidgetApp);
    stateHandler?.({
      mood: "running",
      unreadCount: 0,
      waitingCount: 0,
      runningCount: 1,
      failedCount: 0,
      completedCount: 0,
      idleCount: 0,
      headline: "1 个在跑",
      subline: "",
      rows: [{
        sessionId: "run", title: "build", subtitle: "/tmp", state: "running",
        taskState: "running", kind: "", remoteHost: "", ageMs: 1_000,
        lastOutputAt: now / 1000 - 1, unread: false,
      }],
      overflowCount: 0,
      aiOnly: false,
    });
    await w.vm.$nextTick();
    expect(w.get('[data-test="last-output"]').text()).toBe("live");

    await vi.advanceTimersByTimeAsync(5_000);
    await w.vm.$nextTick();
    expect(w.get('[data-test="last-output"]').text()).toBe("now");
  });

  // Builds a snapshot whose single row carries the given attention state.
  function stateWithRow(state: WidgetState["mood"]): WidgetState {
    return {
      mood: state,
      unreadCount: 0,
      waitingCount: state === "waiting" ? 1 : 0,
      runningCount: state === "running" ? 1 : 0,
      failedCount: state === "failed" ? 1 : 0,
      completedCount: 0,
      idleCount: 0,
      headline: "",
      subline: "",
      rows: [{
        sessionId: "s1", title: "claude", subtitle: "~/proj", state,
        taskState: state === "waiting" ? "waiting_input" : state === "failed" ? "failed" : "running",
        kind: "claude", remoteHost: "", ageMs: 0, lastOutputAt: 0, unread: false,
      }],
      overflowCount: 0,
      aiOnly: false,
    };
  }

  it("highlights a row that newly escalated while collapsed, then clears it after the auto-peek", async () => {
    vi.useFakeTimers();
    const w = mount(WidgetApp);
    bootstrapHandler?.({ collapsed: true, x: 0, y: 0, locale: "zh-CN" });

    // A running session — no attention, no peek, no highlight.
    stateHandler?.(stateWithRow("running"));
    await w.vm.$nextTick();
    expect(w.find(".row.highlighted").exists()).toBe(false);

    // It escalates to waiting_input: the collapsed widget peeks open and the
    // escalated row is highlighted.
    stateHandler?.(stateWithRow("waiting"));
    await w.vm.$nextTick();
    expect(w.find(".row.highlighted").exists()).toBe(true);

    // The highlight survives most of the 15s window...
    await vi.advanceTimersByTimeAsync(14_000);
    await w.vm.$nextTick();
    expect(w.find(".row.highlighted").exists()).toBe(true);

    // ...and clears once the auto-peek ends.
    await vi.advanceTimersByTimeAsync(1_000);
    await w.vm.$nextTick();
    expect(w.find(".row.highlighted").exists()).toBe(false);
  });

  it("does not re-peek for a session that was already waiting", async () => {
    vi.useFakeTimers();
    const w = mount(WidgetApp);
    bootstrapHandler?.({ collapsed: true, x: 0, y: 0, locale: "zh-CN" });

    // First waiting snapshot raises the hand and highlights the row.
    stateHandler?.(stateWithRow("waiting"));
    await w.vm.$nextTick();
    expect(w.find(".row.highlighted").exists()).toBe(true);

    // Let that window fully close.
    await vi.advanceTimersByTimeAsync(15_000);
    await w.vm.$nextTick();
    expect(w.find(".rows").exists()).toBe(false);

    // The same session, still waiting, pushed again: not a new escalation, so
    // no fresh peek and no highlight.
    stateHandler?.(stateWithRow("waiting"));
    await w.vm.$nextTick();
    expect(w.find(".rows").exists()).toBe(false);
    expect(w.find(".row.highlighted").exists()).toBe(false);
  });
});
