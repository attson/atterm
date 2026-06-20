package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
