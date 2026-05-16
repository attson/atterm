package webpush

import "testing"

func TestSubStore_AddRemoveByUser(t *testing.T) {
	s := newSubStore()
	sub := Subscription{Endpoint: "https://push.example/abc"}
	sub.Keys.P256dh = "p"
	sub.Keys.Auth = "a"
	if err := s.Add("user1", sub); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got := s.ByUser("user1")
	if len(got) != 1 {
		t.Fatalf("len after Add = %d; want 1", len(got))
	}
	if got[0].Endpoint != sub.Endpoint {
		t.Fatalf("Endpoint = %q; want %q", got[0].Endpoint, sub.Endpoint)
	}
	if !s.Remove("user1", sub.Endpoint) {
		t.Fatal("Remove(existing) returned false")
	}
	if s.Remove("user1", sub.Endpoint) {
		t.Fatal("Remove(nonexistent) returned true")
	}
	if len(s.ByUser("user1")) != 0 {
		t.Fatal("subs not empty after Remove")
	}
}

func TestSubStoreAddOverwritesSameEndpoint(t *testing.T) {
	s := newSubStore()
	sub := Subscription{Endpoint: "https://push.example/abc"}
	sub.Keys.P256dh = "p"
	sub.Keys.Auth = "a"
	sub.CreatedAt = 100
	_ = s.Add("user1", sub)
	sub.CreatedAt = 200
	_ = s.Add("user1", sub)
	got := s.ByUser("user1")
	if len(got) != 1 {
		t.Fatalf("len after re-Add = %d; want 1", len(got))
	}
	if got[0].CreatedAt != 200 {
		t.Fatalf("CreatedAt = %d; want 200 (refresh)", got[0].CreatedAt)
	}
}

func TestSubStore_CapPerUser(t *testing.T) {
	s := newSubStore()
	for i := 0; i < maxSubsPerUser; i++ {
		sub := Subscription{Endpoint: "https://push.example/" + intToStr(i)}
		if err := s.Add("user1", sub); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	if len(s.ByUser("user1")) != maxSubsPerUser {
		t.Fatalf("ByUser pre-cap = %d; want %d", len(s.ByUser("user1")), maxSubsPerUser)
	}
	overflow := Subscription{Endpoint: "https://push.example/overflow"}
	if err := s.Add("user1", overflow); err != nil {
		t.Fatalf("Add overflow: %v", err)
	}
	if len(s.ByUser("user1")) != maxSubsPerUser {
		t.Fatalf("ByUser post-overflow = %d; want %d (drop silently)", len(s.ByUser("user1")), maxSubsPerUser)
	}
}

func TestSubStoreRemoveUnknownUserIsNoop(t *testing.T) {
	s := newSubStore()
	if s.Remove("nonexistent", "https://push.example/x") {
		t.Fatal("Remove unknown user returned true")
	}
}

func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
