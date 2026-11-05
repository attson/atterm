import { describe, expect, it } from "vitest";
import { dateInTimeZone, formatLocalDateTime, formatLocalTime, formatLocalTimeRange, zonedDateTimeToUtc } from "./timeZone";

describe("system-time-zone formatting", () => {
  it("converts UTC traffic buckets to the requested local time zone", () => {
    expect(formatLocalTime("2026-11-05T03:00:00Z", "zh-CN", "Asia/Shanghai")).toBe("11:00");
    expect(formatLocalDateTime("2026-11-05T03:53:47Z", "zh-CN", "Asia/Shanghai"))
      .toContain("2026/11/05 11:53");
  });

  it("keeps the date on both sides when a bucket crosses local midnight", () => {
    const label = formatLocalTimeRange(
      "2026-11-05T07:30:00Z",
      "2026-11-05T08:30:00Z",
      "en",
      "America/Los_Angeles",
    );
    expect(label).toContain("11/04/2026, 23:30");
    expect(label).toContain("11/05/2026, 00:30");
  });

  it("converts reporting midnights for quarter-hour offsets", () => {
    const instant = zonedDateTimeToUtc("2026-11-05", 0, 0, "Asia/Kathmandu");
    expect(new Date(instant).toISOString()).toBe("2026-11-04T18:15:00.000Z");
    expect(dateInTimeZone(instant, "Asia/Kathmandu")).toBe("2026-11-05");
  });
});
