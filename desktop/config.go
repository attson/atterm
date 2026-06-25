package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/proto"
)

const (
	terminalThemeClassic       = "classic"
	terminalThemeNord          = "nord"
	terminalThemeSolarizedDark = "solarized-dark"
	terminalThemeDaylight      = "daylight"
	defaultShellAuto           = "auto"
)

const (
	localePreferenceSystem            = "system"
	localePreferenceEnglish           = "en"
	localePreferenceChineseSimplified = "zh-CN"
)

var terminalThemes = map[string]struct{}{
	terminalThemeClassic:       {},
	terminalThemeNord:          {},
	terminalThemeSolarizedDark: {},
	terminalThemeDaylight:      {},
}

// prefsMetaEntry tracks per-key sync state for the 5 synced preferences.
// Lives next to the values in config.json, but never sent to the relay.
type prefsMetaEntry struct {
	UpdatedAtLocal int64 `json:"updated_at_local"`
	Dirty          bool  `json:"dirty"`
}

// appConfig is what we persist to ~/.config/atterm/config.json.
// Empty fields mean "not configured" — RelayURL == "" disables uplink entirely.
type appConfig struct {
	RelayURL          string `json:"relay_url,omitempty"`
	RelaySessionToken string `json:"relay_session_token,omitempty"`
	// RelaySessionExpiresAt is the Unix-seconds expiry of the RelaySessionToken
	// minted by /api/pair/consume or /api/auth/login. Zero means "unknown".
	RelaySessionExpiresAt int64 `json:"relay_session_expires_at,omitempty"`
	// RelayLastEmail caches the email used in the most recent successful
	// LoginRemoteRelay call. Only used to prefill the Settings → Relay
	// form on next open; never used for authentication itself. Plaintext
	// is fine because the email is non-secret and lives next to the
	// session token in the same config.json.
	RelayLastEmail string `json:"relay_last_email,omitempty"`
	// RelaySessionUserID is the user id returned by /api/auth/login and
	// stored here so the first-login seed mechanism can key its
	// PrefsSeedMarkers entry by user id rather than email.
	RelaySessionUserID string `json:"relay_session_user_id,omitempty"`
	// RelayRealmID is the relay cluster's stable realm id (from the login
	// response). The account_key is anchored to it in the keychain so it
	// survives relay node/domain switches.
	RelayRealmID string `json:"relay_realm_id,omitempty"`
	// RelayHomeInstanceURL is the user's home relay node for this realm
	// (from the login response home_instance_url). The stateful /uplink WS
	// dials this node; empty falls back to RelayURL (single-instance).
	RelayHomeInstanceURL string `json:"relay_home_instance_url,omitempty"`
	// AllowInsecureRelay lets users opt into ws:// relays outside loopback.
	// It is off by default because ws:// exposes the bearer token and PTY data.
	AllowInsecureRelay bool `json:"allow_insecure_relay,omitempty"`
	// DisableE2EE turns off agent-side sealing for ALL sessions on this
	// desktop instance. The account_key stays loaded in memory so the JS
	// side can still decrypt frames originating from OTHER desktops, but
	// this desktop's agent stops sealing OUT / META / SessionInfo /
	// CommandEventPayload, so the relay sees plaintext content end to
	// end. Intended for testing / demo / regression of the unsealed
	// fallback path. Off by default; the user must opt in via Settings.
	DisableE2EE bool `json:"disable_e2ee,omitempty"`
	// RemotePermission is the default owner permission announced for this
	// desktop's sessions on the remote relay.
	RemotePermission string `json:"remote_permission,omitempty"`
	// RelayPaused is the user-toggled pause flag. Zero value (false) preserves
	// the existing "has URL → connect" behavior, so old config.json files
	// deserialize correctly without any migration code.
	RelayPaused bool `json:"relay_paused,omitempty"`
	// LocalePreference controls UI language. Empty means "system" so older
	// configs keep following the OS/browser language after upgrade.
	LocalePreference string `json:"locale_preference,omitempty"`
	// LocalAdminPassword is the stable password for the desktop-only admin
	// user (local@atterm.local) that owns the in-process mini-relay's
	// userstore. Generated on first run and persisted so the desktop can
	// re-mint a fresh session token on every launch. This file lives in
	// the user's config dir; it is NOT secret from the user, but it is
	// the only key holders of this machine's local relay sessions.
	LocalAdminPassword string `json:"local_admin_password,omitempty"`
	// TerminalTheme is the user's global desktop terminal theme preference.
	// Unknown values fall back to classic so older configs remain usable.
	TerminalTheme string `json:"terminal_theme,omitempty"`
	// DefaultShell selects the shell used for new local sessions. Empty or
	// "auto" lets AT Term choose the first supported shell available locally.
	DefaultShell string `json:"default_shell,omitempty"`
	// LogToFileEnabled controls whether desktop logs are persisted to a file.
	// Nil means "never set" and defaults to true for existing installs.
	LogToFileEnabled *bool `json:"log_to_file_enabled,omitempty"`
	// LogFilePath overrides the platform default desktop log file path.
	// Empty means "use the platform default".
	LogFilePath string `json:"log_file_path,omitempty"`

	// Auto-update settings. Nil means "never set" → treated as default true
	// at read time. Stored as a pointer so we can distinguish "user opted
	// out" from "fresh install".
	AutoCheckUpdates *bool  `json:"auto_check_updates,omitempty"`
	LastCheckAt      int64  `json:"last_check_at,omitempty"`
	SkipVersion      string `json:"skip_version,omitempty"`
	UpdateGHProxyURL string `json:"update_gh_proxy_url,omitempty"`

	// NotificationsEnabled controls whether terminal BEL (\x07) triggers a
	// native system notification when the window is unfocused. Nil means
	// "never set" and defaults to true for existing installs.
	NotificationsEnabled *bool `json:"notifications_enabled,omitempty"`

	// PtyInputDebugEnabled logs every byte slice written into a session PTY
	// (hex, tagged [pty-input] at DEBUG) for diagnosing stuck/dropped input.
	PtyInputDebugEnabled *bool `json:"ptyInputDebugEnabled,omitempty"`

	// ShellIntegrationEnabled controls whether atterm-spawned shells receive
	// OSC 133 hook injection at spawn time. Nil means "never set" and
	// defaults to true for existing installs. Only affects new sessions;
	// already-running PTYs keep their current behavior.
	ShellIntegrationEnabled *bool `json:"shell_integration_enabled,omitempty"`

	// RecoveryDialogEnabled gates the startup recovery dialog. Nil means
	// "never set" → default true. Stored as pointer so we can distinguish
	// "user opted out" from "fresh install".
	RecoveryDialogEnabled *bool `json:"recovery_dialog_enabled,omitempty"`

	// HookAutoInstallEnabled controls whether the desktop materializes
	// the atterm-hook binary and patches ~/.claude/settings.json on
	// startup. Nil means "never set" → default true for fresh installs.
	HookAutoInstallEnabled *bool `json:"hook_auto_install_enabled,omitempty"`
	// CommandNotifyThresholdSeconds gates the command-finished notification:
	// commands shorter than this duration (start-to-finish) do not produce
	// a notification. Nil → default 10. Clamped to [1, 600] at read time.
	CommandNotifyThresholdSeconds *int `json:"command_notify_threshold_seconds,omitempty"`

	// WebglRendererEnabled controls whether xterm loads xterm-addon-webgl
	// for GPU-accelerated rendering. Nil means "never set" and resolves to
	// the platform default: false on Linux (NVIDIA proprietary + X11 +
	// WebKitGTK paint the cursor / last-cell on a delayed schedule that
	// surfaces as visible typing lag even when CPU is idle — #48), true
	// on macOS/Windows so the #33 light-theme ghosting fix stays active.
	WebglRendererEnabled *bool `json:"webgl_renderer_enabled,omitempty"`

	// Plugins is the plugin-system block. Defaults filled in on first run.
	Plugins PluginConfig `json:"plugins"`

	// QuickTemplates persists the user's quick-action button list. Empty
	// or absent means "use defaults" — the renderer seeds DEFAULT_TEMPLATES
	// in that case. See docs/superpowers/specs/2026-06-04-quick-templates-design.md.
	QuickTemplates []QuickTemplate `json:"quick_templates,omitempty"`

	// TaskPreset is the user's chosen session-bar display style.
	// Empty falls back to "iconOnly" at read time.
	TaskPreset string `json:"task_preset,omitempty"`
	// TaskGroupBy controls how the session bar groups rows: "host" (per
	// machine, default) or "state" (per task_state). Empty falls back to
	// "host" at read time.
	TaskGroupBy string `json:"task_group_by,omitempty"`
	// TaskSidebarCollapsed records whether the task sidebar panel is collapsed.
	// Zero value (false) means expanded, which is the default.
	TaskSidebarCollapsed bool `json:"task_sidebar_collapsed,omitempty"`
	// TaskSidebarWidth is the persisted pixel width of the task sidebar
	// (expanded mode). 0 means "use default 240"; valid range enforced
	// on Set in app.go.
	TaskSidebarWidth int `json:"task_sidebar_width,omitempty"`

	// PrefsMeta records per-key sync state (last local update timestamp and
	// dirty flag) for the synced preferences. Never sent to the relay.
	PrefsMeta map[string]prefsMetaEntry `json:"prefs_meta,omitempty"`
	// PrefsSeedMarkers records whether the first-login seed upload has run
	// for a given relay user id. Keyed by user id to survive logout/login.
	PrefsSeedMarkers map[string]bool `json:"prefs_seed_markers,omitempty"`
}

