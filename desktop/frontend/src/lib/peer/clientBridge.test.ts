import { describe, expect, test } from "vitest";
import { normalizePeerCatalogSession } from "./clientBridge";

describe("normalizePeerCatalogSession", () => {
  test("preserves saved SSH alias and appearance from encrypted Peer catalog", () => {
    const session = normalizePeerCatalogSession({
      id: "session-1",
      host_id: "host-1",
      host: "prod.example.com",
      user: "deploy",
      cols: 80,
      rows: 24,
      ssh_host_alias: "production",
      ssh_appearance: { theme_id: "nord", font_head: "", font_size: 17 },
      permission: 3,
    });

    expect(session).toMatchObject({
      session_id: "session-1",
      ssh_host_alias: "production",
      ssh_appearance: { theme_id: "nord", font_head: "", font_size: 17 },
      peer_direct: true,
    });
  });

  test("drops malformed appearance members without inventing values", () => {
    const session = normalizePeerCatalogSession({
      id: "session-1", cols: 80, rows: 24,
      ssh_appearance: { theme_id: 7, font_head: null, font_size: "17" },
    });
    expect(session?.ssh_appearance).toBeUndefined();
  });
});
