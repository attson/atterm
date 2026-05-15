import { SessionConnection } from "./connection";
import type { Endpoint } from "./api";

// Light cache so repeated send() calls do not churn through WS connects.
// Indexed by `${endpoint.url}|${sessionId}`. We rely on SessionConnection's
// own queueing semantics for not-yet-open sockets.
const cache = new Map<string, SessionConnection>();

function key(endpoint: Endpoint, sessionId: string) {
  return `${endpoint.url}|${sessionId}`;
}

export function sendInputToSession(endpoint: Endpoint, sessionId: string, text: string): void {
  const k = key(endpoint, sessionId);
  let conn = cache.get(k);
  if (!conn) {
    conn = new SessionConnection(endpoint, sessionId, {});
    cache.set(k, conn);
    conn.attach();
  }
  conn.sendInput(text);
}
