package main

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/attson/atterm/internal/logging"
)

const (
	desktopDebugScopeAll           = "*"
	desktopDebugScopeApp           = "app"
	desktopDebugScopeStateSync     = "state-sync"
	desktopDebugScopeConnection    = "connection"
	desktopDebugScopePrefsSync     = "prefs-sync"
	desktopDebugScopeTerminalIO    = "terminal-io"
	desktopDebugScopeNotifications = "notifications"
	desktopDebugScopeAuth          = "auth"
	desktopDebugScopePeer          = "peer"
	desktopDebugScopePayload       = "payload"
)

var desktopKnownDebugScopes = map[string]struct{}{
	desktopDebugScopeAll: {}, desktopDebugScopeApp: {}, desktopDebugScopeStateSync: {},
	desktopDebugScopeConnection: {}, desktopDebugScopePrefsSync: {}, desktopDebugScopeTerminalIO: {},
	desktopDebugScopeNotifications: {}, desktopDebugScopeAuth: {}, desktopDebugScopePeer: {},
	desktopDebugScopePayload: {},
}

type desktopDebugRuntimeConfig struct {
	enabled   bool
	scopes    map[string]struct{}
	untilUnix int64
}

var desktopDebugConfig atomic.Pointer[desktopDebugRuntimeConfig]

func normalizeDesktopDebugScopes(scopes []string) []string {
	seen := make(map[string]struct{}, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.ToLower(strings.TrimSpace(scope))
		if _, ok := desktopKnownDebugScopes[scope]; !ok {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	sort.Strings(out)
	return out
}

func validateDesktopDebugScopes(scopes []string) error {
	for _, scope := range scopes {
		if _, ok := desktopKnownDebugScopes[strings.ToLower(strings.TrimSpace(scope))]; !ok {
			return fmt.Errorf("unknown debug scope %q", scope)
		}
	}
	return nil
}

func configureDesktopDebug(enabled bool, scopes []string, until string) {
	normalized := normalizeDesktopDebugScopes(scopes)
	if enabled && len(normalized) == 0 {
		normalized = []string{desktopDebugScopeAll}
	}
	set := make(map[string]struct{}, len(normalized))
	for _, scope := range normalized {
		set[scope] = struct{}{}
	}
	var untilUnix int64
	if parsed, err := time.Parse(time.RFC3339, until); err == nil {
		untilUnix = parsed.Unix()
	}
	desktopDebugConfig.Store(&desktopDebugRuntimeConfig{enabled: enabled, scopes: set, untilUnix: untilUnix})
}

func desktopDebugScopeEnabled(scope string) bool {
	cfg := desktopDebugConfig.Load()
	if cfg == nil {
		return logging.Enabled(logging.LevelDebug)
	}
	if !cfg.enabled || (cfg.untilUnix > 0 && time.Now().Unix() >= cfg.untilUnix) {
		return false
	}
	if _, ok := cfg.scopes[scope]; ok {
		return true
	}
	if scope == desktopDebugScopePayload {
		return false
	}
	_, all := cfg.scopes[desktopDebugScopeAll]
	return all
}

func desktopDebugScopeForTag(tag string) string {
	switch {
	case tag == "state-sync":
		return desktopDebugScopeStateSync
	case strings.HasPrefix(tag, "uplink"), strings.HasPrefix(tag, "relay"),
		strings.HasPrefix(tag, "ssh"), strings.HasPrefix(tag, "service"):
		return desktopDebugScopeConnection
	case strings.HasPrefix(tag, "prefs"):
		return desktopDebugScopePrefsSync
	case strings.HasPrefix(tag, "pty"), tag == "repaint":
		return desktopDebugScopeTerminalIO
	case tag == "notify", tag == "webpush", tag == "feishu", tag == "feishu-form":
		return desktopDebugScopeNotifications
	case tag == "opaque", tag == "keychain", tag == "e2ee":
		return desktopDebugScopeAuth
	case strings.HasPrefix(tag, "peer"):
		return desktopDebugScopePeer
	case tag == "recovery", tag == "ai-sid", tag == "paste",
		strings.HasPrefix(tag, "feishu-anchor"), strings.HasPrefix(tag, "feishu-card"),
		strings.HasPrefix(tag, "feishu-turn"), strings.HasPrefix(tag, "feishu-hook"):
		return desktopDebugScopePayload
	default:
		return desktopDebugScopeApp
	}
}
