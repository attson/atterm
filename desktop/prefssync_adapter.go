package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/attson/atterm/internal/prefssync"
)

// appConfigAdapter glues prefssync.Adapter to the desktop configStore.
// Only the 5 synced keys are exposed.
type appConfigAdapter struct {
	store *configStore
}

func newAppConfigAdapter(s *configStore) *appConfigAdapter { return &appConfigAdapter{store: s} }

func (a *appConfigAdapter) ReadValue(key string) (json.RawMessage, bool) {
	c := a.store.Get()
	switch key {
	case "locale_preference":
		b, _ := json.Marshal(c.LocalePreference)
		return b, true
	case "quick_templates":
		b, _ := json.Marshal(c.QuickTemplates)
		return b, true
	case "notifications_enabled":
		if c.NotificationsEnabled == nil {
			return nil, false
		}
		b, _ := json.Marshal(*c.NotificationsEnabled)
		return b, true
	case "ai_notifications_only":
		if c.AINotificationsOnly == nil {
			return nil, false
		}
		b, _ := json.Marshal(*c.AINotificationsOnly)
		return b, true
	case "command_notify_threshold_seconds":
		if c.CommandNotifyThresholdSeconds == nil {
			return nil, false
		}
		b, _ := json.Marshal(*c.CommandNotifyThresholdSeconds)
		return b, true
	case "shell_integration_enabled":
		if c.ShellIntegrationEnabled == nil {
			return nil, false
		}
		b, _ := json.Marshal(*c.ShellIntegrationEnabled)
		return b, true
	case "pinned_session_ids":
		b, _ := json.Marshal(c.PinnedSessionIDs)
		return b, true
	}
	return nil, false
}

func (a *appConfigAdapter) WriteValue(key string, value json.RawMessage) error {
	c := a.store.Get()
	switch key {
	case "locale_preference":
		var s string
		if err := json.Unmarshal(value, &s); err != nil {
			return err
		}
		c.LocalePreference = s
	case "quick_templates":
		var t []QuickTemplate
		if err := json.Unmarshal(value, &t); err != nil {
			return err
		}
		c.QuickTemplates = t
	case "notifications_enabled":
		var b bool
		if err := json.Unmarshal(value, &b); err != nil {
			return err
		}
		c.NotificationsEnabled = &b
	case "ai_notifications_only":
		var b bool
		if err := json.Unmarshal(value, &b); err != nil {
			return err
		}
		c.AINotificationsOnly = &b
	case "command_notify_threshold_seconds":
		var n int
		if err := json.Unmarshal(value, &n); err != nil {
			return err
		}
		c.CommandNotifyThresholdSeconds = &n
	case "shell_integration_enabled":
		var b bool
		if err := json.Unmarshal(value, &b); err != nil {
			return err
		}
		c.ShellIntegrationEnabled = &b
	case "pinned_session_ids":
		var ids []string
		if err := json.Unmarshal(value, &ids); err != nil {
			return err
		}
		c.PinnedSessionIDs = ids
	default:
		return fmt.Errorf("unknown key %s", key)
	}
	return a.store.Set(c)
}

func (a *appConfigAdapter) ReadMeta(key string) prefssync.Meta {
	c := a.store.Get()
	if c.PrefsMeta == nil {
		return prefssync.Meta{}
	}
	m := c.PrefsMeta[key]
	return prefssync.Meta{UpdatedAtLocal: m.UpdatedAtLocal, Dirty: m.Dirty}
}

func (a *appConfigAdapter) WriteMeta(key string, m prefssync.Meta) error {
	c := a.store.Get()
	if c.PrefsMeta == nil {
		c.PrefsMeta = map[string]prefsMetaEntry{}
	}
	c.PrefsMeta[key] = prefsMetaEntry{UpdatedAtLocal: m.UpdatedAtLocal, Dirty: m.Dirty}
	return a.store.Set(c)
}

func (a *appConfigAdapter) Keys() []string { return prefssync.SyncedKeys() }

// httpRelayClient implements prefssync.RelayClient against the real
// /api/me/preferences endpoints, using the bearer token stored in the
// config store.
type httpRelayClient struct {
	store *configStore
	// http, when non-nil, overrides the per-request client (tests inject an
	// httptest client here). Production leaves it nil so clientFor builds a
	// client that honours the relay's allow_insecure_relay flag.
	http *http.Client
}

func newHTTPRelayClient(s *configStore) *httpRelayClient {
	return &httpRelayClient{store: s}
}

// clientFor returns the HTTP client to use for a request, honouring the
// relay's allow_insecure_relay flag so self-signed relays are reachable.
func (c *httpRelayClient) clientFor() *http.Client {
	if c.http != nil {
		return c.http
	}
	return relayHTTPClient(c.store.Get().AllowInsecureRelay, 0)
}

func (c *httpRelayClient) base() (string, string, error) {
	cfg := c.store.Get()
	if cfg.RelaySessionToken == "" || cfg.RelayURL == "" {
		return "", "", fmt.Errorf("not logged in")
	}
	httpURL, _, err := relayLoginEndpoints(cfg.RelayURL)
	if err != nil {
		return "", "", err
	}
	return strings.TrimRight(httpURL, "/"), cfg.RelaySessionToken, nil
}

func (c *httpRelayClient) Get(ctx context.Context) ([]prefssync.ServerItem, error) {
	base, tok, err := c.base()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/api/me/preferences", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.clientFor().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get prefs: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Items []prefssync.ServerItem `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Items, nil
}

func (c *httpRelayClient) Put(ctx context.Context, items []prefssync.ClientItem) ([]prefssync.ServerItem, error) {
	base, tok, err := c.base()
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]any{"items": items})
	req, err := http.NewRequestWithContext(ctx, "PUT", base+"/api/me/preferences", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.clientFor().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("put prefs: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Items []prefssync.ServerItem `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Items, nil
}
