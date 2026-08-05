# Feishu App-Mode E2E Checklist

Run before each PR that touches `internal/feishu/`, `internal/userstore/feishu_*`,
or `internal/relay/feishu_http.go`. Not part of CI — requires a real Feishu app.

## Prereqs (one-time per developer)

1. Create a self-built Feishu app at https://open.feishu.cn/app
2. Note app_id / app_secret / encrypt_key / verify_token from "凭证与基础信息" + "事件订阅"
3. In "事件订阅" → 开启"事件订阅加密" (mandatory for our integration)
4. Subscribe events: `im.message.receive_v1`, `card.action.trigger`
5. Grant scopes: `im:message`, `im:message:send_as_bot`
6. Set `ATTERM_FEISHU_ENCRYPT_KEY` to a base64'd 32-byte random key

   On macOS / Linux:
   ```bash
   export ATTERM_FEISHU_ENCRYPT_KEY="$(openssl rand -base64 32)"
   ```

## Walkthrough

- [ ] Start relay (`go run ./cmd/atterm-relay`); confirm log: `feishu: app-mode integration enabled`
- [ ] Open atterm desktop UI → Settings → Feishu tab (UI is in a follow-up PR; for now POST via curl)
- [ ] `curl -X POST .../v1/feishu/bindings/me -d '{...}'` with the four secrets
  - Expect: 200 with `app_id_hash` and `callback_url`
  - Verify: a row appears in `feishu_bindings`
- [ ] Paste `callback_url` into Feishu admin "事件订阅 → 请求地址"
  - Expect: Feishu confirms verification succeeded (url_verification echo)
- [ ] `curl -X POST .../v1/feishu/bindings/me/begin-pair`
  - Expect: 200 with `code` (6 chars) and `expires_at`
- [ ] In Feishu IM, private-chat the bot: `/bind <code>`
  - Expect: bot replies "✅ 已绑定到 atterm"
  - Verify: `feishu_bindings.open_id` populated
- [ ] Trigger a `command_finished` from an attached agent (run a long-ish command)
  - Expect: card arrives in Feishu IM with title "命令完成"
- [ ] Tap "确认" on the card
  - Expect: card updates inline to "已确认"
- [ ] Tap "跳回打开 session"
  - Expect: `atterm://session/<sid>` opens — no-op until follow-up PR registers the scheme handler; **document the no-op in the PR description.**
- [ ] Misconfigure verify_token (POST upsert with wrong value)
  - Expect: subsequent /bind attempt logs "verify-token mismatch", bot does NOT reply

## Sealed (E2EE) variant

- [ ] Run agent with E2EE unlocked → trigger command_finished
- [ ] Expect card title "命令完成（仅本机可见）" and body "命令详情仅本机可见"
- [ ] Verify no exit code or label leaked in the card

## Cleanup

- [ ] `curl -X DELETE .../v1/feishu/bindings/me`
- [ ] Verify row removed; subsequent events log "unknown_app_id_hash"
