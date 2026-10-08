package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/quicktunnel"
	"github.com/attson/atterm/internal/rendezvousclient"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

const peerQuickTunnelBundleValidity = 10 * time.Minute
const peerQuickTunnelAuthorizationRefresh = 2 * time.Second

// JSON/base64 overhead beyond the 10 MiB binary body is bounded so malformed
// metadata cannot force an unbounded decode before the payload backstop runs.
const maxPeerPastePayloadBytes = 4*((maxPasteFileBytes+2)/3) + 4*1024

// FS writes use JSON's base64 encoding inside the already encrypted Peer
// record. Bound the encoded request before decoding so a malformed client
// cannot force an unbounded allocation ahead of fsAccess's 5 MiB hard cap.
const maxPeerFSPayloadBytes = 4*((maxWriteBytesHard+2)/3) + 64*1024

type peerQuickTunnelManager interface {
	Start(context.Context) (quicktunnel.Status, error)
	Stop() error
	Status() quicktunnel.Status
}

type peerQuickTunnelChannel interface {
	SendRecord(context.Context, peertransport.RecordKind, []byte) error
	SendFrame(context.Context, []byte) error
	SendConfigMessage(context.Context, peertransport.RecordKind, []byte) error
	RemoteMembershipToken() (string, bool)
	Close() error
}

// PeerQuickTunnelStatus is the Desktop-visible lifecycle state. Starting the
// host is always explicit; constructing App never opens a public route.
type PeerQuickTunnelStatus struct {
	Running     bool   `json:"running"`
	Starting    bool   `json:"starting"`
	PublicURL   string `json:"public_url,omitempty"`
	LocalOrigin string `json:"local_origin,omitempty"`
}

type peerQuickTunnelHost struct {
	app     *App
	host    *relayHost
	runtime *peerHostRuntime
	handler http.Handler
	tunnel  peerQuickTunnelManager

	mu       sync.Mutex
	attempts map[*quicktunnel.SignalChannel]*peerHostAttempt
}

// peerHostRuntime owns the transport-independent authorization and local
// session attachment rules shared by Quick Tunnel and Rendezvous signaling.
type peerHostRuntime struct {
	app  *App
	host *relayHost
}

type peerHostAttempt struct {
	host             *peerHostRuntime
	signal           *quicktunnel.SignalChannel
	remove           func()
	sessionID        uuid.UUID
	permission       string
	clientInstanceID string
	remotePeerID     string

	mu                sync.Mutex
	transport         *peertransport.PionHostAttempt
	channel           peerQuickTunnelChannel
	config            *peerConfigChannel
	remoteMembership  string
	streamCtx         context.Context
	sub               *session.Subscriber
	subscribedSession *session.Session
	streamCancel      context.CancelFunc
	remoteFS          *remoteFS
	fsPool            *fsWorkerPool
	leaseKey          peerHostRouteLeaseKey
	leaseHeld         bool
	starting          bool
	closed            bool
	closeOnce         sync.Once
}

func newPeerQuickTunnelHost(app *App, host *relayHost) (*peerQuickTunnelHost, error) {
	if app == nil || host == nil || host.server == nil || app.cfgStore == nil {
		return nil, errors.New("Quick Tunnel Peer host is unavailable")
	}
	peerHost := &peerQuickTunnelHost{
		app: app, host: host, runtime: &peerHostRuntime{app: app, host: host},
		attempts: make(map[*quicktunnel.SignalChannel]*peerHostAttempt),
	}
	handler, err := quicktunnel.NewPeerHandler(quicktunnel.HostConfig{
		Authorize: peerHost.authorize,
		Join: &quicktunnel.JoinHostConfig{
			LookupSecret: peerHost.lookupJoinSecret,
			Redeem:       peerHost.redeemJoin,
		},
		OnAuthenticated: peerHost.onAuthenticated,
		OnClosed:        peerHost.onSignalClosed,
	})
	if err != nil {
		return nil, err
	}
	peerHost.handler = handler
	peerHost.tunnel = quicktunnel.New(quicktunnel.Config{Handler: handler})
	return peerHost, nil
}

func (h *peerQuickTunnelHost) lookupJoinSecret(ctx context.Context, inviteID string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	manager, err := h.app.peerManager()
	if err != nil {
		return nil, quicktunnel.ErrJoinRejected
	}
	secret, err := manager.invitationPairingSecret(inviteID)
	if err != nil {
		return nil, quicktunnel.ErrJoinRejected
	}
	return secret, nil
}

func (h *peerQuickTunnelHost) redeemJoin(ctx context.Context, requestToken string) (quicktunnel.JoinBootstrap, error) {
	if err := ctx.Err(); err != nil {
		return quicktunnel.JoinBootstrap{}, err
	}
	manager, err := h.app.peerManager()
	if err != nil {
		return quicktunnel.JoinBootstrap{}, quicktunnel.ErrJoinRejected
	}
	result, err := manager.redeemJoinRequest(requestToken)
	if err != nil {
		return quicktunnel.JoinBootstrap{}, err
	}
	return quicktunnel.JoinBootstrap{
		GenesisToken: result.GenesisToken, MembershipToken: result.MembershipToken,
		Memberships: result.Memberships, Revocations: result.Revocations,
		EpochRotations: result.EpochRotations, EpochEnvelopes: result.EpochEnvelopes,
	}, nil
}

