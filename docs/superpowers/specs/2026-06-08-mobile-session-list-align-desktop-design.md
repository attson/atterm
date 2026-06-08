# Mobile Session List — Align with Desktop Task Sidebar

## Goal

Make the mobile session list (`MobileSessionList.vue`) visually and interactively
match the desktop task sidebar (`TaskSidebar.vue` + `TaskGroupedList.vue`):

- Same grouping model (host ↔ state, switchable from the top bar).
- Same state ordering, same completed-seen fold, same unread semantics.
- Trimmed-down cards: keep state icon / state label / short command / shortened
  cwd / host·user helper line / unread dot / row-end ✓; drop the type chip,
  open badge, cols×rows, permission, duration, exit code, last-output, error
  preview, and the long meta string.
- Group-level ✓ and a footer "Mark all read" matching the desktop sidebar.

To keep desktop and mobile from drifting, the grouping logic stays in the
existing `composables/useSessions.ts` (which already exposes `byHost`,
`byState`, `unreadByHost`, `unreadByState`, `completedSeen`, `totalUnread`,
`primaryStateForHost`). The mobile list calls `useSessions(ref([]), remote)`
and consumes the same outputs the desktop sidebar does.

To make "Mark all read" actually work from the iOS app, the relay's CSRF
middleware is updated to let `Authorization: Bearer …` requests through (the
Bearer token is itself an anti-CSRF credential — it is never automatically
attached by browsers).

## Non-goals

- No redesign of the mobile colour or spacing system — reuse existing CSS vars.
- No changes to `MobileTerminal.vue`, `MobileApp.vue`, or the mobile setup flow.
- No collapsible host groups or collapsible side rail on mobile.
- No push notifications or local cache; `refresh()` remains explicit + on mount.
- No new mobile setting for grouping — reuse `useTaskGroupBy` (it already
  falls back to `localStorage` when Wails bindings are absent).
- No special UI for `markSessionsSeen` failures (silent + console.warn, matches
  desktop semantics — relay is the source of truth on next refresh).

## Architecture

```
desktop/frontend/src/
├── lib/
│   └── sessionLabel.ts                ← new; exported commandLabel / fullCommand / rowTitle / hostName
├── components/
│   └── TaskGroupedList.vue            ← switches to importing from lib/sessionLabel
├── mobile/
│   ├── MobileSessionList.vue          ← rewritten around useSessions
│   └── MobileSessionCard.vue          ← new; trimmed card
└── platform/
    ├── types.ts                       ← SessionBridge.markSessionsSeen?
    ├── capacitor.ts                   ← list mapping (unread/attention_at) + markSessionsSeen impl
    └── wails.ts                       ← markSessionsSeen → lib/api.markSessionsSeen

internal/relay/
├── csrfmw.go                          ← Bearer → skip CSRF (auth still enforced inside)
├── csrfmw_test.go                     ← Bearer path coverage
└── sessions_seen_http_test.go         ← +3 Bearer cases
```

### Reusing `useSessions`

`composables/useSessions.ts` already exposes everything needed:

```ts
const sessions = useSessions(ref<RemoteSession[]>([]), remoteSessionsRef)
// sessions.byHost, sessions.byState,
// sessions.unreadByHost, sessions.unreadByState,
// sessions.completedSeen, sessions.totalUnread,
// sessions.primaryStateForHost
```

The mobile list passes an empty local list (it has no local PTYs) and the
remote-session ref. Output is identical in shape to what desktop's `App.vue`
already feeds `<TaskSidebar>`.

Implementation notes for the mobile consumer:

- `byState` keys are TaskState strings, sorted internally by `useSessions` (unread first, then `last_output_at` desc). The mobile renderer picks group order via `STATE_ORDER` (defined inline — three lines).
- `byHost` keys are `host_id`. Group order is `Object.keys(byHost).sort()` — alphabetical by host_id, matching `TaskGroupedList.vue`.
- `completedSeen` in `useSessions` includes both `completed && !unread` AND `failed && !unread` — the mobile fold reuses this definition verbatim.
- `primaryStateForKey(key)`: when `groupBy === 'host'`, call `sessions.primaryStateForHost(key)`; when `groupBy === 'state'`, the key itself IS the state.

