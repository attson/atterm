// SessionConnection wraps a WebSocket attach to a single session, handling
// ATTACH on open, OUT/CLOSE/META decoding, and exponential-backoff reconnect
// with sinceSeq replay.

import {
  TYPE,
  encodeFrame,
  decodeFrame,
  decodeOutPayload,
  decodeText,
  encodeText,
  encodeResize,
  uuidParse,
} from "./proto";
import type { ReplayProgress } from "./replayProgress";

export interface ClosePayload {
  exit_code: number;
  reason?: string;
}

export type Status = "connecting" | "attached" | "reconnecting" | "ended" | "error";

export interface Endpoint {
  url: string; // ws://host:port (no trailing slash)
  token: string;
}

export interface ConnectionHandlers {
  onOutput?: (data: Uint8Array) => void;
  onClose?: (info: ClosePayload) => void;
  onMeta?: (meta: { cwd?: string; title?: string }) => void;
  onStatus?: (s: Status) => void;
  onReplayProgress?: (progress: ReplayProgress) => void;
}

export interface SessionListHandlers {
  onSessions: (sessions: SessionInfo[]) => void;
  onStatus?: (s: Status) => void;
}

const MAX_PASTE_IMAGE_BYTES = 10 * 1024 * 1024;
const SUBPROTOCOL_SAFE = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;

export function pasteImageBlockReason(wsReadyState: number | undefined, blobSize: number): string | null {
  if (wsReadyState !== WebSocket.OPEN) return "websocket is not open";
  if (blobSize > MAX_PASTE_IMAGE_BYTES) {
    return `image too large: ${blobSize} bytes exceeds ${MAX_PASTE_IMAGE_BYTES}`;
  }
  return null;
}

function arrayBufferToBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let out = "";
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    out += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return btoa(out);
}

function stringToBase64URL(value: string): string {
  return arrayBufferToBase64(new TextEncoder().encode(value).buffer)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/g, "");
}

function tokenSubprotocol(token: string): string | undefined {
  if (!token) return undefined;
  if (SUBPROTOCOL_SAFE.test(token)) return `atterm-token.${token}`;
  return `atterm-token-b64.${stringToBase64URL(token)}`;
}

export function webSocketAuth(endpoint: Endpoint, path: string): { url: string; protocols?: string[] } {
  const base = endpoint.url.replace(/\/$/, "");
  const protocol = tokenSubprotocol(endpoint.token);
  return {
    url: `${base}${path}`,
    protocols: protocol ? [protocol] : undefined,
  };
}

export class SessionListConnection {
  private ws: WebSocket | null = null;
  private reconnectAttempts = 0;
  private reconnectTimer: number | null = null;
  private detached = false;

  constructor(
    private endpoint: Endpoint,
    private handlers: SessionListHandlers,
  ) {}

  attach(): void {
    if (this.detached) return;
    this.openWS();
  }

  detach(): void {
    this.detached = true;
    if (this.reconnectTimer !== null) {
      window.clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      try {
        this.ws.close();
      } catch {
        /* ignore */
      }
      this.ws = null;
    }
  }

  private openWS(): void {
    if (this.detached) return;
    const auth = webSocketAuth(this.endpoint, "/client-sessions");
    const ws = auth.protocols ? new WebSocket(auth.url, auth.protocols) : new WebSocket(auth.url);
    ws.binaryType = "arraybuffer";
    this.ws = ws;
    this.handlers.onStatus?.(this.reconnectAttempts === 0 ? "connecting" : "reconnecting");

    ws.onopen = () => {
      this.reconnectAttempts = 0;
      this.handlers.onStatus?.("attached");
    };

    ws.onmessage = (ev: MessageEvent) => {
      let f;
      try {
        f = decodeFrame(new Uint8Array(ev.data as ArrayBuffer));
      } catch {
        return;
      }
      if (f.type !== TYPE.LIST_RESP) return;
      try {
        this.handlers.onSessions(JSON.parse(decodeText(f.payload)) as SessionInfo[]);
      } catch {
        /* ignore malformed snapshots */
      }
    };

    ws.onclose = () => {
      this.ws = null;
      if (this.detached) return;
      this.handlers.onStatus?.("reconnecting");
      const delay = Math.min(8000, 500 * Math.pow(2, this.reconnectAttempts++));
      this.reconnectTimer = window.setTimeout(() => this.openWS(), delay);
    };

    ws.onerror = () => {
      // onclose follows
    };
  }
}

export class SessionConnection {
  private ws: WebSocket | null = null;
  private sidBytes: Uint8Array;
  private lastSeq = 0;
  private reconnectAttempts = 0;
  private reconnectTimer: number | null = null;
  private detached = false;
  // Latest pending resize request whose WS write was deferred (WS still in
  // CONNECTING state). Flushed in ws.onopen right after the ATTACH frame.
  // Only the most recent request is kept; earlier ones are stale.
  private pendingResize: { cols: number; rows: number } | null = null;

  constructor(
    private endpoint: Endpoint,
    private sessionId: string,
    private handlers: ConnectionHandlers = {}
  ) {
    this.sidBytes = uuidParse(sessionId);
  }

  attach(): void {
    if (this.detached) return;
    this.openWS();
  }

