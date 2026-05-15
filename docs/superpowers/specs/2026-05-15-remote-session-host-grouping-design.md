# Remote session picker — group by host

Status: design
Date: 2026-05-15

## Problem

The remote-session selection surfaces list every visible session in a flat
grid. When a user has more than one machine reporting to the same relay, the
list is hard to scan: sessions from different hosts interleave, and the user
must read the per-card host line to figure out where a session lives.

We want the picker UIs to group sessions by host so the user can find a
machine first and a session second.

The change is presentation-only. The backend already emits `host_id`,
`host`, and `user` on every `SessionInfo`; no protocol or wire-format work
is needed.

## Surfaces affected

Three UI entry points across two clients render the list and all three need
the same grouping treatment:

| File | Role |
| --- | --- |
| `desktop/frontend/src/components/RemoteSessionsDialog.vue` | Pure remote list (opened from the remote-sessions shortcut). |
| `desktop/frontend/src/components/SessionPickerDialog.vue` | Local + remote picker. Only the **remote** section is grouped; the local section stays flat. |
| `web/app.js` (`renderList`) | The web client always views the relay, so its single flat grid becomes the grouped grid. |

The Go relay is untouched. `internal/relay/client_sessions_conn.go` still
sorts by session ID; ordering inside a group is left to the backend.

## Grouping semantics

- **Group key:** `host_id`. Sessions whose `host_id` is empty go into a
  single synthetic `__unknown__` group.
- **Group display name:** the `host` (hostname) of the session in the group
  with the most recent `started_at`. If every session in a group has an
  empty `host`, the display name is the literal string `unknown host`.
- **Group order:** ascending lexicographic on the display hostname. The
  `__unknown__` group is always last regardless of its display name.
- **Within a group:** keep the order the backend sent. No re-sorting.

Why `host_id` and not `hostname`: `host_id` is the stable per-install
identity. Reinstalling the OS rotates `host_id` (correctly producing a new
group), and renaming the host keeps the same `host_id` (correctly keeping
the group merged, with the freshest hostname shown in the header).

## Visual design

Each group renders as:

```
mac-mini · 3f9a2c1d · 4 sessions
[card] [card] [card] [card]

attson-air · b1c70a82 · 2 sessions
[card] [card]

unknown host · 1 session
[card]
```

- Header is one line. `hostname` in normal foreground, monospace.
- `host_id` shown as the first 8 hex chars in `var(--fg-dim)`, with the full
  `host_id` exposed via the `title` attribute. Omitted entirely for the
  `unknown` group.
- `N session(s)` count in `var(--fg-dim)`.
- Groups are separated by vertical space; no rule.
- Groups are not collapsible.
- Cards inside a group no longer repeat hostname / host_id:
  - `RemoteSessionsDialog` cards drop the `.host` block.
  - `SessionPickerDialog` remote cards keep `user` only (replacing the
    current `user@host`); the span is hidden when `user` is empty.
- Web cards drop the `.host` block.

## Implementation

### Shared TS helper (desktop only)

Add `desktop/frontend/src/lib/sessions.ts`:

```ts
import type { SessionInfo } from "./connection";

export type SessionGroup = {
  key: string;          // host_id, or "__unknown__"
  hostname: string;     // display name; "unknown host" if every host is empty
  hostId: string;       // raw host_id; "" for the unknown group
  sessions: SessionInfo[];
};

export function groupSessionsByHost(sessions: SessionInfo[]): SessionGroup[];
```

Behaviour:

1. Bucket each session by `s.host_id || "__unknown__"`. Preserve input
   order inside each bucket.
2. For each bucket, pick a display name: scan the bucket for the entry
   with the greatest `started_at` and a non-empty `host`; fall back to
   `"unknown host"` if no entry has a non-empty `host`.
3. Sort buckets by display name (ascending, `localeCompare`). Force the
   `__unknown__` bucket to the end regardless.
4. Return the ordered list.

### Web helper

`web/app-core.js` (the existing pure-function module the web client uses
for testable helpers) gains an exported `groupSessionsByHost(sessions)`
with semantics matching the TS version. `web/app.js` imports it. The
logic is duplicated across desktop and web because `web/` has no build
step, and pulling the desktop bundle in would be disproportionate.

### Vue component changes

`RemoteSessionsDialog.vue`:

- Import `groupSessionsByHost`, compute `const groups = computed(...)`.
- Replace the single `<div class="grid">` with one `<section
  class="host-group">` per group containing a `<header>` and the inner
  grid.
- Remove the `.host` block from each card.

`SessionPickerDialog.vue`:

- Leave the `local` section unchanged.
- Compute `remoteGroups` from `remoteOptions` via the helper.
- Render `remote` as a list of host groups, each with the same header
  shape as `RemoteSessionsDialog`. The existing `<h3>remote</h3>`
  subtitle stays as the section heading above the groups.
- In each remote card, replace the `user@host` span with `user` (hidden
  when `user` is empty). Keep `cmd` and `cwd`.

### Web changes

`web/app.js`:

- Import `groupSessionsByHost` from `./app-core.js`.
- Rewrite `renderList` to iterate groups: for each group, append a
  `<section class="host-group">` with a `<header>` and a `<div
  class="grid">` of cards. Remove the `.host` block from card markup.

`web/style.css`: add `.host-group` and `.host-group > header` rules that
match the desktop look (header is dim, monospace, with the `host_id` and
count spans styled in `var(--fg-dim)`).

## Edge cases

- **Empty list:** unchanged. The existing empty-state copy renders; the
  grouping helper is not invoked.
- **All sessions lack `host_id`:** one `unknown host` group containing
  everything.
- **Same `host_id`, different hostnames** (rename mid-life): single group;
  header shows the freshest hostname. The `title` attribute on the
  `host_id` span surfaces the full hex for disambiguation.
- **Non-empty `host_id`, empty `host`:** group still keyed on `host_id`;
  header displays `unknown host`. The `host_id` span is still shown.

## Testing

### Unit tests

Tests run on both sides since the logic is duplicated:

- `desktop/frontend/src/lib/sessions.test.ts` — vitest is already
  configured via `desktop/frontend/vitest.config.ts`.
- `web/app-core.test.mjs` — extend the existing `node:test` file with
  cases for the new `groupSessionsByHost` export.

Cases (shared between the two suites):

1. Multiple host_ids, multiple sessions each → groups in correct
   lexicographic order, no `unknown` group.
2. Mix of empty and non-empty `host_id` → `unknown` group last.
3. All `host_id` empty → single `unknown` group.
4. Same `host_id`, two sessions with different `host`/`started_at` →
   display name comes from the session with the larger `started_at`.
5. `host_id` set, all `host` empty → display name is `unknown host`,
   group is **not** treated as the `__unknown__` group (i.e. it does
   not get forced to the end).
6. Empty input → `[]`.

### Manual verification

- **Desktop:** with two devices (or a single device running two atterm
  instances faking different `host_id`s), open the remote-sessions dialog
  and the session picker. Confirm:
  - Two host groups, alphabetical.
  - Local section in the picker is unchanged.
  - Adding/closing a session updates counts and group composition.
- **Web:** open the relay site in a browser with the same two-host
  setup. Confirm grouped layout and that an `unknown host` group
  appears if a session is constructed without `host_id` (can be tested
  by temporarily clearing `host_id` in a mock payload).

## Out of scope

- Collapsible groups (deferred; reconsider when a user has many hosts).
- Re-sorting within a group (defer to backend).
- Filtering / search across hosts.
- Any change to the Go backend or to `SessionInfo` wire format.
