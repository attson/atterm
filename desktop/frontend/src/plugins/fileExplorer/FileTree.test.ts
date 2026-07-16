import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import FileTree from "./FileTree.vue";
import { __setPlatformForTests } from '../../platform';
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform';
import { createLocalFSBridge, type FileSystemBridge } from "./fsBridge";

let platform: ReturnType<typeof createFakePlatform>;
let fs: FileSystemBridge;

beforeEach(() => {
  vi.clearAllMocks();
  platform = createFakePlatform();
  __setPlatformForTests(platform);
  fs = createLocalFSBridge(platform.pluginHost!, platform.events);
  (platform.pluginHost!.fs.listDir as ReturnType<typeof vi.fn>).mockImplementation(async (path: string) => {
    if (path === "/proj") {
      return [
        { name: "src", isDir: true },
        { name: ".git", isDir: true },
        { name: "README.md", isDir: false, size: 100 },
      ];
    }
    return [];
  });
});

afterEach(() => {
  __setPlatformForTests(null);
});

describe("FileTree", () => {
  it("lists root entries on mount; filters hidden by default", async () => {
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    const items = w.findAll(".node-name").map((n) => n.text());
    expect(items).toContain("src");
    expect(items).toContain("README.md");
    expect(items).not.toContain(".git");
  });

  it("includes hidden entries when showHidden=true", async () => {
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: true } });
    await flushPromises();
    const items = w.findAll(".node-name").map((n) => n.text());
    expect(items).toContain(".git");
  });

  it("clicking a file emits file-clicked", async () => {
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    await w.findAll(".node[data-type=file]")[0].trigger("click");
    expect(w.emitted("file-clicked")).toBeTruthy();
  });

  it("clicking a file twice rapidly emits file-double-clicked", async () => {
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    const node = w.findAll(".node[data-type=file]")[0];
    await node.trigger("dblclick");
    expect(w.emitted("file-double-clicked")).toBeTruthy();
  });
});
