---
description: Install AT Term and start using its local terminal, SSH workspace, AI agent sessions, and end-to-end encrypted remote access.
---

# Install and quick start

AT Term is a cross-platform terminal with remote takeover. Shell, Codex, Claude, and other long-running tasks start in the desktop app and can later be viewed or controlled from a phone, browser, or another computer. When E2EE is enabled, Relay cannot read terminal output, titles, working directories, or task summaries.

> Local comes first. Add remote synchronization only when you need it.

## How it works

1. Desktop runs local PTYs; OSC 133 derives running, waiting, done, and failed states.
2. After desktop connects to Relay, a remote client can attach. PTY bytes are uploaded lazily only while somebody watches.
3. E2EE keeps the account key on clients and seals terminal content before it reaches Relay.
4. System notifications, Web Push, and Feishu cards can route important events back to the correct session.
5. Claude Code, Codex, Aider, and Gemini CLI are recognized, with agent-specific recovery and hook support described in [AI agents](/en/guide/ai-agents).

## Desktop only

Download a stable package from the [home page](/en/#downloads) or browse [all Releases](https://github.com/attson/atterm/releases/latest).

| Platform | Recommended package | Notes |
|---|---|---|
| macOS Apple Silicon | `AT-Term-darwin-arm64.zip` | Intel machines use `amd64` |
| Linux x64 | `AT-Term-linux-amd64.tar.gz` | Requires WebKitGTK 4.1 |
| Linux arm64 | `AT-Term-linux-arm64.tar.gz` | For ARM Linux desktops |
| Windows x64 | `AT-Term-windows-amd64.zip` | Windows 10 may need WebView2 |

::: warning macOS Gatekeeper
Current macOS packages are unsigned and not notarized. After installing, run `xattr -dr com.apple.quarantine "/Applications/AT Term.app"`. This removes quarantine only from AT Term and does not disable system security.
:::

## Desktop plus a self-hosted Relay

This mode is for starting work on a computer and taking it over elsewhere.

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_ORIGINS='https://relay.example.com' \
docker compose up -d atterm-relay
```

Read the one-time claim token from `docker compose logs atterm-relay`, then register that email at `/signup.html`. Set the Relay URL in desktop Settings, log in, and use the same account in a browser or another client. See [Deploy Relay](/en/guide/deploy-relay) for TLS, storage, and multi-instance details.

## Build the iOS client from source

There is currently no public App Store or TestFlight build. The supported path is the Capacitor project in this repository:

```bash
cd mobile
npm install
npm run ios:open
```

Open the generated project in Xcode and run it on a simulator or a signed development device. Xcode 15+ is required.

## Development setup

```bash
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --dev-insecure

cd web && npm ci && npm run dev

cd desktop
wails dev -tags webkit2_41  # Linux; omit the tag on macOS/Windows
```

Core tools: Go 1.23+, Node.js 20, Wails 2.12, and Xcode 15+ for iOS. Ubuntu 24.04 or Debian 12 also needs `build-essential`, `pkg-config`, `libgtk-3-dev`, and `libwebkit2gtk-4.1-dev`.
