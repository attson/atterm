import { describe, expect, test } from "vitest";
import source from "./connection.ts?raw";

describe("SessionConnection driver state", () => {
  test("generates and stores a clientID per instance", () => {
    expect(source).toMatch(/clientID\s*:\s*string/);
    expect(source).toMatch(/crypto\.randomUUID\(\)/);
  });

  test("includes client_id in ATTACH payload", () => {
    expect(source).toMatch(/client_id\s*:\s*this\.clientID/);
  });

  test("parses driver_client_id from META and surfaces onDriverChange", () => {
    expect(source).toMatch(/driver_client_id/);
    expect(source).toMatch(/onDriverChange/);
  });

  test("exposes claimDriver() that sends a CLAIM_DRIVER frame", () => {
    expect(source).toMatch(/claimDriver\s*\(/);
    expect(source).toMatch(/TYPE\.CLAIM_DRIVER/);
  });
});

describe("SessionConnection client_name", () => {
  test("accepts a clientName option and sends it in ATTACH", () => {
    expect(source).toMatch(/options:\s*SessionConnectionOptions/);
    expect(source).toMatch(/client_name\s*:\s*this\.clientName/);
  });

  test("includes client_name in CLAIM_DRIVER payload", () => {
    expect(source).toMatch(/client_id:\s*this\.clientID,\s*client_name:\s*this\.clientName/);
  });

  test("parses driver_client_name from META and passes to onDriverChange", () => {
    expect(source).toMatch(/driver_client_name/);
    expect(source).toMatch(/newDriverName/);
  });
});

describe("openWS sync-throw isolation", () => {
  // Regression: WebKit throws "The string did not match the expected pattern."
  // synchronously from new WebSocket(invalid). That used to unwind App.vue's
  // boot try/catch and freeze startup. Both openWS impls must trap it and
  // route through handleOpenFailure -> onStatus("error") + backoff retry.
  test("SessionListConnection.openWS wraps new WebSocket in try/catch", () => {
    const m = source.match(/class SessionListConnection[\s\S]*?(?=\n}\n)/);
    expect(m).not.toBeNull();
    expect(m![0]).toMatch(/try\s*{\s*ws\s*=\s*auth\.protocols\s*\?[\s\S]*?}\s*catch[\s\S]*?handleOpenFailure/);
  });

  test("SessionConnection.openWS wraps new WebSocket in try/catch", () => {
    const m = source.match(/class SessionConnection[\s\S]*?(?=\n}\n)/);
    expect(m).not.toBeNull();
    expect(m![0]).toMatch(/try\s*{\s*ws\s*=\s*auth\.protocols\s*\?[\s\S]*?}\s*catch[\s\S]*?handleOpenFailure/);
  });

  test("handleOpenFailure surfaces error status and schedules reconnect", () => {
    expect(source).toMatch(/handleOpenFailure\([\s\S]*?\)\s*:\s*void/);
    expect(source).toMatch(/onStatus\?\.\("error"\)/);
    expect(source).toMatch(/setTimeout\(\(\) => this\.openWS\(\), delay\)/);
  });
});
