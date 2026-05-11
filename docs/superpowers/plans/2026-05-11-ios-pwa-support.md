# iOS PWA Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the relay-hosted `web/` client usable as an iPhone Safari / home-screen PWA for listing and attaching to atterm sessions.

**Architecture:** Keep `web/` as a dependency-free vanilla client served by `atterm-relay --web web`. Extract testable browser-independent helpers into `web/app-core.js`, keep DOM/xterm orchestration in `web/app.js`, and add PWA metadata/assets plus mobile-first HTML/CSS.

**Tech Stack:** Vanilla JavaScript, CSS, xterm.js CDN, Go `net/http` static serving via existing relay, Node's built-in `node:test` for web helper tests.

---

## File Structure

- Create `web/app-core.js`: pure helper functions for token storage, URL generation, route parsing, shortcut byte mappings, and session formatting. It must work in browser globals and Node tests.
- Create `web/app-core.test.mjs`: Node tests for `web/app-core.js` using `node:test` and `node:assert/strict`.
- Modify `web/app.js`: import/use helpers from `app-core.js`; implement mobile shortcut bar handlers, paste fallback, visual viewport resize handling, token panel behavior, and session title display.
- Modify `web/index.html`: add PWA/iOS metadata, updated app shell markup, mobile terminal controls, paste fallback, and `type="module"` script loading.
- Modify `web/style.css`: mobile-first visual design, safe-area support, terminal viewport sizing, bottom shortcut bar, paste fallback, and desktop responsive rules.
- Create `web/manifest.webmanifest`: installable PWA manifest.
- Create `web/icon.svg`: SVG app icon referenced by manifest and Apple touch icon link.
- Modify `README.md`: document iPhone/PWA usage under browser relay usage.

## Task 1: Pure Web Helper Module

**Files:**
- Create: `web/app-core.js`
- Create: `web/app-core.test.mjs`

- [ ] **Step 1: Write failing helper tests**

Create `web/app-core.test.mjs`:

```js
import test from "node:test";
import assert from "node:assert/strict";

import {
  apiURL,
  buildDownloadURL,
  formatHost,
  parseSessionRoute,
  shortcutInput,
  tokenFromLocation,
  wsURL,
} from "./app-core.js";

test("tokenFromLocation stores query token and returns it", () => {
  const stored = [];
  const storage = {
    getItem: () => "old-token",
    setItem: (key, value) => stored.push([key, value]),
  };

  const token = tokenFromLocation("https://relay.example.com/?token=new-token", storage);

  assert.equal(token, "new-token");
  assert.deepEqual(stored, [["atterm-token", "new-token"]]);
});

test("tokenFromLocation falls back to stored token", () => {
  const storage = {
    getItem: (key) => (key === "atterm-token" ? "stored-token" : null),
    setItem: () => assert.fail("setItem should not be called"),
  };

  assert.equal(tokenFromLocation("https://relay.example.com/", storage), "stored-token");
});

test("wsURL follows page protocol and appends token query", () => {
  assert.equal(
    wsURL("https:", "relay.example.com", "/client", "tok en"),
    "wss://relay.example.com/client?token=tok%20en",
  );
  assert.equal(
    wsURL("http:", "127.0.0.1:8080", "/client", ""),
    "ws://127.0.0.1:8080/client",
  );
});

test("apiURL appends token query", () => {
  assert.equal(apiURL("/api/sessions", "dev"), "/api/sessions?token=dev");
  assert.equal(apiURL("/api/sessions", ""), "/api/sessions");
});

test("parseSessionRoute accepts only session routes", () => {
  assert.equal(
    parseSessionRoute("#/s/11111111-1111-4111-8111-111111111111"),
    "11111111-1111-4111-8111-111111111111",
  );
  assert.equal(parseSessionRoute("#/settings"), null);
  assert.equal(parseSessionRoute("#/s/not-a-uuid"), null);
});

test("shortcutInput maps mobile terminal buttons to exact control sequences", () => {
  assert.equal(shortcutInput("esc"), "\x1b");
  assert.equal(shortcutInput("tab"), "\t");
  assert.equal(shortcutInput("ctrl-c"), "\x03");
  assert.equal(shortcutInput("ctrl-d"), "\x04");
  assert.equal(shortcutInput("left"), "\x1b[D");
  assert.equal(shortcutInput("down"), "\x1b[B");
  assert.equal(shortcutInput("up"), "\x1b[A");
  assert.equal(shortcutInput("right"), "\x1b[C");
  assert.equal(shortcutInput("unknown"), "");
});

test("formatHost prefers user@host and falls back to unknown host", () => {
  assert.equal(formatHost({ user: "alice", host: "mbp" }), "alice@mbp");
  assert.equal(formatHost({ user: "alice" }), "alice");
  assert.equal(formatHost({ host: "mbp" }), "mbp");
  assert.equal(formatHost({}), "unknown host");
});

test("buildDownloadURL creates GitHub release asset URL", () => {
  assert.equal(
    buildDownloadURL("https://github.com/attson/atterm/releases/tag/v0.1.6", "v0.1.6", "atterm.zip"),
    "https://github.com/attson/atterm/releases/download/v0.1.6/atterm.zip",
  );
});
```

