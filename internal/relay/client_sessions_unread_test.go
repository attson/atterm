package relay

import (
	"context"
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/userstore"
	"github.com/google/uuid"
)

func TestUnreadComputation(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	srv := &Server{registry: session.NewRegistry(), cfg: Config{Store: store}}

	id := uuid.New()
	sess := session.New(id, proto.SessionInfo{
		Type:        session.SessionTypeAI,
		TaskState:   proto.TaskStateCompleted,
		AttentionAt: 1000,
	})
	sess.OwnerUserID = "userA"
	if _, err := srv.registry.Add(sess); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// No seen row, no subscriber -> unread.
	seen, _ := store.SeenAt(ctx, "userA")
	infos := srv.sessionInfoListForOwner("userA", seen)
	if len(infos) != 1 || !infos[0].Unread {
		t.Fatalf("expected unread, got %+v", infos)
	}

	// Mark seen at >= attention_at -> read.
	if err := store.SetSeen(ctx, "userA", []string{id.String()}, 1000); err != nil {
		t.Fatalf("SetSeen: %v", err)
	}
	seen, _ = store.SeenAt(ctx, "userA")
	infos = srv.sessionInfoListForOwner("userA", seen)
	if infos[0].Unread {
		t.Fatalf("expected read after SetSeen, got unread")
	}
}
