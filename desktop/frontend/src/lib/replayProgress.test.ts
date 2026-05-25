import { describe, expect, it } from "vitest";

import { formatReplayProgress } from "./replayProgress";

describe("replay progress", () => {
  it("formats percent with byte units", () => {
    expect(formatReplayProgress({ phase: "chunk", bytes: 1_048_576, total_bytes: 4_194_304 })).toBe(
      "loading history 25% - 1.0 MB / 4.0 MB",
    );
  });

  it("handles unknown totals without percent", () => {
    expect(formatReplayProgress({ phase: "start", bytes: 512, total_bytes: 0 })).toBe(
      "loading history - 512 B",
    );
  });
});