- [ ] **Step 2: Run tests and verify they fail because module is missing**

Run:

```bash
node --test web/app-core.test.mjs
```

Expected: FAIL with an import/module-not-found error for `web/app-core.js`.

- [ ] **Step 3: Implement `web/app-core.js`**

Create `web/app-core.js`:

```js
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
```

- [ ] **Step 4: Run helper tests and verify they pass**

Run:

```bash
node --test web/app-core.test.mjs
```

Expected: PASS for all tests.

- [ ] **Step 5: Commit helper module**

```bash
git add web/app-core.js web/app-core.test.mjs
git commit -m "feat web add pwa helper module"
```

## Task 2: PWA Metadata and App Shell Markup

**Files:**
- Modify: `web/index.html`
- Create: `web/manifest.webmanifest`
- Create: `web/icon.svg`

- [ ] **Step 1: Write a metadata smoke test**

Create `web/pwa-metadata.test.mjs`:

```js
import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const html = await readFile(new URL("./index.html", import.meta.url), "utf8");
const manifest = JSON.parse(await readFile(new URL("./manifest.webmanifest", import.meta.url), "utf8"));

test("index contains PWA and iOS metadata", () => {
  assert.match(html, /<link rel="manifest" href="manifest\.webmanifest"/);
  assert.match(html, /<meta name="apple-mobile-web-app-capable" content="yes"/);
  assert.match(html, /<meta name="apple-mobile-web-app-title" content="atterm"/);
  assert.match(html, /<link rel="apple-touch-icon" href="icon\.svg"/);
  assert.match(html, /<script type="module" src="app\.js"><\/script>/);
});

test("manifest is installable and scoped to relay root", () => {
  assert.equal(manifest.name, "atterm");
  assert.equal(manifest.short_name, "atterm");
  assert.equal(manifest.start_url, ".");
  assert.equal(manifest.scope, ".");
  assert.equal(manifest.display, "standalone");
  assert.equal(manifest.icons[0].src, "icon.svg");
});
```

- [ ] **Step 2: Run metadata test and verify it fails**

Run:

```bash
node --test web/pwa-metadata.test.mjs
```

Expected: FAIL because `manifest.webmanifest` is missing and `index.html` lacks PWA metadata.

- [ ] **Step 3: Update `web/index.html`**

Replace `web/index.html` with:

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover" />
<meta name="theme-color" content="#0b1020" />
<meta name="apple-mobile-web-app-capable" content="yes" />
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent" />
<meta name="apple-mobile-web-app-title" content="atterm" />
<title>atterm</title>
<link rel="manifest" href="manifest.webmanifest" />
<link rel="apple-touch-icon" href="icon.svg" />
<link rel="icon" href="icon.svg" type="image/svg+xml" />
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css" />
<link rel="stylesheet" href="style.css" />
</head>
<body>
<div id="app-shell">
  <header id="topbar">
    <button id="back" class="ghost-btn" hidden aria-label="Back to sessions">←</button>
    <div class="brand-block">
      <div class="brand">atterm</div>
      <div class="subtitle" id="session-title">mobile relay</div>
    </div>
    <div class="status" id="status">disconnected</div>
    <button id="token-toggle" class="ghost-btn" aria-expanded="false" aria-controls="token-panel">token</button>
  </header>

  <section id="token-panel" class="token-panel" hidden>
    <label for="token">relay token</label>
    <div class="token-row">
      <input id="token" type="password" placeholder="shared bearer token" autocomplete="off" />
      <button id="token-save" type="button">save</button>
    </div>
  </section>

  <main id="main">
    <section id="list-view">
      <div class="section-head">
        <h1>active sessions</h1>
        <button id="refresh" type="button">refresh</button>
      </div>
      <div id="list"></div>
      <div id="empty" hidden>no live sessions. start one from AT Term or <code>atterm-agent</code>.</div>
    </section>

    <section id="term-view" hidden>
      <div id="term"></div>
      <div id="shortcut-bar" aria-label="terminal shortcuts">
        <button data-shortcut="esc" type="button">Esc</button>
        <button data-shortcut="tab" type="button">Tab</button>
        <button data-shortcut="ctrl-c" type="button">Ctrl-C</button>
        <button data-shortcut="ctrl-d" type="button">Ctrl-D</button>
        <button data-shortcut="left" type="button">←</button>
        <button data-shortcut="down" type="button">↓</button>
        <button data-shortcut="up" type="button">↑</button>
        <button data-shortcut="right" type="button">→</button>
        <button id="paste" type="button">Paste</button>
      </div>
      <form id="paste-fallback" hidden>
        <textarea id="paste-text" rows="3" placeholder="paste text here"></textarea>
        <div class="paste-actions">
          <button type="button" id="paste-cancel">cancel</button>
          <button type="submit">send</button>
        </div>
      </form>
    </section>
  </main>
