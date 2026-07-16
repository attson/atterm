import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { hkdf } from "@noble/hashes/hkdf.js";
import { sha256 } from "@noble/hashes/sha2.js";
import { xchacha20poly1305 } from "@noble/ciphers/chacha.js";
import { utf8ToBytes, randomBytes } from "@noble/hashes/utils.js";
import source from "./connection.ts?raw";
import { decryptSessionFields, SessionConnection, type SessionInfo } from "./connection";
import { setAccountKeyProvider } from "./account-key";
import type { SealedSessionFields } from "./opaque";
import { TYPE, decodeFrame, decodeText, encodeFrame, encodeText, uuidParse } from "./proto";

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
      const bytes = encodeFrame(type as any, uuidParse(sessionId), encodeText(JSON.stringify(payload)));
      this.onmessage?.({ data: bytes.buffer } as MessageEvent);
    }
  }

  beforeEach(() => {
    FakeWebSocket.instances = [];
    vi.stubGlobal("WebSocket", FakeWebSocket);
  });

  afterEach(() => {
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
    expect(JSON.parse(decodeText(frame.payload))).toEqual({
      op: "list_dir",
      path: "/tmp",
      request_id: "fs-test-1",
    });

    ws.emit(TYPE.FS_RESPONSE, { request_id: "fs-test-1", ok: true, entries: [{ name: "a.txt" }] });

    await expect(responsePromise).resolves.toEqual({
      request_id: "fs-test-1",
      ok: true,
      entries: [{ name: "a.txt" }],
    });
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

    ws.emit(TYPE.FS_RESPONSE, { request_id: "fs-test-2", ok: true, meta: { size: 12 } });
    await expect(responsePromise).resolves.toEqual({ request_id: "fs-test-2", ok: true, meta: { size: 12 } });
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

describe("SessionConnection re-claims driver across reconnect", () => {
  // After an idle reconnect the host recreates its uplink proxy subscriber; with
  // auto-promote suppressed the session has no driver until someone claims. The
  // frontend must re-assert its claim on reconnect, or the user silently drops
  // to viewer (and previously saw a spurious "remote has taken control").
  test("tracks whether this connection currently holds the driver role", () => {
    expect(source).toMatch(/this\.isDriverRole\s*=/);
  });

  test("re-sends CLAIM_DRIVER in SessionConnection.onopen when it held the driver role", () => {
    const m = source.match(/class SessionConnection[\s\S]*?(?=\n}\n)/);
    expect(m).not.toBeNull();
    expect(m![0]).toMatch(/if\s*\(this\.isDriverRole\)\s*this\.claimDriver\(\)/);
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
