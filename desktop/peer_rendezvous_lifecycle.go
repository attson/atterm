package main

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/attson/atterm/internal/peerdiscovery"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/rendezvousclient"
	"github.com/pion/webrtc/v4"
)

const (
	peerRendezvousRetryMin = 500 * time.Millisecond
	peerRendezvousRetryMax = 8 * time.Second
)

type peerRendezvousLifecycle struct {
	app      *App
	host     *relayHost
	resolved rendezvousclient.ResolvedConfig
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}

	mu     sync.Mutex
	active *peerRendezvousHost
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
		webRTC := webrtc.Configuration{}
		if len(l.resolved.STUNURLs) != 0 {
			webRTC.ICEServers = []webrtc.ICEServer{{URLs: append([]string(nil), l.resolved.STUNURLs...)}}
		}
		host, err := newPeerRendezvousHost(l.ctx, l.app, l.host, rendezvousclient.PresenceConfig{
			ServiceURL: l.resolved.BaseURL,
		}, webRTC)
		if err != nil {
			logWarn("rendezvous", "registration failed: %v", err)
			if !waitPeerRendezvous(l.ctx, retry) {
				return
			}
			retry = time.Duration(math.Min(float64(peerRendezvousRetryMax), float64(retry*2)))
			continue
		}
		retry = peerRendezvousRetryMin
		l.mu.Lock()
		l.active = host
		l.mu.Unlock()
		rotation := time.NewTimer(untilNextPeerPresenceRotation(time.Now()))
		select {
		case <-l.ctx.Done():
		case <-host.route.Done():
		case <-rotation.C:
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
		if l.ctx.Err() == nil && !waitPeerRendezvous(l.ctx, retry) {
			return
		}
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