</div>

<script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.js"></script>
<script src="https://cdn.jsdelivr.net/npm/xterm-addon-fit@0.8.0/lib/xterm-addon-fit.js"></script>
<script type="module" src="app.js"></script>
</body>
</html>
```

- [ ] **Step 4: Create `web/manifest.webmanifest`**

```json
{
  "name": "atterm",
  "short_name": "atterm",
  "description": "Attach to atterm sessions from your phone.",
  "start_url": ".",
  "scope": ".",
  "display": "standalone",
  "background_color": "#05070d",
  "theme_color": "#0b1020",
  "icons": [
    {
      "src": "icon.svg",
      "sizes": "any",
      "type": "image/svg+xml",
      "purpose": "any maskable"
    }
  ]
}
```

- [ ] **Step 5: Create `web/icon.svg`**

```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512">
  <rect width="512" height="512" rx="112" fill="#05070d"/>
  <path d="M112 166c0-28 22-50 50-50h188c28 0 50 22 50 50v180c0 28-22 50-50 50H162c-28 0-50-22-50-50V166z" fill="#0f172a" stroke="#38bdf8" stroke-width="18"/>
  <path d="M176 206l54 50-54 50" fill="none" stroke="#f8fafc" stroke-width="28" stroke-linecap="round" stroke-linejoin="round"/>
  <path d="M258 310h86" stroke="#facc15" stroke-width="28" stroke-linecap="round"/>
</svg>
```

- [ ] **Step 6: Run metadata test and verify it passes**

Run:

```bash
node --test web/pwa-metadata.test.mjs
```

Expected: PASS.

- [ ] **Step 7: Commit PWA shell**

```bash
git add web/index.html web/manifest.webmanifest web/icon.svg web/pwa-metadata.test.mjs
git commit -m "feat web add ios pwa shell"
```

## Task 3: Refactor App JS for Mobile Controls

**Files:**
- Modify: `web/app.js`
- Modify: `web/app-core.js`
- Modify: `web/app-core.test.mjs`

- [ ] **Step 1: Extend helper tests for session titles**

Append to `web/app-core.test.mjs`:

```js
import { sessionTitle } from "./app-core.js";

test("sessionTitle prefers command and short id", () => {
  assert.equal(
    sessionTitle({ id: "11111111-1111-4111-8111-111111111111", command: "/bin/zsh" }),
    "/bin/zsh · 11111111",
  );
  assert.equal(
    sessionTitle({ id: "22222222-2222-4222-8222-222222222222", title: "vim" }),
    "vim · 22222222",
  );
});
```

- [ ] **Step 2: Run helper tests and verify they fail**

Run:

```bash
node --test web/app-core.test.mjs
```

Expected: FAIL because `sessionTitle` is not exported.

- [ ] **Step 3: Add `sessionTitle` helper**

Append to `web/app-core.js`:

```js
export function sessionTitle(session) {
  const label = session.command || session.title || "session";
  return `${label} · ${shortSessionID(session.id)}`;
}
```

- [ ] **Step 4: Replace `web/app.js` with module-based mobile implementation**

Replace `web/app.js` with:

```js
// atterm web client. Mobile-first PWA shell plus the existing relay protocol.

import {
  apiURL as makeAPIURL,
  formatHost,
  parseSessionRoute,
  persistToken,
  sessionTitle,
  shortSessionID,
  shortcutInput,
  tokenFromLocation,
  wsURL as makeWSURL,
} from "./app-core.js";

