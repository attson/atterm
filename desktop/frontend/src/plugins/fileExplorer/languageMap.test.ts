import { describe, expect, it } from "vitest";
import { languageForPath } from "./languageMap";

describe("languageForPath", () => {
  it("returns javascript for .js", async () => {
    const ext = await languageForPath("/x/a.js");
    expect(ext).not.toBeNull();
  });
  it("returns null for unknown extension", async () => {
    const ext = await languageForPath("/x/a.zzz");
    expect(ext).toBeNull();
  });
  it("handles missing extension as null", async () => {
    const ext = await languageForPath("/x/LICENSE");
    expect(ext).toBeNull();
  });
});
