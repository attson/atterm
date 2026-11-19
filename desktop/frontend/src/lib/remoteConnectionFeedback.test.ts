import { describe, expect, it } from "vitest";
import { resolveRemoteConnectionReason } from "./remoteConnectionFeedback";

describe("remote connection reason", () => {
  const configured = { url: "wss://relay.example", token: "test", connected: true };
  it("distinguishes missing configuration, login, pause, and connection preparation", () => {
    expect(resolveRemoteConnectionReason({ ...configured, url: "" })).toBe("unconfigured");
    expect(resolveRemoteConnectionReason({ ...configured, token: "" })).toBe("signed-out");
    expect(resolveRemoteConnectionReason({ ...configured, connected: false, paused: true })).toBe("paused");
    expect(resolveRemoteConnectionReason({ ...configured, connected: false })).toBe("connecting");
    expect(resolveRemoteConnectionReason(configured)).toBe("proxy-unavailable");
    expect(resolveRemoteConnectionReason({ ...configured, remote_proxy_url: "ws://localhost:1" })).toBe("connecting");
  });
});
