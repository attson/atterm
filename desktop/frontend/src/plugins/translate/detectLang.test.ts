import { describe, expect, it } from "vitest";
import { hasCJK, computeAutoTargetLang } from "./detectLang";

describe("hasCJK", () => {
  it("returns true for Chinese", () => {
    expect(hasCJK("你好世界")).toBe(true);
  });
  it("returns true for Japanese kanji", () => {
    expect(hasCJK("日本")).toBe(true);
  });
  it("returns true for mixed text", () => {
    expect(hasCJK("error: 文件不存在")).toBe(true);
  });
  it("returns false for ASCII", () => {
    expect(hasCJK("dial tcp 10.0.0.5:6379")).toBe(false);
  });
  it("returns false for empty string", () => {
    expect(hasCJK("")).toBe(false);
  });
});

describe("computeAutoTargetLang", () => {
  it("CJK text + default zh-CN → returns 'en'", () => {
    expect(computeAutoTargetLang("你好", "zh-CN")).toBe("en");
  });
  it("ASCII text + default zh-CN → returns 'zh-CN'", () => {
    expect(computeAutoTargetLang("hello", "zh-CN")).toBe("zh-CN");
  });
  it("CJK text + default 'en' → returns 'en' (config wins)", () => {
    expect(computeAutoTargetLang("你好", "en")).toBe("en");
  });
  it("ASCII text + default 'ja' → returns 'ja'", () => {
    expect(computeAutoTargetLang("hello", "ja")).toBe("ja");
  });
});
