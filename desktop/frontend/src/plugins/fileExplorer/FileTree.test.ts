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

function installDirChangeEmitter() {
  const handlers = new Set<(data: unknown) => void>();
  (platform.events.on as ReturnType<typeof vi.fn>).mockImplementation((event: string, handler: (data: unknown) => void) => {
    if (event !== "plugin-fs:dir-changed") return () => {};
    handlers.add(handler);
    return () => handlers.delete(handler);
  });
  return (path: string) => handlers.forEach((handler) => handler(path));
}

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

  it("unwatches a directory when an in-flight watch resolves after unmount", async () => {
    let resolveWatch!: (id: number) => void;
    (platform.pluginHost!.fs.watchDir as ReturnType<typeof vi.fn>).mockImplementationOnce(
      () => new Promise<number>((resolve) => { resolveWatch = resolve; }),
    );
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();

    void w.find('.node[data-type="dir"]').trigger("click");
    await vi.waitFor(() => expect(platform.pluginHost!.fs.watchDir).toHaveBeenCalledTimes(1));
    w.unmount();
    resolveWatch(42);
    await flushPromises();

    expect(platform.pluginHost!.fs.unwatchDir).toHaveBeenCalledWith(42);
  });

  it("unwinds directory watches when the root changes", async () => {
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    void w.find('.node[data-type="dir"]').trigger("click");
    await vi.waitFor(() => expect(platform.pluginHost!.fs.watchDir).toHaveBeenCalledTimes(1));

    await w.setProps({ root: "/other" });
    await flushPromises();

    expect(platform.pluginHost!.fs.unwatchDir).toHaveBeenCalledWith(1);
  });

  it("cancels a pending expansion when the directory is clicked again", async () => {
    let resolveChildren!: (entries: Array<{ name: string; isDir: boolean }>) => void;
    (platform.pluginHost!.fs.listDir as ReturnType<typeof vi.fn>).mockImplementation((path: string) => {
      if (path === "/proj") return Promise.resolve([
        { name: "src", isDir: true },
        { name: "README.md", isDir: false },
      ]);
      return new Promise((resolve) => { resolveChildren = resolve; });
    });
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    const dir = w.find('.node[data-type="dir"]');

    void dir.trigger("click");
    void dir.trigger("click");
    await vi.waitFor(() => expect(resolveChildren).toBeTypeOf("function"));
    resolveChildren([]);
    await flushPromises();
    expect(platform.pluginHost!.fs.watchDir).not.toHaveBeenCalled();
    w.unmount();
  });

  it("unwatches an older pending watch after the directory is collapsed and re-expanded", async () => {
    const resolvers: Array<(id: number) => void> = [];
    (platform.pluginHost!.fs.watchDir as ReturnType<typeof vi.fn>).mockImplementation(
      () => new Promise<number>((resolve) => { resolvers.push(resolve); }),
    );
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    const dir = w.find('.node[data-type="dir"]');

    void dir.trigger("click");
    await vi.waitFor(() => expect(platform.pluginHost!.fs.watchDir).toHaveBeenCalledTimes(1));
    await dir.trigger("click");
    void dir.trigger("click");
    await vi.waitFor(() => expect(platform.pluginHost!.fs.watchDir).toHaveBeenCalledTimes(2));

    resolvers[0](11);
    await flushPromises();
    expect(platform.pluginHost!.fs.unwatchDir).toHaveBeenCalledWith(11);

    resolvers[1](12);
    await flushPromises();
    w.unmount();
    await flushPromises();
    expect(platform.pluginHost!.fs.unwatchDir).toHaveBeenCalledWith(12);
  });

  it("unwatches expanded descendants removed by a parent refresh", async () => {
    const emitDirChanged = installDirChangeEmitter();
    let parentReads = 0;
    (platform.pluginHost!.fs.listDir as ReturnType<typeof vi.fn>).mockImplementation((path: string) => {
      if (path === "/proj") return Promise.resolve([{ name: "parent", isDir: true }]);
      if (path === "/proj/parent") {
        parentReads++;
        return Promise.resolve(parentReads === 1
          ? [{ name: "child", isDir: true }]
          : [{ name: "replacement.txt", isDir: false }]);
      }
      return Promise.resolve([]);
    });
    (platform.pluginHost!.fs.watchDir as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce(10)
      .mockResolvedValueOnce(20);
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    await w.find('.node[title="/proj/parent"]').trigger("click");
    await flushPromises();
    await w.find('.node[title="/proj/parent/child"]').trigger("click");
    await vi.waitFor(() => expect(platform.pluginHost!.fs.watchDir).toHaveBeenCalledTimes(2));

    emitDirChanged("/proj/parent");
    await flushPromises();

    expect(platform.pluginHost!.fs.unwatchDir).toHaveBeenCalledWith(20);
    expect(w.text()).toContain("replacement.txt");
  });

  it("keeps the latest parent refresh when directory reads resolve out of order", async () => {
    const emitDirChanged = installDirChangeEmitter();
    const refreshResolvers: Array<(entries: Array<{ name: string; isDir: boolean }>) => void> = [];
    let parentReads = 0;
    (platform.pluginHost!.fs.listDir as ReturnType<typeof vi.fn>).mockImplementation((path: string) => {
      if (path === "/proj") return Promise.resolve([{ name: "parent", isDir: true }]);
      if (path === "/proj/parent") {
        parentReads++;
        if (parentReads === 1) return Promise.resolve([]);
        return new Promise((resolve) => { refreshResolvers.push(resolve); });
      }
      return Promise.resolve([]);
    });
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    await w.find('.node[title="/proj/parent"]').trigger("click");
    await flushPromises();

    emitDirChanged("/proj/parent");
    emitDirChanged("/proj/parent");
    await vi.waitFor(() => expect(refreshResolvers).toHaveLength(2));
    refreshResolvers[1]([{ name: "new.txt", isDir: false }]);
    await flushPromises();
    refreshResolvers[0]([{ name: "old.txt", isDir: false }]);
    await flushPromises();

    expect(w.text()).toContain("new.txt");
    expect(w.text()).not.toContain("old.txt");
  });

  it("does not let a removed node's pending watch replace a recreated node's live watch", async () => {
    const emitDirChanged = installDirChangeEmitter();
    let parentReads = 0;
    const childWatchResolvers: Array<(id: number) => void> = [];
    (platform.pluginHost!.fs.listDir as ReturnType<typeof vi.fn>).mockImplementation((path: string) => {
      if (path === "/proj") return Promise.resolve([{ name: "parent", isDir: true }]);
      if (path === "/proj/parent") {
        parentReads++;
        return Promise.resolve([{ name: "child", isDir: true }]);
      }
      return Promise.resolve([]);
    });
    (platform.pluginHost!.fs.watchDir as ReturnType<typeof vi.fn>).mockImplementation((path: string) => {
      if (path === "/proj/parent") return Promise.resolve(10);
      return new Promise<number>((resolve) => { childWatchResolvers.push(resolve); });
    });
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    await w.find('.node[title="/proj/parent"]').trigger("click");
    await flushPromises();
    void w.find('.node[title="/proj/parent/child"]').trigger("click");
    await vi.waitFor(() => expect(childWatchResolvers).toHaveLength(1));

    emitDirChanged("/proj/parent");
    await flushPromises();
    void w.find('.node[title="/proj/parent/child"]').trigger("click");
    await vi.waitFor(() => expect(childWatchResolvers).toHaveLength(2));
    childWatchResolvers[1](22);
    await flushPromises();
    childWatchResolvers[0](11);
    await flushPromises();

    expect(platform.pluginHost!.fs.unwatchDir).toHaveBeenCalledWith(11);
    expect(platform.pluginHost!.fs.unwatchDir).not.toHaveBeenCalledWith(22);
    w.unmount();
    await flushPromises();
    expect(platform.pluginHost!.fs.unwatchDir).toHaveBeenCalledWith(22);
  });

  it("releases expanded descendants when a parent is collapsed", async () => {
    const emitDirChanged = installDirChangeEmitter();
    const listDir = platform.pluginHost!.fs.listDir as ReturnType<typeof vi.fn>;
    listDir.mockImplementation((path: string) => {
      if (path === "/proj") return Promise.resolve([{ name: "parent", isDir: true }]);
      if (path === "/proj/parent") return Promise.resolve([{ name: "child", isDir: true }]);
      return Promise.resolve([]);
    });
    (platform.pluginHost!.fs.watchDir as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce(10)
      .mockResolvedValueOnce(20);
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    const parent = w.find('.node[title="/proj/parent"]');
    await parent.trigger("click");
    await flushPromises();
    await w.find('.node[title="/proj/parent/child"]').trigger("click");
    await vi.waitFor(() => expect(platform.pluginHost!.fs.watchDir).toHaveBeenCalledTimes(2));
    const childReadsBeforeCollapse = listDir.mock.calls.filter(([path]) => path === "/proj/parent/child").length;

    await parent.trigger("click");
    await flushPromises();
    emitDirChanged("/proj/parent/child");
    await flushPromises();

    expect(platform.pluginHost!.fs.unwatchDir).toHaveBeenCalledWith(10);
    expect(platform.pluginHost!.fs.unwatchDir).toHaveBeenCalledWith(20);
    expect(listDir.mock.calls.filter(([path]) => path === "/proj/parent/child")).toHaveLength(childReadsBeforeCollapse);
  });
});

