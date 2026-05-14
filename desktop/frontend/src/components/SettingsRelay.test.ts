import { describe, expect, test } from "vitest";
import source from "./SettingsRelay.vue?raw";

describe("SettingsRelay", () => {
  test("loads relay config and exposes save/disconnect through defineExpose", () => {
    expect(source).toContain("getRelayConfig");
    expect(source).toContain("setRelayConfig");
    expect(source).toContain("defineExpose");
    expect(source).toContain("save,");
    expect(source).toContain("disconnect,");
  });

  test("renders url, token, permissions, insecure toggle, and status pill", () => {
    expect(source).toContain('placeholder="wss://relay.example.com"');
    expect(source).toContain('type="password"');
    expect(source).toContain("remote session permissions");
    expect(source).toContain("enable insecure mode");
    expect(source).toContain("uplink running");
    expect(source).toContain("uplink stopped");
  });

  test("emits dirty whenever a field diverges from the persisted snapshot", () => {
    expect(source).toMatch(/\(e:\s*"dirty",\s*value:\s*boolean\)\s*:\s*void/);
    expect(source).toContain('emit("dirty"');
  });

  test("emits relay-config-changed after a successful save", () => {
    expect(source).toMatch(/\(e:\s*"relay-config-changed"\)\s*:\s*void/);
    expect(source).toContain('emit("relay-config-changed")');
  });

  test("shows insecure warning paragraph when ws is allowed", () => {
    expect(source).toContain("ws:// sends the relay token");
  });
});
