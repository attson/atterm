import type { SessionInfo } from "../connection";
import { decryptSessionFields } from "../connection";
import { bindings } from "./_bindings";
import type {
  ConnHealthSnapshot,
  DiagnosticsPayload,
  PairingToken,
  RelayConfig,
  RelayMe,
  RelaySessionRow,
  SignOutOthersResult,
} from "./_bindings";

export type {
  ConnHealthSnapshot,
  DiagnosticsPayload,
  PairingToken,
  RelayConfig,
  RelayMe,
  RelaySessionRow,
  SignOutOthersResult,
} from "./_bindings";

// Uplink health snapshot rendered by ConnHealthPill / drawer.
export function getUplinkHealth(): Promise<ConnHealthSnapshot> {
  return bindings().GetUplinkHealth();
}

export function getRelayConfig(): Promise<RelayConfig> {
  return bindings().GetRelayConfig();
}

export function setRelayConfig(cfg: {
  url: string;
  token: string;
  session_expires_at?: number;
  allow_insecure_relay?: boolean;
  disable_e2ee?: boolean;
  remote_permission?: string;
  // When provided, persists the cached email (used to remember inputs after a
  // failed login). Empty leaves the stored value untouched.
  last_email?: string;
}): Promise<void> {
  return bindings().SetRelayConfig({
    url: cfg.url,
    token: cfg.token,
    session_expires_at: cfg.session_expires_at ?? 0,
    allow_insecure_relay: cfg.allow_insecure_relay ?? false,
    disable_e2ee: cfg.disable_e2ee ?? false,
    remote_permission: cfg.remote_permission ?? "full",
    last_email: cfg.last_email ?? "",
    connected: false,
  });
}

export function clearRelayConfig(): Promise<void> {
  return bindings().ClearRelayConfig();
}

// setRelayDisableE2EE flips the agent-side seal toggle directly without
// touching URL / token / permission — used by Settings when the user
// wants the change to take effect immediately, not on the next "Save".
export function setRelayDisableE2EE(disabled: boolean): Promise<void> {
  return bindings().SetRelayDisableE2EE(disabled);
}

export function setUplinkPaused(paused: boolean): Promise<void> {
  return bindings().SetUplinkPaused(paused);
}

// loginRemoteRelay drives the OPAQUE login flow on the Go side. The Wails
// method completes the protocol round-trip, persists the returned session
// token via SetRelayConfig, unlocks the account_key into App memory, and
// restarts the uplink — callers only need to refresh GetRelayConfig()
// afterwards.
export function loginRemoteRelay(relayURL: string, email: string, password: string, allowInsecure: boolean): Promise<void> {
  return bindings().LoginRemoteRelay(relayURL, email, password, allowInsecure);
}

// registerRemoteRelay drives the OPAQUE registration flow on the Go side.
// Same persistence semantics as loginRemoteRelay; claimToken is optional
// (supply the plaintext token printed by `atterm-relay` bootstrap to also
// promote the new user to admin, otherwise pass "").
export function registerRemoteRelay(relayURL: string, email: string, password: string, claimToken: string, allowInsecure: boolean): Promise<void> {
  return bindings().RegisterRemoteRelay(relayURL, email, password, claimToken, allowInsecure);
}

// getAccountKey returns the unlocked account_key as a base64 std string,
// or empty string when locked / no user. The Wails platform layer
// caches this in JS memory so MetaPayload.Sealed decrypt in the hot
// path stays synchronous. See platform/wails.ts.
export function getAccountKey(): Promise<string> {
  return bindings().GetAccountKey();
}

// loadSavedRelayPassword reads the OPAQUE password that the most recent
// successful loginRemoteRelay / registerRemoteRelay persisted for the
// current relay URL + email. Returns "" when nothing is stored so the
// SettingsRelay password field can default to empty without extra checks.
export function loadSavedRelayPassword(): Promise<string> {
  return bindings().LoadSavedRelayPassword();
}

// rememberRelayPassword persists the typed-but-not-yet-verified password
// into the safekeyring slot tied to the current (relay URL, email) pair.
// Used by SettingsRelay's rememberInputs() on failed-connect paths so the
// password field is prefilled on the next attempt. Best-effort on the Go
// side — never throws even if the keychain is unavailable.
export function rememberRelayPassword(password: string): Promise<void> {
  return bindings().RememberRelayPassword(password);
}

// probeRelayVersion calls the Wails ProbeRelayVersion method on the Go side
// to verify the URL points at an atterm relay. Throws on probe failure.
// allowInsecure skips TLS verification so a self-signed relay is reachable.
export function probeRelayVersion(url: string, allowInsecure: boolean): Promise<void> {
  return bindings().ProbeRelayVersion(url, allowInsecure);
}

// fetchRelayMe calls the Go backend (FetchRelayMe Wails binding) which makes
// an HTTP GET to the configured relay's /api/me endpoint using the stored API
// token. The returned email is held in memory only (SEC-1 — not persisted).
export function fetchRelayMe(): Promise<RelayMe> {
  return bindings().FetchRelayMe();
}

export function listRelaySessions(): Promise<RelaySessionRow[]> {
  return bindings().ListRelaySessions();
}

export function revokeRelaySession(idHash: string): Promise<void> {
  return bindings().RevokeRelaySession(idHash);
}

export function signOutOtherRelaySessions(): Promise<SignOutOthersResult> {
  return bindings().SignOutOtherRelaySessions();
}

// createPairingToken asks the relay to mint a 5-minute single-use pairing
// token via the desktop's existing API-token-authenticated channel. The
// returned qr_url is the value to encode into the QR image.
export function createPairingToken(): Promise<PairingToken> {
  return bindings().CreatePairingToken();
}

export function getDiagnostics(userAgent: string): Promise<DiagnosticsPayload> {
  return bindings().GetDiagnostics(userAgent);
}

export function exportDiagnostics(content: string): Promise<string> {
  return bindings().ExportDiagnostics(content);
}

// listRemoteSessions fetches the relay's owner-filtered session list through
// the Go backend (App.ListRemoteSessions) rather than a direct webview
// WebSocket — the WKWebView TLS handshake to the relay is fingerprint-RST on
// some networks while Go's TLS passes. The Go side returns the raw
// /api/sessions JSON, the same SessionInfo[] shape the WS LIST_RESP carries.
export async function listRemoteSessions(): Promise<SessionInfo[]> {
  const raw = await bindings().ListRemoteSessions();
  const parsed = JSON.parse(raw) as SessionInfo[] | null;
  // The Go side returns the relay's raw JSON verbatim, so the E2EE-sealed
  // title/cwd/command fields are still ciphertext here — decrypt them with the
  // unlocked account_key the same way the WS META and Capacitor list paths do.
  return decryptSessionFields(parsed ?? []);
}
