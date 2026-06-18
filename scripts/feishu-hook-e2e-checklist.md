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
3. Add Notification hooks to `~/.claude/settings.json`:
   ```json
   { "hooks": {
       "Notification": [
         { "matcher": {"type":"idle_prompt","tool":"AskUserQuestion"},
           "command": "atterm-hook" },
         { "matcher": {"type":"permission_prompt"},
           "command": "atterm-hook" }
       ] } }
   ```

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
