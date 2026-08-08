package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/attson/atterm/desktop/feishu"
	internalfeishu "github.com/attson/atterm/internal/feishu"
)

// ("auto" | "local" | "relay"). Empty / unknown values resolve to "auto".
func (a *App) GetFeishuModePref() string {
	if a.cfgStore == nil {
		return "auto"
	}
	return a.cfgStore.Get().FeishuModePrefOrDefault()
}

// SetFeishuModePref persists the new preference and triggers a hot
// reconcile of the running Feishu service. Validates against the three
// known values; rejects anything else without mutating state.
func (a *App) SetFeishuModePref(pref string) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store unavailable")
	}
	switch pref {
	case "auto", "local", "relay":
	default:
		return fmt.Errorf("invalid feishu mode preference %q (want auto|local|relay)", pref)
	}
	cfg := a.cfgStore.Get()
	cfg.FeishuModePref = pref
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.markPrefDirtyAndPush("feishu_mode_pref")
	a.reconcileFeishuMode(a.ctx, cfg)
	return nil
}

// GetFeishuEffectiveMode returns the currently-running Feishu mode
// ("local" | "relay"), or "" before startFeishu has run. Independent
// of the persisted preference — reflects the actual swapped state.
func (a *App) GetFeishuEffectiveMode() string {
	a.feishuMu.RLock()
	defer a.feishuMu.RUnlock()
	return a.feishuMode
}

// FeishuRemoteTerminalSettings is returned by GetFeishuRemoteTerminalSettings.
type FeishuRemoteTerminalSettings struct {
	Enabled    bool   `json:"enabled"`
	AutoAttach string `json:"auto_attach"`
}

// GetFeishuRemoteTerminalSettings returns the current binding's remote
// terminal settings. Returns defaults (false, "ai") when the relay host is
// unavailable or no binding exists.
//
// Wails-bound methods must not declare context.Context in their signature.
func (a *App) GetFeishuRemoteTerminalSettings() (FeishuRemoteTerminalSettings, error) {
	defaults := FeishuRemoteTerminalSettings{Enabled: false, AutoAttach: "ai"}
	if a.ctx == nil {
		return defaults, nil
	}
	// Local mode: read the keychain blob.
	if ls := a.localBindingStore(); ls != nil {
		v, err := ls.Get(a.ctx)
		if err != nil {
			return defaults, nil // no blob yet → defaults
		}
		autoAttach := v.SessionAutoAttach
		if autoAttach == "" {
			autoAttach = "ai"
		}
		return FeishuRemoteTerminalSettings{
			Enabled:    v.RemoteTerminalEnabled,
			AutoAttach: autoAttach,
		}, nil
	}
	// Relay mode: read the embedded sqlite binding (unchanged).
	if a.host == nil || a.host.sqliteStore == nil {
		return defaults, nil
	}
	b, err := a.host.sqliteStore.GetFeishuBinding(a.ctx, a.host.adminUserID)
	if err != nil {
		// Binding not yet created → return defaults, not an error.
		return defaults, nil
	}
	autoAttach := b.SessionAutoAttach
	if autoAttach == "" {
		autoAttach = "ai"
	}
	return FeishuRemoteTerminalSettings{
		Enabled:    b.RemoteTerminalEnabled,
		AutoAttach: autoAttach,
	}, nil
}

