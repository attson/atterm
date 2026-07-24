import { describe, expect, test } from "vitest";
import type { RemoteSession } from "../platform/types";
import { matchesSession } from "./sessionMatch";

function mk(overrides: Partial<RemoteSession> = {}): RemoteSession {
  return {
    session_id: "s1",
    host_id: "h1",
    title: "",
    cwd: "",
    command: "",
    cols: 80,
    rows: 24,
    started_at: 0,
    ...overrides,
  } as RemoteSession;
}

describe("matchesSession", () => {
  test("empty query matches every session", () => {
    expect(matchesSession(mk({ title: "anything" }), "")).toBe(true);
    expect(matchesSession(mk({ title: "" }), "")).toBe(true);
  });

  test("matches title (case-insensitive substring)", () => {
    const s = mk({ title: "Feishu Gateway" });
    expect(matchesSession(s, "feishu")).toBe(true);
    expect(matchesSession(s, "gateway")).toBe(true);
    expect(matchesSession(s, "shu ga")).toBe(true);
    expect(matchesSession(s, "nope")).toBe(false);
  });

  test("matches cwd", () => {
    const s = mk({ title: "shell", cwd: "/Users/attson/proj/web" });
    expect(matchesSession(s, "proj")).toBe(true);
    expect(matchesSession(s, "/web")).toBe(true);
    expect(matchesSession(s, "attson")).toBe(true);
  });

  test("matches current_command", () => {
    const s = mk({ title: "shell", current_command: "npm run build" });
    expect(matchesSession(s, "npm")).toBe(true);
    expect(matchesSession(s, "run bui")).toBe(true);
  });

  test("matches CJK", () => {
    const s = mk({ title: "支付网关" });
    expect(matchesSession(s, "支付")).toBe(true);
    expect(matchesSession(s, "网关")).toBe(true);
    expect(matchesSession(s, "别的")).toBe(false);
  });

  test("null / undefined fields never contribute", () => {
    const s = mk({ title: "", cwd: undefined, current_command: undefined });
    expect(matchesSession(s, "x")).toBe(false);
  });

  test("query with internal whitespace is treated literally", () => {
    const s = mk({ title: "proj web" });
    expect(matchesSession(s, "proj web")).toBe(true);
    // Not a multi-token AND: "web proj" is not a substring of "proj web".
    expect(matchesSession(s, "web proj")).toBe(false);
  });

  test("caller is responsible for trim + lowercase (contract test)", () => {
    // matchesSession does NOT re-lowercase q; this documents the contract.
    const s = mk({ title: "Feishu Gateway" });
    // Callers pass q pre-lowercased. If they forget, uppercase won't match.
    expect(matchesSession(s, "FEISHU")).toBe(false);
    expect(matchesSession(s, "feishu")).toBe(true);
  });
});
