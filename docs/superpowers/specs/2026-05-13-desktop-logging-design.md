# Desktop Logging Design

## Goal

Add persistent desktop-app logging that is enabled by default, rotates before the log grows too large, can be disabled by the user, supports changing the target path through a native file picker, and lets users inspect recent log content from Settings without opening the file manually.

## Scope

In scope:

- Desktop-app logging only. This covers the Go backend logs currently emitted through `log.Printf` and the existing desktop diagnostics added for uplink, repaint, and image paste troubleshooting.
- Default file logging enabled on first launch.
- Native file picker to change the log file path.
- User option to disable file logging entirely.
- Size-based log rotation: single file limit `10 MB`, keep `5` old files.
- Dev-build output to both terminal and file; release-build output to file only.
- A read-only log viewer in desktop Settings that shows recent log content from the current file.

Out of scope:

- Structured logging, log levels, or per-subsystem filtering.
- Live tail streaming, full-text search, or editing the log file in-app.
- Uploading logs to a server.
- Changing relay, protocol, PTY, or web-client behavior.

## Approach

Keep the existing `log.Printf` call sites and redirect the standard library logger through a desktop-owned logging manager. The logging manager chooses the current sinks, writes to the active file, performs size-based rotation, and can reopen the file when the user changes logging settings.

This keeps the behavior centralized and avoids plumbing a custom logger through the codebase.

## Default Paths

The logging manager computes a platform-specific default file path:

- macOS: `~/Library/Logs/AT-Term/desktop.log`
- Linux: `${XDG_STATE_HOME:-~/.local/state}/atterm/desktop.log`
- Windows: `%LocalAppData%\\ATTerm\\Logs\\desktop.log`

The user-visible default on macOS matches the path already discussed for local troubleshooting.

## Build Behavior

Build mode is inferred from the existing desktop version semantics:

- Dev build: `Version == "dev"` or empty version.
- Release build: any tagged version string.

Output policy:

- Dev build: write to the configured log file and mirror to the process stderr/stdout stream visible from `wails dev` or manual terminal launch.
- Release build: write to the configured log file only.

If file logging is disabled:

- Dev build falls back to terminal output only.
- Release build stops persistent logging entirely.

## Architecture

### Logging Config

Add persisted fields to `desktop/config.go`:

- `LogToFileEnabled *bool json:"log_to_file_enabled,omitempty"`
- `LogFilePath string json:"log_file_path,omitempty"`

Nil `LogToFileEnabled` means "never set" and defaults to `true` at read time so existing installs pick up logging automatically. Empty `LogFilePath` means "use the computed platform default".

Add helpers on `appConfig`:

- `LogToFileEnabledOrDefault() bool`
- `LogFilePathOrDefault() string`

These helpers keep old configs forward-compatible and avoid rewriting config on read.

### Logging Manager

Create a small desktop-local logging manager, for example in `desktop/logging.go`.

Responsibilities:

- Compute the current effective path.
- Ensure parent directories exist when file logging is enabled.
- Open the active log file.
- Wrap the file in a size-aware writer that rotates at `10 MB`.
- Set `log.SetOutput(...)` to the correct sink:
  - dev + file enabled: terminal + rotating file
  - dev + file disabled: terminal only
  - release + file enabled: rotating file only
  - release + file disabled: discard writer
- Switch sinks at runtime when Settings change.
- Expose helper methods for reading a preview of the current log file and for reporting the effective logging config back to the frontend.

The logging manager owns synchronization around file replacement and rotation so application goroutines can keep calling `log.Printf` without coordination.

### Rotation

Rotation is file-local and rename-based:

- Active file: `desktop.log`
- Rotated files: `desktop.log.1` through `desktop.log.5`

When the active file would exceed `10 MB` after a write:

1. Close the active file.
2. Delete `desktop.log.5` if present.
3. Rename `.4` to `.5`, `.3` to `.4`, `.2` to `.3`, `.1` to `.2`.
4. Rename `desktop.log` to `desktop.log.1`.
5. Create a fresh `desktop.log`.
6. Continue the triggering write into the new active file.

Rotation is based on file bytes, not line count or time.

### Startup Ordering

Logging initialization must happen before `wails.Run(...)` so early startup logs also reach the file. To do that cleanly:

- Extract config loading into a reusable helper that can be called before `NewApp()` finishes bootstrapping.
- Initialize the logging manager in `desktop/main.go`.
- Then construct the app and let `startup(...)` reuse the already-loaded config state or reload from the same source.

This is intentionally earlier than the current `a.cfgStore = loadConfig()` call in `desktop/app.go`.

### App And Wails API

Do not overload `RelayConfig`. Add a separate logging settings surface in `desktop/app.go`, for example:

- `GetLoggingConfig() LoggingConfig`
- `SetLoggingConfig(req LoggingConfig) error`
- `PickLogFilePath() (string, error)`
- `GetLogPreview() (LogPreview, error)`
- `OpenLogLocation() error`

Suggested shapes:

- `LoggingConfig`:
  - `enabled`
  - `path` (persisted path, may be empty when using default)
  - `effective_path`
  - `dev_dual_output`
