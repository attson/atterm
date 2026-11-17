---
description: Track and recover Claude Code, Codex, Aider, and Gemini CLI sessions in AT Term, including the exact boundaries of resume and Feishu support.
---

# AI agents and Feishu

## Start and recognize an agent

Run `claude`, `codex`, `aider`, or `gemini` normally in an AT Term shell. The session sidebar shows the agent type and task state. When a command completes, fails, or waits for input, desktop notifications, Web Push, or Feishu can route you back to the correct session.

| Agent | Recognized | Restore the previous conversation | Feishu AskUserQuestion |
|---|---:|---|---:|
| Claude Code | Yes | Uses `claude --resume` when a verified session id exists | Yes |
| Codex | Yes | Uses `codex resume` when a verified session id exists | No |
| Aider | Yes | Replays the full launch command; chat history is not guaranteed | No |
| Gemini CLI | Yes | Not supported yet | No |

Recovery follows a strict rule: it is better to open a normal shell than resume the wrong conversation. AT Term injects a resume command only when it can verify the session id, and preserves other launch flags.

## Track AI tasks from the sidebar

- Pin a long task, or search by type, title, cwd, or current command.
- `waiting input` means a question or permission needs attention; `completed` and `failed` mean the command has ended.
- Before using a quick template such as `yes`, `continue`, or a number, confirm that the client is the driver and has control permission.
- The desk widget can show only AI sessions or include ordinary builds and tests as well.

## Configure Feishu

Remote Feishu answers currently apply only to Claude Code:

1. Open **Settings → Feishu** on desktop, choose local or Relay mode, and complete its configuration.
2. Start Claude Code inside an AT Term session. A persistent anchor card streams output and provides input, Ctrl-C, Ctrl-D, Escape, Enter, and stop actions.
3. When Claude opens AskUserQuestion, the card switches to single-choice, multiple-choice, and custom-input fields, then restores normal controls after submission.

On the first local AskUserQuestion permission prompt, select **“Yes, and don't ask again.”** Otherwise that prompt consumes the remote keystrokes before they reach the question.

Local mode stores credentials in the operating system credential store. Relay mode is configured by an administrator and stores secrets encrypted. Never paste an App Secret into terminal output, screenshots, or diagnostics.