const TYPE = {
  OPEN: 0x01,
  IN: 0x02,
  OUT: 0x03,
  RESIZE: 0x04,
  META: 0x05,
  CLOSE: 0x06,
  ATTACH: 0x10,
  LIST: 0x11,
  LIST_RESP: 0x12,
  PING: 0x20,
  PONG: 0x21,
};
const VERSION = 1;
const HEADER_LEN = 6;
const SID_LEN = 16;

function uuidParse(s) {
  const hex = s.replace(/-/g, "");
  if (hex.length !== 32) throw new Error("bad uuid");
  const out = new Uint8Array(16);
  for (let i = 0; i < 16; i++) out[i] = parseInt(hex.substr(i * 2, 2), 16);
  return out;
}
const NIL_SID = new Uint8Array(16);

function encodeFrame(type, sid, payload) {
  const p = payload || new Uint8Array(0);
  const buf = new Uint8Array(HEADER_LEN + SID_LEN + p.length);
  const dv = new DataView(buf.buffer);
  buf[0] = VERSION;
  buf[1] = type;
  dv.setUint32(2, p.length, false);
  buf.set(sid || NIL_SID, HEADER_LEN);
  buf.set(p, HEADER_LEN + SID_LEN);
  return buf;
}

function decodeFrame(arr) {
  if (arr.length < HEADER_LEN + SID_LEN) throw new Error("short");
  if (arr[0] !== VERSION) throw new Error("bad version");
  const dv = new DataView(arr.buffer, arr.byteOffset, arr.byteLength);
  const plen = dv.getUint32(2, false);
  if (HEADER_LEN + SID_LEN + plen !== arr.length) throw new Error("bad len");
  return {
    type: arr[1],
    sid: arr.slice(HEADER_LEN, HEADER_LEN + SID_LEN),
    payload: arr.slice(HEADER_LEN + SID_LEN),
  };
}

function decodeOutPayload(p) {
  if (p.length < 8) return { seq: 0, data: new Uint8Array(0) };
  const dv = new DataView(p.buffer, p.byteOffset, p.byteLength);
  const hi = dv.getUint32(0, false);
  const lo = dv.getUint32(4, false);
  return { seq: hi * 0x100000000 + lo, data: p.slice(8) };
}

function encodeResize(cols, rows) {
  const b = new Uint8Array(4);
  const dv = new DataView(b.buffer);
  dv.setUint16(0, cols, false);
  dv.setUint16(2, rows, false);
  return b;
}

const enc = new TextEncoder();
const dec = new TextDecoder();

const tokenInput = document.getElementById("token");
const tokenToggle = document.getElementById("token-toggle");
const tokenPanel = document.getElementById("token-panel");
const tokenSave = document.getElementById("token-save");
const statusEl = document.getElementById("status");
const sessionTitleEl = document.getElementById("session-title");
const listView = document.getElementById("list-view");
const termView = document.getElementById("term-view");
const listEl = document.getElementById("list");
const emptyEl = document.getElementById("empty");
const backBtn = document.getElementById("back");
const refreshBtn = document.getElementById("refresh");
const pasteBtn = document.getElementById("paste");
const pasteFallback = document.getElementById("paste-fallback");
const pasteText = document.getElementById("paste-text");
const pasteCancel = document.getElementById("paste-cancel");

let token = tokenFromLocation(location.href, localStorage);
tokenInput.value = token;

function getToken() {
  return token;
}

function saveToken() {
  token = tokenInput.value.trim();
  persistToken(localStorage, token);
  tokenPanel.hidden = true;
  tokenToggle.setAttribute("aria-expanded", "false");
  refreshList();
}

tokenInput.addEventListener("change", saveToken);
tokenSave.addEventListener("click", saveToken);
tokenToggle.addEventListener("click", () => {
  tokenPanel.hidden = !tokenPanel.hidden;
  tokenToggle.setAttribute("aria-expanded", String(!tokenPanel.hidden));
});

function wsURL(path) {
  return makeWSURL(location.protocol, location.host, path, getToken());
}
function apiURL(path) {
  return makeAPIURL(path, getToken());
}

function setStatus(text, kind) {
  statusEl.textContent = text;
  statusEl.className = "status" + (kind ? " " + kind : "");
}

backBtn.addEventListener("click", () => {
  location.hash = "";
});
refreshBtn.addEventListener("click", () => refreshList());

let listTimer = null;
let lastSessions = [];

async function refreshList() {
  try {
    const res = await fetch(apiURL("/api/sessions"), {
      headers: getToken() ? { Authorization: "Bearer " + getToken() } : {},
    });
    if (!res.ok) {
      setStatus(res.status === 401 ? "unauthorized" : `http ${res.status}`, "err");
      if (res.status === 401) tokenPanel.hidden = false;
      return;
    }
    setStatus("connected", "ok");
    const sessions = await res.json();
    lastSessions = sessions || [];
    renderList(lastSessions);
  } catch {
    setStatus("offline", "err");
  }
}

