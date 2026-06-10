import { describe, expect, it } from "vitest";

import { webSocketAuth } from "./connection";

describe("websocket auth", () => {
  it("sends relay tokens via subprotocol instead of the URL query", () => {
    const auth = webSocketAuth({ url: "wss://relay.example.com", session_token: "tok_en-123" }, "/client");

    expect(auth.url).toBe("wss://relay.example.com/client");
    expect(auth.protocols).toEqual(["atterm-token.tok_en-123"]);
  });
});