- `LogPreview`:
  - `path`
  - `exists`
  - `truncated`
  - `content`

`PickLogFilePath` uses a native save-file dialog so the user chooses the destination file directly instead of typing a path. Canceling the dialog is not an error and must leave settings unchanged.

`OpenLogLocation` is a convenience action for Finder/Explorer/file manager reveal. It is not required for the logging feature to function but helps support workflows.

### Settings UI

Extend `desktop/frontend/src/components/SettingsDialog.vue` with a dedicated logging section.

Controls:

- Checkbox: `write logs to file`
- Read-only path display: current effective file path
- Button: `change location`
- Button: `reset default`
- Button: `view logs`
- Optional button: `show in finder`

Behavior:

- Changing the checkbox applies immediately through `SetLoggingConfig`.
- `change location` opens the native picker, then persists the selected path through `SetLoggingConfig`.
- `reset default` clears the persisted custom path and reapplies the computed platform default.
- The path display should reflect the effective path, not just the raw persisted value, so users can see where logs actually go.

### Log Viewer

The in-app log viewer is a lightweight modal, not a full page.

Behavior:

- Opening the viewer calls `GetLogPreview()`.
- The preview reads from the tail of the current active file only, not rotated history.
- The backend returns at most the last `256 KB` of content. If the file is larger, mark the preview as truncated.
- The viewer is read-only and supports:
  - `refresh`
  - `copy`
- The viewer does not auto-refresh or stream updates while open.

State handling:

- If file logging is disabled, show a clear message instead of empty content.
- If the current file does not exist yet, show a clear message that logs have not been written yet.
- If reading fails, surface the backend error in the existing dialog error area or a viewer-local error block.

## Data Flow

Launch:

1. Compute default log path for the current platform.
2. Load persisted config.
3. Initialize logging manager before `wails.Run(...)`.
4. Select the correct sink based on build mode and `log_to_file_enabled`.
5. Start the app normally; existing `log.Printf` calls now flow through the configured sink.

Enable/disable file logging:

1. User toggles the Settings checkbox.
2. Frontend calls `SetLoggingConfig`.
3. Backend validates and persists the change.
4. Logging manager reconfigures sinks immediately.
5. Frontend refreshes and shows the effective state.

Change path:

1. User clicks `change location`.
2. Frontend calls `PickLogFilePath`.
3. Backend opens the native picker and returns the chosen file path or an empty result on cancel.
4. Frontend persists the chosen path through `SetLoggingConfig`.
5. Logging manager opens the new file and switches future writes to it.

View logs:

1. User clicks `view logs`.
2. Frontend requests `GetLogPreview`.
3. Backend opens the active file, seeks near the end, reads up to the configured preview limit, and returns text plus metadata.
4. Frontend renders the content in a read-only modal.

## Error Handling

- If the configured custom path is invalid or its parent directory cannot be created, `SetLoggingConfig` returns an error and keeps the existing logger sink unchanged.
- If the backend cannot open the selected file when enabling file logging, the operation fails closed and the UI keeps the prior persisted settings.
- If rotation rename/create steps fail mid-rotation, the logging manager should preserve the still-open file when possible and continue best-effort logging rather than crashing the app.
- Dialog cancel from `PickLogFilePath` is a non-error no-op.
- `GetLogPreview` should never read arbitrary files outside the configured effective path.
- The log viewer must tolerate partial UTF-8 at the truncation boundary by replacing invalid sequences rather than failing the entire preview.

## Testing

Go tests:

- `appConfig.LogToFileEnabledOrDefault` defaults to true and preserves explicit true/false.
- `appConfig.LogFilePathOrDefault` returns the platform default when empty and preserves custom paths.
- Logging manager sink selection matches dev/release and enabled/disabled combinations.
- Writing beyond `10 MB` rotates files and caps retained history at `5`.
- `SetLoggingConfig` persists logging settings without disturbing relay/theme/update fields.
- Invalid file paths or directory creation failures return errors and leave the old sink active.
- `GetLogPreview` returns tail content, marks truncation correctly, and handles missing files cleanly.

Frontend tests:

- Settings source includes the logging toggle and location/view actions.
- Logging API wrappers exist in `desktop/frontend/src/lib/api.ts`.
- Log viewer source is present and wired to refresh and copy actions.

Manual verification:

- Dev mode: run `wails dev`, trigger diagnostics, confirm logs appear both in terminal and in the file.
- Release build on macOS: launch the app from Finder, trigger diagnostics, confirm logs are written to `~/Library/Logs/AT-Term/desktop.log`.
- Change the path through the picker and confirm new writes go to the new file.
- Disable file logging and confirm no new file bytes are written.
- Generate enough log volume to rotate and confirm `.1` through `.5` behavior.
- Open the in-app viewer and confirm recent content is readable without opening Finder.

## Non-Goals And Compatibility

This design intentionally leaves existing log call sites, relay behavior, protocol frames, PTY sizing, updater semantics, and web client behavior unchanged. It adds diagnostics persistence and viewing to the desktop app only.
