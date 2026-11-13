import { afterEach, describe, expect, it, vi } from "vitest";
import { buildHistoryClipboardHTML, parseHistoryClipboardHTML, copyHistoryToClipboard } from "./historyClipboard";

describe("history clipboard", () => {
  afterEach(() => { vi.unstubAllGlobals(); });

  it("writes browser HTML and plain-text representations during the initiating gesture", async () => {
    const representations: Record<string, Blob | Promise<Blob>>[] = [];
    vi.stubGlobal("ClipboardItem", class {
      constructor(data: Record<string, Blob | Promise<Blob>>) { representations.push(data); }
    });
    const write = vi.fn(async () => { await representations[0]["text/html"]; });
    vi.stubGlobal("navigator", { clipboard: { write } });
    const copying = copyHistoryToClipboard("see [Image #1]", () => ({ file: new Blob(["png"], { type: "image/png" }), name: "one.png" }));
    expect(write).toHaveBeenCalledTimes(1);
    await expect(copying).resolves.toBe(true);
    const reader = new FileReader();
    const html = new Promise<string>((resolve) => { reader.onload = () => resolve(String(reader.result)); });
    reader.readAsText(await representations[0]["text/html"]);
    expect(await html).toContain('data:image/png;base64,cG5n');
    expect(representations[0]["text/plain"]).toBeInstanceOf(Blob);
  });
  it("preserves text, image order, and original bytes in rich HTML without interpreting input as markup", async () => {
    const html = await buildHistoryClipboardHTML('before <script>\n[Image #1] after', () => ({
      file: new Blob(["png-bytes"], { type: "image/png" }), name: 'image".png',
    }));
    expect(html).not.toContain("<script>");
    const parts = parseHistoryClipboardHTML(html)!;
    expect(parts).toHaveLength(3);
    expect(parts[0]).toEqual({ kind: "text", text: "before <script>\n" });
    expect(parts[1].kind).toBe("image");
    if (parts[1].kind === "image") {
      expect(parts[1].name).toBe('image".png');
      const reader = new FileReader();
      const bytes = new Promise<string>((resolve) => { reader.onload = () => resolve(String(reader.result)); });
      reader.readAsText(parts[1].file);
      expect(await bytes).toBe("png-bytes");
    }
    expect(parts[2]).toEqual({ kind: "text", text: " after" });
  });

  it("rejects missing original images without writing a misleading partial copy", async () => {
    const writeText = vi.fn();
    const writeHTML = vi.fn();
    await expect(copyHistoryToClipboard('[Image #1] missing', () => null, { writeText, writeHTML }))
      .rejects.toThrow("history_images_unavailable");
    expect(writeText).not.toHaveBeenCalled();
    expect(writeHTML).not.toHaveBeenCalled();
  });

  it("writes both plain text and rich HTML through the native clipboard bridge", async () => {
    const writeHTML = vi.fn().mockResolvedValue(undefined);
    const writeText = vi.fn();
    await expect(copyHistoryToClipboard('look [Image #1]', () => ({
      file: new Blob(["png"], { type: "image/png" }), name: "one.png",
    }), { writeText, writeHTML })).resolves.toBe(true);
    expect(writeHTML).toHaveBeenCalledWith('look [Image #1]', expect.stringContaining('data:image/png;base64,cG5n'));
    expect(writeText).not.toHaveBeenCalled();
  });

  it("keeps plain text copies on the existing clipboard path", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    const writeHTML = vi.fn();
    await expect(copyHistoryToClipboard("plain\ntext", () => null, { writeText, writeHTML })).resolves.toBe(true);
    expect(writeText).toHaveBeenCalledWith("plain\ntext");
    expect(writeHTML).not.toHaveBeenCalled();
  });

  it("ignores unrelated HTML and rejects remote image sources and unsupported markup", () => {
    expect(parseHistoryClipboardHTML('<p>ordinary paste</p>')).toBeNull();
    expect(() => parseHistoryClipboardHTML('<div data-atterm-input-history="1"><img src="https://example.com/image.png"></div>')).toThrow();
    expect(() => parseHistoryClipboardHTML('<div data-atterm-input-history="1"><script>bad</script></div>')).toThrow();
  });
});
