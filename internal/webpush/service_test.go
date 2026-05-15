package webpush

import (
	"testing"
)

func TestOpenFreshDirGeneratesKeysAndReturnsService(t *testing.T) {
	dir := t.TempDir()
	svc, err := Open(dir, "mailto:test@example.com")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if svc == nil {
		t.Fatal("Open returned nil service")
	}
	if svc.PublicKey() == "" {
		t.Fatal("PublicKey empty")
	}
}

func TestOpenLoadsExistingState(t *testing.T) {
	dir := t.TempDir()
	first, err := Open(dir, "mailto:test@example.com")
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	pubKey := first.PublicKey()
	second, err := Open(dir, "mailto:test@example.com")
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	if second.PublicKey() != pubKey {
		t.Fatalf("second PublicKey changed (regenerated?); first=%q second=%q", pubKey, second.PublicKey())
	}
}

func TestOpenEmptyDirReturnsInMemoryService(t *testing.T) {
	svc, err := Open("", "mailto:test@example.com")
	if err != nil {
		t.Fatalf("Open(empty dir): %v", err)
	}
	if svc == nil {
		t.Fatal("Open(empty dir) returned nil; expected in-memory service")
	}
	if svc.PublicKey() == "" {
		t.Fatal("in-memory service has no VAPID public key")
	}
}

func TestServiceAddAndRemoveSubscriptionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	svc, _ := Open(dir, "mailto:test@example.com")
	sub := Subscription{Endpoint: "https://push.example/abc"}
	sub.Keys.P256dh = "p"
	sub.Keys.Auth = "a"
	if err := svc.AddSubscription("tokhash", sub); err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	// New Open should see the persisted subscription.
	svc2, _ := Open(dir, "mailto:test@example.com")
	if got := svc2.subStore.ByToken("tokhash"); len(got) != 1 || got[0].Endpoint != sub.Endpoint {
		t.Fatalf("persisted subs not loaded; got %v", got)
	}
	if err := svc.RemoveSubscription("tokhash", sub.Endpoint); err != nil {
		t.Fatalf("RemoveSubscription: %v", err)
	}
	svc3, _ := Open(dir, "mailto:test@example.com")
	if got := svc3.subStore.ByToken("tokhash"); len(got) != 0 {
		t.Fatalf("subs not removed after persist; got %v", got)
	}
}
