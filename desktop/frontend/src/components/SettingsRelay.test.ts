import { describe, expect, test } from "vitest";
import source from "./SettingsRelay.vue?raw";

describe("SettingsRelay", () => {
  test("loads relay config and exposes save through defineExpose", () => {
    expect(source).toContain("getRelayConfig");
    expect(source).toContain("setRelayConfig");
    expect(source).toContain("defineExpose");
    expect(source).toContain("save,");
  });

  test("renders url, token, permissions SelectDropdown, insecure toggle, and status pill", () => {
    expect(source).toContain('placeholder="wss://relay.example.com"');
    expect(source).toContain('type="password"');
    expect(source).toContain("remote session permissions");
    expect(source).toContain("import SelectDropdown");
    expect(source).toContain("<SelectDropdown");
    expect(source).toContain('v-model="remotePermission"');
    expect(source).toContain("enable insecure mode");
    expect(source).toContain("uplink running");
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

  // Task 7.1 new tests
  test("renders API token label, not 'token'", () => {
    expect(source).toContain(">API token<");
    expect(source).not.toContain(">shared bearer token<");
    expect(source).not.toContain(">token<");
  });

  test("renders Uplink ON/OFF toggle bound to RelayPaused", () => {
    // source must have a toggle bound to `paused` ref
    expect(source).toContain("paused");
    // on-change handler calls setUplinkPaused
    expect(source).toContain("setUplinkPaused");
  });

  test("does not have a 'disconnect' button", () => {
    // The disconnect() function and its button must be removed
    expect(source).not.toContain("disconnect()");
    expect(source).not.toContain(">Disconnect<");
    expect(source).not.toContain(">disconnect<");
  });

  test("paste of non-atk token surfaces a warning hint", () => {
    // source must check if token starts with atk_
    expect(source).toContain("atk_");
    // and show a warning when it doesn't
    expect(source).toContain("tokenWarning");
    expect(source).toContain("API token");
  });

  test("Open in browser button calls BrowserOpenURL with relay URL + /settings.html", () => {
    expect(source).toContain("BrowserOpenURL");
    expect(source).toContain("/settings.html");
  });

  // Task 8.1 tests
  test("status row shows user_id_short when AUTH_INFO received but email not yet fetched", () => {
    // Must listen for the relay:auth-info event
    expect(source).toContain('EventsOn("relay:auth-info"');
    // Must show "connected as <short id>" based on a slice of user_id
    expect(source).toContain("connected as");
    expect(source).toContain("connectedUserID");
    // Should slice connectedUserID to short form (8 chars)
    expect(source).toMatch(/connectedUserID.*slice\(0,\s*8\)|slice\(0,\s*8\).*connectedUserID/);
  });

  test("status row shows email once /api/me fetch completes", () => {
    // Must call fetchRelayMe (the api.ts wrapper)
    expect(source).toContain("fetchRelayMe");
    // Must have connectedEmail ref
    expect(source).toContain("connectedEmail");
    // Status text should prefer email when available
    expect(source).toMatch(/connectedEmail.*connected as|connected as.*connectedEmail/);
  });
});
