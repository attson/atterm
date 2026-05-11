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
}

export interface SessionListHandlers {
  onSessions: (sessions: SessionInfo[]) => void;
  onStatus?: (s: Status) => void;
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

  private url(): string {
    const t = encodeURIComponent(this.endpoint.token);
    const base = this.endpoint.url.replace(/\/$/, "");
    return `${base}/client-sessions${t ? `?token=${t}` : ""}`;
  }

  private openWS(): void {
    if (this.detached) return;
    const ws = new WebSocket(this.url());
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

  private url(): string {
    const t = encodeURIComponent(this.endpoint.token);
    const base = this.endpoint.url.replace(/\/$/, "");
    return `${base}/client${t ? `?token=${t}` : ""}`;
  }

  private openWS(): void {
    if (this.detached) return;
    const ws = new WebSocket(this.url());
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
  const t = encodeURIComponent(endpoint.token);
  const httpUrl = endpoint.url.replace(/^ws/, "http").replace(/\/$/, "");
  const url = `${httpUrl}/api/sessions${t ? `?token=${t}` : ""}`;
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
