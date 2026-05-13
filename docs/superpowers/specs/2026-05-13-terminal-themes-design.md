# Terminal Themes Design

## Goal

Add built-in terminal themes for the desktop app. Users can choose a theme in Settings, the app applies it globally to all terminal panes, and the choice persists across launches.

## Scope

In scope:

- Built-in themes only: `classic`, `nord`, `solarized-dark`, and `daylight`.
- Default remains `classic` so existing installs keep the current black terminal look.
- User preference persists in `~/.config/atterm/config.json` as `terminal_theme`.
- Theme changes apply immediately to open terminals and to terminals created later.
- The selected theme also updates the desktop shell colors used around the terminal, so light themes are readable.

Out of scope:

- Importing iTerm, VS Code, or other third-party theme files.
- User-defined custom color editing.
- Per-tab, per-pane, or per-session theme overrides.
- Automatic system light/dark following.
- Any protocol or relay changes.

## Architecture

The Go backend owns persistence and validates only the theme id. The frontend owns theme definitions because xterm.js consumes a JavaScript theme object and the app shell consumes CSS variables. This keeps the stored config small and forward-compatible: future releases can change built-in color values without migrating user config.

The desktop app exposes two Wails bindings:

- `GetTerminalTheme() string`
- `SetTerminalTheme(themeID string) error`

`SetTerminalTheme` rejects unknown ids and saves valid choices through the existing `configStore`.

## Components

### Go Config

`desktop/config.go` adds:

- `TerminalTheme string json:"terminal_theme,omitempty"` on `appConfig`.
- `TerminalThemeOrDefault() string`, returning `classic` unless the stored value is one of the supported ids.

The supported ids should live near the default helper, not in frontend-generated code, so backend validation is independent of the webview.

### Wails App API

`desktop/app.go` adds:

- `GetTerminalTheme`, which returns `cfg.TerminalThemeOrDefault()`.
- `SetTerminalTheme`, which validates the incoming id, preserves all unrelated config fields, and persists the new value.

This API must not restart sessions, touch relay/uplink state, or affect PTY behavior.

### Frontend Theme Registry

Create `desktop/frontend/src/lib/terminalThemes.ts` with:

- `TerminalThemeID` union type.
- `TerminalThemeDefinition` shape containing `id`, `label`, optional `description`, `xtermTheme`, and `appVars`.
- `TERMINAL_THEMES` array.
- `DEFAULT_TERMINAL_THEME_ID = "classic"`.
- `getTerminalTheme(id: string): TerminalThemeDefinition`, falling back to classic.

The xterm theme should include the core ANSI palette (`black`, `red`, `green`, `yellow`, `blue`, `magenta`, `cyan`, `white`, bright variants), `foreground`, `background`, `cursor`, `cursorAccent`, and `selectionBackground`.

The app CSS variable map should include at least:

- `--bg`
- `--panel`
- `--border`
- `--fg`
- `--fg-dim`
- `--accent`
- `--good`
- `--bad`
- terminal-adjacent variables if introduced for grid/cell/overlay readability.

### App State

`desktop/frontend/src/App.vue` owns the current theme id:

- On mount, call `getTerminalThemePreference()` before or alongside other startup reads.
- Apply `selectedTheme.appVars` to the app root with a style binding.
- Pass `selectedTheme.xtermTheme` to `PaneGrid`, then to `TerminalView`.

The existing off-screen xterm measure probe should use the same font settings as before. It does not need theme colors because FitAddon dimension prediction depends on font metrics and padding, not color.

### Terminal View

`desktop/frontend/src/components/TerminalView.vue` receives a `theme` prop and passes it to `new Terminal({ theme })`.

It also watches `props.theme` and updates existing terminals with `term.options.theme = props.theme`. The watcher must not recreate the terminal, reopen WebSockets, clear scrollback, or resize PTYs.

The `.term-view` and `.cell` backgrounds should use theme/app variables rather than hard-coded black where needed so `daylight` does not show black gutters.

### Settings UI

`desktop/frontend/src/components/SettingsDialog.vue` adds a “terminal theme” select:

- Load current theme via `getTerminalThemePreference()` during mount.
- Render one option per `TERMINAL_THEMES` entry.
- On change, call `setTerminalThemePreference(themeID)` immediately.
- If saving fails, show the existing error area and revert the select to the last persisted value.

This can be independent from the relay settings save button. Theme selection is a local desktop preference and should not require “save & connect”.

## Data Flow

Launch:

1. Go loads `appConfig`.
2. Frontend calls `GetTerminalTheme`.
3. Frontend resolves the id through `getTerminalTheme`.
4. `App.vue` applies app variables and passes the xterm theme through the pane tree.
5. Every `TerminalView` opens xterm with that theme.

Theme change:

1. User changes the Settings select.
2. Frontend calls `SetTerminalTheme(themeID)`.
3. Go validates and persists `terminal_theme`.
4. Frontend updates the reactive current theme id.
5. Open `TerminalView` instances update `term.options.theme` in place.

Bad stored value:

1. Go returns `classic` through `TerminalThemeOrDefault`.
2. Frontend also falls back to `classic` if it ever sees an unknown id.
3. The bad value is not required to be rewritten immediately; the next successful user selection will replace it.

## Theme Set

Initial built-ins:

- `classic`: current black-background terminal look; default.
- `nord`: cool, low-contrast dark palette.
- `solarized-dark`: classic warm low-contrast dark palette.
- `daylight`: light palette for bright environments.

Color values should be defined in TypeScript only, with Go validating ids only. The Settings label can show human-readable names such as “AT Term Classic”, “Nord / Arctic”, “Solarized Dark”, and “Daylight”.

## Error Handling

- Unknown theme id in `SetTerminalTheme` returns an error and does not write config.
- Missing config store returns an error, matching existing preference APIs.
- Frontend Settings keeps the prior theme if saving fails and displays the error.
- Unknown theme id in frontend helpers falls back to classic to keep the UI usable.

## Testing

Go tests:

- `appConfig.TerminalThemeOrDefault` returns `classic` for empty and unknown values.
- It returns each supported id unchanged.
- `SetTerminalTheme` persists a valid id without changing relay/update fields.
- `SetTerminalTheme` rejects invalid ids.

Frontend tests:

- Theme registry exposes all four theme ids and falls back to classic for unknown ids.
- `SettingsDialog.vue` source includes a terminal theme select wired to the theme preference API.
- `TerminalView.vue` source includes a `theme` prop and updates `term.options.theme` in a watcher.
- Existing build/type checks continue to pass.

Manual verification:

- Start desktop dev app.
- Open multiple panes.
- Change Settings theme to Nord and confirm all open panes update without reconnecting sessions.
- Restart app and confirm Nord remains selected.
- Change to Daylight and confirm topbar, grid gutters, overlay, Settings, and terminal text remain readable.

## Non-Goals And Compatibility

This feature does not alter protocol frames, relay behavior, session ids, PTY sizing, lazy uplink behavior, update verification, or web client CSP rules. Existing sessions remain attachable the same way; only local desktop rendering colors change.
