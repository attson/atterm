import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import FileEditor from "./FileEditor.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";

let platform: ReturnType<typeof createFakePlatform>;

beforeEach(() => {
  vi.clearAllMocks();
  platform = createFakePlatform();
  __setPlatformForTests(platform);
});
afterEach(() => __setPlatformForTests(null));

function mountFE(path: string, meta: { size: number; isBinary: boolean }) {
  (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
    path, size: meta.size, modTime: 1, isBinary: meta.isBinary,
  });
  return mount(FileEditor, {
    props: { path, showLineNumbers: false, theme: "dimmed", viewMode: "code" },
    global: {
      stubs: {
        CodeViewer: { template: '<div data-test="kind-code" />' },
        ImagePreview: { template: '<div data-test="kind-image" />' },
        MediaPreview: { template: '<div data-test="kind-media" />' },
        PdfPreview: { template: '<div data-test="kind-pdf" />' },
        BinaryBanner: { template: '<div data-test="kind-banner" />' },
      },
    },
  });
}

describe("FileEditor (dispatcher)", () => {
  it("routes .png to ImagePreview", async () => {
    const w = mountFE("/x/photo.png", { size: 1000, isBinary: true });
    await flushPromises();
    expect(w.find('[data-test="kind-image"]').exists()).toBe(true);
  });

  it("routes .mp4 to MediaPreview", async () => {
    const w = mountFE("/x/clip.mp4", { size: 100_000, isBinary: true });
    await flushPromises();
    expect(w.find('[data-test="kind-media"]').exists()).toBe(true);
  });

  it("routes .mp3 to MediaPreview", async () => {
    const w = mountFE("/x/track.mp3", { size: 100_000, isBinary: true });
    await flushPromises();
    expect(w.find('[data-test="kind-media"]').exists()).toBe(true);
  });

  it("routes .pdf to PdfPreview", async () => {
    const w = mountFE("/x/doc.pdf", { size: 200_000, isBinary: true });
    await flushPromises();
    expect(w.find('[data-test="kind-pdf"]').exists()).toBe(true);
  });

  it("routes .go to CodeViewer", async () => {
    const w = mountFE("/x/main.go", { size: 500, isBinary: false });
    await flushPromises();
    expect(w.find('[data-test="kind-code"]').exists()).toBe(true);
  });

  it("routes unknown-binary to BinaryBanner", async () => {
    const w = mountFE("/x/blob.dat", { size: 200, isBinary: true });
    await flushPromises();
    expect(w.find('[data-test="kind-banner"]').exists()).toBe(true);
  });

  it("routes svg to CodeViewer when viewMode=code", async () => {
    const w = mountFE("/x/logo.svg", { size: 200, isBinary: false });
    await flushPromises();
    expect(w.find('[data-test="kind-code"]').exists()).toBe(true);
  });
});
