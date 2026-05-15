const RELAY_URL_KEY = "atterm-relay-url";
const INSECURE_MODE_KEY = "atterm-insecure-mode";

function isLoopbackHostname(hostname) {
  return hostname === "localhost" ||
    hostname === "127.0.0.1" ||
    /^127\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(hostname) ||
    hostname === "::1" ||
    hostname === "[::1]";
}

export function insecureModeFromStorage(storage) {
  return storage.getItem(INSECURE_MODE_KEY) === "1";
}

export function persistInsecureMode(storage, enabled) {
  storage.setItem(INSECURE_MODE_KEY, enabled ? "1" : "0");
}

export function normalizeRelayBaseURL(value, { allowInsecure = false } = {}) {
  const trimmed = (value || "").trim();
  if (!trimmed) return "";
  const url = new URL(trimmed);
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error("relay URL must use http or https");
  }
  if (url.protocol === "http:" && !allowInsecure && !isLoopbackHostname(url.hostname)) {
    throw new Error("enable insecure mode to use a public HTTP relay");
  }
  return url.origin;
}

export function relayBaseURLFromLocation(href, storage, options = {}) {
  const url = new URL(href);
  const hash = url.hash.startsWith("#") ? url.hash.slice(1) : url.hash;
  if (hash && !hash.startsWith("/")) {
    const fromFragment = new URLSearchParams(hash).get("relay");
    if (fromFragment) {
      const relay = normalizeRelayBaseURL(fromFragment, options);
      storage.setItem(RELAY_URL_KEY, relay);
      return relay;
    }
  }
  try {
    return normalizeRelayBaseURL(storage.getItem(RELAY_URL_KEY) || "", options);
  } catch {
    return "";
  }
}

export function tokenURLWithoutSecret(href) {
  const url = new URL(href);
  url.searchParams.delete("token");
  const hash = url.hash.startsWith("#") ? url.hash.slice(1) : url.hash;
  if (hash && !hash.startsWith("/")) {
    const params = new URLSearchParams(hash);
    if (params.has("token")) {
      params.delete("token");
      const next = params.toString();
      url.hash = next ? `#${next}` : "";
    }
  }
  return url.toString();
}

export function persistRelayBaseURL(storage, value, options = {}) {
  const relay = normalizeRelayBaseURL(value, options);
  storage.setItem(RELAY_URL_KEY, relay);
  return relay;
}

function relayURLForPath(relayBaseURL, path) {
  if (!relayBaseURL) return null;
  return new URL(path, normalizeRelayBaseURL(relayBaseURL, { allowInsecure: true }));
}

export function webSocketAuth(protocol, host, path, relayBaseURL = "") {
  const relayURL = relayURLForPath(relayBaseURL, path);
  const proto = relayURL
    ? (relayURL.protocol === "https:" ? "wss:" : "ws:")
    : (protocol === "https:" ? "wss:" : "ws:");
  return {
    url: relayURL ? `${proto}//${relayURL.host}${relayURL.pathname}${relayURL.search}` : `${proto}//${host}${path}`,
    protocols: undefined,
  };
}

export function apiURL(path, _token, relayBaseURL = "") {
  const relayURL = relayURLForPath(relayBaseURL, path);
  if (relayURL) return relayURL.toString();
  return path;
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

// On mobile-web, focusing the terminal pops up the OS soft keyboard because
// xterm.js routes input through a hidden <textarea>. The shortcut bar exists
// precisely so users can send Esc/Tab/Ctrl-*/arrow keys without that keyboard,
// so refocusing after every sendInput defeats the point. Desktop keeps focus
// for seamless follow-up typing.
export function shouldFocusTerminalAfterInput(mode) {
  return mode !== "mobile-web";
}

// How many CSS pixels of the layout viewport's bottom are obscured by the
// on-screen keyboard, derived from visualViewport. Used to lift #shortcut-bar
// above the keyboard so quick keys stay reachable while typing on mobile.
export function keyboardInsetFromViewport({ innerHeight = 0, viewport } = {}) {
  if (!viewport) return 0;
  const visible = (viewport.height || 0) + (viewport.offsetTop || 0);
  const inset = Math.round(innerHeight - visible);
  return inset > 1 ? inset : 0;
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

const HOST_GROUP_UNKNOWN_KEY = "__unknown__";
const HOST_GROUP_UNKNOWN_HOSTNAME = "unknown host";

export function groupSessionsByHost(sessions) {
  const buckets = new Map();
  for (const s of sessions) {
    const key = s.host_id ? s.host_id : HOST_GROUP_UNKNOWN_KEY;
    let bucket = buckets.get(key);
    if (!bucket) {
      bucket = [];
      buckets.set(key, bucket);
    }
    bucket.push(s);
  }

  const groups = [];
  for (const [key, bucket] of buckets) {
    let displayHost = "";
    let bestStartedAt = -Infinity;
    for (const s of bucket) {
      const h = s.host || "";
      // >= so that when started_at values tie, the later-arriving entry wins.
      if (h && s.started_at >= bestStartedAt) {
        displayHost = h;
        bestStartedAt = s.started_at;
      }
    }
    groups.push({
      key,
      hostname: displayHost || HOST_GROUP_UNKNOWN_HOSTNAME,
      hostId: key === HOST_GROUP_UNKNOWN_KEY ? "" : key,
      sessions: bucket,
    });
  }

  groups.sort((a, b) => {
    if (a.key === HOST_GROUP_UNKNOWN_KEY) return 1;
    if (b.key === HOST_GROUP_UNKNOWN_KEY) return -1;
    return a.hostname.localeCompare(b.hostname);
  });

  return groups;
}

export function pushSupported(navigator, win) {
  if (!navigator || !win) return false;
  if (!navigator.serviceWorker) return false;
  if (!win.PushManager) return false;
  if (!win.Notification) return false;
  // iOS Safari requires PWA install (standalone) before Web Push works.
  const ua = navigator.userAgent || "";
  const isIOS = /iPad|iPhone|iPod/.test(ua);
  if (isIOS && !navigator.standalone) return false;
  return true;
}

export function canEnablePush(permission) {
  return permission === "default" || permission === "granted";
}

export function base64UrlToUint8Array(value) {
  const padded = value + "===".slice((value.length + 3) % 4);
  const base64 = padded.replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(base64);
  const arr = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) arr[i] = raw.charCodeAt(i);
  return arr;
}
