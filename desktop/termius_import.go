package main

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/attson/atterm/internal/termius"
	"github.com/google/uuid"
	"github.com/oklog/ulid/v2"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/ssh"
)

type termiusImportState struct {
	token   string
	expires time.Time
	data    *termius.Data
}

// TermiusHostPreview contains metadata only; secrets stay in the Go process.
type TermiusHostPreview struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Host         string   `json:"host"`
	Port         string   `json:"port"`
	User         string   `json:"user"`
	KeyID        string   `json:"key_id"`
	HasPassword  bool     `json:"has_password"`
	Tags         []string `json:"tags"`
	GroupPath    []string `json:"group_path,omitempty"`
	Warnings     []string `json:"warnings"`
	Blocked      bool     `json:"blocked"`
	Existing     bool     `json:"existing"`
	ForwardCount int      `json:"forward_count"`
	JumpIDs      []string `json:"jump_ids"`
	ProxyLabel   string   `json:"proxy_label,omitempty"`
}

// TermiusKeyPreview exposes key labels without revealing private material.
type TermiusKeyPreview struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Valid bool   `json:"valid"`
}

// TermiusImportPreview grants one confirmation of a specific in-memory snapshot.
type TermiusImportPreview struct {
	Token string               `json:"token"`
	Hosts []TermiusHostPreview `json:"hosts"`
	Keys  []TermiusKeyPreview  `json:"keys"`
}

// TermiusImportReport distinguishes additions from hosts preserved on re-import.
type TermiusImportReport struct {
	Hosts   int `json:"hosts"`
	Keys    int `json:"keys"`
	Skipped int `json:"skipped"`
}

// DiscoverTermiusProfiles keeps side-by-side installation selection explicit.
func (a *App) DiscoverTermiusProfiles() ([]string, error) {
	home, e := os.UserHomeDir()
	if e != nil {
		return nil, e
	}
	config, e := os.UserConfigDir()
	if e != nil {
		return nil, e
	}
	return termius.Discover(home, config, os.Getenv("APPDATA")), nil
}

// SelectTermiusDirectory uses a desktop-only dialog; no profile is sent to a relay.
func (a *App) SelectTermiusDirectory() (string, error) {
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: "Termius", CanCreateDirectories: false})
}

// PreviewTermiusImport reads the source without writing files or storing its localKey.
func (a *App) PreviewTermiusImport(path, localKey string) (TermiusImportPreview, error) {
	if a.cfgStore == nil {
		return TermiusImportPreview{}, errors.New("config store not ready")
	}
	if err := termius.RequireStopped(); err != nil {
		return TermiusImportPreview{}, err
	}
	pending := a.beginTermiusImport()
	data, e := termius.Read(path, func() ([][]byte, error) {
		if strings.TrimSpace(localKey) != "" {
			key, e := termius.DecodeKey(localKey)
			if e != nil {
				return nil, errors.New("termius_invalid_local_key")
			}
			return [][]byte{key}, nil
		}
		return readTermiusKeys()
	})
	if e != nil {
		a.termiusImport.CompareAndSwap(pending, nil)
		return TermiusImportPreview{}, e
	}
	return a.finishTermiusImport(pending, data)
}
func (a *App) beginTermiusImport() *termiusImportState {
	pending := &termiusImportState{token: uuid.NewString()}
	a.termiusImport.Store(pending)
	return pending
}
func (a *App) finishTermiusImport(pending *termiusImportState, data *termius.Data) (TermiusImportPreview, error) {
	state := &termiusImportState{token: pending.token, expires: time.Now().Add(10 * time.Minute), data: data}
	preview := TermiusImportPreview{Token: state.token, Hosts: []TermiusHostPreview{}, Keys: []TermiusKeyPreview{}}
	valid := map[string]bool{}
	for _, k := range data.Keys {
		_, e := parseKeyType(k.PrivateKey, k.Passphrase)
		valid[k.ID] = e == nil
		preview.Keys = append(preview.Keys, TermiusKeyPreview{ID: k.ID, Name: k.Name, Valid: e == nil})
	}
	for i := range data.Hosts {
		h := &data.Hosts[i]
		if h.KeyID != "" && !valid[h.KeyID] {
			h.Blocked = true
			h.Warnings = append(h.Warnings, "invalid_key")
		}
	}
	// Propagate unusable hop credentials to dependent preview entries.
	byID := map[string]*termius.Host{}
	for i := range data.Hosts {
		byID[data.Hosts[i].ID] = &data.Hosts[i]
	}
	for pass := 0; pass < len(data.Hosts); pass++ {
		changed := false
		for i := range data.Hosts {
			h := &data.Hosts[i]
			if h.Blocked {
				continue
			}
			for _, id := range h.JumpIDs {
				hop := byID[id]
				if hop == nil || hop.Blocked {
					h.Blocked = true
					h.Warnings = append(h.Warnings, "invalid_jump")
					changed = true
					break
				}
			}
		}
		if !changed {
			break
		}
	}
	for _, h := range data.Hosts {
		warnings := append([]string{}, h.Warnings...)
		blocked := h.Blocked
		existing := termiusHostExists(a.ListSSHHosts(), h)
		if old, ok := termiusExistingHost(a.ListSSHHosts(), h); ok {
			if termiusGroupNeedsRepair(old.GroupPath, h.GroupPath) {
				existing = false
				warnings = append(warnings, "restore_groups")
			}
			if _, changed := termiusForwardNames(old.Forwards, h.Forwards); changed {
				existing = false
				warnings = append(warnings, "restore_forward_names")
			}
		}
		preview.Hosts = append(preview.Hosts, TermiusHostPreview{ID: h.ID, Name: h.Name, Host: h.Address, Port: h.Port, User: h.Username, KeyID: h.KeyID, HasPassword: h.Password != "", Tags: append([]string{}, h.Tags...), Warnings: warnings, Blocked: blocked, Existing: existing, GroupPath: append([]string{}, h.GroupPath...), ForwardCount: len(h.Forwards), JumpIDs: append([]string{}, h.JumpIDs...), ProxyLabel: termiusProxyLabel(h.Proxy)})
	}
	if !a.termiusImport.CompareAndSwap(pending, state) {
		return TermiusImportPreview{}, errors.New("termius_preview_expired")
	}
	// The timer captures only the token, so cancellation releases the snapshot
	// immediately rather than retaining its credentials until the timer fires.
	token := state.token
	time.AfterFunc(10*time.Minute, func() { a.CancelTermiusImport(token) })
	return preview, nil
}