function renderList(sessions) {
  listEl.innerHTML = "";
  if (sessions.length === 0) {
    emptyEl.hidden = false;
    return;
  }
  emptyEl.hidden = true;
  for (const s of sessions) {
    const card = document.createElement("button");
    card.type = "button";
    card.className = "card";
    card.innerHTML = `
      <div class="host">
        <span class="who"></span>
        <span class="hostid" title=""></span>
      </div>
      <div class="cmd"></div>
      <div class="meta">
        <span class="id"></span>
        <span class="size"></span>
        <span class="cwd"></span>
      </div>`;
    card.querySelector(".who").textContent = formatHost(s);
    const hostidEl = card.querySelector(".hostid");
    if (s.host_id) {
      hostidEl.textContent = s.host_id.slice(0, 8);
      hostidEl.title = "host_id " + s.host_id;
    } else {
      hostidEl.hidden = true;
    }
    card.querySelector(".cmd").textContent = s.command || "(unknown)";
    card.querySelector(".id").textContent = shortSessionID(s.id);
    card.querySelector(".size").textContent = `${s.cols}×${s.rows}`;
    card.querySelector(".cwd").textContent = s.cwd || "";
    card.addEventListener("click", () => {
      location.hash = "#/s/" + s.id;
    });
    listEl.appendChild(card);
  }
}

let term = null;
let fitAddon = null;
let currentWS = null;
let currentSID = null;
let lastSeq = 0;
let reconnectAttempts = 0;
let reconnectTimer = null;
let fitTimer = null;

function scheduleFit() {
  clearTimeout(fitTimer);
  fitTimer = setTimeout(() => {
    if (fitAddon) fitAddon.fit();
  }, 50);
}

function ensureTerm() {
  if (term) return;
  term = new Terminal({
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
    fontSize: 13,
    theme: { background: "#000000" },
    cursorBlink: true,
    convertEol: false,
    scrollback: 20000,
    allowProposedApi: true,
  });
  fitAddon = new FitAddon.FitAddon();
  term.loadAddon(fitAddon);
  term.open(document.getElementById("term"));
  scheduleFit();
  window.addEventListener("resize", scheduleFit);
  if (window.visualViewport) {
    window.visualViewport.addEventListener("resize", scheduleFit);
    window.visualViewport.addEventListener("scroll", scheduleFit);
  }
  term.onData((data) => sendInput(data));
  term.onResize(({ cols, rows }) => sendResize(cols, rows));
}

function sendInput(s) {
  if (!currentWS || currentWS.readyState !== 1 || !currentSID) return;
  currentWS.send(encodeFrame(TYPE.IN, currentSID, enc.encode(s)));
  if (term) term.focus();
}
function sendResize(cols, rows) {
  if (!currentWS || currentWS.readyState !== 1 || !currentSID) return;
  currentWS.send(encodeFrame(TYPE.RESIZE, currentSID, encodeResize(cols, rows)));
}

function sessionForID(sessionId) {
  return lastSessions.find((s) => s.id === sessionId) || { id: sessionId };
}

function attachToSession(sessionId) {
  ensureTerm();
  term.reset();
  lastSeq = 0;
  currentSID = uuidParse(sessionId);
  sessionTitleEl.textContent = sessionTitle(sessionForID(sessionId));
  openWS(sessionId);
}

function openWS(sessionId) {
  const ws = new WebSocket(wsURL("/client"));
  ws.binaryType = "arraybuffer";
  currentWS = ws;
  setStatus("connecting...");

  ws.onopen = () => {
    reconnectAttempts = 0;
    setStatus("attached", "ok");
    const attachPayload = enc.encode(JSON.stringify({
      session_id: sessionId,
      since_seq: lastSeq,
    }));
    ws.send(encodeFrame(TYPE.ATTACH, currentSID, attachPayload));
    if (term) {
      scheduleFit();
      const { cols, rows } = term;
      sendResize(cols, rows);
      term.focus();
    }
  };

  ws.onmessage = (ev) => {
    let f;
    try {
      f = decodeFrame(new Uint8Array(ev.data));
    } catch {
      return;
    }
    if (f.type === TYPE.OUT) {
      const { seq, data } = decodeOutPayload(f.payload);
      term.write(data);
      if (seq > lastSeq) lastSeq = seq;
    } else if (f.type === TYPE.CLOSE) {
      setStatus("session ended", "err");
      try {
        const info = JSON.parse(dec.decode(f.payload));
        term.write(`\r\n\x1b[33m[AT Term] session ended (exit ${info.exit_code})\x1b[0m\r\n`);
      } catch {
        term.write("\r\n\x1b[33m[AT Term] session ended\x1b[0m\r\n");
      }
    }
  };

  ws.onclose = () => {
    if (parseSessionRoute(location.hash) !== sessionId) return;
    setStatus("reconnecting...", "err");
    const delay = Math.min(8000, 500 * Math.pow(2, reconnectAttempts++));
    clearTimeout(reconnectTimer);
    reconnectTimer = setTimeout(() => openWS(sessionId), delay);
  };

  ws.onerror = () => {};
}

