# AT Term Mobile

This is the iOS WebView wrapper for AT Term. It uses Capacitor to bundle the `desktop/frontend` capacitor target into a `WKWebView`; the relay remains the source of sessions and terminal I/O.

## Develop

```bash
cd mobile
npm install
npm run ios:add     # first time only; creates mobile/ios
npm run ios:open    # syncs desktop/frontend (capacitor target) and opens Xcode
```

`npm run sync-web` builds `desktop/frontend/` with `VITE_TARGET=capacitor` and copies `desktop/frontend/dist-capacitor/` into `mobile/www/`. The bundled UI is the desktop frontend's capacitor entry (`main.capacitor.ts` -> shared `App.vue`), so mobile uses the same tabs, sidebar, terminal, Settings, and Account surfaces as web/desktop with narrow-screen CSS.

After `ios:add`, keep the generated `mobile/ios` project in git, but do not commit `node_modules`, `www`, `Pods`, or copied Capacitor public assets.

## Relay configuration & smoke

On first launch with no stored relay session, the app opens Settings -> Account. Users can sign in with relay URL + email + password, or scan a pairing QR from a signed-in desktop/web session. Start the relay allowing the WebView origin:

```bash
ATTERM_ORIGINS=capacitor://localhost \
ATTERM_BOOTSTRAP_ADMIN_EMAIL='admin@example.com' \
atterm-relay --addr :8080 --web desktop/frontend/dist-capacitor
```

iOS simulator smoke checklist:

1. Cold start, no config -> shared App opens Settings -> Account.
2. Bad password -> inline account error; the app stays on the login form.
3. Valid login or pairing QR -> Settings closes/refreshes and the session sidebar loads.
4. Tap a session -> terminal attaches; output shows; typing echoes.
5. Long terminal output -> top tab bar stays sticky and reachable.
6. Open Settings on a narrow screen -> dialog is full-screen, tabs are horizontal, Account still exposes scan QR.
7. Pick image/file from terminal aux row -> sends `PASTE_IMAGE` / `PASTE_FILE` when permission is `full`.
8. Revoke the relay session -> next refresh/sign-out returns to Account login.