func (h *peerQuickTunnelHost) authorize(ctx context.Context, request quicktunnel.OpenRequest) (quicktunnel.HostAuthorization, error) {
	if err := ctx.Err(); err != nil {
		return quicktunnel.HostAuthorization{}, err
	}
	authorization, err := h.runtime.authorize(request.ClientPeerID, request.SessionID)
	if err != nil {
		return quicktunnel.HostAuthorization{}, quicktunnel.ErrUnauthorized
	}
	return quicktunnel.HostAuthorization{
		Identity: authorization.Identity, GenesisToken: authorization.GenesisToken,
		ClientMembershipToken: authorization.ClientMembershipToken,
		HostMembershipToken:   authorization.HostMembershipToken,
		Permission:            authorization.Permission,
	}, nil
}

type peerHostAuthorization struct {
	Identity              *peercrypto.Identity
	GenesisToken          string
	ClientMembershipToken string
	HostMembershipToken   string
	Permission            peertransport.Permission
}

func (h *peerHostRuntime) authorize(clientPeerID string, sessionID uuid.UUID) (peerHostAuthorization, error) {
	manager, err := h.app.peerManager()
	if err != nil {
		return peerHostAuthorization{}, quicktunnel.ErrUnauthorized
	}
	state, err := manager.store.Load()
	if err != nil {
		return peerHostAuthorization{}, quicktunnel.ErrUnauthorized
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return peerHostAuthorization{}, quicktunnel.ErrUnauthorized
	}
	identity, err := manager.loadIdentity()
	if err != nil {
		return peerHostAuthorization{}, quicktunnel.ErrUnauthorized
	}
	active, err := activePeerMemberships(state, genesis, time.Now())
	if err != nil {
		return peerHostAuthorization{}, quicktunnel.ErrUnauthorized
	}
	client := membershipForPeerID(active, clientPeerID)
	local := membershipForPeerID(active, identity.PeerID())
	if client == nil || local == nil {
		return peerHostAuthorization{}, quicktunnel.ErrUnauthorized
	}
	sess, ok := h.host.server.Registry().Get(sessionID)
	if !ok || sess.Info().HostID != h.host.hostID || !peerMembershipAllowsSession(*client, sessionID) || !peerMembershipAllowsSession(*local, sessionID) {
		return peerHostAuthorization{}, quicktunnel.ErrUnauthorized
	}
	ownerPermission, ok := directTransportPermission(h.app.cfgStore.Get().RemotePermissionOrDefault())
	if !ok {
		return peerHostAuthorization{}, quicktunnel.ErrUnauthorized
	}
	clientPermission, ok := peerPermission(client.Document.Permission)
	if !ok {
		return peerHostAuthorization{}, quicktunnel.ErrUnauthorized
	}
	localPermission, ok := peerPermission(local.Document.Permission)
	if !ok {
		return peerHostAuthorization{}, quicktunnel.ErrUnauthorized
	}
	return peerHostAuthorization{
		Identity: identity, GenesisToken: state.GenesisToken,
		ClientMembershipToken: client.Token, HostMembershipToken: local.Token,
		Permission: minimumPeerTransportPermission(ownerPermission, clientPermission, localPermission),
	}, nil
}

// authorizeConfig authenticates an active member without granting access to
// any terminal session. The reserved transcript session id keeps this channel
// cryptographically separate from terminal attempts.
func (h *peerHostRuntime) authorizeConfig(clientPeerID string) (peerHostAuthorization, error) {
	manager, err := h.app.peerManager()
	if err != nil {
		return peerHostAuthorization{}, quicktunnel.ErrUnauthorized
	}
	state, err := manager.store.Load()
	if err != nil {
		return peerHostAuthorization{}, quicktunnel.ErrUnauthorized
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return peerHostAuthorization{}, quicktunnel.ErrUnauthorized
	}
	identity, err := manager.loadIdentity()
	if err != nil {
		return peerHostAuthorization{}, quicktunnel.ErrUnauthorized
	}
	active, err := activePeerMemberships(state, genesis, time.Now())
	if err != nil {
		return peerHostAuthorization{}, quicktunnel.ErrUnauthorized
	}
	client := membershipForPeerID(active, clientPeerID)
	local := membershipForPeerID(active, identity.PeerID())
	if client == nil || local == nil {
		return peerHostAuthorization{}, quicktunnel.ErrUnauthorized
	}
	return peerHostAuthorization{
		Identity: identity, GenesisToken: state.GenesisToken,
		ClientMembershipToken: client.Token, HostMembershipToken: local.Token,
		Permission: peertransport.PermissionView,
	}, nil
}

