import { describe, it, expect } from "vitest";
import { normalizeTags, parseTagInput, allHostTags, hostHasAllTags } from "./hostTags";

describe("normalizeTags", () => {
  it("trims surrounding whitespace", () => {
    expect(normalizeTags([" prod ", "db "])).toEqual(["prod", "db"]);
  });

  it("drops empty and whitespace-only entries", () => {
    expect(normalizeTags(["prod", "", "   "])).toEqual(["prod"]);
  });

  it("removes duplicates, keeping the first spelling", () => {
    expect(normalizeTags(["Prod", "prod", "PROD"])).toEqual(["Prod"]);
  });

  it("preserves the order the user entered", () => {
    expect(normalizeTags(["web", "prod", "db"])).toEqual(["web", "prod", "db"]);
  });

  it("returns an empty array for an empty input", () => {
    expect(normalizeTags([])).toEqual([]);
  });
});

describe("parseTagInput", () => {
  it("splits on commas", () => {
    expect(parseTagInput("prod,db")).toEqual(["prod", "db"]);
  });

  it("tolerates spaces around the commas", () => {
    expect(parseTagInput("prod , db , web")).toEqual(["prod", "db", "web"]);
  });

  it("keeps a multi-word tag intact", () => {
    expect(parseTagInput("hong kong")).toEqual(["hong kong"]);
  });

  it("ignores trailing separators", () => {
    expect(parseTagInput("prod,")).toEqual(["prod"]);
  });

  it("returns an empty array for blank text", () => {
    expect(parseTagInput("   ")).toEqual([]);
  });
});

describe("allHostTags", () => {
  it("returns the union across hosts, sorted", () => {
    const hosts = [{ tags: ["web", "prod"] }, { tags: ["db", "prod"] }];
    expect(allHostTags(hosts)).toEqual(["db", "prod", "web"]);
  });

  it("tolerates hosts with no tags field", () => {
    expect(allHostTags([{ tags: ["prod"] }, {}])).toEqual(["prod"]);
  });

  it("folds case-insensitive duplicates into one entry", () => {
    expect(allHostTags([{ tags: ["Prod"] }, { tags: ["prod"] }])).toEqual(["Prod"]);
  });

  it("returns an empty array when nothing is tagged", () => {
    expect(allHostTags([{}, { tags: [] }])).toEqual([]);
  });
});

describe("hostHasAllTags", () => {
  it("matches every host when nothing is selected", () => {
    expect(hostHasAllTags(["prod"], [])).toBe(true);
    expect(hostHasAllTags(undefined, [])).toBe(true);
  });

  it("requires all selected tags to be present", () => {
    expect(hostHasAllTags(["prod", "db"], ["prod", "db"])).toBe(true);
    expect(hostHasAllTags(["prod"], ["prod", "db"])).toBe(false);
  });

  it("ignores extra tags on the host", () => {
    expect(hostHasAllTags(["prod", "db", "web"], ["prod"])).toBe(true);
  });

  it("compares case-insensitively", () => {
    expect(hostHasAllTags(["Prod"], ["prod"])).toBe(true);
  });

  it("never matches an untagged host when a tag is selected", () => {
    expect(hostHasAllTags(undefined, ["prod"])).toBe(false);
    expect(hostHasAllTags([], ["prod"])).toBe(false);
  });
});
