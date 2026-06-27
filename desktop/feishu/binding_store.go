// binding_store.go
package feishu

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// Credentials is the user-supplied tuple required to send + receive
// Feishu IM messages for an app. Identical shape across both storage
// modes.
type Credentials struct {
	AppID       string
	AppSecret   string
	EncryptKey  string
	VerifyToken string
}

// BindingView is the read-side projection seen by the dispatcher.
type BindingView struct {
	AppID, AppSecret, EncryptKey, VerifyToken string
	AppIDHash                                 string
	OpenID                                    string
	BoundAt                                   int64
	DisabledAt                                int64
	// Remote-terminal settings (local mode only; relay mode manages these in
	// the embedded sqlite store, not through this view).
	RemoteTerminalEnabled bool
	SessionAutoAttach     string
	// CallbackURL is the relay-side event endpoint the user must paste into
	// the Feishu console. Only set in relay mode; empty in local mode, where
	// events arrive over the long connection and no public callback exists.
	CallbackURL string
}

// ErrLocalBindingNotFound is the sentinel both local and relay-backed
// implementations return when Get is called against an empty store.
var ErrLocalBindingNotFound = errors.New("desktop/feishu: binding not found")

// BindingStore is the contract the dispatcher relies on.
type BindingStore interface {
	Get(ctx context.Context) (*BindingView, error)
	SetCredentials(ctx context.Context, c Credentials) error
	SetBound(ctx context.Context, openID string) error
	SetDisabled(ctx context.Context) error
	ClearDisabled(ctx context.Context) error
	Delete(ctx context.Context) error
}

// hashAppID mirrors the relay-side helper.
func hashAppID(appID string) string {
	sum := sha256.Sum256([]byte(appID))
	return hex.EncodeToString(sum[:])
}
