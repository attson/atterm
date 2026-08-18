import { describe, expect, it, vi } from "vitest";
import { searchFileNames } from "./fileNameSearch";
import type { FileSystemBridge } from "./fsBridge";

function fsWithTree(tree: Record<string, Array<{ name: string; isDir: boolean }>>): FileSystemBridge {
  return {
    identity: "test",
    listDir: vi.fn(async (path: string) => tree[path] ?? []),
    watchDir: vi.fn(),
    unwatchDir: vi.fn(),
    readFile: vi.fn(),
    fileMeta: vi.fn(),
    openExternal: vi.fn(),
    assetUrlFor: vi.fn(),
    onDirChanged: vi.fn(() => () => {}),
    writeFile: vi.fn(),
    createFile: vi.fn(),
    rename: vi.fn(),
    remove: vi.fn(),
    mkdir: vi.fn(),
    trash: vi.fn(),
  } as unknown as FileSystemBridge;
}

describe("searchFileNames", () => {
  it("recursively finds matching file names and skips heavyweight directories", async () => {
    const fs = fsWithTree({
      "/repo": [
        { name: "src", isDir: true },
        { name: "node_modules", isDir: true },
        { name: ".git", isDir: true },
        { name: "dist", isDir: true },
        { name: "README.md", isDir: false },
      ],
      "/repo/src": [
        { name: "feature-search.ts", isDir: false },
        { name: "nested", isDir: true },
      ],
      "/repo/src/nested": [
        { name: "SearchPanel.vue", isDir: false },
      ],
      "/repo/node_modules": [
        { name: "search-package.js", isDir: false },
      ],
      "/repo/.git": [
        { name: "search-index", isDir: false },
      ],
      "/repo/dist": [
        { name: "search-bundle.js", isDir: false },
      ],
    });

    const result = await searchFileNames(fs, "/repo", "search", { showHidden: false });

    expect(result.results.map((r) => r.path)).toEqual([
      "/repo/src/feature-search.ts",
      "/repo/src/nested/SearchPanel.vue",
    ]);
    expect(fs.listDir).not.toHaveBeenCalledWith("/repo/node_modules");
    expect(fs.listDir).not.toHaveBeenCalledWith("/repo/.git");
    expect(fs.listDir).not.toHaveBeenCalledWith("/repo/dist");
  });

  it("stops after maxResults matches", async () => {
    const fs = fsWithTree({
      "/repo": [
        { name: "one.txt", isDir: false },
        { name: "two.txt", isDir: false },
        { name: "three.txt", isDir: false },
      ],
    });

    const result = await searchFileNames(fs, "/repo", ".txt", { showHidden: true, maxResults: 2 });

    expect(result.results.map((r) => r.name)).toEqual(["one.txt", "two.txt"]);
    expect(result.truncated).toBe(true);
  });

  it("skips unreadable directories and keeps searching other branches", async () => {
    const fs = fsWithTree({
      "/repo": [
        { name: "blocked", isDir: true },
        { name: "src", isDir: true },
      ],
      "/repo/src": [
        { name: "search-result.ts", isDir: false },
      ],
    });
    (fs.listDir as ReturnType<typeof vi.fn>).mockImplementation(async (path: string) => {
      if (path === "/repo/blocked") throw new Error("permission denied");
      return {
        "/repo": [
          { name: "blocked", isDir: true },
          { name: "src", isDir: true },
        ],
        "/repo/src": [
          { name: "search-result.ts", isDir: false },
        ],
      }[path] ?? [];
    });

    const result = await searchFileNames(fs, "/repo", "search", { showHidden: false });

    expect(result.results.map((r) => r.path)).toEqual(["/repo/src/search-result.ts"]);
  });

  it("stops issuing listings the moment the walk is cancelled", async () => {
    // The caller discarding a superseded result does not stop the traffic. On
    // a source where one listing is one SSH round trip, a search abandoned two
    // keystrokes ago must stop dialling, not merely stop being read.
    const fs = fsWithTree({
      "/repo": [
        { name: "search-root.ts", isDir: false },
        { name: "a", isDir: true },
        { name: "b", isDir: true },
      ],
      "/repo/a": [{ name: "search-a.ts", isDir: false }],
      "/repo/b": [{ name: "search-b.ts", isDir: false }],
    });
    let cancelled = false;
    const result = await searchFileNames(fs, "/repo", "search", {
      showHidden: false,
      isCancelled: () => {
        // Let the root listing happen, then cancel before any child is walked.
        const now = cancelled;
        cancelled = true;
        return now;
      },
    });

    expect(result.results).toEqual([]);
    expect(fs.listDir).toHaveBeenCalledTimes(1);
    expect(fs.listDir).toHaveBeenCalledWith("/repo");
  });

  it("honours a smaller maxDirs so a costly source is not crawled", async () => {
    const fs = fsWithTree({
      "/": [{ name: "a", isDir: true }, { name: "b", isDir: true }],
      "/a": [{ name: "search-a.ts", isDir: false }],
      "/b": [{ name: "search-b.ts", isDir: false }],
    });

    const result = await searchFileNames(fs, "/", "search", { showHidden: true, maxDirs: 2 });

    expect(result.truncated).toBe(true);
    expect((fs.listDir as ReturnType<typeof vi.fn>).mock.calls.length).toBeLessThanOrEqual(2);
  });

  it("does not descend into the kernel's pseudo-filesystems at the root", async () => {
    // Starting at "/" — which is where an SSH source starts — /proc alone is
    // thousands of directories that hold no file anyone is searching for.
    const fs = fsWithTree({
      "/": [
        { name: "proc", isDir: true },
        { name: "sys", isDir: true },
        { name: "dev", isDir: true },
        { name: "run", isDir: true },
        { name: "srv", isDir: true },
      ],
      "/srv": [{ name: "search-me.conf", isDir: false }],
      "/proc": [{ name: "search-nope", isDir: false }],
    });

    const result = await searchFileNames(fs, "/", "search", { showHidden: true });

    expect(result.results.map((r) => r.path)).toEqual(["/srv/search-me.conf"]);
    for (const skipped of ["/proc", "/sys", "/dev", "/run"]) {
      expect(fs.listDir).not.toHaveBeenCalledWith(skipped);
    }
  });

  it("still searches a project directory that happens to be called dev", async () => {
    // The skip is anchored to the filesystem root, not to the name.
    const fs = fsWithTree({
      "/repo": [{ name: "dev", isDir: true }],
      "/repo/dev": [{ name: "search-tool.sh", isDir: false }],
    });

    const result = await searchFileNames(fs, "/repo", "search", { showHidden: true });

    expect(result.results.map((r) => r.path)).toEqual(["/repo/dev/search-tool.sh"]);
  });
});
