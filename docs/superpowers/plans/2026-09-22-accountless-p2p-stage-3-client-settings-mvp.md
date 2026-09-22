# Accountless P2P Stage 3 - Shared Clients, Settings, and MVP

> Parent: [Accountless P2P plan](./2026-09-22-accountless-p2p.md)
> Depends on: [Stage 2](./2026-09-22-accountless-p2p-stage-2-transport-rendezvous.md)
> Milestones: P5-P6
> Outcome: Desktop/Web/iOS 可在不登录 Relay 的情况下加入 Peer Space、同步配置并接管终端；Settings 完成最终信息架构。

## Entry Gate

- Headless/test clients can complete join、reconnect、terminal attach and sync。
- Rendezvous official/custom contract stable。
- Peer host failure is isolated from desktop boot and local terminal。

## PR 3.1 - Shared frontend transport

Refactor `desktop/frontend/src/lib/connection.ts` around `BinaryFrameTransport`:

- Existing WebSocket adapter remains behavior-compatible。
- RTCDataChannel adapter provides the same binary frame/status surface。
- Peer WSS adapter interface exists but implementation waits for Stage 4。
- `SessionConnection`/`SessionListConnection` keep parsing existing proto frames。

Platform work:

- Add typed `PeerBridge` to `platform/types.ts`。
- Wails owns host controls and Go identity operations。
- Capacitor uses native Keychain identity plugin and browser WebRTC。
- Web uses stable HTTPS origin, WebCrypto/IndexedDB and browser WebRTC。

## PR 3.2 - Join and bootstrap flow

Implement QR scan/paste/deep-link flows:

- Parse and verify ticket/bundle before dialing。
- Show Space/device fingerprint and requested permissions before join。
- Route redemption only to `redemption_peer_id`。
- Consume once, issue membership, receive epoch envelopes and bootstrap snapshot。
- Store grant/identity before reporting success。
- Secret appears only in fragment/deep-link payload, never query/log/error/analytics。

Existing-config flow:

- Save pre-join local snapshot。
- Adopt Space snapshot。
- Present customized local items as optional import; defaults never auto-push。

## PR 3.3 - Route-aware session integration

- Merge Relay and Peer advertisements by `session_id` only。
- Keep route candidates as metadata; do not key by `host_id`。
- MVP has one active route per attached session and no automatic mid-session failover yet。
- Show `Direct` or `Relay` status accurately。
- Preserve driver/viewer、viewer winsize and replay-progress behavior。
- Old Relay-only startup creates no Peer identity or network connection until enabled.

## PR 3.4 - Relay Account settings consolidation

Change `SettingsDialog.vue`:

- Adaptive desktop size around 1040-1080 × 700-720。
- Mobile full-screen。
- Sidebar/content/footer `min-height: 0`; content area owns scrolling。
- Merge Relay config/account and `SettingsDevices.vue` into `Relay 账户`。
- Remove standalone signed-in-devices nav entry without duplicating its logic。
- Preserve Relay dirty-state guard and pinned save footer。

## PR 3.5 - Peer Connection settings

Create `SettingsPeer.vue` and focused subcomponents:

- Device identity/fingerprint and active Space。
- Desktop host on/off and route health。
- Official/custom/disabled Rendezvous。
- Members、capabilities、revoke、stale/offline states。
- Invitation batch create/QR/copy/revoke/expiry。
- Join existing Space。
- Sync status、pending ops、last peer sync、conflict count、manual sync。
- Secret sync policy and sync/vault epoch state。

Desktop shows host + client controls. Web/Capacitor show client/join/sync controls only.

## PR 3.6 - MVP E2E, diagnostics, and feature gate

- Feature flag defaults off for existing installs during beta。
- Diagnostics exports redacted Peer/Rendezvous/ICE/sync state, never secrets。
- Chinese/English copy for all states。
- Mobile safe area、keyboard、background/foreground reconnect。
- Screenshot/scroll tests at desktop 1280×720/1440×900/wide and mobile 320/390 widths。

E2E matrix:

- Desktop host ↔ Desktop/Web/iOS client。
- Official and self-hosted Rendezvous。
- view/control grants, revoke, expired/consumed invite。
- A edits theme, B adds template, C deletes profile; changes propagate via B without A↔C overlap。
- Vault disabled by default; enabled data reaches only authorized member。

## Stage Exit Gate

- [ ] Fresh client uses atterm without Relay account and attaches through a signed invitation.
- [ ] Restart reconnects with membership, without consuming another invitation.
- [ ] Portable config converges and pending status is visible when no peer is online.
- [ ] Settings scrolls to the final item with no overlap/horizontal overflow.
- [ ] Relay signed-in devices and Peer members are visually and semantically separate.
- [ ] Restrictive NAT failure is explicit: no TURN fallback is claimed.
- [ ] Relay-only behavior and bundle size/network startup remain within agreed regression budget.

Verification:

```bash
go test -race ./internal/peer... ./internal/configsync/... ./internal/rendezvous/... ./desktop/...
go vet -tags webkit2_41 ./...
cd desktop/frontend && npm run build && npm test
cd web && npm run build && npm test && npm run test:contract
cd mobile && npm test
```

Manual gate: real iOS/WKWebView join, background/foreground, Chinese IME terminal control and settings scroll.

## Not In This Stage

- Quick Tunnel、automatic Relay fallback、TURN。
- Peer file explorer/paste/preview/session create。
- Always-online background sync or Peer Web Push replacement。
