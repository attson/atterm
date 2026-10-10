// Package peerdiscovery derives opaque Rendezvous coordinates and selects
// reachable peers for decentralized configuration anti-entropy.
package peerdiscovery

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/rendezvous"
)

const (
	// RotationInterval limits how long one member can be linked by its opaque
	// routing identifier while allowing a practical clock-skew window.
	RotationInterval = 15 * time.Minute
	SmallSpaceLimit  = 8
	DefaultFanout    = 4
	MaxCandidates    = 256
	MaxVectorEntries = 256
)

const (
	topicDomain    = "atterm-peer-rendezvous-topic-v1"
	presenceDomain = "atterm-peer-rendezvous-presence-v1"
	tieDomain      = "atterm-peer-sync-fanout-v1"
)

var ErrInvalidDiscovery = errors.New("peerdiscovery: invalid discovery state")

// Coordinates are opaque to Rendezvous. Topic is stable for one sync epoch;
// PresenceID rotates independently for each member and time slot.
type Coordinates struct {
	Topic      string
	PresenceID string
	Slot       int64
}

// DeriveCoordinates creates canonical base64url routing values from the
// current sync epoch key. Epoch rotation unlinks both values.
func DeriveCoordinates(key configsync.EpochKey, spaceID, peerID string, at time.Time) (Coordinates, error) {
	if key.Class != configsync.KeyClassSync || key.Epoch == 0 || len(key.Bytes()) != configsync.EpochKeySize ||
		spaceID == "" || peerID == "" || at.IsZero() {
		return Coordinates{}, ErrInvalidDiscovery
	}
	slot := at.Unix() / int64(RotationInterval/time.Second)
	topic := deriveOpaque(key.Bytes(), topicDomain, spaceID, key.Epoch)
	presence := deriveOpaque(key.Bytes(), presenceDomain, spaceID, key.Epoch, peerID, slot)
	return Coordinates{Topic: topic, PresenceID: presence, Slot: slot}, nil
}

// ResolvePresence maps an opaque identifier only against the supplied active
// member set. Adjacent slots tolerate ordinary device clock skew.
func ResolvePresence(key configsync.EpochKey, spaceID string, activePeerIDs []string, presenceID string, at time.Time) (string, bool, error) {
	if len(activePeerIDs) > MaxCandidates || presenceID == "" {
		return "", false, ErrInvalidDiscovery
	}
	seen := make(map[string]struct{}, len(activePeerIDs))
	for _, peerID := range activePeerIDs {
		if peerID == "" {
			return "", false, ErrInvalidDiscovery
		}
		if _, duplicate := seen[peerID]; duplicate {
			continue
		}
		seen[peerID] = struct{}{}
		for offset := -1; offset <= 1; offset++ {
			coordinates, err := DeriveCoordinates(key, spaceID, peerID, at.Add(time.Duration(offset)*RotationInterval))
			if err != nil {
				return "", false, err
			}
			if hmac.Equal([]byte(coordinates.PresenceID), []byte(presenceID)) {
				return peerID, true, nil
			}
		}
	}
	return "", false, nil
}

func deriveOpaque(key []byte, domain string, parts ...any) string {
	mac := hmac.New(sha256.New, key)
	writePart(mac, []byte(domain))
	for _, part := range parts {
		switch value := part.(type) {
		case string:
			writePart(mac, []byte(value))
		case uint64:
			var encoded [8]byte
			binary.BigEndian.PutUint64(encoded[:], value)
			writePart(mac, encoded[:])
		case int64:
			var encoded [8]byte
			binary.BigEndian.PutUint64(encoded[:], uint64(value))
			writePart(mac, encoded[:])
		}
	}
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

type hashWriter interface {
	Write([]byte) (int, error)
}

func writePart(out hashWriter, value []byte) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	_, _ = out.Write(size[:])
	_, _ = out.Write(value)
}

// Reachability is an in-memory observation, never a membership or grant.
type Reachability struct {
	PeerID     string
	PresenceID string
	Role       rendezvous.Role
	ObservedAt time.Time
	ExpiresAt  time.Time
}

// Directory tracks ephemeral reachability independently of durable Peer
// Space state. Callers must resolve presence against active memberships first.
type Directory struct {
	mu      sync.Mutex
	byPeer  map[string]Reachability
	byRoute map[string]string
}

func NewDirectory() *Directory {
	return &Directory{byPeer: make(map[string]Reachability), byRoute: make(map[string]string)}
}

// Observe replaces an older route for the same active peer.
func (d *Directory) Observe(observation Reachability) error {
	if d == nil || observation.PeerID == "" || observation.PresenceID == "" || observation.ObservedAt.IsZero() ||
		observation.ExpiresAt.IsZero() || !observation.ExpiresAt.After(observation.ObservedAt) ||
		(observation.Role != rendezvous.RoleHost && observation.Role != rendezvous.RoleMember) {
		return ErrInvalidDiscovery
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if owner, exists := d.byRoute[observation.PresenceID]; exists && owner != observation.PeerID {
		return ErrInvalidDiscovery
	}
	if previous, exists := d.byPeer[observation.PeerID]; exists {
		if observation.ObservedAt.Before(previous.ObservedAt) {
			return nil
		}
		delete(d.byRoute, previous.PresenceID)
	}
	d.byPeer[observation.PeerID] = observation
	d.byRoute[observation.PresenceID] = observation.PeerID
	return nil
}

// RemovePresence removes only the route named by an offline event.
func (d *Directory) RemovePresence(presenceID string) {
	if d == nil || presenceID == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	peerID, exists := d.byRoute[presenceID]
	if !exists {
		return
	}
	delete(d.byRoute, presenceID)
	delete(d.byPeer, peerID)
}

// Expire removes stale routes and returns their peer IDs in stable order.
func (d *Directory) Expire(now time.Time) []string {
	if d == nil || now.IsZero() {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	expired := make([]string, 0)
	for peerID, observation := range d.byPeer {
		if now.Before(observation.ExpiresAt) {
			continue
		}
		delete(d.byPeer, peerID)
		delete(d.byRoute, observation.PresenceID)
		expired = append(expired, peerID)
	}
	sort.Strings(expired)
	return expired
}

// Snapshot returns a deterministic detached view.
func (d *Directory) Snapshot() []Reachability {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Reachability, 0, len(d.byPeer))
	for _, observation := range d.byPeer {
		out = append(out, observation)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PeerID < out[j].PeerID })
	return out
}

