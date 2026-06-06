export type PreviewKind = "code" | "image" | "svg" | "video" | "audio" | "pdf" | "binary-unknown";

const IMAGE_EXTS = new Set(["png", "jpg", "jpeg", "gif", "webp", "bmp", "ico"]);
const VIDEO_EXTS = new Set(["mp4", "webm", "mkv", "mov"]);
const AUDIO_EXTS = new Set(["mp3", "wav", "ogg", "flac", "m4a"]);

function basename(path: string): string {
  const i = path.lastIndexOf("/");
  return i >= 0 ? path.slice(i + 1) : path;
}

function extOf(name: string): string | null {
  const i = name.lastIndexOf(".");
  if (i <= 0) return null; // ".bashrc" → null (dotfile, no real ext)
  return name.slice(i + 1).toLowerCase();
}

/** Decide which preview component should handle `path`.
 *
 *  `isBinary` is the backend `fileMeta.isBinary` flag (NUL byte in first 4 KB).
 *  - Known media extensions win regardless of `isBinary`.
 *  - For an unknown extension, fall back to `code` so CodeViewer can show its
 *    existing too-large / binary banners. The `binary-unknown` kind is only
 *    returned for unknown-ext AND isBinary, where neither preview applies.
 */
export function previewKind(path: string, isBinary: boolean): PreviewKind {
  const name = basename(path);
  const ext = extOf(name);
  if (ext === "svg") return "svg";
  if (ext && IMAGE_EXTS.has(ext)) return "image";
  if (ext && VIDEO_EXTS.has(ext)) return "video";
  if (ext && AUDIO_EXTS.has(ext)) return "audio";
  if (ext === "pdf") return "pdf";
  if (isBinary) return "binary-unknown";
  return "code";
}
