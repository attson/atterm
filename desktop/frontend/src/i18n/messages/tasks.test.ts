import { describe, expect, test } from "vitest";
import { en } from "./en";
import { zhCN } from "./zh-CN";

describe("tasks i18n namespace", () => {
  test("en has the full tasks namespace", () => {
    expect(en.tasks.sidebar.title).toBeTypeOf("string");
    expect(en.tasks.preset.iconOnly.name).toBeTypeOf("string");
    expect(en.tasks.preset.iconOnly.description).toBeTypeOf("string");
    expect(en.tasks.preset.iconLabel.name).toBeTypeOf("string");
    expect(en.tasks.preset.iconLabel.description).toBeTypeOf("string");
    expect(en.tasks.markAllRead).toBeTypeOf("string");
    expect(en.tasks.completedFold).toBeTypeOf("string");
    expect(en.tasks.unreadBadge).toBeTypeOf("string");
    expect(en.tasks.settings.section).toBeTypeOf("string");
    expect(en.tasks.settings.preset).toBeTypeOf("string");
    expect(en.tasks.settings.expandByDefault).toBeTypeOf("string");
    expect(en.tasks.settings.groupBy).toBeTypeOf("string");
    expect(en.tasks.settings.groupByHost).toBeTypeOf("string");
    expect(en.tasks.settings.groupByState).toBeTypeOf("string");
  });
  test("zh-CN matches en shape", () => {
    expect(Object.keys(zhCN.tasks)).toEqual(Object.keys(en.tasks));
    expect(Object.keys(zhCN.tasks.sidebar)).toEqual(Object.keys(en.tasks.sidebar));
    expect(Object.keys(zhCN.tasks.preset)).toEqual(Object.keys(en.tasks.preset));
    expect(Object.keys(zhCN.tasks.preset.iconOnly)).toEqual(Object.keys(en.tasks.preset.iconOnly));
    expect(Object.keys(zhCN.tasks.preset.iconLabel)).toEqual(Object.keys(en.tasks.preset.iconLabel));
    expect(Object.keys(zhCN.tasks.settings)).toEqual(Object.keys(en.tasks.settings));
  });
});
