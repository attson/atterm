package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertraffic"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/quicktunnel"
	"github.com/attson/atterm/internal/rendezvousclient"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

const (
	peerLANCatalogVersion     = 1
	peerLANCatalogMaxSessions = 512
	peerLANCatalogTimeout     = 5 * time.Second
)

type peerLANCatalogRequest struct {
	V int `json:"v"`
}

type peerLANCatalogResponse struct {
	V        int                            `json:"v"`
	Sessions []rendezvousclient.PeerSession `json:"sessions"`
}

// peerLANControlHostAttempt is authenticated against the reserved control
// transcript. It may exchange config and a filtered catalog, but it never
// creates a PTY subscriber, driver, filesystem worker, or service preview.
type peerLANControlHostAttempt struct {
	host   *peerQuickTunnelHost
	signal *quicktunnel.SignalChannel
	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	transport *peertransport.PionHostAttempt
	channel   peerQuickTunnelChannel
	config    *peerConfigChannel
	sessions  *peerSessionControlHost
	closed    bool
	closeOnce sync.Once
}

func (h *peerQuickTunnelHost) onLANControlAuthenticated(signal *quicktunnel.SignalChannel) {
	parent := h.app.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	attempt := &peerLANControlHostAttempt{host: h, signal: signal, ctx: ctx, cancel: cancel}
	h.mu.Lock()
	if _, exists := h.controls[signal]; exists {
		h.mu.Unlock()
		cancel()
		_ = signal.Close()
		return
	}
	h.controls[signal] = attempt
	h.mu.Unlock()

	transport, err := signal.BridgePionHost(parent, quicktunnel.PionHostBridgeConfig{
		WebRTC: webrtc.Configuration{},
		WSSFallback: &quicktunnel.WSSFallbackConfig{
			OnAuthenticated: func(channel *quicktunnel.WSSChannel) {
				if err := attempt.start(channel); err != nil {
					go h.removeControl(signal, true)
				}
			},
			OnRecord: func(peertransport.RecordKind, []byte) {
				go h.removeControl(signal, true)
			},
			OnConfigMessage: attempt.handleControlMessage,
			OnTraffic:       h.app.recordPeerTraffic(peertraffic.RouteGateway),
		},
		OnAuthenticated: func(channel *peertransport.PionHostChannel) {
			if err := attempt.start(channel); err != nil {
				go h.removeControl(signal, true)
			}
		},
		OnRecord: func(peertransport.RecordKind, []byte) {
			go h.removeControl(signal, true)
		},
		OnConfigMessage: attempt.handleControlMessage,
		OnTraffic:       h.app.recordPeerTraffic(peertraffic.RouteDirect),
	})
	if err != nil {
		h.removeControl(signal, true)
		return
	}
	attempt.mu.Lock()
	if attempt.closed {
		attempt.mu.Unlock()
		_ = transport.Close()
		return
	}
	attempt.transport = transport
	attempt.mu.Unlock()
}

func (a *peerLANControlHostAttempt) start(channel peerQuickTunnelChannel) error {
	remoteMembership, ok := channel.RemoteMembershipToken()
	if !ok || remoteMembership == "" {
		return errors.New("Peer LAN control membership is unavailable")
	}
	config, err := newPeerConfigChannel(a.host.app, channel)
	if err != nil {
		return err
	}
	a.mu.Lock()
	if a.closed || a.channel != nil {
		a.mu.Unlock()
		return errors.New("Peer LAN control route is unavailable")
	}
	a.channel = channel
	a.config = config
	sessions, sessionErr := newPeerSessionControlHost(a.ctx, a.host.runtime, channel, remoteMembership)
	if sessionErr == nil {
		a.sessions = sessions
	}
	a.mu.Unlock()
	return config.Start(a.ctx)
}