describe("FileTree context menu + CRUD", () => {
  it("right-click on a file opens the context menu with four items", async () => {
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    await w.find('.node[title="/proj/README.md"]').trigger("contextmenu");
    await flushPromises();
    expect(w.find('[data-test="file-tree-menu"]').exists()).toBe(true);
    expect(w.find('[data-test="menu-new-file"]').exists()).toBe(true);
    expect(w.find('[data-test="menu-new-folder"]').exists()).toBe(true);
    expect(w.find('[data-test="menu-rename"]').exists()).toBe(true);
    expect(w.find('[data-test="menu-delete"]').exists()).toBe(true);
  });

  it("clicking Delete without Shift dispatches fs.trash", async () => {
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    await w.find('.node[title="/proj/README.md"]').trigger("contextmenu");
    await flushPromises();
    await w.find('[data-test="menu-delete"]').trigger("click");
    await flushPromises();
    await w.find('[data-test="btn-trash"]').trigger("click");
    await flushPromises();
    expect(platform.pluginHost!.fs.trash).toHaveBeenCalledWith("/proj/README.md");
    expect(platform.pluginHost!.fs.remove).not.toHaveBeenCalled();
  });

  it("shift-right-click delete dispatches fs.remove", async () => {
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    await w.find('.node[title="/proj/README.md"]').trigger("contextmenu", { shiftKey: true });
    await flushPromises();
    await w.find('[data-test="menu-delete"]').trigger("click");
    await flushPromises();
    await w.find('[data-test="btn-hard"]').trigger("click");
    await flushPromises();
    expect(platform.pluginHost!.fs.remove).toHaveBeenCalledWith("/proj/README.md", false);
    expect(platform.pluginHost!.fs.trash).not.toHaveBeenCalled();
  });

  it("New File on a directory node inserts an inline row and creates on submit", async () => {
    (platform.pluginHost!.fs.watchDir as ReturnType<typeof vi.fn>).mockResolvedValueOnce(10);
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    // expand /proj/src
    await w.find('.node[title="/proj/src"]').trigger("click");
    await flushPromises();
    await w.find('.node[title="/proj/src"]').trigger("contextmenu");
    await flushPromises();
    await w.find('[data-test="menu-new-file"]').trigger("click");
    await flushPromises();
    const inputs = w.findAll('[data-test="inline-edit-input"]');
    expect(inputs.length).toBeGreaterThan(0);
    const input = inputs[inputs.length - 1];
    await input.setValue("new.txt");
    await input.trigger("keydown", { key: "Enter" });
    await flushPromises();
    expect(platform.pluginHost!.fs.createFile).toHaveBeenCalledWith("/proj/src/new.txt");
  });

  it("Rename replaces the row with an inline editor and calls rename", async () => {
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    await w.find('.node[title="/proj/README.md"]').trigger("contextmenu");
    await flushPromises();
    await w.find('[data-test="menu-rename"]').trigger("click");
    await flushPromises();
    const input = w.find('[data-test="inline-edit-input"]');
    await input.setValue("READ.md");
    await input.trigger("keydown", { key: "Enter" });
    await flushPromises();
    expect(platform.pluginHost!.fs.rename).toHaveBeenCalledWith("/proj/README.md", "/proj/READ.md");
  });

  it("trash unavailable → re-prompts with hard-delete confirmation", async () => {
    (platform.pluginHost!.fs.trash as ReturnType<typeof vi.fn>).mockRejectedValueOnce(
      new Error("trash: no platform trash command available"),
    );
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    await w.find('.node[title="/proj/README.md"]').trigger("contextmenu");
    await flushPromises();
    await w.find('[data-test="menu-delete"]').trigger("click");
    await flushPromises();
    await w.find('[data-test="btn-trash"]').trigger("click");
    await flushPromises();
    // hard-delete confirmation now open
    expect(w.find('[data-test="btn-hard"]').exists()).toBe(true);
  });
});
