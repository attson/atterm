package rendezvousclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/rendezvous"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

const (
	routeWireVersion       = 1
	routeAuthorizationTTL  = 30 * time.Second
	routeAckTimeout        = 5 * time.Second
	routeResponseTimeout   = 10 * time.Second
	routeCatalogProbeWait  = 750 * time.Millisecond
	routeReplayWindowSize  = 2048
	routeMaxAttempts       = 32
	routeMaxCatalogs       = 32
	routeCatalogPageSize   = 16
	routeMaxCatalogEntries = 512
	routeMaxIdentifierSize = 128
	routeMaxMetadataSize   = 1024
)

// ConfigSyncSessionID is a reserved transcript namespace for authenticated
// config-only attempts. It never names a terminal session or subscriber.
func ConfigSyncSessionID() uuid.UUID { return peertransport.ConfigSyncSessionID() }

const (
	routeKindOpen            = "open"
	routeKindAuthorized      = "authorized"
	routeKindSignal          = "signal"
	routeKindError           = "error"
	routeKindCatalog         = "catalog_request"
	routeKindCatalogOK       = "catalog_response"
	routeKindCatalogRoutes   = "catalog_request_routes"
	routeKindCatalogRoutesOK = "catalog_response_routes"
)

const (
	routeErrorAuthentication     = "authentication_failed"
	routeErrorServiceUnavailable = "service_unavailable"
)

// PeerRoute is the locally resolved, active membership behind one rotating
// presence identifier. It is never accepted from Rendezvous itself.
type PeerRoute struct {
	PeerID            string
	PresenceID        string
	WrappingPublicKey []byte
}

// PeerOpenRequest is the bounded request a host authorizes against its live
// session registry and current deny-wins membership view.
type PeerOpenRequest struct {
	AttemptID        uuid.UUID
	Ticket           []byte
	SessionID        uuid.UUID
	ClientPeerID     string
	ClientInstanceID string
}

// HostAuthorization supplies the exact current Peer documents used by the
// existing DataChannel membership authenticator.
type HostAuthorization struct {
	Identity              *peercrypto.Identity
	GenesisToken          string
	ClientMembershipToken string
	HostMembershipToken   string
	Permission            peertransport.Permission
}

// HostCallbacks bind one authorized attempt to its application owner.
type HostCallbacks struct {
	OnAuthenticated func(*peertransport.PionHostChannel)
	OnRecord        func(peertransport.RecordKind, []byte)
	OnConfigMessage func(peertransport.RecordKind, []byte) error
	OnTraffic       peertransport.TrafficObserver
	OnClosed        func(error)
}

type HostAuthorizeFunc func(context.Context, PeerOpenRequest) (HostAuthorization, HostCallbacks, error)

// PeerSession is the bounded, pairwise-encrypted session metadata exchanged
// before a terminal attempt exists. Content and terminal frames never leave
// the encrypted route envelope.
type PeerSession struct {
	ID                string                   `json:"id"`
	Command           string                   `json:"command,omitempty"`
	Cwd               string                   `json:"cwd,omitempty"`
	Title             string                   `json:"title,omitempty"`
	Cols              uint16                   `json:"cols"`
	Rows              uint16                   `json:"rows"`
	StartedAt         int64                    `json:"started_at"`
	HostID            string                   `json:"host_id"`
	Host              string                   `json:"host,omitempty"`
	User              string                   `json:"user,omitempty"`
	SSHHostID         string                   `json:"ssh_host_id,omitempty"`
	Permission        peertransport.Permission `json:"permission"`
	TaskState         string                   `json:"task_state,omitempty"`
	CurrentCommand    string                   `json:"current_command,omitempty"`
	CommandStartedAt  int64                    `json:"command_started_at,omitempty"`
	CommandEndedAt    int64                    `json:"command_ended_at,omitempty"`
	CommandDurationMS int                      `json:"command_duration_ms,omitempty"`
	CommandExitCode   *int                     `json:"command_exit_code,omitempty"`
	LastOutputAt      int64                    `json:"last_output_at,omitempty"`
	Type              string                   `json:"type,omitempty"`
	AttentionAt       int64                    `json:"attention_at,omitempty"`
}

type PeerCatalogRequest struct {
	ClientPeerID string
}

type HostCatalogFunc func(context.Context, PeerCatalogRequest) ([]PeerSession, error)

// PeerHost identifies one reachable desktop independently of its session
// count. Permission is the current effective ceiling for host-level actions.
type PeerHost struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name,omitempty"`
	Permission peertransport.Permission `json:"permission"`
}

type HostInfoFunc func(context.Context, PeerCatalogRequest) (PeerHost, error)

// PeerCatalog is a session snapshot plus an optional signed, ticketless
// reconnect bundle. The bundle is transported only inside the pairwise
// encrypted signaling envelope and remains subject to caller-side validation.
type PeerCatalog struct {
	Sessions         []PeerSession
	ConnectionBundle string
	Host             *PeerHost
}

// HostConnectionBundleFunc creates a fresh member reconnect bundle for the
// authenticated catalog requester without starting a transport.
type HostConnectionBundleFunc func(context.Context, PeerCatalogRequest) (string, error)

type RouteConfig struct {
	Connection            *PresenceConnection
	InitialPresence       []rendezvous.Presence
	SpaceID               string
	EpochKey              configsync.EpochKey
	LocalPeerID           string
	LocalWrappingIdentity *peercrypto.WrappingIdentity
	ResolvePeer           func(presenceID string) (PeerRoute, bool)
	AuthorizeHost         HostAuthorizeFunc
	CatalogHost           HostCatalogFunc
	HostInfo              HostInfoFunc
	ConnectionBundleHost  HostConnectionBundleFunc
	WebRTC                webrtc.Configuration
}

type ClientAttemptConfig struct {
	Remote                PeerRoute
	Identity              *peercrypto.Identity
	GenesisToken          string
	ClientMembershipToken string
	HostMembershipToken   string
	SessionID             uuid.UUID
	ClientInstanceID      string
	WebRTC                webrtc.Configuration
	OnAuthenticated       func(*peertransport.PionClientChannel)
	OnRecord              func(peertransport.RecordKind, []byte)
	OnConfigMessage       func(peertransport.RecordKind, []byte) error
	OnTraffic             peertransport.TrafficObserver
	OnDiagnostics         func(iceState, candidateType string)
	OnClosed              func(error)
}

