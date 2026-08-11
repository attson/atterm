import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import WidgetApp from "./WidgetApp.vue";
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

describe("WidgetApp", () => {
  beforeEach(() => {
    stateHandler = null;
    bootstrapHandler = null;
    vi.stubGlobal("ResizeObserver", ResizeObserverStub);
  });

  it("renders the shared running state icon for running rows", async () => {
    const w = mount(WidgetApp);
    bootstrapHandler?.({ collapsed: false, x: 0, y: 0, locale: "zh-CN" });
    stateHandler?.({
      mood: "running",
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
          unread: true,
        },
      ],
      overflowCount: 0,
      aiOnly: false,
    });
    await w.vm.$nextTick();

    expect(w.find('.task-state-icon[data-state="completed"][data-unread="true"]').exists()).toBe(true);
    expect(w.find("circle.task-unread-dot").exists()).toBe(true);
    expect(w.find("path.task-completed-check").exists()).toBe(false);
  });
});
