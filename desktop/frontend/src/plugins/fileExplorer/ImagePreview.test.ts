import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount } from "@vue/test-utils";
import ImagePreview from "./ImagePreview.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";

let platform: ReturnType<typeof createFakePlatform>;
beforeEach(() => {
  vi.clearAllMocks();
  platform = createFakePlatform();
  __setPlatformForTests(platform);
});
afterEach(() => __setPlatformForTests(null));

describe("ImagePreview", () => {
  it("uses fs.assetUrlFor for the <img> src", () => {
    (platform.pluginHost!.fs.assetUrlFor as ReturnType<typeof vi.fn>).mockReturnValueOnce(
      "/pluginfs/AAAA",
    );
    const w = mount(ImagePreview, { props: { path: "/x/photo.png", theme: "dimmed" } });
    expect(w.find("img").attributes("src")).toBe("/pluginfs/AAAA");
  });

  it("toggles fit ↔ native on click", async () => {
    const w = mount(ImagePreview, { props: { path: "/x/photo.png", theme: "dimmed" } });
    expect(w.find(".img-host").classes()).toContain("fit");
    await w.find("img").trigger("click");
    expect(w.find(".img-host").classes()).toContain("native");
  });

  it("falls back to BinaryBanner on <img> error", async () => {
    const w = mount(ImagePreview, { props: { path: "/x/broken.png", theme: "dimmed" } });
    await w.find("img").trigger("error");
    expect(w.text()).toContain("Inline preview unavailable");
  });
});
