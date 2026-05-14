import { describe, expect, test } from "vitest";
import source from "./SettingsUpdates.vue?raw";

describe("SettingsUpdates", () => {
  test("emits request-install with the latest version string", () => {
    expect(source).toMatch(/\(e:\s*"request-install",\s*version:\s*string\)\s*:\s*void/);
    expect(source).toContain('$emit(\'request-install\', state.latest)');
  });

  test("polls update state and exposes check/download bindings", () => {
    expect(source).toContain("getUpdateState");
    expect(source).toContain("checkUpdate");
    expect(source).toContain("startDownload");
    expect(source).toContain("setAutoCheckUpdates");
    expect(source).toContain("setInterval");
  });

  test("renders auto-check toggle, release notes details, and primary actions", () => {
    expect(source).toContain("automatically check for updates");
    expect(source).toContain("release notes");
    expect(source).toContain("check now");
  });

  test("force-install button is labeled force install", () => {
    expect(source).toContain("force install");
  });

  test("clears its poll interval on unmount", () => {
    expect(source).toContain("onBeforeUnmount");
    expect(source).toContain("clearInterval");
  });
});
