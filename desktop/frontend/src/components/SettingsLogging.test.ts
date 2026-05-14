import { describe, expect, test } from "vitest";
import source from "./SettingsLogging.vue?raw";

describe("SettingsLogging", () => {
  test("emits open-log-viewer", () => {
    expect(source).toMatch(/\(e:\s*"open-log-viewer"\)\s*:\s*void/);
  });

  test("loads logging config and exposes path helpers", () => {
    expect(source).toContain("getLoggingConfig");
    expect(source).toContain("setLoggingConfig");
    expect(source).toContain("pickLogFilePath");
  });

  test("renders logging toggle, current path, and action buttons", () => {
    expect(source).toContain("write logs to file");
    expect(source).toContain("change location");
    expect(source).toContain("reset default");
    expect(source).toContain("view logs");
  });

  test('view-logs button emits open-log-viewer', () => {
    expect(source).toContain('@click="$emit(\'open-log-viewer\')"');
  });
});
