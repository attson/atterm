import { describe, expect, test } from "vitest";
import { posixShellQuote } from "./shellQuote";

describe("posixShellQuote", () => {
  test("leaves a plain word unquoted", () => {
    expect(posixShellQuote("foo")).toBe("foo");
  });

  test("leaves a safe absolute path unquoted", () => {
    expect(posixShellQuote("/Users/attson/code/atterm/docker-compose.yml")).toBe(
      "/Users/attson/code/atterm/docker-compose.yml",
    );
  });

  test("quotes when the string contains a space", () => {
    expect(posixShellQuote("foo bar")).toBe("'foo bar'");
  });

  test("quotes when the string contains a shell metacharacter", () => {
    expect(posixShellQuote("path/with$var")).toBe("'path/with$var'");
    expect(posixShellQuote("~/Documents/a.txt")).toBe("'~/Documents/a.txt'");
    expect(posixShellQuote("a&b")).toBe("'a&b'");
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
