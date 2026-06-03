# Session type classification (design)

Date: 2026-06-04
Status: Draft (design phase); pending implementation plan
Roadmap item: P2.11

## 1. Goal

Tag each relay session with a `type` reflecting what kind of workload
it's used for (`ai`, `test`, `build`, `deploy`, or `shell`), derived
server-side from the command line and propagated to every client over
the existing `SessionInfo` channel. Surface the tag as a small chip /
icon on the desktop tab bar, the mobile session list, and the web
session list so users can scan a long list and find "my Claude
session" or "my npm test session" at a glance.

After this lands:

- A new `Type` field appears on `proto.SessionInfo`. Default value
  `"shell"`.
- A new `internal/session/classify.go` holds the classification rules.
- When OSC 133 reports a new command, the session's `Type` updates IF
  the classification is non-shell (sticky non-shell).
- All three frontends (desktop TabBar, mobile MobileSessionList, web
  SessionList) render a colored chip / inline icon for non-shell
  sessions; shell sessions render no chip to keep the list quiet.
- 4 new i18n keys (`mobile.taskTypes.{ai,test,build,deploy}`) in
  English + Chinese.

Out of scope:

- User-configurable classification rules (operators add a YAML / env
  override later if needed).
- Detecting *intent* changes — e.g. the user starts `claude`, then
  exits and starts `npm test`. The "sticky non-shell" rule covers
  the natural transitions; resetting a session's type without
  closing it isn't worth a UI affordance yet.
- Surface in the desktop pane grid (only the tab bar gets the icon
  for now; the pane interior already shows live command text).
- Counting / filtering by type in the lists (just a visual tag now;
  filtering is a separate feature if anyone asks).

## 2. Architecture

```
┌── relay (Go) ─────────────────────────────────────────────────────────┐
│                                                                        │
│  internal/session/                                                     │
│    classify.go (new)                                                   │
│      const SessionTypeShell  = "shell"                                 │
│      const SessionTypeAI     = "ai"                                    │
│      const SessionTypeTest   = "test"                                  │
│      const SessionTypeBuild  = "build"                                 │
│      const SessionTypeDeploy = "deploy"                                │
│      func ClassifyCommand(cmd string) string                           │
│                                                                        │
│    session.go (modified)                                               │
│      • meta.Type defaults to "shell" at session creation               │
│      • applyOSC133Locked('C'): compute ClassifyCommand(currentCommand) │
│        — if non-shell, set s.meta.Type and broadcast new META          │
│      • broadcastCurrentMeta now triggers on Type change too            │
│                                                                        │
│  internal/proto/frame.go (modified)                                    │
│    type SessionInfo struct {                                           │
│      // ...                                                            │
│      Type string `json:"type,omitempty"`                               │
│    }                                                                   │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘

       SessionInfo flows through META frame + /api/sessions JSON
                                  │
                                  ▼
┌── three frontends ────────────────────────────────────────────────────┐
│                                                                       │
│  desktop/frontend/src/lib/connection.ts: SessionInfo.type?: string    │
│  desktop/frontend/src/platform/types.ts: RemoteSession.type?: string  │
│  web/src/shared/api/types.ts: SessionInfo.type?: string               │
│                                                                       │
│  desktop/frontend/src/lib/sessionType.ts (new — pure helper)          │
│    • typeLabel(t) returns the i18n key for a chip                     │
│    • typeColor(t) returns a tailwind-ish color token                  │
│    • typeIcon(t)  returns inline SVG path (or null for shell)         │
│                                                                       │
│  Renderers:                                                           │
│    desktop TabBar.vue        — prepend a 14×14 SVG icon to the title  │
│    mobile MobileSessionList  — chip on the task card title row        │
│    web SessionList.vue       — chip mirroring the mobile look         │
│                                                                       │
│  i18n: 4 keys per locale (en + zh-CN)                                 │
│    mobile.taskTypes.ai     = "AI" / "AI"                              │
│    mobile.taskTypes.test   = "Test" / "测试"                          │
│    mobile.taskTypes.build  = "Build" / "构建"                         │
│    mobile.taskTypes.deploy = "Deploy" / "部署"                        │
│                                                                       │
└───────────────────────────────────────────────────────────────────────┘
```

## 3. Classification rules

`ClassifyCommand(cmd string) string` is pure: same input → same
output, no I/O. Algorithm:

