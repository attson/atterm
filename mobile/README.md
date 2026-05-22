# AT Term Mobile

This is the MVP iOS WebView wrapper for the existing relay web client. It uses Capacitor to bundle `../web` into an iOS app backed by `WKWebView`; the relay remains the source of sessions and terminal I/O.

## Develop

```bash
cd mobile
npm install
npm run ios:add     # first time only; creates mobile/ios
npm run ios:open    # syncs web assets and opens Xcode
```

After `ios:add`, keep the generated `mobile/ios` project in git, but do not commit `node_modules`, `www`, `Pods`, or copied Capacitor public assets.

## Relay configuration

The bundled app cannot make same-origin `/api/sessions` calls. On first launch, the app opens a **setup screen** asking for:

- **Relay URL** — e.g. `https://relay.example.com` (or `http://1.2.3.4:8080` for IP testing)
- **API token** — paste an `atk_…` token. Generate one on a desktop browser via the relay's `/settings.html#api-tokens` page (Settings → API Tokens → Create).
- **Allow insecure HTTP/WS** — turn on only for IP/port testing against a plain HTTP relay. Production must use HTTPS.

The relay must allow the WebView origin. Start the relay with:

```bash
ATTERM_ORIGINS=capacitor://localhost \
ATTERM_BOOTSTRAP_ADMIN_EMAIL='admin@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Strong-Bootstrap-Pass-2026!' \
atterm-relay --addr :8080 --web web/dist
```

To change the relay later: in the app, Settings → Relay → edit fields or Disconnect.

## Mobile smoke checklist

After any change to `web/src/setup/`, `web/src/shared/api/relay-config.ts`, `web/src/shared/mobile-guard.ts`, `web/src/shared/api/client.ts`, or `web/src/shared/ws/client-conn.ts`, run through this in the iOS simulator before merging:

1. Cold start with no config → setup screen renders.
2. Invalid token → inline "API token is invalid" error; not redirected away.
3. Valid token → home screen renders; session list loads.
4. Open session → WS connects; characters echo.
5. Revoke token externally → next API call redirects to `/setup.html?reason=token_invalid`.
6. Settings → Relay → Disconnect → setup screen with empty fields.
