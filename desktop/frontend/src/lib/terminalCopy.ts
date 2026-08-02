export type TerminalCopyPlatform = "mac" | "other";

export interface TerminalSelectionSource {
  getSelection: () => string;
}

export interface ClipboardWriter {
  writeText: (text: string) => Promise<void>;
}

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
): Promise<boolean> {
  const text = term.getSelection();
  if (!text) return false;
  if (clipboard?.writeText) {
    await clipboard.writeText(text);
    return true;
  }
  return fallbackCopyText(text);
}
