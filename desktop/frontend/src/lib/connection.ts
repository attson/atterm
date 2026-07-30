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
import { t } from "../i18n";
import { getCurrentAccountKey } from "./account-key";
import { openMetaFields, openOutFrame, openSessionFields } from "./opaque";

export interface ClosePayload {
  exit_code: number;
  reason?: string;
}

export type Status = "connecting" | "attached" | "reconnecting" | "ended" | "error";

export interface Endpoint {
  url: string; // ws://host:port (no trailing slash)
  session_token: string;
}

export type FSRequestOp =
  | "list_dir"
  | "file_meta"
  | "read_file"
  | "read_chunk"
  | "watch_dir"
  | "unwatch_dir"
  | "open_external"
  | "write_file"
  | "create_file"
  | "rename"
  | "remove"
  | "mkdir"
  | "trash";

export interface FSRequest {
  op: FSRequestOp;
  request_id?: string;
  path?: string;
  max_bytes?: number;
  offset?: number;
  length?: number;
  watch_id?: string;
  /** Base64-encoded file body — matches Go's []byte JSON encoding. */
  data?: string;
  /** Client's last-known modTime (ms). 0 disables the CAS check. */
  expected_modtime?: number;
  /** Target path for rename. */
  new_path?: string;
  /** remove: recursive delete. */
  recursive?: boolean;
  /** write_file: allow creation when the target doesn't exist. */
  create_if_missing?: boolean;
}

export interface FSDirEntry {
  name: string;
  isDir: boolean;
  size?: number;
  modTime?: number;
}

export interface FSFileContent {
  path: string;
  // Go JSON encodes []byte fields as standard base64 strings.
  data: string;
  isBinary: boolean;
  truncatedAt?: number;
}

export interface FSFileMetaInfo {
  path: string;
  size: number;
  modTime: number;
  isBinary: boolean;
}

export interface FSChunkPayload {
  path: string;
  // Go JSON encodes []byte fields as standard base64 strings.
  data: string;
  offset: number;
  length: number;
  eof: boolean;
  contentType?: string;
}

export interface FSResponse {
  request_id: string;
  ok: boolean;
  error?: string;
  entries?: FSDirEntry[];
  meta?: FSFileMetaInfo;
  content?: FSFileContent;
  chunk?: FSChunkPayload;
  watch_id?: string;
}

export interface FSEvent {
  watch_id: string;
  path: string;
  event: "changed" | string;
}

export interface ConnectionHandlers {
  onOutput?: (data: Uint8Array) => void;
  onClose?: (info: ClosePayload) => void;
  onMeta?: (meta: {
    cwd?: string;
    title?: string;
    cols?: number;
    rows?: number;
    driver_client_id?: string;
    driver_client_name?: string;
    task_state?: TaskState;
    current_command?: string;
    command_started_at?: number;
    command_ended_at?: number;
    command_duration_ms?: number;
    command_exit_code?: number;
    last_output_at?: number;
  }) => void;
  onStatus?: (s: Status) => void;
  onReplayProgress?: (progress: ReplayProgress) => void;
  // onDriverChange fires whenever this connection's driver-or-viewer role
  // changes. isMe is true when the broadcast driver_client_id matches our
  // locally-generated clientID; false otherwise (including empty/no-driver).
  // driverClientName is the human-readable hostname of the new driver (may
  // be empty when no driver or driver didn't report a name).
  onDriverChange?: (driverClientID: string, isMe: boolean, driverClientName: string) => void;
}

export interface SessionConnectionOptions {
  // clientName is the human-readable identifier sent to the relay (typically
  // the local machine's hostname). Echoed back in META.driver_client_name
  // when this connection holds the driver role. Defaults to a generic
  // identifier derived from navigator.platform when omitted.
  clientName?: string;
  // remote marks a connection to a session on another host (tunnelled through
  // the desktop's loopback relay proxy). Such OUT frames are E2EE-sealed and
  // must be decrypted before display; local sessions stream plaintext and are
  // left untouched.
  remote?: boolean;
}

export interface SessionListHandlers {
  onSessions: (sessions: SessionInfo[]) => void;
  onStatus?: (s: Status) => void;
  onPrefsChanged?: () => void;
}

