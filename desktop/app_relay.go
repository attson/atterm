package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/attson/atterm/internal/e2eeclient"
)

// LoginRemoteRelay calls POST /api/auth/login on the given relay URL with the
// supplied credentials, parses the returned {session_token, expires_at, user}
// envelope, and persists (relayURL, session_token) to local config via
// SetRelayConfig. Bound to the frontend's "Connect to remote relay" form.
//
// The user-facing input is the HTTP(S) URL of the relay (the same URL their
// browser hits). We POST to that URL directly and normalize the scheme to
// ws:// or wss:// before persistence — the uplink and validateRelayEndpoint
// both expect the WebSocket form. HTTP API calls translate back on the fly
// (see MarkSessionsSeen et al.).
//
// allowInsecure mirrors the "enable insecure mode" toggle on the form. It
// applies to the SetRelayConfig call that persists the new session token so
// the validator sees the user's latest intent, not the previously persisted
// flag — without this, toggling the checkbox in the UI and clicking save
// rejects ws:// targets even though the user just opted in.
func (a *App) LoginRemoteRelay(relayURL, email, password string, allowInsecure bool) error {
	relayURL = strings.TrimRight(strings.TrimSpace(relayURL), "/")
	if relayURL == "" {
		return fmt.Errorf("relay url is empty")
	}
	httpURL, wsURL, err := relayLoginEndpoints(relayURL)
	if err != nil {
		return err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	c := &e2eeclient.Client{BaseURL: httpURL, HTTPClient: relayHTTPClient(allowInsecure, 0)}
	res, err := c.Login(ctx, email, password)
	if err != nil {
		return fmt.Errorf("relay OPAQUE login: %w", err)
	}
	// Set the in-memory key now so the uplink (started by SetRelayConfig below)
	// seals its first announce. Persistence is deferred until the new relay URL
	// and user id are committed to config — see persistAccountKey below.
	a.setAccountKeyInMemory(res.AccountKey)
	// RemotePermission is preserved from the persisted config (the login form
	// doesn't surface it). AllowInsecureRelay comes from the call argument so
	// the validator inside SetRelayConfig sees the form's current checkbox
	// state rather than the previously persisted flag — necessary for
	// ws:// targets the user is just now opting into. Session expiry is no
	// longer returned by the OPAQUE login response (it lives entirely on the
	// relay side); the frontend will rely on 401-on-expiry instead.
	prev := a.GetRelayConfig()
	if err := a.SetRelayConfig(RelayConfig{
		URL:                wsURL,
		Token:              res.SessionToken,
		AllowInsecureRelay: allowInsecure,
		RemotePermission:   prev.RemotePermission,
	}); err != nil {
		return err
	}
	// Persist the email and user id separately — RelayConfig.LastEmail is
	// read-only from the frontend's perspective (SetRelayConfig intentionally
	// ignores it), so LoginRemoteRelay writes the cfgStore directly.
	if a.cfgStore != nil {
		cfg := a.cfgStore.Get()
		cfg.RelayLastEmail = email
		cfg.RelaySessionUserID = res.UserID
		cfg.RelayRealmID = res.RealmID
		cfg.RelayHomeInstanceURL = res.HomeInstanceURL
		if err := a.cfgStore.Set(cfg); err != nil {
			return err
		}
	}
	// Persist the account_key now that the realm id + user id are committed,
	// so the next launch's loadAccountKey(cfg.RelayRealmID, cfg.RelaySessionUserID)
	// finds it. Done after the config write — persisting earlier wrote under the
	// stale/empty user id and lost the key on relaunch.
	a.persistAccountKey(res.AccountKey)
	// Persist the password so SettingsRelay can prefill the password
	// field on subsequent launches. Failure is logged but does not fail
	// the login: the user already has a valid session token and account_key.
	// See docs/superpowers/specs/2026-06-23-desktop-relay-password-persistence-design.md.
	if err := saveRelayPassword(wsURL, email, password); err != nil {
		logWarn("relay", "save relay password: %v", err)
	}
	// Pull + (first-login) seed + push, run as one exclusive unit on the
	// serial sync loop — see enqueuePostLoginSeed in prefs_sync_loop.go,
	// which is also where the emit-on-every-branch behaviour this used to
	// inline here now lives.
	go a.enqueuePostLoginSeed()
	return nil
}

// RegisterRemoteRelay creates a fresh OPAQUE-authenticated account on the
// remote relay (POST /api/auth/register/init + /finalize via SDK), mints a
// session token, persists URL+token+email locally, and stores the freshly
// generated account_key in memory. claimToken is optional — supply the
// plaintext token printed by `atterm-relay` bootstrap to also promote the
// new user to admin.
//
// On success behaves identically to LoginRemoteRelay (same SetRelayConfig
// + prefsSync seed path); on failure the call returns the underlying
// SDK error verbatim so the frontend can surface a meaningful message.
func (a *App) RegisterRemoteRelay(relayURL, email, password, claimToken string, allowInsecure bool) error {
	relayURL = strings.TrimRight(strings.TrimSpace(relayURL), "/")
	if relayURL == "" {
		return fmt.Errorf("relay url is empty")
	}
	httpURL, wsURL, err := relayLoginEndpoints(relayURL)
	if err != nil {
		return err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	c := &e2eeclient.Client{BaseURL: httpURL, HTTPClient: relayHTTPClient(allowInsecure, 0)}
	res, err := c.Register(ctx, email, password, claimToken)
	if err != nil {
		return fmt.Errorf("relay OPAQUE register: %w", err)
	}
	// In-memory now (uplink seals first announce); persist after URL + user id
	// are committed — see persistAccountKey below and LoginRemoteRelay.
	a.setAccountKeyInMemory(res.AccountKey)
	prev := a.GetRelayConfig()
	if err := a.SetRelayConfig(RelayConfig{
		URL:                wsURL,
		Token:              res.SessionToken,
		AllowInsecureRelay: allowInsecure,
		RemotePermission:   prev.RemotePermission,
	}); err != nil {
		return err
	}
	if a.cfgStore != nil {
		cfg := a.cfgStore.Get()
		cfg.RelayLastEmail = email
		cfg.RelaySessionUserID = res.UserID
		cfg.RelayRealmID = res.RealmID
		// Register never assigns a home instance (the relay sets home on login only);
		// clear any stale home from a prior account so the uplink falls back to RelayURL.
		cfg.RelayHomeInstanceURL = ""
		if err := a.cfgStore.Set(cfg); err != nil {
			return err
		}
	}
	a.persistAccountKey(res.AccountKey)
	// Persist the password so SettingsRelay can prefill the password
	// field on subsequent launches. Failure is logged but does not fail
	// the registration: the user already has a valid session token and account_key.
	// See docs/superpowers/specs/2026-06-23-desktop-relay-password-persistence-design.md.
	if err := saveRelayPassword(wsURL, email, password); err != nil {
		logWarn("relay", "save relay password: %v", err)
	}
	return nil
}

// LoadSavedRelayPassword reads the password persisted by the most recent
// successful LoginRemoteRelay / RegisterRemoteRelay for the relay currently
// in the persisted config. Returns "" (no error) when nothing is stored,
// when RelayURL or RelayLastEmail is empty, or when the keychain entry is
// absent. Keychain errors other than "not found" are logged and surfaced
// as "" so the UI just shows an empty password field.
//
// Bound to the frontend's SettingsRelay onMounted prefill.
func (a *App) LoadSavedRelayPassword() (string, error) {
	if a.cfgStore == nil {
		return "", nil
	}
	cfg := a.cfgStore.Get()
	pw, err := loadRelayPassword(cfg.RelayURL, cfg.RelayLastEmail)
	if err != nil {
		logWarn("relay", "load saved relay password: %v", err)
		return "", nil
	}
	return pw, nil
}

// RememberRelayPassword writes password into the safekeyring slot for the
// (RelayURL, RelayLastEmail) currently in cfgStore — used by the Settings
// form's "remember inputs on failed connect" path so the user does not
// have to retype the password after a probe failure / login failure /
// network blip. Empty password is intentionally treated as a no-op rather
// than a delete here, so a failure path with an empty password field
// cannot wipe an existing stored value.
//
// Best-effort: errors are logged and not surfaced to the UI (the real
// failure the user cares about is the underlying connect error). Caller
// is expected to have already called SetRelayConfig with the new URL +
// email so the slot key reflects the latest intent.
func (a *App) RememberRelayPassword(password string) error {
	if a.cfgStore == nil {
		return nil
	}
	if password == "" {
		return nil
	}
	cfg := a.cfgStore.Get()
	if err := saveRelayPassword(cfg.RelayURL, cfg.RelayLastEmail, password); err != nil {
		logWarn("relay", "remember relay password: %v", err)
	}
	return nil
}

// ProbeRelayVersion does a lightweight GET <relayURL>/api/version to verify
// the URL points at an atterm relay. Returns nil if the response is 200 and
// the JSON body has a non-empty "version" field. Otherwise returns an error
// the frontend surfaces as "无法连接到 relay" inline beneath the URL field.
//
// /api/version is auth-less per the session-token spec, so no credentials
// are sent. 5-second timeout keeps the UI from blocking on a stalled
// connection — the user can re-click "保存并连接" if the relay just woke up.
func (a *App) ProbeRelayVersion(relayURL string, allowInsecure bool) error {
	relayURL = strings.TrimRight(strings.TrimSpace(relayURL), "/")
	if relayURL == "" {
		return fmt.Errorf("relay url is empty")
	}
	httpURL, _, err := relayLoginEndpoints(relayURL)
	if err != nil {
		return fmt.Errorf("invalid relay url: %w", err)
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", httpURL+"/api/version", nil)
	if err != nil {
		return err
	}
	client := relayHTTPClient(allowInsecure, 5*time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	var out struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("not an atterm relay (decode): %w", err)
	}
	if out.Version == "" {
		return fmt.Errorf("not an atterm relay (no version field)")
	}
	return nil
}

// relayLoginEndpoints normalizes a user-entered relay URL into the (http(s),
// ws(s)) pair we need. Accepts http://, https://, ws://, wss:// — anything
// else is rejected so the caller sees a clear error before the POST.
func relayLoginEndpoints(raw string) (httpURL, wsURL string, err error) {
	u, perr := url.Parse(raw)
	if perr != nil || u.Host == "" {
		return "", "", fmt.Errorf("invalid relay url %q", raw)
	}
	switch u.Scheme {
	case "http", "ws":
		httpURL = "http://" + u.Host + u.Path
		wsURL = "ws://" + u.Host + u.Path
	case "https", "wss":
		httpURL = "https://" + u.Host + u.Path
		wsURL = "wss://" + u.Host + u.Path
	default:
		return "", "", fmt.Errorf("relay url scheme must be http(s) or ws(s), got %q", u.Scheme)
	}
	return strings.TrimRight(httpURL, "/"), strings.TrimRight(wsURL, "/"), nil
}

// Email is never logged or persisted (SEC-1).
type RelayMe struct {
	UserID  string `json:"user_id"`
	Email   string `json:"email"`
	IsAdmin bool   `json:"is_admin"`
}

// FetchRelayMe queries the configured relay's /api/me endpoint using the
// stored API token and returns the user's identity. The desktop UI calls
// this after receiving a relay:auth-info event to display the email in the
// status row. Email is held in-memory only and is never written to disk.
func (a *App) FetchRelayMe() (RelayMe, error) {
	if a.cfgStore == nil {
		return RelayMe{}, fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		return RelayMe{}, fmt.Errorf("no relay configured")
	}
	// Convert WS scheme to HTTP so we can use net/http.
	baseHTTP := relayHTTPBase(cfg.RelayURL)
	req, err := http.NewRequest("GET", baseHTTP+"/api/me", nil)
	if err != nil {
		return RelayMe{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.RelaySessionToken)
	resp, err := relayHTTPClient(cfg.AllowInsecureRelay, 0).Do(req)
	if err != nil {
		return RelayMe{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return RelayMe{}, fmt.Errorf("relay /api/me returned status %d", resp.StatusCode)
	}
	var out RelayMe
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return RelayMe{}, err
	}
	return out, nil
}

// ListRelaySessions returns every active session for the currently
// logged-in relay account. Bound to Settings → Signed-in Devices tab.
func (a *App) ListRelaySessions() ([]RelaySessionRow, error) {
	if a.cfgStore == nil {
		return nil, fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		return nil, fmt.Errorf("not authenticated")
	}
	return meSessionsGET(a.ctx, relayHTTPBase(cfg.RelayURL), cfg.RelaySessionToken, cfg.AllowInsecureRelay)
}

// RevokeRelaySession revokes one session by id_hash. The current
// session cannot be revoked through this method (the relay endpoint
// itself refuses to revoke the caller's own session, so no extra
// guard is needed here).
func (a *App) RevokeRelaySession(idHash string) error {
	idHash = strings.TrimSpace(idHash)
	if idHash == "" {
		return fmt.Errorf("id_hash is empty")
	}
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		return fmt.Errorf("not authenticated")
	}
	return meSessionDELETE(a.ctx, relayHTTPBase(cfg.RelayURL), cfg.RelaySessionToken, idHash, cfg.AllowInsecureRelay)
}

// SignOutOtherRelaySessions revokes every session except the current
// one. Returns the number of sessions revoked.
func (a *App) SignOutOtherRelaySessions() (SignOutOthersResult, error) {
	if a.cfgStore == nil {
		return SignOutOthersResult{}, fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		return SignOutOthersResult{}, fmt.Errorf("not authenticated")
	}
	return meSessionsSignOutOthers(a.ctx, relayHTTPBase(cfg.RelayURL), cfg.RelaySessionToken, cfg.AllowInsecureRelay)
}

// PairingTokenResponse is what the renderer receives when generating a QR code.
// Mirrors the relay's /api/pair/create response body.
type PairingTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	QRURL     string `json:"qr_url"`
	// Wrapped is true iff the QR URL carries an AEAD-sealed account_key
	// (?&k=<wk>). false when the desktop's account_key was locked at
	// generation time or wrapping failed; the mobile can still consume the
	// pair to obtain a session token but sealed session fields will not
	// decrypt.
	Wrapped bool `json:"wrapped"`
}

// CreatePairingToken asks the configured relay to mint a 5-minute single-use
// pairing token for the desktop's current user and returns the response,
// including the qr_url to encode into a QR code.
//
// If the desktop's account_key is currently unlocked, it is sealed into an
// AEAD envelope and shipped to the relay as part of the create request; the
// fresh wrap key is then appended to the QR URL as &k=<wk> so the mobile can
// recover the account_key without it ever touching the relay in the clear.
// Wrap failures are logged and swallowed — pairing still proceeds without
// the account_key, matching Wrapped=false.
func (a *App) CreatePairingToken() (PairingTokenResponse, error) {
	if a.cfgStore == nil {
		return PairingTokenResponse{}, fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		return PairingTokenResponse{}, fmt.Errorf("no relay configured")
	}

	// Snapshot account_key. If unlocked and wrap succeeds, ship the
	// ciphertext up and remember the wrap key for the QR URL suffix.
	var wrapB64 string
	var wk []byte
	if ak := a.accountKeySnapshot(); len(ak) > 0 {
		env, key, err := wrapAccountKey(ak)
		if err != nil {
			logWarn("e2ee", "wrap account_key for pair: %v (falling back to no-wrap QR)", err)
		} else {
			wrapB64 = base64.StdEncoding.EncodeToString(env)
			wk = key
		}
	}

	body := map[string]string{}
	if wrapB64 != "" {
		body["wrap"] = wrapB64
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return PairingTokenResponse{}, err
	}

	baseHTTP := relayHTTPBase(cfg.RelayURL)
	req, err := http.NewRequest("POST", baseHTTP+"/api/pair/create", bytes.NewReader(bodyBytes))
	if err != nil {
		return PairingTokenResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.RelaySessionToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := relayHTTPClient(cfg.AllowInsecureRelay, 0).Do(req)
	if err != nil {
		return PairingTokenResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return PairingTokenResponse{}, fmt.Errorf("relay /api/pair/create returned status %d", resp.StatusCode)
	}
	var out PairingTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return PairingTokenResponse{}, err
	}

	if len(wk) > 0 {
		sep := "&"
		if !strings.Contains(out.QRURL, "?") {
			sep = "?"
		}
		out.QRURL = out.QRURL + sep + "k=" + base64.RawURLEncoding.EncodeToString(wk)
		out.Wrapped = true
	}
	return out, nil
}

// recordRelayError appends an error entry to the recent-errors ring buffer.
// Nil errors are dropped. Messages are passed through redactErrorLine so
// tokens / Authorization / Cookie values are masked. Newest-first ordering;
// when the buffer is full the oldest entry falls off.
func (a *App) recordRelayError(err error) {
	if err == nil {
		return
	}
	entry := RelayErrorEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Message:   redactErrorLine(err.Error()),
	}
	a.relayErrMu.Lock()
	defer a.relayErrMu.Unlock()
	a.relayErrors = append([]RelayErrorEntry{entry}, a.relayErrors...)
	if len(a.relayErrors) > maxRelayErrors {
		a.relayErrors = a.relayErrors[:maxRelayErrors]
	}
}

// snapshotRelayErrors returns a copy of the recent-errors ring buffer.
// Callers receive a fresh slice safe to mutate; the underlying buffer is
// unaffected.
func (a *App) snapshotRelayErrors() []RelayErrorEntry {
	a.relayErrMu.Lock()
	defer a.relayErrMu.Unlock()
	out := make([]RelayErrorEntry, len(a.relayErrors))
	copy(out, a.relayErrors)
	return out
}

// relayHTTPBase rewrites a stored relay WebSocket URL (wss://, ws://) to the
// HTTP scheme its REST endpoints are served over. http.Client rejects "wss"/
// "ws" with "unsupported protocol scheme", so every HTTP call to the relay must
// go through this first. A URL already using http(s):// is returned unchanged.
func relayHTTPBase(relayURL string) string {
	return strings.Replace(strings.Replace(relayURL, "wss://", "https://", 1), "ws://", "http://", 1)
}
