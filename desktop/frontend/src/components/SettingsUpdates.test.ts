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
    expect(source).toContain("setUpdateGHProxyURL");
    expect(source).toContain("setInterval");
  });

  test("renders auto-check toggle, release notes details, and primary actions", () => {
    expect(source).toContain("settings.updates.autoCheck");
    expect(source).toContain("settings.updates.ghProxy");
    expect(source).toContain("settings.updates.releaseNotes");
    expect(source).toContain("settings.updates.checkNow");
  });

  test("force-install button is labeled force install", () => {
    expect(source).toContain("settings.updates.forceInstallRestart");
  });

  test("clears its poll interval on unmount", () => {
    expect(source).toContain("onBeforeUnmount");
    expect(source).toContain("clearInterval");
  });

  test("renders a version-line radio selector when multiple lines exist", () => {
    // Radio group only shows when there are >= 2 version lines.
    expect(source).toMatch(/lines\??\.length.*>=\s*2/s);
    expect(source).toContain("hasMultipleLines");
    expect(source).toContain('type="radio"');
    expect(source).toContain('v-for="line in state.lines"');
    expect(source).toContain('v-model="selectedLine"');
    // Each option shows the minor line label and its latest version.
    expect(source).toContain("settings.updates.versionLine");
    expect(source).toContain("line.latest");
  });

  test("downloads the selected line's latest version via downloadVersion", () => {
    expect(source).toContain("downloadVersion");
    expect(source).toContain("onDownloadSelected");
    expect(source).toContain("selectedLatest");
  });

  test("defaults the selected line and tracks it reactively", () => {
    expect(source).toContain("selectedLine");
    expect(source).toContain("watch");
  });
});
