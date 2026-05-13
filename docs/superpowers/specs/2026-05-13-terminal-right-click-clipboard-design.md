# Terminal Right-Click Clipboard Design

## Goal

Add desktop-only right-click copy/paste support in terminal panes so users can use a contextual menu instead of memorizing keyboard shortcuts, while preserving existing terminal semantics for text paste and extending the same entry point to image paste.

## Scope

In scope:

- Desktop app only. No web-client changes.
- Terminal-pane right-click menu with exactly two actions: `copy` and `paste`.
- Text copy via the existing terminal selection logic.
- Text paste via xterm's paste path so bracketed paste behavior stays intact.
- Image paste from the system clipboard through the existing `PASTE_IMAGE` session protocol path.
- Explicit disabled/error states when the current session or clipboard contents do not permit the requested action.

Out of scope:

- Browser-default context menus.
- Global app-level context menus outside terminal panes.
- New protocol frame types or protocol version changes.
- Arbitrary clipboard formats other than plain text and images.
- Web-client support in the same change.

## Constraints

- Do not add new frontend dependencies.
- Keep wire compatibility: reuse the existing `PASTE_IMAGE` frame and `IN` text-input path.
- Respect the existing remote-permission model:
  - `view`: no input, no resize, no image paste
  - `control`: text input allowed, image paste blocked
  - `full`: text input and image paste allowed
- Preserve current keyboard behavior:
  - existing copy shortcut remains unchanged
  - existing keyboard/image paste event handling remains unchanged

## User Experience

### Menu Behavior

Right-clicking inside a terminal pane opens a small contextual menu with:

- `copy`
- `paste`

The menu closes when:

- the user clicks elsewhere
- the user presses `Esc`
- the pane loses focus
- the current session disconnects or the view unmounts
- the user picks an action

The menu is positioned near the pointer and clamped to the viewport so it never renders off-screen.

### Copy

- Enabled only when the terminal has a non-empty selection.
- Uses the existing `copyTerminalSelection(...)` flow.
- If copy fails, the menu closes and the existing warning-style logging pattern is used.

### Paste

`paste` is a single action with automatic clipboard detection.

Clipboard precedence:

1. image
2. text

If the clipboard contains both image and text, the action is treated as an image paste. It does not silently downgrade to text paste.

Permission behavior:

- `view`: `paste` is disabled.
- `control`: `paste` is enabled, but image clipboard content is rejected with a clear message because image paste requires `full`.
- `full`: `paste` supports image or text according to clipboard precedence.

This keeps the behavior explicit and consistent with the existing permission model.

## Approach

Use a custom frontend context menu rendered only for terminal panes, backed by a new desktop binding that inspects the native clipboard and returns one normalized payload describing the best paste candidate.

This approach is preferred over relying on the WebView or OS default context menu because:

- xterm content is not a normal editable field, so default clipboard behavior is inconsistent across platforms
- the app already has terminal-specific paste semantics for both text and images
- the permission model lives above the OS clipboard layer and needs app-level enforcement

## Architecture

### Frontend Menu Ownership

The menu state lives in `desktop/frontend/src/components/TerminalView.vue` because:

- right-click behavior is terminal-specific
- enable/disable state depends on the active terminal instance and session metadata
- it avoids introducing a global menu framework for a two-action feature

However, the menu UI should not be rendered inside the terminal pane DOM tree directly because `.term-view` uses `overflow: hidden`. To avoid clipping, render the menu with Vue `Teleport` to `document.body` and use `position: fixed`.

Tracked local state:

- `menuOpen`
- `menuX`, `menuY`
- `hasSelection`
- `pasteBusy`
- last known session status

### Frontend Clipboard Helpers

Add a small helper module, for example `desktop/frontend/src/lib/terminalPaste.ts`, mirroring the role of `terminalCopy.ts`.

Responsibilities:

- request the normalized clipboard payload from the desktop binding
- decide whether the current session permissions allow the payload kind
- route text payloads to `term.paste(text)`
- route image payloads to `conn.sendPasteImage(...)`
- return structured failure reasons for UI messaging and console diagnostics

This keeps `TerminalView.vue` focused on event wiring and presentation instead of clipboard-type branching.

### Desktop Clipboard Payload API

Add a desktop binding in `desktop/app.go`:

- `GetClipboardPastePayload() (ClipboardPastePayload, error)`

Suggested payload shape:

- `kind: "none" | "text" | "image"`
- `text`
- `filename`
- `content_type`
- `data_base64`
- `reason`

Semantics:

- `image` means the backend found clipboard image data and normalized it for the frontend
- `text` means no usable image was chosen, but plain text is available
- `none` means neither image nor text is available, or the clipboard read failed in a way that still allows a graceful UI response
- `reason` is for user-facing hints and logging, not protocol transport

The backend is the authority for image-vs-text precedence. The frontend should not attempt its own independent clipboard inspection for the menu path.

### Session Metadata

The frontend `SessionInfo` type currently does not expose `remote_permission` even though it exists on the protocol. Add that field to the frontend session model and thread the effective permission down to `TerminalView`.

This is needed so the menu can:

