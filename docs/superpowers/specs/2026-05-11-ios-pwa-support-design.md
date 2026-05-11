# iOS PWA Support Design

## Goal

Make atterm usable from iPhone Safari as an installable web app for the core mobile handoff scenario: open a remote relay, see live sessions, attach to one, read history, and keep typing while away from the desktop.

This is web/PWA support, not a native iOS app. The existing relay-hosted `web/` client remains the delivery vehicle.

## Scope

### In scope

- Mobile-first layout for `web/` on iPhone Safari.
- Installable PWA metadata: manifest, iOS home-screen meta tags, icons, theme colors.
- Session list optimized for touch: single-column cards, large hit targets, clear connection/token state.
- Terminal view optimized for mobile: stable viewport sizing, safe-area handling, readable font defaults, reconnect status.
- Mobile shortcut bar for common terminal control keys: `Esc`, `Tab`, `Ctrl-C`, `Ctrl-D`, arrow keys, and paste.
- Keep desktop browser compatibility for the existing `web/` client.
- Keep the current relay protocol and endpoints unchanged.

### Out of scope

- Native Swift/SwiftUI iOS app.
- App Store, TestFlight, signing, push notifications, background execution, or iCloud/keychain integration.
- Running a local PTY on iOS.
- Offline terminal history or offline input queueing.
- Protocol changes.
- User accounts or multi-tenant auth.

## Current State

`web/` is a vanilla HTML/CSS/JS client served by `atterm-relay --web web`. It uses:

- `GET /api/sessions` for the session list.
- `GET /client` WebSocket for attach, input, resize, output, close, and reconnect.
- Query-string token support for browser compatibility.
- xterm.js and fit addon from CDN.

The current client works as a basic browser client, but it is not iOS-first: the header, token field, terminal viewport, and control-key entry are not tuned for touch or home-screen usage.

## Recommended Approach

Build a focused iOS-first PWA layer on top of the existing `web/` client.

Keep the implementation lightweight and dependency-free:

- Continue using vanilla JS/CSS in `web/`.
- Continue loading xterm.js from CDN for this phase.
- Add PWA metadata and static assets under `web/`.
- Refactor only enough JS to make mobile behavior clear and testable.

This approach gives immediate value on iPhone without introducing a second client stack. It also keeps the same web client usable from Android and desktop browsers.

## User Experience

### Entry and configuration

The user opens the relay URL in Safari, for example:

```text
https://relay.example.com/?token=...
```

If a token is present in the query string, the client stores it in `localStorage` and hides sensitive token text by default. If no token is configured, the header exposes a compact token input.

The app can be added to the iOS home screen. In home-screen mode it should feel app-like: full viewport, safe-area-aware top/bottom bars, no unnecessary browser chrome assumptions.

### Session list

On iPhone widths, the list is a single-column touch layout:

- Header shows `atterm`, connection status, and a compact token/settings affordance.
- Cards show host/user, command, cwd, terminal size, and short session id.
- Empty and error states are explicit and actionable.
- Pull-to-refresh is not required; list polling remains acceptable for v1.

On wider screens, keep the existing responsive multi-column card layout.

### Terminal view

The terminal view uses the full available viewport:

- Top bar: back button, connection status, abbreviated session id or command.
- Terminal body: xterm.js sized with `visualViewport`/resize handling where available, plus CSS safe-area insets.
- Bottom shortcut bar: touch buttons for keys that iOS software keyboards make hard to type.

Shortcut behavior:

| Button | Sends |
|--------|-------|
| `Esc` | `\x1b` |
| `Tab` | `\t` |
| `Ctrl-C` | `\x03` |
| `Ctrl-D` | `\x04` |
| `←` | `\x1b[D` |
| `↓` | `\x1b[B` |
| `↑` | `\x1b[A` |
| `→` | `\x1b[C` |
| `Paste` | Clipboard text via `navigator.clipboard.readText()` when available; otherwise show a small paste input fallback. |

The bar should be horizontally scrollable if needed, with large enough touch targets for thumb use.

## Architecture

### Files

- `web/index.html`: add PWA metadata, manifest links, iOS meta tags, updated layout containers.
- `web/style.css`: mobile-first layout, safe-area CSS variables, terminal sizing, shortcut bar styling, desktop responsive rules.
- `web/app.js`: routing, session list, terminal attach, resize handling, shortcut actions, clipboard fallback.
- `web/manifest.webmanifest`: PWA metadata.
- `web/icon.svg`: source icon for the manifest and iOS link tags.
- Optional generated PNG icons if Safari requires better home-screen rendering.

### Data flow

The data flow stays unchanged:

```text
Safari/PWA -> relay /api/sessions -> JSON session list
Safari/PWA -> relay /client WebSocket -> ATTACH/IN/RESIZE/OUT/CLOSE
```

The client remains a pure remote controller. It never owns PTYs and never changes lazy uplink semantics.

### State model

The web client keeps the same small client-side state:

- `token`: persisted in `localStorage`.
- `currentSID`: session id currently attached.
- `lastSeq`: last received OUT sequence for reconnect replay.
- `currentWS`: active WebSocket.
- `listTimer`: session-list polling timer.
- `reconnectAttempts`: exponential backoff counter.

Mobile UI state should be derived from route and connection status rather than introducing a framework.

## Error Handling

- `401` from `/api/sessions`: show `unauthorized`, keep token control visible.
- Other non-OK list responses: show `http <status>`.
- Fetch/network failure: show `offline` and keep polling.
- WebSocket close while still on session route: show `reconnecting...` and retry with existing backoff.
- WebSocket close after navigation away: do not reconnect.
- Clipboard API failure: show paste fallback input instead of silently failing.
- xterm resize before socket open: send current size after `ATTACH` on open, as the existing client already does.

## Security and Privacy

- Token may remain in query string for compatibility, but should be stored in `localStorage` and not rendered in clear text.
- No service worker caching of session output, tokens, API responses, or WebSocket data.
- Service worker, if added, caches only static app shell files.
- PWA should work with HTTPS/WSS for real deployments. Plain HTTP remains acceptable for local development.

## Testing

Automated coverage should focus on logic that can be tested without a browser automation dependency:

- Shortcut button mapping sends exact bytes/string sequences.
- Token query parsing stores token and removes no existing behavior.
- WebSocket URL generation keeps `ws`/`wss` and token query behavior.
- Route parsing for `#/s/<uuid>` remains compatible.

Manual verification should cover:

- iPhone Safari responsive layout at common viewport sizes.
- Add to Home Screen launch.
- Attach to a live session and type normal text.
- Use each shortcut button against a shell.
- Rotate device and confirm terminal resizes.
- Kill network briefly and confirm reconnect status/replay.
- Desktop browser still lists and attaches sessions.

## Rollout

This is backward-compatible and can ship in the next regular release. No relay, protocol, or desktop config migration is required.

## Future Work

- Replace CDN xterm assets with vendored/static assets for better offline shell caching.
- Add QR code pairing from desktop settings to open relay URL/token on phone.
- Add an optional native iOS SwiftUI client if PWA constraints become limiting.
- Add user-level auth when the relay grows beyond shared-token deployments.
