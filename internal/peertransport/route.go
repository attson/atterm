package peertransport

import (
	"errors"
	"fmt"
)

var ErrInvalidRouteTransition = errors.New("peertransport: invalid route transition")

// Route identifies the terminal byte path, not the trust principal.
type Route byte

const (
	RouteNone   Route = 0
	RouteRelay  Route = 1
	RouteDirect Route = 2
)

// RouteState is the client-side handover phase for one session_id.
type RouteState byte

const (
	RouteRelayAttached RouteState = iota + 1
	RouteDirectConnecting
	RouteDirectReplay
	RouteDirectActive
	RouteRelayReattaching
)

// RouteTracker models one output cursor and one input writer across route
// overlap. Network adapters attach the returned generation to their callbacks
// so late frames from closed routes cannot mutate current state.
type RouteTracker struct {
	state             RouteState
	generation        uint64
	committedSeq      uint64
	directStartSeq    uint64
	directObservedSeq uint64
	directReady       bool
}

// NewRouteTracker starts from an attached Relay path and its committed cursor.
func NewRouteTracker(committedSeq uint64) *RouteTracker {
	return &RouteTracker{state: RouteRelayAttached, generation: 1, committedSeq: committedSeq}
}

func (r *RouteTracker) State() RouteState { return r.state }

func (r *RouteTracker) Generation() uint64 { return r.generation }

func (r *RouteTracker) CommittedSeq() uint64 { return r.committedSeq }

// BeginDirect starts an opportunistic attempt while Relay remains the writer.
func (r *RouteTracker) BeginDirect() (uint64, error) {
	if r.state != RouteRelayAttached {
		return r.generation, fmt.Errorf("%w: begin direct from %d", ErrInvalidRouteTransition, r.state)
	}
	r.state = RouteDirectConnecting
	r.directStartSeq = r.committedSeq
	r.directObservedSeq = r.committedSeq
	r.directReady = false
	return r.generation, nil
}

// BeginDirectReplay admits output from both ordered routes. Input stays Relay.
func (r *RouteTracker) BeginDirectReplay(generation uint64) error {
	if generation != r.generation || r.state != RouteDirectConnecting {
		return fmt.Errorf("%w: begin replay generation=%d state=%d", ErrInvalidRouteTransition, generation, r.state)
	}
	r.state = RouteDirectReplay
	return nil
}

// NoteDirectReady records the end of the direct subscriber's initial replay.
// Relay may already have advanced committedSeq, so activation can still wait
// for ordered live direct frames to catch that cursor.
func (r *RouteTracker) NoteDirectReady(generation, replayedSeq uint64) error {
	if generation != r.generation || r.state != RouteDirectReplay || replayedSeq < r.directStartSeq {
		return fmt.Errorf("%w: ready generation=%d replayed=%d start=%d state=%d",
			ErrInvalidRouteTransition, generation, replayedSeq, r.directStartSeq, r.state)
	}
	if replayedSeq > r.directObservedSeq {
		r.directObservedSeq = replayedSeq
	}
	r.directReady = true
	return nil
}

// CanActivateDirect reports whether direct has observed every OUT sequence
// already committed from Relay after DIRECT_READY.
func (r *RouteTracker) CanActivateDirect(generation uint64) bool {
	return generation == r.generation && r.state == RouteDirectReplay && r.directReady && r.directObservedSeq >= r.committedSeq
}

// ActivateDirect switches the sole input writer after direct catch-up.
func (r *RouteTracker) ActivateDirect(generation uint64) error {
	if !r.CanActivateDirect(generation) {
		return fmt.Errorf("%w: activate generation=%d direct_observed=%d committed=%d ready=%t state=%d",
			ErrInvalidRouteTransition, generation, r.directObservedSeq, r.committedSeq, r.directReady, r.state)
	}
	r.state = RouteDirectActive
	return nil
}

// AbortDirect keeps Relay active when setup fails before the switch.
func (r *RouteTracker) AbortDirect(generation uint64) error {
	if generation != r.generation || (r.state != RouteDirectConnecting && r.state != RouteDirectReplay) {
		return fmt.Errorf("%w: abort generation=%d state=%d", ErrInvalidRouteTransition, generation, r.state)
	}
	r.generation++
	r.state = RouteRelayAttached
	return nil
}

// DirectLost invalidates old callbacks and freezes input until Relay attach is
// confirmed. The new generation must be used for the fallback connection.
func (r *RouteTracker) DirectLost(generation uint64) (uint64, error) {
	if generation != r.generation || r.state != RouteDirectActive {
		return r.generation, fmt.Errorf("%w: direct lost generation=%d state=%d", ErrInvalidRouteTransition, generation, r.state)
	}
	r.generation++
	r.state = RouteRelayReattaching
	return r.generation, nil
}

// RelayAttached completes fallback from the current committed OUT cursor.
func (r *RouteTracker) RelayAttached(generation uint64) error {
	if generation != r.generation || r.state != RouteRelayReattaching {
		return fmt.Errorf("%w: relay attached generation=%d state=%d", ErrInvalidRouteTransition, generation, r.state)
	}
	r.state = RouteRelayAttached
	return nil
}

// InputRoute returns the only route currently allowed to send input/control.
// RouteNone deliberately freezes input during ambiguous fallback ownership.
func (r *RouteTracker) InputRoute() Route {
	switch r.state {
	case RouteRelayAttached, RouteDirectConnecting, RouteDirectReplay:
		return RouteRelay
	case RouteDirectActive:
		return RouteDirect
	default:
		return RouteNone
	}
}

// AcceptOutput commits a new OUT sequence exactly once. false means a stale
// route, inactive route, or already committed duplicate and is safe to drop.
func (r *RouteTracker) AcceptOutput(generation uint64, route Route, seq uint64) bool {
	if generation != r.generation || seq == 0 {
		return false
	}
	allowed := false
	switch r.state {
	case RouteRelayAttached, RouteDirectConnecting, RouteRelayReattaching:
		allowed = route == RouteRelay
	case RouteDirectReplay:
		allowed = route == RouteRelay || route == RouteDirect
	case RouteDirectActive:
		allowed = route == RouteDirect
	}
	if !allowed {
		return false
	}
	if route == RouteDirect && r.state == RouteDirectReplay && seq > r.directObservedSeq {
		r.directObservedSeq = seq
	}
	if seq <= r.committedSeq {
		return false
	}
	r.committedSeq = seq
	return true
}