- disable paste for `view`
- reject image payloads locally for `control`
- avoid unnecessary round-trips that the relay or host would reject anyway

No protocol changes are required; this is only frontend type plumbing.

## Native Clipboard Strategy

### Text

Text paste uses existing platform support already present through Wails/runtime-level clipboard access. The desktop binding may reuse that for the text fallback path.

### Image

Image paste requires a new desktop-local clipboard read path because the right-click menu does not have access to a browser `ClipboardEvent`.

Implement a platform-specific clipboard reader alongside the existing image-paste bridge code in `desktop/paste_image.go` or a sibling file.

Requirements:

- inspect the native clipboard for image content
- normalize the result into a supported image format for transport, preferably PNG when conversion is practical
- base64-encode the bytes for the frontend binding payload
- supply a stable synthetic filename such as `clipboard-image.png`
- enforce the existing `10 MB` image size limit before returning `kind=image`

Platform policy:

- macOS: use native system facilities to read clipboard image data and return normalized bytes
- Windows: use native system facilities or PowerShell-backed helpers already acceptable in this codebase to read clipboard image data and return normalized bytes
- Linux: prefer Wayland clipboard tools first, then X11 tools, matching the existing "best available platform helper" style already used for image clipboard write

Fallback behavior:

- if image read is unsupported or unavailable on the current system, continue checking for text
- if image read succeeds but exceeds the size limit, return `none` with a size-related reason rather than silently converting to text

## Data Flow

### Open Menu

1. User right-clicks inside a terminal pane.
2. `TerminalView` prevents the browser default `contextmenu`.
3. The component snapshots terminal selection state and current session permission.
4. The contextual menu is shown at the clamped pointer position.

### Copy

1. User clicks `copy`.
2. Frontend reuses `copyTerminalSelection(term)`.
3. Menu closes.
4. Failures are logged through the existing frontend warning pattern.

### Paste Text

1. User clicks `paste`.
2. Frontend calls `GetClipboardPastePayload()`.
3. Backend returns `kind=text`.
4. Frontend calls `term.paste(text)`.
5. xterm emits the corresponding input with bracketed-paste semantics when enabled by the shell.
6. Menu closes.

### Paste Image

1. User clicks `paste`.
2. Frontend calls `GetClipboardPastePayload()`.
3. Backend returns `kind=image` with base64 bytes and metadata.
4. Frontend checks session permission:
   - if not `full`, show a clear error and stop
   - if `full`, convert the payload back into a `Blob`
5. Frontend calls existing `conn.sendPasteImage(blob, filename)`.
6. Existing relay/desktop host flow handles `PASTE_IMAGE` unchanged.
7. Menu closes.

### No Usable Clipboard Payload

1. User clicks `paste`.
2. Backend returns `kind=none`.
3. Frontend closes the menu and shows a short message derived from `reason`.

## Error Handling

- `copy` without a selection is disabled, not a no-op action.
- `paste` is disabled when the session is not currently attached.
- `paste` is disabled for `view` permission sessions.
- `kind=image` on a `control` session is rejected locally with a specific message such as `image paste requires full remote permission`.
- Clipboard read failures should produce concise diagnostics in logs and a short user-facing message; they must not crash the terminal view.
- Invalid base64 or malformed image payloads from the desktop binding are treated as internal errors and logged before showing a generic failure message.
- If `conn.sendPasteImage(...)` fails, preserve the existing error path and diagnostics already used for image-paste troubleshooting.

## Logging

Add targeted logs on the new menu-driven paste path so image-paste failures remain diagnosable:

- menu paste requested
- clipboard payload resolved (`kind`, content type, byte size when image)
- permission rejection before send
- frontend image-paste send failure
- backend native clipboard read failure and fallback decisions

Logs should describe what decision was made and why, without dumping clipboard text content.

## Testing

Frontend tests:

- `copy` enabled only with selection.
- `paste` disabled when session status is not attached.
- `paste` disabled for `view` permission.
- image payload on `control` permission is rejected locally.
- text payload goes through `term.paste(...)`, not direct socket input.
- image payload becomes a `Blob` and calls `sendPasteImage(...)`.
- menu positioning clamps within the viewport.
- outside click and `Esc` close the menu.

Go tests:

- `GetClipboardPastePayload` returns text when only text exists.
- image payload wins when both image and text are available.
- oversized image returns `kind=none` with a size-related reason.
- unsupported image clipboard read falls back to text when text exists.
- Linux helper ordering prefers Wayland first, then X11, matching existing clipboard-helper style.

Regression expectations:

- existing keyboard copy shortcut still works
- existing keyboard/image paste event flow still works
- no protocol docs change is needed because no wire format changes are introduced

## Implementation Notes

- Keep all clipboard-read code desktop-local. Do not move clipboard-specific logic into `internal/relay`.
- Reuse the existing `maxPasteImageBytes` policy rather than introducing a second limit.
- Prefer narrow, testable helper functions over embedding platform branching directly in `App.GetClipboardPastePayload`.
- Keep the first version intentionally small: no submenu, no select-all, no clear-screen, no editable menu labels.
