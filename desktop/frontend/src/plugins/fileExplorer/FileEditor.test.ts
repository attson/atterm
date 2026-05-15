import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import FileEditor from "./FileEditor.vue";

vi.mock("../../../wailsjs/go/main/PluginFS", () => ({
  ReadFile: vi.fn(),
  FileMeta: vi.fn(),
}));

import { ReadFile, FileMeta } from "../../../wailsjs/go/main/PluginFS";

beforeEach(() => {
  vi.mocked(FileMeta).mockReset();
  vi.mocked(ReadFile).mockReset();
});

describe("FileEditor", () => {
  it("shows placeholder for too-large file (size > 2 MB)", async () => {
    vi.mocked(FileMeta).mockResolvedValue({ path: "/a.txt", size: 3_000_000, modTime: 1, isBinary: false } as any);
    const w = mount(FileEditor, { props: { path: "/a.txt" } });
    await flushPromises();
    expect(w.text()).toContain("File too large");
    expect(ReadFile).not.toHaveBeenCalled();
  });

  it("shows binary placeholder when isBinary=true", async () => {
    vi.mocked(FileMeta).mockResolvedValue({ path: "/b.bin", size: 100, modTime: 1, isBinary: true } as any);
    const w = mount(FileEditor, { props: { path: "/b.bin" } });
    await flushPromises();
    expect(w.text()).toContain("Binary file");
    expect(ReadFile).not.toHaveBeenCalled();
  });

  it("loads file content for normal text file", async () => {
    vi.mocked(FileMeta).mockResolvedValue({ path: "/c.txt", size: 5, modTime: 1, isBinary: false } as any);
    vi.mocked(ReadFile).mockResolvedValue({ path: "/c.txt", data: new TextEncoder().encode("hello"), isBinary: false, truncatedAt: 0 } as any);
    const w = mount(FileEditor, { props: { path: "/c.txt" } });
    await flushPromises();
    expect(ReadFile).toHaveBeenCalled();
    expect(w.text()).not.toContain("File too large");
    expect(w.text()).not.toContain("Binary file");
  });
});