func (h *peerHostRuntime) catalog(clientPeerID string) ([]rendezvousclient.PeerSession, error) {
	manager, err := h.app.peerManager()
	if err != nil {
		return nil, quicktunnel.ErrUnauthorized
	}
	state, err := manager.store.Load()
	if err != nil {
		return nil, quicktunnel.ErrUnauthorized
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return nil, quicktunnel.ErrUnauthorized
	}
	identity, err := manager.loadIdentity()
	if err != nil {
		return nil, quicktunnel.ErrUnauthorized
	}
	active, err := activePeerMemberships(state, genesis, time.Now())
	if err != nil {
		return nil, quicktunnel.ErrUnauthorized
	}
	client := membershipForPeerID(active, clientPeerID)
	local := membershipForPeerID(active, identity.PeerID())
	if client == nil || local == nil {
		return nil, quicktunnel.ErrUnauthorized
	}
	ownerPermission, ok := directTransportPermission(h.app.cfgStore.Get().RemotePermissionOrDefault())
	if !ok {
		return nil, quicktunnel.ErrUnauthorized
	}
	clientPermission, ok := peerPermission(client.Document.Permission)
	if !ok {
		return nil, quicktunnel.ErrUnauthorized
	}
	localPermission, ok := peerPermission(local.Document.Permission)
	if !ok {
		return nil, quicktunnel.ErrUnauthorized
	}
	effective := minimumPeerTransportPermission(ownerPermission, clientPermission, localPermission)
	result := make([]rendezvousclient.PeerSession, 0)
	h.host.server.Registry().ForEach(func(sess *session.Session) bool {
		if sess.Info().HostID != h.host.hostID || !peerMembershipAllowsSession(*client, sess.ID) ||
			!peerMembershipAllowsSession(*local, sess.ID) {
			return true
		}
		result = append(result, peerCatalogSession(sess.Info(), effective))
		return true
	})
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func peerCatalogSession(info proto.SessionInfo, permission peertransport.Permission) rendezvousclient.PeerSession {
	return rendezvousclient.PeerSession{
		ID: truncatePeerCatalogText(info.ID), Command: truncatePeerCatalogText(info.Command),
		Cwd: truncatePeerCatalogText(info.Cwd), Title: truncatePeerCatalogText(info.Title),
		Cols: info.Cols, Rows: info.Rows, StartedAt: info.StartedAt,
		HostID: truncatePeerCatalogText(info.HostID), Host: truncatePeerCatalogText(info.Host),
		User: truncatePeerCatalogText(info.User), SSHHostID: truncatePeerCatalogText(info.SSHHostID),
		Permission: permission, TaskState: truncatePeerCatalogText(info.TaskState),
		CurrentCommand: truncatePeerCatalogText(info.CurrentCommand), CommandStartedAt: info.CommandStartedAt,
		CommandEndedAt: info.CommandEndedAt, CommandDurationMS: info.CommandDurationMS,
		CommandExitCode: info.CommandExitCode, LastOutputAt: info.LastOutputAt,
		Type: truncatePeerCatalogText(info.Type), AttentionAt: info.AttentionAt,
	}
}

func truncatePeerCatalogText(value string) string {
	const limit = 1024
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func membershipForPeerID(memberships []peerproto.VerifiedGrant, peerID string) *peerproto.VerifiedGrant {
	for index := range memberships {
		if memberships[index].Document.SubjectPeerID == peerID {
			return &memberships[index]
		}
	}
	return nil
}

func peerPermission(permission peerproto.Permission) (peertransport.Permission, bool) {
	switch permission {
	case peerproto.PermissionView:
		return peertransport.PermissionView, true
	case peerproto.PermissionControl:
		return peertransport.PermissionControl, true
	case peerproto.PermissionFull:
		return peertransport.PermissionFull, true
	default:
		return 0, false
	}
}

func minimumPeerTransportPermission(permissions ...peertransport.Permission) peertransport.Permission {
	effective := peertransport.PermissionFull
	for _, permission := range permissions {
		if permission < effective {
			effective = permission
		}
	}
	return effective
}

func peerPermissionName(permission peertransport.Permission) string {
	switch permission {
	case peertransport.PermissionView:
		return proto.RemotePermissionView
	case peertransport.PermissionControl:
		return proto.RemotePermissionControl
	case peertransport.PermissionFull:
		return proto.RemotePermissionFull
	default:
		return ""
	}
}

func (h *peerQuickTunnelHost) onAuthenticated(signal *quicktunnel.SignalChannel) {
	binding := signal.Binding()
	permission := peerPermissionName(binding.Permission)
	if binding.AttemptID == uuid.Nil || binding.SessionID == uuid.Nil || permission == "" {
		_ = signal.Close()
		return
	}
	attempt := &peerHostAttempt{
		host: h.runtime, signal: signal, sessionID: binding.SessionID,
		permission: permission, clientInstanceID: binding.ClientInstanceID,
	}
	attempt.remove = func() { h.removeAttempt(signal) }
	h.mu.Lock()
	if _, exists := h.attempts[signal]; exists {
		h.mu.Unlock()
		_ = signal.Close()
		return
	}
	h.attempts[signal] = attempt
	h.mu.Unlock()

	parent := h.app.ctx
	if parent == nil {
		parent = context.Background()
	}
	transport, err := signal.BridgePionHost(parent, quicktunnel.PionHostBridgeConfig{
		WebRTC: webrtc.Configuration{ICEServers: []webrtc.ICEServer{{
			URLs: []string{"stun:stun.cloudflare.com:3478"},
		}}},
		WSSFallback: &quicktunnel.WSSFallbackConfig{
			OnAuthenticated: func(channel *quicktunnel.WSSChannel) {
				if err := attempt.start(parent, channel); err != nil {
					logWarn("quick-tunnel", "Peer WSS fallback start failed session=%s: %v", binding.SessionID, err)
					go h.removeAttempt(signal)
				}
			},
			OnRecord: func(kind peertransport.RecordKind, payload []byte) {
				if err := attempt.handleRecord(parent, kind, payload); err != nil {
					logWarn("quick-tunnel", "Peer WSS record rejected session=%s: %v", binding.SessionID, err)
					go h.removeAttempt(signal)
				}
			},
			OnConfigMessage: attempt.handleConfigMessage,
		},
		OnAuthenticated: func(channel *peertransport.PionHostChannel) {
			if err := attempt.start(parent, channel); err != nil {
				logWarn("quick-tunnel", "Peer session start failed session=%s: %v", binding.SessionID, err)
				go h.removeAttempt(signal)
			}
		},
		OnRecord: func(kind peertransport.RecordKind, payload []byte) {
			if err := attempt.handleRecord(parent, kind, payload); err != nil {
				logWarn("quick-tunnel", "Peer record rejected session=%s: %v", binding.SessionID, err)
				go h.removeAttempt(signal)
			}
		},
		OnConfigMessage: attempt.handleConfigMessage,
	})
	if err != nil {
		h.removeAttempt(signal)
		return
	}
	if !attempt.setTransport(transport) {
		_ = transport.Close()
	}
}

func (h *peerQuickTunnelHost) onSignalClosed(signal *quicktunnel.SignalChannel, _ error) {
	if attempt := h.takeAttempt(signal); attempt != nil {
		attempt.close(false)
	}
}

func (h *peerQuickTunnelHost) removeAttempt(signal *quicktunnel.SignalChannel) {
	if attempt := h.takeAttempt(signal); attempt != nil {
		attempt.close(true)
	}
}

func (h *peerQuickTunnelHost) takeAttempt(signal *quicktunnel.SignalChannel) *peerHostAttempt {
	h.mu.Lock()
	attempt := h.attempts[signal]
	delete(h.attempts, signal)
	h.mu.Unlock()
	return attempt
}

func (a *peerHostAttempt) start(parent context.Context, channel peerQuickTunnelChannel) error {
	a.mu.Lock()
	if a.closed || a.starting || a.sub != nil || a.channel != nil {
		a.mu.Unlock()
		return errors.New("Peer attempt closed or already streaming")
	}
	a.starting = true
	a.mu.Unlock()
	attached := false
	defer func() {
		if attached {
			return
		}
		a.mu.Lock()
		a.starting = false
		a.mu.Unlock()
	}()

	remoteMembership, ok := channel.RemoteMembershipToken()
	if !ok {
		return errors.New("Peer membership is unavailable after authentication")
	}
	currentPermission, remotePeerID, err := a.host.currentPeerAuthorization(remoteMembership, a.sessionID)
	if err != nil || permissionRankName(currentPermission) < permissionRankName(a.permission) {
		return errors.New("Peer authorization changed before attachment")
	}
	config, err := newPeerConfigChannel(a.host.app, channel)
	if err != nil {
		return err
	}
	sess, ok := a.host.host.server.Registry().Get(a.sessionID)
	if !ok || sess.Info().HostID != a.host.host.hostID {
		return errors.New("local Peer session is unavailable")
	}
	leaseKey := peerHostRouteLeaseKey{
		remotePeerID: remotePeerID, sessionID: a.sessionID, clientInstanceID: a.clientInstanceID,
	}
	replacedAttempt := a.host.app.currentPeerHostRouteLease(leaseKey)
	clientID := "peer:" + a.clientInstanceID
	subscribeOptions := []session.SubscribeOption{session.WithoutAutoDrive()}
	if replacedAttempt != nil && replacedAttempt != a {
		if replacedSub := replacedAttempt.currentSubscriberFor(a.sessionID); replacedSub != nil {
			subscribeOptions = append(subscribeOptions, session.ReplacingSubscriber(replacedSub))
		}
	}
	sub, replayToSeq := sess.Subscribe(0, clientID, a.clientInstanceID, subscribeOptions...)
	select {
	case <-sub.Done():
		return errors.New("Peer subscriber closed during attachment")
	default:
	}
	streamCtx, cancel := context.WithCancel(parent)
	peerFS := newRemoteFS(newFSAccess(remoteFSAllowRoots(), false), nil)
	peerFS.driverClientID = a.host.host.DriverClientID
	fsOut := make(chan proto.Frame, fsRequestsPerSession+1)
	fsPool := newFSWorkerPool(streamCtx, fsOut, proto.RemotePermissionFull, peerFS, fsRequestsPerSession)
	a.mu.Lock()
	if a.closed || a.sub != nil || a.channel != nil {
		a.mu.Unlock()
		cancel()
		peerFS.close()
		sess.Unsubscribe(sub)
		return errors.New("Peer attempt closed or already streaming")
	}
	a.channel = channel
	a.config = config
	a.remoteMembership = remoteMembership
	a.streamCtx = streamCtx
	a.sub = sub
	a.subscribedSession = sess
	a.streamCancel = cancel
	a.remoteFS = peerFS
	a.fsPool = fsPool
	a.remotePeerID = remotePeerID
	a.leaseKey = leaseKey
	previous, claimed := a.host.app.claimPeerHostRouteLease(leaseKey, replacedAttempt, a)
	if !claimed {
		a.channel = nil
		a.config = nil
		a.remoteMembership = ""
		a.streamCtx = nil
		a.sub = nil
		a.subscribedSession = nil
		a.streamCancel = nil
		a.remoteFS = nil
		a.fsPool = nil
		a.remotePeerID = ""
		a.leaseKey = peerHostRouteLeaseKey{}
		a.starting = false
		a.mu.Unlock()
		cancel()
		peerFS.close()
		sess.Unsubscribe(sub)
		return errors.New("Peer route lease changed during attachment")
	}
	a.leaseHeld = true
	a.starting = false
	a.mu.Unlock()
	attached = true
	go a.stream(streamCtx, channel, sub, replayToSeq)
	go a.streamFilesystem(streamCtx, channel, peerFS, fsOut)
	go a.watchAuthorization(streamCtx)
	if previous != nil && previous != a {
		previous.removeSelf()
	}
	if err := config.Start(streamCtx); err != nil {
		return fmt.Errorf("start Peer config channel: %w", err)
	}
	return nil
}

func (a *peerHostAttempt) stream(ctx context.Context, channel peerQuickTunnelChannel, sub *session.Subscriber, replayToSeq uint64) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.Done():
			a.removeSelf()
			return
		case frame, open := <-sub.Out():
			if !open {
				a.removeSelf()
				return
			}
			if !a.currentRouteLease() {
				a.removeSelf()
				return
			}
			if frame.Type == proto.TypeReplayProgress {
				var progress proto.ReplayProgressPayload
				if json.Unmarshal(frame.Payload, &progress) == nil && progress.Phase == proto.ReplayProgressEnd {
					if progress.Seq != replayToSeq {
						a.removeSelf()
						return
					}
					ready := make([]byte, 8)
					binary.BigEndian.PutUint64(ready, replayToSeq)
					if err := channel.SendRecord(ctx, peertransport.RecordDirectReady, ready); err != nil {
						a.removeSelf()
						return
					}
				}
				continue
			}
			prepared, forward := prepareRemoteSubscriberFrame(frame, nil)
			if !forward {
				continue
			}
			if err := channel.SendFrame(ctx, proto.Marshal(prepared)); err != nil {
				a.removeSelf()
				return
			}
		}
	}
}