// CancelTermiusImport cannot clear a newer preview opened by another invocation.
func (a *App) CancelTermiusImport(token string) {
	if state := a.termiusImport.Load(); state != nil && state.token == token {
		a.termiusImport.CompareAndSwap(state, nil)
	}
}
func termiusExistingHost(hosts []SSHHost, h termius.Host) (SSHHost, bool) {
	for _, old := range hosts {
		port := old.Port
		if port == "" {
			port = "22"
		}
		if old.Host == h.Address && port == h.Port && old.User == h.Username && old.Alias == h.Name {
			return old, true
		}
	}
	return SSHHost{}, false
}

// Earlier imports kept only the leaf group. Restore missing ancestors without
// replacing a complete or independently assigned destination path.
func termiusGroupNeedsRepair(saved, source []string) bool {
	if len(source) <= len(saved) {
		return false
	}
	offset := len(source) - len(saved)
	for i, part := range saved {
		if part != source[offset+i] {
			return false
		}
	}
	return true
}

// Match both sides uniquely: duplicate endpoints must not acquire another
// rule's name. Copy before filling notes so a failed save can roll back.
func termiusForwardNames(saved []ForwardRule, source []termius.Forward) ([]ForwardRule, bool) {
	result := append([]ForwardRule(nil), saved...)
	changed := false
	same := func(a ForwardRule, b termius.Forward) bool {
		return a.Kind == b.Kind && a.BindAddr == b.BindAddr && a.BindPort == b.BindPort && a.TargetHost == b.TargetHost && a.TargetPort == b.TargetPort
	}
	for i, rule := range saved {
		if rule.Note != "" {
			continue
		}
		matches := 0
		name := ""
		var matched termius.Forward
		for _, candidate := range source {
			if same(rule, candidate) {
				matches++
				name = candidate.Name
				matched = candidate
			}
		}
		if matches != 1 || name == "" {
			continue
		}
		destinations := 0
		for _, candidate := range saved {
			if same(candidate, matched) {
				destinations++
			}
		}
		if destinations != 1 {
			continue
		}
		result[i].Note = name
		changed = true
	}
	return result, changed
}

func termiusHostExists(hosts []SSHHost, h termius.Host) bool {
	_, ok := termiusExistingHost(hosts, h)
	return ok
}
func termiusKeyFingerprint(k termius.Key) (string, error) {
	var signer ssh.Signer
	var e error
	if k.Passphrase != "" {
		signer, e = ssh.ParsePrivateKeyWithPassphrase([]byte(k.PrivateKey), []byte(k.Passphrase))
	} else {
		signer, e = ssh.ParsePrivateKey([]byte(k.PrivateKey))
	}
	if e != nil {
		return "", errors.New("termius_invalid_key")
	}
	return ssh.FingerprintSHA256(signer.PublicKey()), nil
}