function closeWS() {
  clearTimeout(reconnectTimer);
  reconnectTimer = null;
  reconnectAttempts = 0;
  if (currentWS) {
    try {
      currentWS.close();
    } catch {}
    currentWS = null;
  }
  currentSID = null;
}

document.getElementById("shortcut-bar").addEventListener("click", (ev) => {
  const btn = ev.target.closest("button[data-shortcut]");
  if (!btn) return;
  const input = shortcutInput(btn.dataset.shortcut);
  if (input) sendInput(input);
});

pasteBtn.addEventListener("click", async () => {
  try {
    const text = await navigator.clipboard.readText();
    if (text) sendInput(text);
  } catch {
    pasteFallback.hidden = false;
    pasteText.focus();
  }
});

pasteCancel.addEventListener("click", () => {
  pasteFallback.hidden = true;
  pasteText.value = "";
});

pasteFallback.addEventListener("submit", (ev) => {
  ev.preventDefault();
  if (pasteText.value) sendInput(pasteText.value);
  pasteText.value = "";
  pasteFallback.hidden = true;
});

function route() {
  const sessionId = parseSessionRoute(location.hash);
  if (sessionId) {
    listView.hidden = true;
    termView.hidden = false;
    backBtn.hidden = false;
    tokenToggle.hidden = true;
    if (listTimer) {
      clearInterval(listTimer);
      listTimer = null;
    }
    attachToSession(sessionId);
    scheduleFit();
  } else {
    closeWS();
    listView.hidden = false;
    termView.hidden = true;
    backBtn.hidden = true;
    tokenToggle.hidden = false;
    sessionTitleEl.textContent = "mobile relay";
    refreshList();
    if (!listTimer) listTimer = setInterval(refreshList, 2000);
  }
}

window.addEventListener("hashchange", route);
route();
```

- [ ] **Step 5: Run helper tests and verify they pass**

Run:

```bash
node --test web/app-core.test.mjs
```

Expected: PASS.

- [ ] **Step 6: Commit mobile JS**

```bash
git add web/app.js web/app-core.js web/app-core.test.mjs
git commit -m "feat web add mobile terminal controls"
```

## Task 4: Mobile-First CSS and Safe-Area Layout

**Files:**
- Modify: `web/style.css`

- [ ] **Step 1: Write CSS smoke test**

Create `web/mobile-css.test.mjs`:

```js
import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const css = await readFile(new URL("./style.css", import.meta.url), "utf8");

