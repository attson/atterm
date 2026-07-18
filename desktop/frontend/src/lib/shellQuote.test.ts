import { describe, expect, test } from "vitest";
import { posixShellQuote } from "./shellQuote";

describe("posixShellQuote", () => {
  test("wraps a plain word in single quotes", () => {
    expect(posixShellQuote("foo")).toBe("'foo'");
  });

  test("preserves spaces inside single quotes", () => {
    expect(posixShellQuote("foo bar")).toBe("'foo bar'");
  });

  test("escapes an embedded single quote as '\\''", () => {
    expect(posixShellQuote("it's fine")).toBe("'it'\\''s fine'");
  });

  test("wraps the empty string as ''", () => {
    expect(posixShellQuote("")).toBe("''");
  });

  test("handles a path with both spaces and an apostrophe", () => {
    expect(posixShellQuote("/Users/a b/c'd.yml")).toBe("'/Users/a b/c'\\''d.yml'");
  });
});
