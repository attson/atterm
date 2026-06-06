import { describe, expect, test } from "vitest";
import {
  presets,
  type PresetId,
  type TaskState,
  ALL_TASK_STATES,
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
  test("ALL_TASK_STATES enumerates every state", () => {
    expect([...ALL_TASK_STATES].sort()).toEqual([...STATES].sort());
  });

  for (const id of ["vivid", "quiet"] as PresetId[]) {
    describe(id, () => {
      const p = presets[id];
      test("has id + i18nKey", () => {
        expect(p.id).toBe(id);
        expect(p.i18nKey).toContain("tasks.preset." + id);
      });
      test.each(STATES)("colorOf(%s) returns a hex color", (s) => {
        expect(p.colorOf(s)).toMatch(/^#[0-9a-f]{6}$/i);
      });
      test.each(STATES)("glyphOf(%s) returns spinner or a single char", (s) => {
        const g = p.glyphOf(s);
        expect(typeof g).toBe("string");
        if (g !== "spinner") expect(g.length).toBe(1);
      });
      test("running uses spinner glyph", () => {
        expect(p.glyphOf("running")).toBe("spinner");
      });
      test("spinnerDurationMs(running) > 0", () => {
        expect(p.spinnerDurationMs("running")).toBeGreaterThan(0);
      });
      test("spinnerDurationMs(non-running) is 0", () => {
        for (const s of STATES.filter((x) => x !== "running")) {
          expect(p.spinnerDurationMs(s)).toBe(0);
        }
      });
      test("colors match spec exactly", () => {
        if (id === "vivid") {
          expect(p.colorOf("idle")).toBe("#6b7280");
          expect(p.colorOf("running")).toBe("#06b6d4");
          expect(p.colorOf("waiting_input")).toBe("#f59e0b");
          expect(p.colorOf("completed")).toBe("#22c55e");
          expect(p.colorOf("failed")).toBe("#ef4444");
          expect(p.colorOf("disconnected")).toBe("#6b7280");
          expect(p.colorOf("closed")).toBe("#6b7280");
        } else {
          expect(p.colorOf("idle")).toBe("#6b7280");
          expect(p.colorOf("running")).toBe("#4b8a93");
          expect(p.colorOf("waiting_input")).toBe("#b88239");
          expect(p.colorOf("completed")).toBe("#4a8b6a");
          expect(p.colorOf("failed")).toBe("#a04b4b");
          expect(p.colorOf("disconnected")).toBe("#6b7280");
          expect(p.colorOf("closed")).toBe("#6b7280");
        }
      });
    });
  }

  test("vivid pulses waiting_input; quiet does not", () => {
    expect(presets.vivid.animatePulse("waiting_input")).toBe(true);
    expect(presets.quiet.animatePulse("waiting_input")).toBe(false);
  });
  test("only vivid shows type icon", () => {
    expect(presets.vivid.showTypeIcon).toBe(true);
    expect(presets.quiet.showTypeIcon).toBe(false);
  });
  test("quiet text opacity is lower than vivid", () => {
    expect(presets.quiet.textOpacity).toBeLessThan(presets.vivid.textOpacity);
  });
});
