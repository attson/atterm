package session

import (
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
