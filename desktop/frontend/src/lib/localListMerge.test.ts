import { describe, it, expect } from "vitest";
import { mergeLocalSessions } from "./localListMerge";
import type { SessionInfo } from "./connection";

function info(id: string, cwd = "/u"): SessionInfo {
  return {
    id,
    command: "zsh",
    cwd,
    title: id,
    cols: 80,
    rows: 24,
    started_at: 0,
    host_id: "h",
  };
}

describe("mergeLocalSessions", () => {
  it("empty pending: latest push replaces current local wholesale", () => {
    const r = mergeLocalSessions([info("A")], [info("X")], new Set());
    expect(r.localList.map((s) => s.id)).toEqual(["A"]);
    expect([...r.pending]).toEqual([]);
  });

  it("pending sid present in the new push: acks it and drops from pending", () => {
    const r = mergeLocalSessions(
      [info("A"), info("B")],
      [info("A"), info("B")],
      new Set(["B"]),
    );
    // B was the seed; Go just confirmed it. localList = Go's truth verbatim.
    expect(r.localList.map((s) => s.id).sort()).toEqual(["A", "B"]);
    expect([...r.pending]).toEqual([]);
  });

  it("pending sid missing from the new push: preserves the seeded info, keeps pending", () => {
    // The race that broke recovery: Go pushed a stale [A] frame before its
    // broadcasts for B/C landed. Without merging, applyLocalSessions would
    // overwrite localList to [A] and sweep would null tabs holding B/C.
    const r = mergeLocalSessions(
      [info("A")],
      [info("A"), info("B"), info("C")],
      new Set(["B", "C"]),
    );
    expect(r.localList.map((s) => s.id).sort()).toEqual(["A", "B", "C"]);
    expect([...r.pending].sort()).toEqual(["B", "C"]);
  });

  it("partial ack: one pending confirmed, the other preserved", () => {
    const r = mergeLocalSessions(
      [info("A"), info("B")],
      [info("A"), info("B"), info("C")],
      new Set(["B", "C"]),
    );
    expect(r.localList.map((s) => s.id).sort()).toEqual(["A", "B", "C"]);
    expect([...r.pending]).toEqual(["C"]);
  });

  it("never duplicates an id when both arrived and currentLocal hold it", () => {
    // pending sid B is in arrived AND in currentLocal — must end up exactly
    // once. Otherwise downstream `localList.find(...)` collisions / sweep
    // double-iteration would surface as new bugs.
    const r = mergeLocalSessions(
      [info("B", "/from-go")],
      [info("B", "/seeded")],
      new Set(["B"]),
    );
    expect(r.localList.length).toBe(1);
    // Go's truth wins for fields when both exist.
    expect(r.localList[0].cwd).toBe("/from-go");
  });

  it("preserved entry keeps the seeded fields (Go has no opinion yet)", () => {
    const r = mergeLocalSessions(
      [info("A")],
      [info("A"), info("B", "/seeded-cwd")],
      new Set(["B"]),
    );
    const b = r.localList.find((s) => s.id === "B");
    expect(b?.cwd).toBe("/seeded-cwd");
  });

  it("pending entry no longer in currentLocal: drops from pending and from output (defensive)", () => {
    // If a pane was closed and its sessionId was wiped from currentLocal
    // before the next push, there is no info to preserve — the pending entry
    // must not resurrect a phantom. It just falls out of pending.
    const r = mergeLocalSessions(
      [info("A")],
      [info("A")],
      new Set(["B"]),
    );
    expect(r.localList.map((s) => s.id)).toEqual(["A"]);
    expect([...r.pending]).toEqual([]);
  });
});
