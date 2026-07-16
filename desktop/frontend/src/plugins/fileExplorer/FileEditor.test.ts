import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import FileEditor from "./FileEditor.vue";
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

function mountFE(path: string, meta: { size: number; isBinary: boolean }) {
  (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
    path, size: meta.size, modTime: 1, isBinary: meta.isBinary,
  });
  return mount(FileEditor, {
    props: { fs, path, showLineNumbers: false, theme: "dimmed", viewMode: "code" },
    global: {
      stubs: {
        CodeViewer: { template: '<div data-test="kind-code" />' },
        ImagePreview: { template: '<div data-test="kind-image" />' },
        MediaPreview: { template: '<div data-test="kind-media" />' },
        PdfPreview: { template: '<div data-test="kind-pdf" />' },
        MarkdownPreview: { template: '<div data-test="kind-markdown" />' },
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

  it("routes .md to MarkdownPreview when viewMode=render", async () => {
    (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      path: "/x/README.md", size: 500, modTime: 1, isBinary: false,
    });
    const w = mount(FileEditor, {
      props: { fs, path: "/x/README.md", showLineNumbers: false, theme: "dimmed", viewMode: "render" },
      global: {
        stubs: {
          CodeViewer: { template: '<div data-test="kind-code" />' },
          ImagePreview: { template: '<div data-test="kind-image" />' },
          MediaPreview: { template: '<div data-test="kind-media" />' },
          PdfPreview: { template: '<div data-test="kind-pdf" />' },
          MarkdownPreview: { template: '<div data-test="kind-markdown" />' },
          BinaryBanner: { template: '<div data-test="kind-banner" />' },
        },
      },
    });
    await flushPromises();
    expect(w.find('[data-test="kind-markdown"]').exists()).toBe(true);
  });

  it("routes .md to CodeViewer when viewMode=code", async () => {
    (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      path: "/x/README.md", size: 500, modTime: 1, isBinary: false,
    });
    const w = mount(FileEditor, {
      props: { fs, path: "/x/README.md", showLineNumbers: false, theme: "dimmed", viewMode: "code" },
      global: {
        stubs: {
          CodeViewer: { template: '<div data-test="kind-code" />' },
          ImagePreview: { template: '<div data-test="kind-image" />' },
          MediaPreview: { template: '<div data-test="kind-media" />' },
          PdfPreview: { template: '<div data-test="kind-pdf" />' },
          MarkdownPreview: { template: '<div data-test="kind-markdown" />' },
          BinaryBanner: { template: '<div data-test="kind-banner" />' },
        },
      },
    });
    await flushPromises();
    expect(w.find('[data-test="kind-code"]').exists()).toBe(true);
  });

  it("routes svg to ImagePreview when viewMode=render", async () => {
    (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      path: "/x/logo.svg", size: 200, modTime: 1, isBinary: false,
    });
    const w = mount(FileEditor, {
      props: { fs, path: "/x/logo.svg", showLineNumbers: false, theme: "dimmed", viewMode: "render" },
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
    await flushPromises();
    expect(w.find('[data-test="kind-image"]').exists()).toBe(true);
  });

  it("shows error banner when fileMeta rejects", async () => {
    (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockRejectedValueOnce(
      new Error("fs broken"),
    );
    const w = mount(FileEditor, {
      props: { fs, path: "/x/main.go", showLineNumbers: false, theme: "dimmed", viewMode: "code" },
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
    await flushPromises();
    expect(w.find(".err").exists()).toBe(true);
    expect(w.text()).toContain("fs broken");
    expect(w.find('[data-test="kind-code"]').exists()).toBe(false);
  });

  it("does not let a stale fileMeta response replace the newer file kind", async () => {
    let resolveFirst!: (value: { path: string; size: number; modTime: number; isBinary: boolean }) => void;
    (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockImplementation((path: string) => {
      if (path === "/x/first.bin") {
        return new Promise((resolve) => { resolveFirst = resolve; });
      }
      return Promise.resolve({ path, size: 10, modTime: 2, isBinary: false });
    });
    const w = mount(FileEditor, {
      props: { fs, path: "/x/first.bin", showLineNumbers: false, theme: "dimmed", viewMode: "code" },
      global: {
        stubs: {
          CodeViewer: { template: '<div data-test="kind-code" />' },
          ImagePreview: { template: '<div data-test="kind-image" />' },
          MediaPreview: { template: '<div data-test="kind-media" />' },
          PdfPreview: { template: '<div data-test="kind-pdf" />' },
          MarkdownPreview: { template: '<div data-test="kind-markdown" />' },
          BinaryBanner: { template: '<div data-test="kind-banner" />' },
        },
      },
    });
    await flushPromises();
    await w.setProps({ path: "/x/second.txt" });
    await flushPromises();
    resolveFirst({ path: "/x/first.bin", size: 10, modTime: 1, isBinary: true });
    await flushPromises();

    expect(w.find('[data-test="kind-code"]').exists()).toBe(true);
    expect(w.find('[data-test="kind-banner"]').exists()).toBe(false);
  });
});
