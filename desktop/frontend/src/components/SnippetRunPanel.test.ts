import { describe, it, expect, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ref } from "vue";
import SnippetRunPanel from "./SnippetRunPanel.vue";
import panelSource from "./SnippetRunPanel.vue?raw";
import { resetI18nForTest } from "../i18n";
import { __setPlatformForTests } from "../platform";
import { createFakePlatform, fakeEventBus } from "../platform/__tests__/_fakePlatform";
import type { SnippetRunProgress } from "../lib/api";
import type { RemoteSession } from "../platform/types";
import type { SnippetSessionTargetRegistry } from "../lib/snippetSessionTargets";

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
// No alias. Every other fixture has one, which hid the fact that the panel's
// label rule and Go's sshHostLabel disagreed for exactly this case.
const HOST_NOALIAS = { id: "h4", host: "10.0.0.4", user: "root", auth_kind: "password" as const, alias: "" };

const SNIPPET_LABEL = "Snip One";
const SNIPPET_TEXT = "echo hi";

const SESSION_A: RemoteSession = {
  session_id: "s1",
  host_id: "local",
  host: "workstation",
  user: "attson",
  title: "API server",
  cwd: "/work/api",
  cols: 120,
  rows: 32,
  current_command: "npm run dev",
};
const SESSION_B: RemoteSession = {
  ...SESSION_A,
  session_id: "s2",
  title: "Worker",
  cwd: "/work/worker",
  current_command: "go test ./...",
};
const SESSION_REMOTE: RemoteSession = {
  ...SESSION_A,
  session_id: "s3",
  host_id: "remote",
  host: "buildbox",
  user: "root",
  title: "Release",
  cwd: "/srv/release",
};

