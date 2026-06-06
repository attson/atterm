package userstore

import (
	"context"
	"testing"
)

func TestSessionSeen(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	if err := s.SetSeen(ctx, "userA", []string{"sess1", "sess2"}, 100); err != nil {
		t.Fatalf("SetSeen: %v", err)
	}
	if err := s.SetSeen(ctx, "userA", []string{"sess1"}, 200); err != nil {
		t.Fatalf("SetSeen upsert: %v", err)
	}
	got, err := s.SeenAt(ctx, "userA")
	if err != nil {
		t.Fatalf("SeenAt: %v", err)
	}
	if got["sess1"] != 200 || got["sess2"] != 100 {
		t.Fatalf("unexpected seen map: %+v", got)
	}
	other, _ := s.SeenAt(ctx, "userB")
	if len(other) != 0 {
		t.Fatalf("cross-user leak: %+v", other)
	}
	_ = s.SetSeen(ctx, "userB", []string{"sess1"}, 50)
	if err := s.PruneSeenSession(ctx, "sess1"); err != nil {
		t.Fatalf("PruneSeenSession: %v", err)
	}
	a, _ := s.SeenAt(ctx, "userA")
	if _, ok := a["sess1"]; ok {
		t.Fatalf("sess1 not pruned for userA")
	}
	if a["sess2"] != 100 {
		t.Fatalf("prune removed too much: %+v", a)
	}
}
