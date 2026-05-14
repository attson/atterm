import { describe, expect, test } from "vitest";
import source from "./App.vue?raw";

describe("tab activation", () => {
  test("gotoTab sets currentTabId before mutating the hash", () => {
    const body = source.match(/function\s+gotoTab\s*\(id:\s*string\)\s*\{([\s\S]*?)\n\}/)?.[1] ?? "";
    const currentIdx = body.indexOf("currentTabId.value = id");
    const hashIdx = body.indexOf("location.hash =");
    expect(currentIdx).toBeGreaterThanOrEqual(0);
    expect(hashIdx).toBeGreaterThanOrEqual(0);
    expect(currentIdx).toBeLessThan(hashIdx);
  });
});

describe("terminal toasts", () => {
  test("wires pane-grid toast events to the existing toast surface", () => {
    expect(source).toContain('@toast="showToast"');
  });
});

describe("quit confirmation", () => {
  test("registers the before-close listener and imports confirmQuit", () => {
    expect(source).toContain('EventsOn("before-close"');
    expect(source).toContain("confirmQuit");
    expect(source).toContain("ConfirmQuitDialog");
  });

  test("renders ConfirmQuitDialog wired to existing session counts", () => {
    expect(source).toContain("<ConfirmQuitDialog");
    expect(source).toContain(':local-count="localSessionCount"');
    expect(source).toContain(':remote-count="remoteSessionCount"');
    expect(source).toContain('@confirm="onConfirmQuit"');
    expect(source).toContain('@cancel="onCancelQuit"');
  });

  test("gates the dialog: zero counts call confirmQuit, otherwise open dialog", () => {
    expect(source).toMatch(/localSessionCount\.value\s*===\s*0[^\n]*remoteSessionCount\.value\s*===\s*0/);
    expect(source).toContain("quitDialogOpen.value = true");
  });
});
