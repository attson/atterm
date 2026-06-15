import { describe, expect, test, vi } from "vitest";
import { mount } from "@vue/test-utils";
import PaneSplitter from "./PaneSplitter.vue";

// jsdom doesn't ship PointerEvent; polyfill by extending MouseEvent so
// dispatchEvent(new PointerEvent(...)) works the same way it does in WebKit.
if (typeof (globalThis as { PointerEvent?: unknown }).PointerEvent === "undefined") {
  class PointerEventPolyfill extends MouseEvent {
    pointerId: number;
    constructor(type: string, init: PointerEventInit = {}) {
      super(type, init);
      this.pointerId = init.pointerId ?? 0;
    }
  }
  (globalThis as { PointerEvent: unknown }).PointerEvent =
    PointerEventPolyfill as unknown as typeof PointerEvent;
}

function makeRect(width = 1000, height = 800): DOMRect {
  return {
    x: 0,
    y: 0,
    top: 0,
    left: 0,
    right: width,
    bottom: height,
    width,
    height,
    toJSON: () => ({}),
  } as DOMRect;
}

function stubPointerCapture(el: HTMLElement) {
  (el as unknown as { setPointerCapture: (id: number) => void }).setPointerCapture = vi.fn();
  (el as unknown as { releasePointerCapture: (id: number) => void }).releasePointerCapture = vi.fn();
}

describe("PaneSplitter", () => {
  test("renders col-orientation class", () => {
    const w = mount(PaneSplitter, {
      props: { orientation: "col", ratio: 0.5, containerRect: () => makeRect() },
    });
    expect(w.find(".pane-splitter.col").exists()).toBe(true);
  });

  test("renders row-orientation class", () => {
    const w = mount(PaneSplitter, {
      props: { orientation: "row", ratio: 0.5, containerRect: () => makeRect() },
    });
    expect(w.find(".pane-splitter.row").exists()).toBe(true);
  });

  test("pointermove emits update:ratio with delta/width for col", () => {
    const rect = makeRect(1000, 800);
    const w = mount(PaneSplitter, {
      props: { orientation: "col", ratio: 0.5, containerRect: () => rect },
      attachTo: document.body,
    });
    const el = w.find(".pane-splitter").element as HTMLElement;
    stubPointerCapture(el);
    el.dispatchEvent(new PointerEvent("pointerdown", { pointerId: 1, clientX: 500, clientY: 0, button: 0 }));
    el.dispatchEvent(new PointerEvent("pointermove", { pointerId: 1, clientX: 600, clientY: 0 }));
    const events = w.emitted("update:ratio");
    expect(events).toBeTruthy();
    // delta = 100, width = 1000 → next = 0.5 + 0.1 = 0.6
    expect(events![events!.length - 1][0]).toBeCloseTo(0.6, 5);
    w.unmount();
  });

  test("pointermove emits update:ratio with delta/height for row", () => {
    const rect = makeRect(1000, 800);
    const w = mount(PaneSplitter, {
      props: { orientation: "row", ratio: 0.5, containerRect: () => rect },
      attachTo: document.body,
    });
    const el = w.find(".pane-splitter").element as HTMLElement;
    stubPointerCapture(el);
    el.dispatchEvent(new PointerEvent("pointerdown", { pointerId: 1, clientX: 0, clientY: 400, button: 0 }));
    el.dispatchEvent(new PointerEvent("pointermove", { pointerId: 1, clientX: 0, clientY: 480 }));
    const events = w.emitted("update:ratio");
    expect(events).toBeTruthy();
    // delta = 80, height = 800 → next = 0.5 + 0.1 = 0.6
    expect(events![events!.length - 1][0]).toBeCloseTo(0.6, 5);
    w.unmount();
  });

  test("update:ratio is clamped to [0.1, 0.9]", () => {
    const rect = makeRect(1000, 800);
    const w = mount(PaneSplitter, {
      props: { orientation: "col", ratio: 0.5, containerRect: () => rect },
      attachTo: document.body,
    });
    const el = w.find(".pane-splitter").element as HTMLElement;
    stubPointerCapture(el);
    el.dispatchEvent(new PointerEvent("pointerdown", { pointerId: 1, clientX: 500, clientY: 0, button: 0 }));
    el.dispatchEvent(new PointerEvent("pointermove", { pointerId: 1, clientX: 5000, clientY: 0 }));
    const events = w.emitted("update:ratio")!;
    expect(events[events.length - 1][0]).toBeCloseTo(0.9, 5);
    el.dispatchEvent(new PointerEvent("pointermove", { pointerId: 1, clientX: -5000, clientY: 0 }));
    const events2 = w.emitted("update:ratio")!;
    expect(events2[events2.length - 1][0]).toBeCloseTo(0.1, 5);
    w.unmount();
  });

  test("pointerup emits commit", () => {
    const rect = makeRect(1000, 800);
    const w = mount(PaneSplitter, {
      props: { orientation: "col", ratio: 0.5, containerRect: () => rect },
      attachTo: document.body,
    });
    const el = w.find(".pane-splitter").element as HTMLElement;
    stubPointerCapture(el);
    el.dispatchEvent(new PointerEvent("pointerdown", { pointerId: 1, clientX: 500, clientY: 0, button: 0 }));
    el.dispatchEvent(new PointerEvent("pointerup", { pointerId: 1, clientX: 500, clientY: 0 }));
    expect(w.emitted("commit")).toBeTruthy();
    w.unmount();
  });

  test("dblclick emits reset", async () => {
    const w = mount(PaneSplitter, {
      props: { orientation: "col", ratio: 0.7, containerRect: () => makeRect() },
    });
    await w.find(".pane-splitter").trigger("dblclick");
    expect(w.emitted("reset")).toBeTruthy();
  });

  test("ignores right mouse button on pointerdown", () => {
    const rect = makeRect(1000, 800);
    const w = mount(PaneSplitter, {
      props: { orientation: "col", ratio: 0.5, containerRect: () => rect },
      attachTo: document.body,
    });
    const el = w.find(".pane-splitter").element as HTMLElement;
    stubPointerCapture(el);
    // button=2 is right-click. PaneSplitter must NOT engage drag.
    el.dispatchEvent(new PointerEvent("pointerdown", { pointerId: 1, clientX: 500, clientY: 0, button: 2 }));
    el.dispatchEvent(new PointerEvent("pointermove", { pointerId: 1, clientX: 600, clientY: 0 }));
    expect(w.emitted("update:ratio")).toBeUndefined();
    w.unmount();
  });
});
