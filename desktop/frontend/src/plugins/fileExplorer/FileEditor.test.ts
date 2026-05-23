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

afterEach(() => {
  __setPlatformForTests(null);
});

describe("FileEditor", () => {
  it("shows placeholder for too-large file (size > 2 MB)", async () => {
    (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      path: "/a.txt", size: 3_000_000, modTime: 1, isBinary: false,
    });
    const w = mount(FileEditor, { props: { path: "/a.txt", showLineNumbers: false, theme: "dimmed" } });
    await flushPromises();
    expect(w.text()).toContain("File too large");
    expect(platform.pluginHost!.fs.readFile).not.toHaveBeenCalled();
  });

  it("shows binary placeholder when isBinary=true", async () => {
    (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      path: "/b.bin", size: 100, modTime: 1, isBinary: true,
    });
    const w = mount(FileEditor, { props: { path: "/b.bin", showLineNumbers: false, theme: "dimmed" } });
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
    const w = mount(FileEditor, { props: { path: "/c.txt", showLineNumbers: false, theme: "dimmed" } });
    await flushPromises();
    expect(platform.pluginHost!.fs.readFile).toHaveBeenCalled();
    expect(w.text()).not.toContain("File too large");
    expect(w.text()).not.toContain("Binary file");
  });
});