type routeWireMessage struct {
	Version             int                      `json:"v"`
	Kind                string                   `json:"kind"`
	AttemptID           uuid.UUID                `json:"attempt_id"`
	Ticket              []byte                   `json:"ticket,omitempty"`
	SessionID           uuid.UUID                `json:"session_id,omitempty"`
	ClientPeerID        string                   `json:"client_peer_id,omitempty"`
	ClientInstanceID    string                   `json:"client_instance_id,omitempty"`
	UserID              string                   `json:"user_id,omitempty"`
	HostID              string                   `json:"host_id,omitempty"`
	Permission          peertransport.Permission `json:"permission,omitempty"`
	ExpiresAtUnixMillis uint64                   `json:"expires_at_unix_millis,omitempty"`
	SignalType          string                   `json:"signal_type,omitempty"`
	Payload             string                   `json:"payload,omitempty"`
	Code                string                   `json:"code,omitempty"`
	CatalogOffset       int                      `json:"catalog_offset,omitempty"`
	CatalogNextOffset   int                      `json:"catalog_next_offset,omitempty"`
	Sessions            []PeerSession            `json:"sessions,omitempty"`
	ConnectionBundle    string                   `json:"connection_bundle,omitempty"`
	HostInfo            *PeerHost                `json:"host_info,omitempty"`
}

type routeAck struct {
	state string
	err   error
}

type clientRouteAttempt struct {
	remote    PeerRoute
	response  chan routeWireMessage
	attempt   *peertransport.PionClientAttempt
	iceFailed bool
}

type hostRouteAttempt struct {
	remote  PeerRoute
	attempt *peertransport.PionHostAttempt
}

type catalogRequest struct {
	remote   PeerRoute
	response chan routeWireMessage
}

type catalogRoutesCapability uint8

const (
	catalogRoutesUnknown catalogRoutesCapability = iota
	catalogRoutesSupported
	catalogRoutesUnsupported
)

var errCatalogRoutesUnsupported = errors.New("Rendezvous route catalog unsupported")

// Route multiplexes encrypted signaling attempts over one foreground
// PresenceConnection. Terminal and config records never traverse this type.
type Route struct {
	cfg    RouteConfig
	ctx    context.Context
	cancel context.CancelFunc
	replay *SignalReplayWindow

	mu            sync.Mutex
	online        map[string]rendezvous.Role
	acks          map[string]chan routeAck
	clients       map[uuid.UUID]*clientRouteAttempt
	hosts         map[uuid.UUID]*hostRouteAttempt
	catalogs      map[uuid.UUID]catalogRequest
	catalogRoutes map[string]catalogRoutesCapability
	closed        bool
	closeOnce     sync.Once
}

func NewRoute(parent context.Context, cfg RouteConfig) (*Route, error) {
	if parent == nil || cfg.Connection == nil || cfg.Connection.Topic() == "" ||
		cfg.Connection.PresenceID() == "" || cfg.SpaceID == "" || cfg.LocalPeerID == "" ||
		cfg.LocalWrappingIdentity == nil || cfg.ResolvePeer == nil ||
		cfg.EpochKey.Class != configsync.KeyClassSync || cfg.EpochKey.Epoch == 0 {
		return nil, ErrInvalidSignal
	}
	ctx, cancel := context.WithCancel(parent)
	route := &Route{
		cfg: cfg, ctx: ctx, cancel: cancel, replay: NewSignalReplayWindow(routeReplayWindowSize),
		online: make(map[string]rendezvous.Role), acks: make(map[string]chan routeAck),
		clients: make(map[uuid.UUID]*clientRouteAttempt), hosts: make(map[uuid.UUID]*hostRouteAttempt),
		catalogs: make(map[uuid.UUID]catalogRequest), catalogRoutes: make(map[string]catalogRoutesCapability),
	}
	for _, presence := range cfg.InitialPresence {
		if validateOpaqueID(presence.PresenceID, 32) != nil || presence.PresenceID == cfg.Connection.PresenceID() ||
			(presence.Role != rendezvous.RoleHost && presence.Role != rendezvous.RoleMember) {
			cancel()
			return nil, ErrInvalidSignal
		}
		route.online[presence.PresenceID] = presence.Role
	}
	go route.readLoop()
	return route, nil
}

func (r *Route) Dial(ctx context.Context, cfg ClientAttemptConfig) (*peertransport.PionClientAttempt, error) {
	if r == nil || ctx == nil {
		return nil, ErrServiceUnavailable
	}
	clientMembership, hostMembership, genesis, err := validateClientAttempt(cfg)
	if err != nil || genesis.Document.SpaceID != r.cfg.SpaceID || cfg.Remote.PeerID != hostMembership.Document.SubjectPeerID ||
		!bytes.Equal(cfg.Remote.WrappingPublicKey, hostMembership.WrappingPublicKey) {
		return nil, ErrAuthentication
	}
	r.mu.Lock()
	_, online := r.online[cfg.Remote.PresenceID]
	if r.closed {
		r.mu.Unlock()
		return nil, ErrServiceUnavailable
	}
	if !online {
		r.mu.Unlock()
		return nil, ErrPeerOffline
	}
	if len(r.clients)+len(r.hosts) >= routeMaxAttempts {
		r.mu.Unlock()
		return nil, ErrServiceUnavailable
	}
	attemptID := uuid.New()
	pending := &clientRouteAttempt{remote: clonePeerRoute(cfg.Remote), response: make(chan routeWireMessage, 1)}
	r.clients[attemptID] = pending
	r.mu.Unlock()
	cleanup := true
	defer func() {
		if cleanup {
			r.removeClient(attemptID, false)
		}
	}()

	ticket := make([]byte, 32)
	if _, err := rand.Read(ticket); err != nil {
		return nil, fmt.Errorf("create Rendezvous attempt ticket: %w", err)
	}
	open := routeWireMessage{
		Version: routeWireVersion, Kind: routeKindOpen, AttemptID: attemptID, Ticket: ticket,
		SessionID: cfg.SessionID, ClientPeerID: cfg.Identity.PeerID(), ClientInstanceID: cfg.ClientInstanceID,
	}
	if err := r.sendConfirmed(ctx, cfg.Remote, open); err != nil {
		return nil, err
	}

	responseTimer := time.NewTimer(routeResponseTimeout)
	defer responseTimer.Stop()
	var response routeWireMessage
	select {
	case response = <-pending.response:
	case <-ctx.Done():
		return nil, fmt.Errorf("%w: %v", ErrPeerOffline, ctx.Err())
	case <-r.ctx.Done():
		return nil, ErrServiceUnavailable
	case <-responseTimer.C:
		return nil, ErrPeerOffline
	}
	if response.Kind == routeKindError {
		return nil, classifyWireRouteError(response.Code)
	}
	authorization, err := validateAuthorizedRoute(response, open, genesis, clientMembership, hostMembership)
	if err != nil {
		return nil, ErrAuthentication
	}
	authenticator, err := peertransport.NewPeerMembershipAuthenticator(
		cfg.Identity, peertransport.RoleClient, cfg.GenesisToken,
		cfg.ClientMembershipToken, cfg.HostMembershipToken, time.Now(),
	)
	if err != nil {
		return nil, ErrAuthentication
	}
	webRTC := cfg.WebRTC
	if len(webRTC.ICEServers) == 0 {
		webRTC = r.cfg.WebRTC
	}
	attempt, err := peertransport.NewPionClientAttempt(r.ctx, peertransport.PionClientConfig{
		Authorization: authorization, Authenticator: authenticator, WebRTC: webRTC,
		SendSignal: func(signalType, payload string) error {
			return r.sendSignal(cfg.Remote, attemptID, signalType, payload)
		},
		OnAuthenticated: cfg.OnAuthenticated, OnRecord: cfg.OnRecord,
		OnConfigMessage: cfg.OnConfigMessage, OnTraffic: cfg.OnTraffic,
		OnDiagnostics: func(iceState, candidateType string) {
			if iceState == webrtc.ICEConnectionStateFailed.String() {
				r.mu.Lock()
				if current := r.clients[attemptID]; current != nil {
					current.iceFailed = true
				}
				r.mu.Unlock()
			}
			if cfg.OnDiagnostics != nil {
				cfg.OnDiagnostics(iceState, candidateType)
			}
		},
		OnClosed: func(closeErr error) {
			classified := r.classifyClientClose(attemptID, closeErr)
			r.removeClient(attemptID, false)
			if cfg.OnClosed != nil {
				cfg.OnClosed(classified)
			}
		},
	})
	if err != nil {
		return nil, classifyRouteError(err, false)
	}
	r.mu.Lock()
	if current := r.clients[attemptID]; current == pending && !r.closed {
		pending.attempt = attempt
	} else {
		r.mu.Unlock()
		_ = attempt.Close()
		return nil, ErrServiceUnavailable
	}
	r.mu.Unlock()
	if err := attempt.Start(); err != nil {
		_ = attempt.Close()
		return nil, classifyRouteError(err, false)
	}
	cleanup = false
	return attempt, nil
}

