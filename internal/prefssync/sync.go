// Package prefssync is the desktop-side cross-device user-preference
// sync engine. It owns no UI; the Wails frontend continues to call the
// existing setters on App. This package reads/writes the same five
// fields through an Adapter (impl: appConfigAdapter) and reconciles
// with the relay HTTP API (GET/PUT /api/me/preferences).
package prefssync

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/attson/atterm/internal/logging"
)

// Meta is the per-key sync state. Mirrors the JSON tags of
// desktop.prefsMetaEntry so the appConfigAdapter can serialize 1:1.
type Meta struct {
	UpdatedAtLocal int64 `json:"updated_at_local"`
	Dirty          bool  `json:"dirty"`
}

// Adapter is the interface the engine uses to read/write the canonical
// local state for the 5 synced fields. The desktop implementation wraps
// configStore; the test impl is in-memory.
type Adapter interface {
	ReadValue(key string) (json.RawMessage, bool)
	WriteValue(key string, value json.RawMessage) error
	ReadMeta(key string) Meta
	WriteMeta(key string, m Meta) error
	Keys() []string // all keys currently present in the adapter
}

// RelayClient is the network surface. Real impl in this package wraps
// http.Client + config-derived bearer token.
type RelayClient interface {
	Get(ctx context.Context) ([]ServerItem, error)
	Put(ctx context.Context, items []ClientItem) ([]ServerItem, error)
}

type ServerItem struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt int64           `json:"updated_at"`
}

type ClientItem struct {
	Key             string          `json:"key"`
	Value           json.RawMessage `json:"value"`
	ClientUpdatedAt int64           `json:"client_updated_at"`
}

var syncedKeys = []string{
	"locale_preference",
	"quick_templates",
	"notifications_enabled",
	"ai_notifications_only",
	"command_notify_threshold_seconds",
	"shell_integration_enabled",
	"pinned_session_ids",
	"ssh_hosts_encrypted",
	"terminal_theme",
	"terminal_font_head",
	"terminal_font_size",
	"terminal_line_height",
	"terminal_cursor_style",
	"terminal_cursor_blink",
	"terminal_scrollback",
	"default_shell",
	"shortcut_bindings",
	"profiles_encrypted",
}

// SyncedKeys returns the canonical list of keys this engine syncs.
// The returned slice is a copy and safe to mutate.
func SyncedKeys() []string {
	out := append([]string(nil), syncedKeys...)
	sort.Strings(out)
	return out
}

// Engine is a single per-app sync engine instance. NOT safe for
// concurrent calls — wire it into a serial goroutine via the desktop
// boot code.
type Engine struct {
	adapter Adapter
	relay   RelayClient
}

func NewEngine(a Adapter, r RelayClient) *Engine {
	return &Engine{adapter: a, relay: r}
}

// Pull fetches the server state and reconciles into local. Per-key rule:
//   - server.updated_at > local.updated_at_local AND NOT dirty: adopt server
//   - server.updated_at > local.updated_at_local AND dirty: preserve local
//     (subsequent push will reconcile via LWW)
//   - server.updated_at <= local.updated_at_local: no-op
//   - key absent on server: leave local untouched
func (e *Engine) Pull(ctx context.Context) error {
	items, err := e.relay.Get(ctx)
	if err != nil {
		return err
	}
	for _, it := range items {
		local := e.adapter.ReadMeta(it.Key)
		if it.UpdatedAt > local.UpdatedAtLocal {
			if local.Dirty {
				continue
			}
			// Log-and-continue, not return: with the key count going 8 -> 17
			// (2026-08-17 prefs-sync-l1 final review, M2), a WriteValue error on
			// one key — e.g. a malformed shortcut_bindings payload — used to
			// abort the whole Pull and silently skip every key after it in the
			// response. One bad key must not take the rest of the sync down.
			if err := e.adapter.WriteValue(it.Key, it.Value); err != nil {
				logging.Warn("prefssync", "pull: write %s: %v", it.Key, err)
				continue
			}
			if err := e.adapter.WriteMeta(it.Key, Meta{UpdatedAtLocal: it.UpdatedAt, Dirty: false}); err != nil {
				logging.Warn("prefssync", "pull: write meta %s: %v", it.Key, err)
				continue
			}
		}
	}
	return nil
}

