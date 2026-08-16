import { describe, expect, it } from "vitest";
import {
  OUTPUT_BUCKET_SECONDS,
  compareSessionsByLatestActivity,
  compareSessionsBySidebarOrder,
  sessionInteractionAt,
  sessionOutputBucket,
  type SessionSortFields,
} from "./sessionSort";

const T = 1_800_000_000; // unix seconds

function ai(over: Partial<SessionSortFields>): SessionSortFields {
  return {
    session_id: "a",
    task_state: "running",
    unread: false,
    started_at: T - 3600,
    command_started_at: T - 1800,
    ...over,
  };
}

describe("sessionInteractionAt", () => {
  it("ignores output entirely — that is the whole point", () => {
    const base = ai({ command_started_at: T - 100, last_output_at: T });
    const streaming = { ...base, last_output_at: T + 5_000 };
    expect(sessionInteractionAt(streaming)).toBe(sessionInteractionAt(base));
  });

  it("takes the latest of the frozen interaction stamps", () => {
    expect(
      sessionInteractionAt({
        started_at: T - 500,
        command_started_at: T - 400,
        command_ended_at: T - 100,
        attention_at: T - 300,
      }),
    ).toBe(T - 100);
  });
});

describe("sessionOutputBucket", () => {
  it("collapses output within the same minute to one value", () => {
    const a = sessionOutputBucket({ last_output_at: T });
    const b = sessionOutputBucket({ last_output_at: T + OUTPUT_BUCKET_SECONDS - 1 });
    expect(a).toBe(b);
  });

  it("separates output a minute apart", () => {
    expect(sessionOutputBucket({ last_output_at: T + OUTPUT_BUCKET_SECONDS }))
      .toBeGreaterThan(sessionOutputBucket({ last_output_at: T }));
  });

  it("treats a missing timestamp as the oldest bucket", () => {
    expect(sessionOutputBucket({})).toBe(0);
  });
});

// The regression this ordering exists for: two AI sessions running at once,
// each emitting output constantly. Under the old key (max including
// last_output_at) whichever spoke last jumped to the top, so the list churned
// several times a second and rows moved out from under the pointer.
describe("two streaming AI sessions", () => {
  const first = ai({ session_id: "aaa", command_started_at: T - 60 });
  const second = ai({ session_id: "bbb", command_started_at: T - 30 });

  it("keeps a stable order however the output stamps leapfrog", () => {
    const order = (outA: number, outB: number) =>
      [{ ...first, last_output_at: outA }, { ...second, last_output_at: outB }]
        .sort(compareSessionsBySidebarOrder)
        .map((s) => s.session_id);

    const baseline = order(T, T + 1);
    // Same second, alternating leads, and a full 30s of one side shouting:
    // the more recently *started* session stays on top throughout.
    expect(order(T + 1, T)).toEqual(baseline);
    expect(order(T + 2, T + 3)).toEqual(baseline);
    expect(order(T + 30, T + 1)).toEqual(baseline);
    expect(baseline).toEqual(["bbb", "aaa"]);
  });

  it("holds still under the widget's ordering too", () => {
    const order = (outA: number, outB: number) =>
      [{ ...first, last_output_at: outA }, { ...second, last_output_at: outB }]
        .sort(compareSessionsByLatestActivity)
        .map((s) => s.session_id);
    expect(order(T + 1, T)).toEqual(order(T, T + 1));
  });
});

describe("compareSessionsBySidebarOrder", () => {
  // Urgency beats every timestamp, and running leads it: the list answers
  // "what is moving" before "what is asking for me".
  it("still ranks state urgency above everything", () => {
    const waiting = ai({ session_id: "w", task_state: "waiting_input", command_started_at: T });
    const running = ai({ session_id: "r", task_state: "running", command_started_at: T - 9999 });
    expect([waiting, running].sort(compareSessionsBySidebarOrder)[0]).toBe(running);
  });

  it("still puts unread ahead of read within a state", () => {
    const read = ai({ session_id: "r", command_started_at: T });
    const unread = ai({ session_id: "u", unread: true, command_started_at: T - 9999 });
    expect([read, unread].sort(compareSessionsBySidebarOrder)[0]).toBe(unread);
  });

  // A plain shell with no shell integration carries no interaction stamps past
  // started_at, so the coarse output bucket is the only thing that can float it
  // up. It may only do so once a minute.
  it("falls back to the output bucket when interaction stamps tie", () => {
    const quiet = { session_id: "q", task_state: "idle", started_at: T, last_output_at: T };
    const busy = {
      session_id: "b",
      task_state: "idle",
      started_at: T,
      last_output_at: T + OUTPUT_BUCKET_SECONDS,
    };
    expect([quiet, busy].sort(compareSessionsBySidebarOrder)[0]).toBe(busy);
  });

  it("is a total order — equal sessions fall back to id", () => {
    const x = ai({ session_id: "x" });
    const y = ai({ session_id: "y" });
    expect(compareSessionsBySidebarOrder(x, y)).toBeLessThan(0);
    expect(compareSessionsBySidebarOrder(y, x)).toBeGreaterThan(0);
    expect(compareSessionsBySidebarOrder(x, { ...x })).toBe(0);
  });
});