1. **Tokenize** on whitespace. Strip empty / leading-pure-whitespace
   tokens. If zero tokens after that, return `"shell"`.
2. **Strip common prefixes** in a loop, until the first remaining
   token isn't one of:
   - Wrappers that take a command as an argument: `sudo`, `time`,
     `nice`, `env`.
   - Env-var assignments matching `^[A-Z_][A-Z0-9_]*=` (POSIX-y env
     prefix like `DEBUG=1 ./run.sh`).
3. **Basename** the first surviving token (drop any path prefix:
   `/usr/local/bin/claude` → `claude`).
4. **Match** the resulting (first, second) token pair against the
   table:
   - first ∈ {`codex`, `claude`, `gemini`, `aider`} → `"ai"`
   - first ∈ {`kubectl`, `terraform`} → `"deploy"`
   - first == `docker-compose` → `"build"`
   - first == `docker` AND second ∈ {`build`, `compose`} → `"build"`
   - first ∈ {`go`, `npm`, `pnpm`, `yarn`, `cargo`} AND second == `test` → `"test"`
   - otherwise → `"shell"`

Notes:

- The second-token rules deliberately don't match `go run`, `npm
  install`, etc. — those are shell-like background tasks, not the
  workflow-defining categories we want to surface.
- Pattern table lives in `classify.go`, hardcoded. Adding a new tool
  is a one-line edit + one new test case.
- We don't try to recognise programs invoked via `npx claude`,
  `pipx run aider`, etc. yet — the classifier sees `npx` as the
  command name. Future enhancement if it gets common.

## 4. Application timing — sticky non-shell

In `session.go`'s `applyOSC133Locked`, the `C` branch already sets
`s.meta.CurrentCommand = ...`. Right after that:

```go
if newType := ClassifyCommand(currentCommand); newType != SessionTypeShell {
    if s.meta.Type != newType {
        s.meta.Type = newType
        metaChanged = true
    }
}
```

If the new command's classification is `"shell"`, we leave the
existing `Type` alone — that's the stickiness. The session keeps its
last meaningful tag even while idle in a prompt or running ad-hoc
shell commands between AI runs.

Initial value `s.meta.Type = SessionTypeShell` is set when the
session is created (in the constructor or `New`).

A `metaChanged` flag drives `broadcastCurrentMeta()` the same way
existing P0 fields do; no new broadcast path required.

## 5. Frontend type → display helpers

A new pure module `desktop/frontend/src/lib/sessionType.ts`:

```ts
export type SessionType = 'shell' | 'ai' | 'test' | 'build' | 'deploy'

export interface TypeDisplay {
  key: 'ai' | 'test' | 'build' | 'deploy'   // i18n key suffix
  color: string                              // CSS color or design token
  // Inline SVG path-d strings (16×16 viewBox) — kept in TS so
  // every renderer can drop one in without a separate icon dep.
  iconPath: string
}

export function displayForType(t: string | undefined): TypeDisplay | null {
  // Returns null for 'shell' / undefined / unknown so renderers
  // can hide the chip with no extra branching.
}
```

Color palette (just enough to be distinguishable):

| type    | color (light/dark agnostic) | meaning            |
|---------|------------------------------|--------------------|
| ai      | `#a78bfa` (violet-400)       | LLM driver         |
| test    | `#34d399` (emerald-400)      | test runner        |
| build   | `#fbbf24` (amber-400)        | image/asset build  |
| deploy  | `#f87171` (red-400)          | infra apply        |

Icon paths use lucide-style strokes (16×16 viewBox, currentColor
stroke). The shared module owns the path strings so the three
renderers don't drift.

## 6. Renderer-level changes

### 6.1 Desktop `TabBar.vue`

Tab template already has a `.dot` and a `.title`. Insert a small
`.type-icon` between them when `displayForType(activeSession?.type)`
is non-null:

```vue
<span v-if="typeForTab(t)" class="type-icon" :title="typeTitle(t)">
  <svg width="12" height="12" viewBox="0 0 16 16" :style="{ color: typeForTab(t).color }">
    <path :d="typeForTab(t).iconPath" />
  </svg>
</span>
```

No layout change beyond a 4px-margin span. Existing `.dot` and
`.title` keep their styles.

### 6.2 Mobile `MobileSessionList.vue`

In the task card title row, prepend a `<span class="type-chip">`
with the localised label when the type is non-shell. Existing
`.dot`, `.col2`, `.open` siblings unchanged.

