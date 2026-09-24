export type TerminalCopyPlatform = "mac" | "other";

export interface TerminalSelectionSource {
  getSelection: () => string;
}

export interface ClipboardWriter {
  writeText: (text: string) => Promise<void>;
}

export type ClipboardFallback = (text: string) => boolean;

function currentPlatform(): TerminalCopyPlatform {
  if (typeof navigator === "undefined") return "other";
  return navigator.platform?.toLowerCase().includes("mac") ? "mac" : "other";
}

function isCopyKey(e: Pick<KeyboardEvent, "code" | "key">): boolean {
  return e.code === "KeyC" || e.key.toLowerCase() === "c";
}

export function fallbackCopyText(text: string): boolean {
  if (typeof document === "undefined") return false;
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  document.body.appendChild(textarea);
  textarea.select();
  let copied = false;
  try {
    copied = document.execCommand("copy");
  } finally {
    document.body.removeChild(textarea);
  }
  return copied;
}

export function isTerminalCopyShortcut(
  e: Pick<KeyboardEvent, "altKey" | "code" | "ctrlKey" | "key" | "metaKey" | "shiftKey">,
  platform: TerminalCopyPlatform = currentPlatform(),
): boolean {
  if (!isCopyKey(e) || e.altKey) return false;
  if (platform === "mac") {
    return e.metaKey && !e.ctrlKey && !e.shiftKey;
  }
  return e.ctrlKey && e.shiftKey && !e.metaKey;
}

export async function copyTerminalSelection(
  term: TerminalSelectionSource,
  clipboard: ClipboardWriter | undefined = typeof navigator === "undefined" ? undefined : navigator.clipboard,
  fallback: ClipboardFallback = fallbackCopyText,
): Promise<boolean> {
  return copyTextToClipboard(term.getSelection(), clipboard, fallback);
}

export async function copyTextToClipboard(
  text: string,
  clipboard: ClipboardWriter | undefined = typeof navigator === "undefined" ? undefined : navigator.clipboard,
  fallback: ClipboardFallback = fallbackCopyText,
): Promise<boolean> {
  if (!text) return false;
  if (clipboard?.writeText) {
    try {
      await clipboard.writeText(text);
      return true;
    } catch (error) {
      try {
        if (fallback(text)) return true;
      } catch {
        // Keep the original Clipboard API failure; it carries the useful
        // permission/focus diagnosis that caused the fallback attempt.
      }
      throw error;
    }
  }
  return fallback(text);
}
