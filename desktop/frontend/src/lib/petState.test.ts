import { describe, it, expect } from "vitest";
import type { SessionInfo } from "./connection";
import {
  PET_MAX_ROWS,
  moodOf,
  projectPetState,
  subtitleOf,
  type PetMood,
} from "./petState";

const NOW = 1_800_000_000_000; // fixed clock so ageMs assertions are stable

function sess(over: Partial<SessionInfo> & { id: string }): SessionInfo {
  return {
    command: "zsh",
    cwd: "/Users/me/proj",
    title: "",
    cols: 80,
    rows: 24,
    started_at: NOW / 1000 - 600,
    ...over,
  } as SessionInfo;
}

describe("moodOf", () => {
  it("maps the four actionable task states", () => {
    expect(moodOf("waiting_input")).toBe("waiting");
    expect(moodOf("failed")).toBe("failed");
    expect(moodOf("running")).toBe("running");
    expect(moodOf("idle")).toBe("idle");
  });

  it("folds disconnected and closed into idle, not failed", () => {
    // A dropped relay is not a failed command — painting the pet red on every
    // network hiccup would train the user to ignore red.
    expect(moodOf("disconnected")).toBe("idle");
    expect(moodOf("closed")).toBe("idle");
    expect(moodOf(undefined)).toBe("idle");
  });
});

describe("subtitleOf", () => {
  const HOME = "/Users/me";

  it("elides a deep path to its last two segments, like the sidebar", () => {
    const s = sess({ id: "a", cwd: "/Users/me/code/github.com/attson/atterm" });
    expect(subtitleOf(s, HOME, "atterm")).toBe("…/attson/atterm");
  });

  it("keeps a shallow path under home intact", () => {
    const s = sess({ id: "a", cwd: "/Users/me/code/atterm" });
    expect(subtitleOf(s, HOME, "atterm")).toBe("~/code/atterm");
  });

  it("renders a short path under home verbatim with a tilde", () => {
    const s = sess({ id: "a", cwd: "/Users/me/proj" });
    expect(subtitleOf(s, HOME, "proj")).toBe("~/proj");
  });

  it("is empty when the path would only repeat the title", () => {
    // A shell whose OSC title is already its directory would otherwise render
    // the same word on both lines.
    const s = sess({ id: "a", cwd: "/Users/me" });
    expect(subtitleOf(s, HOME, "~")).toBe("");
  });

  it("is empty when the session has no cwd", () => {
    expect(subtitleOf(sess({ id: "a", cwd: "" }), HOME, "zsh")).toBe("");
  });

  it("falls back to the absolute path when home is unknown", () => {
    const s = sess({ id: "a", cwd: "/opt/thing" });
    expect(subtitleOf(s, "", "thing")).toBe("/opt/thing");
  });

  it("does not show the command — a truncated command line is noise here", () => {
    const s = sess({
      id: "a",
      cwd: "/Users/me/proj",
      current_command: "claude --permission-mode bypassPermissions",
    });
    expect(subtitleOf(s, HOME, "Claude Code")).toBe("~/proj");
  });
});

describe("projectPetState — aggregate mood", () => {
  const cases: [string, SessionInfo["task_state"][], PetMood][] = [
    ["waiting outranks everything", ["running", "failed", "waiting_input"], "waiting"],
    ["failed outranks running", ["running", "completed", "failed"], "failed"],
    ["running outranks idle", ["idle", "running"], "running"],
    ["all quiet is idle", ["completed", "idle"], "idle"],
  ];

  for (const [name, states, want] of cases) {
    it(name, () => {
      const sessions = states.map((st, i) => sess({ id: `s${i}`, task_state: st }));
      expect(projectPetState(sessions, { nowMs: NOW }).mood).toBe(want);
    });
  }

  it("is idle for an empty list", () => {
    const st = projectPetState([], { nowMs: NOW });
    expect(st.mood).toBe("idle");
    expect(st.rows).toEqual([]);
    expect(st.headline).toBe("没有会话");
  });
});

describe("projectPetState — ordering", () => {
  it("sorts by priority band, then most-recently-active first", () => {
    const sessions = [
      sess({ id: "old-run", task_state: "running", last_output_at: 100 }),
      sess({ id: "done", task_state: "completed", last_output_at: 900 }),
      sess({ id: "new-run", task_state: "running", last_output_at: 500 }),
      sess({ id: "fail", task_state: "failed", last_output_at: 200 }),
      sess({ id: "wait", task_state: "waiting_input", last_output_at: 1 }),
    ];
    const ids = projectPetState(sessions, { nowMs: NOW }).rows.map((r) => r.sessionId);
    expect(ids).toEqual(["wait", "fail", "new-run", "old-run", "done"]);
  });

  it("drops closed sessions entirely", () => {
    const sessions = [
      sess({ id: "gone", task_state: "closed" }),
      sess({ id: "here", task_state: "running" }),
    ];
    const st = projectPetState(sessions, { nowMs: NOW });
    expect(st.rows.map((r) => r.sessionId)).toEqual(["here"]);
  });
});

