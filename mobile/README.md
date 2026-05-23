# AT Term Mobile

This is the MVP iOS WebView wrapper for the existing relay web client. It uses Capacitor to bundle `../web` into an iOS app backed by `WKWebView`; the relay remains the source of sessions and terminal I/O.

## Develop

```bash
cd mobile
npm install
npm run ios:add     # first time only; creates mobile/ios
npm run ios:open    # syncs desktop/frontend (capacitor target) and opens Xcode
```

`npm run sync-web` builds `desktop/frontend/` with `VITE_TARGET=capacitor` and copies `desktop/frontend/dist-capacitor/` into `mobile/www/`. The bundled UI is the desktop frontend's capacitor entry; relay-config and remote-session UI ship in PR-C.

After `ios:add`, keep the generated `mobile/ios` project in git, but do not commit `node_modules`, `www`, `Pods`, or copied Capacitor public assets.

## Relay configuration & smoke (PR-C)

On first launch the app shows the **setup** screen: relay URL + API token + "allow insecure" toggle. Generate the token on a desktop browser (relay Settings → API Tokens). Start the relay allowing the WebView origin:

```bash
ATTERM_ORIGINS=capacitor://localhost \
ATTERM_BOOTSTRAP_ADMIN_EMAIL='admin@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Strong-Bootstrap-Pass-2026!' \
atterm-relay --addr :8080 --web web/dist
```

iOS simulator smoke checklist:

1. Cold start, no config → setup screen.
2. Bad token → "API token is invalid"; not navigated away.
3. Valid token → session list, grouped by host.
4. Tap a session → terminal attaches; output shows; typing echoes.
5. Back → list; tap the same session → instant (no reconnect/replay).
6. Open a second session → tab strip shows both; switching between tabs is instant.
7. Open 5 sessions → oldest auto-detaches (≤4 tabs).
8. `×` a tab → that terminal closes; others unaffected.
9. Revoke the token on the relay → next refresh → back to setup with "token invalid" banner.
10. Gear → setup → reconnect with a new token.
