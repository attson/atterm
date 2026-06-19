package userstore

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// TestSetSecretCipher_ConcurrentSwap runs Feishu CRUD concurrently with
// cipher swaps. Under -race this proves the atomic pointer + load-once-local
// pattern never data-races or nil-derefs; individual ops may error (cipher
// nil), which is fine.
func TestSetSecretCipher_ConcurrentSwap(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	u, _ := s.CreateOpaqueUser(ctx, "race@example.com")
	c, _ := NewSecretCipher(testKey(t))
	s.SetSecretCipher(c)
	in := FeishuBindingCredentials{AppID: "a", AppSecret: "s", EncryptKey: "k", VerifyToken: "t"}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = s.UpsertFeishuBinding(ctx, u.ID, in)
				_, _ = s.GetFeishuBinding(ctx, u.ID)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			if j%2 == 0 {
				s.SetSecretCipher(nil)
			} else {
				s.SetSecretCipher(c)
			}
		}
	}()
	wg.Wait()
}

// TestSetSecretCipher_RuntimeLifecycle exercises attaching, clearing, and
// re-attaching the field cipher after Open — the mechanism that lets the relay
// toggle Feishu at runtime without a restart.
func TestSetSecretCipher_RuntimeLifecycle(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	u, _ := s.CreateOpaqueUser(ctx, "rt@example.com")
	in := FeishuBindingCredentials{AppID: "cli_rt", AppSecret: "s", EncryptKey: "k", VerifyToken: "t"}

	// No cipher → CRUD errors, HasSecretCipher false.
	if s.HasSecretCipher() {
		t.Fatal("HasSecretCipher = true before any cipher set")
	}
	if err := s.UpsertFeishuBinding(ctx, u.ID, in); err == nil || !strings.Contains(err.Error(), "cipher") {
		t.Fatalf("want cipher error, got %v", err)
	}

	// Attach a cipher → CRUD works and round-trips.
	c1, _ := NewSecretCipher(testKey(t))
	s.SetSecretCipher(c1)
	if !s.HasSecretCipher() {
		t.Fatal("HasSecretCipher = false after SetSecretCipher")
	}
	if err := s.UpsertFeishuBinding(ctx, u.ID, in); err != nil {
		t.Fatalf("upsert after enable: %v", err)
	}
	got, err := s.GetFeishuBinding(ctx, u.ID)
	if err != nil || got.AppSecret != in.AppSecret {
		t.Fatalf("round-trip failed: %+v err=%v", got, err)
	}

	// Clear the cipher → CRUD errors again.
	s.SetSecretCipher(nil)
	if _, err := s.GetFeishuBinding(ctx, u.ID); err == nil || !strings.Contains(err.Error(), "cipher") {
		t.Fatalf("want cipher error after clear, got %v", err)
	}

	// Re-attach the SAME cipher → existing row decrypts (proves disable/enable
	// toggle is non-destructive).
	s.SetSecretCipher(c1)
	if got, err := s.GetFeishuBinding(ctx, u.ID); err != nil || got.AppSecret != in.AppSecret {
		t.Fatalf("re-enable lost the row: %+v err=%v", got, err)
	}
}

// TestSetSecretCipher_CrossKeyCannotDecrypt justifies the admin API's rotation
// guard: a row written under one key is undecryptable under another.
func TestSetSecretCipher_CrossKeyCannotDecrypt(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	u, _ := s.CreateOpaqueUser(ctx, "rot@example.com")

	c1, _ := NewSecretCipher(testKey(t))
	s.SetSecretCipher(c1)
	if err := s.UpsertFeishuBinding(ctx, u.ID, FeishuBindingCredentials{AppID: "a", AppSecret: "s", EncryptKey: "k", VerifyToken: "t"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	c2, _ := NewSecretCipher(testKey(t))
	s.SetSecretCipher(c2)
	if _, err := s.GetFeishuBinding(ctx, u.ID); err == nil {
		t.Fatal("expected decrypt failure under a different key, got nil")
	}
}
