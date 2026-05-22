import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { effectScope } from "vue";
import { useLongPressModifier } from "./useLongPressModifier";

function fireKey(type: "keydown" | "keyup", init: KeyboardEventInit & { key: string }) {
  const ev = new KeyboardEvent(type, { ...init, bubbles: true, cancelable: true });
  document.dispatchEvent(ev);
  return ev;
}

function fireBlur() {
  window.dispatchEvent(new Event("blur"));
}

describe("useLongPressModifier", () => {
  let scope: ReturnType<typeof effectScope>;
  const onShow = vi.fn();
  const onHide = vi.fn();

  beforeEach(() => {
    vi.useFakeTimers();
    onShow.mockReset();
    onHide.mockReset();
    scope = effectScope();
    scope.run(() => useLongPressModifier({ mod: "Control", thresholdMs: 3000, onShow, onHide }));
  });

  afterEach(() => {
    scope.stop();
    vi.useRealTimers();
  });

  it("3s hold of Control -> onShow", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(3000);
    expect(onShow).toHaveBeenCalledTimes(1);
    expect(onHide).not.toHaveBeenCalled();
  });

  it("release before 3s -> neither callback fires", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(2500);
    fireKey("keyup", { key: "Control" });
    expect(onShow).not.toHaveBeenCalled();
    expect(onHide).not.toHaveBeenCalled();
  });

  it("press non-modifier before 3s -> timer canceled, onShow not called", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(1000);
    fireKey("keydown", { key: "n", code: "KeyN", ctrlKey: true });
    vi.advanceTimersByTime(3000);
    expect(onShow).not.toHaveBeenCalled();
  });

  it("press non-modifier after 3s -> onHide fires", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(3000);
    expect(onShow).toHaveBeenCalledTimes(1);
    fireKey("keydown", { key: "n", code: "KeyN", ctrlKey: true });
    expect(onHide).toHaveBeenCalledTimes(1);
  });

  it("release after 3s -> onHide fires", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(3000);
    fireKey("keyup", { key: "Control" });
    expect(onShow).toHaveBeenCalledTimes(1);
    expect(onHide).toHaveBeenCalledTimes(1);
  });

  it("window blur after 3s -> onHide fires", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(3000);
    fireBlur();
    expect(onHide).toHaveBeenCalledTimes(1);
  });

  it("e.repeat=true Control while no timer -> no timer started", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true, repeat: true });
    vi.advanceTimersByTime(3000);
    expect(onShow).not.toHaveBeenCalled();
  });

  it("e.repeat=true while timer running -> timer keeps running", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(1500);
    fireKey("keydown", { key: "Control", ctrlKey: true, repeat: true });
    vi.advanceTimersByTime(1500);
    expect(onShow).toHaveBeenCalledTimes(1);
  });

  it("Alt joining before 3s cancels the timer", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(1000);
    fireKey("keydown", { key: "Alt", ctrlKey: true, altKey: true });
    vi.advanceTimersByTime(3000);
    expect(onShow).not.toHaveBeenCalled();
  });

  it("Control press while Alt already held does not start timer", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true, altKey: true });
    vi.advanceTimersByTime(3000);
    expect(onShow).not.toHaveBeenCalled();
  });

  it("Mod=Meta variant: 3s Meta hold -> onShow", () => {
    scope.stop();
    onShow.mockReset();
    onHide.mockReset();
    scope = effectScope();
    scope.run(() => useLongPressModifier({ mod: "Meta", thresholdMs: 3000, onShow, onHide }));
    fireKey("keydown", { key: "Meta", metaKey: true });
    vi.advanceTimersByTime(3000);
    expect(onShow).toHaveBeenCalledTimes(1);
  });

  it("scope.stop unbinds all listeners", () => {
    scope.stop();
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(3000);
    expect(onShow).not.toHaveBeenCalled();
  });
});
