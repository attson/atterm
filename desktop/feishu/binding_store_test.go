// binding_store_test.go
package feishu

import (
	"context"
	"errors"
	"testing"
)

// inMemBindingStore is a trivial implementation used to assert the
// interface contract without dragging in keychain or HTTP code.
type inMemBindingStore struct {
	view *BindingView
}

func (s *inMemBindingStore) Get(ctx context.Context) (*BindingView, error) {
	if s.view == nil {
		return nil, ErrLocalBindingNotFound
	}
	cp := *s.view
	return &cp, nil
}
func (s *inMemBindingStore) SetCredentials(ctx context.Context, c Credentials) error {
	s.view = &BindingView{
		AppID: c.AppID, AppSecret: c.AppSecret,
		EncryptKey: c.EncryptKey, VerifyToken: c.VerifyToken,
		AppIDHash: hashAppID(c.AppID),
	}
	return nil
}
func (s *inMemBindingStore) SetBound(ctx context.Context, openID string) error {
	if s.view == nil {
		return ErrLocalBindingNotFound
	}
	s.view.OpenID = openID
	return nil
}
func (s *inMemBindingStore) SetDisabled(ctx context.Context) error {
	if s.view == nil {
		return ErrLocalBindingNotFound
	}
	s.view.DisabledAt = 1
	return nil
}
func (s *inMemBindingStore) ClearDisabled(ctx context.Context) error {
	if s.view == nil {
		return ErrLocalBindingNotFound
	}
	s.view.DisabledAt = 0
	return nil
}
func (s *inMemBindingStore) Delete(ctx context.Context) error {
	s.view = nil
	return nil
}

func TestBindingStore_Interface(t *testing.T) {
	var _ BindingStore = (*inMemBindingStore)(nil)
	s := &inMemBindingStore{}
	ctx := context.Background()
	if _, err := s.Get(ctx); !errors.Is(err, ErrLocalBindingNotFound) {
		t.Fatalf("empty Get must return ErrLocalBindingNotFound")
	}
	if err := s.SetCredentials(ctx, Credentials{
		AppID: "cli_x", AppSecret: "s", EncryptKey: "k", VerifyToken: "v",
	}); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}
	v, _ := s.Get(ctx)
	if v.AppID != "cli_x" || v.AppIDHash == "" {
		t.Fatalf("view: %+v", v)
	}
	if err := s.SetBound(ctx, "ou_test"); err != nil {
		t.Fatalf("SetBound: %v", err)
	}
	v, _ = s.Get(ctx)
	if v.OpenID != "ou_test" {
		t.Fatalf("OpenID not set")
	}
	if err := s.SetDisabled(ctx); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}
	v, _ = s.Get(ctx)
	if v.DisabledAt == 0 {
		t.Fatalf("DisabledAt not set")
	}
	if err := s.ClearDisabled(ctx); err != nil {
		t.Fatalf("ClearDisabled: %v", err)
	}
	v, _ = s.Get(ctx)
	if v.DisabledAt != 0 {
		t.Fatalf("DisabledAt not cleared")
	}
}

func TestHashAppID(t *testing.T) {
	a := hashAppID("cli_x")
	b := hashAppID("cli_x")
	if a != b || len(a) != 64 {
		t.Fatalf("hash unstable or wrong length: %q vs %q", a, b)
	}
	if a == hashAppID("cli_y") {
		t.Fatalf("hash collision")
	}
}
