export type RemoteConnectionReason =
  | "initializing"
  | "connecting"
  | "paused"
  | "unconfigured"
  | "signed-out"
  | "proxy-unavailable"
  | "unavailable";

/** These are known connection prerequisites, never guesses about DNS or TLS. */
export function resolveRemoteConnectionReason(config: {
  url: string;
  token: string;
  connected: boolean;
  paused?: boolean;
  remote_proxy_url?: string;
}): RemoteConnectionReason {
  if (!config.url) return "unconfigured";
  if (!config.token) return "signed-out";
  if (config.paused) return "paused";
  if (config.connected && !config.remote_proxy_url) return "proxy-unavailable";
  return "connecting";
}
