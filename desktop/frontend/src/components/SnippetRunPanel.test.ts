import { describe, it, expect, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SnippetRunPanel from "./SnippetRunPanel.vue";
import { resetI18nForTest } from "../i18n";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform, fakeEventBus } from "../platform/__tests__/_fakePlatform";
import type { SnippetRunProgress } from "../lib/api";

const listSSHHosts = vi.fn();
const runSnippetOnHosts = vi.fn();
const cancelSnippetRun = vi.fn();
vi.mock("../lib/api", async () => {
  const actual = await vi.importActual<typeof import("../lib/api")>("../lib/api");
  return {
    ...actual,
    listSSHHosts: (...a: unknown[]) => listSSHHosts(...a),
    runSnippetOnHosts: (...a: unknown[]) => runSnippetOnHosts(...a),
    cancelSnippetRun: (...a: unknown[]) => cancelSnippetRun(...a),
  };
});

const HOST_A = { id: "h1", host: "10.0.0.1", user: "root", auth_kind: "password" as const, alias: "alpha" };
const HOST_B = { id: "h2", host: "10.0.0.2", user: "root", auth_kind: "password" as const, alias: "beta" };
const HOST_C = { id: "h3", host: "10.0.0.3", user: "root", auth_kind: "password" as const, alias: "gamma" };

async function mountPanel(events = fakeEventBus()) {
  __setPlatformForTests({ ...createFakePlatform(), events });
  const w = mount(SnippetRunPanel, { props: { snippetId: "snip-1" } });
  await flushPromises();
  return { w, events };
}

async function selectAndRun(w: Awaited<ReturnType<typeof mountPanel>>["w"], hostIds: string[]) {
  for (const id of hostIds) {
    await w.find(`[data-testid="snippet-run-host-${id}"]`).setValue(true);
  }
  await w.find('[data-testid="snippet-run-start"]').trigger("click");
  await flushPromises();
}