func (a *peerHostAttempt) handleRecord(ctx context.Context, kind peertransport.RecordKind, payload []byte) error {
	if kind != peertransport.RecordClose && !a.currentRouteLease() {
		return errors.New("Peer route lease is no longer active")
	}
	switch kind {
	case peertransport.RecordPing:
		if len(payload) != 8 {
			return errors.New("invalid Peer ping")
		}
		a.mu.Lock()
		channel := a.channel
		remoteMembership := a.remoteMembership
		a.mu.Unlock()
		if channel == nil {
			return errors.New("Peer transport unavailable")
		}
		if err := a.validateCurrentAuthorization(remoteMembership); err != nil {
			return err
		}
		return channel.SendRecord(ctx, peertransport.RecordPong, payload)
	case peertransport.RecordClose:
		go a.removeSelf()
		return nil
	case peertransport.RecordFrame:
		currentPermission, err := a.host.currentOwnerPermission()
		if err != nil {
			return errors.New("Peer authorization is no longer active")
		}
		if permissionRankName(currentPermission) > permissionRankName(a.permission) {
			currentPermission = a.permission
		}
		frame, err := proto.Unmarshal(payload)
		if err != nil || frame.SessionID != a.sessionID {
			return errors.New("invalid Peer terminal frame")
		}
		switch frame.Type {
		case proto.TypeIn, proto.TypeResize:
			if !localFrameAllowedByPermission(currentPermission, frame.Type) {
				return errors.New("Peer frame exceeds permission")
			}
			a.mu.Lock()
			sub := a.sub
			a.mu.Unlock()
			sess, ok := a.host.host.server.Registry().Get(a.sessionID)
			if sub == nil || !ok || !sess.IsDriver(sub) {
				return errors.New("Peer subscriber is not driver")
			}
			return a.host.host.SendLocalInbound(a.sessionID, frame)
		case proto.TypePasteImage, proto.TypePasteFile:
			a.mu.Lock()
			sub := a.sub
			remoteMembership := a.remoteMembership
			a.mu.Unlock()
			pastePermission, err := a.host.currentPermission(remoteMembership, a.sessionID)
			if err != nil ||
				!localFrameAllowedByPermission(currentPermission, frame.Type) ||
				!localFrameAllowedByPermission(pastePermission, frame.Type) {
				return errors.New("Peer paste exceeds permission")
			}
			sess, ok := a.host.host.server.Registry().Get(a.sessionID)
			if sub == nil || !ok || !sess.IsDriver(sub) {
				return errors.New("Peer subscriber is not driver")
			}
			if err := validatePeerPasteFrame(frame); err != nil {
				return err
			}
			return a.host.host.SendLocalInbound(a.sessionID, frame)
		case proto.TypeFSRequest:
			return a.handleFilesystemRequest(frame)
		case proto.TypeClaimDriver:
			if currentPermission == proto.RemotePermissionView {
				return errors.New("Peer driver claim exceeds permission")
			}
			var claim proto.ClaimDriverPayload
			if err := json.Unmarshal(frame.Payload, &claim); err != nil || claim.ClientID == "" {
				return errors.New("invalid Peer driver claim")
			}
			a.mu.Lock()
			sub := a.sub
			a.mu.Unlock()
			sess, ok := a.host.host.server.Registry().Get(a.sessionID)
			if sub == nil || !ok {
				return errors.New("Peer subscriber unavailable")
			}
			sess.ClaimDriver(sub, claim.ClientID, claim.ClientName)
			return nil
		default:
			return fmt.Errorf("Peer frame type 0x%02x is not allowed", frame.Type)
		}
	default:
		return fmt.Errorf("Peer record kind %d is not accepted from client", kind)
	}
}

