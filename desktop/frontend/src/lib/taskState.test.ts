import { describe, expect, test } from "vitest";
import {
  presets,
  type PresetId,
  type TaskState,
} from "./taskState";

const STATES: TaskState[] = [
  "idle",
  "running",
  "waiting_input",
  "completed",
  "failed",
  "disconnected",
  "closed",
];

describe("taskState presets", () => {
  for (const id of ["iconOnly", "iconLabel"] as PresetId[]) {
    describe(id, () => {
      const p = presets[id];
      test("has id + i18nKey", () => {
        expect(p.id).toBe(id);
        expect(p.i18nKey).toContain("tasks.preset." + id);
      });
      test.each(STATES)("colorOf(%s) returns a hex color", (s) => {
        expect(p.colorOf(s)).toMatch(/^#[0-9a-f]{6}$/i);
      });
      test("spinnerDurationMs(running) > 0", () => {
        expect(p.spinnerDurationMs("running")).toBeGreaterThan(0);
      });
      test("spinnerDurationMs(non-running) is 0", () => {
        for (const s of STATES.filter((x) => x !== "running")) {
          expect(p.spinnerDurationMs(s)).toBe(0);
        }
      });
      test("colors match the shared vivid palette", () => {
        expect(p.colorOf("idle")).toBe("#6b7280");
        expect(p.colorOf("running")).toBe("#06b6d4");
        expect(p.colorOf("waiting_input")).toBe("#f59e0b");
        expect(p.colorOf("completed")).toBe("#22c55e");
        expect(p.colorOf("failed")).toBe("#ef4444");
        expect(p.colorOf("disconnected")).toBe("#6b7280");
        expect(p.colorOf("closed")).toBe("#6b7280");
      });
    });
  }

  test("iconOnly hides label, iconLabel shows it", () => {
    expect(presets.iconOnly.showLabel).toBe(false);
    expect(presets.iconLabel.showLabel).toBe(true);
  });
  test("both presets pulse waiting_input (shared animation budget)", () => {
    expect(presets.iconOnly.animatePulse("waiting_input")).toBe(true);
    expect(presets.iconLabel.animatePulse("waiting_input")).toBe(true);
  });
});
