// desktop/feishu/binding_store_local.go
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/safekeyring"
)

const keychainAccount = "binding-v1"

// keychainService namespaces the binding's keychain entry so a dev build does
// not share the production binding (suffix is empty in production).
func keychainService() string {
	return "atterm.feishu.binding" + appdir.KeychainSuffix()
}

// LocalKeychainBindingStore persists the user's Feishu binding to the
// OS keychain as a single JSON blob. Used in non-relay mode.
type LocalKeychainBindingStore struct{}

func NewLocalKeychainBindingStore() *LocalKeychainBindingStore {
	return &LocalKeychainBindingStore{}
}

type localBindingBlob struct {
	AppID       string `json:"app_id"`
	AppSecret   string `json:"app_secret"`
	EncryptKey  string `json:"encrypt_key"`
	VerifyToken string `json:"verify_token"`
	OpenID      string `json:"open_id,omitempty"`
	BoundAt     int64  `json:"bound_at,omitempty"`
	DisabledAt  int64  `json:"disabled_at,omitempty"`
}

func (s *LocalKeychainBindingStore) Get(ctx context.Context) (*BindingView, error) {
	raw, err := safekeyring.Get(keychainService(), keychainAccount)
	if errors.Is(err, safekeyring.ErrNotFound) {
		return nil, ErrLocalBindingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("keyring get: %w", err)
	}
	var b localBindingBlob
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		return nil, fmt.Errorf("decode blob: %w", err)
	}
	return &BindingView{
		AppID: b.AppID, AppSecret: b.AppSecret,
		EncryptKey: b.EncryptKey, VerifyToken: b.VerifyToken,
		AppIDHash:  hashAppID(b.AppID),
		OpenID:     b.OpenID,
		BoundAt:    b.BoundAt,
		DisabledAt: b.DisabledAt,
	}, nil
}

func (s *LocalKeychainBindingStore) write(b localBindingBlob) error {
	buf, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("encode blob: %w", err)
	}
	return safekeyring.Set(keychainService(), keychainAccount, string(buf))
}

func (s *LocalKeychainBindingStore) SetCredentials(ctx context.Context, c Credentials) error {
	cur, err := s.Get(ctx)
	var blob localBindingBlob
	if err == nil {
		blob = localBindingBlob{
			OpenID:  cur.OpenID,
			BoundAt: cur.BoundAt,
		}
	}
	blob.AppID = c.AppID
	blob.AppSecret = c.AppSecret
	blob.EncryptKey = c.EncryptKey
	blob.VerifyToken = c.VerifyToken
	blob.DisabledAt = 0
	return s.write(blob)
}

func (s *LocalKeychainBindingStore) SetBound(ctx context.Context, openID string) error {
	v, err := s.Get(ctx)
	if err != nil {
		return err
	}
	return s.write(localBindingBlob{
		AppID: v.AppID, AppSecret: v.AppSecret,
		EncryptKey: v.EncryptKey, VerifyToken: v.VerifyToken,
		OpenID:     openID,
		BoundAt:    time.Now().Unix(),
		DisabledAt: v.DisabledAt,
	})
}

func (s *LocalKeychainBindingStore) SetDisabled(ctx context.Context) error {
	v, err := s.Get(ctx)
	if err != nil {
		return err
	}
	return s.write(localBindingBlob{
		AppID: v.AppID, AppSecret: v.AppSecret,
		EncryptKey: v.EncryptKey, VerifyToken: v.VerifyToken,
		OpenID:     v.OpenID,
		BoundAt:    v.BoundAt,
		DisabledAt: time.Now().Unix(),
	})
}

func (s *LocalKeychainBindingStore) ClearDisabled(ctx context.Context) error {
	v, err := s.Get(ctx)
	if err != nil {
		return err
	}
	return s.write(localBindingBlob{
		AppID: v.AppID, AppSecret: v.AppSecret,
		EncryptKey: v.EncryptKey, VerifyToken: v.VerifyToken,
		OpenID:     v.OpenID,
		BoundAt:    v.BoundAt,
		DisabledAt: 0,
	})
}

func (s *LocalKeychainBindingStore) Delete(ctx context.Context) error {
	err := safekeyring.Delete(keychainService(), keychainAccount)
	if err != nil && !errors.Is(err, safekeyring.ErrNotFound) {
		return fmt.Errorf("keyring delete: %w", err)
	}
	return nil
}
