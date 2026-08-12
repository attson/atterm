import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { hkdf } from "@noble/hashes/hkdf.js";
import { sha256 } from "@noble/hashes/sha2.js";
import { xchacha20poly1305 } from "@noble/ciphers/chacha.js";
import { utf8ToBytes, randomBytes } from "@noble/hashes/utils.js";
import source from "./connection.ts?raw";
import { decryptSessionFields, SessionConnection, SessionListConnection, type FSResponse, type SessionInfo } from "./connection";
import { setAccountKeyProvider } from "./account-key";
import type { SealedSessionFields } from "./opaque";
import { TYPE, NIL_SID, decodeFrame, decodeText, encodeFrame, encodeText, uuidParse } from "./proto";
import { encodeSegments, decodeSegments } from "./fsSegments";

// FS frames are segment-framed; everything else stays a bare JSON payload.
function isFSFrameType(type: number): boolean {
  return type === TYPE.FS_REQUEST || type === TYPE.FS_RESPONSE || type === TYPE.FS_EVENT;
}

// --- Sealing helper (mirrors opaque.test.ts sealFields) ---------------------
const SESSION_INFO_AAD_FRAME_TYPE = 0x12;
function uuidStringToBytes(s: string): Uint8Array {
  const hex = s.replace(/-/g, "");
  const out = new Uint8Array(16);
  for (let i = 0; i < 16; i++) out[i] = parseInt(hex.substring(i * 2, i * 2 + 2), 16);
  return out;
}
function sealSessionFields(accountKey: Uint8Array, uuid: string, fields: SealedSessionFields): string {
  const uuidBytes = uuidStringToBytes(uuid);
  const prefix = utf8ToBytes("atterm-session-v1");
  const info = new Uint8Array(prefix.length + uuidBytes.length);
  info.set(prefix, 0);
  info.set(uuidBytes, prefix.length);
  const sessionKey = hkdf(sha256, accountKey, undefined, info, 32);
  const nonce = randomBytes(24);
  const aad = new Uint8Array(uuidBytes.length + 1);
  aad.set(uuidBytes, 0);
  aad[uuidBytes.length] = SESSION_INFO_AAD_FRAME_TYPE;
  const aead = xchacha20poly1305(sessionKey, nonce, aad);
  const ct = aead.encrypt(new TextEncoder().encode(JSON.stringify(fields)));
  const env = new Uint8Array(1 + 24 + ct.length);
  env[0] = 0x01;
  env.set(nonce, 1);
  env.set(ct, 1 + 24);
  // base64-std encode
  let bin = "";
  for (const b of env) bin += String.fromCharCode(b);
  return btoa(bin);
}

function baseSession(id: string, over: Partial<SessionInfo> = {}): SessionInfo {
  return { id, command: "", cwd: "", title: "", cols: 80, rows: 24, started_at: 0, ...over };
}

function encodeOutPayload(seq: number, text: string): Uint8Array {
  const data = encodeText(text);
  const out = new Uint8Array(8 + data.length);
  const dv = new DataView(out.buffer);
  dv.setUint32(0, Math.floor(seq / 0x100000000), false);
  dv.setUint32(4, seq >>> 0, false);
  out.set(data, 8);
  return out;
}

