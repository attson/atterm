import { afterEach, describe, expect, test, vi } from "vitest";
import {
  getTaskPreset,
  setTaskPreset,
  getTaskSidebarCollapsed,
  setTaskSidebarCollapsed,
  markSessionsSeen,
  getUserHomeDir,
  getTaskSidebarWidth,
  setTaskSidebarWidth,
  __setBindingsForTest,
} from "./api";

afterEach(() => {
  __setBindingsForTest(undefined);
});

describe("task display api wrappers", () => {
  test("getTaskPreset delegates to bindings", async () => {
    const fn = vi.fn().mockResolvedValue("iconOnly");
    __setBindingsForTest({ GetTaskPreset: fn } as any);
    await expect(getTaskPreset()).resolves.toBe("iconOnly");
    expect(fn).toHaveBeenCalledOnce();
  });
  test("setTaskPreset passes the preset string", async () => {
    const fn = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest({ SetTaskPreset: fn } as any);
    await setTaskPreset("iconLabel");
    expect(fn).toHaveBeenCalledWith("iconLabel");
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
  test("getUserHomeDir delegates to bindings", async () => {
    const fn = vi.fn().mockResolvedValue("/Users/attson");
    __setBindingsForTest({ GetUserHomeDir: fn } as any);
    await expect(getUserHomeDir()).resolves.toBe("/Users/attson");
    expect(fn).toHaveBeenCalledOnce();
  });
  test("getTaskSidebarWidth delegates to bindings", async () => {
    const fn = vi.fn().mockResolvedValue(300);
    __setBindingsForTest({ GetTaskSidebarWidth: fn } as any);
    await expect(getTaskSidebarWidth()).resolves.toBe(300);
  });
  test("setTaskSidebarWidth passes the px value", async () => {
    const fn = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest({ SetTaskSidebarWidth: fn } as any);
    await setTaskSidebarWidth(280);
    expect(fn).toHaveBeenCalledWith(280);
  });
});
