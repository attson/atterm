import { describe, expect, test } from "vitest";
import {
  CommandTracker,
  formatElapsed,
  shouldNotifyCommand,
} from "./commandFinish";

describe("CommandTracker", () => {
  test("A and B do not emit events", () => {
    const t = new CommandTracker();
    expect(t.onOsc133("A", 1000)).toBeNull();
    expect(t.onOsc133("B", 1010)).toBeNull();
  });

  test("D without preceding C is orphan and ignored", () => {
    const t = new CommandTracker();
    expect(t.onOsc133("D;0", 1000)).toBeNull();
  });

  test("C then D emits finished event with elapsedMs and exitCode", () => {
    const t = new CommandTracker();
    t.onOsc133("C", 1000);
    const ev = t.onOsc133("D;0", 12500);
    expect(ev).not.toBeNull();
    expect(ev!.kind).toBe("finished");
    expect(ev!.exitCode).toBe(0);
    expect(ev!.elapsedMs).toBe(11500);
  });

  test("bare D treats exit as 0", () => {
    const t = new CommandTracker();
    t.onOsc133("C", 0);
    const ev = t.onOsc133("D", 5000);
    expect(ev!.exitCode).toBe(0);
  });

  test("non-numeric exit code becomes -1", () => {
    const t = new CommandTracker();
    t.onOsc133("C", 0);
    const ev = t.onOsc133("D;abc", 5000);
    expect(ev!.exitCode).toBe(-1);
  });

  test("two consecutive C overwrite", () => {
    const t = new CommandTracker();
    t.onOsc133("C", 1000);
    t.onOsc133("C", 5000);
    const ev = t.onOsc133("D;1", 7500);
    expect(ev!.elapsedMs).toBe(2500);
  });

  test("after emitting, D cannot fire again without new C", () => {
    const t = new CommandTracker();
    t.onOsc133("C", 1000);
    expect(t.onOsc133("D;0", 2000)).not.toBeNull();
    expect(t.onOsc133("D;0", 3000)).toBeNull();
  });
});

describe("shouldNotifyCommand", () => {
  const ev = { kind: "finished" as const, exitCode: 0, elapsedMs: 15000 };

  test("focused window suppresses notification", () => {
    expect(
      shouldNotifyCommand(ev, { focused: true, thresholdSec: 10, isLocal: true }),
    ).toBe(false);
  });

  test("non-local session suppresses notification", () => {
    expect(
      shouldNotifyCommand(ev, { focused: false, thresholdSec: 10, isLocal: false }),
    ).toBe(false);
  });

  test("below threshold suppresses notification", () => {
    expect(
      shouldNotifyCommand(
        { ...ev, elapsedMs: 5000 },
        { focused: false, thresholdSec: 10, isLocal: true },
      ),
    ).toBe(false);
  });

  test("unfocused, local, >=threshold passes", () => {
    expect(
      shouldNotifyCommand(ev, { focused: false, thresholdSec: 10, isLocal: true }),
    ).toBe(true);
  });

  test("threshold of 0 is clamped to 1 (still passes 15s)", () => {
    expect(
      shouldNotifyCommand(ev, { focused: false, thresholdSec: 0, isLocal: true }),
    ).toBe(true);
  });
});

describe("formatElapsed", () => {
  test.each([
    [0, "0s"],
    [999, "0s"],
    [1000, "1s"],
    [12500, "12s"],
    [59999, "59s"],
    [60000, "1m0s"],
    [125000, "2m5s"],
    [3599000, "59m59s"],
  ])("formats %dms as %s", (ms, want) => {
    expect(formatElapsed(ms)).toBe(want);
  });
});
