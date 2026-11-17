---
description: Reuse terminal input with AT Term input history, quick templates, Snippet batch runs, and configurable keyboard shortcuts.
---

# Productivity tools and shortcuts

## Reuse input history

Each session keeps its own submitted input. Select the history control in the pane's upper-right corner, or press `Mod+Shift+H`, to view, copy, and resend recent input without searching a long scrollback.

![AT Term input history panel](/manual/input-history.png)

History is isolated by session and persisted as a bounded snapshot. Restoring a local shell or reconnecting a saved SSH session carries the previous session generation's entries forward. The host that owns a remote session provides that session's history.

## Quick templates

The template bar below the terminal sends frequent short replies or commands. Defaults include `yes`, `ok`, `continue`, `commit`, `push`, `release`, and numeric choices. Selecting one sends it directly to the active writable terminal.

Under **Settings → Quick templates**, you can add, edit, reorder, hide, or bind a hotkey to a template, and hide the entire bar. Templates do not bypass viewer/driver or remote permission checks. Because they send immediately, long or destructive commands are better stored as Snippets with an explicit target review.

## Snippets and batch runs

Store multi-line scripts or repeated diagnostics as Snippets. The **Snippets** menu below the terminal can send one to the active session or run it across selected saved SSH hosts while showing per-host progress and results.

Review the host set and script before a batch run. A Termius import can migrate independent Snippets, but does not turn a host startup script into a manually runnable Snippet.

## Default shortcuts

| Action | Default |
|---|---|
| New tab | `Mod+T` |
| Previous / next tab | `Mod+Shift+[` / `Mod+Shift+]` |
| Vertical / horizontal split | `Mod+N` / `Mod+Shift+N` |
| Close active pane | `Mod+W` |
| Focus adjacent pane | `Mod+Alt+Arrow` |
| Search terminal output | `Mod+F` |
| Open input history | `Mod+Shift+H` |
| Toggle session sidebar | `Mod+B` |
| Focus sidebar search | `Mod+Shift+F` |
| Increase / decrease font size | `Mod+Shift+=` / `Mod+-` |
| Toggle maximize | `Mod+Enter` |

The desktop app can rebind or disable these under **Settings → Keyboard shortcuts**. If a custom chord conflicts with the shell, browser, or operating system, whichever layer captures it first takes precedence.
