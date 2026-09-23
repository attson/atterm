import { describe, expect, test } from "vitest";
import source from "./StartupUpdateDialog.vue?raw";

describe("StartupUpdateDialog", () => {
  test("emits dismiss on Later", () => {
    expect(source).toMatch(/\(e:\s*"dismiss"\)\s*:\s*void/);
    expect(source).toContain('@click="emit(\'dismiss\')"');
    expect(source).toContain("startupUpdate.later");
  });

  test("polls update state and clears the interval on unmount", () => {
    expect(source).toContain("getUpdateState");
    expect(source).toContain("setInterval");
    expect(source).toContain("onBeforeUnmount");
    expect(source).toContain("clearInterval");
  });

  test("wires download, cancel, and install bindings", () => {
    expect(source).toContain("startDownload");
    expect(source).toContain("cancelDownload");
    expect(source).toContain("installUpdate");
  });

  test("notifies App only after the install helper starts successfully", () => {
    expect(source).toMatch(/await installUpdate\(\);\s*[^}]*emit\("install"\)/);
    const catchBody = source.match(/async function onInstall\(\)[\s\S]*?catch\s*\{([\s\S]*?)\n\s*\}/)?.[1] ?? "";
    expect(catchBody).not.toContain('emit("install")');
  });

  test("shows install button only when ready, download button otherwise", () => {
    expect(source).toContain("startupUpdate.installRestart");
    expect(source).toContain("startupUpdate.downloadInstall");
    expect(source).toContain("state?.ready");
    expect(source).toContain("state?.downloading");
  });

  test("renders release notes and current→latest header", () => {
    expect(source).toContain("startupUpdate.title");
    expect(source).toContain("startupUpdate.currentToLatest");
    expect(source).toContain("startupUpdate.releaseNotes");
  });
});