const MAX_PASTE_IMAGE_BYTES = 10 * 1024 * 1024;
const SUBPROTOCOL_SAFE = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;
const DEFAULT_FS_REQUEST_TIMEOUT_MS = 30_000;

export function pasteImageBlockReason(wsReadyState: number | undefined, blobSize: number): string | null {
  if (wsReadyState !== WebSocket.OPEN) return t("terminal.websocketNotOpen");
  if (blobSize > MAX_PASTE_IMAGE_BYTES) {
    return t("terminal.imageTooLarge", { size: blobSize, limit: MAX_PASTE_IMAGE_BYTES });
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

// defaultClientName returns a best-effort human-readable identifier for the
// running environment. Desktop callers should override via the constructor
// option (passing the real hostname from getHostInfo()).
function defaultClientName(): string {
  if (typeof navigator !== "undefined") {
    const platform = (navigator as any)?.userAgentData?.platform || navigator.platform || "";
    if (platform) return `browser (${platform})`;
  }
  return "browser";
}

function tokenSubprotocol(token: string): string | undefined {
  if (!token) return undefined;
  if (SUBPROTOCOL_SAFE.test(token)) return `atterm-token.${token}`;
  return `atterm-token-b64.${stringToBase64URL(token)}`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isOptionalString(value: unknown): boolean {
  return value === undefined || typeof value === "string";
}

function isOptionalNumber(value: unknown): boolean {
  return value === undefined || (typeof value === "number" && Number.isFinite(value));
}

function isFSDirEntry(value: unknown): value is FSDirEntry {
  if (!isRecord(value)) return false;
  return (
    typeof value.name === "string" &&
    typeof value.isDir === "boolean" &&
    isOptionalNumber(value.size) &&
    isOptionalNumber(value.modTime)
  );
}

function isFSFileMetaInfo(value: unknown): value is FSFileMetaInfo {
  if (!isRecord(value)) return false;
  return (
    typeof value.path === "string" &&
    typeof value.size === "number" &&
    Number.isFinite(value.size) &&
    typeof value.modTime === "number" &&
    Number.isFinite(value.modTime) &&
    typeof value.isBinary === "boolean"
  );
}

function isFSFileContent(value: unknown): value is FSFileContent {
  if (!isRecord(value)) return false;
  return (
    typeof value.path === "string" &&
    typeof value.data === "string" &&
    typeof value.isBinary === "boolean" &&
    isOptionalNumber(value.truncatedAt)
  );
}

function isFSChunkPayload(value: unknown): value is FSChunkPayload {
  if (!isRecord(value)) return false;
  return (
    typeof value.path === "string" &&
    typeof value.data === "string" &&
    typeof value.offset === "number" &&
    Number.isFinite(value.offset) &&
    typeof value.length === "number" &&
    Number.isFinite(value.length) &&
    typeof value.eof === "boolean" &&
    isOptionalString(value.contentType)
  );
}

function isFSResponse(value: unknown): value is FSResponse {
  if (!isRecord(value)) return false;
  if (typeof value.request_id !== "string" || typeof value.ok !== "boolean") return false;
  if (!isOptionalString(value.error) || !isOptionalString(value.watch_id)) return false;
  if (value.entries !== undefined && (!Array.isArray(value.entries) || !value.entries.every(isFSDirEntry))) {
    return false;
  }
  if (value.meta !== undefined && !isFSFileMetaInfo(value.meta)) return false;
  if (value.content !== undefined && !isFSFileContent(value.content)) return false;
  if (value.chunk !== undefined && !isFSChunkPayload(value.chunk)) return false;
  return true;
}

export function webSocketAuth(endpoint: Endpoint, path: string): { url: string; protocols?: string[] } {
  const base = endpoint.url.replace(/\/$/, "");
  const protocol = tokenSubprotocol(endpoint.session_token);
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
    let ws: WebSocket;
    try {
      ws = auth.protocols ? new WebSocket(auth.url, auth.protocols) : new WebSocket(auth.url);
    } catch (e) {
      // WebKit throws "The string did not match the expected pattern." (a
      // SyntaxError DOMException) synchronously when url scheme isn't ws/wss
      // or a subprotocol contains chars outside the RFC 6455 token set.
      // Don't let that tear down the caller's await chain — surface via
      // onStatus and retry with backoff like a normal reconnect.
      this.handleOpenFailure(e, auth);
      return;
    }
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
      if (f.type === TYPE.PREFS_CHANGED) {
        this.handlers.onPrefsChanged?.();
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

  private handleOpenFailure(e: unknown, auth: { url: string; protocols?: string[] }): void {
    const err = e as { name?: string; message?: string } | null;
    console.error("[SessionListConnection] new WebSocket failed", {
      url: auth.url,
      protocols: auth.protocols,
      name: err?.name,
      message: err?.message ?? String(e),
    });
    this.ws = null;
    this.handlers.onStatus?.("error");
    if (this.detached) return;
    const delay = Math.min(8000, 500 * Math.pow(2, this.reconnectAttempts++));
    this.reconnectTimer = window.setTimeout(() => this.openWS(), delay);
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
  // Inputs queued while WS was still CONNECTING (e.g. plugin send right after
  // SessionConnection.attach). Flushed in ws.onopen after ATTACH+RESIZE.
  private pendingInputs: string[] = [];
  // clientID identifies this SessionConnection end-to-end. Sent in ATTACH,
  // echoed back in META.driver_client_id when this connection is the driver.
  private clientID: string;
  private clientName: string;
  // remote: see SessionConnectionOptions.remote. Gates OUT-frame decryption.
  private remote: boolean;
  // currentDriverClientID is the last driver_client_id we observed in a META
  // frame. Used to detect transitions and decide whether to fire onDriverChange.
  private currentDriverClientID = "";
  // isDriverRole tracks whether the authoritative META currently names us the
  // driver. Persisted across reconnects so ws.onopen can re-assert the claim:
  // after an idle reconnect the host recreates its uplink proxy subscriber with
  // auto-promote suppressed, so the session has no driver until we re-claim.
  private isDriverRole = false;
  private pendingFSRequests = new Map<
    string,
    {
      resolve: (response: FSResponse) => void;
      reject: (err: Error) => void;
      timer: number;
    }
  >();
  private retiredFSRequestIDs = new Set<string>();
  private fsEventHandlers = new Set<(event: FSEvent) => void>();

  constructor(
    private endpoint: Endpoint,
    private sessionId: string,
    private handlers: ConnectionHandlers = {},
    options: SessionConnectionOptions = {}
  ) {
    this.sidBytes = uuidParse(sessionId);
    this.clientID = crypto.randomUUID();
    this.clientName = (options.clientName ?? "").trim() || defaultClientName();
    this.remote = options.remote ?? false;
  }

  // decryptOut unseals a remote session's E2EE TypeOut envelope (the relay only
  // carries ciphertext for cross-host sessions). Tolerant, mirroring the agent's
  // openInboundFrame and the web client: non-envelope bytes pass through as
  // plaintext; a sealed envelope is decrypted with the unlocked account_key; on
  // any failure the chunk is dropped rather than written, so xterm never renders
  // ciphertext garble. Only called for remote connections.
  private decryptOut(data: Uint8Array, seq: number): Uint8Array | null {
    const MIN_ENVELOPE = 1 + 24 + 16; // cipher_id + nonce + Poly1305 tag
    if (data.length < MIN_ENVELOPE || data[0] !== 0x01) return data;
    const accountKey = getCurrentAccountKey();
    if (!accountKey) return null;
    return openOutFrame(data, accountKey, this.sessionId, seq);
  }

  attach(): void {
    if (this.detached) return;
    this.openWS();
  }

  detach(): void {
    this.detached = true;
    this.rejectPendingFSRequests(new Error("filesystem request failed: connection detached"));
    this.retiredFSRequestIDs.clear();
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

  sendFSRequest(req: FSRequest, timeoutMs = DEFAULT_FS_REQUEST_TIMEOUT_MS): Promise<FSResponse> {
    const ws = this.ws;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      return Promise.reject(new Error("filesystem request failed: websocket is not open"));
    }
    const requestID = req.request_id || this.newUniqueFSRequestID();
    if (this.pendingFSRequests.has(requestID)) {
      return Promise.reject(new Error(`duplicate filesystem request_id: ${requestID}`));
    }
    if (this.retiredFSRequestIDs.has(requestID)) {
      return Promise.reject(new Error(`retired filesystem request_id after timeout: ${requestID}`));
    }
    const payload: FSRequest = { ...req, request_id: requestID };
    const encoded = encodeText(JSON.stringify(payload));

    return new Promise<FSResponse>((resolve, reject) => {
      const timer = window.setTimeout(() => {
        this.pendingFSRequests.delete(requestID);
        this.retiredFSRequestIDs.add(requestID);
        reject(new Error(`filesystem request timed out: ${requestID}`));
      }, timeoutMs);
      this.pendingFSRequests.set(requestID, { resolve, reject, timer });
      try {
        ws.send(encodeFrame(TYPE.FS_REQUEST, this.sidBytes, encoded));
      } catch (e) {
        window.clearTimeout(timer);
        this.pendingFSRequests.delete(requestID);
        reject(e instanceof Error ? e : new Error(String(e)));
      }
    });
  }

  onFSEvent(handler: (event: FSEvent) => void): () => void {
    this.fsEventHandlers.add(handler);
    return () => {
      this.fsEventHandlers.delete(handler);
    };
  }

  sendInput(s: string): void {
    if (!this.ws || this.ws.readyState === WebSocket.CONNECTING) {
      // Queue while the socket is opening; ws.onopen flushes after ATTACH.
      this.pendingInputs.push(s);
      return;
    }
    if (this.ws.readyState !== WebSocket.OPEN) return;
    this.ws.send(encodeFrame(TYPE.IN, this.sidBytes, encodeText(s)));
  }

  // claimDriver sends a CLAIM_DRIVER frame so the relay promotes this
  // subscription to driver. Idempotent — safe to call when already driver.
  claimDriver(): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    const payload = encodeText(JSON.stringify({ client_id: this.clientID, client_name: this.clientName }));
    this.ws.send(encodeFrame(TYPE.CLAIM_DRIVER, this.sidBytes, payload));
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

  // sendPasteFile is the generic-file counterpart of sendPasteImage. The
  // desktop receiver sanitizes the filename, writes bytes into a session-
  // scoped inbox, and injects the resulting absolute path (no CR, no
  // quoting) into the PTY. Reuses PASTE_IMAGE's block reason for the
  // ≤10 MiB size cap since the wire limit is identical.
  async sendPasteFile(blob: Blob, filename: string): Promise<boolean> {
    const ws = this.ws;
    const blocked = pasteImageBlockReason(ws?.readyState, blob.size);
    if (blocked || !ws) {
      this.handlers.onStatus?.("error");
      throw new Error(blocked ?? "websocket is not open");
    }
    const payload = encodeText(JSON.stringify({
      filename,
      content_type: blob.type || "application/octet-stream",
      data: arrayBufferToBase64(await blob.arrayBuffer()),
    }));
    console.info("[AT Term] sending paste file", {
      filename,
      contentType: blob.type || "application/octet-stream",
      bytes: blob.size,
    });
    ws.send(encodeFrame(TYPE.PASTE_FILE, this.sidBytes, payload));
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
    let ws: WebSocket;
    try {
      ws = auth.protocols ? new WebSocket(auth.url, auth.protocols) : new WebSocket(auth.url);
    } catch (e) {
      // See SessionListConnection.openWS — sync constructor throws must not
      // unwind callers; reroute to error + backoff reconnect.
      this.handleOpenFailure(e, auth);
      return;
    }
    ws.binaryType = "arraybuffer";
    this.ws = ws;
    this.handlers.onStatus?.(this.reconnectAttempts === 0 ? "connecting" : "reconnecting");

    ws.onopen = () => {
      this.reconnectAttempts = 0;
      this.retiredFSRequestIDs.clear();
      this.handlers.onStatus?.("attached");
      const attachPayload = encodeText(
        JSON.stringify({
          session_id: this.sessionId,
          since_seq: this.lastSeq,
          client_id: this.clientID,
          client_name: this.clientName,
        })
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
      if (this.pendingInputs.length > 0) {
        const queued = this.pendingInputs;
        this.pendingInputs = [];
        for (const s of queued) {
          ws.send(encodeFrame(TYPE.IN, this.sidBytes, encodeText(s)));
        }
      }
      // Re-assert the driver claim if we held it before this (re)connect. The
      // host suppresses auto-promote for its uplink proxy sub, so a stream
      // restart leaves the session driverless until the real driver re-claims.
      if (this.isDriverRole) this.claimDriver();
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
        const out = this.remote ? this.decryptOut(data, seq) : data;
        if (out) this.handlers.onOutput?.(out);
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
          const meta = JSON.parse(decodeText(f.payload)) as Record<string, unknown>;
          // M5-meta-mobile: overlay MetaPayload.Sealed when an
          // account_key is unlocked. The platform registers the key
          // via setAccountKeyProvider in lib/account-key.ts.
          const accountKey = getCurrentAccountKey();
          if (accountKey && typeof meta.sealed === "string" && meta.sealed.length > 0) {
            const env = b64StdToBytes(meta.sealed);
            const fields = openMetaFields(env, accountKey, this.sessionId);
            if (fields) {
              if (fields.cwd !== undefined) meta.cwd = fields.cwd;
              if (fields.title !== undefined) meta.title = fields.title;
              if (fields.current_command !== undefined) meta.current_command = fields.current_command;
            }
          }
          this.handlers.onMeta?.(meta);
          const newDriver = String(meta.driver_client_id ?? "");
          const newDriverName = String(meta.driver_client_name ?? "");
          if (newDriver !== this.currentDriverClientID) {
            this.currentDriverClientID = newDriver;
            const isMe = newDriver !== "" && newDriver === this.clientID;
            this.isDriverRole = isMe;
            this.handlers.onDriverChange?.(newDriver, isMe, newDriverName);
          }
        } catch {
          /* ignore */
        }
      } else if (f.type === TYPE.REPLAY_PROGRESS) {
        try {
          this.handlers.onReplayProgress?.(JSON.parse(decodeText(f.payload)) as ReplayProgress);
        } catch {
          /* ignore */
        }
      } else if (f.type === TYPE.FS_RESPONSE) {
        this.handleFSResponse(f.payload);
      } else if (f.type === TYPE.FS_EVENT) {
        this.handleFSEvent(f.payload);
      }
    };

    ws.onclose = () => {
      this.ws = null;
      this.rejectPendingFSRequests(new Error("filesystem request failed: websocket closed"));
      this.retiredFSRequestIDs.clear();
      if (this.detached) return;
      this.handlers.onStatus?.("reconnecting");
      const delay = Math.min(8000, 500 * Math.pow(2, this.reconnectAttempts++));
      this.reconnectTimer = window.setTimeout(() => this.openWS(), delay);
    };

    ws.onerror = () => {
      // onclose follows; nothing to do here
    };
  }

  private handleOpenFailure(e: unknown, auth: { url: string; protocols?: string[] }): void {
    const err = e as { name?: string; message?: string } | null;
    console.error("[SessionConnection] new WebSocket failed", {
      url: auth.url,
      protocols: auth.protocols,
      sessionId: this.sessionId,
      name: err?.name,
      message: err?.message ?? String(e),
    });
    this.ws = null;
    this.handlers.onStatus?.("error");
    if (this.detached) return;
    const delay = Math.min(8000, 500 * Math.pow(2, this.reconnectAttempts++));
    this.reconnectTimer = window.setTimeout(() => this.openWS(), delay);
  }

  private newFSRequestID(): string {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
      return `fs-${crypto.randomUUID()}`;
    }
    return `fs-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
  }

  private newUniqueFSRequestID(): string {
    for (let i = 0; i < 5; i++) {
      const requestID = this.newFSRequestID();
      if (!this.pendingFSRequests.has(requestID)) return requestID;
    }
    return `fs-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
  }

  private handleFSResponse(payload: Uint8Array): void {
    let response: FSResponse;
    try {
      const parsed = JSON.parse(decodeText(payload));
      if (!isFSResponse(parsed)) return;
      response = parsed;
    } catch {
      return;
    }
    const pending = this.pendingFSRequests.get(response.request_id);
    if (!pending) return;
    window.clearTimeout(pending.timer);
    this.pendingFSRequests.delete(response.request_id);
    pending.resolve(response);
  }

  private handleFSEvent(payload: Uint8Array): void {
    let event: FSEvent;
    try {
      const parsed = JSON.parse(decodeText(payload)) as Partial<FSEvent>;
      if (
        !parsed ||
        typeof parsed.watch_id !== "string" ||
        typeof parsed.path !== "string" ||
        typeof parsed.event !== "string"
      ) {
        return;
      }
      event = parsed as FSEvent;
    } catch {
      return;
    }
    for (const handler of this.fsEventHandlers) {
      try {
        handler(event);
      } catch {
        /* keep later handlers isolated */
      }
    }
  }

  private rejectPendingFSRequests(err: Error): void {
    if (this.pendingFSRequests.size === 0) return;
    const pending = Array.from(this.pendingFSRequests.values());
    this.pendingFSRequests.clear();
    for (const item of pending) {
      window.clearTimeout(item.timer);
      item.reject(err);
    }
  }
}

export async function fetchSessions(endpoint: Endpoint): Promise<SessionInfo[]> {
  const httpUrl = endpoint.url.replace(/^ws/, "http").replace(/\/$/, "");
  const url = `${httpUrl}/api/sessions`;
  let res: Response;
  try {
    res = await fetch(url, {
      headers: endpoint.session_token ? { Authorization: "Bearer " + endpoint.session_token } : {},
    });
  } catch (e: any) {
    throw new Error(`fetch ${url}: ${e?.message ?? e}`);
  }
  if (!res.ok) throw new Error(`fetch ${url}: http ${res.status}`);
  return decryptSessionFields((await res.json()) as SessionInfo[]);
}

// decryptSessionFields overlays the E2EE-sealed {title, cwd, command,
// current_command} onto each SessionInfo when an account_key is unlocked.
// The relay strips the plaintext for these fields once the agent seals them
// (uplink M6-final), so without this the session list shows only the short
// session id. Mirrors the Capacitor list path (platform/capacitor.ts) and the
// WS META decrypt above. Sessions without a `sealed` envelope — or when the
// key is locked or the cipher fails — pass through unchanged so the plaintext
// fields an additive-rollout agent still ships keep working.
export function decryptSessionFields(sessions: SessionInfo[]): SessionInfo[] {
  const accountKey = getCurrentAccountKey();
  if (!accountKey) return sessions;
  return sessions.map((s) => {
    if (!s.sealed) return s;
    try {
      const fields = openSessionFields(b64StdToBytes(s.sealed), accountKey, s.id);
      if (!fields) return s;
      const next: SessionInfo = { ...s };
      if (fields.title !== undefined) next.title = fields.title;
      if (fields.cwd !== undefined) next.cwd = fields.cwd;
      if (fields.command !== undefined) next.command = fields.command;
      if (fields.current_command !== undefined) next.current_command = fields.current_command;
      return next;
    } catch {
      return s;
    }
  });
}

export interface SessionSummary {
  recent_output?: string;
  error_lines?: string[];
  captured_at?: number;
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
  remote_permission?: string;
  task_state?: TaskState;
  current_command?: string;
  command_started_at?: number;
  command_ended_at?: number;
  command_duration_ms?: number;
  command_exit_code?: number;
  last_output_at?: number;
  type?: string; // "shell" | "ai" | "test" | "build" | "deploy" — absent on older publishers
  summary?: SessionSummary;
  unread?: boolean;
  /** Base64 (std) AEAD envelope over {title, cwd, command, current_command}
   *  sealed by the agent under HKDF(account_key, session_uuid). The relay
   *  strips the matching plaintext once a session is sealed (uplink M6-final),
   *  so a client MUST run decryptSessionFields to recover them. Empty/absent
   *  for sessions whose agent had no unlocked account_key. */
  sealed?: string;
}

export type TaskState =
  | "idle"
  | "running"
  | "waiting_input"
  | "completed"
  | "failed"
  | "disconnected"
  | "closed";

/** Std / URL-safe base64 → bytes. Tolerant of either Go encoding so a
 * future Sealed-field wire-encoding change cannot silently break the
 * mobile META decrypt. */
function b64StdToBytes(s: string): Uint8Array {
  const norm = s.replace(/-/g, "+").replace(/_/g, "/");
  const pad = norm.length % 4 === 0 ? "" : "=".repeat(4 - (norm.length % 4));
  const bin = atob(norm + pad);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}