// Catalog fetches the remote host's currently authorized session metadata.
// Every page is independently pairwise encrypted and correlated; no terminal
// subscriber or Pion attempt is created by this operation.
func (r *Route) Catalog(ctx context.Context, remote PeerRoute) ([]PeerSession, error) {
	catalog, err := r.fetchCatalog(ctx, remote, routeKindCatalog, routeKindCatalogOK, 0)
	return catalog.Sessions, err
}

// CatalogWithRoutes probes for the optional route-bearing catalog extension.
// A peer that does not answer the short probe is cached as v1-only for this
// Route lifetime. Once support is observed, transient v2 failures fall back to
// v1 sessions without downgrading the cached capability.
func (r *Route) CatalogWithRoutes(ctx context.Context, remote PeerRoute) (PeerCatalog, error) {
	if r == nil || ctx == nil {
		return PeerCatalog{}, ErrServiceUnavailable
	}
	r.mu.Lock()
	capability := r.catalogRoutes[remote.PeerID]
	r.mu.Unlock()
	if capability == catalogRoutesUnsupported {
		return r.fetchCatalog(ctx, remote, routeKindCatalog, routeKindCatalogOK, 0)
	}
	probeWait := time.Duration(0)
	if capability == catalogRoutesUnknown {
		probeWait = routeCatalogProbeWait
	}
	catalog, err := r.fetchCatalog(ctx, remote, routeKindCatalogRoutes, routeKindCatalogRoutesOK, probeWait)
	if err == nil {
		r.mu.Lock()
		r.catalogRoutes[remote.PeerID] = catalogRoutesSupported
		r.mu.Unlock()
		return catalog, nil
	}
	if ctx.Err() != nil {
		return PeerCatalog{}, err
	}
	if errors.Is(err, ErrAuthentication) || errors.Is(err, ErrInvalidSignal) {
		return PeerCatalog{}, err
	}
	if errors.Is(err, errCatalogRoutesUnsupported) {
		r.mu.Lock()
		r.catalogRoutes[remote.PeerID] = catalogRoutesUnsupported
		r.mu.Unlock()
	}
	fallback, fallbackErr := r.fetchCatalog(ctx, remote, routeKindCatalog, routeKindCatalogOK, 0)
	if fallbackErr != nil {
		return PeerCatalog{}, errors.Join(err, fallbackErr)
	}
	return fallback, nil
}

func (r *Route) fetchCatalog(ctx context.Context, remote PeerRoute, requestKind, responseKind string, firstResponseWait time.Duration) (PeerCatalog, error) {
	if r == nil || ctx == nil || remote.PeerID == "" || validateOpaqueID(remote.PresenceID, 32) != nil {
		return PeerCatalog{}, ErrServiceUnavailable
	}
	var catalog PeerCatalog
	for offset := 0; ; {
		requestID := uuid.New()
		response := make(chan routeWireMessage, 1)
		r.mu.Lock()
		_, online := r.online[remote.PresenceID]
		if r.closed {
			r.mu.Unlock()
			return PeerCatalog{}, ErrServiceUnavailable
		}
		if !online {
			r.mu.Unlock()
			return PeerCatalog{}, ErrPeerOffline
		}
		if len(r.catalogs) >= routeMaxCatalogs {
			r.mu.Unlock()
			return PeerCatalog{}, ErrServiceUnavailable
		}
		r.catalogs[requestID] = catalogRequest{remote: clonePeerRoute(remote), response: response}
		r.mu.Unlock()

		request := routeWireMessage{
			Version: routeWireVersion, Kind: requestKind, AttemptID: requestID,
			ClientPeerID: r.cfg.LocalPeerID, CatalogOffset: offset,
		}
		err := r.sendConfirmed(ctx, remote, request)
		if err == nil {
			responseWait := routeResponseTimeout
			if offset == 0 && firstResponseWait > 0 {
				responseWait = firstResponseWait
			}
			timer := time.NewTimer(responseWait)
			select {
			case message := <-response:
				if message.Kind == routeKindError {
					err = classifyWireRouteError(message.Code)
				} else if validateCatalogResponse(message, request, remote, responseKind) != nil {
					err = ErrAuthentication
				} else {
					catalog.Sessions = append(catalog.Sessions, clonePeerSessions(message.Sessions)...)
					if offset == 0 {
						catalog.ConnectionBundle = message.ConnectionBundle
						catalog.Host = clonePeerHost(message.HostInfo)
					}
					if len(catalog.Sessions) > routeMaxCatalogEntries {
						err = ErrInvalidSignal
					} else if message.CatalogNextOffset == 0 {
						timer.Stop()
						r.removeCatalog(requestID)
						return catalog, nil
					} else {
						offset = message.CatalogNextOffset
					}
				}
			case <-ctx.Done():
				err = fmt.Errorf("%w: %v", ErrPeerOffline, ctx.Err())
			case <-r.ctx.Done():
				err = ErrServiceUnavailable
			case <-timer.C:
				if offset == 0 && firstResponseWait > 0 {
					err = errCatalogRoutesUnsupported
				} else {
					err = ErrPeerOffline
				}
			}
			timer.Stop()
		}
		r.removeCatalog(requestID)
		if err != nil {
			return PeerCatalog{}, err
		}
	}
}

