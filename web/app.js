// atterm web client. Two views, share one WebSocket lifecycle for the session view.

const TYPE = {
  OPEN:     0x01,
  IN:       0x02,
  OUT:      0x03,
  RESIZE:   0x04,
  META:     0x05,
  CLOSE:    0x06,
  ATTACH:   0x10,
  LIST:     0x11,
  LIST_RESP:0x12,
  PING:     0x20,
  PONG:     0x21,
};
const VERSION = 1;
const HEADER_LEN = 6;
const SID_LEN = 16;

// ─── frame codec ────────────────────────────────────────────────────────────

function uuidParse(s) {
  const hex = s.replace(/-/g, "");
  if (hex.length !== 32) throw new Error("bad uuid");
  const out = new Uint8Array(16);
  for (let i = 0; i < 16; i++) out[i] = parseInt(hex.substr(i * 2, 2), 16);
  return out;
}
const NIL_SID = new Uint8Array(16);

function encodeFrame(type, sid /* Uint8Array(16) */, payload /* Uint8Array */) {
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

function decodeFrame(arr /* Uint8Array */) {
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
  // JS bigint for u64
  const hi = dv.getUint32(0, false);
  const lo = dv.getUint32(4, false);
  const seq = hi * 0x100000000 + lo; // safe up to 2^53; agents won't hit that
  return { seq, data: p.slice(8) };
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

// ─── token / config ─────────────────────────────────────────────────────────

const tokenInput = document.getElementById("token");
function getToken() {
  const fromQuery = new URLSearchParams(location.search).get("token");
  if (fromQuery) {
    localStorage.setItem("atterm-token", fromQuery);
    return fromQuery;
  }
  return localStorage.getItem("atterm-token") || "";
}
tokenInput.value = getToken();
tokenInput.addEventListener("change", () => {
  localStorage.setItem("atterm-token", tokenInput.value.trim());
});

function wsURL(path) {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const t = encodeURIComponent(getToken());
  return `${proto}//${location.host}${path}${t ? `?token=${t}` : ""}`;
}
function apiURL(path) {
  const t = encodeURIComponent(getToken());
  return `${path}${t ? `?token=${t}` : ""}`;
}

// ─── status indicator ───────────────────────────────────────────────────────

const statusEl = document.getElementById("status");
function setStatus(text, kind) {
  statusEl.textContent = text;
  statusEl.className = "status" + (kind ? " " + kind : "");
}

// ─── list view ──────────────────────────────────────────────────────────────

const listView = document.getElementById("list-view");
const termView = document.getElementById("term-view");
const listEl = document.getElementById("list");
const emptyEl = document.getElementById("empty");
const backBtn = document.getElementById("back");

backBtn.addEventListener("click", () => {
  location.hash = "";
});

let listTimer = null;
async function refreshList() {
  try {
    const res = await fetch(apiURL("/api/sessions"), {
      headers: getToken() ? { Authorization: "Bearer " + getToken() } : {},
    });
    if (!res.ok) {
      setStatus(res.status === 401 ? "unauthorized" : `http ${res.status}`, "err");
      return;
    }
    setStatus("connected", "ok");
    const sessions = await res.json();
    renderList(sessions || []);
  } catch (e) {
    setStatus("offline", "err");
  }
}

function formatHost(s) {
  const u = s.user || "";
  const h = s.host || "";
  if (u && h) return `${u}@${h}`;
  return u || h || "unknown host";
}

function renderList(sessions) {
  listEl.innerHTML = "";
  if (sessions.length === 0) {
    emptyEl.hidden = false;
    return;
  }
  emptyEl.hidden = true;
  for (const s of sessions) {
    const card = document.createElement("div");
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
    card.querySelector(".id").textContent = s.id.slice(0, 8);
    card.querySelector(".size").textContent = `${s.cols}×${s.rows}`;
    card.querySelector(".cwd").textContent = s.cwd || "";
    card.addEventListener("click", () => {
      location.hash = "#/s/" + s.id;
    });
    listEl.appendChild(card);
  }
}

// ─── term view ──────────────────────────────────────────────────────────────

let term = null, fitAddon = null, currentWS = null, currentSID = null;
let lastSeq = 0;
let reconnectAttempts = 0;
let reconnectTimer = null;

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
  fitAddon.fit();
  window.addEventListener("resize", () => { if (fitAddon) fitAddon.fit(); });
  term.onData(data => sendInput(data));
  term.onResize(({ cols, rows }) => sendResize(cols, rows));
}

function sendInput(s) {
  if (!currentWS || currentWS.readyState !== 1) return;
  currentWS.send(encodeFrame(TYPE.IN, currentSID, enc.encode(s)));
}
function sendResize(cols, rows) {
  if (!currentWS || currentWS.readyState !== 1) return;
  currentWS.send(encodeFrame(TYPE.RESIZE, currentSID, encodeResize(cols, rows)));
}

function attachToSession(sessionId) {
  ensureTerm();
  term.reset();
  lastSeq = 0;
  currentSID = uuidParse(sessionId);
  openWS(sessionId);
}

function openWS(sessionId) {
  const ws = new WebSocket(wsURL("/client"));
  ws.binaryType = "arraybuffer";
  currentWS = ws;
  setStatus("connecting…");

  ws.onopen = () => {
    reconnectAttempts = 0;
    setStatus("attached", "ok");
    const attachPayload = enc.encode(JSON.stringify({
      session_id: sessionId,
      since_seq: lastSeq,
    }));
    ws.send(encodeFrame(TYPE.ATTACH, currentSID, attachPayload));
    // push current size right away so PTY matches the browser viewport
    if (term) {
      const { cols, rows } = term;
      sendResize(cols, rows);
    }
  };

  ws.onmessage = (ev) => {
    let f;
    try { f = decodeFrame(new Uint8Array(ev.data)); } catch { return; }
    if (f.type === TYPE.OUT) {
      const { seq, data } = decodeOutPayload(f.payload);
      term.write(data);
      if (seq > lastSeq) lastSeq = seq;
    } else if (f.type === TYPE.CLOSE) {
      setStatus("session ended", "err");
      try {
        const info = JSON.parse(dec.decode(f.payload));
        term.write(`\r\n\x1b[33m[atterm] session ended (exit ${info.exit_code})\x1b[0m\r\n`);
      } catch {}
    } else if (f.type === TYPE.META) {
      // future: update title
    }
  };

  ws.onclose = (ev) => {
    if (location.hash !== "#/s/" + sessionId) return; // user navigated away
    setStatus("reconnecting…", "err");
    const delay = Math.min(8000, 500 * Math.pow(2, reconnectAttempts++));
    clearTimeout(reconnectTimer);
    reconnectTimer = setTimeout(() => openWS(sessionId), delay);
  };

  ws.onerror = () => { /* onclose follows */ };
}

function closeWS() {
  clearTimeout(reconnectTimer);
  reconnectTimer = null;
  reconnectAttempts = 0;
  if (currentWS) {
    try { currentWS.close(); } catch {}
    currentWS = null;
  }
  currentSID = null;
}

// ─── routing ────────────────────────────────────────────────────────────────

function route() {
  const m = location.hash.match(/^#\/s\/([0-9a-f-]{36})$/i);
  if (m) {
    listView.hidden = true;
    termView.hidden = false;
    backBtn.hidden = false;
    if (listTimer) { clearInterval(listTimer); listTimer = null; }
    attachToSession(m[1]);
    setTimeout(() => fitAddon && fitAddon.fit(), 0);
  } else {
    closeWS();
    listView.hidden = false;
    termView.hidden = true;
    backBtn.hidden = true;
    refreshList();
    if (!listTimer) listTimer = setInterval(refreshList, 2000);
  }
}

window.addEventListener("hashchange", route);
route();
