// desktop/feishu/binding_store_local_test.go
package feishu

import (
	"context"
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

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
