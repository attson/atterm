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
	AppID                 string `json:"app_id"`
	AppSecret             string `json:"app_secret"`
	EncryptKey            string `json:"encrypt_key"`
	VerifyToken           string `json:"verify_token"`
	OpenID                string `json:"open_id,omitempty"`
	BoundAt               int64  `json:"bound_at,omitempty"`
	DisabledAt            int64  `json:"disabled_at,omitempty"`
	RemoteTerminalEnabled bool   `json:"remote_terminal_enabled,omitempty"`
	SessionAutoAttach     string `json:"session_auto_attach,omitempty"`
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
		AppIDHash:             hashAppID(b.AppID),
		OpenID:                b.OpenID,
		BoundAt:               b.BoundAt,
		DisabledAt:            b.DisabledAt,
		RemoteTerminalEnabled: b.RemoteTerminalEnabled,
		SessionAutoAttach:     b.SessionAutoAttach,
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
			OpenID:                cur.OpenID,
			BoundAt:               cur.BoundAt,
			RemoteTerminalEnabled: cur.RemoteTerminalEnabled,
			SessionAutoAttach:     cur.SessionAutoAttach,
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
		OpenID:                openID,
		BoundAt:               time.Now().Unix(),
		DisabledAt:            v.DisabledAt,
		RemoteTerminalEnabled: v.RemoteTerminalEnabled,
		SessionAutoAttach:     v.SessionAutoAttach,
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
		OpenID:                v.OpenID,
		BoundAt:               v.BoundAt,
		DisabledAt:            time.Now().Unix(),
		RemoteTerminalEnabled: v.RemoteTerminalEnabled,
		SessionAutoAttach:     v.SessionAutoAttach,
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
		OpenID:                v.OpenID,
		BoundAt:               v.BoundAt,
		DisabledAt:            0,
		RemoteTerminalEnabled: v.RemoteTerminalEnabled,
		SessionAutoAttach:     v.SessionAutoAttach,
	})
}

// SetRemoteTerminalSettings persists the remote-terminal toggle and autoAttach
// mode into the keychain blob. Independent of credentials: a blob is created
// with only these fields if none exists yet, so the user can enable remote
// terminal before filling in AppID/AppSecret.
func (s *LocalKeychainBindingStore) SetRemoteTerminalSettings(ctx context.Context, enabled bool, autoAttach string) error {
	switch autoAttach {
	case "ai", "all", "none":
	default:
		return fmt.Errorf("desktop/feishu: invalid session_auto_attach %q (want ai|all|none)", autoAttach)
	}
	cur, err := s.Get(ctx)
	var blob localBindingBlob
	if err == nil {
		blob = localBindingBlob{
			AppID: cur.AppID, AppSecret: cur.AppSecret,
			EncryptKey: cur.EncryptKey, VerifyToken: cur.VerifyToken,
			OpenID: cur.OpenID, BoundAt: cur.BoundAt, DisabledAt: cur.DisabledAt,
		}
	}
	blob.RemoteTerminalEnabled = enabled
	blob.SessionAutoAttach = autoAttach
	return s.write(blob)
}

func (s *LocalKeychainBindingStore) Delete(ctx context.Context) error {
	err := safekeyring.Delete(keychainService(), keychainAccount)
	if err != nil && !errors.Is(err, safekeyring.ErrNotFound) {
		return fmt.Errorf("keyring delete: %w", err)
	}
	return nil
}