test("css includes safe-area and mobile terminal controls", () => {
  assert.match(css, /env\(safe-area-inset-top\)/);
  assert.match(css, /env\(safe-area-inset-bottom\)/);
  assert.match(css, /#shortcut-bar/);
  assert.match(css, /#paste-fallback/);
  assert.match(css, /visual|100dvh|--app-height/);
});

test("css keeps desktop responsive session grid", () => {
  assert.match(css, /@media \(min-width: 720px\)/);
  assert.match(css, /grid-template-columns: repeat\(auto-fill, minmax\(320px, 1fr\)\)/);
});
```

- [ ] **Step 2: Run CSS smoke test and verify it fails**

Run:

```bash
node --test web/mobile-css.test.mjs
```

Expected: FAIL because current CSS lacks shortcut bar and safe-area rules.

- [ ] **Step 3: Replace `web/style.css`**

Replace `web/style.css` with:

```css
:root {
  --bg: #05070d;
  --panel: #0f172a;
  --panel-2: #111827;
  --border: #263244;
  --fg: #e5eefb;
  --fg-dim: #93a4ba;
  --accent: #38bdf8;
  --accent-2: #facc15;
  --good: #4ade80;
  --bad: #fb7185;
  --topbar-h: 58px;
  --shortcut-h: 58px;
  --app-height: 100dvh;
}
* { box-sizing: border-box; }
html, body {
  margin: 0;
  padding: 0;
  min-height: 100%;
  background: radial-gradient(circle at top left, #172554 0, #05070d 42%);
  color: var(--fg);
  font-family: ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  overscroll-behavior: none;
}
body { min-height: var(--app-height); }
button, input, textarea { font: inherit; }
button { -webkit-tap-highlight-color: transparent; }

#app-shell {
  min-height: var(--app-height);
  display: flex;
  flex-direction: column;
}
#topbar {
  min-height: calc(var(--topbar-h) + env(safe-area-inset-top));
  padding: calc(10px + env(safe-area-inset-top)) 12px 10px;
  display: flex;
  align-items: center;
  gap: 10px;
  background: rgba(5, 7, 13, 0.86);
  border-bottom: 1px solid rgba(148, 163, 184, 0.18);
  backdrop-filter: blur(18px);
  position: sticky;
  top: 0;
  z-index: 20;
}
.brand-block { min-width: 0; }
.brand { font-weight: 800; letter-spacing: 0.08em; color: var(--fg); line-height: 1.1; }
.subtitle { font-size: 11px; color: var(--fg-dim); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 46vw; }
.status { font-size: 12px; color: var(--fg-dim); margin-left: auto; white-space: nowrap; }
.status.ok { color: var(--good); }
.status.err { color: var(--bad); }
.ghost-btn, #refresh, #token-save, .paste-actions button {
  background: rgba(15, 23, 42, 0.9);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 999px;
  padding: 9px 12px;
  cursor: pointer;
}
.ghost-btn:hover, #refresh:hover, #token-save:hover, .paste-actions button:hover { border-color: var(--accent); color: var(--accent); }

.token-panel {
  padding: 12px;
  background: rgba(15, 23, 42, 0.96);
  border-bottom: 1px solid var(--border);
}
.token-panel label { display: block; font-size: 12px; color: var(--fg-dim); margin-bottom: 6px; }
.token-row { display: flex; gap: 8px; }
.token-row input {
  flex: 1;
  min-width: 0;
  background: #020617;
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 11px 12px;
}

#main {
  flex: 1;
  width: 100%;
  max-width: 1120px;
  margin: 0 auto;
  padding: 16px 12px calc(20px + env(safe-area-inset-bottom));
}
.section-head { display: flex; align-items: center; gap: 12px; margin-bottom: 14px; }
#list-view h1 {
  margin: 0;
  font-size: 12px;
  font-weight: 800;
  color: var(--fg-dim);
  text-transform: uppercase;
  letter-spacing: 0.12em;
}
#refresh { margin-left: auto; font-size: 12px; padding: 7px 10px; }
#list { display: grid; grid-template-columns: 1fr; gap: 12px; }
.card {
  width: 100%;
  text-align: left;
  background: linear-gradient(145deg, rgba(15, 23, 42, 0.98), rgba(17, 24, 39, 0.92));
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 18px;
  padding: 16px;
  color: var(--fg);
  cursor: pointer;
  transition: transform 120ms, border-color 120ms;
  box-shadow: 0 18px 60px rgba(0, 0, 0, 0.24);
}
.card:active { transform: scale(0.99); }
.card:hover { border-color: var(--accent); }
.card .host { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; margin-bottom: 8px; display: flex; align-items: baseline; gap: 8px; }
.card .host .who { color: var(--accent-2); }
.card .host .hostid { color: var(--fg-dim); font-size: 11px; }
.card .cmd { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 15px; color: var(--fg); margin-bottom: 8px; word-break: break-all; }
.card .meta { font-size: 11px; color: var(--fg-dim); display: flex; gap: 12px; flex-wrap: wrap; }
.card .meta .id { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
#empty { color: var(--fg-dim); font-size: 14px; text-align: center; padding: 56px 18px; line-height: 1.5; }
#empty code { background: var(--panel); border: 1px solid var(--border); padding: 2px 6px; border-radius: 6px; }

#main:has(#term-view:not([hidden])) {
  max-width: none;
  padding: 0;
  height: calc(var(--app-height) - var(--topbar-h) - env(safe-area-inset-top));
}
#term-view {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: #000;
}
#term-view[hidden] { display: none; }
#term {
  flex: 1 1 auto;
  min-height: 0;
  padding: 6px 8px;
  background: #000;
}
.xterm { height: 100%; }
.xterm-viewport { background: transparent !important; }
#shortcut-bar {
  flex: 0 0 auto;
  min-height: calc(var(--shortcut-h) + env(safe-area-inset-bottom));
  padding: 8px 8px calc(8px + env(safe-area-inset-bottom));
  display: flex;
  gap: 8px;
  overflow-x: auto;
  background: rgba(2, 6, 23, 0.96);
  border-top: 1px solid var(--border);
  scrollbar-width: none;
}
#shortcut-bar::-webkit-scrollbar { display: none; }
#shortcut-bar button {
  flex: 0 0 auto;
  min-width: 54px;
  border: 1px solid #334155;
  border-radius: 12px;
  background: #0f172a;
  color: var(--fg);
  padding: 10px 12px;
}
#shortcut-bar button:active { background: #1e293b; color: var(--accent); }
#paste-fallback {
  position: fixed;
  left: 12px;
  right: 12px;
  bottom: calc(72px + env(safe-area-inset-bottom));
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 16px;
  padding: 12px;
  z-index: 30;
  box-shadow: 0 20px 70px rgba(0, 0, 0, 0.45);
}
#paste-fallback textarea {
  width: 100%;
  resize: vertical;
  background: #020617;
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 10px;
}
.paste-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px; }

