package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func TestRemoteProxyWS_WaitsForUpstreamBeforeOpeningLocal(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		typ, data, err := c.Read(r.Context())
		if err == nil {
			_ = c.Write(r.Context(), typ, data)
		}
	}))
	defer relay.Close()
	defer close(release)
	p := newHTTPProxyFixture(t, relay)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	opened := make(chan *websocket.Conn, 1)
	failed := make(chan error, 1)
	go func() {
		c, _, err := websocket.Dial(ctx, p.wsURL()+"/client", &websocket.DialOptions{Subprotocols: []string{"atterm-token.local"}})
		if err != nil {
			failed <- err
			return
		}
		opened <- c
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("upstream dial did not start")
	}
	select {
	case c := <-opened:
		c.CloseNow()
		t.Fatal("local socket opened while upstream handshake was still pending")
	case err := <-failed:
		t.Fatalf("local dial failed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	release <- struct{}{}
	var c *websocket.Conn
	select {
	case c = <-opened:
	case err := <-failed:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal("local socket did not open after upstream")
	}
	defer c.CloseNow()
	if c.Subprotocol() != "atterm-token.local" {
		t.Fatalf("local subprotocol = %q", c.Subprotocol())
	}
	if err := c.Write(ctx, websocket.MessageBinary, []byte("frame")); err != nil {
		t.Fatal(err)
	}
	_, data, err := c.Read(ctx)
	if err != nil || string(data) != "frame" {
		t.Fatalf("forwarded frame = %q, err = %v", data, err)
	}
}

func TestRemoteProxyWS_UpstreamFailureDoesNotOpenLocal(t *testing.T) {
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer relay.Close()
	p := newHTTPProxyFixture(t, relay)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, p.wsURL()+"/client-sessions", nil)
	if c != nil {
		c.CloseNow()
	}
	if err == nil {
		t.Fatal("local socket opened despite upstream rejecting authentication")
	}
}