func (r *Route) removeCatalog(id uuid.UUID) {
	r.mu.Lock()
	delete(r.catalogs, id)
	r.mu.Unlock()
}

func (r *Route) sendSignal(remote PeerRoute, attemptID uuid.UUID, signalType, payload string) error {
	if !validRouteSignal(signalType, payload) {
		return ErrInvalidSignal
	}
	return r.send(r.ctx, remote, routeWireMessage{
		Version: routeWireVersion, Kind: routeKindSignal, AttemptID: attemptID,
		SignalType: signalType, Payload: payload,
	})
}

func (r *Route) sendConfirmed(ctx context.Context, remote PeerRoute, message routeWireMessage) error {
	messageID, err := newSignalMessageID()
	if err != nil {
		return fmt.Errorf("create Rendezvous message id: %w", err)
	}
	waiter := make(chan routeAck, 1)
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrServiceUnavailable
	}
	r.acks[messageID] = waiter
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.acks, messageID)
		r.mu.Unlock()
	}()
	if err := r.sendWithID(ctx, remote, messageID, message); err != nil {
		return err
	}
	timer := time.NewTimer(routeAckTimeout)
	defer timer.Stop()
	select {
	case result := <-waiter:
		if result.err != nil {
			return result.err
		}
		if result.state == rendezvous.DeliveryQueued {
			return ErrPeerOffline
		}
		if result.state != rendezvous.DeliveryDelivered {
			return ErrServiceUnavailable
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%w: %v", ErrServiceUnavailable, ctx.Err())
	case <-r.ctx.Done():
		return ErrServiceUnavailable
	case <-timer.C:
		return ErrServiceUnavailable
	}
}

func (r *Route) send(ctx context.Context, remote PeerRoute, message routeWireMessage) error {
	messageID, err := newSignalMessageID()
	if err != nil {
		return err
	}
	return r.sendWithID(ctx, remote, messageID, message)
}

func (r *Route) sendWithID(ctx context.Context, remote PeerRoute, messageID string, message routeWireMessage) error {
	if remote.PeerID == "" || validateOpaqueID(remote.PresenceID, 32) != nil {
		return ErrInvalidSignal
	}
	encoded, err := json.Marshal(message)
	if err != nil || len(encoded) > maxSignalPlaintext {
		return ErrInvalidSignal
	}
	cipher, err := NewSignalCipher(SignalCipherConfig{
		EpochKey: r.cfg.EpochKey, SpaceID: r.cfg.SpaceID,
		LocalPeerID: r.cfg.LocalPeerID, RemotePeerID: remote.PeerID,
		LocalWrappingIdentity: r.cfg.LocalWrappingIdentity, RemoteWrappingPublicKey: remote.WrappingPublicKey,
	})
	if err != nil {
		return err
	}
	sealed, err := cipher.Seal(SignalRouteBinding{
		Topic: r.cfg.Connection.Topic(), FromPresenceID: r.cfg.Connection.PresenceID(),
		ToPresenceID: remote.PresenceID, MessageID: messageID,
	}, encoded)
	if err != nil {
		return err
	}
	return r.cfg.Connection.publishWithID(ctx, remote.PresenceID, messageID, sealed)
}

func (r *Route) readLoop() {
	for {
		event, err := r.cfg.Connection.ReadEvent(r.ctx)
		if err != nil {
			r.finish(fmt.Errorf("%w: read: %v", ErrServiceUnavailable, err))
			return
		}
		switch event.Kind {
		case rendezvous.KindPresence:
			r.handlePresence(event)
		case rendezvous.KindAck:
			r.deliverAck(event.MessageID, routeAck{state: event.State})
		case rendezvous.KindError:
			if event.MessageID != "" {
				r.deliverAck(event.MessageID, routeAck{err: ErrServiceUnavailable})
			}
		case rendezvous.KindSignal:
			r.handleEncryptedSignal(event)
		}
	}
}

func (r *Route) handlePresence(event rendezvous.EventMessage) {
	if validateOpaqueID(event.PresenceID, 32) != nil || event.PresenceID == r.cfg.Connection.PresenceID() ||
		(event.Role != rendezvous.RoleHost && event.Role != rendezvous.RoleMember) ||
		(event.Event != rendezvous.PresenceOnline && event.Event != rendezvous.PresenceOffline) {
		return
	}
	r.mu.Lock()
	if event.Event == rendezvous.PresenceOnline {
		r.online[event.PresenceID] = event.Role
	} else if event.Event == rendezvous.PresenceOffline {
		delete(r.online, event.PresenceID)
	}
	r.mu.Unlock()
}

func (r *Route) deliverAck(messageID string, result routeAck) {
	r.mu.Lock()
	waiter := r.acks[messageID]
	r.mu.Unlock()
	if waiter != nil {
		select {
		case waiter <- result:
		default:
		}
	}
}

