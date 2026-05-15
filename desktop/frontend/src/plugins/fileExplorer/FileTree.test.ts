import { describe, expect, it, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import FileTree from "./FileTree.vue";

vi.mock("../../../wailsjs/go/main/PluginFS", () => ({
  ListDir: vi.fn(),
  WatchDir: vi.fn(() => Promise.resolve(1)),
  UnwatchDir: vi.fn(() => Promise.resolve()),
}));

vi.mock("../../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => () => undefined),
}));

import { ListDir } from "../../../wailsjs/go/main/PluginFS";

beforeEach(() => {
  vi.mocked(ListDir).mockImplementation(async (path: string) => {
    if (path === "/proj") {
      return [
        { name: "src", isDir: true },
        { name: ".git", isDir: true },
        { name: "README.md", isDir: false, size: 100 },
      ] as any;
    }
    return [] as any;
  });
});

describe("FileTree", () => {
  it("lists root entries on mount; filters hidden by default", async () => {
    const w = mount(FileTree, { props: { root: "/proj", showHidden: false } });
    await flushPromises();
    const items = w.findAll(".node-name").map((n) => n.text());
    expect(items).toContain("src");
    expect(items).toContain("README.md");
    expect(items).not.toContain(".git");
  });

  it("includes hidden entries when showHidden=true", async () => {
    const w = mount(FileTree, { props: { root: "/proj", showHidden: true } });
    await flushPromises();
    const items = w.findAll(".node-name").map((n) => n.text());
    expect(items).toContain(".git");
  });

  it("clicking a file emits file-clicked", async () => {
    const w = mount(FileTree, { props: { root: "/proj", showHidden: false } });
    await flushPromises();
    await w.findAll(".node[data-type=file]")[0].trigger("click");
    expect(w.emitted("file-clicked")).toBeTruthy();
  });

  it("clicking a file twice rapidly emits file-double-clicked", async () => {
    const w = mount(FileTree, { props: { root: "/proj", showHidden: false } });
    await flushPromises();
    const node = w.findAll(".node[data-type=file]")[0];
    await node.trigger("dblclick");
    expect(w.emitted("file-double-clicked")).toBeTruthy();
  });
});