func (a *peerHostAttempt) handleFilesystemRequest(frame proto.Frame) error {
	if len(frame.Payload) > maxPeerFSPayloadBytes {
		return errors.New("Peer filesystem payload exceeds encoded size limit")
	}
	a.mu.Lock()
	remoteMembership := a.remoteMembership
	pool := a.fsPool
	sub := a.sub
	a.mu.Unlock()
	permission, err := a.host.currentPermission(remoteMembership, a.sessionID)
	if err != nil || permission != proto.RemotePermissionFull || a.permission != proto.RemotePermissionFull {
		return errors.New("Peer filesystem requires full permission")
	}
	request, err := proto.DecodeFSRequest(frame.Payload, nil, a.sessionID)
	if err != nil {
		return errors.New("invalid Peer filesystem request")
	}
	if request.RequestID == "" || len(request.RequestID) > 256 || request.Op == "" || len(request.Op) > 64 {
		return errors.New("invalid Peer filesystem request")
	}
	if pool == nil {
		return errors.New("Peer filesystem is unavailable")
	}
	// The renderer is untrusted. Bind open_external and diagnostics to the
	// authenticated logical client identity carried by the route lease.
	request.ClientID = a.clientInstanceID
	if request.Op == "open_external" {
		sess, ok := a.host.host.server.Registry().Get(a.sessionID)
		if sub == nil || !ok || !sess.IsDriver(sub) {
			return errors.New("Peer filesystem open_external requires current driver")
		}
	}
	pool.submit(a.sessionID, request)
	return nil
}

