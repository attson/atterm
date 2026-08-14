import { describe, expect, it } from "vitest";
import { formatLastOutput } from "./lastOutput";

const NOW = 1_800_000_000_000;
const at = (elapsedMs: number) => (NOW - elapsedMs) / 1000;

describe("formatLastOutput", () => {
  it("hides missing, invalid, and non-positive timestamps", () => {
    expect(formatLastOutput(undefined, "running", NOW)).toBeNull();
    expect(formatLastOutput(0, "running", NOW)).toBeNull();
    expect(formatLastOutput(Number.NaN, "running", NOW)).toBeNull();
  });

  it("stays live for a running session no matter the gap since its last chunk", () => {
    // A quiet gap between output bursts must not flip the indicator off live —
    // that gap is the model thinking, not the session going idle.
    for (const elapsedMs of [0, 1_000, 5_001, 30_000, 5 * 60_000]) {
      expect(formatLastOutput(at(elapsedMs), "running", NOW)).toEqual({
        text: "live", live: true, title: "Output active",
      });
    }
  });

  it("shows relative time, never live, for non-running sessions", () => {
    expect(formatLastOutput(at(1_000), "waiting_input", NOW)?.text).toBe("now");
    expect(formatLastOutput(at(1_000), "waiting_input", NOW)?.live).toBe(false);
    expect(formatLastOutput(at(90_000), "completed", NOW)?.text).toBe("1m");
    expect(formatLastOutput(at(90_000), "completed", NOW)?.live).toBe(false);
  });

  it("clamps a future timestamp to now for non-running sessions", () => {
    expect(formatLastOutput((NOW + 60_000) / 1000, "idle", NOW)).toEqual({
      text: "now", live: false, title: "Last output just now",
    });
  });

  it.each([
    [59_999, "now"],
    [60_000, "1m"],
    [17 * 60_000 + 59_000, "17m"],
    [60 * 60_000, "1h"],
    [8 * 60 * 60_000 + 59 * 60_000, "8h"],
    [24 * 60 * 60_000, "1d"],
    [12 * 24 * 60 * 60_000, "12d"],
  ])("formats %dms as %s", (elapsedMs, text) => {
    expect(formatLastOutput(at(elapsedMs), "idle", NOW)?.text).toBe(text);
  });
});
