---
description: Find AT Term general, task, Relay, Peer, plugin, template, update, diagnostics, and device settings across supported clients.
---

# Settings

Select the gear at the right of the tab bar. AT Term shows sections according to the current client's capabilities, so desktop, browser, and iOS do not have identical lists.

## Find a setting by goal

| Goal | Section |
|---|---|
| Terminal theme, font, shell, notification threshold, recovery, background subscriptions | **General** |
| Session grouping and task display | **Tasks** |
| Connect the desktop to an account service and set remote permissions | **Relay** |
| Join or manage a trusted device group | **Peer** |
| Review and revoke signed-in devices | **Devices** |
| File explorer, desk widget, and extensions | **Plugins** |
| Application shortcuts | **Keyboard shortcuts** |
| Replies in the terminal template bar | **Quick templates** |
| Check and manually install a release | **Updates** |
| Logs, diagnostics, and configuration import/export | **Diagnostics** |
| Claude Code remote interaction through Feishu | **Feishu** |
| Files delivered from remote clients | **Received files** |

## Platform differences

- **Desktop** has local PTYs, Relay, plugins, shortcuts, updates, diagnostics, Feishu, and received files. SSH hosts use their dedicated tab-bar panel.
- **Web / PWA** has browser-appropriate account, task, device, and template settings, but no local PTY, desktop plugins, or desktop updater.
- **iOS** adds QR pairing, Keychain, and mobile auxiliary keys. Its SSH hosts page is a read-only reference to desktop-synced hosts; iPhone does not start a local SSH session.
- **Peer** appears only when the current platform provides a Peer bridge.

## Configuration and credentials

**Diagnostics → Import & export** on desktop can move preferences and SSH host definitions, but never Relay passwords, session tokens, SSH passwords, or private keys. AT Term previews changes before applying them. If an existing SSH host's address changes, its saved credential is cleared to prevent reuse against the wrong host.

Updates are checked in the background but never install or restart silently. Installation requires an explicit confirmation under **Updates**, and the package is signature-verified before its hash is checked.
