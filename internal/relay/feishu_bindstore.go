// internal/relay/feishu_bindstore.go
package relay

import (
	"context"
	"errors"

	"github.com/attson/atterm/internal/feishu"
	"github.com/attson/atterm/internal/userstore"
)

// feishuBindStore adapts *userstore.DBStore to feishu.BindingStore.
type feishuBindStore struct{ st *userstore.DBStore }

// NewFeishuBindStore wraps a userstore for feishu.Service consumption.
func NewFeishuBindStore(st *userstore.DBStore) feishu.BindingStore {
	return &feishuBindStore{st: st}
}

func (a *feishuBindStore) GetBindingByAppIDHash(ctx context.Context, hash string) (*feishu.Binding, error) {
	row, err := a.st.GetFeishuBindingByAppIDHash(ctx, hash)
	if errors.Is(err, userstore.ErrFeishuBindingNotFound) {
		return nil, feishu.ErrBindingNotFound
	}
	if err != nil {
		return nil, err
	}
	return rowToBinding(row), nil
}

func (a *feishuBindStore) GetBindingByUserID(ctx context.Context, userID string) (*feishu.Binding, error) {
	row, err := a.st.GetFeishuBinding(ctx, userID)
	if errors.Is(err, userstore.ErrFeishuBindingNotFound) {
		return nil, feishu.ErrBindingNotFound
	}
	if err != nil {
		return nil, err
	}
	return rowToBinding(row), nil
}

func (a *feishuBindStore) MarkBound(ctx context.Context, userID, openID string) error {
	return a.st.MarkFeishuBindingBound(ctx, userID, openID)
}

func (a *feishuBindStore) MarkDisabled(ctx context.Context, userID string) error {
	return a.st.MarkFeishuBindingDisabled(ctx, userID)
}

func (a *feishuBindStore) ConsumePendingBind(ctx context.Context, code string) (string, error) {
	uid, err := a.st.ConsumeFeishuPendingBind(ctx, code)
	if errors.Is(err, userstore.ErrFeishuPendingBindNotFound) {
		return "", feishu.ErrFeishuPendingBindNotFoundService
	}
	return uid, err
}

func rowToBinding(r *userstore.FeishuBinding) *feishu.Binding {
	return &feishu.Binding{
		UserID:      r.UserID,
		AppID:       r.AppID,
		AppSecret:   r.AppSecret,
		EncryptKey:  r.EncryptKey,
		VerifyToken: r.VerifyToken,
		AppIDHash:   r.AppIDHash,
		OpenID:      r.OpenID,
		DisabledAt:  r.DisabledAt,
	}
}
