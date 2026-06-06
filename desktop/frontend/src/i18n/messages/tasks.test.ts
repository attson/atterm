import { describe, expect, test } from "vitest";
import { en } from "./en";
import { zhCN } from "./zh-CN";

describe("tasks i18n namespace", () => {
  test("en has the full tasks namespace", () => {
    expect(en.tasks.sidebar.title).toBeTypeOf("string");
    expect(en.tasks.preset.vivid.name).toBeTypeOf("string");
    expect(en.tasks.preset.vivid.description).toBeTypeOf("string");
    expect(en.tasks.preset.quiet.name).toBeTypeOf("string");
    expect(en.tasks.preset.quiet.description).toBeTypeOf("string");
    expect(en.tasks.markAllRead).toBeTypeOf("string");
    expect(en.tasks.markRead).toBeTypeOf("string");
    expect(en.tasks.completedFold).toBeTypeOf("string");
    expect(en.tasks.unreadBadge).toBeTypeOf("string");
    expect(en.tasks.settings.section).toBeTypeOf("string");
    expect(en.tasks.settings.preset).toBeTypeOf("string");
    expect(en.tasks.settings.expandByDefault).toBeTypeOf("string");
    expect(en.tasks.unavailableToast).toBeTypeOf("string");
  });
  test("zh-CN matches en shape", () => {
    expect(Object.keys(zhCN.tasks)).toEqual(Object.keys(en.tasks));
    expect(Object.keys(zhCN.tasks.sidebar)).toEqual(Object.keys(en.tasks.sidebar));
    expect(Object.keys(zhCN.tasks.preset)).toEqual(Object.keys(en.tasks.preset));
    expect(Object.keys(zhCN.tasks.preset.vivid)).toEqual(Object.keys(en.tasks.preset.vivid));
    expect(Object.keys(zhCN.tasks.preset.quiet)).toEqual(Object.keys(en.tasks.preset.quiet));
    expect(Object.keys(zhCN.tasks.settings)).toEqual(Object.keys(en.tasks.settings));
  });
});
