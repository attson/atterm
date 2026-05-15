package session

import (
	"errors"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestRegistrySubscribeChangesReceivesAddRemoveAndNotify(t *testing.T) {
	reg := NewRegistry()
	sub := reg.SubscribeChanges()
	defer sub.Close()

	id := uuid.New()
	reg.Add(New(id, proto.SessionInfo{Command: "bash"}))
	requireRegistryChange(t, sub.C())

	reg.NotifyChange()
	requireRegistryChange(t, sub.C())

	reg.Remove(id)
	requireRegistryChange(t, sub.C())
}

func TestRegistrySubscribeChangesIsNonBlockingForSlowSubscribers(t *testing.T) {
	reg := NewRegistry()
	sub := reg.SubscribeChanges()
	defer sub.Close()

	for i := 0; i < 100; i++ {
		reg.NotifyChange()
	}
	requireRegistryChange(t, sub.C())
}

func requireRegistryChange(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for registry change")
	}
}

// TestRegisterSession_AssignsOwner: registering a new session with OwnerUserID
// "u1" must persist so a subsequent Get returns the same owner.
func TestRegisterSession_AssignsOwner(t *testing.T) {
	reg := NewRegistry()
	id := uuid.New()
	sess := New(id, proto.SessionInfo{Command: "bash"})
	sess.OwnerUserID = "u1"

	_, err := reg.Add(sess)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	got, ok := reg.Get(id)
	if !ok {
		t.Fatal("Get: session not found")
	}
	if got.OwnerUserID != "u1" {
		t.Fatalf("OwnerUserID = %q; want %q", got.OwnerUserID, "u1")
	}
}

// TestRegisterSession_DuplicateIDSameOwnerSucceeds: re-publishing with the
// same session id and same non-empty OwnerUserID must succeed (idempotent
// re-publish) and return the existing session entry.
func TestRegisterSession_DuplicateIDSameOwnerSucceeds(t *testing.T) {
	reg := NewRegistry()
	id := uuid.New()

	sess1 := New(id, proto.SessionInfo{Command: "bash"})
	sess1.OwnerUserID = "u1"
	existing, err := reg.Add(sess1)
	if err != nil {
		t.Fatalf("first Add failed: %v", err)
	}

	sess2 := New(id, proto.SessionInfo{Command: "bash"})
	sess2.OwnerUserID = "u1"
	got, err := reg.Add(sess2)
	if err != nil {
		t.Fatalf("second Add (same owner) failed: %v", err)
	}
	// Must return the existing entry, not the newly constructed session.
	if got != existing {
		t.Fatalf("Add returned new session instead of existing; want idempotent return of first")
	}
}

// TestRegisterSession_DuplicateIDDifferentOwnerRejected: re-publishing with
// the same session id but a different non-empty OwnerUserID must return
// ErrSessionOwnerMismatch and leave the original session intact.
func TestRegisterSession_DuplicateIDDifferentOwnerRejected(t *testing.T) {
	reg := NewRegistry()
	id := uuid.New()

	sess1 := New(id, proto.SessionInfo{Command: "bash"})
	sess1.OwnerUserID = "u1"
	original, err := reg.Add(sess1)
	if err != nil {
		t.Fatalf("first Add failed: %v", err)
	}

	sess2 := New(id, proto.SessionInfo{Command: "bash"})
	sess2.OwnerUserID = "u2"
	_, err = reg.Add(sess2)
	if !errors.Is(err, ErrSessionOwnerMismatch) {
		t.Fatalf("Add returned %v; want ErrSessionOwnerMismatch", err)
	}

	// Original session must remain in the registry and be open.
	inReg, ok := reg.Get(id)
	if !ok {
		t.Fatal("original session was removed from registry")
	}
	if inReg != original {
		t.Fatal("registry now holds a different session after rejected re-publish")
	}
	if inReg.IsClosed() {
		t.Fatal("original session was closed after rejected re-publish")
	}
}