No change to `useSessions.ts` API. `TaskGroupedList.vue` keeps consuming its
props from `App.vue` → `TaskSidebar` unchanged.

### `lib/sessionLabel.ts`

Pulled out of `TaskGroupedList.vue` so mobile can reuse it:

```ts
export function fullCommand(s: { current_command?: string; title?: string; session_id: string }): string
export function commandLabel(s: { current_command?: string; title?: string; session_id: string }): string
export function rowTitle(s: { cwd?: string; current_command?: string; title?: string; session_id: string }): string
export function hostName(
  hostId: string,
  list: { host?: string }[] | undefined,
  unknownHostFallback: string,
): string  // `list?.[0]?.host || hostId || unknownHostFallback`
```

`TaskGroupedList.vue` switches to importing all four from here. Mobile uses
the same module to keep `commandLabel`/`hostName` formatting identical across
surfaces.

### `MobileSessionList.vue`

```
┌─────────────────────────────────────────────────────────┐
│  会话 · 12              [host▾]      ⟳     ⚙           │
└─────────────────────────────────────────────────────────┘
   ▼  failed                ⊘ 3   ●5   ✓
   ┌────────────────────────────────────────┐
   │ ⊘  ✦ waiting   claude · ~/.../atterm ●│
   │                attson@mac-mini         │
   └────────────────────────────────────────┘
   ┌────────────────────────────────────────┐
   │ ⊘  ✦ waiting   codex  · ~/proj         │
   │                attson@laptop           │
   └────────────────────────────────────────┘
   ▼  running                   ▶ 2
   …
   ─────────────────────────────────────────
   ▶  已完成 · 5                           ← collapsed by default
   [全部标记已读]                          ← footer; only when totalUnread > 0
```

Components used: `<TaskStateIcon>` (existing), `<MobileSessionCard>` (new).

#### Top bar

| Slot | Content | Notes |
|---|---|---|
| Left | `t('mobile.sessionsTitle')` + ` · ${total}` | `total = sessions.length` |
| Mid | group-toggle button | label `host` / `state`; click cycles; reuses `useTaskGroupBy` |
| Right | refresh + settings icons | existing SVGs + `data-testid`s preserved |

#### Group header (per group)

| Element | Source | Behaviour |
|---|---|---|
| caret | `▼` (decorative) | non-interactive |
| group name | state → `t('mobile.taskStates.<key>')`; host → `hostName(key)` from `useSessionGroups` | |
| state icon | `<TaskStateIcon :state="primaryStateForKey(key)" size="10">` | |
| count | `byGroup[key].length` | |
| unread badge | `t('tasks.unreadBadge', { count })` | only if `unreadByGroup[key] > 0` |
| ✓ group mark | emits `markSeen({ ids: <unread ids in group> })` | only if `unreadByGroup[key] > 0` |

#### Card (`MobileSessionCard.vue`)

| Element | Source | Notes |
|---|---|---|
| state icon | `<TaskStateIcon :state size="14">` | |
| state label | `stateLabel(s.task_state)` | only if `useTaskPreset().active.value.showLabel` is true |
| short command | `commandLabel(s)` | from `lib/sessionLabel` |
| cwd | `shortenCwd(s.cwd, home)` | second line slot, after short command |
| host·user | `${s.host}·${s.user}` | dim helper line under the title row |
| unread dot ● | when `s.unread` | |
| ✓ row mark | when `s.unread`; `@click.stop` → emit `markSeen({ ids: [s.session_id] })` | 44 × 44 dp touch area |

Card click anywhere except ✓ → `emit('open', s)`. Min card height 56 px.

