import { describe, expect, it } from "vitest";
import { validateRelayBase, relayBaseToWsUrl } from "./relayUrl";

describe("validateRelayBase", () => {
  it("accepts https with host", () => { expect(validateRelayBase("https://r.example.com", false)).toBeNull(); });
  it("rejects empty", () => { expect(validateRelayBase("", false)).toMatch(/required/i); });
  it("rejects malformed", () => { expect(validateRelayBase("not a url", false)).toMatch(/invalid|malformed/i); });
  it("rejects non-http(s) scheme", () => { expect(validateRelayBase("wss://r.example.com", false)).toMatch(/http/i); });
  it("rejects path/query/fragment", () => {
    expect(validateRelayBase("https://r.example.com/x", false)).toMatch(/path|query|fragment/i);
    expect(validateRelayBase("https://r.example.com/?a=1", false)).toMatch(/path|query|fragment/i);
  });
  it("accepts http loopback without insecure flag", () => {
    expect(validateRelayBase("http://localhost:8080", false)).toBeNull();
    expect(validateRelayBase("http://127.0.0.1:8080", false)).toBeNull();
  });
  it("rejects http non-loopback without insecure flag", () => {
    expect(validateRelayBase("http://r.example.com", false)).toMatch(/insecure/i);
  });
  it("accepts http non-loopback with insecure flag", () => {
    expect(validateRelayBase("http://r.example.com", true)).toBeNull();
  });
});

describe("relayBaseToWsUrl", () => {
  it("https -> wss", () => {
    expect(relayBaseToWsUrl("https://r.example.com")).toBe("wss://r.example.com");
    expect(relayBaseToWsUrl("https://r.example.com/")).toBe("wss://r.example.com");
  });
  it("http -> ws, preserves port", () => {
    expect(relayBaseToWsUrl("http://1.2.3.4:8080")).toBe("ws://1.2.3.4:8080");
  });
});
