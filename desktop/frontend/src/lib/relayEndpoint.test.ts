import { describe, expect, test } from "vitest";
import { buildRelayWebSocketEndpoint } from "./relayEndpoint";

describe("buildRelayWebSocketEndpoint", () => {
  test.each([
    ["https://relay.example", "wss://relay.example"],
    ["wss://relay.example", "wss://relay.example"],
    ["http://127.0.0.1:8080", "ws://127.0.0.1:8080"],
    ["ws://127.0.0.1:8080", "ws://127.0.0.1:8080"],
  ])("maps %s without weakening its transport", (base, expected) => {
    expect(buildRelayWebSocketEndpoint(base, "token")).toEqual({
      url: expected,
      session_token: "token",
    });
  });

  test("rejects missing credentials and unsupported schemes", () => {
    expect(buildRelayWebSocketEndpoint("https://relay.example", "")).toBeNull();
    expect(buildRelayWebSocketEndpoint("file:///tmp/relay", "token")).toBeNull();
    expect(buildRelayWebSocketEndpoint("not a URL", "token")).toBeNull();
  });
});
