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
