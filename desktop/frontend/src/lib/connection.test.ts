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
