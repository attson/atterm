package main

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/attson/atterm/internal/peerdiscovery"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/rendezvousclient"
	"github.com/pion/webrtc/v4"
)

const (
	peerRendezvousRetryMin            = 500 * time.Millisecond
	peerRendezvousRetryMax            = 8 * time.Second
	peerRendezvousRegistrationTimeout = 10 * time.Second
)

type peerRendezvousRuntimeState struct {
	State            string
	LastRegisteredAt int64
	RegistrationMS   int64
	LastErrorCode    string
	NextRetryAt      int64
}

// PeerRendezvousStatus is a redaction-safe operational projection. Opaque
// topic/presence identifiers and signaling payloads never enter this type.
type PeerRendezvousStatus struct {
	Mode             string `json:"mode"`
	State            string `json:"state"`
	URL              string `json:"url,omitempty"`
	LastRegisteredAt int64  `json:"last_registered_at,omitempty"`
	RegistrationMS   int64  `json:"registration_ms,omitempty"`
	ReachablePeers   int    `json:"reachable_peers"`
	LastErrorCode    string `json:"last_error_code,omitempty"`
	NextRetryAt      int64  `json:"next_retry_at,omitempty"`
}

type peerRendezvousHostFactory func(
	context.Context,
	context.Context,
	*App,
	*relayHost,
	rendezvousclient.PresenceConfig,
	webrtc.Configuration,
) (*peerRendezvousHost, error)

type peerRendezvousLifecycle struct {
	app      *App
	host     *relayHost
	resolved rendezvousclient.ResolvedConfig
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}

	mu     sync.Mutex
	active *peerRendezvousHost
	state  peerRendezvousRuntimeState

	registrationTimeout time.Duration
	registerHost        peerRendezvousHostFactory
}

func (a *App) reconcilePeerRendezvous(cfg appConfig) {
	if a == nil || a.ctx == nil || a.host == nil {
		return
	}
	a.peerRendezvousReconcileMu.Lock()
	defer a.peerRendezvousReconcileMu.Unlock()
	resolved, err := rendezvousclient.ResolveConfig(rendezvousclient.Config{
		Mode: rendezvousclient.Mode(cfg.PeerRendezvousMode), URL: cfg.PeerRendezvousURL,
		STUNMode: rendezvousclient.STUNMode(cfg.PeerSTUNMode), STUNURLs: cfg.PeerSTUNURLs,
	}, false)
	if err != nil {
		logWarn("rendezvous", "configuration rejected: %v", err)
		return
	}
	a.stopPeerRendezvousLocked()
	if resolved.Mode == rendezvousclient.ModeDisabled || resolved.BaseURL == "" {
		return
	}
	manager, err := a.peerManager()
	if err != nil {
		logWarn("rendezvous", "Peer manager unavailable: %v", err)
		return
	}
	status, err := manager.status()
	if err != nil || !status.Configured {
		return
	}
	ctx, cancel := context.WithCancel(a.ctx)
	lifecycle := &peerRendezvousLifecycle{
		app: a, host: a.host, resolved: resolved, ctx: ctx, cancel: cancel, done: make(chan struct{}),
		state: peerRendezvousRuntimeState{State: "connecting"},
	}
	a.peerRendezvousMu.Lock()
	a.peerRendezvous = lifecycle
	a.peerRendezvousMu.Unlock()
	go lifecycle.run()
}

func (a *App) stopPeerRendezvous() {
	if a == nil {
		return
	}
	a.peerRendezvousReconcileMu.Lock()
	defer a.peerRendezvousReconcileMu.Unlock()
	a.stopPeerRendezvousLocked()
}

func (a *App) stopPeerRendezvousLocked() {
	a.peerRendezvousMu.Lock()
	lifecycle := a.peerRendezvous
	a.peerRendezvous = nil
	a.peerRendezvousMu.Unlock()
	if lifecycle != nil {
		lifecycle.Stop()
	}
}

