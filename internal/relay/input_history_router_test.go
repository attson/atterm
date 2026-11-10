package relay

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestInputHistoryRouterTargetsOnlyOriginalRequester(t *testing.T) {
	router := newInputHistoryRouter()
	sessionID := uuid.New()
	first := make(chan proto.Frame, 1)
	second := make(chan proto.Frame, 1)

	if !router.register(sessionID, "request-1", first) {
		t.Fatal("first route was not registered")
	}
	if router.register(sessionID, "request-1", second) {
		t.Fatal("duplicate route was registered")
	}
	payload, _ := json.Marshal(proto.InputHistoryResponsePayload{
		RequestID: "request-1",
		OK:        true,
		Sealed:    []byte("opaque"),
	})
	frame := proto.Frame{Type: proto.TypeInputHistoryResponse, SessionID: sessionID, Payload: payload}
	if !router.route(frame) {
		t.Fatal("matching response was not routed")
	}
	select {
	case got := <-first:
		if string(got.Payload) != string(payload) {
			t.Fatalf("routed payload = %q", got.Payload)
		}
	default:
		t.Fatal("original requester received no response")
	}
	select {
	case <-second:
		t.Fatal("other requester received the response")
	default:
	}
	if router.route(frame) {
		t.Fatal("completed route accepted a replay")
	}
}

func TestInputHistoryRouterConcurrentDuplicateHasOneWinner(t *testing.T) {
	router := newInputHistoryRouter()
	sessionID := uuid.New()
	const contenders = 16
	start := make(chan struct{})
	winners := make(chan chan proto.Frame, contenders)
	outputs := make([]chan proto.Frame, contenders)
	var wg sync.WaitGroup
	for i := range outputs {
		outputs[i] = make(chan proto.Frame, 1)
		wg.Add(1)
		go func(out chan proto.Frame) {
			defer wg.Done()
			<-start
			if router.register(sessionID, "same-request", out) {
				winners <- out
			}
		}(outputs[i])
	}
	close(start)
	wg.Wait()
	close(winners)

	var winner chan proto.Frame
	for out := range winners {
		if winner != nil {
			t.Fatal("duplicate request had more than one winning route")
		}
		winner = out
	}
	if winner == nil {
		t.Fatal("duplicate request had no winning route")
	}
	payload, _ := json.Marshal(proto.InputHistoryResponsePayload{RequestID: "same-request", OK: true})
	if !router.route(proto.Frame{Type: proto.TypeInputHistoryResponse, SessionID: sessionID, Payload: payload}) {
		t.Fatal("winning route did not receive the response")
	}
	select {
	case <-winner:
	default:
		t.Fatal("winning requester received no response")
	}
	for _, out := range outputs {
		if out == winner {
			continue
		}
		select {
		case <-out:
			t.Fatal("losing requester received the response")
		default:
		}
	}
}

func TestInputHistoryRouterConcurrentIndependentRequestsAllComplete(t *testing.T) {
	router := newInputHistoryRouter()
	sessionID := uuid.New()
	const requests = 16
	start := make(chan struct{})
	outputs := make([]chan proto.Frame, requests)
	var wg sync.WaitGroup
	for i := range outputs {
		outputs[i] = make(chan proto.Frame, 1)
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			requestID := fmt.Sprintf("request-%d", index)
			if !router.register(sessionID, requestID, outputs[index]) {
				t.Errorf("register %s failed", requestID)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	for i, out := range outputs {
		requestID := fmt.Sprintf("request-%d", i)
		payload, _ := json.Marshal(proto.InputHistoryResponsePayload{RequestID: requestID, OK: true})
		if !router.route(proto.Frame{Type: proto.TypeInputHistoryResponse, SessionID: sessionID, Payload: payload}) {
			t.Fatalf("route %s failed", requestID)
		}
		select {
		case <-out:
		default:
			t.Fatalf("request %s received no response", requestID)
		}
	}
}

func TestInputHistoryRouterReapsExpiredRequests(t *testing.T) {
	router := newInputHistoryRouter()
	sessionID := uuid.New()
	out := make(chan proto.Frame, 1)
	if !router.register(sessionID, "expired", out) {
		t.Fatal("route was not registered")
	}

	key := inputHistoryRouteKey{sessionID: sessionID, requestID: "expired"}
	router.mu.Lock()
	route := router.routes[key]
	route.registeredAt = time.Now().Add(-inputHistoryRequestTTL)
	router.routes[key] = route
	router.mu.Unlock()

	if !router.register(uuid.New(), "fresh", out) {
		t.Fatal("fresh route was not registered")
	}
	if router.has(sessionID, "expired") {
		t.Fatal("expired route survived the next registration sweep")
	}
}

func TestInputHistoryRouterUnregistersClientAndSession(t *testing.T) {
	router := newInputHistoryRouter()
	firstSession := uuid.New()
	secondSession := uuid.New()
	out := make(chan proto.Frame, 2)
	router.register(firstSession, "first", out)
	router.register(secondSession, "second", out)

	router.unregisterSession(firstSession)
	if router.has(firstSession, "first") {
		t.Fatal("session route survived session cleanup")
	}
	router.unregisterClient(out)
	if router.has(secondSession, "second") {
		t.Fatal("client route survived disconnect cleanup")
	}
}
