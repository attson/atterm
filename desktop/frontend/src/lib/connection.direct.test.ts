import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { setAccountKeyProvider } from "./account-key";
import {
  SessionConnection,
  type DirectTransport,
  type Endpoint,
} from "./connection";
import type { DirectClientOptions } from "./directClient";
import {
  TYPE,
  decodeFrame,
  decodeText,
  encodeFrame,
  encodeText,
  uuidParse,
} from "./proto";

const sessionId = "11111111-2222-3333-4444-555555555555";
const relayEndpoint: Endpoint = { url: "ws://127.0.0.1:1234", session_token: "relay-token" };
const directEndpoint: Endpoint = { url: "wss://relay.example", session_token: "relay-token" };

function encodeOutPayload(seq: number, text: string): Uint8Array {
  const data = encodeText(text);
  const payload = new Uint8Array(8 + data.length);
  const view = new DataView(payload.buffer);
  view.setUint32(0, Math.floor(seq / 0x100000000), false);
  view.setUint32(4, seq >>> 0, false);
  payload.set(data, 8);
  return payload;
}

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
  onclose: ((event?: CloseEvent) => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(public url: string, public protocols?: string[]) {
    FakeWebSocket.instances.push(this);
  }

  send(data: Uint8Array): void {
    this.sent.push(data);
  }

  close(): void {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.();
  }

  open(): void {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.();
  }

  emit(type: number, payload: Uint8Array): void {
    const bytes = encodeFrame(type as (typeof TYPE)[keyof typeof TYPE], uuidParse(sessionId), payload);
    this.onmessage?.({ data: bytes.buffer } as MessageEvent);
  }

  emitJSON(type: number, value: unknown): void {
    this.emit(type, encodeText(JSON.stringify(value)));
  }
}

class FakeDirectTransport implements DirectTransport {
  sent: Uint8Array[] = [];
  started = false;
  closed = false;

  constructor(readonly options: DirectClientOptions) {}

  start(): void {
    this.started = true;
  }

  sendFrame(frame: Uint8Array): boolean {
    if (this.closed) return false;
    this.sent.push(frame);
    return true;
  }

  close(): void {
    this.closed = true;
  }

  authenticate(): void {
    this.options.callbacks.onAuthenticated?.();
  }

  emitFrame(type: number, payload: Uint8Array): void {
    this.options.callbacks.onFrame(
      encodeFrame(type as (typeof TYPE)[keyof typeof TYPE], uuidParse(sessionId), payload),
    );
  }

  ready(seq: number): void {
    this.options.callbacks.onReady(seq);
  }

  fail(message = "direct lost"): void {
    this.closed = true;
    this.options.callbacks.onFailure(new Error(message));
  }
}

