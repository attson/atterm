const TOKEN_KEY = "atterm-token";

export function tokenFromLocation(href, storage) {
  const url = new URL(href);
  const fromQuery = url.searchParams.get("token");
  if (fromQuery) {
    storage.setItem(TOKEN_KEY, fromQuery);
    return fromQuery;
  }
  return storage.getItem(TOKEN_KEY) || "";
}

export function tokenURLWithoutSecret(href) {
  const url = new URL(href);
  url.searchParams.delete("token");
  return url.toString();
}

export function persistToken(storage, value) {
  storage.setItem(TOKEN_KEY, value.trim());
}

export function wsURL(protocol, host, path, token) {
  const proto = protocol === "https:" ? "wss:" : "ws:";
  const t = encodeURIComponent(token || "");
  return `${proto}//${host}${path}${t ? `?token=${t}` : ""}`;
}

export function apiURL(path, token) {
  const t = encodeURIComponent(token || "");
  return `${path}${t ? `?token=${t}` : ""}`;
}

export function parseSessionRoute(hash) {
  const m = hash.match(/^#\/s\/([0-9a-f-]{36})$/i);
  return m ? m[1] : null;
}

export function shortcutInput(action) {
  const map = {
    esc: "\x1b",
    tab: "\t",
    "ctrl-c": "\x03",
    "ctrl-d": "\x04",
    left: "\x1b[D",
    down: "\x1b[B",
    up: "\x1b[A",
    right: "\x1b[C",
  };
  return map[action] || "";
}

function currentPlatform() {
  if (typeof navigator === "undefined") return "other";
  return navigator.platform?.toLowerCase().includes("mac") ? "mac" : "other";
}

function isCopyKey(ev) {
  return ev.code === "KeyC" || String(ev.key || "").toLowerCase() === "c";
}

function fallbackCopyText(text) {
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

export function isTerminalCopyShortcut(ev, platform = currentPlatform()) {
  if (!isCopyKey(ev) || ev.altKey) return false;
  if (platform === "mac") {
    return ev.metaKey && !ev.ctrlKey && !ev.shiftKey;
  }
  return ev.ctrlKey && ev.shiftKey && !ev.metaKey;
}

export async function copyTerminalSelection(
  term,
  clipboard = typeof navigator === "undefined" ? undefined : navigator.clipboard,
) {
  const text = term.getSelection();
  if (!text) return false;
  if (clipboard?.writeText) {
    await clipboard.writeText(text);
    return true;
  }
  return fallbackCopyText(text);
}

export function detectClientMode({
  coarsePointer = false,
  maxTouchPoints = 0,
  width = 1024,
} = {}) {
  if (coarsePointer) return "mobile-web";
  if (maxTouchPoints > 0 && width < 900) return "mobile-web";
  return "desktop-web";
}

export function isIOSWebKit(userAgent = "") {
  const ua = userAgent.toLowerCase();
  const iOS = /\b(iphone|ipad|ipod)\b/.test(ua) || (/\bmacintosh\b/.test(ua) && /\bmobile\//.test(ua));
  return iOS && /\bsafari\//.test(ua) && !/\b(crios|fxios|edgios)\b/.test(ua);
}

export function shouldShowInstallHint({
  userAgent = "",
  standalone = false,
  dismissed = false,
} = {}) {
  return isIOSWebKit(userAgent) && !standalone && !dismissed;
}

export function canRegisterServiceWorker({
  protocol = "",
  hostname = "",
  serviceWorker = undefined,
} = {}) {
  if (!serviceWorker) return false;
  if (protocol === "https:") return true;
  return protocol === "http:" && /^(localhost|127(?:\.\d{1,3}){3}|\[::1\])$/.test(hostname);
}

export function arrayBufferToBase64(buffer) {
  const bytes = new Uint8Array(buffer);
  let out = "";
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    out += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return btoa(out);
}

export function formatHost(session) {
  const u = session.user || "";
  const h = session.host || "";
  if (u && h) return `${u}@${h}`;
  return u || h || "unknown host";
}

export function shortSessionID(id) {
  return (id || "").slice(0, 8);
}

export function buildDownloadURL(releaseURL, tag, assetName) {
  const url = new URL(releaseURL);
  const marker = "/releases/tag/";
  const idx = url.pathname.lastIndexOf(marker);
  const prefix = idx >= 0 ? url.pathname.slice(0, idx) : "";
  url.pathname = `${prefix}/releases/download/${encodeURIComponent(tag)}/${encodeURIComponent(assetName)}`;
  url.search = "";
  url.hash = "";
  return url.toString();
}

export function sessionTitle(session) {
  const label = session.command || session.title || "session";
  return `${label} · ${shortSessionID(session.id)}`;
}

export function shouldAutoScrollToBottom({ userScrolledUp, isReplay }) {
  return isReplay || !userScrolledUp;
}

function formatBytes(value) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}

export function replayProgressPercent(progress) {
  if (!progress || progress.total_bytes <= 0) return 0;
  return Math.max(0, Math.min(100, Math.round((progress.bytes / progress.total_bytes) * 100)));
}

export function formatReplayProgress(progress) {
  if (!progress || progress.total_bytes <= 0) {
    return `loading history · ${formatBytes(progress?.bytes || 0)}`;
  }
  return `loading history ${replayProgressPercent(progress)}% · ${formatBytes(progress.bytes)} / ${formatBytes(progress.total_bytes)}`;
}

export function versionLabel(version) {
  return `version ${version || "dev"}`;
}
