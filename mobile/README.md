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

The bundled app cannot use same-origin `/api/sessions`, so the web client now has a `relay URL` field in the token panel. Enter an HTTPS relay URL such as:

```text
https://relay.example.com
```

For trusted IP:port testing, enable `allow insecure HTTP relay` in the same panel and enter:

```text
http://121.43.40.128:23301
```

The iOS MVP includes an ATS exception so this explicit in-app opt-in can connect to plain HTTP relays. Do not use insecure mode for production or App Store builds; use HTTPS/WSS instead.

For a public relay, allow the Capacitor WebView origin when starting the relay:

```bash
ATTERM_ORIGINS=capacitor://localhost \
ATTERM_BOOTSTRAP_ADMIN_EMAIL='admin@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Strong-Bootstrap-Pass-2026!' \
atterm-relay --addr :8080 --web web
```

Once running, sign in through the web UI as you would in a desktop browser (the Capacitor WebView shares the cookie store with the wrapped site).

Use HTTPS/WSS for production devices. Plain `http://` relay URLs are for trusted simulator/local/IP testing only.

## Verify

```bash
npm test
npm run sync-web
node --test ../web/*.test.mjs scripts/*.test.mjs
```

The current Codex environment only has Command Line Tools, not full Xcode, so `xcodebuild` verification requires opening this project on a machine with Xcode installed.
