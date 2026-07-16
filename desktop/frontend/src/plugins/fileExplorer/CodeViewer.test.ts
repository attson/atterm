import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import CodeViewer from "./CodeViewer.vue";
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

afterEach(() => {
  __setPlatformForTests(null);
});

describe("CodeViewer", () => {
  it("shows placeholder for too-large file (size > 2 MB)", async () => {
    (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      path: "/a.txt", size: 3_000_000, modTime: 1, isBinary: false,
    });
    const w = mount(CodeViewer, { props: { fs, path: "/a.txt", showLineNumbers: false, theme: "dimmed" } });
    await flushPromises();
    expect(w.text()).toContain("File too large");
    expect(platform.pluginHost!.fs.readFile).not.toHaveBeenCalled();
  });

  it("shows binary placeholder when isBinary=true", async () => {
    (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      path: "/b.bin", size: 100, modTime: 1, isBinary: true,
    });
    const w = mount(CodeViewer, { props: { fs, path: "/b.bin", showLineNumbers: false, theme: "dimmed" } });
    await flushPromises();
    expect(w.text()).toContain("Binary file");
    expect(platform.pluginHost!.fs.readFile).not.toHaveBeenCalled();
  });

  it("loads file content for normal text file", async () => {
    (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      path: "/c.txt", size: 5, modTime: 1, isBinary: false,
    });
    (platform.pluginHost!.fs.readFile as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      path: "/c.txt", data: new TextEncoder().encode("hello"), isBinary: false, truncatedAt: 0,
    });
    const w = mount(CodeViewer, { props: { fs, path: "/c.txt", showLineNumbers: false, theme: "dimmed" } });
    await flushPromises();
    expect(platform.pluginHost!.fs.readFile).toHaveBeenCalled();
    expect(w.text()).not.toContain("File too large");
    expect(w.text()).not.toContain("Binary file");
  });

  it("ignores a delayed fileMeta result for the previous path", async () => {
    let resolveOld!: (value: { path: string; size: number; modTime: number; isBinary: boolean }) => void;
    (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockImplementation((path: string) => {
      if (path === "/old.bin") return new Promise((resolve) => { resolveOld = resolve; });
      return Promise.resolve({ path, size: 3, modTime: 2, isBinary: false });
    });
    (platform.pluginHost!.fs.readFile as ReturnType<typeof vi.fn>).mockResolvedValue({
      path: "/new.txt", data: "bmV3IGNvbnRlbnQ=", isBinary: false, truncatedAt: 0,
    });
    const w = mount(CodeViewer, { props: { fs, path: "/old.bin", showLineNumbers: false, theme: "dimmed" } });
    await flushPromises();
    await w.setProps({ path: "/new.txt" });
    await flushPromises();
    resolveOld({ path: "/old.bin", size: 3, modTime: 1, isBinary: true });
    await flushPromises();

    expect(w.text()).toContain("new content");
    expect(w.text()).not.toContain("Binary file");
  });
});