@media (min-width: 720px) {
  #main { padding: 24px; }
  #list { grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); }
  .subtitle { max-width: 360px; }
  #shortcut-bar { justify-content: center; }
  #main:has(#term-view:not([hidden])) { height: calc(100vh - 58px); }
}
```

- [ ] **Step 4: Run CSS smoke test and verify it passes**

Run:

```bash
node --test web/mobile-css.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Commit mobile CSS**

```bash
git add web/style.css web/mobile-css.test.mjs
git commit -m "feat web add mobile pwa layout"
```

## Task 5: Static Serving and Browser Manual Verification

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Run all web unit tests**

Run:

```bash
node --test web/*.test.mjs
```

Expected: PASS.

- [ ] **Step 2: Run relay locally with the web client**

Run:

```bash
ATTERM_TOKEN=dev go run ./cmd/atterm-relay --addr :8080 --web web
```

Expected: process stays running and logs no startup error.

- [ ] **Step 3: In a second terminal, create a session**

Run:

```bash
ATTERM_TOKEN=dev go run ./cmd/atterm-agent --relay ws://localhost:8080 -- bash
```

Expected: agent connects and starts a shell session.

- [ ] **Step 4: Manually verify desktop browser behavior**

Open:

```text
http://localhost:8080/?token=dev
```

Expected:

- Session list shows the bash session.
- Clicking it opens terminal view.
- Typing `echo desktop-ok` and Enter shows `desktop-ok`.
- Back button returns to session list.

- [ ] **Step 5: Manually verify iPhone-sized viewport**

Use browser responsive mode or an actual iPhone Safari pointed at the relay URL.

Expected:

- Session cards are single-column and touch-sized.
- Terminal fills the visible viewport.
- Bottom shortcut bar is visible.
- `Tab`, `Ctrl-C`, `Ctrl-D`, and arrow buttons send expected input.
- Paste button sends clipboard text or opens fallback textarea.
- Rotate/resize refits the terminal.

- [ ] **Step 6: Update README browser usage section**

In `README.md`, under "自己跑 relay + 浏览器", add this paragraph after the browser URL example:

```markdown
手机接管：iPhone Safari 可以直接打开同一个 relay URL，例如
`https://relay.example.com/?token=...`。页面支持添加到主屏幕作为 PWA；手机端会显示触控优化的 session 列表、终端视图和 `Esc` / `Tab` / `Ctrl-C` / 方向键等快捷键。真实部署建议使用 HTTPS/WSS。
```

- [ ] **Step 7: Commit README update**

```bash
git add README.md
git commit -m "docs describe ios pwa client"
```

## Task 6: Full Verification and Release Prep

**Files:**
- No production code changes expected.

- [ ] **Step 1: Run Go vet**

Run:

```bash
export PATH=$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go vet -tags webkit2_41 ./...
```

Expected: exit 0.

- [ ] **Step 2: Run desktop Go tests**

Run:

```bash
export PATH=$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go test -tags webkit2_41 -timeout 60s ./desktop/
```

Expected: PASS.

- [ ] **Step 3: Run frontend build**

Run:

```bash
cd desktop/frontend && npm run build
```

Expected: `vue-tsc --noEmit && vite build` completes successfully.

- [ ] **Step 4: Run web tests**

Run:

```bash
node --test web/*.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Check git status**

Run:

```bash
git status --short --branch
```

Expected: clean working tree on the feature branch.

- [ ] **Step 6: Push branch**

Run:

```bash
git push
```

Expected: feature branch updates on origin.

