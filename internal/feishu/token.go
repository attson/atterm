// internal/feishu/token.go
package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// authClassCodes are Feishu error codes that mean "your app credentials
// are wrong / disabled" — the relay turns these into a "disable binding"
// signal upstream. List drawn from open.feishu.cn /docs (auth errors).
var authClassCodes = map[int]bool{
	99991663: true, // invalid app_secret
	99991664: true, // invalid app_id
	99991665: true, // app disabled
}

// AuthClassError wraps an upstream auth-related code.
type AuthClassError struct {
	Code int
	Msg  string
}

func (e *AuthClassError) Error() string {
	return fmt.Sprintf("feishu auth-class error: code=%d msg=%s", e.Code, e.Msg)
}

// IsAuthClassError reports whether err (or anything it wraps) is *AuthClassError.
func IsAuthClassError(err error) bool {
	var e *AuthClassError
	return errors.As(err, &e)
}

// TenantTokenCache memoizes tenant_access_token per app_id.
type TenantTokenCache struct {
	baseURL string
	httpC   *http.Client
	now     func() time.Time
	sf      singleflight.Group
	mu      sync.Mutex
	entries map[string]cachedToken
}

type cachedToken struct {
	token     string
	expiresAt time.Time
}

func NewTenantTokenCache(baseURL string, httpC *http.Client, now func() time.Time) *TenantTokenCache {
	if httpC == nil {
		httpC = &http.Client{Timeout: 10 * time.Second}
	}
	if now == nil {
		now = time.Now
	}
	return &TenantTokenCache{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpC:   httpC,
		now:     now,
		entries: map[string]cachedToken{},
	}
}

// Get returns a valid token, fetching if necessary. Concurrent callers
// for the same app_id share the same upstream request via singleflight.
func (c *TenantTokenCache) Get(ctx context.Context, appID, appSecret string) (string, error) {
	if cur, ok := c.read(appID); ok {
		return cur, nil
	}
	out, err, _ := c.sf.Do(appID, func() (any, error) {
		if cur, ok := c.read(appID); ok {
			return cur, nil
		}
		tok, expSec, err := c.fetch(ctx, appID, appSecret)
		if err != nil {
			return "", err
		}
		c.store(appID, tok, expSec)
		return tok, nil
	})
	if err != nil {
		return "", err
	}
	return out.(string), nil
}

// Invalidate drops the cached entry for appID (used after binding delete).
func (c *TenantTokenCache) Invalidate(appID string) {
	c.mu.Lock()
	delete(c.entries, appID)
	c.mu.Unlock()
}

func (c *TenantTokenCache) read(appID string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[appID]
	if !ok {
		return "", false
	}
	// Refresh 5 min before actual expiry to absorb clock drift.
	if c.now().After(e.expiresAt.Add(-5 * time.Minute)) {
		return "", false
	}
	return e.token, true
}

func (c *TenantTokenCache) store(appID, token string, expiresInSec int) {
	c.mu.Lock()
	c.entries[appID] = cachedToken{
		token:     token,
		expiresAt: c.now().Add(time.Duration(expiresInSec) * time.Second),
	}
	c.mu.Unlock()
}

type tenantTokenResp struct {
	Code              int    `json:"code"`
	Msg               string `json:"msg"`
	TenantAccessToken string `json:"tenant_access_token"`
	Expire            int    `json:"expire"`
}

func (c *TenantTokenCache) fetch(ctx context.Context, appID, appSecret string) (string, int, error) {
	body, _ := json.Marshal(map[string]string{
		"app_id":     appID,
		"app_secret": appSecret,
	})
	req, _ := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/open-apis/auth/v3/tenant_access_token/internal",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpC.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("tenant_access_token request: %w", err)
	}
	defer resp.Body.Close()
	var r tenantTokenResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", 0, fmt.Errorf("decode token resp: %w", err)
	}
	if r.Code != 0 {
		if authClassCodes[r.Code] {
			return "", 0, &AuthClassError{Code: r.Code, Msg: r.Msg}
		}
		return "", 0, fmt.Errorf("feishu tenant_access_token: code=%d msg=%s", r.Code, r.Msg)
	}
	if r.TenantAccessToken == "" {
		return "", 0, fmt.Errorf("feishu tenant_access_token: empty token")
	}
	return r.TenantAccessToken, r.Expire, nil
}