func (a *peerHostAttempt) streamFilesystem(ctx context.Context, channel peerQuickTunnelChannel, fs *remoteFS, responses <-chan proto.Frame) {
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-responses:
			if !a.sendFilesystemFrame(ctx, channel, frame) {
				return
			}
		case frame := <-fs.events():
			if !a.sendFilesystemFrame(ctx, channel, frame) {
				return
			}
		}
	}
}

func (a *peerHostAttempt) sendFilesystemFrame(ctx context.Context, channel peerQuickTunnelChannel, frame proto.Frame) bool {
	if frame.SessionID != a.sessionID ||
		(frame.Type != proto.TypeFSResponse && frame.Type != proto.TypeFSEvent) ||
		!a.currentRouteLease() {
		return false
	}
	a.mu.Lock()
	currentChannel := a.channel
	remoteMembership := a.remoteMembership
	a.mu.Unlock()
	permission, err := a.host.currentPermission(remoteMembership, a.sessionID)
	if err != nil || permission != proto.RemotePermissionFull || currentChannel != channel {
		return false
	}
	if err := channel.SendFrame(ctx, proto.Marshal(frame)); err != nil {
		a.removeSelf()
		return false
	}
	return true
}

func validatePeerPasteFrame(frame proto.Frame) error {
	if len(frame.Payload) > maxPeerPastePayloadBytes {
		return errors.New("Peer paste payload exceeds encoded size limit")
	}
	switch frame.Type {
	case proto.TypePasteImage:
		var payload proto.PasteImagePayload
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return errors.New("invalid Peer paste image payload")
		}
		if len(payload.Data) == 0 || len(payload.Data) > maxPasteImageBytes {
			return errors.New("Peer paste image data exceeds size limit")
		}
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(payload.ContentType)), "image/") {
			return errors.New("invalid Peer paste image content type")
		}
	case proto.TypePasteFile:
		var payload proto.PasteFilePayload
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return errors.New("invalid Peer paste file payload")
		}
		if len(payload.Data) == 0 || len(payload.Data) > maxPasteFileBytes {
			return errors.New("Peer paste file data exceeds size limit")
		}
	default:
		return errors.New("unsupported Peer paste frame")
	}
	return nil
}

func (a *peerHostAttempt) handleConfigMessage(kind peertransport.RecordKind, payload []byte) error {
	if !a.currentRouteLease() {
		return errors.New("Peer route lease is no longer active")
	}
	a.mu.Lock()
	config := a.config
	remoteMembership := a.remoteMembership
	ctx := a.streamCtx
	a.mu.Unlock()
	if config == nil {
		return errors.New("Peer config channel unavailable")
	}
	if err := a.validateCurrentAuthorization(remoteMembership); err != nil {
		return err
	}
	if err := config.Handle(ctx, kind, payload); err != nil {
		return err
	}
	return a.validateCurrentAuthorization(remoteMembership)
}

func (a *peerHostAttempt) watchAuthorization(ctx context.Context) {
	ticker := time.NewTicker(peerQuickTunnelAuthorizationRefresh)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !a.currentRouteLease() {
				a.removeSelf()
				return
			}
			a.mu.Lock()
			remoteMembership := a.remoteMembership
			a.mu.Unlock()
			if err := a.validateCurrentAuthorization(remoteMembership); err != nil {
				a.removeSelf()
				return
			}
		}
	}
}

func (a *peerHostAttempt) validateCurrentAuthorization(remoteMembership string) error {
	currentPermission, err := a.host.currentPermission(remoteMembership, a.sessionID)
	if err != nil || permissionRankName(currentPermission) < permissionRankName(a.permission) {
		return errors.New("Peer authorization is no longer active")
	}
	return nil
}

func (a *peerHostAttempt) setTransport(transport *peertransport.PionHostAttempt) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return false
	}
	a.transport = transport
	return true
}

func (a *peerHostAttempt) close(closeSignal bool) {
	a.closeOnce.Do(func() {
		a.mu.Lock()
		a.closed = true
		cancel := a.streamCancel
		sub := a.sub
		sess := a.subscribedSession
		transport := a.transport
		channel := a.channel
		peerFS := a.remoteFS
		signal := a.signal
		leaseKey := a.leaseKey
		leaseHeld := a.leaseHeld
		a.streamCancel = nil
		a.remoteFS = nil
		a.fsPool = nil
		a.sub = nil
		a.subscribedSession = nil
		a.channel = nil
		a.config = nil
		a.remoteMembership = ""
		a.remotePeerID = ""
		a.streamCtx = nil
		a.leaseHeld = false
		a.starting = false
		a.mu.Unlock()
		if leaseHeld && a.host != nil {
			a.host.app.releasePeerHostRouteLease(leaseKey, a)
		}
		if cancel != nil {
			cancel()
		}
		if peerFS != nil {
			peerFS.close()
		}
		if sub != nil && sess != nil {
			sess.Unsubscribe(sub)
		}
		if transport != nil {
			_ = transport.Close()
		}
		if channel != nil {
			_ = channel.Close()
		}
		if closeSignal && signal != nil {
			_ = signal.Close()
		}
	})
}

func (a *peerHostAttempt) removeSelf() {
	if a == nil {
		return
	}
	if a.remove != nil {
		a.remove()
		return
	}
	a.close(true)
}

func (a *peerHostAttempt) syncConfigNow() (bool, error) {
	if a == nil {
		return false, nil
	}
	a.mu.Lock()
	config := a.config
	ctx := a.streamCtx
	closed := a.closed
	a.mu.Unlock()
	if closed || config == nil || ctx == nil || ctx.Err() != nil || !a.currentRouteLease() {
		return false, nil
	}
	return true, config.Start(ctx)
}

