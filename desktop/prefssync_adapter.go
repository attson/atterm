package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	// The L1 keys below read the raw config field, never a *OrDefault()
	// accessor: what syncs is what the user explicitly set, not what the
	// value resolves to. A machine where the user never touched font size
	// must report "unset", not the resolved default — otherwise it would
	// push that default up and overwrite a real choice made on another
	// machine.
	case "terminal_theme":
		b, _ := json.Marshal(c.TerminalTheme)
		return b, true
	case "terminal_font_head":
		b, _ := json.Marshal(c.TerminalFontHead)
		return b, true
	case "terminal_font_size":
		b, _ := json.Marshal(c.TerminalFontSize)
		return b, true
	case "terminal_line_height":
		b, _ := json.Marshal(c.TerminalLineHeight)
		return b, true
	case "terminal_cursor_style":
		b, _ := json.Marshal(c.TerminalCursorStyle)
		return b, true
	case "terminal_cursor_blink":
		return marshalPtr(c.TerminalCursorBlink)
	case "terminal_scrollback":
		b, _ := json.Marshal(c.TerminalScrollback)
		return b, true
	case "default_shell":
		b, _ := json.Marshal(c.DefaultShell)
		return b, true
	case "shortcut_bindings":
		b, _ := json.Marshal(c.ShortcutBindings)
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
	case "profiles_encrypted":
		key := a.accountKey()
		if len(key) == 0 {
			return nil, false // E2EE inactive → local only, never sync
		}
		blob, err := sealProfiles(key, c.Profiles, c.DefaultProfileID)
		if err != nil || blob == nil {
			return nil, false
		}
		return blob, true
	}
	return nil, false
}

func (a *appConfigAdapter) WriteValue(key string, value json.RawMessage) error {
	c := a.store.Get()
	if handled, err := applyPortableConfigValue(&c, key, value); handled || err != nil {
		if err != nil {
			return err
		}
		return a.store.Set(c)
	}
	switch key {
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
	case "profiles_encrypted":
		key := a.accountKey()
		if len(key) == 0 {
			return nil // no key → ignore inbound sync silently (local only)
		}
		incoming, defaultID, err := openProfiles(key, value)
		if err != nil {
			return err
		}
		// Validate before merging: nothing upstream of this point checked a
		// payload that came off the relay (SetProfiles only guards local
		// edits). Drop individually-malformed entries rather than reject the
		// whole payload — see filterValidProfiles's doc comment for why.
		incoming = filterValidProfiles(incoming)
		// Merge, never replace. SyncEnv defaults to false, so a profile that
		// opted out of env sync is indistinguishable on the wire from one
		// that has no env at all. Replacing c.Profiles wholesale with
		// `incoming` would erase whatever env this machine configured
		// locally for a profile the moment any other machine pushes an
		// update to it — mergeProfiles is what keeps that env alive across
		// a pull (design §5.1).
		c.Profiles = mergeProfiles(c.Profiles, incoming)
		// A default-profile id that no longer names a surviving profile
		// (e.g. it pointed at an entry filterValidProfiles just dropped)
		// must not linger — resolveDefaultProfileID clears it in that case.
		c.DefaultProfileID = resolveDefaultProfileID(defaultID, c.Profiles)
	default:
		return fmt.Errorf("unknown key %s", key)
	}
	return a.store.Set(c)
}

// applyPortableConfigValue applies a non-sealed compatibility value without
// performing I/O. Relay pulls and Peer materialization share this function so
// they cannot drift on validation or zero-value semantics.
func applyPortableConfigValue(c *appConfig, key string, value json.RawMessage) (bool, error) {
	if c == nil {
		return false, errors.New("config is nil")
	}
	switch key {
	case "locale_preference":
		return true, json.Unmarshal(value, &c.LocalePreference)
	case "quick_templates":
		return true, json.Unmarshal(value, &c.QuickTemplates)
	case "notifications_enabled":
		var b bool
		if err := json.Unmarshal(value, &b); err != nil {
			return true, err
		}
		c.NotificationsEnabled = &b
		return true, nil
	case "ai_notifications_only":
		var b bool
		if err := json.Unmarshal(value, &b); err != nil {
			return true, err
		}
		c.AINotificationsOnly = &b
		return true, nil
	case "command_notify_threshold_seconds":
		var n int
		if err := json.Unmarshal(value, &n); err != nil {
			return true, err
		}
		c.CommandNotifyThresholdSeconds = &n
		return true, nil
	case "shell_integration_enabled":
		var b bool
		if err := json.Unmarshal(value, &b); err != nil {
			return true, err
		}
		c.ShellIntegrationEnabled = &b
		return true, nil
	case "pinned_session_ids":
		return true, json.Unmarshal(value, &c.PinnedSessionIDs)
	case "terminal_theme":
		return true, json.Unmarshal(value, &c.TerminalTheme)
	case "terminal_font_head":
		return true, json.Unmarshal(value, &c.TerminalFontHead)
	case "terminal_font_size":
		return true, json.Unmarshal(value, &c.TerminalFontSize)
	case "terminal_line_height":
		return true, json.Unmarshal(value, &c.TerminalLineHeight)
	case "terminal_cursor_style":
		return true, json.Unmarshal(value, &c.TerminalCursorStyle)
	case "terminal_cursor_blink":
		var b *bool
		if err := json.Unmarshal(value, &b); err != nil {
			return true, err
		}
		c.TerminalCursorBlink = b
		return true, nil
	case "terminal_scrollback":
		return true, json.Unmarshal(value, &c.TerminalScrollback)
	case "default_shell":
		return true, json.Unmarshal(value, &c.DefaultShell)
	case "shortcut_bindings":
		var bindings map[string]string
		if err := json.Unmarshal(value, &bindings); err != nil {
			return true, err
		}
		// Invalid entries would suppress the action's default binding without
		// installing a usable replacement, so keep the setter's validation.
		filtered := make(map[string]string, len(bindings))
		for actionID, binding := range bindings {
			if actionID != "" && isValidShortcutBinding(binding) {
				filtered[actionID] = binding
			}
		}
		c.ShortcutBindings = filtered
		return true, nil
	default:
		return false, nil
	}
}

// clearPortableConfigValue applies a canonical scalar tombstone. Collection
// values use an encoded empty array instead, so only scalar keys belong here.
func clearPortableConfigValue(c *appConfig, key string) bool {
	if c == nil {
		return false
	}
	switch key {
	case "locale_preference":
		c.LocalePreference = ""
	case "notifications_enabled":
		c.NotificationsEnabled = nil
	case "ai_notifications_only":
		c.AINotificationsOnly = nil
	case "command_notify_threshold_seconds":
		c.CommandNotifyThresholdSeconds = nil
	case "shell_integration_enabled":
		c.ShellIntegrationEnabled = nil
	case "pinned_session_ids":
		c.PinnedSessionIDs = nil
	case "terminal_theme":
		c.TerminalTheme = ""
	case "terminal_font_head":
		c.TerminalFontHead = ""
	case "terminal_font_size":
		c.TerminalFontSize = 0
	case "terminal_line_height":
		c.TerminalLineHeight = 0
	case "terminal_cursor_style":
		c.TerminalCursorStyle = ""
	case "terminal_cursor_blink":
		c.TerminalCursorBlink = nil
	case "terminal_scrollback":
		c.TerminalScrollback = 0
	case "default_shell":
		c.DefaultShell = ""
	case "shortcut_bindings":
		c.ShortcutBindings = nil
	default:
		return false
	}
	return true
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
