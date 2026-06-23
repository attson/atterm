import { afterEach, describe, expect, test, vi } from "vitest";
import {
  InitializeNotifications,
  IsNotificationAvailable,
  CheckNotificationAuthorization,
  RequestNotificationAuthorization,
  SendNotification,
} from "../../wailsjs/runtime/runtime";
import {
  getTaskPreset,
  setTaskPreset,
  getTaskSidebarCollapsed,
  setTaskSidebarCollapsed,
  markSessionsSeen,
  getUserHomeDir,
  getTaskSidebarWidth,
  setTaskSidebarWidth,
  showNotification,
  listShells,
  setDefaultShell,
  __setBindingsForTest,
  __resetNotificationRuntimeForTest,
  __resetShellsCacheForTest,
} from "./api";

vi.mock("../../wailsjs/runtime/runtime", () => ({
  InitializeNotifications: vi.fn().mockResolvedValue(undefined),
  IsNotificationAvailable: vi.fn().mockResolvedValue(true),
  CheckNotificationAuthorization: vi.fn().mockResolvedValue(true),
  RequestNotificationAuthorization: vi.fn().mockResolvedValue(true),
  SendNotification: vi.fn().mockResolvedValue(undefined),
}));

afterEach(() => {
  __setBindingsForTest(undefined);
  __resetNotificationRuntimeForTest();
  __resetShellsCacheForTest();
  vi.clearAllMocks();
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

describe("listShells caching", () => {
  test("caches the shell list so repeated calls hit the binding once", async () => {
    const fn = vi.fn().mockResolvedValue(["/bin/zsh", "/bin/bash"]);
    __setBindingsForTest({ ListShells: fn } as any);
    expect(await listShells()).toEqual(["/bin/zsh", "/bin/bash"]);
    expect(await listShells()).toEqual(["/bin/zsh", "/bin/bash"]);
    expect(fn).toHaveBeenCalledOnce();
  });

  test("setDefaultShell invalidates the cache (default reorders the list)", async () => {
    const list = vi
      .fn()
      .mockResolvedValueOnce(["/bin/zsh"])
      .mockResolvedValueOnce(["/opt/homebrew/bin/fish", "/bin/zsh"]);
    const set = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest({ ListShells: list, SetDefaultShell: set } as any);
    expect(await listShells()).toEqual(["/bin/zsh"]);
    await setDefaultShell("/opt/homebrew/bin/fish");
    expect(await listShells()).toEqual(["/opt/homebrew/bin/fish", "/bin/zsh"]);
    expect(list).toHaveBeenCalledTimes(2);
  });

  test("does not cache a rejected lookup so it can be retried", async () => {
    const fn = vi
      .fn()
      .mockRejectedValueOnce(new Error("not ready"))
      .mockResolvedValueOnce(["/bin/zsh"]);
    __setBindingsForTest({ ListShells: fn } as any);
    await expect(listShells()).rejects.toThrow("not ready");
    expect(await listShells()).toEqual(["/bin/zsh"]);
    expect(fn).toHaveBeenCalledTimes(2);
  });
});

describe("notification api wrapper", () => {
  test("prefers Wails native notifications over the Go shell fallback", async () => {
    const fallback = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest({ ShowNotification: fallback } as any);

    await showNotification("AT Term", "Bell in caiji2");

    expect(IsNotificationAvailable).toHaveBeenCalledOnce();
    expect(InitializeNotifications).toHaveBeenCalledOnce();
    expect(CheckNotificationAuthorization).toHaveBeenCalledOnce();
    expect(RequestNotificationAuthorization).not.toHaveBeenCalled();
    expect(SendNotification).toHaveBeenCalledWith(
      expect.objectContaining({
        id: expect.stringMatching(/^atterm-/),
        title: "AT Term",
        body: "Bell in caiji2",
      }),
    );
    expect(fallback).not.toHaveBeenCalled();
  });

  test("falls back to the Go binding when Wails native notifications are unavailable", async () => {
    const fallback = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest({ ShowNotification: fallback } as any);
    vi.mocked(IsNotificationAvailable).mockResolvedValueOnce(false);

    await showNotification("AT Term", "Bell in caiji2");

    expect(SendNotification).not.toHaveBeenCalled();
    expect(fallback).toHaveBeenCalledWith("AT Term", "Bell in caiji2");
  });
});
