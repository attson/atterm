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
import type { Frame } from "./proto";
import type { ReplayProgress } from "./replayProgress";
import { t } from "../i18n";
import { getCurrentAccountKey } from "./account-key";
import { deriveServiceKeys, openMetaFields, openOutFrame, openSessionFields, sealUnsequenced, openUnsequencedFrame, b64ToBytes } from "./opaque";
import { encodeSegments, decodeSegments } from "./fsSegments";
import { errText, logDebug, logError, logWarn } from "./log";
import { DirectClientTransport, type DirectClientOptions, type DirectTransportDiagnostics } from "./directClient";
import { DirectRoute, DirectRouteState, DirectRouteTracker } from "./directRoute";

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
  onRouteChange?: (diagnostics: SessionRouteDiagnostics) => void;
  // onDriverChange fires whenever this connection's driver-or-viewer role
  // changes. isMe is true when the broadcast driver_client_id matches our
  // locally-generated clientID; false otherwise (including empty/no-driver).
  // driverClientName is the human-readable hostname of the new driver (may
  // be empty when no driver or driver didn't report a name).
  onDriverChange?: (driverClientID: string, isMe: boolean, driverClientName: string) => void;
}

export type SessionRoute = "relay" | "connecting-direct" | "direct";
export type DirectFallbackReason =
  | "account_key_unavailable"
  | "signal_endpoint_unavailable"
  | "webrtc_unavailable"
  | "timeout"
  | "signaling_rejected"
  | "host_unavailable"
  | "ice_failed"
  | "authentication_failed"
  | "backpressure"
  | "protocol_error"
  | "direct_disconnected"
  | "preference_disabled"
  | "transport_error";

export interface SessionRouteDiagnostics {
  route: SessionRoute;
  iceState?: RTCIceConnectionState;
  candidateType?: "host" | "srflx" | "prflx" | "relay";
  setupTimeMs?: number;
  fallbackReason?: DirectFallbackReason;
}

export function directFallbackReason(error: unknown, wasActive = false): DirectFallbackReason {
  const message = errText(error).toLowerCase();
  if (message.includes("rtcpeerconnection") || message.includes("webrtc is unavailable")) return "webrtc_unavailable";
  if (message.includes("timed out") || message.includes("ticket_expired")) return "timeout";
  if (message.includes("host_offline") || message.includes("peer_disconnected")) return "host_unavailable";
  if (message.includes("signaling rejected") || message.includes("signaling disconnected")) return "signaling_rejected";
  if (message.includes("peer connection failed") || message.includes("ice")) return "ice_failed";
  if (message.includes("handshake") || message.includes("proof") || message.includes("auth")) return "authentication_failed";
  if (message.includes("backpressure")) return "backpressure";
  if (message.includes("unsupported") || message.includes("invalid direct") || message.includes("record")) return "protocol_error";
  if (wasActive || message.includes("disconnected") || message.includes("channel closed") || message.includes("peer closed")) {
    return "direct_disconnected";
  }
  return "transport_error";
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
  // Direct transport is an opportunistic remote-session acceleration. It is
  // disabled by default until the user-facing rollout preference enables it.
  preferDirect?: boolean;
  // Public Relay endpoint used only for /direct-signal. Wails remote terminal
  // traffic uses a loopback proxy endpoint, which cannot broker WebRTC peers.
  directEndpoint?: Endpoint | null;
  /** Injection seam for deterministic transport lifecycle tests. */
  directTransportFactory?: (options: DirectClientOptions) => DirectTransport;
}

export interface DirectTransport {
  start(): void;
  sendFrame(frame: Uint8Array): boolean;
  close(): void;
}

export interface SessionListHandlers {
  onSessions: (sessions: SessionInfo[]) => void;
  onStatus?: (s: Status) => void;
  onPrefsChanged?: () => void;
}

const MAX_PASTE_IMAGE_BYTES = 10 * 1024 * 1024;
const SUBPROTOCOL_SAFE = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;
const DEFAULT_FS_REQUEST_TIMEOUT_MS = 30_000;
const DEFAULT_SERVICE_OPEN_TIMEOUT_MS = 30_000;

export interface ServiceOpenResult {
  serviceId: string;
  clientTicket: string;
  clientToHostKey: Uint8Array;
  hostToClientKey: Uint8Array;
}

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

// hexHead renders the first `max` bytes of a frame payload as hex, with an
// elision marker when truncated — enough to recognise what a bad payload
// actually was, without dumping a whole session list into the log.
function hexHead(b: Uint8Array, max = 64): string {
  const n = Math.min(b.length, max);
  let s = "";
  for (let i = 0; i < n; i++) s += b[i].toString(16).padStart(2, "0");
  return b.length > max ? `${s}…` : s;
}

export class SessionListConnection {
  private ws: WebSocket | null = null;
  private reconnectAttempts = 0;
  private reconnectTimer: number | null = null;
  private detached = false;
  private listParseFailures = 0;

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
    this.handlers.onStatus?.(this.reconnectAttempts > 0 ? "reconnecting" : "connecting");

