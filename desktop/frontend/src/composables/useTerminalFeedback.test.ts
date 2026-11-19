import { effectScope, ref } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Status } from "../lib/connection";
import { useTerminalFeedback } from "./useTerminalFeedback";

const scopes: ReturnType<typeof effectScope>[] = [];
function feedback(waitForOutput = false) {
  const status = ref<Status>("connecting");
  const timeout = vi.fn();
  const scope = effectScope();
  scopes.push(scope);
  const state = scope.run(() => useTerminalFeedback(status, timeout, waitForOutput))!;
  return { status, timeout, ...state };
}
afterEach(() => {
  scopes.splice(0).forEach(scope => scope.stop());
  vi.useRealTimers();
});

describe("terminal loading feedback", () => {
  it("waits for host output when an empty relay cache finishes before the lazy stream arrives", () => {
    const view = feedback(true);
    view.status.value = "attached";
    view.onReplayProgress({ phase: "end", bytes: 0, total_bytes: 0 })?.();
    expect(view.waitingForOutput.value).toBe(true);
    expect(view.initialLoading.value).toBe(true);
    const outputRendered = view.onOutput();
    expect(view.initialLoading.value).toBe(true);
    outputRendered?.();
    expect(view.initialLoading.value).toBe(false);
    expect(view.waitingForOutput.value).toBe(false);
  });

  it("keeps rendered output across reconnects that resume with an empty replay", () => {
    const view = feedback(true);
    const outputRendered = view.onOutput();
    view.status.value = "reconnecting";
    outputRendered?.();
    view.status.value = "attached";
    view.onReplayProgress({ phase: "end", bytes: 0, total_bytes: 0 })?.();
    expect(view.initialLoading.value).toBe(false);
  });

  it("finishes a route handover that has no additional replay", () => {
    const view = feedback();
    view.status.value = "attached";
    view.onReplayProgress({ phase: "end", bytes: 0, total_bytes: 0 })?.();
    view.status.value = "reconnecting";
    expect(view.loading.value).toBe(true);
    view.status.value = "attached";
    expect(view.loading.value).toBe(false);
  });

  it("keeps first-load feedback after the socket opens until replay has rendered", () => {
    const view = feedback();
    expect(view.initialLoading.value).toBe(true);
    view.status.value = "attached";
    expect(view.initialLoading.value).toBe(true);
    view.onReplayProgress({ phase: "chunk", bytes: 512, total_bytes: 1024 });
    expect(view.replayProgress.value?.bytes).toBe(512);
    const rendered = view.onReplayProgress({ phase: "end", bytes: 1024, total_bytes: 1024 });
    expect(view.renderingReplay.value).toBe(true);
    expect(view.initialLoading.value).toBe(true);
    rendered?.();
    expect(view.loading.value).toBe(false);
    expect(view.initialLoading.value).toBe(false);
  });

  it("does not let an old write-drain hide a newer replay", () => {
    const view = feedback();
    view.status.value = "attached";
    const oldDrain = view.onReplayProgress({ phase: "end", bytes: 0, total_bytes: 0 });
    view.status.value = "reconnecting";
    view.status.value = "attached";
    view.onReplayProgress({ phase: "start", bytes: 0, total_bytes: 2048 });
    oldDrain?.();
    expect(view.initialLoading.value).toBe(true);
    expect(view.replayProgress.value?.total_bytes).toBe(2048);
  });

  it("keeps existing terminal content visible on subsequent reconnects and clears stale progress on failure", () => {
    const view = feedback();
    view.status.value = "attached";
    view.onReplayProgress({ phase: "end", bytes: 0, total_bytes: 0 })?.();
    view.status.value = "reconnecting";
    expect(view.loading.value).toBe(true);
    expect(view.initialLoading.value).toBe(false);
    view.onReplayProgress({ phase: "chunk", bytes: 8, total_bytes: 16 });
    view.status.value = "error";
    expect(view.loading.value).toBe(false);
    expect(view.replayProgress.value).toBeNull();
  });
});

describe("terminal takeover feedback", () => {
  it("ignores repeated Space/click requests while awaiting authoritative driver confirmation", () => {
    vi.useFakeTimers();
    const view = feedback();
    const claim = vi.fn(() => true);
    view.requestTakeControl(claim);
    view.requestTakeControl(claim);
    expect(claim).toHaveBeenCalledTimes(1);
    expect(view.takeoverPending.value).toBe(true);
    view.onDriverChange(false);
    expect(view.takeoverPending.value).toBe(true);
    view.onDriverChange(true);
    expect(view.takeoverPending.value).toBe(false);
    vi.advanceTimersByTime(15_000);
    expect(view.timeout).not.toHaveBeenCalled();
  });

  it("shows a queued takeover during reconnect and permits retry after timeout", () => {
    vi.useFakeTimers();
    const view = feedback();
    const claim = vi.fn(() => false);
    view.requestTakeControl(claim);
    expect(view.takeoverQueued.value).toBe(true);
    view.status.value = "reconnecting";
    expect(view.takeoverPending.value).toBe(true);
    vi.advanceTimersByTime(15_000);
    expect(view.takeoverPending.value).toBe(false);
    expect(view.timeout).toHaveBeenCalledOnce();
    view.requestTakeControl(claim);
    expect(claim).toHaveBeenCalledTimes(2);
  });

  it("cleans up takeover timers on session end, send failure, and disposal", () => {
    vi.useFakeTimers();
    const view = feedback();
    view.requestTakeControl(() => true);
    view.status.value = "ended";
    expect(view.takeoverPending.value).toBe(false);
    expect(() => view.requestTakeControl(() => { throw new Error("closed"); })).toThrow("closed");
    expect(view.takeoverPending.value).toBe(false);
    view.requestTakeControl(() => true);
    scopes[0].stop();
    vi.advanceTimersByTime(15_000);
    expect(view.timeout).not.toHaveBeenCalled();
  });
});
