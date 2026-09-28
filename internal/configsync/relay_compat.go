package configsync

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
)

const relayCompatibilityStateVersion = 1

var (
	ErrInvalidRelayState  = errors.New("configsync: invalid relay compatibility state")
	ErrRelayTimestampFork = errors.New("configsync: relay timestamp maps to different values")
)

// RelayCompatibilityState is durable adapter state, not canonical config.
// Disabling Relay may stop using it, but must not delete the replica or Peer
// operations. PendingExportHash survives a process restart so the eventual
// Relay response is still recognized as an echo.
type RelayCompatibilityState struct {
	Version           int                      `json:"version"`
	RealmID           string                   `json:"realm_id"`
	LocalSeeded       bool                     `json:"local_seeded,omitempty"`
	MigrationComplete bool                     `json:"migration_complete,omitempty"`
	Keys              map[string]RelayKeyState `json:"keys"`
}

// RelayKeyState tracks the last value actually observed on Relay separately
// from a locally materialized value submitted to it.
type RelayKeyState struct {
	RelayValueHash        string `json:"relay_value_hash,omitempty"`
	RelayUpdatedAt        int64  `json:"relay_updated_at,omitempty"`
	PendingExportHash     string `json:"pending_export_hash,omitempty"`
	LastExportedValueHash string `json:"last_exported_value_hash,omitempty"`
}

// RelayValue is the schema-validated compatibility representation shared by
// pull, push, and tests. UpdatedAt is Relay's server timestamp on pull and the
// materialized local timestamp on push.
type RelayValue struct {
	Key       string
	Value     json.RawMessage
	UpdatedAt int64
}

// RelayImport applies one normalized Relay value by creating canonical signed
// operations. The callback must return only after those operations are
// durable; state advances afterward so a failed import remains retryable.
type RelayImport func(RelayValue) error

// RelayCompatibility turns Relay snapshots into canonical imports and plans
// canonical materialized values for Relay export. It never writes app config.
type RelayCompatibility struct {
	mu    sync.Mutex
	state RelayCompatibilityState
}

func NewRelayCompatibility(realmID string, state RelayCompatibilityState) (*RelayCompatibility, error) {
	if realmID == "" {
		return nil, fmt.Errorf("%w: empty realm", ErrInvalidRelayState)
	}
	if state.Version == 0 {
		state = RelayCompatibilityState{Version: relayCompatibilityStateVersion, RealmID: realmID, Keys: map[string]RelayKeyState{}}
	}
	if err := validateRelayCompatibilityState(state); err != nil {
		return nil, err
	}
	if state.RealmID != realmID {
		return nil, fmt.Errorf("%w: realm mismatch", ErrInvalidRelayState)
	}
	state.Keys = cloneRelayKeyStates(state.Keys)
	return &RelayCompatibility{state: state}, nil
}

// Import ingests a GET/PUT response. Values are processed independently in
// stable key order: one malformed key does not hide valid sibling values.
// The returned errors identify rejected keys while successful keys commit.
func (c *RelayCompatibility) Import(items []RelayValue, apply RelayImport) []error {
	c.mu.Lock()
	defer c.mu.Unlock()
	ordered := append([]RelayValue(nil), items...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Key < ordered[j].Key })
	seen := make(map[string]struct{}, len(ordered))
	var errs []error
	for _, item := range ordered {
		if _, duplicate := seen[item.Key]; duplicate {
			errs = append(errs, fmt.Errorf("%w: duplicate key %q", ErrInvalidSchemaValue, item.Key))
			continue
		}
		seen[item.Key] = struct{}{}
		if item.UpdatedAt <= 0 {
			errs = append(errs, fmt.Errorf("%w: %s: non-positive relay timestamp", ErrInvalidSchemaValue, item.Key))
			continue
		}
		normalized, err := NormalizeRelayValue(item.Key, item.Value)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		hash := relayValueHash(normalized)
		entry := c.state.Keys[item.Key]
		if item.UpdatedAt < entry.RelayUpdatedAt {
			continue
		}
		if item.UpdatedAt == entry.RelayUpdatedAt && entry.RelayValueHash != "" && hash != entry.RelayValueHash {
			errs = append(errs, fmt.Errorf("%w: %s at %d", ErrRelayTimestampFork, item.Key, item.UpdatedAt))
			continue
		}

		echo := hash == entry.PendingExportHash || hash == entry.RelayValueHash
		if !echo {
			if apply == nil {
				errs = append(errs, fmt.Errorf("%w: missing import callback for %s", ErrInvalidRelayState, item.Key))
				continue
			}
			if err := apply(RelayValue{Key: item.Key, Value: normalized, UpdatedAt: item.UpdatedAt}); err != nil {
				errs = append(errs, fmt.Errorf("import relay key %s: %w", item.Key, err))
				continue
			}
		}
		entry.RelayValueHash = hash
		entry.RelayUpdatedAt = item.UpdatedAt
		if hash == entry.PendingExportHash {
			entry.LastExportedValueHash = hash
			entry.PendingExportHash = ""
		} else if entry.PendingExportHash != "" {
			// Relay returned a competing winner. The imported winner becomes the
			// observed baseline; a still-winning Peer value will be planned again.
			entry.PendingExportHash = ""
		}
		c.state.Keys[item.Key] = entry
	}
	return errs
}

