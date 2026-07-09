package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// RelaySessionRow mirrors the relay's SessionRow shape used by
// GET /api/me/sessions.
type RelaySessionRow struct {
	IDHash    string `json:"id_hash"`
	UserAgent string `json:"user_agent"`
	IPPrefix  string `json:"ip_prefix"`
	CreatedAt int64  `json:"created_at"` // unix ms
	ExpiresAt int64  `json:"expires_at"` // unix ms
	IsCurrent bool   `json:"is_current"`
}

// SignOutOthersResult mirrors the response of POST
// /api/me/sessions/sign-out-others.
type SignOutOthersResult struct {
	Deleted int `json:"deleted"`
}

// meSessionsGET issues GET /api/me/sessions and parses the response.
func (a *App) meSessionsGET(ctx context.Context, base, token string, allowInsecure bool) ([]RelaySessionRow, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+"/api/me/sessions", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	body, err := a.doRelayHTTP(req, allowInsecure)
	if err != nil {
		return nil, err
	}
	var rows []RelaySessionRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("parse sessions response: %w", err)
	}
	return rows, nil
}

// meSessionDELETE revokes one session by id_hash.
func (a *App) meSessionDELETE(ctx context.Context, base, token, idHash string, allowInsecure bool) error {
	u := strings.TrimRight(base, "/") + "/api/me/sessions/" + url.PathEscape(idHash)
	req, err := http.NewRequestWithContext(ctx, "DELETE", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	_, err = a.doRelayHTTP(req, allowInsecure)
	return err
}

// meSessionsSignOutOthers revokes every session except the current one.
func (a *App) meSessionsSignOutOthers(ctx context.Context, base, token string, allowInsecure bool) (SignOutOthersResult, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+"/api/me/sessions/sign-out-others", nil)
	if err != nil {
		return SignOutOthersResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	body, err := a.doRelayHTTP(req, allowInsecure)
	if err != nil {
		return SignOutOthersResult{}, err
	}
	var out SignOutOthersResult
	if err := json.Unmarshal(body, &out); err != nil {
		return SignOutOthersResult{}, fmt.Errorf("parse sign-out-others response: %w", err)
	}
	return out, nil
}

// doRelayHTTP issues req via the shared relay http client and returns
// the body on 2xx. 401 gets a friendly "session expired" message; other
// non-2xx codes surface verbatim so the frontend can display them.
func (a *App) doRelayHTTP(req *http.Request, allowInsecure bool) ([]byte, error) {
	// Same TLS + proxy policy as FetchRelayMe (desktop/app.go:1919).
	client := relayHTTPClient(allowInsecure, 0)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("session expired, please log in again")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("relay returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}
