import { describe, expect, it } from "vitest";
import { quoteForShell } from "./shellQuote";

describe("quoteForShell", () => {
  it("returns a simple path unchanged", () => {
    expect(quoteForShell("/home/user/file.txt")).toBe("/home/user/file.txt");
  });

  it("returns a path with dots/dashes/underscores unchanged", () => {
    expect(quoteForShell("/a/b-c_d.e.txt")).toBe("/a/b-c_d.e.txt");
  });

  it("single-quotes a path containing spaces", () => {
    expect(quoteForShell("/a b/c.txt")).toBe("'/a b/c.txt'");
  });

  it("single-quotes a path with shell metacharacters", () => {
    expect(quoteForShell("/a$(b)/c.txt")).toBe("'/a$(b)/c.txt'");
  });

  it("escapes embedded single quotes", () => {
    expect(quoteForShell("/a'b/c.txt")).toBe("'/a'\\''b/c.txt'");
  });

  it("quotes a path with a tilde in the middle", () => {
    expect(quoteForShell("/a~b/c")).toBe("'/a~b/c'");
  });
});