// SyncCandidate combines ephemeral routing with the last durable frontier
// acknowledged by the authenticated peer.
type SyncCandidate struct {
	PeerID         string
	PresenceID     string
	Acknowledged   configsync.VersionVector
	LastExchangeAt int64
}

type PlanRequest struct {
	LocalPeerID       string
	ActiveMemberCount int
	LocalVector       configsync.VersionVector
	Candidates        []SyncCandidate
	MaxFanout         int
	Slot              int64
}

type scoredCandidate struct {
	SyncCandidate
	lag uint64
}

// PlanSyncTargets selects every reachable remote in small Spaces. Larger
// Spaces prioritize peers missing more local operations and bound each round.
func PlanSyncTargets(request PlanRequest) ([]SyncCandidate, error) {
	if request.LocalPeerID == "" || request.ActiveMemberCount < 1 || len(request.LocalVector) > MaxVectorEntries ||
		len(request.Candidates) > MaxCandidates || request.ActiveMemberCount < len(request.Candidates)+1 {
		return nil, ErrInvalidDiscovery
	}
	maxFanout := request.MaxFanout
	if maxFanout == 0 {
		maxFanout = DefaultFanout
	}
	if maxFanout < 1 || maxFanout > MaxCandidates {
		return nil, ErrInvalidDiscovery
	}
	for actor := range request.LocalVector {
		if actor == "" {
			return nil, ErrInvalidDiscovery
		}
	}

	seenPeers := make(map[string]struct{}, len(request.Candidates))
	seenRoutes := make(map[string]struct{}, len(request.Candidates))
	scored := make([]scoredCandidate, 0, len(request.Candidates))
	for _, candidate := range request.Candidates {
		if candidate.PeerID == "" || candidate.PeerID == request.LocalPeerID || candidate.PresenceID == "" ||
			len(candidate.Acknowledged) > MaxVectorEntries {
			return nil, ErrInvalidDiscovery
		}
		if _, duplicate := seenPeers[candidate.PeerID]; duplicate {
			return nil, ErrInvalidDiscovery
		}
		if _, duplicate := seenRoutes[candidate.PresenceID]; duplicate {
			return nil, ErrInvalidDiscovery
		}
		seenPeers[candidate.PeerID] = struct{}{}
		seenRoutes[candidate.PresenceID] = struct{}{}
		lag := uint64(0)
		for actor, localCounter := range request.LocalVector {
			if actor == "" {
				return nil, ErrInvalidDiscovery
			}
			if acknowledged := candidate.Acknowledged[actor]; localCounter > acknowledged {
				lag = saturatingAdd(lag, localCounter-acknowledged)
			}
		}
		for actor := range candidate.Acknowledged {
			if actor == "" {
				return nil, ErrInvalidDiscovery
			}
		}
		scored = append(scored, scoredCandidate{SyncCandidate: cloneCandidate(candidate), lag: lag})
	}

	if request.ActiveMemberCount <= SmallSpaceLimit {
		sort.Slice(scored, func(i, j int) bool { return scored[i].PeerID < scored[j].PeerID })
		return unscore(scored), nil
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].lag != scored[j].lag {
			return scored[i].lag > scored[j].lag
		}
		if scored[i].LastExchangeAt != scored[j].LastExchangeAt {
			return scored[i].LastExchangeAt < scored[j].LastExchangeAt
		}
		return scored[i].PeerID < scored[j].PeerID
	})
	rotateEqualPriorityGroups(scored, request.LocalPeerID, request.Slot)
	if len(scored) > maxFanout {
		scored = scored[:maxFanout]
	}
	return unscore(scored), nil
}

func rotateEqualPriorityGroups(candidates []scoredCandidate, localPeerID string, slot int64) {
	for start := 0; start < len(candidates); {
		end := start + 1
		for end < len(candidates) && candidates[end].lag == candidates[start].lag &&
			candidates[end].LastExchangeAt == candidates[start].LastExchangeAt {
			end++
		}
		group := candidates[start:end]
		if len(group) > 1 {
			digest := sha256.Sum256([]byte(tieDomain + "\x00" + localPeerID))
			base := binary.BigEndian.Uint64(digest[:8]) % uint64(len(group))
			offset := int((base + uint64(slot%int64(len(group))+int64(len(group)))) % uint64(len(group)))
			rotated := append([]scoredCandidate(nil), group...)
			for index := range group {
				group[index] = rotated[(index+offset)%len(group)]
			}
		}
		start = end
	}
}

func saturatingAdd(left, right uint64) uint64 {
	if math.MaxUint64-left < right {
		return math.MaxUint64
	}
	return left + right
}

func cloneCandidate(candidate SyncCandidate) SyncCandidate {
	candidate.Acknowledged = candidate.Acknowledged.Clone()
	return candidate
}

func unscore(scored []scoredCandidate) []SyncCandidate {
	out := make([]SyncCandidate, len(scored))
	for index := range scored {
		out[index] = scored[index].SyncCandidate
	}
	return out
}