// AutoCheckUpdatesOrDefault returns the user's preference, defaulting to
// true when the field has never been set (fresh installs).
func (c appConfig) AutoCheckUpdatesOrDefault() bool {
	if c.AutoCheckUpdates == nil {
		return true
	}
	return *c.AutoCheckUpdates
}

func (c appConfig) NotificationsEnabledOrDefault() bool {
	if c.NotificationsEnabled == nil {
		return true
	}
	return *c.NotificationsEnabled
}

func (c appConfig) PtyInputDebugEnabledOrDefault() bool {
	if c.PtyInputDebugEnabled == nil {
		return false
	}
	return *c.PtyInputDebugEnabled
}

func (c appConfig) RecoveryDialogEnabledOrDefault() bool {
	if c.RecoveryDialogEnabled == nil {
		return true
	}
	return *c.RecoveryDialogEnabled
}

func (c appConfig) HookAutoInstallEnabledOrDefault() bool {
	if c.HookAutoInstallEnabled == nil {
		return true
	}
	return *c.HookAutoInstallEnabled
}

func (c appConfig) RemotePermissionOrDefault() string {
	switch c.RemotePermission {
	case proto.RemotePermissionView, proto.RemotePermissionControl, proto.RemotePermissionFull:
		return c.RemotePermission
	default:
		return proto.RemotePermissionFull
	}
}

