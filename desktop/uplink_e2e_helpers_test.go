package main

import (
	"context"
	"net"
	"net/http"
	"testing"

	"github.com/attson/atterm/internal/relay"
	"github.com/attson/atterm/internal/userstore"
)

// startE2ERemoteRelay spins up a relay.Server backed by an in-memory userstore
// with one pre-created user + session, listens on 127.0.0.1:0, and returns the
// addr and the plaintext session token. The HTTP server and store are torn
// down when the test ends.
//
// All four desktop uplink e2e tests previously used a hardcoded "rt" Bearer
// (the legacy share-secret); Phase 1 of the auth migration deleted share-secret,
// so the remote relay now requires a real userstore session for every request.
// This helper centralizes the per-test fixture.
func startE2ERemoteRelay(t *testing.T) (addr, sessionToken string) {
	t.Helper()
	store := userstore.NewInMemory(t)
	ctx := context.Background()
	u, err := store.CreateUser(ctx, "e2e@example.com", "Correct-Horse-Battery-Staple-1!")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	tok, _, err := store.CreateSession(ctx, u.ID, "e2e-desktop", "127.0.0.1", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	srv := relay.NewServer(relay.Config{
		Store:    store,
		Resolver: relay.NewIdentityResolver(store),
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	hsrv := &http.Server{Handler: srv}
	go func() { _ = hsrv.Serve(ln) }()
	t.Cleanup(func() { _ = hsrv.Close() })
	return ln.Addr().String(), tok
}
