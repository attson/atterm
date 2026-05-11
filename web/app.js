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
        term.write(`\r\n\x1b[33m[atterm] session ended (exit ${info.exit_code})\x1b[0m\r\n`);
      } catch {
        term.write("\r\n\x1b[33m[atterm] session ended\x1b[0m\r\n");
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
