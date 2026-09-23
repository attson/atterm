package peertransport

import (
	"errors"
	"testing"
)

func TestRouteTrackerRepeatedHandoverHasOneOutputAndInputRoute(t *testing.T) {
	tracker := NewRouteTracker(0)
	delivered := make(map[uint64]int)
	nextSeq := uint64(1)

	commit := func(generation uint64, route Route, seq uint64) {
		t.Helper()
		if tracker.AcceptOutput(generation, route, seq) {
			delivered[seq]++
		}
	}

	for cycle := 0; cycle < 100; cycle++ {
		generation, err := tracker.BeginDirect()
		if err != nil {
			t.Fatal(err)
		}
		if tracker.InputRoute() != RouteRelay {
			t.Fatal("direct setup changed input writer")
		}
		commit(generation, RouteRelay, nextSeq)
		nextSeq++
		if err := tracker.BeginDirectReplay(generation); err != nil {
			t.Fatal(err)
		}

		// Both paths may surface the overlap. Whichever arrives first commits;
		// the other is a duplicate and must not reach the terminal.
		commit(generation, RouteDirect, nextSeq)
		commit(generation, RouteRelay, nextSeq)
		nextSeq++
		if err := tracker.NoteDirectReady(generation, tracker.CommittedSeq()); err != nil {
			t.Fatal(err)
		}
		if err := tracker.ActivateDirect(generation); err != nil {
			t.Fatal(err)
		}
		if tracker.InputRoute() != RouteDirect {
			t.Fatal("direct active did not become sole input writer")
		}
		commit(generation, RouteRelay, nextSeq)
		commit(generation, RouteDirect, nextSeq)
		nextSeq++

		fallbackGeneration, err := tracker.DirectLost(generation)
		if err != nil {
			t.Fatal(err)
		}
		if tracker.InputRoute() != RouteNone {
			t.Fatal("input was not frozen during fallback")
		}
		commit(generation, RouteDirect, nextSeq)
		commit(fallbackGeneration, RouteRelay, nextSeq)
		nextSeq++
		if err := tracker.RelayAttached(fallbackGeneration); err != nil {
			t.Fatal(err)
		}
		if tracker.InputRoute() != RouteRelay {
			t.Fatal("Relay did not regain sole input writer")
		}
	}

	for seq := uint64(1); seq < nextSeq; seq++ {
		if delivered[seq] != 1 {
			t.Fatalf("seq %d delivered %d times", seq, delivered[seq])
		}
	}
}

func TestRouteTrackerRejectsStaleAndInvalidTransitions(t *testing.T) {
	tracker := NewRouteTracker(10)
	generation, _ := tracker.BeginDirect()
	if _, err := tracker.BeginDirect(); !errors.Is(err, ErrInvalidRouteTransition) {
		t.Fatalf("second attempt accepted: %v", err)
	}
	if err := tracker.BeginDirectReplay(generation + 1); !errors.Is(err, ErrInvalidRouteTransition) {
		t.Fatalf("stale generation accepted: %v", err)
	}
	if err := tracker.BeginDirectReplay(generation); err != nil {
		t.Fatal(err)
	}
	if err := tracker.NoteDirectReady(generation, 9); !errors.Is(err, ErrInvalidRouteTransition) {
		t.Fatalf("activation behind committed cursor accepted: %v", err)
	}
	if err := tracker.AbortDirect(generation); err != nil {
		t.Fatal(err)
	}
	if tracker.AcceptOutput(generation, RouteRelay, 11) {
		t.Fatal("stale generation output accepted")
	}
}

func TestRouteTrackerWaitsForDirectAfterRelayAdvancesPastReady(t *testing.T) {
	tracker := NewRouteTracker(10)
	generation, err := tracker.BeginDirect()
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.BeginDirectReplay(generation); err != nil {
		t.Fatal(err)
	}
	if !tracker.AcceptOutput(generation, RouteRelay, 12) {
		t.Fatal("Relay output was not committed")
	}
	if err := tracker.NoteDirectReady(generation, 10); err != nil {
		t.Fatal(err)
	}
	if tracker.CanActivateDirect(generation) {
		t.Fatal("direct activated before catching Relay cursor")
	}
	if tracker.AcceptOutput(generation, RouteDirect, 11) {
		t.Fatal("duplicate direct output was committed")
	}
	if tracker.CanActivateDirect(generation) {
		t.Fatal("direct activated one sequence behind Relay")
	}
	if tracker.AcceptOutput(generation, RouteDirect, 12) {
		t.Fatal("duplicate direct output was committed")
	}
	if !tracker.CanActivateDirect(generation) {
		t.Fatal("direct did not become activatable after catching Relay cursor")
	}
	if err := tracker.ActivateDirect(generation); err != nil {
		t.Fatal(err)
	}
}
