import { describe, expect, it } from "vitest";
import type { SessionInfo } from "./connection";
import { SeenReceiptOverlay } from "./seenReceiptOverlay";

function session(
  id: string,
  attentionAt: number,
  unread = true,
): SessionInfo {
  return {
    id,
    command: "codex",
    cwd: "/work",
    title: id,
    cols: 80,
    rows: 24,
    started_at: 1,
    host_id: "host",
    host: "host",
    user: "user",
    attention_at: attentionAt,
    unread,
  };
}

describe("SeenReceiptOverlay", () => {
  it("keeps a successful read receipt across a stale unread list snapshot", () => {
    const overlay = new SeenReceiptOverlay();
    overlay.confirm([session("s1", 100)], { ids: ["s1"] });

    const reconciled = overlay.apply([session("s1", 100)]);

    expect(reconciled[0].unread).toBe(false);
  });

  it("accepts newer attention and stops suppressing that session", () => {
    const overlay = new SeenReceiptOverlay();
    overlay.confirm([session("s1", 100)], { ids: ["s1"] });

    expect(overlay.apply([session("s1", 101)])[0].unread).toBe(true);
    expect(overlay.apply([session("s1", 100)])[0].unread).toBe(true);
  });

  it("records only targeted sessions and supports mark-all", () => {
    const selected = new SeenReceiptOverlay();
    const snapshot = [session("s1", 100), session("s2", 200)];
    selected.confirm(snapshot, { ids: ["s1"] });

    expect(selected.apply(snapshot).map((item) => item.unread)).toEqual([false, true]);

    const all = new SeenReceiptOverlay();
    all.confirm(snapshot, { all: true });
    expect(all.apply(snapshot).map((item) => item.unread)).toEqual([false, false]);
  });

  it("uses the newest observed attention when a session appears in both lists", () => {
    const overlay = new SeenReceiptOverlay();
    overlay.confirm([session("s1", 100), session("s1", 110)], { ids: ["s1"] });

    expect(overlay.apply([session("s1", 105)])[0].unread).toBe(false);
    expect(overlay.apply([session("s1", 111)])[0].unread).toBe(true);
  });
});
