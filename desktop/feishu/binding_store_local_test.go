// desktop/feishu/binding_store_local_test.go
package feishu

import (
	"context"
	"errors"
	"testing"

	"github.com/attson/atterm/internal/safekeyring"
	"github.com/zalando/go-keyring"
)

// TestLocalKeychainBindingStore_GetDegradedKeychain locks the fix for the
// "飞书集成尚未启用" bug: on an unsigned/ad-hoc build the OS keychain helper is
// SIGKILL'd ("signal: killed"). The store must fall back to the 0600 file via
// safekeyring and report ErrLocalBindingNotFound — NOT a wrapped error — so
// GetFeishuStatus reports the integration as enabled-but-unbound instead of
// failing the status fetch (which the UI silently rendered as "not enabled").
func TestLocalKeychainBindingStore_GetDegradedKeychain(t *testing.T) {
	safekeyring.Reset()
	safekeyring.SetFileDirForTest(t.TempDir())
	t.Cleanup(func() {
		safekeyring.Reset()
		safekeyring.SetFileDirForTest("")
	})
	keyring.MockInitWithError(errors.New("exec: signal: killed"))
	t.Cleanup(func() { keyring.MockInitWithError(nil) })

	s := NewLocalKeychainBindingStore()
	if _, err := s.Get(context.Background()); !errors.Is(err, ErrLocalBindingNotFound) {
		t.Fatalf("degraded keychain Get must return ErrLocalBindingNotFound, got %v", err)
	}
}

func TestLocalKeychainBindingStore_RoundTrip(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)

	s := NewLocalKeychainBindingStore()
	ctx := context.Background()

	if _, err := s.Get(ctx); !errors.Is(err, ErrLocalBindingNotFound) {
		t.Fatalf("empty Get: %v", err)
	}

	if err := s.SetCredentials(ctx, Credentials{
		AppID: "cli_x", AppSecret: "sec",
		EncryptKey: "enc", VerifyToken: "vt",
	}); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}

	v, err := s.Get(ctx)
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if v.AppID != "cli_x" || v.AppSecret != "sec" {
		t.Fatalf("creds round-trip: %+v", v)
	}
	if v.AppIDHash == "" {
		t.Fatalf("AppIDHash should be derived")
	}

	if err := s.SetBound(ctx, "ou_user"); err != nil {
		t.Fatalf("SetBound: %v", err)
	}
	v, _ = s.Get(ctx)
	if v.OpenID != "ou_user" || v.BoundAt == 0 {
		t.Fatalf("bound state: %+v", v)
	}

	if err := s.SetDisabled(ctx); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}
	v, _ = s.Get(ctx)
	if v.DisabledAt == 0 {
		t.Fatalf("DisabledAt not stored")
	}

	if err := s.ClearDisabled(ctx); err != nil {
		t.Fatalf("ClearDisabled: %v", err)
	}
	v, _ = s.Get(ctx)
	if v.DisabledAt != 0 {
		t.Fatalf("DisabledAt not cleared")
	}

	if err := s.Delete(ctx); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx); !errors.Is(err, ErrLocalBindingNotFound) {
		t.Fatalf("after Delete Get must return ErrLocalBindingNotFound: %v", err)
	}
}

func TestLocalKeychainBindingStore_CredentialUpsertPreservesBoundState(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)

	s := NewLocalKeychainBindingStore()
	ctx := context.Background()
	_ = s.SetCredentials(ctx, Credentials{AppID: "a", AppSecret: "x", EncryptKey: "k", VerifyToken: "v"})
	_ = s.SetBound(ctx, "ou_keep")
	_ = s.SetCredentials(ctx, Credentials{AppID: "a", AppSecret: "x2", EncryptKey: "k2", VerifyToken: "v2"})
	v, _ := s.Get(ctx)
	if v.OpenID != "ou_keep" {
		t.Fatalf("re-Upsert must preserve open_id; got %+v", v)
	}
	if v.AppSecret != "x2" {
		t.Fatalf("re-Upsert must update secrets; got %+v", v)
	}
}

func TestLocalStore_RemoteTerminalRoundTrip(t *testing.T) {
	safekeyring.SetFileDirForTest(t.TempDir())
	safekeyring.UseFileStore()
	t.Cleanup(func() {
		safekeyring.Reset()
		safekeyring.SetFileDirForTest("")
	})
	s := NewLocalKeychainBindingStore()
	ctx := context.Background()

	// Settings persist even with no prior credentials blob.
	if err := s.SetRemoteTerminalSettings(ctx, true, "all"); err != nil {
		t.Fatalf("SetRemoteTerminalSettings (fresh): %v", err)
	}
	v, err := s.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !v.RemoteTerminalEnabled || v.SessionAutoAttach != "all" {
		t.Fatalf("want enabled+all, got %+v", v)
	}
}

func TestLocalStore_RemoteTerminalRejectsInvalidAutoAttach(t *testing.T) {
	safekeyring.SetFileDirForTest(t.TempDir())
	safekeyring.UseFileStore()
	t.Cleanup(func() {
		safekeyring.Reset()
		safekeyring.SetFileDirForTest("")
	})
	s := NewLocalKeychainBindingStore()
	if err := s.SetRemoteTerminalSettings(context.Background(), true, "bogus"); err == nil {
		t.Fatal("want error for invalid autoAttach, got nil")
	}
}
