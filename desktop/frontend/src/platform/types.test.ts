import { describe, expect, test } from "vitest";
import type { RemoteSession } from "./types";

describe("RemoteSession type", () => {
  test("accepts unread and attention_at fields", () => {
    const s: RemoteSession = {
      session_id: "s1",
      host_id: "h1",
      host: "mac",
      user: "you",
      title: "claude",
      cols: 80,
      rows: 24,
      unread: true,
      attention_at: 1700000000,
    };
    expect(s.unread).toBe(true);
    expect(s.attention_at).toBe(1700000000);
  });
});
