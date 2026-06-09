import { describe, expect, test } from "vitest";
import { en } from "./en";
import { zhCN } from "./zh-CN";

describe("mobile.selection i18n namespace", () => {
  test("en has all selection keys", () => {
    expect(en.mobile.selection.copy).toBeTypeOf("string");
    expect(en.mobile.selection.send).toBeTypeOf("string");
    expect(en.mobile.selection.cancel).toBeTypeOf("string");
    expect(en.mobile.selection.copied).toBeTypeOf("string");
  });
  test("zh-CN matches en shape", () => {
    expect(Object.keys(zhCN.mobile.selection)).toEqual(Object.keys(en.mobile.selection));
  });
});