#### Completed-seen fold

- `s.task_state === 'completed' && !s.unread` → moved to `completedSeen`,
  excluded from main groups.
- Bottom fold with `▶ 已完成 · N` toggle; same key `t('tasks.completedFold')`
  as desktop. Items use `dim` style; tapping still opens; no row-level ✓.

#### Footer

- Sticky at bottom, visible only when `totalUnread > 0`.
- Single button `t('tasks.markAllRead')` → `markSeen({ all: true })`.
- `padding-bottom: max(8px, env(safe-area-inset-bottom))`.

#### MarkSeen wiring

```ts
async function onMarkSeen(p: { ids: string[] } | { all: true }) {
  try {
    await platform.sessions.markSessionsSeen?.(p)
  } catch (e) {
    if (e instanceof Error && e.message === 'relay_unauthorized') {
      emit('tokenInvalid'); return
    }
    console.warn('mark-seen failed', e)
    return
  }
  await refresh()
}
```

No optimistic update; no toast on failure (parity with desktop).

### Platform bridge

#### `platform/types.ts`

```ts
export type MarkSessionsSeenOpts = { ids: string[] } | { all: true }

export interface SessionBridge {
  newSession?(req: _Req): Promise<_Resp>
  closeSession(sessionID: string): Promise<void>
  listShells(): Promise<string[]>
  listRemoteSessions(): Promise<RemoteSession[]>
  markSessionsSeen?(opts: MarkSessionsSeenOpts): Promise<void>
}
```

#### `platform/wails.ts`

```ts
markSessionsSeen: (opts) => apiMarkSessionsSeen(opts), // re-export from lib/api
```

#### `platform/capacitor.ts`

`listRemoteSessions` raw shape gains `unread?: boolean; attention_at?: number`,
and the mapping forwards both:

```ts
if (s.unread !== undefined) out.unread = s.unread
if (s.attention_at !== undefined) out.attention_at = s.attention_at
```

`markSessionsSeen`:

```ts
async markSessionsSeen(opts) {
  const cfg = parseRelayJSON(await secureStorage.get(STORAGE_KEY))
            ?? loadLegacyFromLocalStorage()
  if (!cfg?.url || !cfg.token) return
  const base = cfg.url.replace(/\/$/, '')
  const body = 'all' in opts && opts.all
    ? { all: true }
    : { session_ids: (opts as { ids: string[] }).ids }
  const res = await fetch(base + '/api/sessions/seen', {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${cfg.token}`,
      'Content-Type': 'application/json',
    },
    credentials: 'omit',
    body: JSON.stringify(body),
  })
  if (res.status === 401) throw new Error('relay_unauthorized')
  if (!res.ok) throw new Error(`mark-seen: HTTP ${res.status}`)
}
```

### Relay: Bearer bypass for CSRF

`internal/relay/csrfmw.go`:

```go
func RequireCSRF(resolver *IdentityResolver, inner http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case http.MethodGet, http.MethodHead, http.MethodOptions:
            inner.ServeHTTP(w, r); return
        }
        if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
            // Bearer tokens are not auto-attached by browsers, so CSRF does
            // not apply. Auth is still enforced — inner handlers call
            // Resolver.Resolve / IsUser themselves.
            inner.ServeHTTP(w, r); return
        }
        // existing cookie + X-CSRF-Token path unchanged
    })
}
```

`handleSessionsSeenHTTP` is not modified — it already calls
`Resolver.Resolve(r)` and rejects non-user principals with 401.

## Data flow

```
relay (POST /api/sessions/seen)  ←──┐
        │                            │
        │ Bearer or cookie+CSRF       │
        │                            │
   csrfmw.RequireCSRF                 │
        │                            │
   handleSessionsSeenHTTP             │
        │ Store.SetSeen               │
        │                            │
   client-sessions WS pushes refreshed SessionInfo (unread recomputed)
        │
   MobileApp ←── platform.events
        │
   MobileSessionList.refresh()
        │
   sessions.value reflects new unread → UI updates
