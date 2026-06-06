# Session attention / seen model (design)

Date: 2026-06-06
Status: Draft (design phase); pending implementation plan
Roadmap item: P-personal (session state UX hardening; not the original
P3 collaboration track). Covers the seen/unread inbox, type-aware
notification de-noising, and host-level rollup. Inspired by herdr's
done-unseen / idle-seen state split.

## 1. Goal

Turn the task list into an **inbox you can clear**. Today a session that
reaches `completed` sits in `completed` forever — there is no notion of
"new since I last looked", so the list cannot be triaged and Web Push
keeps firing for sessions the user is actively watching.

Introduce a single per-session signal `attention_at` and a per-user
`seen_at`, and derive one `unread` predicate from them plus live
subscriber presence. The same subscriber-presence signal also drives
notification de-noising. After this lands:

- Each relay session carries `attention_at` (last time it entered an
  attention-worthy state).
- The relay persists per-user `seen_at` per session in `userstore`
  (SQLite), so "seen" is unified across the user's devices.
- `unread` is computed server-side per authenticated user and surfaced
  on `SessionInfo` + `MetaPayload`; clients render, they do not compute.
- Web Push for a session is suppressed while any client is attached to
  it.
- Frontends show an unread marker per card, a "mark all read" action,
  single-card mark-read, and a host-group rollup (unread count + most
  urgent child state).

Out of scope:

- **Detection accuracy** of `task_state` itself (false `waiting_input`,
  missed `running`, foreground-process heuristics) — that is a separate
  spec (the deferred "#2" track).
- **Precise per-tab focus.** "Attached" is used as the proxy for
  "watching". A backgrounded-but-attached desktop tab counts as
  watching. Acceptable for v1; documented as a known limitation.
- **Seen persistence across session-id reuse.** Session IDs are
  ephemeral; the `seen` row is pruned when the session leaves the
  registry.
- Cross-user / shared-session semantics (single-user project).

## 2. The model

One inequality defines the whole inbox:

```
unread(session, user) =
      attention_at > 0
  AND attention_at > seen_at[user, session]
  AND subscriberCount(session) == 0
```

- `attention_at` is bumped by the relay on attention-worthy transitions
  (§4).
- `seen_at[user, session]` is pushed to "now" by attach or by an
  explicit mark-read (§5).
- `subscriberCount(session) == 0` makes "currently watching == read"
  automatic, with no client focus reporting, and is the *same* signal
  that gates notifications (§6).

## 3. Architecture

```
┌── relay (Go) ───────────────────────────────────────────────────────┐
│                                                                      │
│  internal/proto/frame.go (modified)                                  │
│    • SessionInfo:  + AttentionAt int64, + Unread bool                │
│    • MetaPayload:  + AttentionAt int64, + Unread bool                │
│                                                                      │
│  internal/session/session.go (modified)                             │
│    • meta.AttentionAt bumped on attention-worthy state transitions   │
│      (see §4); broadcast in the same META the transition produces    │
│                                                                      │
│  internal/userstore/ (modified)                                      │
│    • migrations/NNNN_session_seen.sql (new table)                    │
│    • SetSeen(ctx, userID, sessionIDs, at) / SetAllSeen(...)          │
│    • SeenAt(ctx, userID) map[sessionID]int64   (bulk load)          │
│    • PruneSeen(ctx, userID, sessionID)         (on session removal)  │
│                                                                      │
│  internal/relay/ (modified)                                          │
│    • ListResp build: fill Unread per authed user using SeenAt +      │
│      live subscriberCount                                            │
│    • META broadcast: fill Unread (single-user → all subscribers are  │
│      the owner)                                                       │
│    • ATTACH handler: SetSeen(now) for the attaching user+session     │
│    • registry remove: PruneSeen                                      │
│    • HTTP POST /api/sessions/seen  (auth) → SetSeen / SetAllSeen     │
│    • uplink_conn.go dispatch sites (~177, ~503): skip when           │
│      subscriberCount(session) > 0                                    │
└──────────────────────────────────────────────────────────────────────┘

┌── clients (desktop / web / mobile) ─────────────────────────────────┐
│  • render unread marker from SessionInfo.unread / META.unread        │
│  • host group header: unread count badge + most-urgent child state   │
│  • desktop TabBar: unread dot per tab                                 │
│  • "mark all read" (list + group header) → POST /api/sessions/seen   │
│    {all:true}; single mark-read (swipe/long-press) → {ids:[id]}      │
└──────────────────────────────────────────────────────────────────────┘
```