// PlanExports returns materialized values that differ from the most recently
// observed Relay winner. Planning records a pending hash but does not treat it
// as acknowledged, so a failed request is retried on the next plan.
func (c *RelayCompatibility) PlanExports(values []RelayValue) ([]RelayValue, []error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ordered := append([]RelayValue(nil), values...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Key < ordered[j].Key })
	seen := make(map[string]struct{}, len(ordered))
	out := make([]RelayValue, 0, len(ordered))
	var errs []error
	for _, value := range ordered {
		if _, duplicate := seen[value.Key]; duplicate {
			errs = append(errs, fmt.Errorf("%w: duplicate key %q", ErrInvalidSchemaValue, value.Key))
			continue
		}
		seen[value.Key] = struct{}{}
		if value.UpdatedAt <= 0 {
			errs = append(errs, fmt.Errorf("%w: %s: non-positive materialized timestamp", ErrInvalidSchemaValue, value.Key))
			continue
		}
		normalized, err := NormalizeRelayValue(value.Key, value.Value)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		hash := relayValueHash(normalized)
		entry := c.state.Keys[value.Key]
		if hash == entry.RelayValueHash {
			entry.PendingExportHash = ""
			c.state.Keys[value.Key] = entry
			continue
		}
		entry.PendingExportHash = hash
		c.state.Keys[value.Key] = entry
		out = append(out, RelayValue{Key: value.Key, Value: normalized, UpdatedAt: value.UpdatedAt})
	}
	return out, errs
}

func (c *RelayCompatibility) State() RelayCompatibilityState {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.state
	state.Keys = cloneRelayKeyStates(c.state.Keys)
	return state
}

func (c *RelayCompatibility) SetMigrationMarkers(localSeeded, complete bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.LocalSeeded = localSeeded
	c.state.MigrationComplete = complete
}

// ParseRelayCompatibilityState strictly reads persisted adapter state.
func ParseRelayCompatibilityState(raw []byte) (RelayCompatibilityState, error) {
	var state RelayCompatibilityState
	if err := decodeStrictJSON(raw, &state); err != nil {
		return RelayCompatibilityState{}, fmt.Errorf("%w: JSON: %v", ErrInvalidRelayState, err)
	}
	if err := validateRelayCompatibilityState(state); err != nil {
		return RelayCompatibilityState{}, err
	}
	state.Keys = cloneRelayKeyStates(state.Keys)
	return state, nil
}

func validateRelayCompatibilityState(state RelayCompatibilityState) error {
	if state.Version != relayCompatibilityStateVersion || state.RealmID == "" || state.Keys == nil {
		return fmt.Errorf("%w: version, realm, or keys", ErrInvalidRelayState)
	}
	for key, entry := range state.Keys {
		if _, ok := RelaySpec(key); !ok {
			return fmt.Errorf("%w: unknown key %q", ErrInvalidRelayState, key)
		}
		if entry.RelayUpdatedAt < 0 {
			return fmt.Errorf("%w: negative timestamp for %q", ErrInvalidRelayState, key)
		}
		for _, hash := range []string{entry.RelayValueHash, entry.PendingExportHash, entry.LastExportedValueHash} {
			if hash != "" && !validRelayValueHash(hash) {
				return fmt.Errorf("%w: hash for %q", ErrInvalidRelayState, key)
			}
		}
		if entry.RelayUpdatedAt > 0 && entry.RelayValueHash == "" {
			return fmt.Errorf("%w: timestamp without value hash for %q", ErrInvalidRelayState, key)
		}
	}
	return nil
}

func cloneRelayKeyStates(source map[string]RelayKeyState) map[string]RelayKeyState {
	out := make(map[string]RelayKeyState, len(source))
	for key, state := range source {
		out[key] = state
	}
	return out
}

func relayValueHash(value []byte) string {
	hash := sha256.Sum256(value)
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func validRelayValueHash(value string) bool {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && base64.RawURLEncoding.EncodeToString(decoded) == value
}
