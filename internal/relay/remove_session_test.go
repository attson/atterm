package relay

import (
	"context"
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/userstore"
	"github.com/google/uuid"
)

func TestRemoveSessionPrunesSeen(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	srv := &Server{registry: session.NewRegistry(), cfg: Config{Store: store}}

	id := uuid.New()
	sess := session.New(id, proto.SessionInfo{})
	sess.OwnerUserID = "userA"
	if _, err := srv.registry.Add(sess); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := store.SetSeen(ctx, "userA", []string{id.String()}, 123); err != nil {
		t.Fatalf("SetSeen: %v", err)
	}

	srv.removeSession(id)

	seen, _ := store.SeenAt(ctx, "userA")
	if _, ok := seen[id.String()]; ok {
		t.Fatalf("removeSession did not prune seen row")
	}
	if _, ok := srv.registry.Get(id); ok {
		t.Fatalf("removeSession did not remove from registry")
	}
}
