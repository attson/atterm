package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
)

// startPreviewFixture spins up a target HTTP server, a byte-pipe relay, an owner
// service host, and a running preview gateway, returning the manager + gateway
// URL. It mirrors TestServicePreviewLoopbackE2E's wiring.
func startPreviewFixture(t *testing.T, previews *servicePreviewManager) (string, func()) {
	t.Helper()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "preview-through-owner")
	}))
	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	portNumber, err := strconv.Atoi(targetURL.Port())
	if err != nil {
		t.Fatalf("target port: %q %v", targetURL.Port(), err)
	}

	pipe := newServicePipeFixture()
	relay := httptest.NewServer(http.HandlerFunc(pipe.serveHTTP))
	relayWS := "ws" + strings.TrimPrefix(relay.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	accountKey := bytes.Repeat([]byte{0x42}, e2eecrypto.SessionKeySize)
	sessionID := uuid.New()
	serviceID := uuid.New()

	model := session.New(sessionID, proto.SessionInfo{RemotePermission: proto.RemotePermissionFull})
	sessionKey, _ := e2eecrypto.DeriveSessionKey(accountKey, sessionID)
	fields, _ := json.Marshal(proto.SealedServiceOpenFields{Port: uint16(portNumber), Scheme: "http"})
	sealed, _ := e2eecrypto.SealUnsequenced(sessionKey, sessionID, byte(proto.TypeServiceOpen), fields)
	rawRequest, _ := json.Marshal(proto.ServiceOpenPayload{
		RequestID: "request-1", ServiceID: serviceID.String(), HostTicket: "host-ticket", Sealed: sealed,
	})
	hostOut := make(chan proto.Frame, 1)
	hosts := newServiceHostManager(ctx, relayWS, "token", false, func() []byte { return accountKey }, hostOut, proto.RemotePermissionFull)
	hosts.open(proto.Frame{Type: proto.TypeServiceOpen, SessionID: sessionID, Payload: rawRequest})
	select {
	case <-hostOut:
	case <-ctx.Done():
		t.Fatal("timeout waiting for owner host registration")
	}

	keys, _ := e2eecrypto.DeriveServiceKeys(accountKey, serviceID)
	local, err := previews.start(ctx, appConfig{RelayURL: relayWS, RelaySessionToken: "token"}, ServicePreviewStartRequest{
		ServiceID:       serviceID.String(),
		ClientTicket:    "client-ticket",
		ClientToHostKey: keys.ClientToHost,
		HostToClientKey: keys.HostToClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		previews.stopAll()
		hosts.close()
		model.Close()
		relay.Close()
		target.Close()
		cancel()
	}
	return local.URL, cleanup
}

// TestServicePreviewGatewaySurvivesPipeDeath is the core self-heal guarantee:
// when a mapping's pipe dies the gateway stays up on the same URL and serves a
// 503 (+Retry-After) rather than the 502 a closed proxy target used to yield.
func TestServicePreviewGatewaySurvivesPipeDeath(t *testing.T) {
	var previews servicePreviewManager
	previewURL, cleanup := startPreviewFixture(t, &previews)
	defer cleanup()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(previewURL)
	if err != nil {
		t.Fatalf("GET preview: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "preview-through-owner" {
		t.Fatalf("initial GET = %d %q", resp.StatusCode, body)
	}

	// Locate the gateway and kill its only pipe.
	previews.mu.Lock()
	var gateway *servicePreviewGateway
	for _, g := range previews.previews {
		gateway = g
	}
	previews.mu.Unlock()
	if gateway == nil || len(gateway.mappings) != 1 {
		t.Fatalf("expected one gateway with one mapping, got %+v", gateway)
	}
	mp := gateway.mappings[0]
	mp.mu.Lock()
	pipe := mp.pipe
	mp.mu.Unlock()
	pipe.cancel()

	waitFor(t, 3*time.Second, "mapping marked dead", func() bool {
		mp.mu.Lock()
		defer mp.mu.Unlock()
		return mp.dead
	})

	resp, err = client.Get(previewURL)
	if err != nil {
		t.Fatalf("GET after pipe death: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("dead-window status = %d, want 503", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") != "1" {
		t.Fatalf("Retry-After = %q, want 1", resp.Header.Get("Retry-After"))
	}
}

// TestServicePreviewPipeDeathEmitsEvent verifies the renderer gets told which
// mapping to reconnect.
func TestServicePreviewPipeDeathEmitsEvent(t *testing.T) {
	events := make(chan map[string]any, 4)
	var previews servicePreviewManager
	previews.emit = func(name string, data any) {
		if name == "service-preview:pipe-dead" {
			if m, ok := data.(map[string]any); ok {
				events <- m
			}
		}
	}
	_, cleanup := startPreviewFixture(t, &previews)
	defer cleanup()

	previews.mu.Lock()
	var gateway *servicePreviewGateway
	for _, g := range previews.previews {
		gateway = g
	}
	previews.mu.Unlock()
	mp := gateway.mappings[0]
	mp.mu.Lock()
	pipe := mp.pipe
	mp.mu.Unlock()
	pipe.cancel()

	select {
	case ev := <-events:
		if ev["gateway_id"] != gateway.id.String() {
			t.Fatalf("gateway_id = %v", ev["gateway_id"])
		}
		if idx, _ := ev["mapping_index"].(int); idx != 0 {
			t.Fatalf("mapping_index = %v", ev["mapping_index"])
		}
		if _, ok := ev["service_id"].(string); !ok {
			t.Fatalf("missing service_id: %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pipe-dead event not emitted")
	}
}

func TestServicePreviewRebindRejectsUnknownGateway(t *testing.T) {
	var previews servicePreviewManager
	err := previews.rebind(appConfig{RelayURL: "ws://127.0.0.1:1", RelaySessionToken: "t"}, uuid.New(), ServicePreviewRebindRequest{})
	if err == nil || err.Error() != "preview_gone" {
		t.Fatalf("rebind unknown gateway = %v, want preview_gone", err)
	}
}

// TestServicePreviewEnqueueTimesOut proves a wedged send buffer no longer nukes
// the pipe instantly (transient burst) but does tear down after the timeout.
func TestServicePreviewEnqueueTimesOut(t *testing.T) {
	old := serviceEnqueueWait
	serviceEnqueueWait = 40 * time.Millisecond
	defer func() { serviceEnqueueWait = old }()

	p := newServicePreview(context.Background(), nil, nil, uuid.New())
	defer p.cancel()
	for i := 0; i < serviceHostQueueDepth; i++ {
		p.send <- serviceHostMessage{}
	}
	start := time.Now()
	if p.enqueue(serviceHostMessage{}) {
		t.Fatal("enqueue on a full buffer should return false")
	}
	if elapsed := time.Since(start); elapsed < serviceEnqueueWait {
		t.Fatalf("enqueue returned after %v, expected to block ~%v", elapsed, serviceEnqueueWait)
	}
	if p.ctx.Err() == nil {
		t.Fatal("timed-out enqueue should cancel the pipe")
	}
}
