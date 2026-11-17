---
description: Understand AT Term OPAQUE login, account-key handling, end-to-end encrypted envelopes, remote permissions, and Relay security boundaries.
---

# End-to-end encryption and security

## Account setup

Registration and login use OPAQUE with P-256-SHA256 and Scrypt, so Relay never receives a plaintext password. A client creates a random 32-byte `account_key`, wraps it locally with Argon2id and XChaCha20-Poly1305, and uploads only the wrapped blob. After login, the key stays in browser session storage, the desktop keyring, or iOS Keychain.

Once unlocked, terminal output, title, cwd, current command, task summary, and command-finished push content are sealed before Relay sees them. Browser, desktop, and mobile clients decrypt locally. A service worker asks a visible tab to decrypt push content through a `MessageChannel`; otherwise it shows generic text.

## Envelope details

Session keys are derived from `account_key` with HKDF-SHA256. AAD binds each envelope to `session_id` and its unique frame type, preventing cross-type replay. Relay retains only routing metadata such as session IDs and timestamps.

Hard account deletion requires a 60-second, one-use step-up token obtained through another OPAQUE login. There is no password recovery: an admin reset creates a new account key and old sealed history becomes unreadable.

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

See the repository [authentication specification](https://github.com/attson/atterm/blob/main/docs/spec/auth.md) for protocol details and error codes.
