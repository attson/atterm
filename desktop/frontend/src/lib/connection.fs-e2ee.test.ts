import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { SessionConnection, type FSResponse } from "./connection";
import { setAccountKeyProvider } from "./account-key";
import { TYPE, decodeFrame, decodeText, encodeFrame, encodeText, uuidParse } from "./proto";
import { encodeSegments, decodeSegments } from "./fsSegments";
import { sealUnsequenced } from "./opaque";

const sessionId = "11111111-2222-3333-4444-555555555555";
const endpoint = { url: "ws://localhost:1234", session_token: "tok" };
const KEY = new Uint8Array(32).map((_, i) => i);

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  static OPEN = 1;
  readyState = 1;
  sent: Uint8Array[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;

  constructor() {
    FakeWebSocket.instances.push(this);
  }
  send(data: ArrayBuffer) {
    this.sent.push(new Uint8Array(data));
  }
  close() {
    this.readyState = 3;
  }
  open() {
    this.onopen?.();
  }
  deliver(type: number, segments: Uint8Array[]) {
    const bytes = encodeFrame(type as any, uuidParse(sessionId), encodeSegments(segments));
    this.onmessage?.({ data: bytes.buffer } as MessageEvent);
  }
}

function openConnection() {
  const conn = new SessionConnection(endpoint, sessionId);
  conn.attach();
  const ws = FakeWebSocket.instances.at(-1)!;
  ws.open();
  return { conn, ws };
}

function lastFSRequest(ws: FakeWebSocket): Uint8Array {
  const frames = ws.sent.map((b) => decodeFrame(b)).filter((f) => f.type === TYPE.FS_REQUEST);
  return frames.at(-1)!.payload;
}

beforeEach(() => {
  FakeWebSocket.instances = [];
  vi.stubGlobal("WebSocket", FakeWebSocket);
});

afterEach(() => {
  setAccountKeyProvider(null);
  vi.unstubAllGlobals();
});

describe("FS request sealing", () => {
  test("seals path into segment 1 and keeps op plaintext for the relay gate", () => {
    setAccountKeyProvider(() => KEY);
    const { conn, ws } = openConnection();
    void conn.sendFSRequest({ op: "read_file", path: "/home/u/.env", max_bytes: 1024 });

    const segs = decodeSegments(lastFSRequest(ws));
    expect(segs).not.toBeNull();
    expect(segs!.length).toBe(2);

    const head = JSON.parse(decodeText(segs![0]));
    expect(head.op).toBe("read_file");
    expect(head.max_bytes).toBe(1024);
    expect(head.path).toBeUndefined();
    expect(JSON.stringify(head)).not.toContain(".env");
  });

  test("falls back to a single plaintext segment without a key", () => {
    setAccountKeyProvider(() => null);
    const { conn, ws } = openConnection();
    void conn.sendFSRequest({ op: "list_dir", path: "/home/u" });

    const segs = decodeSegments(lastFSRequest(ws));
    expect(segs!.length).toBe(1);
    expect(JSON.parse(decodeText(segs![0])).path).toBe("/home/u");
  });
});

describe("FS response unsealing", () => {
  test("overlays sealed metadata and reattaches raw content bytes", async () => {
    setAccountKeyProvider(() => KEY);
    const { conn, ws } = openConnection();
    const pending = conn.sendFSRequest({ op: "read_file", path: "/home/u/.env", request_id: "r1" });

    const head = encodeText(JSON.stringify({ request_id: "r1", ok: true }));
    const meta = sealUnsequenced(
      KEY,
      sessionId,
      TYPE.FS_RESPONSE,
      encodeText(JSON.stringify({ content: { path: "/home/u/.env", isBinary: false } })),
    );
    const raw = sealUnsequenced(KEY, sessionId, TYPE.FS_RESPONSE, new TextEncoder().encode("SECRET=1"));
    ws.deliver(TYPE.FS_RESPONSE, [head, meta, raw]);

    const resp = (await pending) as FSResponse;
    expect(resp.ok).toBe(true);
    expect(resp.content?.path).toBe("/home/u/.env");
    // Re-base64'd so remoteSessionFS decodes it the same way as plaintext.
    expect(atob(resp.content!.data)).toBe("SECRET=1");
  });

  test("drops a sealed response when no key is available", async () => {
    setAccountKeyProvider(() => KEY);
    const { conn, ws } = openConnection();
    const pending = conn.sendFSRequest({ op: "list_dir", path: "/home/u", request_id: "r2" });

    setAccountKeyProvider(() => null);
    const head = encodeText(JSON.stringify({ request_id: "r2", ok: true }));
    const meta = sealUnsequenced(KEY, sessionId, TYPE.FS_RESPONSE, encodeText(JSON.stringify({ entries: [] })));
    ws.deliver(TYPE.FS_RESPONSE, [head, meta]);

    // Nothing resolved: the frame was dropped rather than surfacing a
    // response with its sealed fields silently missing.
    let settled = false;
    void pending.then(() => (settled = true)).catch(() => (settled = true));
    await Promise.resolve();
    expect(settled).toBe(false);
  });

  test("accepts a single-segment plaintext response while holding a key", async () => {
    setAccountKeyProvider(() => KEY);
    const { conn, ws } = openConnection();
    const pending = conn.sendFSRequest({ op: "list_dir", path: "/home/u", request_id: "r3" });

    ws.deliver(TYPE.FS_RESPONSE, [
      encodeText(JSON.stringify({ request_id: "r3", ok: true, entries: [{ name: "a.txt", isDir: false }] })),
    ]);

    const resp = (await pending) as FSResponse;
    expect(resp.entries).toEqual([{ name: "a.txt", isDir: false }]);
  });
});

describe("FS event unsealing", () => {
  test("recovers the sealed path", () => {
    setAccountKeyProvider(() => KEY);
    const { conn, ws } = openConnection();
    const seen: string[] = [];
    conn.onFSEvent((e) => seen.push(e.path));

    const head = encodeText(JSON.stringify({ watch_id: "w1", event: "changed" }));
    const sealed = sealUnsequenced(
      KEY,
      sessionId,
      TYPE.FS_EVENT,
      encodeText(JSON.stringify({ path: "/home/u/secrets" })),
    );
    ws.deliver(TYPE.FS_EVENT, [head, sealed]);

    expect(seen).toEqual(["/home/u/secrets"]);
  });

  test("drops an event sealed under the wrong frame type", () => {
    setAccountKeyProvider(() => KEY);
    const { conn, ws } = openConnection();
    const seen: string[] = [];
    conn.onFSEvent((e) => seen.push(e.path));

    const head = encodeText(JSON.stringify({ watch_id: "w1", event: "changed" }));
    // Sealed as a RESPONSE — AAD mismatch must make this unopenable.
    const sealed = sealUnsequenced(
      KEY,
      sessionId,
      TYPE.FS_RESPONSE,
      encodeText(JSON.stringify({ path: "/home/u/secrets" })),
    );
    ws.deliver(TYPE.FS_EVENT, [head, sealed]);

    expect(seen).toEqual([]);
  });
});
