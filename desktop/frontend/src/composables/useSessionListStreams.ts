import {
  SessionListConnection,
  type Endpoint,
  type SessionInfo,
} from "../lib/connection";

/**
 * useSessionListStreams owns the three module-scope handles App.vue used
 * to keep to manage the session-list transports:
 *
 *   - localSessionListConn   — WS to the local Wails-side session list
 *   - remoteSessionListConn  — WS to the relay's /client-sessions stream
 *   - remotePollHandle       — setInterval id for HTTP-poll fallback
 *
 * Every attach/detach/replace had to detach the old handle first, and the
 * remote side had two competing transports (WS + HTTP poll) that had to
 * be mutually exclusive. This composable centralises that policy so
 * callers just say "give me a local stream" / "give me a remote WS" /
 * "give me a remote poll" and never touch the underlying handles.
 *
 * All app-level state (list refs, endpoints, missingSince map, config
 * dedup key, prune-stale-tabs sweep) stays with the caller. The composable
 * is purely the transport layer.
 */
export interface SessionListStreamCallbacks {
  /** LIST_RESP handler — receives the full current list on every push. */
  onSessions: (sessions: SessionInfo[]) => void;
  /** Server-pushed "prefs changed" notification (remote WS only). */
  onPrefsChanged?: () => void;
  /** Transport status transitions ("error" is the only one the caller
   *  currently reacts to). */
  onStatus: (status: string) => void;
}

export interface UseSessionListStreams {
  /** Attach or replace the local session-list stream. Idempotent. */
  attachLocal(endpoint: Endpoint, cb: SessionListStreamCallbacks): void;
  /** Attach or replace the remote session-list WS stream. Stops any
   *  active poll and previous WS handle first. */
  attachRemoteWS(endpoint: Endpoint, cb: SessionListStreamCallbacks): void;
  /** Switch to interval polling for the remote list. Stops any active
   *  WS handle and previous poll first. `fn`'s returned Promise (if any)
   *  is swallowed — the caller handles errors inside. */
  startRemotePoll(fn: () => void | Promise<void>, intervalMs: number): void;
  /** Detach the remote WS + clear the poll. Idempotent. Leaves local alone. */
  stopRemote(): void;
  /** True when the remote WS handle is currently attached. Used to skip
   *  a redundant HTTP poll when the WS is already streaming. */
  isRemoteWSAttached(): boolean;
  /** onUnmounted cleanup: detaches local + remote streams. */
  detachAll(): void;
}

export function useSessionListStreams(): UseSessionListStreams {
  let localConn: SessionListConnection | null = null;
  let remoteConn: SessionListConnection | null = null;
  let remotePollHandle: number | null = null;

  function stopRemote() {
    remoteConn?.detach();
    remoteConn = null;
    if (remotePollHandle !== null) {
      window.clearInterval(remotePollHandle);
      remotePollHandle = null;
    }
  }

  function attachLocal(endpoint: Endpoint, cb: SessionListStreamCallbacks) {
    localConn?.detach();
    localConn = new SessionListConnection(endpoint, {
      onSessions: cb.onSessions,
      onStatus: cb.onStatus,
    });
    localConn.attach();
  }

  function attachRemoteWS(endpoint: Endpoint, cb: SessionListStreamCallbacks) {
    stopRemote();
    remoteConn = new SessionListConnection(endpoint, {
      onSessions: cb.onSessions,
      onPrefsChanged: cb.onPrefsChanged,
      onStatus: cb.onStatus,
    });
    remoteConn.attach();
  }

  function startRemotePoll(fn: () => void | Promise<void>, intervalMs: number) {
    stopRemote();
    remotePollHandle = window.setInterval(() => {
      void fn();
    }, intervalMs);
  }

  function detachAll() {
    localConn?.detach();
    localConn = null;
    stopRemote();
  }

  return {
    attachLocal,
    attachRemoteWS,
    startRemotePoll,
    stopRemote,
    isRemoteWSAttached: () => remoteConn !== null,
    detachAll,
  };
}