func (r *Route) handleEncryptedSignal(event rendezvous.EventMessage) {
	remote, ok := r.cfg.ResolvePeer(event.From)
	if !ok || remote.PresenceID != event.From || remote.PeerID == "" {
		return
	}
	binding := SignalRouteBinding{
		Topic: r.cfg.Connection.Topic(), FromPresenceID: event.From,
		ToPresenceID: r.cfg.Connection.PresenceID(), MessageID: event.MessageID,
	}
	if err := r.replay.Accept(binding); err != nil {
		return
	}
	sealed, err := decodeSignalPayload(event.Payload)
	if err != nil {
		return
	}
	cipher, err := NewSignalCipher(SignalCipherConfig{
		EpochKey: r.cfg.EpochKey, SpaceID: r.cfg.SpaceID,
		LocalPeerID: r.cfg.LocalPeerID, RemotePeerID: remote.PeerID,
		LocalWrappingIdentity: r.cfg.LocalWrappingIdentity, RemoteWrappingPublicKey: remote.WrappingPublicKey,
	})
	if err != nil {
		return
	}
	plaintext, err := cipher.Open(binding, sealed)
	if err != nil {
		return
	}
	var message routeWireMessage
	if decodeRouteWire(plaintext, &message) != nil || message.Version != routeWireVersion || message.AttemptID == uuid.Nil {
		return
	}
	switch message.Kind {
	case routeKindOpen:
		r.handleOpen(remote, message)
	case routeKindAuthorized, routeKindError:
		r.handleClientResponse(remote, message)
	case routeKindSignal:
		r.handleAttemptSignal(remote, message)
	case routeKindCatalog, routeKindCatalogRoutes:
		r.handleCatalogRequest(remote, message)
	case routeKindCatalogOK, routeKindCatalogRoutesOK:
		r.handleCatalogResponse(remote, message)
	}
}

func (r *Route) handleCatalogRequest(remote PeerRoute, message routeWireMessage) {
	if message.Kind == routeKindCatalogRoutes && r.cfg.ConnectionBundleHost == nil {
		return
	}
	if r.cfg.CatalogHost == nil || validateCatalogRequest(message, message.Kind) != nil || message.ClientPeerID != remote.PeerID {
		r.sendRouteError(remote, message.AttemptID, routeErrorAuthentication)
		return
	}
	request := PeerCatalogRequest{ClientPeerID: message.ClientPeerID}
	sessions, err := r.cfg.CatalogHost(r.ctx, request)
	if err != nil {
		r.sendRouteError(remote, message.AttemptID, routeErrorAuthentication)
		return
	}
	if len(sessions) > routeMaxCatalogEntries {
		sessions = sessions[:routeMaxCatalogEntries]
	}
	for _, session := range sessions {
		if validatePeerSession(session) != nil {
			r.sendRouteError(remote, message.AttemptID, routeErrorAuthentication)
			return
		}
	}
	if message.CatalogOffset > len(sessions) {
		r.sendRouteError(remote, message.AttemptID, routeErrorAuthentication)
		return
	}
	connectionBundle := ""
	var hostInfo *PeerHost
	// host_info is part of the optional route-bearing extension. Keeping it
	// out of catalog_response preserves strict-decoder compatibility with v1.
	if message.Kind == routeKindCatalogRoutes && message.CatalogOffset == 0 && r.cfg.HostInfo != nil {
		host, hostErr := r.cfg.HostInfo(r.ctx, request)
		if hostErr != nil || validatePeerHost(host) != nil {
			r.sendRouteError(remote, message.AttemptID, routeErrorAuthentication)
			return
		}
		hostInfo = &host
	}
	responseKind := routeKindCatalogOK
	if message.Kind == routeKindCatalogRoutes {
		responseKind = routeKindCatalogRoutesOK
		if message.CatalogOffset == 0 {
			connectionBundle, err = r.cfg.ConnectionBundleHost(r.ctx, request)
			if err != nil || connectionBundle == "" || !utf8.ValidString(connectionBundle) {
				r.sendRouteError(remote, message.AttemptID, routeErrorServiceUnavailable)
				return
			}
		}
	}
	end := min(message.CatalogOffset+routeCatalogPageSize, len(sessions))
	response, err := catalogPageResponse(
		responseKind, r.cfg.LocalPeerID, message.AttemptID, message.CatalogOffset, end, sessions, connectionBundle, hostInfo,
	)
	if err != nil {
		r.sendRouteError(remote, message.AttemptID, routeErrorServiceUnavailable)
		return
	}
	if err := r.send(r.ctx, remote, response); err != nil {
		r.sendRouteError(remote, message.AttemptID, routeErrorServiceUnavailable)
	}
}

func catalogPageResponse(kind, hostID string, requestID uuid.UUID, offset, end int, sessions []PeerSession, connectionBundle string, hostInfo *PeerHost) (routeWireMessage, error) {
	for {
		next := 0
		if end < len(sessions) {
			next = end
		}
		response := routeWireMessage{
			Version: routeWireVersion, Kind: kind, AttemptID: requestID,
			HostID: hostID, CatalogOffset: offset, CatalogNextOffset: next,
			Sessions: clonePeerSessions(sessions[offset:end]), ConnectionBundle: connectionBundle,
			HostInfo: clonePeerHost(hostInfo),
		}
		encoded, err := json.Marshal(response)
		if err == nil && len(encoded) <= maxSignalPlaintext {
			return response, nil
		}
		if end <= offset+1 {
			return routeWireMessage{}, ErrInvalidSignal
		}
		end--
	}
}

func (r *Route) handleCatalogResponse(remote PeerRoute, message routeWireMessage) {
	r.mu.Lock()
	pending, ok := r.catalogs[message.AttemptID]
	r.mu.Unlock()
	if !ok || pending.remote.PeerID != remote.PeerID || pending.remote.PresenceID != remote.PresenceID ||
		message.HostID != remote.PeerID {
		return
	}
	select {
	case pending.response <- message:
	default:
	}
}

