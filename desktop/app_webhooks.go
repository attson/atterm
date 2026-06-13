package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Webhook mirrors the relay /api/me/webhooks row. AllowInsecure is intentionally
// absent — the relay never echoes it back on list/create responses.
type Webhook struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Format    string `json:"format"`
	CreatedAt string `json:"created_at"`
}

// CreateWebhookReq is the body for POST /api/me/webhooks.
type CreateWebhookReq struct {
	Name          string `json:"name"`
	URL           string `json:"url"`
	Format        string `json:"format"`
	AllowInsecure bool   `json:"allow_insecure"`
}

// ListWebhooks returns the relay user's webhook rows.
func (a *App) ListWebhooks() ([]Webhook, error) {
	base, tok, err := a.relayHTTPBase()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, base+"/api/me/webhooks", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("relay_unauthorized")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, decodeRelayError(resp)
	}
	var out []Webhook
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode webhooks: %w", err)
	}
	if out == nil {
		out = []Webhook{}
	}
	return out, nil
}

// CreateWebhook posts a new webhook config and returns the created row.
func (a *App) CreateWebhook(in CreateWebhookReq) (Webhook, error) {
	base, tok, err := a.relayHTTPBase()
	if err != nil {
		return Webhook{}, err
	}
	body, err := json.Marshal(in)
	if err != nil {
		return Webhook{}, err
	}
	req, err := http.NewRequest(http.MethodPost, base+"/api/me/webhooks", bytes.NewReader(body))
	if err != nil {
		return Webhook{}, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Webhook{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return Webhook{}, fmt.Errorf("relay_unauthorized")
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return Webhook{}, decodeRelayError(resp)
	}
	var out Webhook
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Webhook{}, fmt.Errorf("decode webhook: %w", err)
	}
	return out, nil
}

// DeleteWebhook removes a webhook by id. 404 is mapped to webhook_not_found.
func (a *App) DeleteWebhook(id string) error {
	base, tok, err := a.relayHTTPBase()
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodDelete, base+"/api/me/webhooks/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return fmt.Errorf("relay_unauthorized")
	case http.StatusNotFound:
		return fmt.Errorf("webhook_not_found")
	default:
		return decodeRelayError(resp)
	}
}

// relayHTTPBase returns the normalized http(s) base URL and the bearer token
// from configStore. Returns an error when relay is not configured.
func (a *App) relayHTTPBase() (string, string, error) {
	if a.cfgStore == nil {
		return "", "", fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		return "", "", fmt.Errorf("no relay configured")
	}
	base := strings.Replace(strings.Replace(cfg.RelayURL, "wss://", "https://", 1), "ws://", "http://", 1)
	return strings.TrimRight(base, "/"), cfg.RelaySessionToken, nil
}

// decodeRelayError reads `{error: "code"}` if present and returns it verbatim
// so the frontend can map codes to localized strings. Falls back to status.
func decodeRelayError(resp *http.Response) error {
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil && body.Error != "" {
		return fmt.Errorf("%s", body.Error)
	}
	return fmt.Errorf("relay error: HTTP %d", resp.StatusCode)
}
