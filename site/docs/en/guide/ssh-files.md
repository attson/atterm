---
description: Manage SSH hosts, import Termius or SSH config, and browse, preview, edit, and transfer local and remote files in AT Term.
---

# SSH and files

The desktop app is both a local terminal and an SSH workspace. Host definitions, key references, jump routes, and forwarding rules live in the **SSH hosts** panel in the tab bar. Once connected, an SSH session participates in tabs, the sidebar, and recovery like any other session.

## Add and connect to a host

1. Select the SSH icon at the right of the tab bar.
2. Create a host with a name, address, port, and username, then choose a password or an imported private key when needed.
3. Configure a jump route, proxy, or tunnel before saving when the host requires one.
4. Save and select the host to connect. A host can override terminal theme and appearance.

The desktop app also has two migration paths:

- **Import from `~/.ssh/config`** previews selectable Host entries. It does not guess credentials from the config.
- **Import from Termius** requires Termius to be fully quit before you select its data directory and preview hosts, keys, and Snippets. The `localKey` is the base64 key from the Termius keychain entry, not your account password.

Configuration exports never include SSH passwords or private keys. Add credentials again after importing on another computer.

## Browse, preview, and edit files

Expand the workspace's right panel to open the file explorer. It follows the active pane: local shells use the local filesystem, while SSH and remote sessions access the filesystem belonging to that session.

![AT Term file explorer and file preview](/manual/file-explorer.png)

You can browse directories, search and sort names, preview text, Markdown, code, and supported images, and edit text in the built-in editor. The panel also supports create, rename, download, and delete operations. A remote directory has no trash or undo semantics, so destructive recursive work should be done in a terminal after checking the path.

Local paths printed in the terminal can be revealed directly in the file explorer.

## Send a file or image from a client

Browser and mobile clients can explicitly choose an image or file and send it to the attached remote session. All three conditions must hold:

1. The session is attached.
2. This client is the driver, not a read-only viewer.
3. The desktop owner has set the session's remote permission to `full`.

`view` permits watching, `control` adds input and resize, and `full` adds file, image, and filesystem operations. See [Cross-device clients](/en/guide/remote-clients) and [End-to-end encryption](/en/guide/e2ee).
