import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import CodeEditor from "./CodeEditor.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";
import { createLocalFSBridge, type FileSystemBridge } from "./fsBridge";
import codeEditorSource from "./CodeEditor.vue?raw";

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

function stubCM() {
  const meta = { path: "/x", size: 3, modTime: 100, isBinary: false };
  (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockResolvedValue(meta);
  (platform.pluginHost!.fs.readFile as ReturnType<typeof vi.fn>).mockResolvedValue({
    path: "/x", data: "aGkK", isBinary: false, truncatedAt: 0,
  });
  (platform.pluginHost!.fs.writeFile as ReturnType<typeof vi.fn>).mockResolvedValue({
    path: "/x", size: 4, modTime: 200, isBinary: false,
  });
}

describe("CodeEditor", () => {
  it("wires CodeMirror search so Mod-F opens editor-local search", () => {
    expect(codeEditorSource).toContain("@codemirror/search");
    expect(codeEditorSource).toContain("search(");
    expect(codeEditorSource).toContain("searchKeymap");
  });

  it("shows placeholder for too-large file (size > 2 MB)", async () => {
    (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      path: "/a.txt", size: 3_000_000, modTime: 1, isBinary: false,
    });
    const w = mount(CodeEditor, { props: { fs, path: "/a.txt", showLineNumbers: false, theme: "dimmed" } });
    await flushPromises();
    expect(w.text()).toContain("File too large");
    expect(platform.pluginHost!.fs.readFile).not.toHaveBeenCalled();
  });

  it("shows binary placeholder when isBinary=true", async () => {
    (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      path: "/b.bin", size: 100, modTime: 1, isBinary: true,
    });
    const w = mount(CodeEditor, { props: { fs, path: "/b.bin", showLineNumbers: false, theme: "dimmed" } });
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
    const w = mount(CodeEditor, { props: { fs, path: "/c.txt", showLineNumbers: false, theme: "dimmed" } });
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
    const w = mount(CodeEditor, { props: { fs, path: "/old.bin", showLineNumbers: false, theme: "dimmed" } });
    await flushPromises();
    await w.setProps({ path: "/new.txt" });
    await flushPromises();
    resolveOld({ path: "/old.bin", size: 3, modTime: 1, isBinary: true });
    await flushPromises();

    expect(w.text()).toContain("new content");
    expect(w.text()).not.toContain("Binary file");
  });

  it("ignores a delayed readFile result for the previous path", async () => {
    let resolveOld!: (value: { path: string; data: string; isBinary: boolean; truncatedAt: number }) => void;
    (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockImplementation((path: string) =>
      Promise.resolve({ path, size: 10, modTime: 1, isBinary: false }),
    );
    (platform.pluginHost!.fs.readFile as ReturnType<typeof vi.fn>).mockImplementation((path: string) => {
      if (path === "/old.txt") return new Promise((resolve) => { resolveOld = resolve; });
      return Promise.resolve({ path, data: "bmV3IGNvbnRlbnQ=", isBinary: false, truncatedAt: 0 });
    });
    const w = mount(CodeEditor, { props: { fs, path: "/old.txt", showLineNumbers: false, theme: "dimmed" } });
    await vi.waitFor(() => expect(platform.pluginHost!.fs.readFile).toHaveBeenCalledWith("/old.txt", 2 * 1024 * 1024));
    await w.setProps({ path: "/new.txt" });
    await flushPromises();
    resolveOld({ path: "/old.txt", data: "b2xkIGNvbnRlbnQ=", isBinary: false, truncatedAt: 0 });
    await flushPromises();

    expect(w.text()).toContain("new content");
    expect(w.text()).not.toContain("old content");
  });

  it("emits dirty-change=true on doc edit and false on save", async () => {
    stubCM();
    const w = mount(CodeEditor, { props: { fs, path: "/x", showLineNumbers: false, theme: "dimmed" } });
    await flushPromises();
    (w.vm as unknown as { testAppend: (s: string) => void }).testAppend("y");
    await flushPromises();
    const dirtyEvents = w.emitted("dirty-change") ?? [];
    expect(dirtyEvents.some(([v]) => v === true)).toBe(true);
    const ok = await (w.vm as unknown as { save: () => Promise<boolean> }).save();
    expect(ok).toBe(true);
    expect(platform.pluginHost!.fs.writeFile).toHaveBeenCalledWith(
      "/x",
      expect.any(Array),
      100,
      false,
    );
    const finalDirty = (w.emitted("dirty-change") ?? []).slice(-1)[0]?.[0];
    expect(finalDirty).toBe(false);
  });

  it("shows stale_modtime conflict banner and allows overwrite", async () => {
    stubCM();
    (platform.pluginHost!.fs.writeFile as ReturnType<typeof vi.fn>)
      .mockRejectedValueOnce(new Error("stale_modtime: current=250"))
      .mockResolvedValueOnce({ path: "/x", size: 4, modTime: 250, isBinary: false });
    const w = mount(CodeEditor, { props: { fs, path: "/x", showLineNumbers: false, theme: "dimmed" } });
    await flushPromises();
    (w.vm as unknown as { testAppend: (s: string) => void }).testAppend("y");
    const first = await (w.vm as unknown as { save: () => Promise<boolean> }).save();
    expect(first).toBe(false);
    expect(w.find('[data-test="conflict-banner"]').exists()).toBe(true);
    await w.find('[data-test="conflict-overwrite"]').trigger("click");
    await flushPromises();
    // second call replays with new expected_modtime = 250
    expect(platform.pluginHost!.fs.writeFile).toHaveBeenLastCalledWith(
      "/x",
      expect.any(Array),
      250,
      false,
    );
  });

  it("shows saveError banner for non-conflict write failures", async () => {
    stubCM();
    (platform.pluginHost!.fs.writeFile as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new Error("write_denied: EACCES"));
    const w = mount(CodeEditor, { props: { fs, path: "/x", showLineNumbers: false, theme: "dimmed" } });
    await flushPromises();
    (w.vm as unknown as { testAppend: (s: string) => void }).testAppend("y");
    const ok = await (w.vm as unknown as { save: () => Promise<boolean> }).save();
    expect(ok).toBe(false);
    expect(w.find('[data-test="save-error"]').exists()).toBe(true);
  });
});