func (l *peerRendezvousLifecycle) run() {
	defer close(l.done)
	retry := peerRendezvousRetryMin
	for l.ctx.Err() == nil {
		l.setConnecting()
		webRTC := webrtc.Configuration{}
		if len(l.resolved.STUNURLs) != 0 {
			webRTC.ICEServers = []webrtc.ICEServer{{URLs: append([]string(nil), l.resolved.STUNURLs...)}}
		}
		startedAt := time.Now()
		registrationCtx, cancelRegistration := context.WithTimeout(l.ctx, l.registrationTimeoutValue())
		registerHost := l.registerHost
		if registerHost == nil {
			registerHost = newPeerRendezvousHostWithRegistrationContext
		}
		host, err := registerHost(l.ctx, registrationCtx, l.app, l.host, rendezvousclient.PresenceConfig{
			ServiceURL: l.resolved.BaseURL,
		}, webRTC)
		cancelRegistration()
		if err != nil {
			if l.ctx.Err() != nil {
				return
			}
			logWarn("rendezvous", "registration failed: %v", err)
			l.setFailure(classifyPeerRendezvousError(err), retry)
			if !waitPeerRendezvous(l.ctx, retry) {
				return
			}
			retry = time.Duration(math.Min(float64(peerRendezvousRetryMax), float64(retry*2)))
			continue
		}
		retry = peerRendezvousRetryMin
		l.mu.Lock()
		l.active = host
		registrationMS := time.Since(startedAt).Milliseconds()
		if registrationMS < 1 {
			registrationMS = 1
		}
		l.state.State = "online"
		l.state.LastRegisteredAt = time.Now().Unix()
		l.state.RegistrationMS = registrationMS
		l.state.LastErrorCode = ""
		l.state.NextRetryAt = 0
		l.mu.Unlock()
		rotation := time.NewTimer(untilNextPeerPresenceRotation(time.Now()))
		rotated := false
		select {
		case <-l.ctx.Done():
		case <-host.route.Done():
		case <-rotation.C:
			rotated = true
		}
		if !rotation.Stop() {
			select {
			case <-rotation.C:
			default:
			}
		}
		host.Close()
		l.mu.Lock()
		if l.active == host {
			l.active = nil
		}
		l.mu.Unlock()
		if l.ctx.Err() != nil {
			return
		}
		if rotated {
			l.setConnecting()
		} else {
			l.setFailure("service_unavailable", retry)
		}
		if !waitPeerRendezvous(l.ctx, retry) {
			return
		}
	}
}

func (l *peerRendezvousLifecycle) registrationTimeoutValue() time.Duration {
	if l.registrationTimeout > 0 {
		return l.registrationTimeout
	}
	return peerRendezvousRegistrationTimeout
}

func (l *peerRendezvousLifecycle) setConnecting() {
	l.mu.Lock()
	l.state.State = "connecting"
	l.state.NextRetryAt = 0
	l.mu.Unlock()
}

func (l *peerRendezvousLifecycle) setFailure(code string, retry time.Duration) {
	l.mu.Lock()
	l.state.State = "error"
	l.state.LastErrorCode = code
	l.state.NextRetryAt = time.Now().Add(retry).Unix()
	l.mu.Unlock()
}

func (l *peerRendezvousLifecycle) snapshot() PeerRendezvousStatus {
	if l == nil {
		return PeerRendezvousStatus{State: "waiting"}
	}
	l.mu.Lock()
	state := l.state
	active := l.active
	l.mu.Unlock()
	return PeerRendezvousStatus{
		Mode: string(l.resolved.Mode), State: state.State, URL: l.resolved.BaseURL,
		LastRegisteredAt: state.LastRegisteredAt, RegistrationMS: state.RegistrationMS,
		ReachablePeers: active.reachablePeers(), LastErrorCode: state.LastErrorCode,
		NextRetryAt: state.NextRetryAt,
	}
}

