package relay

import (
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
)

func TestWebPushSessionResolverReturnsAuthorizedTokenHashes(t *testing.T) {
	srv := NewServer(Config{
		Token:          "write-token",
		ReadOnlyTokens: []string{"read-token"},
	})
	// Register a session with remote_permission = full.
	sid := uuid.New()
	info := proto.SessionInfo{
		Command:          "bash",
		HostID:           uuid.New().String(),
		RemotePermission: proto.RemotePermissionFull,
	}
	srv.Registry().Add(session.New(sid, info))
	defer srv.Registry().Remove(sid)
	got := srv.WebPushSessionResolver(sid)
	wantWrite := tokenHash("write-token")
	wantRead := tokenHash("read-token")
	if !containsString(got, wantWrite) {
		t.Errorf("missing write tokenHash; got %v want contains %v", got, wantWrite)
	}
	if !containsString(got, wantRead) {
		t.Errorf("missing read tokenHash; got %v want contains %v", got, wantRead)
	}
}

func TestWebPushSessionResolverEmptyForUnknownSession(t *testing.T) {
	srv := NewServer(Config{Token: "write-token"})
	got := srv.WebPushSessionResolver(uuid.New())
	if len(got) != 0 {
		t.Fatalf("WebPushSessionResolver(unknown) = %v; want empty", got)
	}
}

func TestWebPushSessionResolverSkipsReadTokenForViewOnlyRemotePermission(t *testing.T) {
	srv := NewServer(Config{
		Token:          "write-token",
		ReadOnlyTokens: []string{"read-token"},
	})
	sid := uuid.New()
	// remote_permission view: both read and write tokens can view.
	info := proto.SessionInfo{
		HostID:           uuid.New().String(),
		RemotePermission: proto.RemotePermissionView,
	}
	srv.Registry().Add(session.New(sid, info))
	defer srv.Registry().Remove(sid)
	got := srv.WebPushSessionResolver(sid)
	if !containsString(got, tokenHash("write-token")) || !containsString(got, tokenHash("read-token")) {
		t.Fatalf("expected both tokens for view permission; got %v", got)
	}
}

func TestWebPushSessionResolverIncludesAdminManagedReadOnlyHashes(t *testing.T) {
	// Compute the hash the subscribe handler would store for "ro-admin-token".
	expectedHash := tokenHash("ro-admin-token")
	// And the canonical admin-stored form (with prefix).
	prefixed := "sha256:" + expectedHash
	srv := NewServer(Config{
		Token:               "write-token",
		ReadOnlyTokenHashes: []string{prefixed},
	})
	sid := uuid.New()
	info := proto.SessionInfo{
		HostID:           uuid.New().String(),
		RemotePermission: proto.RemotePermissionFull,
	}
	srv.Registry().Add(session.New(sid, info))
	defer srv.Registry().Remove(sid)
	got := srv.WebPushSessionResolver(sid)
	if !containsString(got, expectedHash) {
		t.Fatalf("resolver missing admin RO tokenHash; got %v", got)
	}
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
