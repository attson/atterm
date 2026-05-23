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

## Relay configuration

PR-B boots the mobile app to a placeholder page (`MobilePlaceholder.vue`) confirming the Capacitor bundle and the desktop frontend's `platform/` adapter load inside iOS WebView. **Actual relay configuration UI ships in PR-C.**

The relay must allow the WebView origin. Start it with:

```bash
ATTERM_ORIGINS=capacitor://localhost \
ATTERM_BOOTSTRAP_ADMIN_EMAIL='admin@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Strong-Bootstrap-Pass-2026!' \
atterm-relay --addr :8080 --web web/dist
```
