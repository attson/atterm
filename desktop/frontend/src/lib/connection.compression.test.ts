import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DecompressionStream, TransformStream } from "node:stream/web";
import { deflateRawSync } from "node:zlib";
import { xchacha20poly1305 } from "@noble/ciphers/chacha.js";
import { SessionConnection, type ConnectionHandlers } from "./connection";
import { hkdf } from "@noble/hashes/hkdf.js";
import { sha256 } from "@noble/hashes/sha2.js";
import { setAccountKeyProvider } from "./account-key";
import * as opaque from "./opaque";
import { TYPE, decodeFrame, decodeText, encodeFrame, encodeText, uuidParse, type FrameType } from "./proto";

const sid = "11111111-2222-3333-4444-555555555555";
const key = new Uint8Array(32).fill(42);
const endpoint = { url: "wss://relay.example", session_token: "token" };
const OUT_COMPRESSED = 0x43 as FrameType;

function encoded(codec: number, expected: number, body: Uint8Array): Uint8Array {
  const bytes = new Uint8Array(body.length + 5);
  bytes[0] = codec;
  new DataView(bytes.buffer).setUint32(1, expected, false);
  bytes.set(body, 5);
  return bytes;
}

function outputPayload(seq: number, data: Uint8Array): Uint8Array {
  const bytes = new Uint8Array(8 + data.length);
  new DataView(bytes.buffer).setBigUint64(0, BigInt(seq), false);
  bytes.set(data, 8);
  return bytes;
}

function sealedOutput(seq: number, plaintext: Uint8Array, type = 0x43): Uint8Array {
  const nonce = new Uint8Array(24).fill(17);
  const aad = new Uint8Array(25);
  aad.set(uuidParse(sid));
  aad[16] = type;
  new DataView(aad.buffer).setBigUint64(17, BigInt(seq), false);
  const prefix = encodeText("atterm-session-v1");
  const info = new Uint8Array(prefix.length + 16);
  info.set(prefix);
  info.set(uuidParse(sid), prefix.length);
  const sessionKey = hkdf(sha256, key, undefined, info, 32);
  const ciphertext = xchacha20poly1305(sessionKey, nonce, aad).encrypt(plaintext);
  const envelope = new Uint8Array(ciphertext.length + 25);
  envelope[0] = 1;
  envelope.set(nonce, 1);
  envelope.set(ciphertext, 25);
  return outputPayload(seq, envelope);
}

function compressed(seq: number, text: string): Uint8Array {
  const data = encodeText(text);
  return sealedOutput(seq, encoded(1, data.length, deflateRawSync(data)));
}

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  static OPEN = 1;
  readyState = 0;
  binaryType = "";
  sent: Uint8Array[] = [];
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: (() => void) | null = null;
  constructor() { FakeWebSocket.instances.push(this); }
  send(bytes: Uint8Array): void { this.sent.push(bytes); }
  open(): void { this.readyState = 1; this.onopen?.(); }
  close(): void { this.readyState = 3; this.onclose?.(); }
  emit(type: FrameType, body: Uint8Array): void {
    this.onmessage?.({ data: encodeFrame(type, uuidParse(sid), body).buffer } as MessageEvent);
  }
  json(type: FrameType, body: unknown): void { this.emit(type, encodeText(JSON.stringify(body))); }
  attachPayload(): Record<string, unknown> { return JSON.parse(decodeText(decodeFrame(this.sent[0]).payload)); }
}

function connect(handlers: ConnectionHandlers = {}, remote = true): { conn: SessionConnection; ws: FakeWebSocket } {
  const conn = new SessionConnection(endpoint, sid, handlers, { remote });
  conn.attach();
  const ws = FakeWebSocket.instances[FakeWebSocket.instances.length - 1];
  ws.open();
  return { conn, ws };
}

function delayedInflation(): () => void {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => { release = resolve; });
  vi.stubGlobal("DecompressionStream", class {
    readonly writable;
    readonly readable;
    constructor(format: "deflate-raw") {
      const native = new DecompressionStream(format);
      this.writable = native.writable;
      this.readable = native.readable.pipeThrough(new TransformStream<Uint8Array, Uint8Array>({
        async transform(chunk, controller) { await gate; controller.enqueue(chunk); },
      }));
    }
  });
  return release;
}