describe("SessionConnection direct route handover", () => {
  let directInstances: FakeDirectTransport[];
  let directFactory: (options: DirectClientOptions) => DirectTransport;

  beforeEach(() => {
    FakeWebSocket.instances = [];
    directInstances = [];
    directFactory = (options) => {
      const transport = new FakeDirectTransport(options);
      directInstances.push(transport);
      return transport;
    };
    vi.stubGlobal("WebSocket", FakeWebSocket);
    setAccountKeyProvider(() => new Uint8Array(32).fill(0x42));
  });

  afterEach(() => {
    setAccountKeyProvider(null);
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  test("stays Relay-only unless preferDirect is explicitly enabled", () => {
    const conn = new SessionConnection(relayEndpoint, sessionId, {}, {
      remote: true,
      directEndpoint,
      directTransportFactory: directFactory,
    });
    conn.attach();
    const relay = FakeWebSocket.instances[0];
    relay.open();
    relay.emitJSON(TYPE.REPLAY_PROGRESS, { phase: "end", seq: 0 });

    expect(directInstances).toHaveLength(0);
    expect(relay.readyState).toBe(FakeWebSocket.OPEN);
  });

  test("keeps takeover on Relay when the runtime has no WebRTC", () => {
    const routes: string[] = [];
    const conn = new SessionConnection(relayEndpoint, sessionId, {
      onRouteChange: (diagnostics) => routes.push(`${diagnostics.route}:${diagnostics.fallbackReason ?? ""}`),
    }, {
      clientName: "viewer-device",
      remote: true,
      preferDirect: true,
      directEndpoint,
    });

    conn.attach();
    const relay = FakeWebSocket.instances[0];
    relay.open();
    relay.emitJSON(TYPE.REPLAY_PROGRESS, { phase: "end", seq: 0 });

    expect(FakeWebSocket.instances).toHaveLength(1);
    expect(routes.at(-1)).toBe("relay:webrtc_unavailable");
    expect(conn.claimDriver()).toBe(true);
    expect(decodeFrame(relay.sent.at(-1)!).type).toBe(TYPE.CLAIM_DRIVER);
  });

  test("queues a takeover while Relay reconnects and sends it before input", () => {
    vi.useFakeTimers();
    const conn = new SessionConnection(relayEndpoint, sessionId, {}, { remote: true });
    conn.attach();
    const firstRelay = FakeWebSocket.instances[0];
    firstRelay.open();
    firstRelay.emitJSON(TYPE.REPLAY_PROGRESS, { phase: "end", seq: 0 });
    firstRelay.close();

    expect(conn.claimDriver()).toBe(false);
    vi.runAllTimers();
    const reconnectedRelay = FakeWebSocket.instances[1];
    conn.sendInput("after-takeover");
    reconnectedRelay.open();

    expect(reconnectedRelay.sent.map((frame) => decodeFrame(frame).type)).toEqual([
      TYPE.ATTACH,
      TYPE.CLAIM_DRIVER,
      TYPE.IN,
    ]);
  });

  test("deduplicates overlap, transfers driver, and resumes Relay after direct loss", () => {
    const output: string[] = [];
    const statuses: string[] = [];
    const routes: string[] = [];
    const conn = new SessionConnection(relayEndpoint, sessionId, {
      onOutput: (bytes) => output.push(decodeText(bytes)),
      onStatus: (status) => statuses.push(status),
      onRouteChange: (diagnostics) => routes.push(`${diagnostics.route}:${diagnostics.fallbackReason ?? ""}`),
    }, {
      clientName: "viewer-device",
      remote: true,
      preferDirect: true,
      directEndpoint,
      directTransportFactory: directFactory,
    });

    conn.attach();
    const relay = FakeWebSocket.instances[0];
    relay.open();
    const attach = JSON.parse(decodeText(decodeFrame(relay.sent[0]).payload)) as { client_id: string };
    relay.emitJSON(TYPE.META, { driver_client_id: attach.client_id, driver_client_name: "viewer-device" });
    relay.emit(TYPE.OUT, encodeOutPayload(10, "ten"));
    relay.emitJSON(TYPE.REPLAY_PROGRESS, { phase: "end", seq: 10 });

    expect(directInstances).toHaveLength(1);
    const direct = directInstances[0];
    expect(direct.started).toBe(true);
    expect(direct.options.signalURL).toBe("wss://relay.example/direct-signal");
    expect(direct.options.sinceSeq).toBe(10);
    expect(direct.options.clientInstanceId).toBe(attach.client_id);
    expect(routes.at(-1)).toBe("connecting-direct:");

    direct.authenticate();
    relay.emit(TYPE.OUT, encodeOutPayload(11, "eleven"));
    relay.emit(TYPE.OUT, encodeOutPayload(12, "twelve"));
    direct.ready(10);
    expect(relay.readyState).toBe(FakeWebSocket.OPEN);

    direct.emitFrame(TYPE.OUT, encodeOutPayload(11, "duplicate-eleven"));
    expect(relay.readyState).toBe(FakeWebSocket.OPEN);
    direct.emitFrame(TYPE.OUT, encodeOutPayload(12, "duplicate-twelve"));

    expect(output).toEqual(["ten", "eleven", "twelve"]);
    expect(relay.readyState).toBe(FakeWebSocket.CLOSED);
    expect(decodeFrame(direct.sent[0]).type).toBe(TYPE.CLAIM_DRIVER);
    expect(routes.at(-1)).toBe("direct:");
    expect(JSON.parse(decodeText(decodeFrame(direct.sent[0]).payload))).toEqual({
      client_id: attach.client_id,
      client_name: "viewer-device",
    });

    conn.sendInput("direct-input");
    expect(decodeFrame(direct.sent[1]).type).toBe(TYPE.IN);
    expect(decodeText(decodeFrame(direct.sent[1]).payload)).toBe("direct-input");

    direct.fail();
    expect(routes.at(-1)).toBe("relay:direct_disconnected");
    expect(FakeWebSocket.instances).toHaveLength(2);
    const fallback = FakeWebSocket.instances[1];
    conn.sendInput("queued-during-fallback");
    fallback.open();
    expect(JSON.parse(decodeText(decodeFrame(fallback.sent[0]).payload))).toMatchObject({
      session_id: sessionId,
      since_seq: 12,
    });
    expect(fallback.sent).toHaveLength(1);

    fallback.emitJSON(TYPE.META, { driver_client_id: "", driver_client_name: "" });
    expect(fallback.sent).toHaveLength(1);
    fallback.emitJSON(TYPE.REPLAY_PROGRESS, { phase: "end", seq: 12 });

    expect(fallback.sent.map((frame) => decodeFrame(frame).type)).toEqual([
      TYPE.ATTACH,
      TYPE.CLAIM_DRIVER,
      TYPE.IN,
    ]);
    expect(decodeText(decodeFrame(fallback.sent[2]).payload)).toBe("queued-during-fallback");
    expect(statuses.at(-1)).toBe("attached");
  });

  test("keeps Relay as the writer when a direct attempt fails before activation", () => {
    const conn = new SessionConnection(relayEndpoint, sessionId, {}, {
      remote: true,
      preferDirect: true,
      directEndpoint,
      directTransportFactory: directFactory,
    });
    conn.attach();
    const relay = FakeWebSocket.instances[0];
    relay.open();
    relay.emitJSON(TYPE.REPLAY_PROGRESS, { phase: "end", seq: 0 });
    directInstances[0].fail("ICE unavailable");

    conn.sendInput("relay-input");
    expect(FakeWebSocket.instances).toHaveLength(1);
    expect(decodeFrame(relay.sent.at(-1)!).type).toBe(TYPE.IN);
    expect(decodeText(decodeFrame(relay.sent.at(-1)!).payload)).toBe("relay-input");
  });

  test("can enable after Relay replay and disable an active direct route", () => {
    const routes: string[] = [];
    const conn = new SessionConnection(relayEndpoint, sessionId, {
      onRouteChange: (diagnostics) => routes.push(`${diagnostics.route}:${diagnostics.fallbackReason ?? ""}`),
    }, {
      remote: true,
      directEndpoint,
      directTransportFactory: directFactory,
    });
    conn.attach();
    const relay = FakeWebSocket.instances[0];
    relay.open();
    relay.emitJSON(TYPE.REPLAY_PROGRESS, { phase: "end", seq: 0 });

    conn.setPreferDirect(true);
    expect(directInstances).toHaveLength(1);
    directInstances[0].authenticate();
    directInstances[0].ready(0);
    expect(routes.at(-1)).toBe("direct:");

    conn.setPreferDirect(false);
    expect(directInstances[0].closed).toBe(true);
    expect(routes.at(-1)).toBe("relay:preference_disabled");
    expect(FakeWebSocket.instances).toHaveLength(2);
  });
});