async function mountPanel(
  events = fakeEventBus(),
  target: "hosts" | "sessions" = "hosts",
  sessions: RemoteSession[] = [],
  targets: SnippetSessionTargetRegistry = new Map(),
) {
  __setPlatformForTests({ ...createFakePlatform(), events });
  const w = mount(SnippetRunPanel, {
    props: { snippetLabel: SNIPPET_LABEL, snippetText: SNIPPET_TEXT },
    global: {
      provide: {
        "atterm:snippetSessions": ref(sessions),
        "atterm:snippetSessionTargets": targets,
      },
    },
  });
  await flushPromises();
  if (target === "hosts") {
    await w.find('[data-testid="snippet-target-hosts"]').trigger("click");
  }
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

  it("uses a wide modal with a responsive multi-column host list", async () => {
    const { w } = await mountPanel();

    expect(w.find(".snippet-run-modal").exists()).toBe(true);
    expect(w.find(".snippet-run-drawer").exists()).toBe(false);
    expect(panelSource).toMatch(/\.snippet-run-modal\s*\{[\s\S]*?width:\s*min\(960px/);
    expect(panelSource).toMatch(/\.host-list\s*\{[\s\S]*?grid-template-columns:\s*repeat\(auto-fill/);
  });

  it("defaults to terminal sessions and can switch to saved SSH hosts", async () => {
    const targets: SnippetSessionTargetRegistry = new Map([
      [SESSION_A.session_id, { send: vi.fn(), canSend: () => true }],
    ]);
    const { w } = await mountPanel(fakeEventBus(), "sessions", [SESSION_A], targets);

    expect(w.find('[data-testid="snippet-target-sessions"]').attributes("aria-pressed")).toBe("true");
    expect(w.find(`[data-testid="snippet-run-session-${SESSION_A.session_id}"]`).exists()).toBe(true);

    await w.find('[data-testid="snippet-target-hosts"]').trigger("click");
    expect(w.find('[data-testid="snippet-target-hosts"]').attributes("aria-pressed")).toBe("true");
    expect(w.find(`[data-testid="snippet-run-host-${HOST_A.id}"]`).exists()).toBe(true);
  });

  it("shows all known sessions but only lets writable mounted drivers be selected", async () => {
    const targets: SnippetSessionTargetRegistry = new Map([
      [SESSION_A.session_id, { send: vi.fn(), canSend: () => true }],
      [SESSION_B.session_id, { send: vi.fn(), canSend: () => false }],
    ]);
    const { w } = await mountPanel(
      fakeEventBus(),
      "sessions",
      [SESSION_A, SESSION_B, SESSION_REMOTE],
      targets,
    );

    expect(w.find(`[data-testid="snippet-run-session-${SESSION_A.session_id}"]`).attributes("disabled")).toBeUndefined();
    expect(w.find(`[data-testid="snippet-run-session-${SESSION_B.session_id}"]`).attributes("disabled")).toBeDefined();
    expect(w.find(`[data-testid="snippet-run-session-${SESSION_REMOTE.session_id}"]`).attributes("disabled")).toBeDefined();
    expect(w.text()).toContain("Not writable now");
    expect(w.text()).toContain("Not open in this window");
  });

  it("searches sessions by command and keeps host grouping visible", async () => {
    const targets: SnippetSessionTargetRegistry = new Map([
      [SESSION_A.session_id, { send: vi.fn(), canSend: () => true }],
      [SESSION_REMOTE.session_id, { send: vi.fn(), canSend: () => true }],
    ]);
    const { w } = await mountPanel(
      fakeEventBus(),
      "sessions",
      [SESSION_A, SESSION_REMOTE],
      targets,
    );

    await w.find('[data-testid="snippet-run-session-search"]').setValue("release");

    expect(w.find(`[data-testid="snippet-run-session-${SESSION_A.session_id}"]`).exists()).toBe(false);
    expect(w.find(`[data-testid="snippet-run-session-${SESSION_REMOTE.session_id}"]`).exists()).toBe(true);
    expect(w.text()).toContain("buildbox");
  });

  it("sends text and carriage return through each existing session target", async () => {
    vi.useFakeTimers();
    try {
      const sendA = vi.fn();
      const sendB = vi.fn();
      const targets: SnippetSessionTargetRegistry = new Map([
        [SESSION_A.session_id, { send: sendA, canSend: () => true }],
        [SESSION_B.session_id, { send: sendB, canSend: () => true }],
      ]);
      const { w } = await mountPanel(fakeEventBus(), "sessions", [SESSION_A, SESSION_B], targets);
      await w.find(`[data-testid="snippet-run-session-${SESSION_A.session_id}"]`).setValue(true);
      await w.find(`[data-testid="snippet-run-session-${SESSION_B.session_id}"]`).setValue(true);

      await w.find('[data-testid="snippet-run-start"]').trigger("click");
      expect(sendA).toHaveBeenCalledTimes(1);
      expect(sendA).toHaveBeenNthCalledWith(1, SNIPPET_TEXT);
      expect(sendB).toHaveBeenNthCalledWith(1, SNIPPET_TEXT);

      await vi.advanceTimersByTimeAsync(16);
      await flushPromises();

      expect(sendA.mock.calls).toEqual([[SNIPPET_TEXT], ["\r"]]);
      expect(sendB.mock.calls).toEqual([[SNIPPET_TEXT], ["\r"]]);
      expect(w.find(`[data-testid="snippet-run-session-state-${SESSION_A.session_id}"]`).text()).toBe("Sent");
      expect(runSnippetOnHosts).not.toHaveBeenCalled();
      expect(panelSource).not.toContain("sendInputToSession");
      expect(panelSource).not.toContain("new SessionConnection");
    } finally {
      vi.useRealTimers();
    }
  });

  it("marks a selected session unsent if it loses driver permission before execution", async () => {
    let writable = true;
    const send = vi.fn();
    const targets: SnippetSessionTargetRegistry = new Map([
      [SESSION_A.session_id, { send, canSend: () => writable }],
    ]);
    const { w } = await mountPanel(fakeEventBus(), "sessions", [SESSION_A], targets);
    await w.find(`[data-testid="snippet-run-session-${SESSION_A.session_id}"]`).setValue(true);
    writable = false;

    await w.find('[data-testid="snippet-run-start"]').trigger("click");
    await flushPromises();

    expect(send).not.toHaveBeenCalled();
    expect(w.find(`[data-testid="snippet-run-session-state-${SESSION_A.session_id}"]`).attributes("data-state")).toBe("error");
    expect(w.find('[data-testid="snippet-run-session-retry"]').exists()).toBe(true);
  });

  it("renders one pending row per selected host, and only the selected hosts", async () => {
    const { w } = await mountPanel();
    await selectAndRun(w, [HOST_A.id, HOST_B.id]);

    expect(runSnippetOnHosts).toHaveBeenCalledWith(SNIPPET_LABEL, SNIPPET_TEXT, [HOST_A.id, HOST_B.id]);
    expect(w.find(`[data-testid="snippet-run-row-${HOST_A.id}"]`).exists()).toBe(true);
    expect(w.find(`[data-testid="snippet-run-row-${HOST_B.id}"]`).exists()).toBe(true);
    expect(w.find(`[data-testid="snippet-run-row-${HOST_C.id}"]`).exists()).toBe(false);
    expect(w.find(`[data-testid="snippet-run-state-${HOST_A.id}"]`).attributes("data-state")).toBe("pending");
    expect(w.find(`[data-testid="snippet-run-state-${HOST_B.id}"]`).attributes("data-state")).toBe("pending");
    // F8: pending is routed through t(), not a hardcoded '—'.
    expect(w.find(`[data-testid="snippet-run-state-${HOST_A.id}"]`).text()).toBe("Pending");
  });

  it("the run button is disabled with no hosts selected", async () => {
    const { w } = await mountPanel();
    const btn = w.find('[data-testid="snippet-run-start"]');
    expect(btn.attributes("disabled")).toBeDefined();
    await w.find(`[data-testid="snippet-run-host-${HOST_A.id}"]`).setValue(true);
    expect(w.find('[data-testid="snippet-run-start"]').attributes("disabled")).toBeUndefined();
  });

  it("filters hosts by search and renders matching group paths", async () => {
    listSSHHosts.mockReset().mockResolvedValue([
      { ...HOST_A, tags: ["prod"], group_path: ["Production", "API"] },
      { ...HOST_B, tags: ["staging"], group_path: ["Staging"] },
      HOST_C,
    ]);
    const { w } = await mountPanel();

    await w.find('[data-testid="snippet-run-search"]').setValue("production");

    expect(w.find(`[data-testid="snippet-run-host-${HOST_A.id}"]`).exists()).toBe(true);
    expect(w.find(`[data-testid="snippet-run-host-${HOST_B.id}"]`).exists()).toBe(false);
    expect(w.find(`[data-testid="snippet-run-host-${HOST_C.id}"]`).exists()).toBe(false);
    expect(w.text()).toContain("Production / API");
  });

  it("selects only the current tag-filtered results and preserves hidden selections", async () => {
    listSSHHosts.mockReset().mockResolvedValue([
      { ...HOST_A, tags: ["prod"] },
      { ...HOST_B, tags: ["staging"] },
      { ...HOST_C, tags: ["prod"] },
    ]);
    const { w } = await mountPanel();

    await w.find(`[data-testid="snippet-run-host-${HOST_B.id}"]`).setValue(true);
    await w.find('[data-testid="snippet-run-tag-prod"]').trigger("click");
    await w.find('[data-testid="snippet-run-select-visible"]').setValue(true);

    expect(w.find('[data-testid="snippet-run-start"]').text()).toBe("Run on 3 hosts");
    await w.find('[data-testid="snippet-run-tag-prod"]').trigger("click");
    expect((w.find(`[data-testid="snippet-run-host-${HOST_B.id}"]`).element as HTMLInputElement).checked).toBe(true);
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

  // MAJOR F1: Go can spawn host goroutines and emit "running"/a terminal
  // event before the RunSnippetOnHosts promise resolves with the run id
  // (the id only reaches the frontend after the Wails round trip). A row
  // that arrives this early must not be dropped and stuck at "pending"
  // forever — it must be buffered and replayed once the run id is known.
  it("buffers a progress event that arrives before the run id resolves, and applies it once resolved", async () => {
    let resolveRun!: (id: string) => void;
    runSnippetOnHosts.mockReset().mockImplementation(
      () => new Promise<string>((resolve) => { resolveRun = resolve; }),
    );
    const { w, events } = await mountPanel();

    await w.find(`[data-testid="snippet-run-host-${HOST_A.id}"]`).setValue(true);
    await w.find('[data-testid="snippet-run-start"]').trigger("click");
    await flushPromises();

    // Terminal error arrives before RunSnippetOnHosts has resolved with a
    // run id at all — the panel is still on the host-select phase.
    expect(w.find(`[data-testid="snippet-run-row-${HOST_A.id}"]`).exists()).toBe(false);
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

    resolveRun("run-1");
    await flushPromises();

    const row = w.find(`[data-testid="snippet-run-row-${HOST_A.id}"]`);
    expect(row.exists()).toBe(true);
    expect(row.attributes("data-state")).toBe("error");
    expect(row.text()).toContain("connection refused");
    // isRunActive must reflect the buffered terminal result too, not just
    // whatever markRunning saw live — otherwise Cancel would stay forever.
    expect(w.find('[data-testid="snippet-run-cancel"]').exists()).toBe(false);
  });

  it("does not replay a buffered event belonging to a different run", async () => {
    let resolveRun!: (id: string) => void;
    runSnippetOnHosts.mockReset().mockImplementation(
      () => new Promise<string>((resolve) => { resolveRun = resolve; }),
    );
    const { w, events } = await mountPanel();

    await w.find(`[data-testid="snippet-run-host-${HOST_A.id}"]`).setValue(true);
    await w.find('[data-testid="snippet-run-start"]').trigger("click");
    await flushPromises();

    // A panel closed while run A was still emitting, then reopened to start
    // run B, buffers A's stragglers during B's await window. The run_id
    // filter on replay is the only thing standing between them and B's rows,
    // so a terminal event from A must not resolve B's host.
    events.emit("snippet:run:progress", {
      run_id: "run-OLD",
      result: {
        host_id: HOST_A.id,
        host_label: "alpha",
        state: "ok",
        exit_code: 0,
        output: "stale output from the previous run",
        truncated: false,
      },
    } satisfies SnippetRunProgress);
    await flushPromises();

    resolveRun("run-1");
    await flushPromises();

    const row = w.find(`[data-testid="snippet-run-row-${HOST_A.id}"]`);
    expect(row.exists()).toBe(true);
    expect(row.attributes("data-state")).toBe("pending");
    expect(row.text()).not.toContain("stale output from the previous run");
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
    expect(row.text()).toContain("boom");
    // Must read as "failed" (ran, exited non-zero), never as "error" (never ran).
    expect(row.text().toLowerCase()).not.toContain("error");
    // F6: the exit code must appear in exactly one place (the dedicated
    // .row-exitcode span), not composed into the state label as well.
    const exitcode = row.find(`[data-testid="snippet-run-exitcode-${HOST_A.id}"]`);
    expect(exitcode.exists()).toBe(true);
    expect(exitcode.text()).toBe("Exit code 7");
    expect(w.find(`[data-testid="snippet-run-state-${HOST_A.id}"]`).text()).toBe("Failed");
    const occurrences = row.text().split("7").length - 1;
    expect(occurrences).toBe(1);
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

  // MAJOR F3: Go's message is rendered verbatim everywhere, including a
  // host-key rejection whose English happens to contain "not trusted yet" —
  // there is no substring-matched localized substitute any more.
  it("renders Go's host-key-untrusted message verbatim, not a localized substitute", async () => {
    const { w, events } = await mountPanel();
    await selectAndRun(w, [HOST_A.id]);

    const rawMessage =
      'host key for 10.0.0.1:22 is not trusted yet (fingerprint SHA256:abc); open a terminal to this host once and accept the fingerprint, then run the snippet again';
    events.emit("snippet:run:progress", {
      run_id: "run-1",
      result: {
        host_id: HOST_A.id,
        host_label: "alpha",
        state: "error",
        exit_code: 0,
        output: "",
        truncated: false,
        error: rawMessage,
      },
    } satisfies SnippetRunProgress);
    await flushPromises();

    const row = w.find(`[data-testid="snippet-run-row-${HOST_A.id}"]`);
    expect(row.text()).toContain(rawMessage);
  });

  // MINOR F7: Go deliberately preserves partial output from before a
  // connection drop even on a run that never finished (state "error") — the
  // panel must not discard it.
  it("shows preserved partial output on an error row when Go kept some", async () => {
    const { w, events } = await mountPanel();
    await selectAndRun(w, [HOST_A.id]);

    events.emit("snippet:run:progress", {
      run_id: "run-1",
      result: {
        host_id: HOST_A.id,
        host_label: "alpha",
        state: "error",
        exit_code: 0,
        output: "partial output before drop",
        truncated: false,
        error: "connection reset by peer",
      },
    } satisfies SnippetRunProgress);
    await flushPromises();

    const output = w.find(`[data-testid="snippet-run-output-${HOST_A.id}"]`);
    expect(output.exists()).toBe(true);
    expect(output.text()).toContain("partial output before drop");
  });

  it("does not render an output block for an error row with no output", async () => {
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

    expect(w.find(`[data-testid="snippet-run-output-${HOST_A.id}"]`).exists()).toBe(false);
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

  it("retries only hosts whose terminal state failed or errored", async () => {
    runSnippetOnHosts
      .mockReset()
      .mockResolvedValueOnce("run-1")
      .mockResolvedValueOnce("run-2");
    const { w, events } = await mountPanel();
    await selectAndRun(w, [HOST_A.id, HOST_B.id, HOST_C.id]);

    events.emit("snippet:run:progress", {
      run_id: "run-1",
      result: { host_id: HOST_A.id, host_label: "alpha", state: "failed", exit_code: 1, output: "bad", truncated: false },
    } satisfies SnippetRunProgress);
    events.emit("snippet:run:progress", {
      run_id: "run-1",
      result: { host_id: HOST_B.id, host_label: "beta", state: "error", exit_code: 0, output: "", truncated: false, error: "offline" },
    } satisfies SnippetRunProgress);
    events.emit("snippet:run:progress", {
      run_id: "run-1",
      result: { host_id: HOST_C.id, host_label: "gamma", state: "ok", exit_code: 0, output: "done", truncated: false },
    } satisfies SnippetRunProgress);
    await flushPromises();

    await w.find('[data-testid="snippet-run-retry"]').trigger("click");
    await flushPromises();

    expect(runSnippetOnHosts).toHaveBeenNthCalledWith(2, SNIPPET_LABEL, SNIPPET_TEXT, [HOST_A.id, HOST_B.id]);
    expect(w.find(`[data-testid="snippet-run-row-${HOST_C.id}"]`).exists()).toBe(false);
  });

  it("copy-all concatenates every host's output under a '=== {host_label} ===' header", async () => {
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
      // MINOR F5: pin the exact per-host header format, not just relative
      // ordering — a bare "${label}" with no "=== ... ===" must fail this.
      expect(text).toBe("=== alpha ===\noutput-a\n\n=== beta ===\noutput-b");
    } finally {
      Object.defineProperty(navigator, "clipboard", { configurable: true, value: original });
    }
  });

  it("labels an unaliased host the same way Go does, so the label does not flip mid-run", async () => {
    listSSHHosts.mockReset().mockResolvedValue([HOST_NOALIAS]);
    const { w, events } = await mountPanel();
    await selectAndRun(w, [HOST_NOALIAS.id]);

    // Pending row: the panel invented this label locally.
    const row = w.find(`[data-testid="snippet-run-row-${HOST_NOALIAS.id}"]`);
    expect(row.text()).toContain("10.0.0.4");
    expect(row.text()).not.toContain("root@10.0.0.4");

    // Go's sshHostLabel is alias-or-host, and its value overwrites ours the
    // moment the first event lands. If the two rules disagree the row reads
    // one thing while pending and another a moment later — including in the
    // copy-all header.
    const before = row.text();
    events.emit("snippet:run:progress", {
      run_id: "run-1",
      result: { host_id: HOST_NOALIAS.id, host_label: "10.0.0.4", state: "running", exit_code: 0, output: "", truncated: false },
    } satisfies SnippetRunProgress);
    await flushPromises();
    const after = w.find(`[data-testid="snippet-run-row-${HOST_NOALIAS.id}"]`).text();
    expect(after.replace("Running", "").trim()).toContain("10.0.0.4");
    expect(before).toContain("10.0.0.4");
  });

  it("copy-all keeps the partial output an 'error' host printed before it dropped", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    const original = navigator.clipboard;
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    try {
      const { w, events } = await mountPanel();
      await selectAndRun(w, [HOST_A.id]);

      // Go deliberately preserves whatever the host printed before the
      // connection dropped (snippet_run.go: "worth keeping"). Copy-all is
      // where a user takes that away to diagnose with, so dropping it here
      // discards the most useful half of an "error" row.
      events.emit("snippet:run:progress", {
        run_id: "run-1",
        result: {
          host_id: HOST_A.id,
          host_label: "alpha",
          state: "error",
          exit_code: 0,
          output: "partial-before-drop",
          truncated: false,
          error: "run: dropped mid-command",
        },
      } satisfies SnippetRunProgress);
      await flushPromises();

      await w.find('[data-testid="snippet-run-copyall"]').trigger("click");
      await flushPromises();

      expect(writeText.mock.calls[0][0]).toBe(
        "=== alpha ===\nrun: dropped mid-command\npartial-before-drop",
      );
    } finally {
      Object.defineProperty(navigator, "clipboard", { configurable: true, value: original });
    }
  });

  // MAJOR F4: this panel is a repeatedly-opened modal (SettingsTemplates.vue
  // mounts/unmounts it with v-if per run). A leaked "snippet:run:progress"
  // listener writing into a destroyed instance is exactly the bug worth
  // pinning, and nothing else in this file would catch its absence.
  it("unsubscribes the snippet:run:progress listener on unmount", async () => {
    const events = fakeEventBus();
    const offSpy = vi.fn();
    const realOn = events.on.bind(events);
    events.on = (event, handler) => {
      const off = realOn(event, handler);
      return () => {
        offSpy();
        off();
      };
    };

    const { w } = await mountPanel(events);
    expect(offSpy).not.toHaveBeenCalled();

    w.unmount();

    expect(offSpy).toHaveBeenCalledTimes(1);
  });
});
