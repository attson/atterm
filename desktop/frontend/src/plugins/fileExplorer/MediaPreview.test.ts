import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import MediaPreview from "./MediaPreview.vue";
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

describe("MediaPreview", () => {
  it("renders a <video> tag for kind=video", async () => {
    (platform.pluginHost!.fs.assetUrlFor as ReturnType<typeof vi.fn>).mockReturnValueOnce("/pluginfs/V");
    const w = mount(MediaPreview, { props: { fs, path: "/x/c.mp4", kind: "video" } });
    await flushPromises();
    expect(w.find("video").exists()).toBe(true);
    expect(w.find("video").attributes("src")).toBe("/pluginfs/V");
    expect(w.find("video").attributes("preload")).toBe("metadata");
  });

  it("renders an <audio> tag for kind=audio", async () => {
    (platform.pluginHost!.fs.assetUrlFor as ReturnType<typeof vi.fn>).mockReturnValueOnce("/pluginfs/A");
    const w = mount(MediaPreview, { props: { fs, path: "/x/t.mp3", kind: "audio" } });
    await flushPromises();
    expect(w.find("audio").exists()).toBe(true);
    expect(w.find("audio").attributes("src")).toBe("/pluginfs/A");
  });

  it("falls back to BinaryBanner on media error", async () => {
    const revokeAssetUrl = vi.fn();
    fs.revokeAssetUrl = revokeAssetUrl;
    const w = mount(MediaPreview, { props: { fs, path: "/x/c.mp4", kind: "video" } });
    await flushPromises();
    await w.find("video").trigger("error");
    expect(w.text()).toContain("Inline preview unavailable");
    expect(revokeAssetUrl).toHaveBeenCalledWith("/x/c.mp4");
  });

  it("ignores an error while the asset URL is still pending", async () => {
    let resolveAsset!: (url: string) => void;
    (platform.pluginHost!.fs.assetUrlFor as ReturnType<typeof vi.fn>).mockImplementationOnce(
      () => new Promise<string>((resolve) => { resolveAsset = resolve; }),
    );
    const w = mount(MediaPreview, { props: { fs, path: "/x/pending.mp4", kind: "video" } });
    await flushPromises();
    await w.find("video").trigger("error");
    resolveAsset("blob:ready");
    await flushPromises();

    expect(w.find("video").attributes("src")).toBe("blob:ready");
    expect(w.text()).not.toContain("Inline preview unavailable");
  });

  it("ignores a queued error from the old video element after a new src is assigned", async () => {
    (platform.pluginHost!.fs.assetUrlFor as ReturnType<typeof vi.fn>)
      .mockReturnValueOnce("blob:old")
      .mockReturnValueOnce("blob:new");
    const w = mount(MediaPreview, { props: { fs, path: "/x/old.mp4", kind: "video" } });
    await flushPromises();
    const oldVideo = w.find("video").element;
    await w.setProps({ path: "/x/new.mp4" });
    await flushPromises();
    expect(w.find("video").element).not.toBe(oldVideo);

    oldVideo.dispatchEvent(new Event("error"));
    await flushPromises();

    expect(w.text()).not.toContain("Inline preview unavailable");
  });
});