func (r *Route) handleOpen(remote PeerRoute, message routeWireMessage) {
	if r.cfg.AuthorizeHost == nil || validateOpenWire(message) != nil || message.ClientPeerID != remote.PeerID {
		r.sendRouteError(remote, message.AttemptID, routeErrorAuthentication)
		return
	}
	request := PeerOpenRequest{
		AttemptID: message.AttemptID, Ticket: append([]byte(nil), message.Ticket...), SessionID: message.SessionID,
		ClientPeerID: message.ClientPeerID, ClientInstanceID: message.ClientInstanceID,
	}
	authorization, callbacks, err := r.cfg.AuthorizeHost(r.ctx, request)
	if err != nil {
		r.sendRouteError(remote, message.AttemptID, routeErrorAuthentication)
		return
	}
	abort := func(code string, closeErr error) {
		if callbacks.OnClosed != nil {
			callbacks.OnClosed(closeErr)
		}
		r.sendRouteError(remote, message.AttemptID, code)
	}
	claims, authenticator, exact, err := validateHostRouteAuthorization(request, authorization, remote, r.cfg.SpaceID)
	if err != nil {
		abort(routeErrorAuthentication, ErrAuthentication)
		return
	}
	r.mu.Lock()
	closed := r.closed
	capacityReached := len(r.clients)+len(r.hosts) >= routeMaxAttempts
	idCollision := r.clients[message.AttemptID] != nil || r.hosts[message.AttemptID] != nil
	if closed || capacityReached || idCollision {
		r.mu.Unlock()
		if idCollision {
			abort(routeErrorAuthentication, ErrAuthentication)
		} else {
			abort(routeErrorServiceUnavailable, ErrServiceUnavailable)
		}
		return
	}
	hostState := &hostRouteAttempt{remote: clonePeerRoute(remote)}
	r.hosts[message.AttemptID] = hostState
	r.mu.Unlock()
	attempt, err := peertransport.NewPionHostAttempt(r.ctx, peertransport.PionHostConfig{
		Authorization: exact, Authenticator: authenticator, WebRTC: r.cfg.WebRTC,
		SendSignal: func(signalType, payload string) error {
			return r.sendSignal(remote, message.AttemptID, signalType, payload)
		},
		OnAuthenticated: callbacks.OnAuthenticated, OnRecord: callbacks.OnRecord,
		OnConfigMessage: callbacks.OnConfigMessage, OnTraffic: callbacks.OnTraffic,
		OnClosed: func(closeErr error) {
			r.removeHost(message.AttemptID, false)
			if callbacks.OnClosed != nil {
				callbacks.OnClosed(classifyRouteError(closeErr, false))
			}
		},
	})
	if err != nil {
		r.removeHost(message.AttemptID, false)
		abort(routeErrorServiceUnavailable, classifyRouteError(err, false))
		return
	}
	r.mu.Lock()
	if current := r.hosts[message.AttemptID]; current == hostState && !r.closed {
		hostState.attempt = attempt
	} else {
		r.mu.Unlock()
		_ = attempt.Close()
		return
	}
	r.mu.Unlock()
	if err := r.send(r.ctx, remote, claims); err != nil {
		r.removeHost(message.AttemptID, true)
	}
}

func (r *Route) sendRouteError(remote PeerRoute, attemptID uuid.UUID, code string) {
	if attemptID == uuid.Nil {
		return
	}
	_ = r.send(r.ctx, remote, routeWireMessage{
		Version: routeWireVersion, Kind: routeKindError, AttemptID: attemptID, Code: code,
	})
}

func (r *Route) handleClientResponse(remote PeerRoute, message routeWireMessage) {
	r.mu.Lock()
	pending := r.clients[message.AttemptID]
	catalog, hasCatalog := r.catalogs[message.AttemptID]
	r.mu.Unlock()
	if hasCatalog && message.Kind == routeKindError && catalog.remote.PeerID == remote.PeerID &&
		catalog.remote.PresenceID == remote.PresenceID {
		select {
		case catalog.response <- message:
		default:
		}
		return
	}
	if pending == nil || pending.remote.PeerID != remote.PeerID || pending.remote.PresenceID != remote.PresenceID {
		return
	}
	select {
	case pending.response <- message:
	default:
	}
}

func (r *Route) handleAttemptSignal(remote PeerRoute, message routeWireMessage) {
	if !validRouteSignal(message.SignalType, message.Payload) {
		return
	}
	r.mu.Lock()
	client := r.clients[message.AttemptID]
	host := r.hosts[message.AttemptID]
	r.mu.Unlock()
	if client != nil && client.remote.PeerID == remote.PeerID && client.attempt != nil {
		if err := client.attempt.HandleSignal(message.SignalType, message.Payload); err != nil {
			r.removeClient(message.AttemptID, true)
		}
		return
	}
	if host != nil && host.remote.PeerID == remote.PeerID && host.attempt != nil {
		if err := host.attempt.HandleSignal(message.SignalType, message.Payload); err != nil {
			r.removeHost(message.AttemptID, true)
		}
	}
}

func (r *Route) classifyClientClose(attemptID uuid.UUID, err error) error {
	r.mu.Lock()
	state := r.clients[attemptID]
	failed := state != nil && state.iceFailed
	r.mu.Unlock()
	return classifyRouteError(err, failed)
}

func classifyRouteError(err error, iceFailed bool) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, peertransport.ErrAuthentication) || errors.Is(err, peertransport.ErrInvalidHandshake) {
		return fmt.Errorf("%w: %v", ErrAuthentication, err)
	}
	if iceFailed || errors.Is(err, peertransport.ErrDirectTransport) {
		return fmt.Errorf("%w: %v", ErrICEFailed, err)
	}
	return err
}

func classifyWireRouteError(code string) error {
	switch code {
	case routeErrorServiceUnavailable, "closed":
		return ErrServiceUnavailable
	case routeErrorAuthentication:
		return ErrAuthentication
	default:
		return ErrAuthentication
	}
}

func (r *Route) removeClient(id uuid.UUID, closeAttempt bool) {
	r.mu.Lock()
	state := r.clients[id]
	delete(r.clients, id)
	r.mu.Unlock()
	if closeAttempt && state != nil && state.attempt != nil {
		_ = state.attempt.Close()
	}
}

func (r *Route) removeHost(id uuid.UUID, closeAttempt bool) {
	r.mu.Lock()
	state := r.hosts[id]
	delete(r.hosts, id)
	r.mu.Unlock()
	if closeAttempt && state != nil && state.attempt != nil {
		_ = state.attempt.Close()
	}
}

func (r *Route) Close() {
	if r != nil {
		r.finish(nil)
	}
}

// Done closes when the shared presence connection or its parent lifecycle
// ends. Callers use it to schedule reconnect without coupling other routes.
func (r *Route) Done() <-chan struct{} {
	if r == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return r.ctx.Done()
}

// OnlinePeerCount returns the current opaque presence count without exposing
// any Rendezvous routing identifier to callers or diagnostics.
func (r *Route) OnlinePeerCount() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.online)
}

// OnlinePeerRoutes resolves the current opaque host presences through the
// caller's active membership view. The returned identifiers never came from
// Rendezvous as trust assertions.
func (r *Route) OnlinePeerRoutes() []PeerRoute {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	presences := make([]string, 0, len(r.online))
	for presenceID, role := range r.online {
		if role == rendezvous.RoleHost {
			presences = append(presences, presenceID)
		}
	}
	r.mu.Unlock()
	routes := make([]PeerRoute, 0, len(presences))
	seen := make(map[string]struct{}, len(presences))
	for _, presenceID := range presences {
		remote, ok := r.cfg.ResolvePeer(presenceID)
		if !ok || remote.PresenceID != presenceID || remote.PeerID == "" {
			continue
		}
		if _, duplicate := seen[remote.PeerID]; duplicate {
			continue
		}
		seen[remote.PeerID] = struct{}{}
		routes = append(routes, clonePeerRoute(remote))
	}
	return routes
}

