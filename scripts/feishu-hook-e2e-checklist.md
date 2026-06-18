# Feishu Hook + Desktop-Direct E2E Checklist

Run before each PR that touches `desktop/feishu/`, `cmd/atterm-hook/`,
or `internal/feishu/`. Not part of CI — requires a real Feishu app.

## Prereqs (one-time)

1. Build atterm desktop + the `atterm-hook` CLI:
   ```bash
   go build -o /tmp/atterm-hook ./cmd/atterm-hook
   sudo install /tmp/atterm-hook /usr/local/bin/atterm-hook
   ```
2. Create a self-built Feishu app at https://open.feishu.cn/app, note
   app_id/app_secret/encrypt_key/verify_token, enable "事件订阅加密",
   subscribe `im.message.receive_v1` + `card.action.trigger`.
3. Hook is now auto-installed. **No manual settings.json editing needed.**
   On first atterm launch with auto-install enabled (default), check:
   - `ls -la ~/.atterm/bin/atterm-hook` shows a symlink
   - `cat ~/.claude/settings.json | python3 -m json.tool` shows two
     atterm-managed Notification entries pointing at the symlink.

   To opt out: in atterm Settings → Feishu, toggle off "Auto-install
   Claude Code hook". The two atterm entries will be removed; any
   user-managed Notification entries are preserved.

## Relay-backed mode

- [ ] Log in to a relay account in atterm desktop.
- [ ] Settings → Feishu → save credentials → confirm "configured".
- [ ] Click "Start Pair" → receive a short code.
- [ ] In Feishu IM, private-message the bot: `/bind <code>` → bot replies
      "✅ 已绑定到 atterm" (via relay HTTPS callback path).
- [ ] In atterm, start a session and run `claude` inside.
- [ ] Ask claude something that triggers `AskUserQuestion` → wait for
      a Feishu card with title "Session 等待输入" + the question text
      in a fenced code block.
- [ ] Tap "确认" → card updates to "已确认".
- [ ] Tap "跳回打开 session" → `atterm://session/<id>` (still no-op
      until URL scheme handler ships).
- [ ] Run a non-AI command that takes > 5s and finishes → confirm a
      separate `command_finished` card.

## Local-only mode

- [ ] Log out of relay; restart atterm.
- [ ] Settings → Feishu shows "local" mode.
- [ ] Save credentials (same app) → long-connection should attach
      within seconds (check `atterm` logs for `longconn: connected`).
- [ ] Start pair → `/bind <code>` over IM → bot replies → settings tab
      flips to "bound".
- [ ] Run claude inside an atterm session → AskUserQuestion → card lands.
- [ ] Tap "确认" → card updates (handled by long-conn ack path).

## Edge cases

- [ ] Misconfigure verify_token in local mode → long-conn should reject
      handshake → settings tab shows "disabled" banner.
- [ ] Send 3 IM messages while authcode is wrong (force IM 99991663) →
      `feishu_bindings.disabled_at` flag flips → cards stop sending.
- [ ] Delete binding → confirm long-connection cleanly closes.

## Cleanup

- [ ] Delete binding via UI → confirm endpoint file `~/.config/atterm/hook-endpoint`
      removed after atterm shutdown.

## Hook auto-install

- [ ] Fresh launch on a machine with **no** `~/.claude/settings.json`:
      file created with both atterm entries; `~/.atterm/bin/atterm-hook`
      symlink points at `atterm-hook-<sha8>`; Settings · Feishu shows
      green dot + "Hook installed and healthy".
- [ ] Fresh launch with pre-existing **non-atterm** Notification hook:
      both atterm entries appended; user's entry preserved verbatim.
- [ ] Toggle "Auto-install Claude Code hook" OFF in Settings · Feishu:
      both atterm entries removed; user's entry intact; symlink removed;
      versioned `atterm-hook-<sha8>` file kept.
- [ ] Toggle ON again: re-installs cleanly.
- [ ] Break the binary: `rm ~/.atterm/bin/atterm-hook-<sha8>` (the
      symlink target). Open Settings · Feishu; status dot is amber for
      the first poll, then auto-repair writes a fresh `atterm-hook-<sha8>`
      and the next poll shows green. (Note: `chmod 000` alone does NOT
      trigger re-write — `ensureBinary` skips the write when the target
      file already exists. Removal is the right reproduction.)
- [ ] Make settings.json read-only: `chmod 444 ~/.claude/settings.json`;
      restart atterm; status dot is amber; LastError mentions "cannot
      update Claude settings"; atterm is otherwise functional.
- [ ] Put garbage in settings.json: `echo not-json > ~/.claude/settings.json`;
      restart atterm; status dot amber; LastError mentions "invalid JSON";
      file is **not** overwritten.