```

The mobile list does not optimistically clear unread — it re-fetches after the
POST and lets the relay's recomputed `unread` flag drive the UI.

## Testing

### Go

- `csrfmw_test.go` (new if absent): Bearer header → inner runs; non-Bearer
  POST without cookie → 401; GET → passthrough.
- `sessions_seen_http_test.go` (extend):
  - `TestSessionsSeen_Bearer_MarkAll` → 204 + Store.SetSeen called for owner's
    sessions.
  - `TestSessionsSeen_Bearer_CrossUser` → other user's sessions silently
    ignored.
  - `TestSessionsSeen_Bearer_InvalidToken` → 401.
  - Existing cookie + CSRF cases continue to pass.

### Vitest (frontend)

- `lib/sessionLabel.test.ts` (new): unit tests for `commandLabel`,
  `fullCommand`, `rowTitle`, `hostName`. These functions previously lived in
  `TaskGroupedList.vue` (untested as exports); pulling them out is a good
  moment to add coverage.
- `composables/useSessions.test.ts` (existing) is unchanged — `useSessions`
  itself is untouched and its tests should continue to pass.
- `mobile/MobileSessionList.test.ts` (extend / create):
  - Renders host + state groupings.
  - `group-toggle` click switches mode (mock `useTaskGroupBy`).
  - Row ✓ → `platform.sessions.markSessionsSeen({ ids:[id] })`.
  - Group ✓ → `markSessionsSeen({ ids:[unread ids in group] })`.
  - Footer ✓ → `markSessionsSeen({ all: true })`.
  - completed-seen fold default closed, expands on tap, items not in main
    groups.
  - 401 from `markSessionsSeen` → `tokenInvalid` event.
- `platform/capacitor.test.ts` (extend / create):
  - `listRemoteSessions` maps `unread` and `attention_at`.
  - `markSessionsSeen({ ids })` posts `session_ids` + Bearer header.
  - `markSessionsSeen({ all: true })` posts `{ all: true }`.
  - `401` → throws `relay_unauthorized`.

Existing `App.test.ts` / `App.theme.test.ts` run unchanged. The only
desktop touchpoint is `TaskGroupedList.vue` swapping local helpers for
`lib/sessionLabel` imports — its existing tests should pass without
modification; treat any failure as a diagnostic of the refactor, not an
excuse to mutate the test.

## Edge cases

- `useTaskGroupBy` on Capacitor: `bindings().GetTaskGroupBy()` throws → falls
  back to `localStorage["taskGroupBy"]`. Tests stub `__resetForTests` between
  cases to avoid singleton bleed.
- `useTaskPreset` is platform-agnostic; mobile reuses the same `<TaskStateIcon>`
  rendering.
- iOS safe area: card list inherits `env(safe-area-inset-*)` from existing
  `.list`; footer adds its own `padding-bottom`.
- Short command on narrow screens: keep `commandLabel`; the full command is
  available once the session is opened. No tooltip on mobile.
- `markSessionsSeen` is optional on the bridge. Mobile guards with `?.` and is
  a no-op if the bridge does not implement it (defensive — Wails always does).

## Open risks

- Bearer-bypass on `RequireCSRF` affects every CSRF-gated route, not just
  mark-seen. This is a feature (Bearer tokens are valid out-of-browser
  credentials) but the test plan does not exhaustively re-test every gated
  route. Spot-check the auth flow during manual verification.
- `commandLabel` / `hostName` extraction touches the desktop
  `TaskGroupedList.vue` — run desktop tests to confirm no regression.
- `useSessions.completedSeen` includes both `completed && !unread` AND
  `failed && !unread`. The mobile fold therefore also shows seen-failed
  sessions, not just seen-completed ones. This is intentional (parity with
  desktop) but slightly broader than a literal "completed only" reading of
  the section above.
