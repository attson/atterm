import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import ImagePreview from "./ImagePreview.vue";
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

describe("ImagePreview", () => {
  it("uses the asynchronously resolved asset URL for the <img> src", async () => {
    (platform.pluginHost!.fs.assetUrlFor as ReturnType<typeof vi.fn>).mockReturnValueOnce(
      "/pluginfs/AAAA",
    );
    const w = mount(ImagePreview, { props: { fs, path: "/x/photo.png", theme: "dimmed" } });
    await flushPromises();
    expect(w.find("img").attributes("src")).toBe("/pluginfs/AAAA");
  });

  it("toggles fit ↔ native on click", async () => {
    const w = mount(ImagePreview, { props: { fs, path: "/x/photo.png", theme: "dimmed" } });
    expect(w.find(".img-host").classes()).toContain("fit");
    await w.find("img").trigger("click");
    expect(w.find(".img-host").classes()).toContain("native");
  });

  it("falls back to BinaryBanner on <img> error", async () => {
    const w = mount(ImagePreview, { props: { fs, path: "/x/broken.png", theme: "dimmed" } });
    await flushPromises();
    await w.find("img").trigger("error");
    expect(w.text()).toContain("Inline preview unavailable");
  });

  it("ignores an error from a stale image URL", async () => {
    (platform.pluginHost!.fs.assetUrlFor as ReturnType<typeof vi.fn>)
      .mockReturnValueOnce("blob:old")
      .mockReturnValueOnce("blob:new");
    const w = mount(ImagePreview, { props: { fs, path: "/x/old.png", theme: "dimmed" } });
    await flushPromises();
    await w.setProps({ path: "/x/new.png" });
    await flushPromises();

    const image = w.find("img");
    image.element.setAttribute("src", "blob:old");
    await image.trigger("error");

    expect(w.text()).not.toContain("Inline preview unavailable");
  });
});