describe("projectPetState — truncation", () => {
  it("caps rows at maxRows and reports the overflow", () => {
    const sessions = Array.from({ length: PET_MAX_ROWS + 3 }, (_, i) =>
      sess({ id: `s${i}`, task_state: "running" }),
    );
    const st = projectPetState(sessions, { nowMs: NOW });
    expect(st.rows).toHaveLength(PET_MAX_ROWS);
    expect(st.overflowCount).toBe(3);
    // Counts describe every live session, not just the visible slice.
    expect(st.runningCount).toBe(PET_MAX_ROWS + 3);
  });

  it("reports no overflow when everything fits", () => {
    const st = projectPetState([sess({ id: "a", task_state: "running" })], { nowMs: NOW });
    expect(st.overflowCount).toBe(0);
  });
});

describe("projectPetState — headline and subline", () => {
  it("leads with waiting and never repeats it in the subline", () => {
    const sessions = [
      sess({ id: "w", task_state: "waiting_input" }),
      sess({ id: "r1", task_state: "running" }),
      sess({ id: "r2", task_state: "running" }),
      sess({ id: "c", task_state: "completed" }),
    ];
    const st = projectPetState(sessions, { nowMs: NOW });
    expect(st.headline).toBe("1 个等你输入");
    expect(st.subline).toBe("2 个在跑 · 1 个已完成");
  });

  it("leads with failed when nothing is waiting", () => {
    const sessions = [
      sess({ id: "f", task_state: "failed" }),
      sess({ id: "r", task_state: "running" }),
    ];
    const st = projectPetState(sessions, { nowMs: NOW });
    expect(st.headline).toBe("1 个失败");
    expect(st.subline).toBe("1 个在跑");
  });

  it("says everything finished when only completed sessions remain", () => {
    const st = projectPetState([sess({ id: "c", task_state: "completed" })], { nowMs: NOW });
    expect(st.headline).toBe("都跑完了");
    expect(st.subline).toBe("1 个已完成");
  });
});

describe("projectPetState — row fields", () => {
  it("marks sessions on another host as remote", () => {
    const sessions = [
      sess({ id: "local", host_id: "H1", host: "mbp", task_state: "running" }),
      sess({ id: "far", host_id: "H2", host: "mac-mini", task_state: "running" }),
    ];
    const st = projectPetState(sessions, { nowMs: NOW, localHostId: "H1" });
    const byId = Object.fromEntries(st.rows.map((r) => [r.sessionId, r]));
    expect(byId.local.remoteHost).toBe("");
    expect(byId.far.remoteHost).toBe("mac-mini");
  });

  it("treats every session as local when the host id is unknown", () => {
    // Boot races can leave localHostId empty; guessing "remote" would tag the
    // user's own sessions with another machine's name.
    const sessions = [sess({ id: "a", host_id: "H2", host: "mac-mini" })];
    const st = projectPetState(sessions, { nowMs: NOW, localHostId: "" });
    expect(st.rows[0].remoteHost).toBe("");
  });

  it("exposes the AI kind only for AI-classified sessions", () => {
    const sessions = [
      sess({ id: "ai", type: "ai", current_command: "/usr/local/bin/claude --resume x" }),
      sess({ id: "sh", type: "shell", current_command: "npm run build" }),
    ];
    const st = projectPetState(sessions, { nowMs: NOW });
    const byId = Object.fromEntries(st.rows.map((r) => [r.sessionId, r]));
    expect(byId.ai.kind).toBe("claude");
    expect(byId.sh.kind).toBe("");
  });

  it("computes age from command_started_at for live commands only", () => {
    const sessions = [
      sess({ id: "run", task_state: "running", command_started_at: NOW / 1000 - 30 }),
      sess({ id: "done", task_state: "completed", command_started_at: NOW / 1000 - 30 }),
    ];
    const st = projectPetState(sessions, { nowMs: NOW });
    const byId = Object.fromEntries(st.rows.map((r) => [r.sessionId, r]));
    expect(byId.run.ageMs).toBe(30_000);
    expect(byId.done.ageMs).toBe(0);
  });

  it("clamps a future command_started_at to zero instead of going negative", () => {
    const sessions = [
      sess({ id: "skew", task_state: "running", command_started_at: NOW / 1000 + 60 }),
    ];
    const st = projectPetState(sessions, { nowMs: NOW });
    expect(st.rows[0].ageMs).toBe(0);
  });

  it("names an idle shell by its cwd, not by a hex session id", () => {
    // titleOrCommand() bottoms out at session_id.slice(0, 8), which is useless
    // in a one-line row — displayTitle() must reach the cwd basename first.
    const sessions = [sess({ id: "abcdef1234", title: "", cwd: "/Users/me/atterm", command: "zsh" })];
    const st = projectPetState(sessions, { nowMs: NOW });
    expect(st.rows[0].title).toBe("atterm");
  });

  it("titles a row by its command and subtitles it with the path", () => {
    const sessions = [
      sess({ id: "a", title: "", cwd: "/Users/me/atterm", current_command: "npm run build" }),
    ];
    const row = projectPetState(sessions, { nowMs: NOW, home: "/Users/me" }).rows[0];
    expect(row.title).toBe("npm");
    expect(row.subtitle).toBe("~/atterm");
  });

  it("falls back to the launch command when there is no cwd either", () => {
    const sessions = [sess({ id: "abcdef1234", title: "", cwd: "", command: "/bin/zsh" })];
    expect(projectPetState(sessions, { nowMs: NOW }).rows[0].title).toBe("zsh");
  });
});

