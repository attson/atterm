import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount } from "@vue/test-utils";
import MediaPreview from "./MediaPreview.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";

let platform: ReturnType<typeof createFakePlatform>;
beforeEach(() => {
  vi.clearAllMocks();
  platform = createFakePlatform();
  __setPlatformForTests(platform);
});
afterEach(() => __setPlatformForTests(null));

describe("MediaPreview", () => {
  it("renders a <video> tag for kind=video", () => {
    (platform.pluginHost!.fs.assetUrlFor as ReturnType<typeof vi.fn>).mockReturnValueOnce("/pluginfs/V");
    const w = mount(MediaPreview, { props: { path: "/x/c.mp4", kind: "video" } });
    expect(w.find("video").exists()).toBe(true);
    expect(w.find("video").attributes("src")).toBe("/pluginfs/V");
    expect(w.find("video").attributes("preload")).toBe("metadata");
  });

  it("renders an <audio> tag for kind=audio", () => {
    (platform.pluginHost!.fs.assetUrlFor as ReturnType<typeof vi.fn>).mockReturnValueOnce("/pluginfs/A");
    const w = mount(MediaPreview, { props: { path: "/x/t.mp3", kind: "audio" } });
    expect(w.find("audio").exists()).toBe(true);
    expect(w.find("audio").attributes("src")).toBe("/pluginfs/A");
  });

  it("falls back to BinaryBanner on media error", async () => {
    const w = mount(MediaPreview, { props: { path: "/x/c.mp4", kind: "video" } });
    await w.find("video").trigger("error");
    expect(w.text()).toContain("Inline preview unavailable");
  });
});
