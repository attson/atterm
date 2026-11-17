---
description: Troubleshoot AT Term child-process PATH, Relay HTTPS and OPAQUE login, claim tokens, remote input, notifications, macOS Gatekeeper, and recovery.
---

# FAQ and troubleshooting

## A child process says `command not found`

AT Term starts an interactive login shell. Check that the tool's PATH is configured in the login-shell files such as `.zshrc` or `.bash_profile`, not only in a non-interactive startup file.

## Relay login or OPAQUE fails in a browser

Browser cryptography requires a secure context: HTTPS or `localhost`. Put Caddy, nginx, Cloudflare, or Tailscale in front of Relay; alternatively run Relay with a real certificate and `--https-addr`. For local access, tunnel to loopback with `ssh -L 8080:127.0.0.1:8080 <host>`.

## Relay exits at startup

Public listening fails closed without `ATTERM_BOOTSTRAP_ADMIN_EMAIL` and `ATTERM_ORIGINS`. Use `--dev-insecure` only for loopback development.

## No claim token appears

Set `ATTERM_BOOTSTRAP_ADMIN_EMAIL` and inspect `docker compose logs atterm-relay`. The token lasts seven days, is single-use, and registers only a new account with that email. For an existing account, promote it directly in the database: `UPDATE users SET is_admin=1 WHERE email='you@…';`.

## Desktop cannot connect to Relay

Use an HTTPS/WSS URL, confirm the browser Origin is in `ATTERM_ORIGINS`, and enable insecure mode only for a trusted plaintext network. See [Deploy Relay](/en/guide/deploy-relay).

## I can see a session but cannot type

Remote attach starts in viewer mode. Press Space to take over, then check the owner's `remote_permission`: `view` is read-only, `control` allows input, and `full` also permits file operations. See [Cross-device clients](/en/guide/remote-clients).

## Notifications do not appear

Grant notification permission, subscribe to Web Push in the Web/PWA client, and verify Feishu binding under Settings when using cards. Relay-managed Feishu also needs to be enabled in [Admin console](/en/guide/admin).

## macOS says the developer cannot be verified

Current packages are unsigned and not notarized. After moving the app to Applications, run:

```bash
xattr -dr com.apple.quarantine "/Applications/AT Term.app"
```

This changes only AT Term's quarantine attribute. Homebrew cannot bypass Gatekeeper for an unsigned app either.

## I forgot my password

There is no recovery flow. An admin reset creates a new `account_key`, so previously sealed content remains unreadable by design.

## I do not want the recovery dialog

Disable it under Settings → General. The next launch starts a new tab directly.