func (c appConfig) TerminalThemeOrDefault() string {
	if isSupportedTerminalTheme(c.TerminalTheme) {
		return c.TerminalTheme
	}
	return terminalThemeClassic
}

// taskPresetDefault is the preset used when TaskPreset has never been set.
const taskPresetDefault = "iconOnly"

func (c appConfig) TaskPresetOrDefault() string {
	switch c.TaskPreset {
	case "iconOnly", "iconLabel":
		return c.TaskPreset
	default:
		return taskPresetDefault
	}
}

// taskGroupByDefault is what the session bar uses when TaskGroupBy is unset.
const taskGroupByDefault = "host"

func (c appConfig) TaskGroupByOrDefault() string {
	switch c.TaskGroupBy {
	case "host", "state":
		return c.TaskGroupBy
	default:
		return taskGroupByDefault
	}
}

const defaultTaskSidebarWidth = 240

func (c appConfig) TaskSidebarWidthOrDefault() int {
	if c.TaskSidebarWidth == 0 {
		return defaultTaskSidebarWidth
	}
	return c.TaskSidebarWidth
}

func (c appConfig) LocalePreferenceOrDefault() string {
	switch c.LocalePreference {
	case localePreferenceSystem, localePreferenceEnglish, localePreferenceChineseSimplified:
		return c.LocalePreference
	default:
		return localePreferenceSystem
	}
}