## 4. `attention_at` derivation

`attention_at` is bumped (set to the transition timestamp) **only** on
these state transitions, evaluated where `session.go` already updates
`TaskState`:

| Transition into | Condition | Bump? |
| --- | --- | --- |
| `waiting_input` | any type | yes |
| `completed` | `Type != "shell"` | yes |
| `failed` | `Type != "shell"` | yes |
| `running` | — | no |
| `idle` | — | no |
| `completed`/`failed` | `Type == "shell"` | no |

This reuses the existing P2.11 `Type` classification so interactive
shell noise (`ls`, `cd`, `git status`) never enters the inbox, while
AI / test / build / deploy completions do. `attention_at` is monotonic
within a session's current attention episode: re-entering `running`
does not clear it; the next attention-worthy transition overwrites it
with a newer timestamp (which, being `> seen_at`, re-flags as unread —
correct, since a fresh result arrived).

## 5. `seen_at` write paths

Maps to the four agreed "mark seen" triggers:

1. **Open / attach** — relay writes `seen_at = now` for `(user,
   session)` when it handles ATTACH. Server-side; zero client cost;
   covers the primary path on all three frontends.
2. **Currently-watching stays read** — covered structurally by the
   `subscriberCount == 0` term in §2; no write needed. New events that
   arrive while attached do not surface as unread.
3. **Mark all read** — `POST /api/sessions/seen {all:true}` writes
   `seen_at = now` for every currently-listed session of the user.
4. **Single mark read** — `POST /api/sessions/seen {ids:[...]}`.

The HTTP endpoint is authenticated with the existing user session
mechanism (same as the rest of `/api/*`).

## 6. Notification de-noising

Both dispatch sites in `internal/relay/uplink_conn.go`
(`DispatchSessionNotification` ~177, `DispatchCommandFinished` ~503)
gain a guard: **skip dispatch when `subscriberCount(session) > 0`** —
i.e. some client is attached and presumably watching. This is the same
signal as the §2 unread term, keeping "watching == read == no push"
coherent.

Webhook dispatch (~513) is left firing regardless: webhooks are
machine-to-machine integrations, not human attention, so subscriber
presence should not gate them.

Known limitation: subscriber presence is coarser than tab focus. A
desktop user with the session attached in a background tab will not get
a push. Accepted for v1.

## 7. Frontend rendering

- **Protocol consumption.** `SessionInfo.unread` / `MetaPayload.unread`
  are authoritative; clients never compute the predicate. `attention_at`
  is carried for ordering/diagnostics but rendering keys off `unread`.
- **Inbox (#1, mobile `MobileSessionList.vue`, web `SessionList.vue`).**
  Unread cards get an emphasis marker (dot + weight). The existing
  `needs_attention` bucket keeps `waiting_input`; unread non-shell
  `completed`/`failed` render with emphasis at the top of their bucket.
  Seen items de-emphasize into the normal `completed` / `idle` grouping.
- **Bulk / single.** "Mark all read" control on the list header and
  each group header → `{all:true}`. Single card swipe (mobile) /
  long-press or context action → `{ids:[id]}`.
- **Rollup (#4).** Host group headers show `unread count badge +
  most-urgent-child-state color`. Desktop `TabBar.vue` shows an unread
  dot on tabs whose session is unread.
- **i18n.** New keys for "mark all read" / "mark read" (en + zh-CN).

## 8. Testing

**Go**

- `attention_at` bump table: each transition × type → expect bump or
  not (§4 table).
- `unread` predicate: cross of `attention_at` vs `seen_at` vs
  `subscriberCount` (incl. the watching==read case).
- De-noising: `subscriberCount > 0` → dispatch skipped;
  `== 0` → dispatch fires; webhook always fires.
- userstore: `session_seen` migration applies; `SetSeen` / `SetAllSeen`
  / `SeenAt` round-trip; `PruneSeen` removes the row; `ListResp` fills
  `Unread` correctly for two different users (isolation).
- ATTACH writes `seen_at`; registry-remove prunes.

**Frontend**

- Unread marker renders from `unread`; clears after mark-all-read.
- Host rollup count and most-urgent color.
- A focused/attached session does not produce a push (suppression
  surfaced via the test harness's dispatch spy) and renders as read.

## 9. Migration / compatibility

Per project policy (no backward-compat for old releases): old publishers
that omit `attention_at` simply never generate unread items (predicate
is false when `attention_at == 0`), which is the correct degraded
behavior. The new `session_seen` table is additive; the migration is a
single forward-only `CREATE TABLE`.
