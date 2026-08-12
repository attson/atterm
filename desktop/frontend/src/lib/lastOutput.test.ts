import { describe, expect, it } from "vitest";
import { formatLastOutput, LAST_OUTPUT_LIVE_WINDOW_MS } from "./lastOutput";

const NOW = 1_800_000_000_000;
const at = (elapsedMs: number) => (NOW - elapsedMs) / 1000;

describe("formatLastOutput", () => {
  it("hides missing, invalid, and non-positive timestamps", () => {
    expect(formatLastOutput(undefined, "running", NOW)).toBeNull();
    expect(formatLastOutput(0, "running", NOW)).toBeNull();
    expect(formatLastOutput(Number.NaN, "running", NOW)).toBeNull();
  });

  it("shows live only for running sessions inside the freshness window", () => {
    expect(formatLastOutput(at(LAST_OUTPUT_LIVE_WINDOW_MS), "running", NOW)).toEqual({
      text: "live", live: true, title: "Output active",
    });
    expect(formatLastOutput(at(1_000), "waiting_input", NOW)?.text).toBe("now");
    expect(formatLastOutput(at(LAST_OUTPUT_LIVE_WINDOW_MS + 1), "running", NOW)?.text).toBe("now");
  });

  it("clamps a future timestamp to now", () => {
    expect(formatLastOutput((NOW + 60_000) / 1000, "idle", NOW)?.text).toBe("now");
    expect(formatLastOutput((NOW + 60_000) / 1000, "running", NOW)).toEqual({
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