```vue
<span v-if="typeForSession(s)" class="type-chip" :style="{ '--chip': typeForSession(s).color }">
  {{ t(`mobile.taskTypes.${typeForSession(s).key}`) }}
</span>
```

CSS:

```css
.type-chip {
  font-size: 0.66rem; line-height: 1;
  padding: 2px 6px; border-radius: 4px;
  border: 1px solid color-mix(in srgb, var(--chip) 60%, transparent);
  color: var(--chip);
  background: color-mix(in srgb, var(--chip) 12%, transparent);
  text-transform: uppercase; letter-spacing: 0.04em;
  margin-right: 6px; flex: 0 0 auto;
}
```

### 6.3 Web `SessionList.vue`

Mirrors the mobile chip exactly — same `displayForType()` helper,
same i18n key, same chip CSS shipped via the shared lib (or
copy-pasted; the web project doesn't currently share components
with mobile/desktop, but it does share `web/src/shared/api/types.ts`,
so the helper can live alongside or be duplicated).

## 7. Errors and observability

- Classification is pure and total — every input yields a string.
  Empty / whitespace-only command returns `"shell"`.
- No new log lines. The classification is a 200-ns hot-path
  computation per OSC 133 `C`; logging would dominate.
- A possible future addition: emit a Prometheus counter
  `atterm_session_classify_total{type=...}` for ops visibility.
  Out of scope for this PR.

## 8. Testing

### 8.1 `internal/session/classify_test.go`

Table-driven, ~20 cases:

```go
cases := []struct{ in, want string }{
    {"", "shell"},
    {"bash", "shell"},
    {"claude", "ai"},
    {"/usr/local/bin/claude", "ai"},
    {"claude --help", "ai"},
    {"codex", "ai"},
    {"sudo claude", "ai"},
    {"time go test ./...", "test"},
    {"DEBUG=1 npm test", "test"},
    {"yarn test", "test"},
    {"cargo test --release", "test"},
    {"docker build .", "build"},
    {"docker compose up", "build"},
    {"docker-compose up -d", "build"},
    {"docker ps", "shell"},        // not build/deploy
    {"kubectl get pods", "deploy"},
    {"terraform plan", "deploy"},
    {"gemini chat", "ai"},
    {"aider", "ai"},
    {"npx claude", "shell"},       // documented limitation
    {"go run ./cmd/foo", "shell"}, // 'go' alone is not a test
}
```

### 8.2 `internal/session/session_test.go` additions

- `TestPushOut_AssignsTypeOnNonShellCommand` — feed an OSC 133 C with
  `claude --help`, assert `s.meta.Type == "ai"` and a meta frame went
  out with that field.
- `TestPushOut_TypeStickyAfterShellCommand` — feed `claude`, then
  `ls`, assert `Type` still `"ai"`.
- `TestPushOut_TypeChangesBetweenTwoNonShells` — feed `claude`, then
  `npm test`, assert `Type` transitions to `"test"`.

### 8.3 Frontend tests

- `desktop/frontend/src/lib/__tests__/sessionType.test.ts` — covers
  `displayForType` for each of the five inputs plus undefined.
- `desktop/frontend/src/components/TabBar.test.ts` — extends the
  existing test file with a case: tab whose `activeSession.type ===
  'ai'` renders the type icon; shell tab doesn't.
- `desktop/frontend/src/mobile/__tests__/MobileSessionList.test.ts` —
  a session with `type: 'test'` shows the chip text "Test" / "测试"
  depending on locale.

## 9. Migration / rollout

- Single Go ALTER-free change — `Type` is a string field on an
  in-memory struct; SQLite isn't involved. Old `MetaPayload` JSON
  without `type` decodes fine (the field gets the zero value `""`,
  which renderers treat the same as `"shell"`).
- Clients on older bundles ignore the new field — the chip just
  doesn't render. No breakage.
- No feature flag.

## 10. Non-goals revisited

- **No per-user classification overrides** — the rule table is
  hardcoded. If a user reaches for an exotic tool that should classify
  as `ai`, a one-line PR adds it. We're not building a config plane
  yet.
- **No retroactive classification** for already-running sessions when
  the relay restarts — the in-memory `Type` resets to `shell` on
  process restart and re-classifies on the next OSC 133. Acceptable
  because reclassification fires on the next user-initiated command.
- **No analytics surface** — the chip is informational only. No
  "click to filter by type" feature yet; if users want it the chip
  is the obvious affordance.
