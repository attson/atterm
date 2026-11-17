---
description: Track and recover Claude Code, Codex, Aider, and Gemini CLI sessions in AT Term, including the exact boundaries of resume and Feishu support.
---

# AI agents and Feishu

## Support matrix

| Agent | Recognition | Resume | Session discovery | Notification hook | Feishu AskUserQuestion |
|---|---|---|---|---|---|
| Claude Code | Yes | `claude --resume <sid>` | Claude project JSONL | Automatic | Yes |
| Codex | Yes | `codex resume <sid>` | Codex rollout JSONL | Not yet | No |
| Aider | Yes | Replays the full launch command | No stable session protocol | No | No |
| Gemini CLI | Yes | Not yet | No | No | No |

Only Claude Code currently uses Notification hooks for permission and AskUserQuestion events. AT Term discovers Claude and Codex session IDs from their JSONL files after OSC 133 classifies the command. If it cannot prove the ID, recovery opens a normal shell instead of resuming a possibly unrelated conversation.

## Feishu anchor card

For Claude Code, `atterm-hook` sends PreToolUse and Notification events to desktop. A persistent anchor card streams assistant output and exposes input plus Ctrl-C, Ctrl-D, Escape, Enter, and stop actions.

When Claude opens AskUserQuestion, the card replaces normal controls with a form. Submitted choices are translated into the tested terminal key sequence. On the first local permission prompt, choose “Yes, and don't ask again”; otherwise that prompt consumes the remote answer.

Feishu can connect directly from desktop or through Relay. Local credentials use the system keyring; Relay credentials are encrypted in its database and managed under Admin.