// PrefsSeedMarkerFor reports whether the first-login seed upload has
// already run for the given relay user id. Lives separately from
// PrefsMeta so logout/login doesn't have to touch the meta map.
func (c appConfig) PrefsSeedMarkerFor(userID string) bool {
	if c.PrefsSeedMarkers == nil {
		return false
	}
	return c.PrefsSeedMarkers[userID]
}

func (c appConfig) DefaultShellOrDefault() string {
	shell := strings.TrimSpace(c.DefaultShell)
	if shell == "" || strings.EqualFold(shell, defaultShellAuto) {
		return defaultShellAuto
	}
	return shell
}

func (c appConfig) LogToFileEnabledOrDefault() bool {
	if c.LogToFileEnabled == nil {
		return true
	}
	return *c.LogToFileEnabled
}

func (c appConfig) LogFilePathOrDefault() string {
	if c.LogFilePath != "" {
		return c.LogFilePath
	}
	return defaultLogFilePath()
}

func (c appConfig) ShellIntegrationEnabledOrDefault() bool {
	if c.ShellIntegrationEnabled == nil {
		return true
	}
	return *c.ShellIntegrationEnabled
}

// WebglRendererEnabledOrDefault returns the user's preference for the xterm
// WebGL renderer. See the field comment for the rationale behind the
// platform-specific default.
func (c appConfig) WebglRendererEnabledOrDefault() bool {
	if c.WebglRendererEnabled == nil {
		return defaultWebglRendererEnabledFor(runtime.GOOS)
	}
	return *c.WebglRendererEnabled
}

// defaultWebglRendererEnabledFor is split out from WebglRendererEnabledOrDefault
// so tests can exercise every platform branch without GOOS injection tricks.
func defaultWebglRendererEnabledFor(goos string) bool {
	return goos != "linux"
}

func (c appConfig) CommandNotifyThresholdSecondsOrDefault() int {
	const (
		minSec     = 1
		maxSec     = 600
		defaultSec = 10
	)
	if c.CommandNotifyThresholdSeconds == nil {
		return defaultSec
	}
	v := *c.CommandNotifyThresholdSeconds
	if v < minSec {
		return minSec
	}
	if v > maxSec {
		return maxSec
	}
	return v
}

func isSupportedTerminalTheme(theme string) bool {
	_, ok := terminalThemes[theme]
	return ok
}

func isSupportedTaskPreset(p string) bool {
	switch p {
	case "iconOnly", "iconLabel":
		return true
	}
	return false
}

func isSupportedTaskGroupBy(p string) bool {
	switch p {
	case "host", "state":
		return true
	}
	return false
}

func supportedTerminalThemes() []string {
	return []string{
		terminalThemeClassic,
		terminalThemeNord,
		terminalThemeSolarizedDark,
		terminalThemeDaylight,
	}
}

func configPath() string {
	dir, err := appdir.ConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "config.json")
}

// configStore is a thin lock-protected wrapper around appConfig with disk I/O.
type configStore struct {
	mu  sync.Mutex
	cfg appConfig
}

func loadConfig() *configStore {
	s := &configStore{}
	p := configPath()
	if p == "" {
		return s
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s.cfg)
	applyConfigDefaults(&s.cfg)
	return s
}

// Get returns a snapshot of the current config.
func (s *configStore) Get() appConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// Set replaces the config and persists it to disk atomically (write-temp-rename).
func (s *configStore) Set(c appConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = c
	p := configPath()
	if p == "" {
		return errors.New("user config dir unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// applyConfigDefaults fills nil/zero fields with their defaults. Called from
// loadConfig() after JSON unmarshal. Idempotent.
func applyConfigDefaults(c *appConfig) {
	c.Plugins.applyDefaults()
}