describe("SnippetRunPanel", () => {
  beforeEach(() => {
    resetI18nForTest();
    listSSHHosts.mockReset().mockResolvedValue([HOST_A, HOST_B, HOST_C]);
    runSnippetOnHosts.mockReset().mockResolvedValue("run-1");
    cancelSnippetRun.mockReset().mockResolvedValue(undefined);
  });

  it("renders one pending row per selected host, and only the selected hosts", async () => {
    const { w } = await mountPanel();
    await selectAndRun(w, [HOST_A.id, HOST_B.id]);

    expect(runSnippetOnHosts).toHaveBeenCalledWith("snip-1", [HOST_A.id, HOST_B.id]);
    expect(w.find(`[data-testid="snippet-run-row-${HOST_A.id}"]`).exists()).toBe(true);
    expect(w.find(`[data-testid="snippet-run-row-${HOST_B.id}"]`).exists()).toBe(true);
    expect(w.find(`[data-testid="snippet-run-row-${HOST_C.id}"]`).exists()).toBe(false);
    expect(w.find(`[data-testid="snippet-run-state-${HOST_A.id}"]`).attributes("data-state")).toBe("pending");
    expect(w.find(`[data-testid="snippet-run-state-${HOST_B.id}"]`).attributes("data-state")).toBe("pending");
  });

  it("the run button is disabled with no hosts selected", async () => {
    const { w } = await mountPanel();
    const btn = w.find('[data-testid="snippet-run-start"]');
    expect(btn.attributes("disabled")).toBeDefined();
    await w.find(`[data-testid="snippet-run-host-${HOST_A.id}"]`).setValue(true);
    expect(w.find('[data-testid="snippet-run-start"]').attributes("disabled")).toBeUndefined();
  });

  it("a snippet:run:progress event updates only its own host's row", async () => {
    const { w, events } = await mountPanel();
    await selectAndRun(w, [HOST_A.id, HOST_B.id]);

    const progress: SnippetRunProgress = {
      run_id: "run-1",
      result: { host_id: HOST_A.id, host_label: "alpha", state: "running", exit_code: 0, output: "", truncated: false },
    };
    events.emit("snippet:run:progress", progress);
    await flushPromises();

    expect(w.find(`[data-testid="snippet-run-state-${HOST_A.id}"]`).attributes("data-state")).toBe("running");
    // Host B's row must be untouched by an event about host A.
    expect(w.find(`[data-testid="snippet-run-state-${HOST_B.id}"]`).attributes("data-state")).toBe("pending");
  });

  it("ignores progress events for a different run id", async () => {
    const { w, events } = await mountPanel();
    await selectAndRun(w, [HOST_A.id]);

    events.emit("snippet:run:progress", {
      run_id: "some-other-run",
      result: { host_id: HOST_A.id, host_label: "alpha", state: "ok", exit_code: 0, output: "hi", truncated: false },
    } satisfies SnippetRunProgress);
    await flushPromises();

    expect(w.find(`[data-testid="snippet-run-state-${HOST_A.id}"]`).attributes("data-state")).toBe("pending");
  });

  it("a failed row shows the exit code and output, and is not styled or worded as an error", async () => {
    const { w, events } = await mountPanel();
    await selectAndRun(w, [HOST_A.id]);

    events.emit("snippet:run:progress", {
      run_id: "run-1",
      result: { host_id: HOST_A.id, host_label: "alpha", state: "failed", exit_code: 7, output: "boom\n", truncated: false },
    } satisfies SnippetRunProgress);
    await flushPromises();

    const row = w.find(`[data-testid="snippet-run-row-${HOST_A.id}"]`);
    expect(row.attributes("data-state")).toBe("failed");
    expect(row.text()).toContain("7");
    expect(row.text()).toContain("boom");
    // Must read as "failed" (ran, exited non-zero), never as "error" (never ran).
    expect(row.text().toLowerCase()).not.toContain("error");
  });

  it("an error row shows the message, has no exit code, and is not styled or worded as failed", async () => {
    const { w, events } = await mountPanel();
    await selectAndRun(w, [HOST_A.id]);

    events.emit("snippet:run:progress", {
      run_id: "run-1",
      result: {
        host_id: HOST_A.id,
        host_label: "alpha",
        state: "error",
        exit_code: 0,
        output: "",
        truncated: false,
        error: "dial tcp 10.0.0.1:22: connection refused",
      },
    } satisfies SnippetRunProgress);
    await flushPromises();

    const row = w.find(`[data-testid="snippet-run-row-${HOST_A.id}"]`);
    expect(row.attributes("data-state")).toBe("error");
    expect(row.text()).toContain("connection refused");
    // Must read as "error" (never ran), never as "failed" (ran, exited non-zero).
    expect(row.text().toLowerCase()).not.toContain("failed");
    // No exit code should be shown for a host that never ran.
    expect(row.find('[data-testid^="snippet-run-exitcode-"]').exists()).toBe(false);
  });

  it("a truncated row says so, rather than silently showing a clipped tail", async () => {
    const { w, events } = await mountPanel();
    await selectAndRun(w, [HOST_A.id]);

    events.emit("snippet:run:progress", {
      run_id: "run-1",
      result: { host_id: HOST_A.id, host_label: "alpha", state: "ok", exit_code: 0, output: "x".repeat(10), truncated: true },
    } satisfies SnippetRunProgress);
    await flushPromises();

    expect(w.find(`[data-testid="snippet-run-truncated-${HOST_A.id}"]`).exists()).toBe(true);
    expect(w.text()).toMatch(/truncated/i);
  });

  it("does not show a truncated notice when output was not truncated", async () => {
    const { w, events } = await mountPanel();
    await selectAndRun(w, [HOST_A.id]);

    events.emit("snippet:run:progress", {
      run_id: "run-1",
      result: { host_id: HOST_A.id, host_label: "alpha", state: "ok", exit_code: 0, output: "fine", truncated: false },
    } satisfies SnippetRunProgress);
    await flushPromises();

    expect(w.find(`[data-testid="snippet-run-truncated-${HOST_A.id}"]`).exists()).toBe(false);
  });

  it("cancel calls cancelSnippetRun with the live run id", async () => {
    const { w } = await mountPanel();
    await selectAndRun(w, [HOST_A.id, HOST_B.id]);

    await w.find('[data-testid="snippet-run-cancel"]').trigger("click");
    await flushPromises();

    expect(cancelSnippetRun).toHaveBeenCalledWith("run-1");
  });

  it("hides the cancel button once every host has reached a terminal state", async () => {
    const { w, events } = await mountPanel();
    await selectAndRun(w, [HOST_A.id]);
    expect(w.find('[data-testid="snippet-run-cancel"]').exists()).toBe(true);

    events.emit("snippet:run:progress", {
      run_id: "run-1",
      result: { host_id: HOST_A.id, host_label: "alpha", state: "ok", exit_code: 0, output: "done", truncated: false },
    } satisfies SnippetRunProgress);
    await flushPromises();

    expect(w.find('[data-testid="snippet-run-cancel"]').exists()).toBe(false);
  });

  it("copy-all concatenates every host's output with a per-host header", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    const original = navigator.clipboard;
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    try {
      const { w, events } = await mountPanel();
      await selectAndRun(w, [HOST_A.id, HOST_B.id]);

      events.emit("snippet:run:progress", {
        run_id: "run-1",
        result: { host_id: HOST_A.id, host_label: "alpha", state: "ok", exit_code: 0, output: "output-a", truncated: false },
      } satisfies SnippetRunProgress);
      events.emit("snippet:run:progress", {
        run_id: "run-1",
        result: { host_id: HOST_B.id, host_label: "beta", state: "failed", exit_code: 3, output: "output-b", truncated: false },
      } satisfies SnippetRunProgress);
      await flushPromises();

      await w.find('[data-testid="snippet-run-copyall"]').trigger("click");
      await flushPromises();

      expect(writeText).toHaveBeenCalledTimes(1);
      const text = writeText.mock.calls[0][0] as string;
      expect(text.indexOf("alpha")).toBeGreaterThanOrEqual(0);
      expect(text.indexOf("output-a")).toBeGreaterThan(text.indexOf("alpha"));
      expect(text.indexOf("beta")).toBeGreaterThan(text.indexOf("output-a"));
      expect(text.indexOf("output-b")).toBeGreaterThan(text.indexOf("beta"));
    } finally {
      Object.defineProperty(navigator, "clipboard", { configurable: true, value: original });
    }
  });
});
