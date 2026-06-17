// binding_store_relay.go
package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrRelayManagedBoundState is returned by SetBound / SetDisabled /
// ClearDisabled on the relay-backed store because those fields are
// owned by the relay.
var ErrRelayManagedBoundState = errors.New("desktop/feishu: bound state managed by relay")

// RelayBackedBindingStore proxies binding operations to the relay's
// /v1/feishu/bindings/me endpoints.
type RelayBackedBindingStore struct {
	baseURL string
	tokenFn func() string
	client  *http.Client
}

func NewRelayBackedBindingStore(baseURL string, tokenFn func() string) *RelayBackedBindingStore {
	return &RelayBackedBindingStore{
		baseURL: strings.TrimRight(baseURL, "/"),
		tokenFn: tokenFn,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *RelayBackedBindingStore) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader *bytes.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(buf)
	}
	var req *http.Request
	var err error
	if reader != nil {
		req, err = http.NewRequestWithContext(ctx, method, s.baseURL+path, reader)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, s.baseURL+path, nil)
	}
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.tokenFn())
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return s.client.Do(req)
}

func (s *RelayBackedBindingStore) Get(ctx context.Context) (*BindingView, error) {
	resp, err := s.do(ctx, "GET", "/v1/feishu/bindings/me", nil)
	if err != nil {
		return nil, fmt.Errorf("relay get binding: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("relay get binding: status %d", resp.StatusCode)
	}
	var r struct {
		Configured  bool   `json:"configured"`
		Bound       bool   `json:"bound"`
		OpenID      string `json:"open_id"`
		DisabledAt  int64  `json:"disabled_at"`
		CallbackURL string `json:"callback_url"`
		AppIDHash   string `json:"app_id_hash"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("decode binding: %w", err)
	}
	if !r.Configured {
		return nil, ErrLocalBindingNotFound
	}
	hash := r.AppIDHash
	if hash == "" && r.CallbackURL != "" {
		if i := strings.LastIndex(r.CallbackURL, "/"); i >= 0 {
			hash = r.CallbackURL[i+1:]
		}
	}
	return &BindingView{
		AppIDHash:  hash,
		OpenID:     r.OpenID,
		DisabledAt: r.DisabledAt,
	}, nil
}

func (s *RelayBackedBindingStore) SetCredentials(ctx context.Context, c Credentials) error {
	resp, err := s.do(ctx, "POST", "/v1/feishu/bindings/me", map[string]string{
		"AppID":       c.AppID,
		"AppSecret":   c.AppSecret,
		"EncryptKey":  c.EncryptKey,
		"VerifyToken": c.VerifyToken,
	})
	if err != nil {
		return fmt.Errorf("relay set credentials: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := readSnippet(resp.Body)
		return fmt.Errorf("relay set credentials: status %d body=%s", resp.StatusCode, body)
	}
	return nil
}

func (s *RelayBackedBindingStore) SetBound(ctx context.Context, openID string) error {
	return ErrRelayManagedBoundState
}

func (s *RelayBackedBindingStore) SetDisabled(ctx context.Context) error {
	return ErrRelayManagedBoundState
}

func (s *RelayBackedBindingStore) ClearDisabled(ctx context.Context) error {
	return ErrRelayManagedBoundState
}

func (s *RelayBackedBindingStore) Delete(ctx context.Context) error {
	resp, err := s.do(ctx, "DELETE", "/v1/feishu/bindings/me", nil)
	if err != nil {
		return fmt.Errorf("relay delete binding: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("relay delete binding: status %d", resp.StatusCode)
	}
	return nil
}

func readSnippet(r interface{ Read([]byte) (int, error) }) (string, error) {
	buf := make([]byte, 256)
	n, err := r.Read(buf)
	return string(buf[:n]), err
}
