---
description: Take over AT Term sessions from a browser, iPhone, or another computer and understand lazy upload, driver permissions, session state, and recovery.
---

# Remote takeover and session sidebar

## Lazy remote streaming

- Browser, desktop, and iOS clients can attach after the owner desktop connects to Relay.
- A remote client starts as a viewer. Press Space to request takeover before sending input.
- When no remote subscriber watches, desktop does not upload PTY bytes.
- The owner chooses `view`, `control`, or `full`; Relay and the desktop host both enforce the permission.
- Viewer counts and connection health expose current subscribers and RTT.

## Session sidebar

Pin important sessions, search title/cwd/current command, and group sessions by host. Pins are keyed by `session_id` and migrate when a recovered local pane receives a new ID. Search expands matching collapsed groups and shows an explicit empty state when nothing matches.

The [interactive demo](/en/) runs the real sidebar and terminal UI with an in-memory backend.

## Task state and notifications

OSC 133 produces running, waiting-input, completed, failed, and disconnected states. Commands and AI tools receive type chips, and templates can send frequent responses. System notifications, Web Push, and Feishu cards carry the session ID so a click returns to the right pane.

## Desktop widget

Settings can enable a small always-on-top widget that shows running, failed, and waiting sessions without connecting to Relay itself. The main app sends an already decrypted projection to the widget process. It supports local and remote sessions, collapse/temporary expansion, reduced motion, and temporary muting.

## Recovery

Desktop stores the tab/pane layout and captured AI session IDs in `~/.config/atterm/recovery.json`.

- Local shells respawn in the previous working directory.
- Remote panes rebind to the previous `session_id` and remain disconnected placeholders if the session has ended.
- Claude and Codex resume commands are written by the Go host at the first prompt; an unknown ID falls back to a normal shell rather than risking the wrong conversation.

Recovery can be disabled in Settings.

## Web and mobile

The Web client reuses the desktop App UI and adds browser auxiliary keys plus explicit file/image pickers. Sending input or files requires an attached driver and sufficient permission. The iOS Capacitor shell adds QR pairing, Keychain storage, IME handling, keyboard controls, and viewer-size locking.