// SetFeishuRemoteTerminalSettings updates the remote terminal toggle and
// autoAttach mode for the current user's Feishu binding. If the enabled flag
// flipped, OnRemoteTerminalToggle is called to tear down (or arm) active
// subscribers.
//
// Wails-bound methods must not declare context.Context in their signature.
func (a *App) SetFeishuRemoteTerminalSettings(enabled bool, autoAttach string) error {
	if a.ctx == nil {
		return fmt.Errorf("app not ready")
	}
	// Local mode: write the keychain blob; the toggle side effect still runs
	// against the in-memory subscriber map below.
	if ls := a.localBindingStore(); ls != nil {
		prevEnabled := false
		if v, err := ls.Get(a.ctx); err == nil {
			prevEnabled = v.RemoteTerminalEnabled
		}
		if err := ls.SetRemoteTerminalSettings(a.ctx, enabled, autoAttach); err != nil {
			return err
		}
		if a.host != nil && prevEnabled != enabled {
			a.host.OnRemoteTerminalToggle(enabled)
		}
		return nil
	}
	// Relay mode: write the embedded sqlite binding (unchanged).
	if a.host == nil || a.host.sqliteStore == nil {
		return fmt.Errorf("relay host unavailable")
	}
	prev, _ := a.host.sqliteStore.GetFeishuBinding(a.ctx, a.host.adminUserID)
	if err := a.host.sqliteStore.SetRemoteTerminalSettings(a.ctx, a.host.adminUserID, enabled, autoAttach); err != nil {
		return err
	}
	if prev != nil && prev.RemoteTerminalEnabled != enabled {
		a.host.OnRemoteTerminalToggle(enabled)
	}
	return nil
}

// feishuServiceConfig builds the ServiceConfig for the current relay login
// state and returns it alongside the resolved mode ("relay" / "local"). Shared
// by startFeishu (first init) and reconcileFeishuMode (runtime switch).
func (a *App) feishuServiceConfig(cfg appConfig) (feishu.ServiceConfig, string) {
	loggedIn := cfg.RelayURL != "" && cfg.RelaySessionToken != "" && !cfg.RelayPaused

	effective := "local"
	switch cfg.FeishuModePrefOrDefault() {
	case "local":
		effective = "local"
	case "relay":
		if loggedIn {
			effective = "relay"
		} else {
			effective = "local"
			logWarn("feishu", "mode=relay requested but not effectively logged in (RelayPaused=%v); falling back to local", cfg.RelayPaused)
		}
	default: // "auto"
		if loggedIn {
			effective = "relay"
		} else {
			effective = "local"
		}
	}

	if effective == "relay" {
		// The stored relay URL is a WebSocket URL (wss://). The Feishu relay
		// store/token source make plain HTTP REST calls, and http.Client rejects
		// "wss"/"ws" ("unsupported protocol scheme"), so rewrite the scheme.
		return feishu.ServiceConfig{
			Mode:     feishu.ModeRelay,
			RelayURL: relayHTTPBase(cfg.RelayURL),
			// Reads cfgStore on each call so it stays current after token refresh.
			RelayToken: func() string {
				if a.cfgStore == nil {
					return ""
				}
				return a.cfgStore.Get().RelaySessionToken
			},
			// Same client the rest of the app uses for the relay: pins ALPN to
			// http/1.1 and trusts a self-signed relay when the user opted in.
			RelayHTTPClient: relayHTTPClient(cfg.AllowInsecureRelay, 10*time.Second),
			Sessions:        a.host,
		}, "relay"
	}
	return feishu.ServiceConfig{Mode: feishu.ModeLocal, Sessions: a.host}, "local"
}