func (r *Route) finish(reason error) {
	r.closeOnce.Do(func() {
		r.cancel()
		r.cfg.Connection.CloseNow()
		r.mu.Lock()
		r.closed = true
		clients := r.clients
		hosts := r.hosts
		acks := r.acks
		catalogs := r.catalogs
		r.clients = make(map[uuid.UUID]*clientRouteAttempt)
		r.hosts = make(map[uuid.UUID]*hostRouteAttempt)
		r.acks = make(map[string]chan routeAck)
		r.catalogs = make(map[uuid.UUID]catalogRequest)
		r.mu.Unlock()
		for _, waiter := range acks {
			select {
			case waiter <- routeAck{err: ErrServiceUnavailable}:
			default:
			}
		}
		for _, state := range clients {
			if state.attempt != nil {
				_ = state.attempt.Close()
			} else {
				select {
				case state.response <- routeWireMessage{Version: routeWireVersion, Kind: routeKindError, Code: "closed"}:
				default:
				}
			}
		}
		for _, state := range hosts {
			if state.attempt != nil {
				_ = state.attempt.Close()
			}
		}
		for _, pending := range catalogs {
			select {
			case pending.response <- routeWireMessage{Version: routeWireVersion, Kind: routeKindError, Code: "closed"}:
			default:
			}
		}
		_ = reason
	})
}

func validateClientAttempt(cfg ClientAttemptConfig) (peerproto.VerifiedGrant, peerproto.VerifiedGrant, peerproto.VerifiedGenesis, error) {
	if cfg.Identity == nil || cfg.SessionID == uuid.Nil || !validRouteIdentifier(cfg.ClientInstanceID) ||
		cfg.Remote.PeerID == "" || validateOpaqueID(cfg.Remote.PresenceID, 32) != nil {
		return peerproto.VerifiedGrant{}, peerproto.VerifiedGrant{}, peerproto.VerifiedGenesis{}, ErrAuthentication
	}
	genesis, err := peerproto.VerifyGenesis(cfg.GenesisToken)
	if err != nil {
		return peerproto.VerifiedGrant{}, peerproto.VerifiedGrant{}, peerproto.VerifiedGenesis{}, err
	}
	now := time.Now()
	client, err := peerproto.VerifyGrant(cfg.ClientMembershipToken, genesis, now)
	if err != nil || client.Document.SubjectPeerID != cfg.Identity.PeerID() || !membershipAllowsRoute(client, cfg.SessionID) {
		return peerproto.VerifiedGrant{}, peerproto.VerifiedGrant{}, peerproto.VerifiedGenesis{}, ErrAuthentication
	}
	host, err := peerproto.VerifyGrant(cfg.HostMembershipToken, genesis, now)
	if err != nil || !membershipAllowsRoute(host, cfg.SessionID) {
		return peerproto.VerifiedGrant{}, peerproto.VerifiedGrant{}, peerproto.VerifiedGenesis{}, ErrAuthentication
	}
	return client, host, genesis, nil
}

func validateHostRouteAuthorization(request PeerOpenRequest, auth HostAuthorization, remote PeerRoute, spaceID string) (routeWireMessage, *peertransport.PeerMembershipAuthenticator, peertransport.Authorization, error) {
	genesis, err := peerproto.VerifyGenesis(auth.GenesisToken)
	if err != nil || genesis.Document.SpaceID != spaceID {
		return routeWireMessage{}, nil, peertransport.Authorization{}, ErrAuthentication
	}
	now := time.Now()
	client, err := peerproto.VerifyGrant(auth.ClientMembershipToken, genesis, now)
	if err != nil {
		return routeWireMessage{}, nil, peertransport.Authorization{}, ErrAuthentication
	}
	host, err := peerproto.VerifyGrant(auth.HostMembershipToken, genesis, now)
	if err != nil || auth.Identity == nil || client.Document.SubjectPeerID != request.ClientPeerID ||
		client.Document.SubjectPeerID != remote.PeerID || host.Document.SubjectPeerID != auth.Identity.PeerID() ||
		!bytes.Equal(client.WrappingPublicKey, remote.WrappingPublicKey) ||
		!membershipAllowsRoute(client, request.SessionID) || !membershipAllowsRoute(host, request.SessionID) ||
		!permissionWithin(auth.Permission, client.Document.Permission) || !permissionWithin(auth.Permission, host.Document.Permission) {
		return routeWireMessage{}, nil, peertransport.Authorization{}, ErrAuthentication
	}
	authenticator, err := peertransport.NewPeerMembershipAuthenticator(
		auth.Identity, peertransport.RoleHost, auth.GenesisToken,
		auth.ClientMembershipToken, auth.HostMembershipToken, now,
	)
	if err != nil {
		return routeWireMessage{}, nil, peertransport.Authorization{}, ErrAuthentication
	}
	expires := uint64(now.Add(routeAuthorizationTTL).UnixMilli())
	exact := peertransport.Authorization{
		AttemptID: request.AttemptID, Ticket: append([]byte(nil), request.Ticket...), SessionID: request.SessionID,
		UserID: genesis.Document.SpaceID, HostID: host.Document.SubjectPeerID,
		ClientInstanceID: request.ClientInstanceID, Permission: auth.Permission, ExpiresAtUnixMillis: expires,
	}
	claims := routeWireMessage{
		Version: routeWireVersion, Kind: routeKindAuthorized, AttemptID: request.AttemptID,
		UserID: exact.UserID, HostID: exact.HostID, Permission: exact.Permission,
		ExpiresAtUnixMillis: exact.ExpiresAtUnixMillis,
	}
	return claims, authenticator, exact, nil
}

func validateAuthorizedRoute(message, open routeWireMessage, genesis peerproto.VerifiedGenesis, client, host peerproto.VerifiedGrant) (peertransport.Authorization, error) {
	if message.Version != routeWireVersion || message.Kind != routeKindAuthorized || message.AttemptID != open.AttemptID ||
		message.UserID != genesis.Document.SpaceID || message.HostID != host.Document.SubjectPeerID ||
		message.ExpiresAtUnixMillis <= uint64(time.Now().UnixMilli()) ||
		!permissionWithin(message.Permission, client.Document.Permission) || !permissionWithin(message.Permission, host.Document.Permission) {
		return peertransport.Authorization{}, ErrAuthentication
	}
	return peertransport.Authorization{
		AttemptID: open.AttemptID, Ticket: append([]byte(nil), open.Ticket...), SessionID: open.SessionID,
		UserID: message.UserID, HostID: message.HostID, ClientInstanceID: open.ClientInstanceID,
		Permission: message.Permission, ExpiresAtUnixMillis: message.ExpiresAtUnixMillis,
	}, nil
}

