package webpush

import "testing"

func TestSubStoreAddNewEndpoint(t *testing.T) {
	s := newSubStore()
	sub := Subscription{Endpoint: "https://push.example/abc"}
	sub.Keys.P256dh = "p"
	sub.Keys.Auth = "a"
	if err := s.Add("tok1", sub); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got := s.ByToken("tok1")
	if len(got) != 1 {
		t.Fatalf("len after Add = %d; want 1", len(got))
	}
	if got[0].Endpoint != sub.Endpoint {
		t.Fatalf("Endpoint = %q; want %q", got[0].Endpoint, sub.Endpoint)
	}
}

func TestSubStoreAddOverwritesSameEndpoint(t *testing.T) {
	s := newSubStore()
	sub := Subscription{Endpoint: "https://push.example/abc"}
	sub.Keys.P256dh = "p"
	sub.Keys.Auth = "a"
	sub.CreatedAt = 100
	_ = s.Add("tok1", sub)
	sub.CreatedAt = 200
	_ = s.Add("tok1", sub)
	got := s.ByToken("tok1")
	if len(got) != 1 {
		t.Fatalf("len after re-Add = %d; want 1", len(got))
	}
	if got[0].CreatedAt != 200 {
		t.Fatalf("CreatedAt = %d; want 200 (refresh)", got[0].CreatedAt)
	}
}

func TestSubStoreCapAt16PerToken(t *testing.T) {
	s := newSubStore()
	for i := 0; i < maxSubsPerToken; i++ {
		sub := Subscription{Endpoint: "https://push.example/" + intToStr(i)}
		if err := s.Add("tok1", sub); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	if len(s.ByToken("tok1")) != maxSubsPerToken {
		t.Fatalf("ByToken pre-cap = %d; want %d", len(s.ByToken("tok1")), maxSubsPerToken)
	}
	overflow := Subscription{Endpoint: "https://push.example/overflow"}
	if err := s.Add("tok1", overflow); err != nil {
		t.Fatalf("Add overflow: %v", err)
	}
	if len(s.ByToken("tok1")) != maxSubsPerToken {
		t.Fatalf("ByToken post-overflow = %d; want %d (drop silently)", len(s.ByToken("tok1")), maxSubsPerToken)
	}
}

func TestSubStoreRemoveIsIdempotent(t *testing.T) {
	s := newSubStore()
	sub := Subscription{Endpoint: "https://push.example/abc"}
	_ = s.Add("tok1", sub)
	if !s.Remove("tok1", sub.Endpoint) {
		t.Fatal("Remove(existing) returned false")
	}
	if s.Remove("tok1", sub.Endpoint) {
		t.Fatal("Remove(nonexistent) returned true")
	}
	if len(s.ByToken("tok1")) != 0 {
		t.Fatal("subs not empty after Remove")
	}
}

func TestSubStoreRemoveUnknownTokenIsNoop(t *testing.T) {
	s := newSubStore()
	if s.Remove("nonexistent", "https://push.example/x") {
		t.Fatal("Remove unknown token returned true")
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
