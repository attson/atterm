import { detectImagePlaceholders, type SessionHistoryImagePreview } from "../composables/useTerminalImagePreview";
import { copyTextToClipboard, type ClipboardWriter } from "./terminalCopy";

const MAX_HTML_BYTES = 24 * 1024 * 1024;
const MAX_IMAGE_BYTES = 10 * 1024 * 1024;
const MAX_IMAGES = 32;
export type HistoryClipboardPart = { kind: "text"; text: string } | { kind: "image"; file: Blob; name: string };
type ResolveImage = (occurrence: number) => SessionHistoryImagePreview | null;

function escapeHTML(text: string): string {
  return text.replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char]!);
}

function dataURL(file: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(new Error("history_image_read_failed"));
    reader.readAsDataURL(file);
  });
}

export async function buildHistoryClipboardHTML(text: string, resolveImage: ResolveImage): Promise<string> {
  const matches = detectImagePlaceholders(text);
  if (matches.length > MAX_IMAGES) throw new Error("history_clipboard_too_large");
  // Resolve every image before awaiting so eviction cannot produce a partial copy.
  const images = matches.map((_, occurrence) => {
    const image = resolveImage(occurrence);
    if (!image) throw new Error("history_images_unavailable");
    if (image.file.size > MAX_IMAGE_BYTES || !/^image\/(png|jpeg|gif|webp)$/.test(image.file.type)) {
      throw new Error("history_image_unsupported");
    }
    return image;
  });
  if (images.reduce((size, image) => size + image.file.size, 0) * 4 / 3 + text.length * 6 > MAX_HTML_BYTES) {
    throw new Error("history_clipboard_too_large");
  }
  const urls = await Promise.all(images.map((image) => dataURL(image.file)));
  let cursor = 0;
  let body = "";
  matches.forEach((match, occurrence) => {
    body += `<span>${escapeHTML(text.slice(cursor, match.start))}</span>`;
    body += `<img src="${urls[occurrence]}" alt="${escapeHTML(images[occurrence].name)}">`;
    cursor = match.end;
  });
  body += `<span>${escapeHTML(text.slice(cursor))}</span>`;
  return `<div data-atterm-input-history="1" style="white-space: pre-wrap">${body}</div>`;
}

export async function copyHistoryToClipboard(text: string, resolveImage: ResolveImage, clipboard?: ClipboardWriter): Promise<boolean> {
  if (!detectImagePlaceholders(text).length) return copyTextToClipboard(text, clipboard);
  const html = buildHistoryClipboardHTML(text, resolveImage);
  // A clipboard permission rejection may occur before the browser reads the
  // promised HTML representation; keep that representation rejection handled.
  void html.catch(() => {});
  if (clipboard?.writeHTML) {
    await clipboard.writeHTML(text, await html);
    return true;
  }
  if (typeof ClipboardItem === "undefined" || !navigator.clipboard?.write) {
    await html;
    throw new Error("history_rich_clipboard_unavailable");
  }
  // Safari requires write() during the click gesture; encoding runs inside
  // the promised ClipboardItem representation rather than before write().
  await navigator.clipboard.write([new ClipboardItem({
    "text/plain": new Blob([text], { type: "text/plain" }),
    "text/html": html.then((value) => new Blob([value], { type: "text/html" })),
  })]);
  return true;
}

export function parseHistoryClipboardHTML(html: string): HistoryClipboardPart[] | null {
  if (!html.includes('data-atterm-input-history')) return null;
  if (html.length > MAX_HTML_BYTES) throw new Error("history_clipboard_too_large");
  const document = new DOMParser().parseFromString(html, "text/html");
  const root = document.querySelector('[data-atterm-input-history="1"]');
  if (!root) return null;
  const parts: HistoryClipboardPart[] = [];
  let imageCount = 0;
  for (const node of Array.from(root.childNodes)) {
    if (node.nodeType === Node.TEXT_NODE) {
      parts.push({ kind: "text", text: node.textContent ?? "" });
    } else if (node instanceof HTMLElement && node.tagName === "SPAN" && !node.children.length) {
      parts.push({ kind: "text", text: node.textContent ?? "" });
    } else if (node instanceof HTMLElement && node.tagName === "IMG") {
      const match = /^data:(image\/(?:png|jpeg|gif|webp));base64,([A-Za-z0-9+/=]+)$/.exec(node.getAttribute("src") ?? "");
      if (!match || match[2].length > MAX_IMAGE_BYTES * 4 / 3 + 4 || ++imageCount > MAX_IMAGES) {
        throw new Error("history_image_unsupported");
      }
      const bytes = Uint8Array.from(atob(match[2]), (char) => char.charCodeAt(0));
      parts.push({ kind: "image", file: new Blob([bytes], { type: match[1] }), name: node.getAttribute("alt") || "history-image" });
    } else {
      throw new Error("history_clipboard_invalid");
    }
  }
  return parts;
}
