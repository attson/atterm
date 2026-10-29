import { describe, expect, it, vi } from "vitest";
import { searchFileContents } from "./contentSearch";
import type { FileSystemBridge } from "./fsBridge";

function makeFS(): FileSystemBridge {
  const files: Record<string, string> = {
    "/repo/README.md": "hello\nworkspace",
    "/repo/src/app.ts": "export const answer = 42;",
  };
  const dirs: Record<string, Array<{ name: string; isDir: boolean }>> = {
    "/repo": [{ name: "README.md", isDir: false }, { name: "src", isDir: true }],
    "/repo/src": [{ name: "app.ts", isDir: false }],
  };
  return {
    identity: "test",
    listDir: vi.fn(async (path: string) => dirs[path] ?? []),
    fileMeta: vi.fn(async (path: string) => ({ size: files[path]?.length ?? 0, isBinary: false, modTime: 1 })),
    readFile: vi.fn(async (path: string) => ({ data: Array.from(new TextEncoder().encode(files[path] ?? "")) })),
  } as unknown as FileSystemBridge;
}

describe("searchFileContents", () => {
  it("returns matching files with line and preview", async () => {
    const result = await searchFileContents(makeFS(), "/repo", "answer", { showHidden: false });
    expect(result.results).toEqual([{ path: "/repo/src/app.ts", name: "app.ts", line: 1, preview: "export const answer = 42;" }]);
  });

  it("supports case-sensitive and regular-expression matching", async () => {
    const fs = makeFS();
    expect((await searchFileContents(fs, "/repo", "ANSWER", { showHidden: false })).results).toHaveLength(1);
    expect((await searchFileContents(fs, "/repo", "ANSWER", { showHidden: false, caseSensitive: true })).results).toHaveLength(0);
    expect((await searchFileContents(fs, "/repo", "answer\\s*=", { showHidden: false, useRegex: true })).results[0]?.path).toBe("/repo/src/app.ts");
  });
});