// startFeishu constructs feishu.Service, starts the HookServer, writes the
// endpoint file, and wires up the relayHost. Called once from startup(); the
// runtime mode switch is handled by reconcileFeishuMode, which reuses the
// HookServer this starts.
func (a *App) startFeishu(ctx context.Context, cfg appConfig) {
	svcCfg, mode := a.feishuServiceConfig(cfg)

	svc, err := feishu.NewService(svcCfg)
	if err != nil {
		logError("feishu", "service init: %v", err)
		return
	}

	addr, _, err := svc.HookServer().Start()
	if err != nil {
		logError("feishu", "hook server start: %v", err)
		return
	}
	hookEndpoint := "http://" + addr + "/atterm-hook/notify"

	if err := feishu.WriteEndpointFile(hookEndpoint); err != nil {
		logError("feishu", "write feishu endpoint file: %v", err)
	}

	svc.HookServer().SetSuspectCallback(func() {
		// A misrouted POST may indicate stale install; nudge the
		// debounced auto-repair on next UI poll.
		hookInstallLastAttemptMu.Lock()
		hookInstallLastAttempt = time.Time{}
		hookInstallLastAttemptMu.Unlock()
	})

	if a.host != nil {
		a.host.FeishuHookEndpoint = hookEndpoint
		a.host.SetFeishuDispatcher(svc.Dispatcher())
		a.host.SetFeishuRemoteTermState(a.feishuRemoteTermState)
		// Wire the inbound router so LongConn card actions and reply messages
		// are routed through the CardIndex + FeishuSubscriber registry.
		router := internalfeishu.NewRouter(a.host.feishuCards, func(sessionID string) internalfeishu.Subscriber {
			a.host.feishuSubsMu.Lock()
			defer a.host.feishuSubsMu.Unlock()
			rec, ok := a.host.feishuSessions[sessionID]
			if !ok || rec == nil || rec.sub == nil {
				return nil
			}
			return rec.sub
		})
		svc.SetRouter(router)
	}

	if mode == "local" {
		if err := svc.EnsureLongConn(ctx); err != nil {
			// Not fatal — credentials may not be set yet.
			logError("feishu", "long-conn: %v", err)
		}
	}

	a.feishuMu.Lock()
	a.feishuService = svc
	a.feishuMode = mode
	a.feishuHookSrv = svc.HookServer()
	a.feishuHookEndpoint = hookEndpoint
	a.feishuMu.Unlock()
	logInfo("feishu", "service started (mode=%s endpoint=%s)", mode, hookEndpoint)
}

// reconcileFeishuMode rebuilds the Feishu service when the relay login state no
// longer matches the running mode (relay login/logout at runtime). The
// long-lived HookServer/listener is kept — only the dispatcher, store, token
// source and long-conn are swapped — so the ATTERM_HOOK_ENDPOINT already baked
// into open PTYs stays valid. No-op before first init or when already correct.
func (a *App) reconcileFeishuMode(ctx context.Context, cfg appConfig) {
	a.feishuMu.Lock()
	defer a.feishuMu.Unlock()
	if a.feishuService == nil || a.feishuHookSrv == nil {
		return // first init not done yet; startFeishu handles it
	}
	svcCfg, desired := a.feishuServiceConfig(cfg)
	if desired == a.feishuMode {
		return
	}
	newSvc, err := feishu.NewService(svcCfg)
	if err != nil {
		logError("feishu", "reload (%s→%s): %v", a.feishuMode, desired, err)
		return
	}
	// Stop the outgoing service's long-conn (relay mode has none).
	if err := a.feishuService.CloseLongConn(ctx); err != nil {
		logWarn("feishu", "close long-conn: %v", err)
	}
	// Repoint the persistent hook server + host at the new dispatcher. The
	// endpoint/port is unchanged, so already-spawned PTYs keep working.
	a.feishuHookSrv.SetDispatcher(newSvc.Dispatcher())
	if a.host != nil {
		a.host.SetFeishuDispatcher(newSvc.Dispatcher())
		a.host.SetFeishuRemoteTermState(a.feishuRemoteTermState)
		// Rebuild the inbound router for the new service so LongConn events
		// are routed through the same CardIndex + FeishuSubscriber registry.
		router := internalfeishu.NewRouter(a.host.feishuCards, func(sessionID string) internalfeishu.Subscriber {
			a.host.feishuSubsMu.Lock()
			defer a.host.feishuSubsMu.Unlock()
			rec, ok := a.host.feishuSessions[sessionID]
			if !ok || rec == nil || rec.sub == nil {
				return nil
			}
			return rec.sub
		})
		newSvc.SetRouter(router)
	}
	a.feishuService = newSvc
	a.feishuMode = desired
	if desired == "local" {
		if err := newSvc.EnsureLongConn(ctx); err != nil {
			logError("feishu", "long-conn after reload: %v", err)
		}
	}
	logInfo("feishu", "mode reconciled → %s (endpoint unchanged %s)", desired, a.feishuHookEndpoint)
}

