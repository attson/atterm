# Sync visibility — design

Roadmap item 30 (P7). Make preference sync something the user can see and
steer: a status indicator, a notice when an item changed on another device,
and manual push / pull.

## 1. A defect this feature would otherwise make worse

`internal/prefssync/sync.go` says, at the `Engine` type:

> Engine is a single per-app sync engine instance. NOT safe for concurrent
> calls — wire it into a serial goroutine via the desktop boot code.

There is no such serial goroutine. The engine is called from four
independent places, none of them serialized and none holding a lock:

- `app.go:465` — startup `Pull`
- `app_relay.go:100`/`:117` — a bare `go func()` doing `Pull` then `Push`
  after login
- `app.go:1791` — `markPrefDirtyAndPush` spawns `go Push()` **on every
  preference setter call**
- `prefs_watch.go:82` — the polling watcher's `Pull`

`grep -rn "prefsSyncMu\|syncMu" desktop/*.go` returns nothing.

So changing three settings in quick succession already runs three
concurrent `Push` calls. `Push` is a check-then-act over shared state — it
records `sentAt[key]`, does a network round trip that "can take seconds"
(its own comment), then re-reads the meta and writes value and meta only if
the stamp is unchanged. Two overlapping pushes can both observe an unchanged
stamp and both write, and a `Pull` interleaving between another push's
`ReadMeta` and its `WriteValue` can have its adopted value overwritten by a
stale echo.

Manual push/pull buttons hand this race to the user as a feature. So item 30
builds the serial goroutine the comment already promises, and routes every
caller — startup, login, watcher, per-setter, and the new buttons — through
it. This is the prerequisite, not a side quest: the feature is
"see and steer the sync", and steering an unserialized engine is how you
lose a preference.

**Ruling:** one owning goroutine, one request channel, no lock.

This design originally justified the channel by saying it makes the queue
observable, "exactly what the status indicator needs to report '2 changes
waiting'". That turned out to be wrong about its own design: §2 defines
`PendingKeys` as the count of **dirty keys**, read from `PrefsMeta`, and the
shipped code never reads the queue depth at all. The channel is still the
right choice — coalescing a burst of setter-driven pushes into at most one
extra round trip is natural to express as a merge-in-place on a buffered
channel and awkward as a mutex plus a pending-flags struct — but it is not
load-bearing for the indicator. Recorded rather than quietly corrected,
because a rationale that survives into a comment somewhere is how the next
person learns the wrong thing.

## 2. Sync status

```go
type SyncStatus struct {
    State        string `json:"state"`          // "idle" | "syncing" | "offline" | "error"
    LastSyncedAt int64  `json:"last_synced_at"` // ms, 0 = never
    PendingKeys  int    `json:"pending_keys"`   // dirty keys not yet accepted
    LastError    string `json:"last_error,omitempty"`
}
```

- `offline` when there is no relay URL, no session token, or the relay is
  paused — distinct from `error`, because "not configured" is not a failure
  and must not show a red indicator.
- `error` carries the last failure's message. It clears on the next success.
  This matters: the `markPrefDirtyAndPush` comment records that a silent
  400 kept `ssh_hosts_encrypted` broken *for months*. The indicator is where
  that stops being invisible.
- `PendingKeys` counts dirty keys. A number that stays above zero is the
  visible form of "your changes are not leaving this machine".

Pushed to the frontend as `sync:status` on every transition, and readable
via `GetSyncStatus()` for first paint.

## 3. "This item was updated on another device"

`Pull` currently returns only `error`, so nothing downstream can tell which
keys it adopted. It gains a result:

```go
type PullResult struct {
    Adopted  []string // server value taken; local had no competing edit
    Conflict []string // server was newer BUT local was dirty; local kept
}
```

`Adopted` drives the notice. `Conflict` is the more interesting half and is
currently silent: today `Pull` sees a newer server value, sees the local key
is dirty, and `continue`s — the local edit wins for now and a later `Push`
resolves it by last-writer-wins. The user is never told that two devices
disagreed and that a timestamp comparison picked the winner. `Conflict`
surfaces that as "changed on another device too — this machine's version was
kept".

No merge UI, and no prompt. Last-writer-wins stays the resolution rule; this
only stops it being a secret. A per-key merge dialog for preferences would
ask the user to adjudicate `terminal_font_size`, which is not a question
worth interrupting anyone for.

## 4. Manual push / pull

`SyncNow()` enqueues a pull-then-push and returns immediately; the indicator
carries the outcome. One button, not two: "pull" alone can discard nothing
but leaves local edits stranded, and "push" alone can clobber a newer remote
value without ever having looked at it. Sequencing them is the only order
that is always safe, so the UI does not offer the unsafe halves.

Enqueuing while a sync is in flight coalesces — a second click does not
queue a second round trip.

## 5. Surface

- `App.GetSyncStatus() SyncStatus`
- `App.SyncNow() error` — error only for "cannot start" (offline); the
  sync's own outcome arrives via the event
- Event `sync:status` with a `SyncStatus` payload
- Event `sync:pulled` with `PullResult`, for the per-item notices
- UI: an indicator in the settings header showing state + relative last-sync
  time, a "sync now" button, and a dismissible notice listing adopted and
  conflicting keys by their human-readable setting names.

## 6. Non-goals

- **No merge UI, no conflict resolution prompts.** §3.
- **No sync for anything outside `prefssync.SyncedKeys()`.** Sessions,
  scrollback, and known_hosts are not preferences and are out of scope.
- **No push on a timer.** The existing watcher's cadence is unchanged; this
  item adds visibility and one manual trigger, not a new sync schedule.
- **No mobile indicator.** Mobile reads preferences over HTTP from the relay
  and does not run this engine.
