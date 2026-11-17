---
description: Understand AT Term OPAQUE login, account-key handling, end-to-end encrypted envelopes, remote permissions, and Relay security boundaries.
---

# End-to-end encryption and security

## Account and account key

Registration and login use OPAQUE, so Relay never receives or stores a plaintext password. During registration the client creates an independent random 32-byte `account_key` and uploads only its password-protected wrapped form. After login, the client unlocks it locally.

The unlocked key stays inside the current client boundary: tab-scoped `sessionStorage` on Web, the operating system keyring on desktop, and Keychain on iOS. It never belongs in a URL, log, IndexedDB, or plaintext Relay storage.

Once unlocked, terminal output, title, cwd, current command, task summary, and command-finished push content are sealed before Relay sees them. Browser, desktop, and mobile clients decrypt locally. If no unlocked browser page is available, a background notification shows generic, non-sensitive text.

## What Relay can see

Relay needs session IDs, timestamps, connection state, and routing metadata to deliver ciphertext to the correct client. It cannot read sealed terminal output, title, cwd, current command, task summary, or command-finished notification content. Supported clients use the same OPAQUE implementation and account-key format, then decrypt content locally.

There is no recovery path that bypasses E2EE. An administrator password reset creates a new `account_key`, leaving old sealed content permanently unreadable. High-risk operations such as account deletion require another OPAQUE step-up and a short-lived, one-use token.

## Remote permissions

| Permission | Remote capability |
|---|---|
| `view` | Output and history only |
| `control` | Input and resize |
| `full` | Control plus image/file paste and remote file browsing |

Relay rejects unauthorized frames first, and the desktop host checks again before writing the PTY or filesystem.

## Fail-closed defaults

- Public Relay requires a bootstrap admin email and an explicit Origin allowlist unless `--dev-insecure` is used for local development.
- Tokens are accepted through Bearer headers or the WebSocket subprotocol, never query strings.
- Session tokens are stored as SHA256 hashes and shown in plaintext only once to the client.
- Web assets are same-origin and do not depend on a CDN.
- CSP, Referrer-Policy, nosniff, Permissions-Policy, rate limits, and connection limits are enabled by default.
- Desktop rejects non-loopback plaintext `ws://` unless insecure mode is explicitly enabled.
- Updates fail closed unless the Ed25519 signature and asset SHA256 both verify.

See [Deploy Relay](/en/guide/deploy-relay) for TLS, bootstrap, and Origin requirements, and [Admin console](/en/guide/admin) for operator actions.
