package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/attson/atterm/internal/feishu"
)

// TokenSource returns a (tenant_access_token, open_id, app_id_hash) tuple.
type TokenSource interface {
	Get(ctx context.Context) (token, openID, appIDHash string, err error)
	Invalidate()
}

var (
	ErrTokenNotConfigured = errors.New("desktop/feishu: feishu not configured")
	ErrTokenDisabled      = errors.New("desktop/feishu: feishu binding disabled")
)

// RelayBorrowedTokenSource calls POST /v1/feishu/relay-token/me on the relay.
type RelayBorrowedTokenSource struct {
	baseURL string
	tokenFn func() string
	client  *http.Client

	mu    sync.Mutex
	cache cachedRelayToken
}

type cachedRelayToken struct {
	tok, openID, hash string
	expiresAt         time.Time
}

func NewRelayBorrowedTokenSource(baseURL string, tokenFn func() string) *RelayBorrowedTokenSource {
	return &RelayBorrowedTokenSource{
		baseURL: strings.TrimRight(baseURL, "/"),
		tokenFn: tokenFn,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (r *RelayBorrowedTokenSource) Invalidate() {
	r.mu.Lock()
	r.cache = cachedRelayToken{}
	r.mu.Unlock()
}

func (r *RelayBorrowedTokenSource) Get(ctx context.Context) (string, string, string, error) {
	r.mu.Lock()
	if r.cache.tok != "" && time.Now().Before(r.cache.expiresAt.Add(-5*time.Minute)) {
		c := r.cache
		r.mu.Unlock()
		return c.tok, c.openID, c.hash, nil
	}
	r.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, "POST", r.baseURL+"/v1/feishu/relay-token/me", nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+r.tokenFn())
	resp, err := r.client.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("borrow token: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return "", "", "", ErrTokenNotConfigured
	case http.StatusGone:
		return "", "", "", ErrTokenDisabled
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return "", "", "", fmt.Errorf("borrow token: status %d body=%s", resp.StatusCode, body)
	}
	var rr struct {
		Token     string `json:"tenant_access_token"`
		OpenID    string `json:"open_id"`
		Hash      string `json:"app_id_hash"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return "", "", "", fmt.Errorf("decode token: %w", err)
	}
	r.mu.Lock()
	r.cache = cachedRelayToken{
		tok: rr.Token, openID: rr.OpenID, hash: rr.Hash,
		expiresAt: time.Now().Add(time.Duration(rr.ExpiresIn) * time.Second),
	}
	r.mu.Unlock()
	return rr.Token, rr.OpenID, rr.Hash, nil
}

// LocalTenantTokenSource mints tokens directly against Feishu using the
// credentials stored in a local BindingStore.
type LocalTenantTokenSource struct {
	store BindingStore
	cache *feishu.TenantTokenCache
}

func NewLocalTenantTokenSource(store BindingStore, baseURL string, httpC *http.Client, now func() time.Time) *LocalTenantTokenSource {
	return &LocalTenantTokenSource{
		store: store,
		cache: feishu.NewTenantTokenCache(baseURL, httpC, now),
	}
}

func (l *LocalTenantTokenSource) Invalidate() {}

func (l *LocalTenantTokenSource) Get(ctx context.Context) (string, string, string, error) {
	v, err := l.store.Get(ctx)
	if err != nil {
		if errors.Is(err, ErrLocalBindingNotFound) {
			return "", "", "", ErrTokenNotConfigured
		}
		return "", "", "", err
	}
	if v.DisabledAt != 0 {
		return "", "", "", ErrTokenDisabled
	}
	tok, err := l.cache.Get(ctx, v.AppID, v.AppSecret)
	if err != nil {
		return "", "", "", fmt.Errorf("mint local token: %w", err)
	}
	return tok, v.OpenID, v.AppIDHash, nil
}