    ws.onopen = () => {
      this.handlers.onStatus?.("attached");
    };

    ws.onmessage = (ev: MessageEvent) => {
      let f;
      try {
        f = decodeFrame(new Uint8Array(ev.data as ArrayBuffer));
      } catch {
        return;
      }
      // The desktop loopback proxy accepts the browser-facing websocket
      // before its upstream relay dial/authentication has succeeded. Only a
      // framed server message proves the full path is healthy; resetting in
      // onopen turns an upstream 401 into a permanent 500 ms retry loop.
      this.reconnectAttempts = 0;
      if (f.type === TYPE.PREFS_CHANGED) {
        this.handlers.onPrefsChanged?.();
        return;
      }
      if (f.type !== TYPE.LIST_RESP) return;

      // Parsing and dispatch are reported separately. A parse/decrypt failure
      // means the frame itself is unusable; a throw out of onSessions is a UI
      // bug that says nothing about the wire format. Conflating the two hid
      // the cause of a 6491-frame outage in v0.4.16.
      let sessions: SessionInfo[];
      try {
        sessions = decryptSessionFields(JSON.parse(decodeText(f.payload)) as SessionInfo[]);
      } catch (e) {
        this.noteUnusableListFrame(f.payload, e);
        return;
      }
      // Reset before dispatching so a throwing handler cannot be mistaken for
      // a run of bad frames.
      this.listParseFailures = 0;
      try {
        this.handlers.onSessions(sessions);
      } catch (e) {
        logWarn("conn", "session list handler threw", { url: this.endpoint.url, error: errText(e) });
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

  // Rate limited: LIST_RESP arrives several times a second, so a persistent
  // failure would write thousands of identical lines — in v0.4.16 that rotated
  // nine days of history out of desktop.log and left nothing to diagnose from.
  // The first failure and every hundredth carry the full context;
  // `consecutive` accounts for the ones in between.
  private noteUnusableListFrame(payload: Uint8Array, e: unknown): void {
    this.listParseFailures++;
    const n = this.listParseFailures;
    if (n !== 1 && n % 100 !== 0) return;
    logWarn("conn", "dropping unusable LIST_RESP", {
      url: this.endpoint.url,
      consecutive: n,
      payload_len: payload.length,
      payload_head: hexHead(payload),
      error: errText(e),
    });
  }

  private handleOpenFailure(e: unknown, auth: { url: string; protocols?: string[] }): void {
    const err = e as { name?: string; message?: string } | null;
    logError("conn", "session-list: new WebSocket failed", {
      url: auth.url,
      protocols: auth.protocols?.join(","),
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

/** encodeFSRequestPayload mirrors proto.EncodeFSRequest. request_id / op /
 *  client_id stay in segment 0 because the relay gates on them
 *  (isReadOnlyFSOperation) and routes replies by request_id; path and
 *  new_path go into the sealed segment. A keyless client emits a single
 *  plaintext segment, matching the agent's "no key = no encryption" rule. */
function encodeFSRequestPayload(payload: FSRequest, sessionId: string): Uint8Array {
  const accountKey = getCurrentAccountKey();
  if (!accountKey) {
    return encodeSegments([encodeText(JSON.stringify(payload))]);
  }
  const sealed = encodeText(JSON.stringify({ path: payload.path, new_path: payload.new_path }));
  const env = sealUnsequenced(accountKey, sessionId, TYPE.FS_REQUEST, sealed);
  // Red line #23: never ship a plaintext copy of what we just sealed.
  const head = { ...payload };
  delete head.path;
  delete head.new_path;
  return encodeSegments([encodeText(JSON.stringify(head)), env]);
}

/** bytesToBase64 restores the shape Go's encoding/json gives []byte, so
 *  remoteSessionFS keeps decoding file contents the same way whether the
 *  bytes arrived sealed or plain. */
function bytesToBase64(b: Uint8Array): string {
  let bin = "";
  for (const x of b) bin += String.fromCharCode(x);
  return btoa(bin);
}

export class SessionConnection {
  private ws: WebSocket | null = null;
  private sidBytes: Uint8Array;
  private lastSeq = 0;
  private route = new DirectRouteTracker();
  private direct: DirectTransport | null = null;
  private directAttempted = false;
  private reconnectAttempts = 0;
  private reconnectTimer: number | null = null;
  private detached = false;
  private suspended = false;
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
  private preferDirect: boolean;
  private directEndpoint: Endpoint | null;
  private directTransportFactory: (options: DirectClientOptions) => DirectTransport;
  private directTransportSupported: () => boolean;
  private pendingDriverClaim = false;
  private relayReplayComplete = false;
  private directStartedAt = 0;
  private directDiagnostics: DirectTransportDiagnostics | null = null;
  private lastFallbackReason: DirectFallbackReason | undefined;
  // currentDriverClientID is the last driver_client_id we observed in a META
  // frame. Used to detect transitions and decide whether to fire onDriverChange.
  private currentDriverClientID = "";
  // isDriverRole tracks whether the authoritative META currently names us the
  // driver. It is retained while disconnected only so the first META after a
  // reconnect can restore control if (and only if) the session is driverless.
  private isDriverRole = false;
  // One-shot reconnect intent. We cannot claim in onopen: another client may
  // have taken control while this tab's socket was suspended. The first META is
  // authoritative and either consumes this intent or permits a vacant reclaim.
  private recoverDriverIfVacant = false;
  private recoverDriverVacant = false;
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
  private pendingServiceOpens = new Map<
    string,
    {
      serviceId: string;
      resolve: (result: ServiceOpenResult) => void;
      reject: (err: Error) => void;
      timer: number;
      clientToHostKey: Uint8Array;
      hostToClientKey: Uint8Array;
    }
  >();

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
    this.preferDirect = options.preferDirect ?? false;
    this.directEndpoint = options.directEndpoint ?? null;
    const injectedDirectTransport = options.directTransportFactory;
    this.directTransportFactory = injectedDirectTransport ?? ((directOptions) => new DirectClientTransport(directOptions));
    // Test/native adapters own their capability checks. The browser adapter
    // requires WebRTC to exist before opening signaling, otherwise a host-side
    // attempt is allocated only to fail later at `new RTCPeerConnection()`.
    this.directTransportSupported = injectedDirectTransport
      ? () => true
      : () => typeof globalThis.RTCPeerConnection === "function";
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
    if (this.suspended) {
      this.route = new DirectRouteTracker(this.lastSeq);
      this.directAttempted = false;
      this.relayReplayComplete = false;
    }
    this.suspended = false;
    if (this.ws || this.reconnectTimer !== null) return;
    this.openWS(this.route.generation);
  }

  setPreferDirect(enabled: boolean): void {
    if (this.preferDirect === enabled) return;
    this.preferDirect = enabled;
    if (enabled) {
      this.directAttempted = false;
      if (this.relayReplayComplete) this.maybeStartDirect();
      return;
    }
    const direct = this.direct;
    this.direct = null;
    direct?.close();
    if (this.route.state === DirectRouteState.DirectConnecting || this.route.state === DirectRouteState.DirectReplay) {
      try { this.route.abortDirect(this.route.generation); } catch { /* route already moved */ }
      this.emitRoute("relay", "preference_disabled");
      return;
    }
    if (this.route.state === DirectRouteState.DirectActive) {
      const generation = this.route.directLost(this.route.generation);
      this.emitRoute("relay", "preference_disabled");
      this.handlers.onStatus?.("reconnecting");
      this.openWS(generation);
    }
  }

  suspend(): void {
    if (this.detached) return;
    this.suspended = true;
    this.rejectPendingFSRequests(new Error("filesystem request failed: connection suspended"));
    this.rejectPendingServiceOpens(new Error("service preview failed: connection suspended"));
    this.retiredFSRequestIDs.clear();
    this.direct?.close();
    this.direct = null;
    this.pendingDriverClaim = false;
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

  detach(): void {
    this.detached = true;
    this.suspended = false;
    this.rejectPendingFSRequests(new Error("filesystem request failed: connection detached"));
    this.rejectPendingServiceOpens(new Error("service preview failed: connection detached"));
    this.retiredFSRequestIDs.clear();
    this.direct?.close();
    this.direct = null;
    this.pendingDriverClaim = false;
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
    const encoded = encodeFSRequestPayload(payload, this.sessionId);

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

  openService(port: number, host = "localhost", timeoutMs = DEFAULT_SERVICE_OPEN_TIMEOUT_MS): Promise<ServiceOpenResult> {
    const ws = this.ws;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      return Promise.reject(new Error("service preview failed: websocket is not open"));
    }
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      return Promise.reject(new Error("service preview failed: invalid port"));
    }
    if (!["localhost", "127.0.0.1", "::1"].includes(host)) {
      return Promise.reject(new Error("service preview failed: target must be loopback"));
    }
    const accountKey = getCurrentAccountKey();
    if (!accountKey || accountKey.length !== 32) {
      return Promise.reject(new Error("service preview requires unlocked E2EE"));
    }
    const requestId = crypto.randomUUID();
    const serviceId = crypto.randomUUID();
    const keys = deriveServiceKeys(accountKey, serviceId);
    const sealed = sealUnsequenced(
      accountKey,
      this.sessionId,
      TYPE.SERVICE_OPEN,
      encodeText(JSON.stringify({ port, scheme: "http", host })),
    );
    const payload = encodeText(JSON.stringify({
      request_id: requestId,
      service_id: serviceId,
      sealed: bytesToBase64(sealed),
    }));
    return new Promise<ServiceOpenResult>((resolve, reject) => {
      const timer = window.setTimeout(() => {
        this.pendingServiceOpens.delete(requestId);
        reject(new Error("service preview timed out"));
      }, timeoutMs);
      this.pendingServiceOpens.set(requestId, {
        serviceId,
        resolve,
        reject,
        timer,
        clientToHostKey: keys.clientToHost,
        hostToClientKey: keys.hostToClient,
      });
      try {
        ws.send(encodeFrame(TYPE.SERVICE_OPEN, this.sidBytes, payload));
      } catch (e) {
        window.clearTimeout(timer);
        this.pendingServiceOpens.delete(requestId);
        reject(e instanceof Error ? e : new Error(String(e)));
      }
    });
  }

  closeService(serviceId: string): void {
    const ws = this.ws;
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    try {
      ws.send(encodeFrame(
        TYPE.SERVICE_CLOSE,
        this.sidBytes,
        encodeText(JSON.stringify({ service_id: serviceId })),
      ));
    } catch {
      // Connection teardown also revokes the relay lease.
    }
  }

  onFSEvent(handler: (event: FSEvent) => void): () => void {
    this.fsEventHandlers.add(handler);
    return () => {
      this.fsEventHandlers.delete(handler);
    };
  }

  sendInput(s: string): void {
    const route = this.route.inputRoute();
    if (route === DirectRoute.Direct) {
      if (!this.direct?.sendFrame(encodeFrame(TYPE.IN, this.sidBytes, encodeText(s)))) {
        this.pendingInputs.push(s);
      }
      return;
    }
    if (route === DirectRoute.None) {
      this.pendingInputs.push(s);
      return;
    }
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
  claimDriver(): boolean {
    if (this.detached || this.suspended) return false;
    const payload = encodeText(JSON.stringify({ client_id: this.clientID, client_name: this.clientName }));
    const frame = encodeFrame(TYPE.CLAIM_DRIVER, this.sidBytes, payload);
    const route = this.route.inputRoute();
    if (route === DirectRoute.Direct) {
      if (this.direct?.sendFrame(frame)) {
        this.pendingDriverClaim = false;
        return true;
      }
      this.pendingDriverClaim = true;
      return false;
    }
    if (route === DirectRoute.Relay && this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(frame);
      this.pendingDriverClaim = false;
      return true;
    }
    // A user click during Relay reconnect must not disappear. The claim is
    // flushed immediately after the next ATTACH, before queued input/resize.
    this.pendingDriverClaim = true;
    return false;
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
    logDebug("conn", "sending paste image", {
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
    logDebug("conn", "sending paste file", {
      filename,
      contentType: blob.type || "application/octet-stream",
      bytes: blob.size,
    });
    ws.send(encodeFrame(TYPE.PASTE_FILE, this.sidBytes, payload));
    return true;
  }

  sendResize(cols: number, rows: number): void {
    const route = this.route.inputRoute();
    if (route === DirectRoute.Direct) {
      if (this.direct?.sendFrame(encodeFrame(TYPE.RESIZE, this.sidBytes, encodeResize(cols, rows)))) {
        this.pendingResize = null;
      } else {
        this.pendingResize = { cols, rows };
      }
      return;
    }
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

  private openWS(generation: number): void {
    if (this.detached || this.suspended || !this.route.acceptsRoute(generation, DirectRoute.Relay)) return;
    const auth = webSocketAuth(this.endpoint, "/client");
    let ws: WebSocket;
    try {
      ws = auth.protocols ? new WebSocket(auth.url, auth.protocols) : new WebSocket(auth.url);
    } catch (e) {
      // See SessionListConnection.openWS — sync constructor throws must not
      // unwind callers; reroute to error + backoff reconnect.
      this.handleOpenFailure(e, auth, generation);
      return;
    }
    ws.binaryType = "arraybuffer";
    this.ws = ws;
    this.handlers.onStatus?.(this.reconnectAttempts === 0 ? "connecting" : "reconnecting");

    ws.onopen = () => {
      if (this.ws !== ws || !this.route.acceptsRoute(generation, DirectRoute.Relay)) {
        try { ws.close(); } catch { /* ignore */ }
        return;
      }
      this.retiredFSRequestIDs.clear();
      this.recoverDriverIfVacant = this.isDriverRole;
      this.recoverDriverVacant = false;
      if (this.route.state !== DirectRouteState.RelayReattaching) {
        this.handlers.onStatus?.("attached");
      }
      const attachPayload = encodeText(
        JSON.stringify({
          session_id: this.sessionId,
          since_seq: this.route.committedSeq,
          client_id: this.clientID,
          client_name: this.clientName,
        })
      );
      ws.send(encodeFrame(TYPE.ATTACH, this.sidBytes, attachPayload));
      // Flush any resize that arrived while WS was still CONNECTING. Order
      // matters: ATTACH first, RESIZE after, so the relay applies the size
      // to the right session subscription.
      if (this.route.inputRoute() === DirectRoute.Relay) this.flushPendingRelayWrites(ws);
    };

    ws.onmessage = (ev: MessageEvent) => {
      if (this.ws !== ws || !this.route.acceptsRoute(generation, DirectRoute.Relay)) return;
      let f;
      try {
        f = decodeFrame(new Uint8Array(ev.data as ArrayBuffer));
      } catch {
        return;
      }
      // See SessionListConnection: a local proxy onopen does not prove its
      // upstream websocket was authenticated. A valid protocol frame does.
      this.reconnectAttempts = 0;
      this.handleProtocolFrame(f, DirectRoute.Relay, generation);
    };

    // The event is optional only because test doubles call this bare; a log
    // line must never be the thing that throws on a disconnect path.
    ws.onclose = (e?: CloseEvent) => {
      if (this.ws !== ws) return;
      this.ws = null;
      this.rejectPendingFSRequests(new Error("filesystem request failed: websocket closed"));
      this.rejectPendingServiceOpens(new Error("service preview failed: websocket closed"));
      this.retiredFSRequestIDs.clear();
      if (this.detached || this.suspended) return;
      // The close code is the only thing that distinguishes "the relay hung up"
      // from "this renderer's own websocket gave up", and the user just sees a
      // reconnecting badge either way.
      logWarn("conn", "session websocket closed", {
        sessionId: this.sessionId,
        code: e?.code,
        reason: e?.reason,
        wasClean: e?.wasClean,
        attempt: this.reconnectAttempts,
      });
      this.handlers.onStatus?.("reconnecting");
      const delay = Math.min(8000, 500 * Math.pow(2, this.reconnectAttempts++));
      this.reconnectTimer = window.setTimeout(() => {
        this.reconnectTimer = null;
        this.openWS(this.route.generation);
      }, delay);
    };

    ws.onerror = () => {
      // onclose follows; nothing to do here
    };
  }

  private handleProtocolFrame(f: Frame, route: DirectRoute, generation: number): void {
    if (!this.route.acceptsRoute(generation, route)) return;
    if (!this.sameSessionID(f.sid)) {
      if (route === DirectRoute.Direct) throw new Error("direct frame session mismatch");
      return;
    }

    if (f.type === TYPE.OUT) {
      const { seq, data } = decodeOutPayload(f.payload);
      if (seq === 0) {
        if (route === DirectRoute.Direct) throw new Error("direct OUT is missing sequence");
        const out = this.remote ? this.decryptOut(data, seq) : data;
        if (out) this.handlers.onOutput?.(out);
        return;
      }
      if (!this.route.acceptOutput(generation, route, seq)) {
        logDebug("conn", "dropping duplicate OUT", {
          sessionId: this.sessionId,
          seq,
          lastSeq: this.route.committedSeq,
          route: route === DirectRoute.Direct ? "direct" : "relay",
        });
        if (route === DirectRoute.Direct) this.maybeActivateDirect(generation);
        return;
      }
      const out = this.remote ? this.decryptOut(data, seq) : data;
      if (out) this.handlers.onOutput?.(out);
      this.lastSeq = this.route.committedSeq;
      if (route === DirectRoute.Direct) this.maybeActivateDirect(generation);
      return;
    }

    if (f.type === TYPE.CLOSE) {
      try {
        this.handlers.onClose?.(JSON.parse(decodeText(f.payload)) as ClosePayload);
      } catch {
        this.handlers.onClose?.({ exit_code: 0 });
      }
      this.handlers.onStatus?.("ended");
      this.direct?.close();
      this.direct = null;
      return;
    }

    if (f.type === TYPE.META) {
      this.handleMeta(f.payload, route);
      return;
    }

    if (f.type === TYPE.REPLAY_PROGRESS) {
      let progress: ReplayProgress;
      try {
        progress = JSON.parse(decodeText(f.payload)) as ReplayProgress;
      } catch {
        return;
      }
      this.handlers.onReplayProgress?.(progress);
      if (route === DirectRoute.Relay && progress.phase === "end") {
        this.relayReplayComplete = true;
        if (this.route.state === DirectRouteState.RelayReattaching) {
          this.route.relayAttached(generation);
          this.handlers.onStatus?.("attached");
          this.emitRoute("relay", this.lastFallbackReason);
          if (this.recoverDriverIfVacant && this.recoverDriverVacant) {
            this.recoverDriverIfVacant = false;
            this.recoverDriverVacant = false;
            this.claimDriver();
          }
          if (this.ws?.readyState === WebSocket.OPEN) this.flushPendingRelayWrites(this.ws);
        } else {
          this.maybeStartDirect();
        }
      }
      return;
    }

    if (route === DirectRoute.Direct) {
      throw new Error(`unsupported direct frame type 0x${f.type.toString(16).padStart(2, "0")}`);
    }
    if (f.type === TYPE.FS_RESPONSE) this.handleFSResponse(f.payload);
    else if (f.type === TYPE.FS_EVENT) this.handleFSEvent(f.payload);
    else if (f.type === TYPE.SERVICE_OPENED) this.handleServiceOpened(f.payload);
  }

  private handleMeta(payload: Uint8Array, route: DirectRoute): void {
    let meta: Record<string, unknown>;
    try {
      const parsed: unknown = JSON.parse(decodeText(payload));
      if (!isRecord(parsed)) throw new Error("META is not an object");
      meta = parsed;
      const accountKey = getCurrentAccountKey();
      if (accountKey && typeof meta.sealed === "string" && meta.sealed.length > 0) {
        const fields = openMetaFields(b64ToBytes(meta.sealed), accountKey, this.sessionId);
        if (fields) {
          if (fields.cwd !== undefined) meta.cwd = fields.cwd;
          if (fields.title !== undefined) meta.title = fields.title;
          if (fields.current_command !== undefined) meta.current_command = fields.current_command;
        }
      }
    } catch (error) {
      if (route === DirectRoute.Direct) throw new Error(`invalid direct META: ${errText(error)}`);
      return;
    }
    try {
      this.handlers.onMeta?.(meta);
      const newDriver = String(meta.driver_client_id ?? "");
      const newDriverName = String(meta.driver_client_name ?? "");
      if (route === DirectRoute.Relay && this.recoverDriverIfVacant) {
        if (newDriver === "") {
          this.recoverDriverVacant = true;
          if (this.route.state !== DirectRouteState.RelayReattaching) {
            this.recoverDriverIfVacant = false;
            this.recoverDriverVacant = false;
            this.claimDriver();
          }
          return;
        }
        this.recoverDriverIfVacant = false;
        this.recoverDriverVacant = false;
      }
      if (newDriver !== this.currentDriverClientID) {
        this.currentDriverClientID = newDriver;
        const isMe = newDriver !== "" && newDriver === this.clientID;
        this.isDriverRole = isMe;
        this.handlers.onDriverChange?.(newDriver, isMe, newDriverName);
      }
    } catch {
      /* isolate UI callbacks and a reconnect-time claim send */
    }
  }

  private maybeStartDirect(): void {
    if (!this.preferDirect || !this.remote || this.directAttempted ||
        this.route.state !== DirectRouteState.RelayAttached || this.detached || this.suspended) return;
    if (!this.directEndpoint) {
      this.directAttempted = true;
      this.emitRoute("relay", "signal_endpoint_unavailable");
      return;
    }
    if (!this.directTransportSupported()) {
      this.directAttempted = true;
      this.emitRoute("relay", "webrtc_unavailable");
      return;
    }
    const accountKey = getCurrentAccountKey();
    if (!accountKey || accountKey.length !== 32) {
      this.directAttempted = true;
      this.emitRoute("relay", "account_key_unavailable");
      return;
    }

    this.directAttempted = true;
    this.directStartedAt = performance.now();
    this.directDiagnostics = null;
    this.lastFallbackReason = undefined;
    const generation = this.route.beginDirect();
    this.emitRoute("connecting-direct");
    const auth = webSocketAuth(this.directEndpoint, "/direct-signal");
    let transport: DirectTransport;
    try {
      transport = this.directTransportFactory({
        signalURL: auth.url,
        signalProtocols: auth.protocols,
        sessionId: this.sessionId,
        sinceSeq: this.route.committedSeq,
        clientInstanceId: this.clientID,
        accountKey,
        callbacks: {
          onAuthenticated: () => {
            if (this.direct !== transport || generation !== this.route.generation) return;
            this.route.beginDirectReplay(generation);
          },
          onFrame: (bytes) => {
            if (this.direct !== transport || generation !== this.route.generation) return;
            this.handleProtocolFrame(decodeFrame(bytes), DirectRoute.Direct, generation);
          },
          onReady: (replayedSeq) => {
            if (this.direct !== transport || generation !== this.route.generation) return;
            this.route.noteDirectReady(generation, replayedSeq);
            this.maybeActivateDirect(generation);
          },
          onDiagnostics: (diagnostics) => {
            if (this.direct !== transport || generation !== this.route.generation) return;
            this.directDiagnostics = diagnostics;
            this.emitRoute(this.route.state === DirectRouteState.DirectActive ? "direct" : "connecting-direct");
          },
          onFailure: (error) => this.handleDirectFailure(generation, transport, error),
        },
      });
      this.direct = transport;
      transport.start();
    } catch (error) {
      if (this.route.generation === generation) {
        try { this.route.abortDirect(generation); } catch { /* route already moved */ }
      }
      this.direct = null;
      this.lastFallbackReason = directFallbackReason(error);
      this.emitRoute("relay", this.lastFallbackReason);
      logWarn("direct", "direct attempt setup failed", { sessionId: this.sessionId, error: errText(error) });
    }
  }

  private maybeActivateDirect(generation: number): void {
    const transport = this.direct;
    if (!transport || !this.route.canActivateDirect(generation)) return;
    if (this.isDriverRole) {
      const claim = encodeText(JSON.stringify({ client_id: this.clientID, client_name: this.clientName }));
      if (!transport.sendFrame(encodeFrame(TYPE.CLAIM_DRIVER, this.sidBytes, claim))) return;
    }
    this.route.activateDirect(generation);
    this.emitRoute("direct");
    if (this.reconnectTimer !== null) {
      window.clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    const relay = this.ws;
    this.ws = null;
    this.rejectPendingFSRequests(new Error("filesystem request failed: direct terminal route activated"));
    this.rejectPendingServiceOpens(new Error("service preview failed: direct terminal route activated"));
    this.retiredFSRequestIDs.clear();
    try { relay?.close(); } catch { /* ignore */ }
    this.flushPendingDirectWrites(transport);
  }

  private handleDirectFailure(generation: number, transport: DirectTransport, error: Error): void {
    if (this.direct !== transport || generation !== this.route.generation) return;
    this.direct = null;
    const wasActive = this.route.state === DirectRouteState.DirectActive;
    this.lastFallbackReason = directFallbackReason(error, wasActive);
    logWarn("direct", "direct route failed", { sessionId: this.sessionId, error: errText(error) });
    if (this.route.state === DirectRouteState.DirectConnecting || this.route.state === DirectRouteState.DirectReplay) {
      this.route.abortDirect(generation);
      this.emitRoute("relay", this.lastFallbackReason);
      return;
    }
    if (this.route.state !== DirectRouteState.DirectActive || this.detached || this.suspended) return;
    const fallbackGeneration = this.route.directLost(generation);
    this.emitRoute("relay", this.lastFallbackReason);
    this.handlers.onStatus?.("reconnecting");
    this.openWS(fallbackGeneration);
  }

  private emitRoute(route: SessionRoute, fallbackReason?: DirectFallbackReason): void {
    const elapsed = this.directStartedAt > 0 && route !== "relay"
      ? Math.max(0, Math.round(performance.now() - this.directStartedAt))
      : undefined;
    try {
      this.handlers.onRouteChange?.({
        route,
        ...(this.directDiagnostics?.iceState ? { iceState: this.directDiagnostics.iceState } : {}),
        ...(this.directDiagnostics?.candidateType ? { candidateType: this.directDiagnostics.candidateType } : {}),
        ...(elapsed !== undefined ? { setupTimeMs: elapsed } : {}),
        ...(fallbackReason ? { fallbackReason } : {}),
      });
    } catch {
      /* isolate UI diagnostics from transport routing */
    }
  }

  private flushPendingRelayWrites(ws: WebSocket): void {
    if (this.pendingDriverClaim) {
      const payload = encodeText(JSON.stringify({ client_id: this.clientID, client_name: this.clientName }));
      ws.send(encodeFrame(TYPE.CLAIM_DRIVER, this.sidBytes, payload));
      this.pendingDriverClaim = false;
    }
    if (this.pendingResize) {
      const { cols, rows } = this.pendingResize;
      ws.send(encodeFrame(TYPE.RESIZE, this.sidBytes, encodeResize(cols, rows)));
      this.pendingResize = null;
    }
    if (this.pendingInputs.length > 0) {
      const queued = this.pendingInputs;
      this.pendingInputs = [];
      for (const s of queued) ws.send(encodeFrame(TYPE.IN, this.sidBytes, encodeText(s)));
    }
  }

  private flushPendingDirectWrites(transport: DirectTransport): void {
    if (this.pendingDriverClaim) {
      const payload = encodeText(JSON.stringify({ client_id: this.clientID, client_name: this.clientName }));
      if (!transport.sendFrame(encodeFrame(TYPE.CLAIM_DRIVER, this.sidBytes, payload))) return;
      this.pendingDriverClaim = false;
    }
    if (this.pendingResize) {
      const { cols, rows } = this.pendingResize;
      if (!transport.sendFrame(encodeFrame(TYPE.RESIZE, this.sidBytes, encodeResize(cols, rows)))) return;
      this.pendingResize = null;
    }
    while (this.pendingInputs.length > 0) {
      const value = this.pendingInputs[0];
      if (!transport.sendFrame(encodeFrame(TYPE.IN, this.sidBytes, encodeText(value)))) return;
      this.pendingInputs.shift();
    }
  }

  private sameSessionID(other: Uint8Array): boolean {
    if (other.length !== this.sidBytes.length) return false;
    for (let i = 0; i < other.length; i++) if (other[i] !== this.sidBytes[i]) return false;
    return true;
  }

  private handleOpenFailure(
    e: unknown,
    auth: { url: string; protocols?: string[] },
    generation: number,
  ): void {
    const err = e as { name?: string; message?: string } | null;
    logError("conn", "session: new WebSocket failed", {
      url: auth.url,
      protocols: auth.protocols?.join(","),
      sessionId: this.sessionId,
      name: err?.name,
      message: err?.message ?? String(e),
    });
    this.ws = null;
    this.handlers.onStatus?.("error");
    if (this.detached || this.suspended || !this.route.acceptsRoute(generation, DirectRoute.Relay)) return;
    const delay = Math.min(8000, 500 * Math.pow(2, this.reconnectAttempts++));
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null;
      this.openWS(this.route.generation);
    }, delay);
  }

  private newFSRequestID(): string {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
      return `fs-${crypto.randomUUID()}`;
    }
    return `fs-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
  }

  private handleServiceOpened(payload: Uint8Array): void {
    let response: {
      request_id?: string;
      service_id?: string;
      ok?: boolean;
      error?: string;
      client_ticket?: string;
    };
    try {
      response = JSON.parse(decodeText(payload));
    } catch {
      return;
    }
    if (!response.request_id) return;
    const pending = this.pendingServiceOpens.get(response.request_id);
    if (!pending) return;
    window.clearTimeout(pending.timer);
    this.pendingServiceOpens.delete(response.request_id);
    if (!response.ok || !response.client_ticket || response.service_id !== pending.serviceId) {
      pending.reject(new Error(response.error || "service preview rejected"));
      return;
    }
    pending.resolve({
      serviceId: pending.serviceId,
      clientTicket: response.client_ticket,
      clientToHostKey: pending.clientToHostKey,
      hostToClientKey: pending.hostToClientKey,
    });
  }

  private rejectPendingServiceOpens(err: Error): void {
    for (const pending of this.pendingServiceOpens.values()) {
      window.clearTimeout(pending.timer);
      pending.reject(err);
    }
    this.pendingServiceOpens.clear();
  }

  private newUniqueFSRequestID(): string {
    for (let i = 0; i < 5; i++) {
      const requestID = this.newFSRequestID();
      if (!this.pendingFSRequests.has(requestID)) return requestID;
    }
    return `fs-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
  }

  private handleFSResponse(payload: Uint8Array): void {
    const segs = decodeSegments(payload);
    if (!segs) return;
    let response: FSResponse;
    try {
      const parsed = JSON.parse(decodeText(segs[0]));
      if (!isFSResponse(parsed)) return;
      response = parsed;
    } catch {
      return;
    }
    if (segs.length > 1 && !this.openSealedFSResponse(response, segs)) return;
    const pending = this.pendingFSRequests.get(response.request_id);
    if (!pending) return;
    window.clearTimeout(pending.timer);
    this.pendingFSRequests.delete(response.request_id);
    pending.resolve(response);
  }

  private handleFSEvent(payload: Uint8Array): void {
    const segs = decodeSegments(payload);
    if (!segs) return;
    let event: FSEvent;
    try {
      const parsed = JSON.parse(decodeText(segs[0])) as Partial<FSEvent>;
      if (!parsed || typeof parsed.watch_id !== "string" || typeof parsed.event !== "string") {
        return;
      }
      if (segs.length > 1) {
        // path is sealed; the head carries only watch_id / event.
        const accountKey = getCurrentAccountKey();
        if (!accountKey) return;
        const opened = openUnsequencedFrame(accountKey, this.sessionId, TYPE.FS_EVENT, segs[1]);
        if (!opened) return;
        const fields = JSON.parse(decodeText(opened)) as { path?: string };
        parsed.path = fields.path ?? "";
      }
      if (typeof parsed.path !== "string") return;
      event = parsed as FSEvent;
    } catch {
      return;
    }
    for (const handler of this.fsEventHandlers) {
      try {
        handler(event);
      } catch (e) {
        // Later handlers still run — but a throwing subscriber is a bug, and
        // silence is why it would go unnoticed.
        logWarn("conn", "fs event handler threw", { error: errText(e) });
      }
    }
  }

  /** openSealedFSResponse overlays the sealed segments onto `response`,
   *  returning false when the frame must be dropped. Segment 1 carries
   *  the metadata (entries / meta / error / content and chunk minus
   *  their bytes); segment 2, when present, carries the raw file bytes
   *  which are re-base64'd so consumers keep seeing Go's []byte shape. */
  private openSealedFSResponse(response: FSResponse, segs: Uint8Array[]): boolean {
    const accountKey = getCurrentAccountKey();
    if (!accountKey) return false;
    const meta = openUnsequencedFrame(accountKey, this.sessionId, TYPE.FS_RESPONSE, segs[1]);
    if (!meta) return false;
    try {
      Object.assign(response, JSON.parse(decodeText(meta)));
    } catch {
      return false;
    }
    if (segs.length > 2) {
      const raw = openUnsequencedFrame(accountKey, this.sessionId, TYPE.FS_RESPONSE, segs[2]);
      if (!raw) return false;
      const encoded = bytesToBase64(raw);
      if (response.content) response.content.data = encoded;
      else if (response.chunk) response.chunk.data = encoded;
    }
    return true;
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
      const fields = openSessionFields(b64ToBytes(s.sealed), accountKey, s.id);
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
  // ssh_host_id marks an SSH session connected from a saved host (SSHHost.ID);
  // used by recovery to reconnect. Absent for local shells / ad-hoc SSH.
  ssh_host_id?: string;
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
