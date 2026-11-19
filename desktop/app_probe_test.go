package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProbeRelayVersion_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/version" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"version":"v0.2.99"}`)
	}))
	defer srv.Close()

	app := &App{ctx: context.Background()}
	if err := app.ProbeRelayVersion(srv.URL, false); err != nil {
		t.Fatalf("ProbeRelayVersion: %v", err)
	}
	version, err := app.FetchRelayVersion(srv.URL, false)
	if err != nil || version != "v0.2.99" {
		t.Fatalf("FetchRelayVersion: version=%q err=%v", version, err)
	}
}

func TestProbeRelayVersion_AllowsSlowDNS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"version":"v0.2.99"}`)
	}))
	defer srv.Close()

	// Some routers take a full DNS retry interval before answering the
	// combined A/AAAA lookup. Reproduce that delay before TCP connects.
	original := http.DefaultTransport
	transport := original.(*http.Transport).Clone()
	dial := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		timer := time.NewTimer(5100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			return dial(ctx, network, addr)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	http.DefaultTransport = transport
	t.Cleanup(func() {
		http.DefaultTransport = original
		transport.CloseIdleConnections()
	})

	app := &App{ctx: context.Background()}
	if err := app.ProbeRelayVersion(srv.URL, false); err != nil {
		t.Fatalf("healthy relay rejected after slow DNS: %v", err)
	}
}

func TestProbeRelayVersion_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	app := &App{ctx: context.Background()}
	err := app.ProbeRelayVersion(srv.URL, false)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error; got %v", err)
	}
}

func TestProbeRelayVersion_NoVersionField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	app := &App{ctx: context.Background()}
	err := app.ProbeRelayVersion(srv.URL, false)
	if err == nil || !strings.Contains(err.Error(), "no version field") {
		t.Fatalf("expected 'no version field' error; got %v", err)
	}
}

func TestProbeRelayVersion_Unreachable(t *testing.T) {
	app := &App{ctx: context.Background()}
	err := app.ProbeRelayVersion("http://127.0.0.1:1", false)
	if err == nil || !strings.Contains(err.Error(), "connect") {
		t.Fatalf("expected connect error; got %v", err)
	}
}

func TestProbeRelayVersion_EmptyURL(t *testing.T) {
	app := &App{ctx: context.Background()}
	err := app.ProbeRelayVersion("", false)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty-url error; got %v", err)
	}
}

func TestProbeRelayVersion_RejectsPublicCleartextWithoutOptIn(t *testing.T) {
	app := &App{ctx: context.Background()}
	err := app.ProbeRelayVersion("http://relay.example.com", false)
	if err == nil || !strings.Contains(err.Error(), "insecure relay url") {
		t.Fatalf("expected insecure relay rejection before dialing; got %v", err)
	}
}