func classifyPeerRendezvousError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "registration_timeout"
	case errors.Is(err, rendezvousclient.ErrAuthentication):
		return "authentication_failed"
	case errors.Is(err, rendezvousclient.ErrServiceUnavailable):
		return "service_unavailable"
	default:
		return "registration_failed"
	}
}

func (l *peerRendezvousLifecycle) Stop() {
	if l == nil {
		return
	}
	l.cancel()
	l.mu.Lock()
	active := l.active
	l.mu.Unlock()
	if active != nil {
		active.Close()
	}
	<-l.done
}

func (l *peerRendezvousLifecycle) connectionRoute() (peerproto.ConnectionRoute, bool) {
	if l == nil {
		return peerproto.ConnectionRoute{}, false
	}
	l.mu.Lock()
	active := l.active
	l.mu.Unlock()
	if active == nil || active.serviceURL == "" || active.topic == "" {
		return peerproto.ConnectionRoute{}, false
	}
	return peerproto.ConnectionRoute{
		Kind: peerproto.RouteRendezvous, URL: active.serviceURL, Topic: active.topic,
	}, true
}

func (l *peerRendezvousLifecycle) syncConfigNow() (int, error) {
	if l == nil {
		return 0, nil
	}
	l.mu.Lock()
	active := l.active
	l.mu.Unlock()
	return active.syncConfigNow()
}

// GetPeerRendezvousStatus returns only operational metadata suitable for UI
// and diagnostics. Presence IDs and opaque routing topics remain internal.
func (a *App) GetPeerRendezvousStatus() PeerRendezvousStatus {
	if a == nil || a.cfgStore == nil {
		return PeerRendezvousStatus{Mode: string(rendezvousclient.ModeDisabled), State: "disabled"}
	}
	cfg := a.cfgStore.Get()
	resolved, err := rendezvousclient.ResolveConfig(rendezvousclient.Config{
		Mode: rendezvousclient.Mode(cfg.PeerRendezvousMode), URL: cfg.PeerRendezvousURL,
		STUNMode: rendezvousclient.STUNMode(cfg.PeerSTUNMode), STUNURLs: cfg.PeerSTUNURLs,
	}, false)
	if err != nil {
		return PeerRendezvousStatus{Mode: cfg.PeerRendezvousMode, State: "error", LastErrorCode: "invalid_config"}
	}
	if resolved.Mode == rendezvousclient.ModeDisabled || resolved.BaseURL == "" {
		return PeerRendezvousStatus{Mode: string(resolved.Mode), State: "disabled"}
	}
	a.peerRendezvousMu.Lock()
	lifecycle := a.peerRendezvous
	a.peerRendezvousMu.Unlock()
	if lifecycle == nil {
		return PeerRendezvousStatus{Mode: string(resolved.Mode), State: "waiting", URL: resolved.BaseURL}
	}
	return lifecycle.snapshot()
}

// ReconnectPeerRendezvous restarts only the local discovery/signaling route.
// Relay, Quick Tunnel, and local PTY lifecycles are untouched.
func (a *App) ReconnectPeerRendezvous() (PeerRendezvousStatus, error) {
	if a == nil || a.cfgStore == nil {
		return PeerRendezvousStatus{}, errors.New("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if _, err := resolvePeerRendezvousConfig(rendezvousclient.Config{
		Mode: rendezvousclient.Mode(cfg.PeerRendezvousMode), URL: cfg.PeerRendezvousURL,
		STUNMode: rendezvousclient.STUNMode(cfg.PeerSTUNMode), STUNURLs: cfg.PeerSTUNURLs,
	}); err != nil {
		return PeerRendezvousStatus{}, err
	}
	a.reconcilePeerRendezvous(cfg)
	return a.GetPeerRendezvousStatus(), nil
}

func waitPeerRendezvous(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func untilNextPeerPresenceRotation(now time.Time) time.Duration {
	seconds := int64(peerdiscovery.RotationInterval / time.Second)
	next := time.Unix((now.Unix()/seconds+1)*seconds, 0)
	delay := next.Sub(now)
	if delay <= 0 {
		return time.Second
	}
	return delay
}
