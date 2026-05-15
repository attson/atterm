# Web Push Notifications

AT Term can deliver "command finished" notifications to a browser or PWA via the [Web Push API](https://developer.mozilla.org/en-US/docs/Web/API/Push_API), so you get a ping even when the page is not open. This builds on the [shell integration](shell-integration.md) shipped in v0.1.55 and is self-hosted — the AT Term relay is the push origin; no third-party services are involved.

## Requirements

- A reachable relay (the desktop app must have `relay_url` configured, and the browser must be talking to that same relay).
- Shell integration enabled (Settings → General → "Enable shell integration") so the desktop frontend can detect command boundaries.
- A modern browser:
  - Chrome / Edge / Brave / Firefox on desktop or Android
  - Safari on macOS (the page must be added to the Dock for full functionality)
  - Safari on iOS 16.4+ in a PWA installed via "Add to Home Screen"

## How to enable

1. Open your relay URL in the browser, paste the relay token, connect.
2. Click the "🔔 Enable notifications" button in the status row.
3. The browser will prompt for permission. Click Allow.
4. The button changes to "🔔 ON".

On iOS Safari, follow these steps in this order:
1. Open the relay URL in Safari.
2. Tap the Share button → Add to Home Screen.
3. Open the AT Term app from the Home Screen (now in PWA / standalone mode).
4. Click "🔔 Enable notifications" and follow the iOS prompt.

## What triggers a push

The same gate as the desktop-side OS notification (`Settings → General → Command-finished notification threshold (seconds)`):

- AT Term desktop window is NOT focused.
- Command ran for at least the threshold (default 10s, configurable 1-600s).
- The session is local to the desktop AT Term (not a cast-attached remote pane).

When the gate passes, every browser that has subscribed under a relay token authorized to view that session receives a push.

## How to disable

Click the "🔔 ON" button to go back to "🔔 Enable notifications".

To globally disable Web Push on a relay, stop the relay, delete `<RELAY_CONFIG_DIR>/web-push.json`, and the four `/api/push/*` endpoints will return 503 until you re-init. (Easier: just don't enable on the client.)

## Where state lives

`<RELAY_CONFIG_DIR>/web-push.json` holds:
- the VAPID keypair (P-256 ECDSA, generated on first start)
- per-token subscription records (endpoint + browser keys)

The file is rewritten on every subscription change via atomic write-temp-rename. Loss of the file means: regenerated VAPID keypair, all existing browser subscriptions invalidated — users need to re-enable. The previous corrupt file (if any) is preserved as `web-push.json.corrupt-<timestamp>` so you can inspect it.

## Configuration

| Flag / env | Default | Notes |
|------------|---------|-------|
| `--config-dir` / `ATTERM_RELAY_CONFIG_DIR` | `./data/atterm-relay` | The persistent state directory. Web Push file lives here. |
| `--vapid-subject` / `ATTERM_VAPID_SUBJECT` | `mailto:noreply@atterm.local` | The VAPID JWT subject. Push services may reject non-`mailto:` values from some providers. |

## Limitations

- iOS requires PWA install. Plain Safari tabs cannot receive Web Push on iOS.
- Token rotation invalidates subscriptions tied to the old token. Browsers must re-enable.
- VAPID key wipe is irreversible — old subscriptions become unusable.
- No relay-side suppression for "user is actively watching this session". Both an in-page event and a Web Push may fire on a device that is also actively attached. The browser groups them by tag.
- We currently push only command-finished events. BEL and session lifecycle events are deferred.

## Troubleshooting

- **Button is missing**: your browser may not support Web Push (e.g. iOS Safari outside PWA), or `navigator.serviceWorker` isn't available (require HTTPS or loopback dev). Open the JS console.
- **"Server has push disabled"**: the relay started with no usable config directory or failed to load `web-push.json`. Check the relay log for `webpush:` lines.
- **No notifications arrive**: confirm the desktop AT Term is connected to the same relay AND the window is unfocused for at least the threshold seconds. Try the "Test notification" button (or send a `POST /api/push/test`) to confirm the relay → browser path works.
