# Accountless P2P Stage 1 - Peer Space and Decentralized Sync

> Parent: [Accountless P2P plan](./2026-09-22-accountless-p2p.md)
> Depends on: [Stage 0](./2026-09-22-accountless-p2p-stage-0-spec-spikes.md)
> Milestones: P1-P2
> Outcome: 本地具备完整 Peer Space、邀请和可持久化去中心化配置副本，但尚不依赖公网连接。

## Entry Gate

- Stage 0 spec accepted，golden vectors 和 merge rules 已冻结。
- 选定的 key storage 能在 desktop/Web/iOS 上持久化。
- Relay compatibility adapter 的迁移顺序已有测试模型。

## PR 1.1 - Peer crypto and typed protocol

Create:

- `internal/peercrypto/`
- `internal/peerproto/`
- TS mirror under `desktop/frontend/src/lib/peer/`

Implement strict encode/decode for genesis、membership、grant、ticket、bundle、handshake and encrypted records. All parsers have size/depth bounds; unknown critical fields/versions fail closed.

Tests consume Stage 0 vectors and fuzz all untrusted parsers.

## PR 1.2 - Identity and local stores

Create `internal/peerstore/` and platform storage adapters:

- Desktop identity/storage wrapping key in OS keyring。
- iOS identity in a Capacitor 8 Keychain plugin。
- Browser non-exportable WebCrypto key in IndexedDB。
- File-backed Space/governance/op stores use atomic write、0600、schema version and corruption detection。

Rules:

- Ordinary `desktop/config.go` stores only non-secret toggles/URLs。
- Reset identity is explicit and warns that all memberships become invalid。
- Logs expose peer-id prefix only; no private key、ticket secret、epoch key、plaintext op payload。

## PR 1.3 - Peer Space governance and invitations

Implement:

- Create Space genesis and initial admin membership。
- `can_invite` / `can_sync_secrets` capability narrowing and depth=1 delegation。
- Route-independent ticket batches; default 5 one-use/24-hour tickets。
- Issuer-bound `redemption_peer_id` atomic consume ledger。
- Device/grant/batch revoke, deny-wins governance log。
- Deterministic same-parent epoch rotation winner and losing-branch rebase。

Tests:

- 10 concurrent redeems produce exactly one success。
- A non-redemption device cannot consume even with a valid replicated ticket。
- expired/revoked/unknown/consumed share the public `invite_invalid` result。

## PR 1.4 - Config replica core

Create `internal/configsync/`:

- Signed immutable ops and monotonic device counters。
- HLC with bounded physical skew and compact version vectors。
- Scalar causal/LWW register。
- Record-level remove-wins map and stable ordering tokens。
- Materialized view, durable ack vectors, signed snapshots and compaction。
- One serialized owner loop; no concurrent engine mutation.

Property tests cover commutativity、associativity、idempotence and randomized partitions.

## PR 1.5 - Existing configuration adapters

Adapt the current 18 `internal/prefssync` keys:

- Scalars become individual configsync records。
- quick templates、profiles、SSH hosts become record-level collections。
- profile env/SSH credentials/private keys use vault records and remain opt-in。
- Machine-local values remain outside the replica。

Migration:

1. Existing Relay users complete one old-engine pull.
2. Current values + `PrefsMeta` seed the signed local replica once.
3. Relay GET/PUT becomes a compatibility adapter, not a second writer.
4. Relay echoes dedupe by origin/value hash.
5. Whole-blob sealed values round-trip during the compatibility window.

## PR 1.6 - Epoch keys and bootstrap snapshots

Implement separate `sync_epoch_key` and `vault_epoch_key`:

- Per-member public-key envelopes。
- AAD includes space/collection/record/op/epoch and has registered namespace tags。
- Revocation rotates forward; old knowledge is documented as unrecoverable。
- New-member bootstrap snapshot contains only collections allowed by grant。

Join policy:

- Fresh device adopts Space snapshot。
- Existing local customizations are backed up and shown as pending import。
- Only explicit import creates new Space ops; defaults never overwrite the Space。

## Stage Exit Gate

- [ ] Restart preserves identity, counters, pending ops and governance state.
- [ ] Three in-memory replicas converge through A↔B↔C without A↔C.
- [ ] Revoked members cannot decrypt a new epoch.
- [ ] Non-vault members never receive a vault envelope.
- [ ] Relay-only upgrade retains all existing synced values and old clients still work.
- [ ] With every network adapter disabled, local setters remain immediate and durable.

Verification:

```bash
go test -race ./internal/peercrypto/... ./internal/peerproto/... ./internal/peerstore/... ./internal/configsync/... ./desktop/...
go vet -tags webkit2_41 ./...
cd desktop/frontend && npm run build && npm test
cd mobile && npm test
```

## Not In This Stage

- Pion/WebRTC、Rendezvous、Quick Tunnel。
- Session list/attach。
- Final Settings UI; only minimal bindings/test harnesses are allowed.
