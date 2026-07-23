package main

// recoverySnapshotVersion is bumped whenever the on-disk JSON shape
// changes incompatibly. RecoveryStore.Load treats unknown versions
// as "no snapshot" (and deletes the file) — see §11 of the spec.
const recoverySnapshotVersion = 1

// RecoverySnapshot is the entire ~/.config/atterm/recovery.json document.
// Field tags use snake_case to match the spec; Wails/JSON wire shape
// is the canonical form so the frontend can JSON.parse it directly.
type RecoverySnapshot struct {
	Version       int           `json:"version"`
	HostID        string        `json:"host_id"`
	CleanShutdown bool          `json:"clean_shutdown"`
	SavedAtUnix   int64         `json:"saved_at_unix"`
	ActiveTabID   string        `json:"active_tab_id,omitempty"`
	Tabs          []TabSnapshot `json:"tabs"`
}

// TabSnapshot mirrors the frontend `Tab` type. Layout / col_ratio / row_ratio
// are restored verbatim; `id` is only used to map ActiveTabID → restored tab,
// the restored tab gets a fresh frontend id.
type TabSnapshot struct {
	ID            string         `json:"id"`
	Layout        string         `json:"layout"` // single | vertical | horizontal | grid2x2
	ActivePaneIdx int            `json:"active_pane_idx"`
	ColRatio      float64        `json:"col_ratio"`
	RowRatio      float64        `json:"row_ratio"`
	Panes         []PaneSnapshot `json:"panes"`
}

// PaneSnapshot describes a single pane. `shell` is the binary forked at
// NewSession time (i.e. the PTY child). `last_command_line` is the most
// recent OSC 133;C payload — used by aider for resume and by the dialog
// for display.
//
// Remote panes (Remote=true) skip the spawn path entirely: on restore the
// pane is rebound to SessionID on the remote host so the same long-running
// session resumes, instead of being replaced by a fresh local shell forked
// against Shell/LastCwd (which would land in a directory that doesn't even
// exist on the local machine). HostID is informational so the recovery
// dialog can group restored panes by their origin host.
//
// For local panes (Remote=false), SessionID instead carries the *previous
// generation's* session id — the local pane always gets a fresh id on
// respawn, so this field has no meaning to Go itself. It exists purely so
// the frontend can migrate pinned-session ids across the respawn (rename
// old id -> new id in useSessionPins) — see
// 2026-07-23-pinned-session-recovery-design.md §4.3.
type PaneSnapshot struct {
	Slot            int      `json:"slot"`
	Remote          bool     `json:"remote,omitempty"`
	HostID          string   `json:"host_id,omitempty"`
	SessionID       string   `json:"session_id,omitempty"`
	Shell           string   `json:"shell"`
	ShellArgs       []string `json:"shell_args,omitempty"`
	LastCwd         string   `json:"last_cwd,omitempty"`
	SessionType     string   `json:"session_type,omitempty"` // shell | ai | test | build | deploy
	LastCommandLine string   `json:"last_command_line,omitempty"`
	Title           string   `json:"title,omitempty"`
	AI              *AIInfo  `json:"ai,omitempty"`
}

// AIInfo carries the externally observed AI-side session ID (claude/codex
// only). SessionID may be empty when sniffing timed out; aider always has
// an empty SessionID because aider resumes by cwd, not by ID.
type AIInfo struct {
	Kind           string `json:"kind"` // claude | codex | aider
	SessionID      string `json:"session_id,omitempty"`
	CapturedAtUnix int64  `json:"captured_at_unix,omitempty"`
}