// FeishuStatusResp is returned by GetFeishuStatus.
//
// Enabled / RelayDisabled / Error are mutually exclusive views the UI renders
// differently:
//   - Enabled=true: integration is active (see Bound/Disabled for detail).
//   - RelayDisabled=true: relay mode, but the relay admin turned Feishu off.
//   - Error!="": the status fetch failed (network/keychain/etc.) — state unknown.
//   - all zero: integration is not running on this client.
type FeishuStatusResp struct {
	Enabled  bool   `json:"enabled"`
	Mode     string `json:"mode"`
	Bound    bool   `json:"bound"`
	OpenID   string `json:"open_id"`
	Disabled bool   `json:"disabled"`
	// RelayDisabled is set when the relay responded that Feishu is disabled
	// server-side (HTTP 503). Distinct from "not configured".
	RelayDisabled bool `json:"relay_disabled,omitempty"`
	// Error carries a human-readable reason the status could not be fetched.
	// When set, the UI must NOT claim the integration is disabled — the real
	// state is unknown. Empty on success.
	Error string `json:"error,omitempty"`
	// Configured reports that app credentials are stored (regardless of bind
	// state). The UI uses it to render a "configured" view instead of an empty
	// form — credentials persist but are never echoed back, so without this the
	// form looks blank on reopen and reads as "not saved".
	Configured bool `json:"configured,omitempty"`
	// AppID echoes the stored (non-secret) App ID so the UI can show which app
	// is configured. Available in local mode; empty in relay mode (the relay
	// does not echo it back).
	AppID string `json:"app_id,omitempty"`
	// AppIDHash is sha256(AppID) — the suffix of the event callback URL.
	AppIDHash string `json:"app_id_hash,omitempty"`
	// CallbackURL is the relay event endpoint to paste into the Feishu console.
	// Set only in relay mode; empty in local mode (long-conn, no public URL).
	CallbackURL string `json:"callback_url,omitempty"`
}

// GetFeishuStatus returns the current Feishu integration state.
//
// Wails-bound methods must not declare context.Context in their signature —
// the generator surfaces it as a required JS argument, so the frontend call
// fails with "received 0 arguments, expected 1". Internal callers (long-conn,
// store, dispatcher) get the lifecycle context via a.ctx.
func (a *App) GetFeishuStatus() (FeishuStatusResp, error) {
	svc, mode := a.currentFeishu()
	if svc == nil {
		return FeishuStatusResp{Enabled: false}, nil
	}
	v, err := svc.Store().Get(a.ctx)
	if errors.Is(err, feishu.ErrLocalBindingNotFound) {
		return FeishuStatusResp{
			Enabled: true,
			Mode:    mode,
			Bound:   false,
		}, nil
	}
	if errors.Is(err, feishu.ErrRelayFeishuDisabled) {
		// The relay reachable but the admin turned Feishu off server-side.
		return FeishuStatusResp{Mode: mode, RelayDisabled: true}, nil
	}
	if err != nil {
		// Status couldn't be fetched (network, keychain, relay error). Report
		// it as a non-nil Error rather than returning a Go error: the latter
		// surfaces as a rejected Promise that the UI silently swallowed, which
		// is exactly what made a transient failure look like "not enabled".
		return FeishuStatusResp{Mode: mode, Error: err.Error()}, nil
	}
	// Local mode keeps full credentials in the keychain; a blob with an OpenID
	// but no AppSecret (e.g. a stale bind left over after switching modes) is
	// effectively unconfigured — the long-conn and token mint both need the
	// secret. Treat it as "not configured" so the UI shows the credentials
	// form instead of a misleading "bound" view that can't actually send.
	// Relay mode never echoes the secret back, so this check is local-only.
	if mode == "local" && v.AppSecret == "" {
		return FeishuStatusResp{
			Enabled: true,
			Mode:    mode,
			Bound:   false,
		}, nil
	}
	return FeishuStatusResp{
		Enabled:     true,
		Mode:        mode,
		Bound:       v.OpenID != "",
		OpenID:      v.OpenID,
		Disabled:    v.DisabledAt != 0,
		Configured:  true,
		AppID:       v.AppID,
		AppIDHash:   v.AppIDHash,
		CallbackURL: v.CallbackURL,
	}, nil
}