func (a *peerLANControlHostAttempt) handleControlMessage(kind peertransport.RecordKind, payload []byte) error {
	a.mu.Lock()
	channel := a.channel
	config := a.config
	sessionControl := a.sessions
	closed := a.closed
	a.mu.Unlock()
	if closed || channel == nil || config == nil {
		return errors.New("Peer LAN control route is unavailable")
	}
	if sessionControl != nil {
		handled, err := sessionControl.Handle(kind, payload)
		if handled {
			return err
		}
	}
	if kind != peertransport.RecordCatalogRequest {
		return config.Handle(a.ctx, kind, payload)
	}
	var request peerLANCatalogRequest
	if decodePeerConfigMessage(payload, &request) != nil || request.V != peerLANCatalogVersion {
		return errors.New("invalid Peer LAN catalog request")
	}
	sessions, err := a.host.runtime.catalog(config.remotePeerID)
	if err != nil {
		return err
	}
	if len(sessions) > peerLANCatalogMaxSessions {
		sessions = sessions[:peerLANCatalogMaxSessions]
	}
	response, err := json.Marshal(peerLANCatalogResponse{V: peerLANCatalogVersion, Sessions: sessions})
	if err != nil || len(response) > peertransport.MaxConfigMessageSize {
		return errors.New("Peer LAN catalog response exceeds limit")
	}
	return channel.SendConfigMessage(a.ctx, peertransport.RecordCatalogResponse, response)
}

func (h *peerQuickTunnelHost) takeControl(signal *quicktunnel.SignalChannel) *peerLANControlHostAttempt {
	h.mu.Lock()
	attempt := h.controls[signal]
	delete(h.controls, signal)
	h.mu.Unlock()
	return attempt
}

func (h *peerQuickTunnelHost) removeControl(signal *quicktunnel.SignalChannel, closeTransport bool) {
	if attempt := h.takeControl(signal); attempt != nil {
		attempt.close(closeTransport)
	}
}

func (a *peerLANControlHostAttempt) close(closeTransport bool) {
	var transport *peertransport.PionHostAttempt
	var channel peerQuickTunnelChannel
	a.closeOnce.Do(func() {
		a.mu.Lock()
		a.closed = true
		transport = a.transport
		channel = a.channel
		a.transport = nil
		a.channel = nil
		a.config = nil
		a.sessions = nil
		a.mu.Unlock()
		a.cancel()
	})
	if !closeTransport {
		return
	}
	if transport != nil {
		_ = transport.Close()
	} else if channel != nil {
		_ = channel.Close()
	}
}

