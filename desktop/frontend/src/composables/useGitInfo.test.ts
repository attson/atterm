import { describe, it, expect, vi, afterEach } from "vitest";
import { effectScope, ref } from "vue";
import { useGitInfo } from "./useGitInfo";
import type { GitInfo } from "../lib/api/git";

const g = (cwd: string, branch = "main"): GitInfo => ({ cwd, branch, added: 0, deleted: 0 });

function flush() {
  return new Promise((r) => setTimeout(r, 0));
}

describe("useGitInfo", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("fetches immediately with deduped non-empty cwds and exposes the map", async () => {
    const fetcher = vi.fn(async (cwds: string[]) => cwds.map((c) => g(c)));
    const cwds = ref<string[]>(["/a", "/a", "", "/b"]);
    const scope = effectScope();
    let out!: ReturnType<typeof useGitInfo>;
    scope.run(() => { out = useGitInfo(cwds, fetcher); });
    await flush();
    expect(fetcher).toHaveBeenCalledWith(["/a", "/b"]);
    expect(out.byCwd.value.get("/a")?.branch).toBe("main");
    scope.stop();
  });

  it("refetches when the cwd set changes, not on reorder", async () => {
    const fetcher = vi.fn(async (cwds: string[]) => cwds.map((c) => g(c)));
    const cwds = ref<string[]>(["/a", "/b"]);
    const scope = effectScope();
    scope.run(() => useGitInfo(cwds, fetcher));
    await flush();
    expect(fetcher).toHaveBeenCalledTimes(1);
    cwds.value = ["/b", "/a"]; // same set
    await flush();
    expect(fetcher).toHaveBeenCalledTimes(1);
    cwds.value = ["/a", "/c"];
    await flush();
    expect(fetcher).toHaveBeenCalledTimes(2);
    scope.stop();
  });

  it("keeps the previous map when a poll fails", async () => {
    let fail = false;
    const fetcher = vi.fn(async (cwds: string[]) => {
      if (fail) throw new Error("boom");
      return cwds.map((c) => g(c));
    });
    const cwds = ref<string[]>(["/a"]);
    const scope = effectScope();
    let out!: ReturnType<typeof useGitInfo>;
    scope.run(() => { out = useGitInfo(cwds, fetcher); });
    await flush();
    expect(out.byCwd.value.size).toBe(1);
    fail = true;
    await out.refresh();
    expect(out.byCwd.value.size).toBe(1);
    scope.stop();
  });

  it("polls on the interval and stops when the scope is disposed", async () => {
    vi.useFakeTimers();
    const fetcher = vi.fn(async (cwds: string[]) => cwds.map((c) => g(c)));
    const cwds = ref<string[]>(["/a"]);
    const scope = effectScope();
    scope.run(() => useGitInfo(cwds, fetcher, { intervalMs: 1000 }));
    await vi.advanceTimersByTimeAsync(0);
    expect(fetcher).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(2100);
    expect(fetcher).toHaveBeenCalledTimes(3);
    scope.stop();
    await vi.advanceTimersByTimeAsync(3000);
    expect(fetcher).toHaveBeenCalledTimes(3);
  });

  it("never polls when disabled (web/mobile: no Wails binding)", async () => {
    vi.useFakeTimers();
    const fetcher = vi.fn(async () => []);
    const cwds = ref<string[]>(["/a"]);
    const scope = effectScope();
    scope.run(() => useGitInfo(cwds, fetcher, { enabled: false }));
    await vi.advanceTimersByTimeAsync(30_000);
    expect(fetcher).not.toHaveBeenCalled();
    scope.stop();
  });
});