// currentFeishu returns the live service + mode under the read lock. Both may
// be swapped at runtime by reconcileFeishuMode on a relay login/logout.
func (a *App) currentFeishu() (*feishu.Service, string) {
	a.feishuMu.RLock()
	defer a.feishuMu.RUnlock()
	return a.feishuService, a.feishuMode
}

// localBindingStore returns the keychain-backed store when Feishu is running in
// local mode, or nil otherwise. Used to route remote-terminal settings to the
// keychain (relay mode keeps them in the embedded sqlite store).
func (a *App) localBindingStore() *feishu.LocalKeychainBindingStore {
	svc, mode := a.currentFeishu()
	if svc == nil || mode != "local" {
		return nil
	}
	ls, _ := svc.Store().(*feishu.LocalKeychainBindingStore)
	return ls
}

// feishuRemoteTermState reads the remote-terminal gate state for the live mode:
// the keychain blob in local mode, the embedded sqlite binding in relay mode.
// Returns ok=false when no binding exists yet or the store is unavailable.
func (a *App) feishuRemoteTermState(ctx context.Context) (enabled bool, openID, autoAttach string, ok bool) {
	if ls := a.localBindingStore(); ls != nil {
		v, err := ls.Get(ctx)
		if err != nil {
			return false, "", "", false
		}
		aa := v.SessionAutoAttach
		if aa == "" {
			aa = "ai"
		}
		return v.RemoteTerminalEnabled, v.OpenID, aa, true
	}
	if a.host == nil || a.host.sqliteStore == nil {
		return false, "", "", false
	}
	b, err := a.host.sqliteStore.GetFeishuBinding(ctx, a.host.adminUserID)
	if err != nil {
		return false, "", "", false
	}
	aa := b.SessionAutoAttach
	if aa == "" {
		aa = "ai"
	}
	return b.RemoteTerminalEnabled, b.OpenID, aa, true
}

// SetFeishuCredentials saves app credentials and (re)starts the long-conn.
func (a *App) SetFeishuCredentials(c feishu.Credentials) error {
	svc, _ := a.currentFeishu()
	if svc == nil {
		return errors.New("feishu disabled")
	}
	if err := svc.Store().SetCredentials(a.ctx, c); err != nil {
		return err
	}
	return svc.EnsureLongConn(a.ctx)
}

// BeginFeishuPair issues a short-code that the user sends to the bot via
// private chat to complete the bind flow. In relay mode the code is issued by
// the relay; in local mode it is generated in-process.
func (a *App) BeginFeishuPair() (string, error) {
	svc, _ := a.currentFeishu()
	if svc == nil {
		return "", errors.New("feishu disabled")
	}
	return svc.BeginPair(a.ctx)
}

// DeleteFeishuBinding removes the bound OpenID from the store.
func (a *App) DeleteFeishuBinding() error {
	svc, _ := a.currentFeishu()
	if svc == nil {
		return errors.New("feishu disabled")
	}
	return svc.Store().Delete(a.ctx)
}

// SendFeishuTestCard renders and sends one notification card to the bound
// OpenID through the live token + IM path, so the user can verify delivery from
// Settings without waiting for a real trigger. scenario is one of the
// feishu.TestCard* values ("command_success", "command_failure",
// "command_sealed", "waiting_input"). Any failure (not configured, disabled,
// unbound, or send error) is returned verbatim for the UI to display.
func (a *App) SendFeishuTestCard(scenario string) error {
	svc, _ := a.currentFeishu()
	if svc == nil {
		return errors.New("feishu disabled")
	}
	return svc.SendTestCard(a.ctx, feishu.TestCardScenario(scenario))
}