func (a *App) discoverPeerLANSessions(ctx context.Context) ([]proto.SessionInfo, error) {
	routes, err := a.resolvedPeerLANRoutes(ctx)
	if err != nil {
		return nil, err
	}
	if len(routes) == 0 {
		a.replacePeerManualCatalog(nil)
		return nil, nil
	}
	type result struct {
		route    resolvedPeerLANRoute
		sessions []rendezvousclient.PeerSession
		err      error
	}
	results := make(chan result, len(routes))
	for _, route := range routes {
		route := route
		go func() {
			sessions, err := a.fetchPeerLANCatalog(ctx, route)
			results <- result{route: route, sessions: sessions, err: err}
		}()
	}
	next := make(map[uuid.UUID]peerDiscoveredSession)
	var joined error
	for range routes {
		select {
		case item := <-results:
			if item.err != nil {
				joined = errors.Join(joined, item.err)
				continue
			}
			remote := rendezvousclient.PeerRoute{
				PeerID:            item.route.PeerID,
				WrappingPublicKey: append([]byte(nil), item.route.Membership.WrappingPublicKey...),
			}
			validator := &peerRendezvousHost{app: a}
			accepted, err := validator.validateCatalog(remote, item.sessions)
			if err != nil {
				joined = errors.Join(joined, err)
				continue
			}
			for id, session := range accepted {
				session.manualURL = item.route.URL
				next[id] = session
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	a.replacePeerManualCatalog(next)
	if len(next) == 0 && joined != nil {
		return nil, joined
	}
	infos := make([]proto.SessionInfo, 0, len(next))
	for _, session := range next {
		infos = append(infos, session.info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	return infos, nil
}

func (a *App) fetchPeerLANCatalog(parent context.Context, route resolvedPeerLANRoute) ([]rendezvousclient.PeerSession, error) {
	ctx, cancel := context.WithTimeout(parent, peerLANCatalogTimeout)
	defer cancel()
	manager, err := a.peerManager()
	if err != nil {
		return nil, err
	}
	state, err := manager.store.Load()
	if err != nil {
		return nil, err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return nil, err
	}
	identity, err := manager.loadIdentity()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if manager.now != nil {
		now = manager.now()
	}
	active, err := activePeerMemberships(state, genesis, now)
	if err != nil {
		return nil, err
	}
	local := membershipForPeerID(active, identity.PeerID())
	remote := membershipForPeerID(active, route.PeerID)
	if local == nil || remote == nil || remote.Token != route.Membership.Token {
		return nil, quicktunnel.ErrUnauthorized
	}
	response := make(chan []rendezvousclient.PeerSession, 1)
	errCh := make(chan error, 1)
	var configMu sync.Mutex
	var config *peerConfigChannel
	signal, err := quicktunnel.Dial(ctx, quicktunnel.ClientConfig{
		URL: route.URL, AllowInsecure: true, Identity: identity, GenesisToken: state.GenesisToken,
		ClientMembershipToken: local.Token, HostMembershipToken: remote.Token,
		SessionID: peertransport.ConfigSyncSessionID(), ClientInstanceID: "peer-lan-catalog-" + uuid.NewString(),
		OnClosed: func(_ *quicktunnel.SignalChannel, closeErr error) {
			if closeErr != nil {
				select {
				case errCh <- closeErr:
				default:
				}
			}
		},
	})
	if err != nil {
		return nil, err
	}
	defer signal.Close()
	if err := signal.BindWSSFallback(quicktunnel.WSSFallbackConfig{
		OnAuthenticated: func(channel *quicktunnel.WSSChannel) {
			configChannel, configErr := newPeerConfigChannel(a, channel)
			if configErr != nil {
				select {
				case errCh <- configErr:
				default:
				}
				return
			}
			configMu.Lock()
			config = configChannel
			configMu.Unlock()
			request, _ := json.Marshal(peerLANCatalogRequest{V: peerLANCatalogVersion})
			if sendErr := channel.SendConfigMessage(ctx, peertransport.RecordCatalogRequest, request); sendErr != nil {
				select {
				case errCh <- sendErr:
				default:
				}
				return
			}
			if startErr := configChannel.Start(ctx); startErr != nil {
				select {
				case errCh <- startErr:
				default:
				}
			}
		},
		OnRecord: func(peertransport.RecordKind, []byte) {
			select {
			case errCh <- errors.New("terminal record on Peer LAN control route"):
			default:
			}
		},
		OnConfigMessage: func(kind peertransport.RecordKind, payload []byte) error {
			if kind == peertransport.RecordCatalogResponse {
				var catalog peerLANCatalogResponse
				if decodePeerConfigMessage(payload, &catalog) != nil || catalog.V != peerLANCatalogVersion || len(catalog.Sessions) > peerLANCatalogMaxSessions {
					return errors.New("invalid Peer LAN catalog response")
				}
				select {
				case response <- catalog.Sessions:
				default:
				}
				return nil
			}
			configMu.Lock()
			configChannel := config
			configMu.Unlock()
			if configChannel == nil {
				return errors.New("Peer LAN config channel unavailable")
			}
			return configChannel.Handle(ctx, kind, payload)
		},
		OnTraffic: a.recordPeerTraffic(peertraffic.RouteLAN),
	}); err != nil {
		return nil, err
	}
	if _, err := signal.StartWSSFallback(ctx); err != nil {
		return nil, err
	}
	select {
	case sessions := <-response:
		return sessions, nil
	case err := <-errCh:
		return nil, fmt.Errorf("Peer LAN catalog: %w", err)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (a *App) replacePeerManualCatalog(next map[uuid.UUID]peerDiscoveredSession) {
	if next == nil {
		next = make(map[uuid.UUID]peerDiscoveredSession)
	}
	a.peerManualCatalogMu.Lock()
	a.peerManualCatalog = next
	a.peerManualCatalogMu.Unlock()
}

func (a *App) peerManualDiscoveredSession(id uuid.UUID) (peerDiscoveredSession, bool) {
	a.peerManualCatalogMu.Lock()
	session, ok := a.peerManualCatalog[id]
	a.peerManualCatalogMu.Unlock()
	return session, ok
}