describe("SessionConnection FS RPC", () => {
  const sessionId = "11111111-2222-3333-4444-555555555555";
  const endpoint = { url: "ws://127.0.0.1:1234", session_token: "token" };

  class FakeWebSocket {
    static instances: FakeWebSocket[] = [];
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSED = 3;

    readyState = FakeWebSocket.CONNECTING;
    binaryType = "";
    sent: Uint8Array[] = [];
    throwOnSend = false;
    onopen: (() => void) | null = null;
    onmessage: ((event: MessageEvent) => void) | null = null;
    onclose: (() => void) | null = null;
    onerror: (() => void) | null = null;

    constructor(
      public url: string,
      public protocols?: string[],
    ) {
      FakeWebSocket.instances.push(this);
    }

    send(data: Uint8Array) {
      if (this.throwOnSend) throw new Error("send failed");
      this.sent.push(data);
    }

    close() {
      this.readyState = FakeWebSocket.CLOSED;
      this.onclose?.();
    }

    open() {
      this.readyState = FakeWebSocket.OPEN;
      this.onopen?.();
    }

    emit(type: number, payload: unknown) {
      // FS frames carry segment-framed payloads since they gained E2EE.
      // This fake peer is keyless, so one plaintext segment is correct.
      const json = encodeText(JSON.stringify(payload));
      const body = isFSFrameType(type) ? encodeSegments([json]) : json;
      const bytes = encodeFrame(type as any, uuidParse(sessionId), body);
      this.onmessage?.({ data: bytes.buffer } as MessageEvent);
    }
  }

  beforeEach(() => {
    FakeWebSocket.instances = [];
    vi.stubGlobal("WebSocket", FakeWebSocket);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  function openConnection(): { conn: SessionConnection; ws: FakeWebSocket } {
    const conn = new SessionConnection(endpoint, sessionId);
    conn.attach();
    const ws = FakeWebSocket.instances[FakeWebSocket.instances.length - 1];
    ws.open();
    return { conn, ws };
  }

  test("sendFSRequest sends FS_REQUEST and resolves the matching FS_RESPONSE", async () => {
    const { conn, ws } = openConnection();

    const responsePromise = conn.sendFSRequest({ op: "list_dir", path: "/tmp", request_id: "fs-test-1" });

    const frame = decodeFrame(ws.sent[1]);
    expect(frame.type).toBe(TYPE.FS_REQUEST);
    expect(Array.from(frame.sid)).toEqual(Array.from(uuidParse(sessionId)));
    expect(JSON.parse(decodeText(decodeSegments(frame.payload)![0]))).toEqual({
      op: "list_dir",
      path: "/tmp",
      request_id: "fs-test-1",
    });

    ws.emit(TYPE.FS_RESPONSE, { request_id: "fs-test-1", ok: true, entries: [{ name: "a.txt", isDir: false }] });

    await expect(responsePromise).resolves.toEqual({
      request_id: "fs-test-1",
      ok: true,
      entries: [{ name: "a.txt", isDir: false }],
    });
  });

  test("generates fs-prefixed request IDs when absent", async () => {
    const { conn, ws } = openConnection();

    const responsePromise = conn.sendFSRequest({ op: "list_dir", path: "/tmp" });
    const frame = decodeFrame(ws.sent[1]);
    const payload = JSON.parse(decodeText(decodeSegments(frame.payload)![0]));
    expect(payload.request_id).toMatch(/^fs-/);

    ws.emit(TYPE.FS_RESPONSE, { request_id: payload.request_id, ok: true, entries: [] });
    await expect(responsePromise).resolves.toMatchObject({ request_id: payload.request_id, ok: true });
  });

  test("exports structured FS_RESPONSE result types matching the wire schema", () => {
    expect(source).toMatch(/export interface FSDirEntry/);
    expect(source).toMatch(/entries\?: FSDirEntry\[\]/);
    expect(source).toMatch(/meta\?: FSFileMetaInfo/);
    expect(source).toMatch(/content\?: FSFileContent/);
    expect(source).toMatch(/chunk\?: FSChunkPayload/);

    const response: FSResponse = {
      request_id: "fs-typed",
      ok: true,
      entries: [{ name: "src", isDir: true, size: 0, modTime: 1760000000000 }],
      meta: { path: "/tmp/a.txt", size: 12, modTime: 1760000000001, isBinary: false },
      content: { path: "/tmp/a.txt", data: "aGVsbG8=", isBinary: false, truncatedAt: 0 },
      chunk: {
        path: "/tmp/a.txt",
        data: "aGVs",
        offset: 0,
        length: 3,
        eof: false,
        contentType: "text/plain",
      },
      watch_id: "watch-1",
    };

    expect(response.content?.data).toBe("aGVsbG8=");
    expect(response.chunk?.offset).toBe(0);
  });

  test("unknown FS_RESPONSE does not resolve another pending request", async () => {
    const { conn, ws } = openConnection();
    let settled = false;

    const responsePromise = conn
      .sendFSRequest({ op: "file_meta", path: "/tmp/a.txt", request_id: "fs-test-2" })
      .then((res) => {
        settled = true;
        return res;
      });

    ws.emit(TYPE.FS_RESPONSE, { request_id: "fs-other", ok: true });
    await Promise.resolve();
    expect(settled).toBe(false);

    ws.emit(TYPE.FS_RESPONSE, {
      request_id: "fs-test-2",
      ok: true,
      meta: { path: "/tmp/a.txt", size: 12, modTime: 1, isBinary: false },
    });
    await expect(responsePromise).resolves.toEqual({
      request_id: "fs-test-2",
      ok: true,
      meta: { path: "/tmp/a.txt", size: 12, modTime: 1, isBinary: false },
    });
  });

  test("malformed matching FS_RESPONSE is ignored until a valid matching response arrives", async () => {
    const { conn, ws } = openConnection();
    let settled = false;
    const responsePromise = conn
      .sendFSRequest({ op: "read_file", path: "/tmp/a.txt", request_id: "fs-malformed" })
      .then((res) => {
        settled = true;
        return res;
      });

    ws.emit(TYPE.FS_RESPONSE, { request_id: "fs-malformed", ok: true, content: { data: false } });
    await Promise.resolve();
    expect(settled).toBe(false);

    ws.emit(TYPE.FS_RESPONSE, {
      request_id: "fs-malformed",
      ok: true,
      content: { path: "/tmp/a.txt", data: "aGVsbG8=", isBinary: false, truncatedAt: 0 },
    });
    await expect(responsePromise).resolves.toEqual({
      request_id: "fs-malformed",
      ok: true,
      content: { path: "/tmp/a.txt", data: "aGVsbG8=", isBinary: false, truncatedAt: 0 },
    });
  });

  test("duplicate request_id is rejected before sending and the original remains resolvable", async () => {
    const { conn, ws } = openConnection();
    const firstPromise = conn.sendFSRequest({ op: "list_dir", path: "/tmp", request_id: "fs-duplicate" });
    const duplicatePromise = conn.sendFSRequest(
      { op: "file_meta", path: "/tmp/a.txt", request_id: "fs-duplicate" },
      1,
    );

    await expect(duplicatePromise).rejects.toThrow(/duplicate/i);
    expect(ws.sent).toHaveLength(2);

    ws.emit(TYPE.FS_RESPONSE, { request_id: "fs-duplicate", ok: true, entries: [] });
    await expect(firstPromise).resolves.toEqual({ request_id: "fs-duplicate", ok: true, entries: [] });
  });

  test("timed out explicit request_id is retired so late responses cannot resolve a later request", async () => {
    vi.useFakeTimers();
    const { conn, ws } = openConnection();
    const firstPromise = conn.sendFSRequest({ op: "list_dir", path: "/tmp", request_id: "fs-timeout" }, 10);
    const firstRejection = expect(firstPromise).rejects.toThrow(/timed out/i);

    await vi.advanceTimersByTimeAsync(10);
    await firstRejection;

    const secondPromise = conn.sendFSRequest({ op: "list_dir", path: "/tmp", request_id: "fs-timeout" }, 10);
    const secondRejection = expect(secondPromise).rejects.toThrow(/retired|timed out/i);
    ws.emit(TYPE.FS_RESPONSE, { request_id: "fs-timeout", ok: true, entries: [] });
    await secondRejection;
    expect(ws.sent).toHaveLength(2);
  });

  test("ws.send exception rejects and clears the pending FS request", async () => {
    const { conn, ws } = openConnection();
    ws.throwOnSend = true;

    await expect(conn.sendFSRequest({ op: "list_dir", path: "/tmp", request_id: "fs-send-fail" })).rejects.toThrow(
      /send failed/i,
    );
    expect(ws.sent).toHaveLength(1);

    ws.throwOnSend = false;
    const retryPromise = conn.sendFSRequest({ op: "list_dir", path: "/tmp", request_id: "fs-send-fail" });
    ws.emit(TYPE.FS_RESPONSE, { request_id: "fs-send-fail", ok: true, entries: [] });
    await expect(retryPromise).resolves.toEqual({ request_id: "fs-send-fail", ok: true, entries: [] });
  });

  test("FS_EVENT invokes registered handlers and unsubscribe removes them", () => {
    const { conn, ws } = openConnection();
    const handler = vi.fn();
    const unsubscribe = conn.onFSEvent(handler);

    ws.emit(TYPE.FS_EVENT, { watch_id: "watch-1", path: "/tmp", event: "changed" });
    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler).toHaveBeenCalledWith({ watch_id: "watch-1", path: "/tmp", event: "changed" });

    unsubscribe();
    ws.emit(TYPE.FS_EVENT, { watch_id: "watch-1", path: "/tmp", event: "changed" });
    expect(handler).toHaveBeenCalledTimes(1);
  });

  test("malformed FS_EVENT is ignored", () => {
    const { conn, ws } = openConnection();
    const handler = vi.fn();
    conn.onFSEvent(handler);

    ws.emit(TYPE.FS_EVENT, { watch_id: "watch-1", path: "/tmp", event: false });

    expect(handler).not.toHaveBeenCalled();
  });

  test("throwing FS_EVENT handler does not prevent later handlers", () => {
    const { conn, ws } = openConnection();
    const first = vi.fn(() => {
      throw new Error("handler failed");
    });
    const second = vi.fn();
    conn.onFSEvent(first);
    conn.onFSEvent(second);

    expect(() => ws.emit(TYPE.FS_EVENT, { watch_id: "watch-1", path: "/tmp", event: "changed" })).not.toThrow();

    expect(first).toHaveBeenCalledTimes(1);
    expect(second).toHaveBeenCalledTimes(1);
  });

  test("sendFSRequest rejects when websocket is not open", async () => {
    const conn = new SessionConnection(endpoint, sessionId);

    await expect(conn.sendFSRequest({ op: "list_dir", path: "/tmp" })).rejects.toThrow(/websocket is not open/i);
  });

  test("pending FS request rejects on detach or close", async () => {
    const first = openConnection();
    const detachPromise = first.conn.sendFSRequest({ op: "list_dir", path: "/tmp", request_id: "fs-detach" });
    first.conn.detach();
    await expect(detachPromise).rejects.toThrow(/detached/i);

    const second = openConnection();
    const closePromise = second.conn.sendFSRequest({ op: "list_dir", path: "/tmp", request_id: "fs-close" });
    second.ws.close();
    await expect(closePromise).rejects.toThrow(/closed/i);
  });
});

describe("SessionListConnection prefs events", () => {
  const endpoint = { url: "ws://127.0.0.1:1234", session_token: "token" };

  class FakeWebSocket {
    static instances: FakeWebSocket[] = [];
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSED = 3;

    readyState = FakeWebSocket.CONNECTING;
    binaryType = "";
    onopen: (() => void) | null = null;
    onmessage: ((event: MessageEvent) => void) | null = null;
    onclose: (() => void) | null = null;
    onerror: (() => void) | null = null;

    constructor(public url: string, public protocols?: string[]) {
      FakeWebSocket.instances.push(this);
    }

    close() {
      this.readyState = FakeWebSocket.CLOSED;
      this.onclose?.();
    }

    open() {
      this.readyState = FakeWebSocket.OPEN;
      this.onopen?.();
    }

    emit(type: number, payload?: Uint8Array) {
      const bytes = encodeFrame(type as any, NIL_SID, payload ?? new Uint8Array(0));
      this.onmessage?.({ data: bytes.buffer } as MessageEvent);
    }
  }

  beforeEach(() => {
    FakeWebSocket.instances = [];
    vi.stubGlobal("WebSocket", FakeWebSocket);
  });

  afterEach(() => {
    setAccountKeyProvider(null);
    vi.unstubAllGlobals();
  });

  test("PREFS_CHANGED invokes onPrefsChanged without disturbing session list handling", () => {
    const onSessions = vi.fn();
    const onPrefsChanged = vi.fn();
    const conn = new SessionListConnection(endpoint, { onSessions, onPrefsChanged });
    conn.attach();
    const ws = FakeWebSocket.instances[0];
    ws.open();

    ws.emit(TYPE.PREFS_CHANGED);
    expect(onPrefsChanged).toHaveBeenCalledTimes(1);
    expect(onSessions).not.toHaveBeenCalled();

    ws.emit(TYPE.LIST_RESP, encodeText(JSON.stringify([baseSession("sid-1")])));
    expect(onSessions).toHaveBeenCalledTimes(1);
    expect(onSessions.mock.calls[0][0][0].id).toBe("sid-1");
  });

  test("LIST_RESP overlays sealed session fields before notifying handlers", () => {
    const uuid = "a1b2c3d4-e5f6-7890-1234-567890abcdef";
    const accountKey = new Uint8Array(32).map((_, i) => (i * 11) & 0xff);
    setAccountKeyProvider(() => accountKey);
    const sealed = sealSessionFields(accountKey, uuid, {
      title: "codex release",
      cwd: "/Users/alice/proj",
      command: "codex",
      current_command: "gh run list",
    });
    const onSessions = vi.fn();
    const conn = new SessionListConnection(endpoint, { onSessions });
    conn.attach();
    const ws = FakeWebSocket.instances[0];
    ws.open();

    ws.emit(TYPE.LIST_RESP, encodeText(JSON.stringify([baseSession(uuid, { sealed })])));

    expect(onSessions).toHaveBeenCalledTimes(1);
    expect(onSessions.mock.calls[0][0][0]).toMatchObject({
      title: "codex release",
      cwd: "/Users/alice/proj",
      command: "codex",
      current_command: "gh run list",
    });
  });
});

describe("SessionConnection driver state", () => {
  test("generates and stores a clientID per instance", () => {
    expect(source).toMatch(/clientID\s*:\s*string/);
    expect(source).toMatch(/crypto\.randomUUID\(\)/);
  });

  test("includes client_id in ATTACH payload", () => {
    expect(source).toMatch(/client_id\s*:\s*this\.clientID/);
  });

  test("parses driver_client_id from META and surfaces onDriverChange", () => {
    expect(source).toMatch(/driver_client_id/);
    expect(source).toMatch(/onDriverChange/);
  });

  test("exposes claimDriver() that sends a CLAIM_DRIVER frame", () => {
    expect(source).toMatch(/claimDriver\s*\(/);
    expect(source).toMatch(/TYPE\.CLAIM_DRIVER/);
  });
});

describe("SessionConnection suspend/resume", () => {
  const sessionId = "11111111-2222-3333-4444-555555555555";
  const endpoint = { url: "ws://127.0.0.1:1234", session_token: "token" };

  class FakeWebSocket {
    static instances: FakeWebSocket[] = [];
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSED = 3;

    readyState = FakeWebSocket.CONNECTING;
    binaryType = "";
    sent: Uint8Array[] = [];
    onopen: (() => void) | null = null;
    onmessage: ((event: MessageEvent) => void) | null = null;
    onclose: (() => void) | null = null;

    constructor(public url: string, public protocols?: string[]) {
      FakeWebSocket.instances.push(this);
    }

    send(data: Uint8Array) {
      this.sent.push(data);
    }

    close() {
      this.readyState = FakeWebSocket.CLOSED;
      this.onclose?.();
    }

    open() {
      this.readyState = FakeWebSocket.OPEN;
      this.onopen?.();
    }
  }

  beforeEach(() => {
    FakeWebSocket.instances = [];
    vi.stubGlobal("WebSocket", FakeWebSocket);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  test("suspends the websocket without losing since_seq for later resume", () => {
    const onOutput = vi.fn();
    const conn = new SessionConnection(endpoint, sessionId, { onOutput });
    conn.attach();
    const ws1 = FakeWebSocket.instances[0];
    ws1.open();

    ws1.onmessage?.({
      data: encodeFrame(TYPE.OUT, uuidParse(sessionId), encodeOutPayload(7, "hello")).buffer,
    } as MessageEvent);
    expect(onOutput).toHaveBeenCalledOnce();

    conn.suspend();
    expect(ws1.readyState).toBe(FakeWebSocket.CLOSED);
    expect(FakeWebSocket.instances).toHaveLength(1);

    conn.attach();
    const ws2 = FakeWebSocket.instances[1];
    ws2.open();

    const attach = decodeFrame(ws2.sent[0]);
    expect(JSON.parse(decodeText(attach.payload))).toMatchObject({
      session_id: sessionId,
      since_seq: 7,
    });
  });

  test("drops duplicate OUT frames instead of writing them twice", () => {
    const onOutput = vi.fn();
    const conn = new SessionConnection(endpoint, sessionId, { onOutput });
    conn.attach();
    const ws = FakeWebSocket.instances[0];
    ws.open();
    const output = "prompt\r\n";
    const frame = encodeFrame(TYPE.OUT, uuidParse(sessionId), encodeOutPayload(3, output));
    ws.onmessage?.({ data: frame.buffer } as MessageEvent);
    ws.onmessage?.({ data: frame.buffer } as MessageEvent);
    expect(onOutput).toHaveBeenCalledOnce();
    expect(Array.from(onOutput.mock.calls[0][0] as Uint8Array)).toEqual(Array.from(encodeText(output)));
  });

  test("ignores a stale suspended socket close after a new socket has attached", () => {
    class AsyncCloseWebSocket extends FakeWebSocket {
      close() {
        this.readyState = FakeWebSocket.CLOSED;
      }
    }
    vi.stubGlobal("WebSocket", AsyncCloseWebSocket);

    const conn = new SessionConnection(endpoint, sessionId);
    conn.attach();
    const ws1 = AsyncCloseWebSocket.instances[0];
    ws1.open();

    conn.suspend();
    conn.attach();
    const ws2 = AsyncCloseWebSocket.instances[1];
    ws2.open();

    ws1.onclose?.();
    conn.sendInput("x");

    expect(ws2.sent).toHaveLength(2);
    expect(decodeFrame(ws2.sent[1]).type).toBe(TYPE.IN);
  });
});

describe("SessionConnection client_name", () => {
  test("accepts a clientName option and sends it in ATTACH", () => {
    expect(source).toMatch(/options:\s*SessionConnectionOptions/);
    expect(source).toMatch(/client_name\s*:\s*this\.clientName/);
  });

  test("includes client_name in CLAIM_DRIVER payload", () => {
    expect(source).toMatch(/client_id:\s*this\.clientID,\s*client_name:\s*this\.clientName/);
  });

  test("parses driver_client_name from META and passes to onDriverChange", () => {
    expect(source).toMatch(/driver_client_name/);
    expect(source).toMatch(/newDriverName/);
  });
});

describe("SessionConnection driver reconciliation across reconnect", () => {
  const sessionId = "11111111-2222-3333-4444-555555555555";
  const endpoint = { url: "ws://127.0.0.1:1234", session_token: "token" };

  class FakeWebSocket {
    static instances: FakeWebSocket[] = [];
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSED = 3;

    readyState = FakeWebSocket.CONNECTING;
    binaryType = "";
    sent: Uint8Array[] = [];
    onopen: (() => void) | null = null;
    onmessage: ((event: MessageEvent) => void) | null = null;
    onclose: (() => void) | null = null;
    onerror: (() => void) | null = null;

    constructor(public url: string, public protocols?: string[]) {
      FakeWebSocket.instances.push(this);
    }

    send(data: Uint8Array) {
      this.sent.push(data);
    }

    close() {
      this.readyState = FakeWebSocket.CLOSED;
      this.onclose?.();
    }

    open() {
      this.readyState = FakeWebSocket.OPEN;
      this.onopen?.();
    }

    emitMeta(driverClientID: string, driverClientName = "") {
      const frame = encodeFrame(
        TYPE.META,
        uuidParse(sessionId),
        encodeText(JSON.stringify({
          driver_client_id: driverClientID,
          driver_client_name: driverClientName,
          cols: 80,
          rows: 24,
        })),
      );
      this.onmessage?.({ data: frame.buffer } as MessageEvent);
    }
  }

  beforeEach(() => {
    FakeWebSocket.instances = [];
    vi.stubGlobal("WebSocket", FakeWebSocket);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function attachAsDriver(onDriverChange = vi.fn()) {
    const conn = new SessionConnection(endpoint, sessionId, { onDriverChange });
    conn.attach();
    const ws = FakeWebSocket.instances[0];
    ws.open();
    const attach = JSON.parse(decodeText(decodeFrame(ws.sent[0]).payload));
    ws.emitMeta(attach.client_id, "this-host");
    expect(onDriverChange).toHaveBeenLastCalledWith(attach.client_id, true, "this-host");
    return { conn, ws, clientID: String(attach.client_id), onDriverChange };
  }

  test("does not reclaim when another client became driver while this tab was suspended", () => {
    const { conn, onDriverChange } = attachAsDriver();

    conn.suspend();
    conn.attach();
    const resumed = FakeWebSocket.instances[1];
    resumed.open();

    // Reconnect must wait for the relay's authoritative snapshot instead of
    // immediately replaying the stale driver role from before suspension.
    expect(resumed.sent.map((bytes) => decodeFrame(bytes).type)).toEqual([TYPE.ATTACH]);

    resumed.emitMeta("other-client", "phone");
    expect(resumed.sent.map((bytes) => decodeFrame(bytes).type)).toEqual([TYPE.ATTACH]);
    expect(onDriverChange).toHaveBeenLastCalledWith("other-client", false, "phone");

    // The old claim intent is consumed by the first authoritative META. A
    // later driverless update must not make us steal control automatically.
    resumed.emitMeta("");
    expect(resumed.sent.map((bytes) => decodeFrame(bytes).type)).toEqual([TYPE.ATTACH]);
  });

  test("reclaims once after reconnect only when the authoritative driver is empty", () => {
    const { conn, clientID } = attachAsDriver();

    conn.suspend();
    conn.attach();
    const resumed = FakeWebSocket.instances[1];
    resumed.open();
    expect(resumed.sent.map((bytes) => decodeFrame(bytes).type)).toEqual([TYPE.ATTACH]);

    resumed.emitMeta("");
    expect(resumed.sent.map((bytes) => decodeFrame(bytes).type)).toEqual([
      TYPE.ATTACH,
      TYPE.CLAIM_DRIVER,
    ]);
    const claim = JSON.parse(decodeText(decodeFrame(resumed.sent[1]).payload));
    expect(claim.client_id).toBe(clientID);

    resumed.emitMeta("");
    expect(resumed.sent.map((bytes) => decodeFrame(bytes).type)).toEqual([
      TYPE.ATTACH,
      TYPE.CLAIM_DRIVER,
    ]);
  });
});

describe("openWS sync-throw isolation", () => {
  // Regression: WebKit throws "The string did not match the expected pattern."
  // synchronously from new WebSocket(invalid). That used to unwind App.vue's
  // boot try/catch and freeze startup. Both openWS impls must trap it and
  // route through handleOpenFailure -> onStatus("error") + backoff retry.
  test("SessionListConnection.openWS wraps new WebSocket in try/catch", () => {
    const m = source.match(/class SessionListConnection[\s\S]*?(?=\n}\n)/);
    expect(m).not.toBeNull();
    expect(m![0]).toMatch(/try\s*{\s*ws\s*=\s*auth\.protocols\s*\?[\s\S]*?}\s*catch[\s\S]*?handleOpenFailure/);
  });

  test("SessionConnection.openWS wraps new WebSocket in try/catch", () => {
    const m = source.match(/class SessionConnection[\s\S]*?(?=\n}\n)/);
    expect(m).not.toBeNull();
    expect(m![0]).toMatch(/try\s*{\s*ws\s*=\s*auth\.protocols\s*\?[\s\S]*?}\s*catch[\s\S]*?handleOpenFailure/);
  });

  test("handleOpenFailure surfaces error status and schedules reconnect", () => {
    expect(source).toMatch(/handleOpenFailure\([\s\S]*?\)\s*:\s*void/);
    expect(source).toMatch(/onStatus\?\.\("error"\)/);
    expect(source).toMatch(/setTimeout\(\(\) => this\.openWS\(\), delay\)/);
  });
});

describe("decryptSessionFields", () => {
  const uuid = "a1b2c3d4-e5f6-7890-1234-567890abcdef";
  const accountKey = new Uint8Array(32).map((_, i) => (i * 11) & 0xff);

  afterEach(() => setAccountKeyProvider(null));

  test("overlays sealed title/cwd/command/current_command when key is unlocked", () => {
    setAccountKeyProvider(() => accountKey);
    const sealed = sealSessionFields(accountKey, uuid, {
      title: "atterm - claude",
      cwd: "/Users/alice/proj",
      command: "claude",
      current_command: "rg api_key",
    });
    const [out] = decryptSessionFields([baseSession(uuid, { sealed })]);
    expect(out.title).toBe("atterm - claude");
    expect(out.cwd).toBe("/Users/alice/proj");
    expect(out.command).toBe("claude");
    expect(out.current_command).toBe("rg api_key");
  });

  test("passes sessions through unchanged when account key is locked", () => {
    setAccountKeyProvider(() => null);
    const sealed = sealSessionFields(accountKey, uuid, { title: "secret" });
    const input = [baseSession(uuid, { sealed })];
    const out = decryptSessionFields(input);
    expect(out[0].title).toBe("");
  });

  test("leaves plaintext sessions (no sealed envelope) untouched", () => {
    setAccountKeyProvider(() => accountKey);
    const [out] = decryptSessionFields([baseSession(uuid, { title: "plain", command: "bash" })]);
    expect(out.title).toBe("plain");
    expect(out.command).toBe("bash");
  });

  test("keeps original fields when the sealed envelope fails to open", () => {
    setAccountKeyProvider(() => accountKey);
    const [out] = decryptSessionFields([baseSession(uuid, { title: "fallback", sealed: "bm90LXZhbGlk" })]);
    expect(out.title).toBe("fallback");
  });
});