func validateOpenWire(message routeWireMessage) error {
	if message.Version != routeWireVersion || message.Kind != routeKindOpen || message.AttemptID == uuid.Nil ||
		len(message.Ticket) != 32 || message.SessionID == uuid.Nil || !validRouteIdentifier(message.ClientPeerID) ||
		!validRouteIdentifier(message.ClientInstanceID) {
		return ErrAuthentication
	}
	return nil
}

func validateCatalogRequest(message routeWireMessage, expectedKind string) error {
	if message.Version != routeWireVersion || message.Kind != expectedKind ||
		(expectedKind != routeKindCatalog && expectedKind != routeKindCatalogRoutes) || message.AttemptID == uuid.Nil ||
		!validRouteIdentifier(message.ClientPeerID) || message.CatalogOffset < 0 || message.CatalogOffset > routeMaxCatalogEntries ||
		len(message.Sessions) != 0 || message.CatalogNextOffset != 0 || message.ConnectionBundle != "" || message.HostInfo != nil {
		return ErrAuthentication
	}
	return nil
}

func validateCatalogResponse(message, request routeWireMessage, remote PeerRoute, expectedKind string) error {
	if message.Version != routeWireVersion || message.Kind != expectedKind ||
		(expectedKind != routeKindCatalogOK && expectedKind != routeKindCatalogRoutesOK) || message.AttemptID != request.AttemptID ||
		message.HostID != remote.PeerID || message.CatalogOffset != request.CatalogOffset ||
		len(message.Sessions) > routeCatalogPageSize || message.CatalogNextOffset < 0 ||
		message.CatalogNextOffset > routeMaxCatalogEntries ||
		(message.CatalogNextOffset != 0 && (message.CatalogNextOffset <= message.CatalogOffset ||
			message.CatalogNextOffset != message.CatalogOffset+len(message.Sessions))) {
		return ErrAuthentication
	}
	if expectedKind == routeKindCatalogOK && message.ConnectionBundle != "" {
		return ErrAuthentication
	}
	if expectedKind == routeKindCatalogRoutesOK &&
		((message.CatalogOffset == 0 && (message.ConnectionBundle == "" || !utf8.ValidString(message.ConnectionBundle))) ||
			(message.CatalogOffset != 0 && message.ConnectionBundle != "")) {
		return ErrAuthentication
	}
	if request.CatalogOffset == 0 {
		if message.HostInfo != nil && validatePeerHost(*message.HostInfo) != nil {
			return ErrAuthentication
		}
	} else if message.HostInfo != nil {
		return ErrAuthentication
	}
	for _, session := range message.Sessions {
		if validatePeerSession(session) != nil {
			return ErrAuthentication
		}
	}
	return nil
}

func validatePeerHost(host PeerHost) error {
	if !validRouteIdentifier(host.ID) || len(host.Name) > routeMaxMetadataSize || !utf8.ValidString(host.Name) ||
		!permissionWithin(host.Permission, peerproto.PermissionFull) {
		return ErrInvalidSignal
	}
	return nil
}

func validatePeerSession(session PeerSession) error {
	if id, err := uuid.Parse(session.ID); err != nil || id == uuid.Nil || session.Cols == 0 || session.Rows == 0 ||
		!validRouteIdentifier(session.HostID) || !permissionWithin(session.Permission, peerproto.PermissionFull) {
		return ErrInvalidSignal
	}
	for _, value := range []string{
		session.Command, session.Cwd, session.Title, session.Host, session.User, session.SSHHostID,
		session.TaskState, session.CurrentCommand, session.Type,
	} {
		if len(value) > routeMaxMetadataSize || !utf8.ValidString(value) {
			return ErrInvalidSignal
		}
	}
	return nil
}

func clonePeerSessions(sessions []PeerSession) []PeerSession {
	if len(sessions) == 0 {
		return nil
	}
	out := append([]PeerSession(nil), sessions...)
	for index := range out {
		if out[index].CommandExitCode != nil {
			value := *out[index].CommandExitCode
			out[index].CommandExitCode = &value
		}
	}
	return out
}

func clonePeerHost(host *PeerHost) *PeerHost {
	if host == nil {
		return nil
	}
	copy := *host
	return &copy
}

func validRouteSignal(signalType, payload string) bool {
	if !utf8.ValidString(payload) {
		return false
	}
	switch signalType {
	case "offer", "answer":
		return payload != "" && len(payload) <= 32<<10
	case "ice_candidate":
		return payload != "" && len(payload) <= 8<<10
	case "ice_end":
		return payload == ""
	default:
		return false
	}
}

func validRouteIdentifier(value string) bool {
	return value != "" && len(value) <= routeMaxIdentifierSize && utf8.ValidString(value)
}

func membershipAllowsSession(membership peerproto.VerifiedGrant, sessionID uuid.UUID) bool {
	if len(membership.Document.AllowedSessionIDs) == 0 {
		return true
	}
	for _, candidate := range membership.Document.AllowedSessionIDs {
		if candidate == sessionID.String() {
			return true
		}
	}
	return false
}

func membershipAllowsRoute(membership peerproto.VerifiedGrant, sessionID uuid.UUID) bool {
	return sessionID == peertransport.ConfigSyncSessionID() || membershipAllowsSession(membership, sessionID)
}

func permissionWithin(effective peertransport.Permission, granted peerproto.Permission) bool {
	var ceiling peertransport.Permission
	switch granted {
	case peerproto.PermissionView:
		ceiling = peertransport.PermissionView
	case peerproto.PermissionControl:
		ceiling = peertransport.PermissionControl
	case peerproto.PermissionFull:
		ceiling = peertransport.PermissionFull
	default:
		return false
	}
	return effective >= peertransport.PermissionView && effective <= ceiling
}

func decodeRouteWire(payload []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return ErrInvalidSignal
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalidSignal
	}
	return nil
}

func decodeSignalPayload(encoded string) ([]byte, error) {
	if encoded == "" {
		return nil, ErrInvalidSignal
	}
	// The PresenceConnection already validates canonical event JSON. Decode
	// through the same strict base64url contract as the server.
	payload, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || base64.RawURLEncoding.EncodeToString(payload) != encoded || len(payload) == 0 || len(payload) > rendezvous.MaxPayloadBytes {
		return nil, ErrInvalidSignal
	}
	return payload, nil
}

func clonePeerRoute(route PeerRoute) PeerRoute {
	route.WrappingPublicKey = append([]byte(nil), route.WrappingPublicKey...)
	return route
}