// ImportTermius consumes a preview once. Validation precedes consumption; a
// failed write rolls back new credentials and makes the snapshot retryable.
func (a *App) ImportTermius(token string, hostIDs, keyIDs []string) (TermiusImportReport, error) {
	var report TermiusImportReport
	state := a.termiusImport.Load()
	if state == nil || state.data == nil || state.token != token || time.Now().After(state.expires) {
		return report, errors.New("termius_preview_expired")
	}
	if a.cfgStore == nil {
		return report, errors.New("config store not ready")
	}
	hosts := map[string]termius.Host{}
	keys := map[string]termius.Key{}
	for _, h := range state.data.Hosts {
		hosts[h.ID] = h
	}
	for _, k := range state.data.Keys {
		keys[k.ID] = k
	}
	selectedHosts := []termius.Host{}
	selectedKeys := map[string]bool{}
	seen := map[string]bool{}
	var collect func(string, map[string]bool) error
	collect = func(id string, path map[string]bool) error {
		if path[id] || len(path) > maxJumpDepth {
			return errors.New("termius_invalid_selection")
		}
		if seen[id] {
			return nil
		}
		h, ok := hosts[id]
		if !ok || h.Blocked {
			return errors.New("termius_invalid_selection")
		}
		path[id] = true
		defer delete(path, id)
		for _, hop := range h.JumpIDs {
			if err := collect(hop, path); err != nil {
				return err
			}
		}
		seen[id] = true
		selectedHosts = append(selectedHosts, h)
		return nil
	}
	for _, id := range hostIDs {
		if err := collect(id, map[string]bool{}); err != nil {
			return report, err
		}
	}
	for _, id := range keyIDs {
		if _, ok := keys[id]; !ok {
			return report, errors.New("termius_invalid_selection")
		}
		selectedKeys[id] = true
	}
	if len(selectedHosts) == 0 && len(selectedKeys) == 0 {
		return report, errors.New("termius_empty_selection")
	}
	original := a.cfgStore.Get()
	cfg := original
	cfg.SSHHosts = append([]SSHHost{}, cfg.SSHHosts...)
	cfg.SSHKeys = append([]SSHKey{}, cfg.SSHKeys...)
	additions := []termius.Host{}
	hostMap := map[string]string{}
	existingMap := map[string]SSHHost{}
	for _, h := range selectedHosts {
		if old, ok := termiusExistingHost(cfg.SSHHosts, h); ok {
			hostMap[h.ID] = old.ID
			existingMap[h.ID] = old
			forwards, namesChanged := termiusForwardNames(old.Forwards, h.Forwards)
			groupsChanged := termiusGroupNeedsRepair(old.GroupPath, h.GroupPath)
			if groupsChanged || namesChanged {
				for i := range cfg.SSHHosts {
					if cfg.SSHHosts[i].ID == old.ID {
						if groupsChanged {
							cfg.SSHHosts[i].GroupPath = append([]string{}, h.GroupPath...)
						}
						if namesChanged {
							cfg.SSHHosts[i].Forwards = forwards
						}
					}
				}
				report.Hosts++
			} else {
				report.Skipped++
			}
			continue
		}
		additions = append(additions, h)
		hostMap[h.ID] = ulid.Make().String()
		if h.KeyID != "" {
			selectedKeys[h.KeyID] = true
		}
	}
	// Existing dependency routes must agree; preserving an unrelated saved
	// route would silently change how the newly imported destination is reached.
	dependencies := map[string]bool{}
	for _, h := range selectedHosts {
		for _, id := range h.JumpIDs {
			dependencies[id] = true
		}
	}
	for dep := range dependencies {
		if old, ok := existingMap[dep]; ok {
			source := hosts[dep]
			if (old.ProxyCommand != "" && !isNativeProxy(old)) || old.ProxyJump != termiusJumpRoute(source, hostMap) || !reflect.DeepEqual(old.Proxy, termiusProxy(source.Proxy)) {
				return report, errors.New("termius_dependency_conflict")
			}
		}
	}
	for id := range selectedKeys {
		if _, e := termiusKeyFingerprint(keys[id]); e != nil {
			return TermiusImportReport{}, e
		}
	}
	if !a.termiusImport.CompareAndSwap(state, nil) {
		return TermiusImportReport{}, errors.New("termius_preview_expired")
	}
	var cleanup []func() error
	rollback := func(cause error) (TermiusImportReport, error) {
		failed := false
		for i := len(cleanup) - 1; i >= 0; i-- {
			if cleanup[i]() != nil {
				failed = true
			}
		}
		a.termiusImport.CompareAndSwap(nil, state)
		if failed {
			return TermiusImportReport{}, errors.New("termius_rollback_failed")
		}
		return TermiusImportReport{}, cause
	}
	existingKeys := map[string]string{}
	for _, k := range cfg.SSHKeys {
		if len(selectedKeys) == 0 {
			break
		}
		sec, e := sshKeySecretSlot(k.ID).Load()
		if e != nil {
			return rollback(errors.New("termius_keychain_failed"))
		}
		if fp, e := termiusKeyFingerprint(termius.Key{PrivateKey: sec.PrivateKey, Passphrase: sec.Passphrase}); e == nil {
			existingKeys[fp] = k.ID
		}
	}
	keyMap := map[string]string{}
	// Source ordering makes key allocation and test results independent of map iteration.
	for _, k := range state.data.Keys {
		if !selectedKeys[k.ID] {
			continue
		}
		fp, _ := termiusKeyFingerprint(k)
		if id := existingKeys[fp]; id != "" {
			keyMap[k.ID] = id
			continue
		}
		keyType, e := parseKeyType(k.PrivateKey, k.Passphrase)
		if e != nil {
			return rollback(errors.New("termius_invalid_key"))
		}
		id := ulid.Make().String()
		slot := sshKeySecretSlot(id)
		if e = slot.Save(sshKeySecret{PrivateKey: k.PrivateKey, Passphrase: k.Passphrase}); e != nil {
			return rollback(errors.New("termius_keychain_failed"))
		}
		cleanup = append(cleanup, slot.Clear)
		name := k.Name
		if name == "" {
			name = "Termius"
		}
		cfg.SSHKeys = append(cfg.SSHKeys, SSHKey{ID: id, Name: name, KeyType: keyType})
		keyMap[k.ID] = id
		existingKeys[fp] = id
		report.Keys++
	}
	for _, h := range additions {
		id := hostMap[h.ID]
		host := SSHHost{ID: id, Alias: h.Name, Host: h.Address, Port: h.Port, User: h.Username, AuthKind: "password", Tags: h.Tags, GroupPath: append([]string{}, h.GroupPath...), Forwards: []ForwardRule{}, Proxy: termiusProxy(h.Proxy), ProxyJump: termiusJumpRoute(h, hostMap)}
		if h.Proxy != nil {
			host.ProxyCommand = nativeProxyMarker
		}
		if h.KeyID != "" {
			host.AuthKind = "key"
			host.KeyID = keyMap[h.KeyID]
			if host.KeyID == "" {
				return rollback(errors.New("termius_invalid_key"))
			}
		}
		for _, f := range h.Forwards {
			host.Forwards = append(host.Forwards, ForwardRule{ID: ulid.Make().String(), Note: f.Name, Kind: f.Kind, BindAddr: f.BindAddr, BindPort: f.BindPort, TargetHost: f.TargetHost, TargetPort: f.TargetPort})
		}
		if h.Password != "" || (h.Proxy != nil && h.Proxy.Password != "") {
			slot := sshCredentialSlot(id)
			cred := sshCredential{Password: h.Password}
			if h.Proxy != nil {
				cred.ProxyPassword = h.Proxy.Password
			}
			if e := slot.Save(cred); e != nil {
				return rollback(errors.New("termius_keychain_failed"))
			}
			cleanup = append(cleanup, slot.Clear)
		}
		cfg.SSHHosts = append(cfg.SSHHosts, host)
		report.Hosts++
	}
	if report.Hosts+report.Keys > 0 {
		if e := a.cfgStore.Set(cfg); e != nil {
			// configStore publishes in-memory state even on disk failure.
			// Restore that state before rolling back the credential writes.
			_ = a.cfgStore.Set(original)
			return rollback(fmt.Errorf("save imported Termius configuration: %w", e))
		}
		a.markSSHHostsDirty()
	}
	return report, nil
}

func termiusProxy(p *termius.Proxy) *SSHProxy {
	if p == nil {
		return nil
	}
	return &SSHProxy{Kind: p.Kind, Host: p.Host, Port: p.Port, Username: p.Username}
}
func termiusProxyLabel(p *termius.Proxy) string {
	if p == nil {
		return ""
	}
	return strings.ToUpper(p.Kind) + " · " + p.Host + ":" + p.Port
}
func termiusJumpRoute(h termius.Host, ids map[string]string) string {
	hops := make([]string, 0, len(h.JumpIDs))
	for _, id := range h.JumpIDs {
		if ids[id] != "" {
			hops = append(hops, "atterm-id:"+ids[id])
		}
	}
	return strings.Join(hops, ",")
}
