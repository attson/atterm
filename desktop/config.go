package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

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

// appConfig is what we persist to ~/.config/atterm/config.json.
// Empty fields mean "not configured" — RelayURL == "" disables uplink entirely.
type appConfig struct {
	RelayURL   string `json:"relay_url,omitempty"`
	RelayToken string `json:"relay_token,omitempty"`
	// AllowInsecureRelay lets users opt into ws:// relays outside loopback.
	// It is off by default because ws:// exposes the bearer token and PTY data.
	AllowInsecureRelay bool `json:"allow_insecure_relay,omitempty"`
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

	// ShellIntegrationEnabled controls whether atterm-spawned shells receive
	// OSC 133 hook injection at spawn time. Nil means "never set" and
	// defaults to true for existing installs. Only affects new sessions;
	// already-running PTYs keep their current behavior.
	ShellIntegrationEnabled *bool `json:"shell_integration_enabled,omitempty"`
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
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "atterm", "config.json")
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
