import { afterEach, describe, expect, test, vi } from "vitest";
import {
  getTaskPreset,
  setTaskPreset,
  getTaskSidebarCollapsed,
  setTaskSidebarCollapsed,
  markSessionsSeen,
  __setBindingsForTest,
} from "./api";

afterEach(() => {
  __setBindingsForTest(undefined);
});

describe("task display api wrappers", () => {
  test("getTaskPreset delegates to bindings", async () => {
    const fn = vi.fn().mockResolvedValue("vivid");
    __setBindingsForTest({ GetTaskPreset: fn } as any);
    await expect(getTaskPreset()).resolves.toBe("vivid");
    expect(fn).toHaveBeenCalledOnce();
  });
  test("setTaskPreset passes the preset string", async () => {
    const fn = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest({ SetTaskPreset: fn } as any);
    await setTaskPreset("quiet");
    expect(fn).toHaveBeenCalledWith("quiet");
  });
  test("getTaskSidebarCollapsed returns boolean", async () => {
    const fn = vi.fn().mockResolvedValue(true);
    __setBindingsForTest({ GetTaskSidebarCollapsed: fn } as any);
    await expect(getTaskSidebarCollapsed()).resolves.toBe(true);
  });
  test("setTaskSidebarCollapsed passes the flag", async () => {
    const fn = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest({ SetTaskSidebarCollapsed: fn } as any);
    await setTaskSidebarCollapsed(true);
    expect(fn).toHaveBeenCalledWith(true);
  });
  test("markSessionsSeen ids variant", async () => {
    const fn = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest({ MarkSessionsSeen: fn } as any);
    await markSessionsSeen({ ids: ["a", "b"] });
    expect(fn).toHaveBeenCalledWith(["a", "b"], false);
  });
  test("markSessionsSeen all variant", async () => {
    const fn = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest({ MarkSessionsSeen: fn } as any);
    await markSessionsSeen({ all: true });
    expect(fn).toHaveBeenCalledWith([], true);
  });
});
