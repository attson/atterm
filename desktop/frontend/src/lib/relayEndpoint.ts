import type { Endpoint } from "./connection";

/** Converts either the desktop ws(s) relay URL or the web/mobile http(s)
 * base URL into the public WebSocket origin used by direct signaling. */
export function buildRelayWebSocketEndpoint(base: string, token: string): Endpoint | null {
  if (!base || !token) return null;
  try {
    const url = new URL(base);
    const protocol = url.protocol === "https:" || url.protocol === "wss:"
      ? "wss:"
      : url.protocol === "http:" || url.protocol === "ws:"
        ? "ws:"
        : null;
    if (!protocol || !url.host) return null;
    return { url: `${protocol}//${url.host}`, session_token: token };
  } catch {
    return null;
  }
}