  detach(): void {
    this.detached = true;
    if (this.reconnectTimer !== null) {
      window.clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      try {
        this.ws.close();
      } catch {
        /* ignore */
      }
      this.ws = null;
    }
  }

  sendInput(s: string): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    this.ws.send(encodeFrame(TYPE.IN, this.sidBytes, encodeText(s)));
  }

  async sendPasteImage(blob: Blob, filename = "clipboard-image"): Promise<boolean> {
    const ws = this.ws;
    const blocked = pasteImageBlockReason(ws?.readyState, blob.size);
    if (blocked || !ws) {
      this.handlers.onStatus?.("error");
      throw new Error(blocked ?? "websocket is not open");
    }
    const payload = encodeText(JSON.stringify({
      filename,
      content_type: blob.type || "image/png",
      data: arrayBufferToBase64(await blob.arrayBuffer()),
    }));
    console.info("[AT Term] sending paste image", {
      filename,
      contentType: blob.type || "image/png",
      bytes: blob.size,
    });
    ws.send(encodeFrame(TYPE.PASTE_IMAGE, this.sidBytes, payload));
    return true;
  }

  sendResize(cols: number, rows: number): void {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(encodeFrame(TYPE.RESIZE, this.sidBytes, encodeResize(cols, rows)));
      this.pendingResize = null;
      return;
    }
    // WS not open yet (initial CONNECTING, or mid-reconnect). Stash and
    // flush in ws.onopen below — otherwise the size we just learned never
    // reaches the relay and the PTY drifts from xterm's view.
    this.pendingResize = { cols, rows };
  }

  private openWS(): void {
    if (this.detached) return;
    const auth = webSocketAuth(this.endpoint, "/client");
    const ws = auth.protocols ? new WebSocket(auth.url, auth.protocols) : new WebSocket(auth.url);
    ws.binaryType = "arraybuffer";
    this.ws = ws;
    this.handlers.onStatus?.(this.reconnectAttempts === 0 ? "connecting" : "reconnecting");

    ws.onopen = () => {
      this.reconnectAttempts = 0;
      this.handlers.onStatus?.("attached");
      const attachPayload = encodeText(
        JSON.stringify({ session_id: this.sessionId, since_seq: this.lastSeq })
      );
      ws.send(encodeFrame(TYPE.ATTACH, this.sidBytes, attachPayload));
      // Flush any resize that arrived while WS was still CONNECTING. Order
      // matters: ATTACH first, RESIZE after, so the relay applies the size
      // to the right session subscription.
      if (this.pendingResize) {
        const { cols, rows } = this.pendingResize;
        this.pendingResize = null;
        ws.send(encodeFrame(TYPE.RESIZE, this.sidBytes, encodeResize(cols, rows)));
      }
    };

    ws.onmessage = (ev: MessageEvent) => {
      let f;
      try {
        f = decodeFrame(new Uint8Array(ev.data as ArrayBuffer));
      } catch {
        return;
      }
      if (f.type === TYPE.OUT) {
        const { seq, data } = decodeOutPayload(f.payload);
        this.handlers.onOutput?.(data);
        if (seq > this.lastSeq) this.lastSeq = seq;
      } else if (f.type === TYPE.CLOSE) {
        try {
          const info = JSON.parse(decodeText(f.payload)) as ClosePayload;
          this.handlers.onClose?.(info);
        } catch {
          this.handlers.onClose?.({ exit_code: 0 });
        }
        this.handlers.onStatus?.("ended");
      } else if (f.type === TYPE.META) {
        try {
          const meta = JSON.parse(decodeText(f.payload));
          this.handlers.onMeta?.(meta);
        } catch {
          /* ignore */
        }
      } else if (f.type === TYPE.REPLAY_PROGRESS) {
        try {
          this.handlers.onReplayProgress?.(JSON.parse(decodeText(f.payload)) as ReplayProgress);
        } catch {
          /* ignore */
        }
      }
    };

    ws.onclose = () => {
      this.ws = null;
      if (this.detached) return;
      this.handlers.onStatus?.("reconnecting");
      const delay = Math.min(8000, 500 * Math.pow(2, this.reconnectAttempts++));
      this.reconnectTimer = window.setTimeout(() => this.openWS(), delay);
    };

    ws.onerror = () => {
      // onclose follows; nothing to do here
    };
  }
}

export async function fetchSessions(endpoint: Endpoint): Promise<SessionInfo[]> {
  const httpUrl = endpoint.url.replace(/^ws/, "http").replace(/\/$/, "");
  const url = `${httpUrl}/api/sessions`;
  let res: Response;
  try {
    res = await fetch(url, {
      headers: endpoint.token ? { Authorization: "Bearer " + endpoint.token } : {},
    });
  } catch (e: any) {
    throw new Error(`fetch ${url}: ${e?.message ?? e}`);
  }
  if (!res.ok) throw new Error(`fetch ${url}: http ${res.status}`);
  return (await res.json()) as SessionInfo[];
}

export interface SessionInfo {
  id: string;
  command: string;
  cwd: string;
  title: string;
  cols: number;
  rows: number;
  started_at: number;
  host_id?: string;
  host?: string;
  user?: string;
}