describe("projectPetState — idle sessions are still sessions", () => {
  it("counts shells sitting at a prompt instead of claiming there are none", () => {
    // Regression: idle was not a band, so a window listing ten live sessions
    // announced "没有会话" while rendering all ten below it.
    const sessions = Array.from({ length: 10 }, (_, i) =>
      sess({ id: `s${i}`, task_state: "idle" }),
    );
    const st = projectPetState(sessions, { nowMs: NOW });
    expect(st.idleCount).toBe(10);
    expect(st.headline).toBe("10 个空闲");
    expect(st.subline).toBe("");
  });

  it("still reports no sessions when there really are none", () => {
    const st = projectPetState([], { nowMs: NOW });
    expect(st.idleCount).toBe(0);
    expect(st.headline).toBe("没有会话");
  });

  it("does not count a completed session in both the completed and idle bands", () => {
    const sessions = [
      sess({ id: "done", task_state: "completed" }),
      sess({ id: "resting", task_state: "idle" }),
    ];
    const st = projectPetState(sessions, { nowMs: NOW });
    expect(st.completedCount).toBe(1);
    expect(st.idleCount).toBe(1);
    expect(st.headline).toBe("1 个已完成");
    expect(st.subline).toBe("1 个空闲");
  });

  it("counts a disconnected session as idle rather than failed", () => {
    const st = projectPetState([sess({ id: "gone", task_state: "disconnected" })], {
      nowMs: NOW,
    });
    expect(st.idleCount).toBe(1);
    expect(st.failedCount).toBe(0);
  });

  it("keeps idle last so the actionable bands lead", () => {
    const sessions = [
      sess({ id: "w", task_state: "waiting_input" }),
      sess({ id: "r", task_state: "running" }),
      sess({ id: "i1", task_state: "idle" }),
      sess({ id: "i2", task_state: "idle" }),
    ];
    const st = projectPetState(sessions, { nowMs: NOW });
    expect(st.headline).toBe("1 个等你输入");
    expect(st.subline).toBe("1 个在跑 · 2 个空闲");
  });
});

describe("projectPetState — AI-only filter", () => {
  const mixed = () => [
    sess({ id: "ai1", type: "ai", task_state: "running", current_command: "claude" }),
    sess({ id: "sh1", type: "shell", task_state: "running", current_command: "npm run dev" }),
    sess({ id: "sh2", type: "shell", task_state: "idle" }),
  ];

  it("keeps every session when the filter is off", () => {
    const st = projectPetState(mixed(), { nowMs: NOW });
    expect(st.rows).toHaveLength(3);
    expect(st.aiOnly).toBe(false);
  });

  it("keeps only AI sessions when the filter is on", () => {
    const st = projectPetState(mixed(), { nowMs: NOW, aiOnly: true });
    expect(st.rows.map((r) => r.sessionId)).toEqual(["ai1"]);
    expect(st.aiOnly).toBe(true);
  });

  it("counts and headline describe the filtered set, not the whole list", () => {
    // The filter runs before every count, so the header can never advertise
    // sessions the list does not show.
    const st = projectPetState(mixed(), { nowMs: NOW, aiOnly: true });
    expect(st.runningCount).toBe(1);
    expect(st.idleCount).toBe(0);
    expect(st.headline).toBe("1 个在跑");
  });

  it("says which emptiness it is when the filter hides everything", () => {
    const shells = [sess({ id: "sh", type: "shell", task_state: "idle" })];
    expect(projectPetState(shells, { nowMs: NOW, aiOnly: true }).headline).toBe(
      "没有 AI 会话",
    );
    expect(projectPetState(shells, { nowMs: NOW }).headline).toBe("1 个空闲");
  });

  it("still drops closed sessions while filtering", () => {
    const sessions = [
      sess({ id: "gone", type: "ai", task_state: "closed" }),
      sess({ id: "here", type: "ai", task_state: "running" }),
    ];
    const st = projectPetState(sessions, { nowMs: NOW, aiOnly: true });
    expect(st.rows.map((r) => r.sessionId)).toEqual(["here"]);
  });
});
