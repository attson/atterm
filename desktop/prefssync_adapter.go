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
// Only the whitelisted synced keys are exposed. accountKey supplies the E2EE
// key for the ssh_hosts_encrypted value (nil when E2EE is inactive → that key
// stays local-only, never synced).
type appConfigAdapter struct {
	store      *configStore
	accountKey func() []byte
}

func newAppConfigAdapter(s *configStore, accountKey func() []byte) *appConfigAdapter {
	return &appConfigAdapter{store: s, accountKey: accountKey}
}

// marshalPtr renders an optional preference: nil → (nil, false) so
// prefssync knows nothing is stored yet, non-nil → JSON of *p + true.
// Used by the ReadValue switch to keep each optional-field case one line
// and stop silent json.Marshal errors from hiding a "no value" signal.
func marshalPtr[T any](p *T) (json.RawMessage, bool) {
	if p == nil {
		return nil, false
	}
	b, _ := json.Marshal(*p)
	return b, true
}

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
		return marshalPtr(c.NotificationsEnabled)
	case "ai_notifications_only":
		return marshalPtr(c.AINotificationsOnly)
	case "command_notify_threshold_seconds":
		return marshalPtr(c.CommandNotifyThresholdSeconds)
	case "shell_integration_enabled":
		return marshalPtr(c.ShellIntegrationEnabled)
	case "pinned_session_ids":
		b, _ := json.Marshal(c.PinnedSessionIDs)
		return b, true
	case "ssh_hosts_encrypted":
		key := a.accountKey()
		if len(key) == 0 {
			return nil, false // E2EE inactive → local only, never sync
		}
		creds := make(map[string]sshCredential, len(c.SSHHosts))
		for _, h := range c.SSHHosts {
			if cr, err := sshCredentialSlot(h.ID).Load(); err == nil && cr != (sshCredential{}) {
				creds[h.ID] = cr
			}
		}
		keySecrets := make(map[string]sshKeySecret, len(c.SSHKeys))
		for _, k := range c.SSHKeys {
			if sec, err := sshKeySecretSlot(k.ID).Load(); err == nil && sec != (sshKeySecret{}) {
				keySecrets[k.ID] = sec
			}
		}
		blob, err := sealSSHHosts(key, c.SSHHosts, creds, c.SSHKeys, keySecrets)
		if err != nil || blob == nil {
			return nil, false
		}
		return blob, true
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
	case "ssh_hosts_encrypted":
		key := a.accountKey()
		if len(key) == 0 {
			return nil // no key → ignore inbound sync silently (local only)
		}
		hosts, creds, keys, keySecrets, err := openSSHHosts(key, value)
		if err != nil {
			return err
		}
		for id, cr := range creds {
			if err := sshCredentialSlot(id).Save(cr); err != nil {
				return err
			}
		}
		for id, sec := range keySecrets {
			if err := sshKeySecretSlot(id).Save(sec); err != nil {
				return err
			}
		}
		c.SSHHosts = hosts
		c.SSHKeys = keys
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
