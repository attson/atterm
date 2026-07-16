import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount } from "@vue/test-utils";
import BinaryBanner from "./BinaryBanner.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";
import { createLocalFSBridge, type FileSystemBridge } from "./fsBridge";

let platform: ReturnType<typeof createFakePlatform>;
let fs: FileSystemBridge;
beforeEach(() => {
  vi.clearAllMocks();
  platform = createFakePlatform();
  __setPlatformForTests(platform);
  fs = createLocalFSBridge(platform.pluginHost!, platform.events);
});
afterEach(() => __setPlatformForTests(null));

describe("BinaryBanner", () => {
  it("renders the default message and an open-in-system button", () => {
    const w = mount(BinaryBanner, { props: { fs, path: "/x/blob.dat" } });
    expect(w.text()).toContain("Inline preview unavailable");
    expect(w.find("button").exists()).toBe(true);
  });

  it("uses an override message when provided", () => {
    const w = mount(BinaryBanner, { props: { fs, path: "/x/blob.dat", message: "boom" } });
    expect(w.text()).toContain("boom");
  });

  it("clicking the button calls fs.openExternal with the path", async () => {
    const w = mount(BinaryBanner, { props: { fs, path: "/x/blob.dat" } });
    await w.find("button").trigger("click");
    expect(platform.pluginHost!.fs.openExternal).toHaveBeenCalledWith("/x/blob.dat");
  });
});