beforeEach(() => {
  FakeWebSocket.instances = [];
  vi.stubGlobal("WebSocket", FakeWebSocket);
  vi.stubGlobal("DecompressionStream", DecompressionStream);
  setAccountKeyProvider(() => key);
});
afterEach(() => {
  vi.restoreAllMocks();
  setAccountKeyProvider(null);
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("compressed terminal connection", () => {
  it("advertises compression only for remote, unlocked clients with native raw DEFLATE", () => {
    const supported = connect();
    expect(supported.ws.attachPayload().output_compression).toBe("deflate-raw-v1");
    supported.conn.detach();
    const local = connect({}, false);
    expect(local.ws.attachPayload()).not.toHaveProperty("output_compression");
    local.conn.detach();
    setAccountKeyProvider(null);
    const locked = connect();
    expect(locked.ws.attachPayload()).not.toHaveProperty("output_compression");
    locked.conn.detach();
    setAccountKeyProvider(() => key);
    vi.stubGlobal("DecompressionStream", class { constructor() { throw new TypeError("unsupported"); } });
    const unsupported = connect();
    expect(unsupported.ws.attachPayload()).not.toHaveProperty("output_compression");
    unsupported.conn.detach();
  });

  it("keeps output, metadata, replay progress and close ordered behind asynchronous inflation", async () => {
    const release = delayedInflation();
    const events: string[] = [];
    const { conn, ws } = connect({
      onOutput: (bytes) => events.push(decodeText(bytes)),
      onMeta: () => events.push("meta"),
      onReplayProgress: () => events.push("replay"),
      onClose: () => events.push("close"),
    });
    ws.emit(OUT_COMPRESSED, compressed(1, "first"));
    ws.json(TYPE.META, { cols: 80, rows: 24 });
    ws.emit(OUT_COMPRESSED, compressed(2, "second"));
    ws.emit(TYPE.OUT, outputPayload(3, encodeText("legacy")));
    ws.json(TYPE.REPLAY_PROGRESS, { phase: "end" });
    ws.json(TYPE.CLOSE, { exit_code: 0 });
    expect(events).toEqual([]);
    release();
    await vi.waitFor(() => expect(events).toEqual(["first", "meta", "second", "legacy", "replay", "close"]));
    conn.detach();
  });

  it("drops pending inflation and queued controls after suspension and lets the new socket proceed", async () => {
    const release = delayedInflation();
    const events: string[] = [];
    const { conn, ws } = connect({ onOutput: (bytes) => events.push(decodeText(bytes)), onMeta: () => events.push("stale meta") });
    ws.emit(OUT_COMPRESSED, compressed(1, "stale"));
    ws.json(TYPE.META, { title: "old" });
    conn.suspend();
    conn.attach();
    const current = FakeWebSocket.instances[1];
    current.open();
    expect(current.attachPayload().since_seq).toBe(0);
    current.emit(TYPE.OUT, outputPayload(2, encodeText("current")));
    expect(events).toEqual(["current"]);
    release();
    await new Promise((resolve) => setTimeout(resolve, 30));
    expect(events).toEqual(["current"]);
    conn.suspend();
    conn.attach();
    FakeWebSocket.instances[2].open();
    expect(FakeWebSocket.instances[2].attachPayload().since_seq).toBe(2);
    conn.detach();
  });

  it("preserves pending relay output and controls when a direct probe is aborted", async () => {
    const release = delayedInflation();
    const events: string[] = [];
    const conn = new SessionConnection(endpoint, sid, {
      onOutput: (bytes) => events.push(decodeText(bytes)),
      onMeta: () => events.push("meta"),
      onClose: () => events.push("close"),
    }, {
      remote: true, preferDirect: true, directEndpoint: endpoint,
      directTransportFactory: () => ({ start() {}, sendFrame() { return true; }, close() {} }),
    });
    conn.attach();
    const ws = FakeWebSocket.instances[0];
    ws.open();
    ws.json(TYPE.REPLAY_PROGRESS, { phase: "end" });
    ws.emit(OUT_COMPRESSED, compressed(1, "original"));
    ws.json(TYPE.META, { title: "original metadata" });
    conn.setPreferDirect(false);
    ws.emit(TYPE.OUT, outputPayload(2, encodeText("next")));
    ws.json(TYPE.CLOSE, { exit_code: 0 });
    expect(events).toEqual([]);
    release();
    await vi.waitFor(() => expect(events).toEqual(["original", "meta", "next", "close"]));
    conn.suspend();
    conn.attach();
    FakeWebSocket.instances[1].open();
    expect(FakeWebSocket.instances[1].attachPayload().since_seq).toBe(2);
    conn.detach();
  });

  it("does not deliver or advance the cursor after detach during inflation", async () => {
    const release = delayedInflation();
    const onOutput = vi.fn();
    const { conn, ws } = connect({ onOutput });
    ws.emit(OUT_COMPRESSED, compressed(1, "stale"));
    conn.detach();
    release();
    await new Promise((resolve) => setTimeout(resolve, 30));
    expect(onOutput).not.toHaveBeenCalled();
  });

  it("only commits authenticated and decoded output, allowing a valid retransmission of the same sequence", async () => {
    const outputs: string[] = [];
    const { conn, ws } = connect({ onOutput: (bytes) => outputs.push(decodeText(bytes)) });
    const corrupt = sealedOutput(7, encodeText("bad"), 0x03);
    corrupt[corrupt.length - 1] ^= 1;
    ws.emit(TYPE.OUT, corrupt);
    ws.emit(TYPE.OUT, sealedOutput(7, encodeText("valid ordinary"), 0x03));
    expect(outputs).toEqual(["valid ordinary"]);
    ws.emit(OUT_COMPRESSED, sealedOutput(8, encoded(9, 0, new Uint8Array())));
    ws.emit(OUT_COMPRESSED, sealedOutput(8, encoded(1, 65537, new Uint8Array())));
    ws.emit(OUT_COMPRESSED, sealedOutput(8, encoded(1, 5, new Uint8Array([255]))));
    ws.emit(OUT_COMPRESSED, sealedOutput(8, encoded(0, 5, encodeText("valid"))));
    await vi.waitFor(() => expect(outputs).toEqual(["valid ordinary", "valid"]));
    conn.suspend();
    conn.attach();
    FakeWebSocket.instances[1].open();
    expect(FakeWebSocket.instances[1].attachPayload().since_seq).toBe(8);
    conn.detach();
  });

  it("rejects oversized compressed envelopes before authentication without committing their sequence", async () => {
    const onOutput = vi.fn();
    const open = vi.spyOn(opaque, "openCompressedOutFrame");
    const { conn, ws } = connect({ onOutput });
    const oversized = sealedOutput(7, encoded(0, 65536, new Uint8Array(65537)));
    ws.emit(OUT_COMPRESSED, oversized);
    ws.emit(OUT_COMPRESSED, new Uint8Array(65591));
    expect(open).not.toHaveBeenCalled();
    expect(onOutput).not.toHaveBeenCalled();
    const maximum = new Uint8Array(65536).fill(65);
    ws.emit(OUT_COMPRESSED, sealedOutput(7, encoded(0, maximum.length, maximum)));
    await vi.waitFor(() => expect(onOutput).toHaveBeenCalledOnce());
    expect(open).toHaveBeenCalledOnce();
    expect(Array.from(onOutput.mock.calls[0][0] as Uint8Array)).toEqual(Array.from(maximum));
    conn.suspend();
    conn.attach();
    FakeWebSocket.instances[1].open();
    expect(FakeWebSocket.instances[1].attachPayload().since_seq).toBe(7);
    conn.detach();
    open.mockRestore();
  });

  it("preserves synchronous ordinary OUT and sequence-zero resets for legacy peers", () => {
    const outputs: string[] = [];
    const { conn, ws } = connect({ onOutput: (bytes) => outputs.push(decodeText(bytes)) });
    ws.emit(TYPE.OUT, outputPayload(0, encodeText("reset")));
    ws.emit(TYPE.OUT, outputPayload(1, encodeText("legacy")));
    expect(outputs).toEqual(["reset", "legacy"]);
    conn.detach();
  });
});