func (h *peerHostRuntime) currentPermission(remoteMembership string, sessionID uuid.UUID) (string, error) {
	permission, _, err := h.currentPeerAuthorization(remoteMembership, sessionID)
	return permission, err
}

func (h *peerHostRuntime) currentPeerAuthorization(remoteMembership string, sessionID uuid.UUID) (string, string, error) {
	if remoteMembership == "" {
		return "", "", quicktunnel.ErrUnauthorized
	}
	manager, err := h.app.peerManager()
	if err != nil {
		return "", "", quicktunnel.ErrUnauthorized
	}
	state, err := manager.store.Load()
	if err != nil {
		return "", "", quicktunnel.ErrUnauthorized
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return "", "", quicktunnel.ErrUnauthorized
	}
	now := time.Now()
	if manager.now != nil {
		now = manager.now()
	}
	remote, err := peerproto.VerifyGrant(remoteMembership, genesis, now)
	if err != nil {
		return "", "", quicktunnel.ErrUnauthorized
	}
	identity, err := manager.loadIdentity()
	if err != nil {
		return "", "", quicktunnel.ErrUnauthorized
	}
	active, err := activePeerMemberships(state, genesis, now)
	if err != nil {
		return "", "", quicktunnel.ErrUnauthorized
	}
	canonicalRemote := membershipForPeerID(active, remote.Document.SubjectPeerID)
	local := membershipForPeerID(active, identity.PeerID())
	if canonicalRemote == nil || canonicalRemote.Token != remoteMembership || local == nil {
		return "", "", quicktunnel.ErrUnauthorized
	}
	sess, ok := h.host.server.Registry().Get(sessionID)
	if !ok || sess.Info().HostID != h.host.hostID {
		return "", "", quicktunnel.ErrUnauthorized
	}
	if !peerMembershipAllowsSession(remote, sessionID) || !peerMembershipAllowsSession(*local, sessionID) {
		return "", "", quicktunnel.ErrUnauthorized
	}
	ownerPermission, err := h.currentOwnerPermission()
	if err != nil {
		return "", "", quicktunnel.ErrUnauthorized
	}
	ownerTransportPermission, ok := directTransportPermission(ownerPermission)
	if !ok {
		return "", "", quicktunnel.ErrUnauthorized
	}
	remotePermission, ok := peerPermission(remote.Document.Permission)
	if !ok {
		return "", "", quicktunnel.ErrUnauthorized
	}
	localPermission, ok := peerPermission(local.Document.Permission)
	if !ok {
		return "", "", quicktunnel.ErrUnauthorized
	}
	return peerPermissionName(minimumPeerTransportPermission(ownerTransportPermission, remotePermission, localPermission)), remote.Document.SubjectPeerID, nil
}

