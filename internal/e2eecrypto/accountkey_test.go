package e2eecrypto

import "testing"

func TestWrapUnwrap_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	wrap, err := WrapAccountKey("hunter2", key, DefaultKDFParams())
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if len(wrap.Wrapped) == 0 || len(wrap.Nonce) != 24 || len(wrap.Salt) != 16 {
		t.Fatalf("wrap envelope shape wrong: %+v", wrap)
	}
	got, err := UnwrapAccountKey("hunter2", wrap)
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	if string(got) != string(key) {
		t.Fatalf("round-trip mismatch")
	}
}

func TestUnwrap_WrongPassword(t *testing.T) {
	key := make([]byte, 32)
	wrap, _ := WrapAccountKey("hunter2", key, DefaultKDFParams())
	if _, err := UnwrapAccountKey("not-the-password", wrap); err == nil {
		t.Fatalf("expected error on wrong password, got nil")
	}
}
