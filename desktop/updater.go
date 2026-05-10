package main

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// UpdateState is the observable view of the auto-update subsystem.
// Mirrored as a TypeScript interface in desktop/frontend/src/lib/api.ts.
type UpdateState struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	Available   bool   `json:"available"`
	Notes       string `json:"notes"`
	Checking    bool   `json:"checking"`
	LastCheckAt int64  `json:"last_check_at"`
	Downloading bool   `json:"downloading"`
	DownloadPct int    `json:"download_pct"`
	Ready       bool   `json:"ready"`
	Error       string `json:"error"`
	AssetURL    string `json:"asset_url"`
	AssetSize   int64  `json:"asset_size"`
	DownloadDir string `json:"download_dir"`
}

// updaterConfig is the constructor-time bag for Updater. Test code
// substitutes the http.Client and now() to drive the cache + network paths
// without hitting GitHub or wall-clock time.
type updaterConfig struct {
	current string
	repo    string // e.g. "attson/atterm"
	client  *http.Client
	now     func() time.Time // optional; defaults to time.Now
}

// Updater owns state for the auto-update flow. All methods are goroutine-safe.
type Updater struct {
	cfg updaterConfig

	mu       sync.Mutex
	state    UpdateState
	cachedAt time.Time // when we last fetched the latest-release manifest
}

func newUpdater(cfg updaterConfig) *Updater {
	if cfg.client == nil {
		cfg.client = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	return &Updater{
		cfg:   cfg,
		state: UpdateState{Current: cfg.current},
	}
}

// State returns a snapshot of the current state.
func (u *Updater) State() UpdateState {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.state
}

// devOrEmpty reports whether we should short-circuit out of all update logic.
func (u *Updater) devOrEmpty() bool {
	return u.cfg.current == "" || u.cfg.current == "dev"
}

// Check fetches the latest release. force=true bypasses the 1h response cache.
// In dev/empty builds it's a no-op that never touches the network.
func (u *Updater) Check(ctx context.Context, force bool) error {
	if u.devOrEmpty() {
		return nil
	}
	// real implementation lands in Task 5
	return nil
}
