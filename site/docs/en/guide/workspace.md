---
description: Organize local and remote terminal work with AT Term tabs, split panes, the session sidebar, task states, terminal search, and recovery.
---

# Workspace and sessions

AT Term organizes a working context into **tabs, panes, and sessions**. Tabs separate topics, each tab can contain several panes, and each pane connects to one local or remote session. Closing a pane closes or detaches according to the session type; AT Term warns before closing a local session that is still running.

![Session sidebar, tabs, and terminal in the AT Term workspace](/manual/workspace.png)

## Tabs and split panes

1. Select `+` in the tab bar, or press `Mod+T`, to create a local shell tab.
2. Press `Mod+N` for a vertical split or `Mod+Shift+N` for a horizontal split. A new local shell inherits the active local pane's working directory when possible.
3. Use `Mod+Alt+Arrow` to focus an adjacent pane and `Mod+W` to close the active pane.
4. Drag a session from the sidebar onto a pane to open it there. You can also drag a pane by its grip to rearrange existing sessions.

`Mod` means Command on macOS and Ctrl on Windows/Linux. Change defaults under **Settings → Keyboard shortcuts**.

## Find work in the session sidebar

The sidebar groups local and remote sessions by host or state and shows the current command, working directory, task state, and unread changes.

- Select a session to focus its existing pane, or open it in a new tab if it is not currently visible.
- Use the context menu to pin important sessions. Pins survive restart and follow a recovered local session to its new session id.
- Press `Mod+Shift+F` to search title, cwd, or current command; press `Esc` to clear the query.
- Press `Mod+B` to collapse or expand the sidebar.

Shell integration reports running, waiting for input, completed, failed, and disconnected states across desktop, Web, and mobile. These states describe the task; they do not pause the process on the host.

## Search and adjust a terminal

- `Mod+F` searches the active terminal's scrollback, not the sidebar.
- `Mod+Shift+=` / `Mod+-` increases or decreases terminal text size.
- `Mod+Enter` toggles window maximization.
- Use the control in the pane's upper-right corner for a per-pane theme; global appearance lives under **Settings → General**.

## Recover an unfinished workspace

The desktop app continuously records tabs, panes, cwd, and AI conversation ids it can verify. On the next launch, choose which tabs to restore:

- A local shell starts a new PTY in the previous cwd.
- A live remote session reattaches by session id. If it is gone, AT Term keeps a disconnected placeholder instead of attaching the wrong task.
- A recognized Claude Code or Codex conversation resumes after the first shell prompt. If AT Term cannot identify it safely, it restores an ordinary shell.

Continue with [SSH and files](/en/guide/ssh-files), or reduce repeated typing with [Productivity tools](/en/guide/productivity).