func peerMembershipAllowsSession(membership peerproto.VerifiedGrant, sessionID uuid.UUID) bool {
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

func (h *peerHostRuntime) currentOwnerPermission() (string, error) {
	permission := h.app.cfgStore.Get().RemotePermissionOrDefault()
	if _, ok := directTransportPermission(permission); !ok {
		return "", quicktunnel.ErrUnauthorized
	}
	return permission, nil
}

func permissionRankName(permission string) int {
	switch permission {
	case proto.RemotePermissionView:
		return 1
	case proto.RemotePermissionControl:
		return 2
	case proto.RemotePermissionFull:
		return 3
	default:
		return 0
	}
}

func (h *peerQuickTunnelHost) Start(ctx context.Context) (PeerQuickTunnelStatus, error) {
	manager, err := h.app.peerManager()
	if err != nil {
		return PeerQuickTunnelStatus{}, err
	}
	status, err := manager.status()
	if err != nil {
		return PeerQuickTunnelStatus{}, err
	}
	if !status.Configured {
		return PeerQuickTunnelStatus{}, errors.New("Peer Space must be created before starting Quick Tunnel")
	}
	state, err := manager.store.Load()
	if err != nil {
		return PeerQuickTunnelStatus{}, err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return PeerQuickTunnelStatus{}, err
	}
	identity, err := manager.loadIdentity()
	if err != nil {
		return PeerQuickTunnelStatus{}, err
	}
	active, err := activePeerMemberships(state, genesis, time.Now())
	if err != nil {
		return PeerQuickTunnelStatus{}, err
	}
	if membershipForPeerID(active, identity.PeerID()) == nil {
		return PeerQuickTunnelStatus{}, errors.New("local Peer membership is not active")
	}
	tunnelStatus, err := h.tunnel.Start(ctx)
	return publicPeerQuickTunnelStatus(tunnelStatus), err
}

func (h *peerQuickTunnelHost) Status() PeerQuickTunnelStatus {
	return publicPeerQuickTunnelStatus(h.tunnel.Status())
}

func publicPeerQuickTunnelStatus(status quicktunnel.Status) PeerQuickTunnelStatus {
	return PeerQuickTunnelStatus{
		Running: status.Running, Starting: status.Starting,
		PublicURL: status.PublicURL, LocalOrigin: status.LocalOrigin,
	}
}

func (h *peerQuickTunnelHost) ConnectionBundle(invitationToken string) (string, error) {
	status := h.tunnel.Status()
	manager, err := h.app.peerManager()
	if err != nil {
		return "", err
	}
	state, err := manager.store.Load()
	if err != nil {
		return "", err
	}
	identity, err := manager.loadIdentity()
	if err != nil {
		return "", err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return "", err
	}
	now := time.Now()
	active, err := activePeerMemberships(state, genesis, now)
	if err != nil {
		return "", err
	}
	local := membershipForPeerID(active, identity.PeerID())
	if local == nil {
		return "", errors.New("local Peer membership is not active")
	}
	routes := make([]peerproto.ConnectionRoute, 0, 2)
	if status.Running && status.PublicURL != "" {
		routes = append(routes, peerproto.ConnectionRoute{Kind: peerproto.RouteQuickTunnel, URL: status.PublicURL})
	}
	h.app.peerRendezvousMu.Lock()
	rendezvousLifecycle := h.app.peerRendezvous
	h.app.peerRendezvousMu.Unlock()
	if route, ok := rendezvousLifecycle.connectionRoute(); ok {
		routes = append(routes, route)
	}
	if len(routes) == 0 {
		return "", errors.New("Peer connection has no published route")
	}
	if invitationToken != "" {
		if !status.Running || status.PublicURL == "" {
			return "", errors.New("first Peer join still requires a published Quick Tunnel route")
		}
		open := false
		for _, invitation := range state.Invitations {
			if invitation.Token == invitationToken && invitation.ConsumedAt == 0 && invitation.RevokedAt == 0 && now.Unix() < invitation.ExpiresAt {
				open = true
				break
			}
		}
		if !open {
			return "", errors.New("Peer invitation is not open")
		}
		ticket, issuer, err := peerproto.VerifyInvitation(invitationToken, genesis, now)
		if err != nil || ticket.RedemptionPeerID != identity.PeerID() || issuer.Token != local.Token {
			return "", errors.New("Peer invitation is not authorized by the active local membership")
		}
		return peerproto.NewConnectionBundle(identity, genesis, invitationToken, routes, now, peerQuickTunnelBundleValidity)
	}
	return peerproto.NewMemberConnectionBundle(identity, genesis, local.Token, routes, now, peerQuickTunnelBundleValidity)
}

func (h *peerQuickTunnelHost) Stop() error {
	h.mu.Lock()
	attempts := make([]*peerHostAttempt, 0, len(h.attempts))
	for signal, attempt := range h.attempts {
		delete(h.attempts, signal)
		attempts = append(attempts, attempt)
	}
	h.mu.Unlock()
	var wait sync.WaitGroup
	wait.Add(len(attempts))
	for _, attempt := range attempts {
		go func() {
			defer wait.Done()
			attempt.close(true)
		}()
	}
	wait.Wait()
	return h.tunnel.Stop()
}

func (h *peerQuickTunnelHost) revalidateAttempts() {
	h.mu.Lock()
	attempts := make([]*peerHostAttempt, 0, len(h.attempts))
	for _, attempt := range h.attempts {
		attempts = append(attempts, attempt)
	}
	h.mu.Unlock()
	for _, attempt := range attempts {
		attempt.mu.Lock()
		remoteMembership := attempt.remoteMembership
		attempt.mu.Unlock()
		if remoteMembership != "" && attempt.validateCurrentAuthorization(remoteMembership) != nil {
			h.removeAttempt(attempt.signal)
		}
	}
}

func (h *peerQuickTunnelHost) syncConfigNow() (int, error) {
	if h == nil {
		return 0, nil
	}
	h.mu.Lock()
	attempts := make([]*peerHostAttempt, 0, len(h.attempts))
	for _, attempt := range h.attempts {
		attempts = append(attempts, attempt)
	}
	h.mu.Unlock()
	sent := 0
	var syncErr error
	for _, attempt := range attempts {
		active, err := attempt.syncConfigNow()
		if active {
			sent++
		}
		if err != nil {
			syncErr = errors.Join(syncErr, err)
		}
	}
	return sent, syncErr
}

func (a *App) revalidatePeerQuickTunnelAttempts() {
	a.mu.Lock()
	host, _ := a.quickTunnel.(*peerQuickTunnelHost)
	a.mu.Unlock()
	if host != nil {
		host.revalidateAttempts()
	}
}

func (a *App) ensurePeerQuickTunnelHost() (*peerQuickTunnelHost, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.quickTunnel != nil {
		host, ok := a.quickTunnel.(*peerQuickTunnelHost)
		if !ok {
			return nil, errors.New("Quick Tunnel lifecycle is not a Peer host")
		}
		return host, nil
	}
	host, err := newPeerQuickTunnelHost(a, a.host)
	if err != nil {
		return nil, err
	}
	a.quickTunnel = host
	return host, nil
}

// StartPeerQuickTunnel explicitly publishes the local Peer gateway.
func (a *App) StartPeerQuickTunnel() (PeerQuickTunnelStatus, error) {
	host, err := a.ensurePeerQuickTunnelHost()
	if err != nil {
		return PeerQuickTunnelStatus{}, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return host.Start(ctx)
}

// StopPeerQuickTunnel removes the public route without deleting Peer trust.
func (a *App) StopPeerQuickTunnel() error {
	a.mu.Lock()
	host, _ := a.quickTunnel.(*peerQuickTunnelHost)
	a.mu.Unlock()
	if host == nil {
		return nil
	}
	return host.Stop()
}

// GetPeerQuickTunnelStatus returns an idle status before first explicit start.
func (a *App) GetPeerQuickTunnelStatus() PeerQuickTunnelStatus {
	a.mu.Lock()
	host, _ := a.quickTunnel.(*peerQuickTunnelHost)
	a.mu.Unlock()
	if host == nil {
		return PeerQuickTunnelStatus{}
	}
	return host.Status()
}

// CreatePeerConnectionBundle signs the current temporary route. Passing an
// invitation publishes a first-join bundle; an empty value publishes a member
// reconnect bundle without rotating durable trust.
func (a *App) CreatePeerConnectionBundle(invitationToken string) (string, error) {
	host, err := a.ensurePeerQuickTunnelHost()
	if err != nil {
		return "", err
	}
	return host.ConnectionBundle(invitationToken)
}
