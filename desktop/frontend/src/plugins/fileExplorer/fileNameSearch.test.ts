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
});
