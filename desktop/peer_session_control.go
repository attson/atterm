package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/proto"
)

// peerSessionControlHost rejects retired session creation over the reserved
// authenticated control route without subscribing to or creating a PTY.
type peerSessionControlHost struct {
	runtime          *peerHostRuntime
	ctx              context.Context
	channel          peerQuickTunnelChannel
	remoteMembership string
}

func newPeerSessionControlHost(ctx context.Context, runtime *peerHostRuntime, channel peerQuickTunnelChannel, remoteMembership string) (*peerSessionControlHost, error) {
	if ctx == nil || runtime == nil || runtime.host == nil || channel == nil || remoteMembership == "" {
		return nil, errors.New("Peer session control route is unavailable")
	}
	if _, err := runtime.currentHostControlPermission(remoteMembership); err != nil {
		return nil, err
	}
	return &peerSessionControlHost{
		runtime: runtime, ctx: ctx, channel: channel, remoteMembership: remoteMembership,
	}, nil
}

func (c *peerSessionControlHost) Handle(kind peertransport.RecordKind, payload []byte) (bool, error) {
	if kind != peertransport.RecordSessionCreate {
		return false, nil
	}
	if len(payload) == 0 || len(payload) > maxPeerSessionCreatePayloadBytes {
		return true, errors.New("invalid Peer host session create request")
	}
	var request proto.SessionCreatePayload
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		request.RequestID == "" || len(request.RequestID) > 256 ||
		request.ProfileID == "" || len(request.ProfileID) > 256 ||
		request.HostID == "" || request.HostID != c.runtime.host.hostID {
		return true, errors.New("invalid Peer host session create request")
	}
	if _, err := c.runtime.currentHostControlPermission(c.remoteMembership); err != nil {
		return true, err
	}
	response := retiredSessionCreateFrame(request.RequestID)
	return true, c.channel.SendConfigMessage(c.ctx, peertransport.RecordSessionCreated, response.Payload)
}

func (h *peerHostRuntime) currentHostControlPermission(remoteMembership string) (string, error) {
	manager, err := h.app.peerManager()
	if err != nil {
		return "", errors.New("Peer host control is unauthorized")
	}
	state, err := manager.store.Load()
	if err != nil {
		return "", errors.New("Peer host control is unauthorized")
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return "", errors.New("Peer host control is unauthorized")
	}
	now := time.Now()
	if manager.now != nil {
		now = manager.now()
	}
	remote, err := peerproto.VerifyGrant(remoteMembership, genesis, now)
	if err != nil {
		return "", errors.New("Peer host control is unauthorized")
	}
	identity, err := manager.loadIdentity()
	if err != nil {
		return "", errors.New("Peer host control is unauthorized")
	}
	active, err := activePeerMemberships(state, genesis, now)
	if err != nil {
		return "", errors.New("Peer host control is unauthorized")
	}
	canonicalRemote := membershipForPeerID(active, remote.Document.SubjectPeerID)
	local := membershipForPeerID(active, identity.PeerID())
	if canonicalRemote == nil || canonicalRemote.Token != remoteMembership || local == nil ||
		len(canonicalRemote.Document.AllowedSessionIDs) != 0 || len(local.Document.AllowedSessionIDs) != 0 {
		return "", errors.New("Peer host control is unauthorized")
	}
	ownerPermission, err := h.currentOwnerPermission()
	if err != nil {
		return "", err
	}
	owner, ok := directTransportPermission(ownerPermission)
	if !ok {
		return "", errors.New("Peer host control is unauthorized")
	}
	remotePermission, remoteOK := peerPermission(canonicalRemote.Document.Permission)
	localPermission, localOK := peerPermission(local.Document.Permission)
	if !remoteOK || !localOK {
		return "", errors.New("Peer host control is unauthorized")
	}
	effective := peerPermissionName(minimumPeerTransportPermission(owner, remotePermission, localPermission))
	if permissionRankName(effective) < permissionRankName(proto.RemotePermissionControl) {
		return "", errors.New("Peer host control requires control permission")
	}
	return effective, nil
}
