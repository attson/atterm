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
	"command_notify_threshold_seconds",
	"shell_integration_enabled",
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
	if err != nil { return err }
	for _, it := range items {
		local := e.adapter.ReadMeta(it.Key)
		if it.UpdatedAt > local.UpdatedAtLocal {
			if local.Dirty {
				continue
			}
			if err := e.adapter.WriteValue(it.Key, it.Value); err != nil { return err }
			if err := e.adapter.WriteMeta(it.Key, Meta{UpdatedAtLocal: it.UpdatedAt, Dirty: false}); err != nil { return err }
		}
	}
	return nil
}
