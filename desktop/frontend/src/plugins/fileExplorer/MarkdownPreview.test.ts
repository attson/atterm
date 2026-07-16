import { describe, expect, it, vi, beforeEach, beforeAll, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import MarkdownPreview from "./MarkdownPreview.vue";
import { __setPlatformForTests } from "../../platform";
import { createFakePlatform } from "../../platform/__tests__/_fakePlatform";
import { createLocalFSBridge, type FileSystemBridge } from "./fsBridge";

// Warm the markdown-it dynamic-import cache so flushPromises() inside each
// test sees the parser load synchronously. Without this the very first test's
// `await import("markdown-it")` takes one extra resolver tick past what
// flushPromises drains, and the assertion runs while state is still loading.
beforeAll(async () => { await import("markdown-it"); });

let platform: ReturnType<typeof createFakePlatform>;
let fs: FileSystemBridge;
beforeEach(() => {
  vi.clearAllMocks();
  platform = createFakePlatform();
  __setPlatformForTests(platform);
  fs = createLocalFSBridge(platform.pluginHost!, platform.events);
});
afterEach(() => __setPlatformForTests(null));

function mockRead(path: string, source: string, size?: number) {
  (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
    path, size: size ?? source.length, modTime: 1, isBinary: false,
  });
  (platform.pluginHost!.fs.readFile as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
    path, data: new TextEncoder().encode(source), isBinary: false, truncatedAt: 0,
  });
}

describe("MarkdownPreview", () => {
  it("renders headings as <h1> tags", async () => {
    mockRead("/x/a.md", "# Hello world\n\nbody text");
    const w = mount(MarkdownPreview, { props: { fs, path: "/x/a.md", theme: "dimmed" } });
    await flushPromises();
    expect(w.find(".md-host").exists()).toBe(true);
    expect(w.html()).toContain("<h1>Hello world</h1>");
  });

  it("renders fenced code blocks as <pre><code>", async () => {
    mockRead("/x/a.md", "```\nconst x = 1;\n```");
    const w = mount(MarkdownPreview, { props: { fs, path: "/x/a.md", theme: "dimmed" } });
    await flushPromises();
    expect(w.html()).toContain("<pre>");
    expect(w.html()).toContain("const x = 1;");
  });

  it("escapes raw HTML in the source (html: false)", async () => {
    mockRead("/x/a.md", "before <script>alert(1)</script> after");
    const w = mount(MarkdownPreview, { props: { fs, path: "/x/a.md", theme: "dimmed" } });
    await flushPromises();
    // The literal "<script>" must NOT survive into the rendered DOM.
    expect(w.find("script").exists()).toBe(false);
    // The source text is shown escaped, so the literal angle-bracket sequence
    // appears in the rendered HTML as &lt;script&gt;.
    expect(w.html()).toContain("&lt;script&gt;");
  });

  it("falls back to BinaryBanner when fileMeta size > 2 MB", async () => {
    (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      path: "/x/big.md", size: 3 * 1024 * 1024, modTime: 1, isBinary: false,
    });
    const w = mount(MarkdownPreview, { props: { fs, path: "/x/big.md", theme: "dimmed" } });
    await flushPromises();
    expect(w.text()).toContain("Inline preview unavailable");
  });

  it("falls back to BinaryBanner when fileMeta rejects", async () => {
    (platform.pluginHost!.fs.fileMeta as ReturnType<typeof vi.fn>).mockRejectedValueOnce(
      new Error("boom"),
    );
    const w = mount(MarkdownPreview, { props: { fs, path: "/x/a.md", theme: "dimmed" } });
    await flushPromises();
    expect(w.text()).toContain("Inline preview unavailable");
  });

  it("opens http(s) links via system.openExternalURL on click", async () => {
    mockRead("/x/a.md", "[example](https://example.com)");
    const w = mount(MarkdownPreview, { props: { fs, path: "/x/a.md", theme: "dimmed" } });
    await flushPromises();
    const a = w.find("a");
    expect(a.exists()).toBe(true);
    await a.trigger("click");
    expect(platform.system.openExternalURL).toHaveBeenCalledWith("https://example.com");
  });

  it("does not call openExternalURL for relative links", async () => {
    mockRead("/x/a.md", "[local](./foo.md)");
    const w = mount(MarkdownPreview, { props: { fs, path: "/x/a.md", theme: "dimmed" } });
    await flushPromises();
    await w.find("a").trigger("click");
    expect(platform.system.openExternalURL).not.toHaveBeenCalled();
  });
});