// MarkDirty stamps the meta entry for key with the given timestamp and
// flips Dirty=true. The desktop App should call this after each
// successful setter for a synced field, with timestamp = time.Now().UnixMilli().
//
// The stamp is forced strictly monotonic per key: two edits inside the same
// millisecond would otherwise share an UpdatedAtLocal, and that value is the
// signal Push uses to tell "nobody touched this key while the PUT was in
// flight" from "the user edited it again". Without the bump the second edit
// looks untouched and gets clobbered by the server echo.
func (e *Engine) MarkDirty(key string, updatedAtLocalMs int64) {
	if prev := e.adapter.ReadMeta(key); updatedAtLocalMs <= prev.UpdatedAtLocal {
		updatedAtLocalMs = prev.UpdatedAtLocal + 1
	}
	e.adapter.WriteMeta(key, Meta{UpdatedAtLocal: updatedAtLocalMs, Dirty: true})
}

// SeedFromLocal stamps Dirty=true (with updatedAtLocalMs) for every
// synced key that:
//   - has a value in the local adapter, AND
//   - is reported as non-default by isCustomized, AND
//   - has Meta{Dirty: false} currently
//
// Intended to run once per (relay user, device) after the first PULL,
// to carry pre-sync customizations up to the server.
func (e *Engine) SeedFromLocal(isCustomized func(key string) bool, updatedAtLocalMs int64) {
	for _, k := range e.adapter.Keys() {
		if _, ok := e.adapter.ReadValue(k); !ok {
			continue
		}
		if !isCustomized(k) {
			continue
		}
		m := e.adapter.ReadMeta(k)
		if m.Dirty {
			continue
		}
		e.adapter.WriteMeta(k, Meta{UpdatedAtLocal: updatedAtLocalMs, Dirty: true})
	}
}

// Push collects all dirty keys, sends them as a single PUT, and
// reconciles per-key with the server response (LWW: server's
// updated_at is authoritative).
func (e *Engine) Push(ctx context.Context) error {
	var items []ClientItem
	for _, k := range e.adapter.Keys() {
		m := e.adapter.ReadMeta(k)
		if !m.Dirty {
			continue
		}
		v, ok := e.adapter.ReadValue(k)
		if !ok {
			continue
		}
		items = append(items, ClientItem{
			Key: k, Value: v, ClientUpdatedAt: m.UpdatedAtLocal,
		})
	}
	if len(items) == 0 {
		return nil
	}

	// Remember what each key looked like when we serialized the request, so
	// the reconciliation below can tell an untouched key from one the user
	// edited while the round trip was in flight.
	sentAt := make(map[string]int64, len(items))
	for _, it := range items {
		sentAt[it.Key] = it.ClientUpdatedAt
	}

	resp, err := e.relay.Put(ctx, items)
	if err != nil {
		return err
	}

	for _, it := range resp {
		// A PUT to a remote relay can take seconds. If the user changed this
		// key in the meantime (MarkDirty stamped a newer UpdatedAtLocal),
		// applying the echo would overwrite the newer local value AND clear
		// its Dirty flag, so the edit would be lost locally and never pushed
		// — the pin the user just clicked would silently undo itself. Leave
		// the key alone; it is still dirty, so the next Push reconciles it.
		if e.adapter.ReadMeta(it.Key).UpdatedAtLocal != sentAt[it.Key] {
			continue
		}
		// Always trust server's updated_at; if it accepted our push, server.value == ours.
		// If server rejected (server newer), server.value overrides ours.
		if err := e.adapter.WriteValue(it.Key, it.Value); err != nil {
			return err
		}
		if err := e.adapter.WriteMeta(it.Key, Meta{UpdatedAtLocal: it.UpdatedAt, Dirty: false}); err != nil {
			return err
		}
	}
	return nil
}
