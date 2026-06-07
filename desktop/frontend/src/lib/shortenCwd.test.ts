import { describe, expect, test } from "vitest";
import { shortenCwd } from "./shortenCwd";

describe("shortenCwd", () => {
  const HOME = "/Users/attson";

  test("empty input returns empty string", () => {
    expect(shortenCwd("", HOME)).toBe("");
    expect(shortenCwd(undefined, HOME)).toBe("");
  });

  test("HOME prefix replaced with ~", () => {
    expect(shortenCwd("/Users/attson", HOME)).toBe("~");
    expect(shortenCwd("/Users/attson/code", HOME)).toBe("~/code");
    expect(shortenCwd("/Users/attson/code/atterm", HOME)).toBe("~/code/atterm");
  });

  test("paths with 2 or fewer segments are kept verbatim", () => {
    expect(shortenCwd("/tmp", HOME)).toBe("/tmp");
    expect(shortenCwd("/tmp/build", HOME)).toBe("/tmp/build");
    expect(shortenCwd("~/code", HOME)).toBe("~/code");
  });

  test("long absolute paths get …/last/two", () => {
    expect(shortenCwd("/Users/attson/code/github.com.attson/atterm", HOME))
      .toBe("…/github.com.attson/atterm");
    expect(shortenCwd("/Users/someone/a/b/c", HOME))
      .toBe("…/b/c");
  });

  test("long HOME-rooted paths get …/last/two", () => {
    expect(shortenCwd("/Users/attson/a/b/c/d", HOME)).toBe("…/c/d");
  });

  test("empty HOME skips ~ substitution but still truncates long paths", () => {
    expect(shortenCwd("/Users/attson/code/atterm", "")).toBe("…/code/atterm");
  });
});
