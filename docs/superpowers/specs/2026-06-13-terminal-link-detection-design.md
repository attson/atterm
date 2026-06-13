# Terminal Link Detection & Mod+Click Open

Date: 2026-06-13
Scope: desktop (Wails) only — mobile/iOS deferred

## Goal

Recognize URLs and on-disk paths in terminal output, decorate them on hover, and
let users open them via Mod+Click (⌘ on macOS, Ctrl elsewhere). Add matching
"Open Link" / "Copy Link" entries to the terminal context menu.

## What gets detected

1. `http://…` and `https://…`
2. `file://…`
3. Absolute POSIX paths starting with `/` (e.g. `/usr/local/bin`, `/var/log/x.log`)
4. Home-anchored paths starting with `~/` (e.g. `~/Projects/foo`)

Detection only spans a single visual buffer line. URLs hard-wrapped by the
terminal stay broken in half — matches the simplest practical behavior; can be
revisited later.

Trailing punctuation stripping:
- Always strip from the right end: `.`, `,`, `;`, `:`, `"`, `'`, `!`, `?`.
- For `)` and `]`: only strip if the match has no matching `(` or `[` inside
  it (i.e. `https://en.wikipedia.org/wiki/Foo_(bar)` keeps the trailing
  `)`, but `(see https://example.com)` matches `https://example.com` and
  drops the `)`).
- Unicode quotes (`”`, `’`, `」` etc.) are NOT stripped this round —
  ASCII-only to keep the rule simple.

Out of scope (this release): relative paths, `file:line:col` syntax, `mailto:`,
`ssh://`, raw email addresses.

## What does NOT change

- Existing selection / copy / paste flows untouched.
- Mobile (Capacitor) build untouched — no link provider registered, no menu
  changes. A future spec will design the popover-based open action.
- WebGL renderer behavior, OSC 133, replay progress, bell, etc. untouched.

## Architecture

```
TerminalView.vue (ensureTerm)
  ├─ useTerminalLinkProvider({ term, openURL, isMac, getHomeDir })  → IDisposable
  │     └─ term.registerLinkProvider({ provideLinks })
  │           ├─ provideLinks(y, cb): cb(detectLinks(line).map(toILink))
  │           └─ ILink.activate(ev, text):
  │                 if (!isModClickEvent(ev, isMac)) return
  │                 openURL(normalizeForOpen(match, homeDir))
  └─ onContextMenu (existing)
        └─ if detectLinks(line) hit covers click col:
              menu.push({ "open-link", "copy-link" })

lib/terminalLinks.ts (pure, unit-testable)
  detectLinks(line)        — regex sweep + trailing-punct trim
  normalizeForOpen(m, home) — http/file passthrough, path → file://, ~/ → home/

platform.system.openExternalURL(url)
  ├─ wails:     BrowserOpenURL(url)
  └─ capacitor: window.open(url)   (not invoked this round)
```

Three layers, hard boundaries:

- **`lib/terminalLinks.ts`** — pure string logic. No DOM, no xterm. All regex
  and normalization live here so the link provider and the context menu hit
  the same judgment.
- **`composables/useTerminalLinkProvider.ts`** — adapter from xterm's
  `(bufferLineNumber, callback)` to `detectLinks(line)`, plus Mod-key
  guard on `activate`.
- **`TerminalView.vue`** — registers the provider in `ensureTerm()` and
  augments the existing context menu with link-aware items.

## Components

### `lib/terminalLinks.ts` (new)

```ts
export type LinkKind = 'http' | 'file' | 'path'

export interface LinkMatch {
  start: number      // inclusive column in line
  end: number        // exclusive column in line
  text: string       // already trimmed of trailing punctuation
  kind: LinkKind
}

export function detectLinks(line: string): LinkMatch[]
export function normalizeForOpen(m: LinkMatch, homeDir?: string): string | null
export function isModClickEvent(e: MouseEvent, isMac: boolean): boolean
```

Behavior contract:
- `detectLinks` returns matches in left-to-right order, non-overlapping.
- `normalizeForOpen` returns `null` if the match is a `~/…` path and `homeDir`
  is empty/undefined; caller toasts an error.
- `isModClickEvent`: mac requires `metaKey && !ctrlKey`; non-mac requires
  `ctrlKey && !metaKey`. Either way, `altKey`/`shiftKey` must be false to
  avoid colliding with existing terminal shortcuts.

### `composables/useTerminalLinkProvider.ts` (new)

```ts
interface Deps {
  term: Terminal
  isMac: boolean
  getHomeDir: () => string                // resolved once at register-time
  openURL: (url: string) => Promise<void>
  onError: (key: I18nKey) => void         // toast hook
}
export function useTerminalLinkProvider(deps: Deps): IDisposable
```

(Function name matches the file name and the project's `useXxx` convention,
e.g. `useTerminalShortcuts.ts`.)

`provideLinks(y, cb)` is called by xterm with a 1-indexed buffer line number.
The corresponding `term.buffer.active.getLine(y - 1)` returns the line; call
`.translateToString(true)` to get plain text. Then run `detectLinks`, and map
each match to:

```ts
{
  range: { start: { x: m.start + 1, y }, end: { x: m.end, y } },
  text: m.text,
  decorations: { underline: true, pointerCursor: true },
  activate: (ev, text) => {
    if (!isModClickEvent(ev, isMac)) return
    const url = normalizeForOpen(m, homeDir)
    if (!url) { onError('terminal.link.openFailedNoHome'); return }
    openURL(url).catch(() => onError('terminal.link.openFailed'))
  },
}
```

xterm's hover decoration draws the underline + pointer cursor automatically.
No custom CSS needed.

### `components/TerminalView.vue` (modified)

Two minimal changes:

1. **`ensureTerm()` tail** — after WebGL load, after OSC 133 wiring:
   ```ts
   const homeDir = await api.getUserHomeDir().catch(() => '')
   linkProviderDisposer = useTerminalLinkProvider({
     term, isMac, getHomeDir: () => homeDir,
     openURL: (u) => platform.system.openExternalURL(u),
     onError: (k) => emit('toast', t(k)),
   })
   ```
   Dispose in the existing component teardown alongside `resizeObserver` etc.

2. **Context menu** — the existing `onContextMenu` handler that calls
   `collectContextMenuItems(...)` already gets click coords. Before
   constructing the final menu, compute `(col, row)` via existing
   `terminalCellCoords.ts`, run `detectLinks(bufferLineAt(row))`, find the
   match covering `col`. If found, prepend two items:

   ```ts
   { id: 'open-link', label: t('terminal.contextMenu.openLink'),
     action: () => platform.system.openExternalURL(normalizeForOpen(hit, homeDir)) },
   { id: 'copy-link', label: t('terminal.contextMenu.copyLink'),
     action: () => navigator.clipboard.writeText(hit.text) },
   ```

   Existing menu items follow. No reordering.

### Home-dir binding (already exists)

`desktop/app.go:732 GetUserHomeDir()` and `lib/api.ts:517 getUserHomeDir()`
are already wired and used by `TaskGroupedList.vue` /
`MobileSessionList.vue`. Reuse as-is — no Go / api.ts changes needed.

### `i18n/messages/{zh,en}.ts` (modified)

Add four keys:
- `terminal.contextMenu.openLink` — "Open Link" / "打开链接"
- `terminal.contextMenu.copyLink` — "Copy Link" / "复制链接"
- `terminal.link.openFailed` — "Failed to open link" / "无法打开链接"
- `terminal.link.openFailedNoHome` — "Cannot resolve `~` (home dir
  unavailable)" / "无法解析 `~`（拿不到 home 目录）"

## Data flow

```
PTY output → xterm buffer
            ↓
xterm calls provideLinks(y, cb)
            ↓
detectLinks(line) → ILink[]
            ↓
hover: underline + pointer  (xterm built-in)
            ↓
Mod+Click → activate
            ↓
isModClickEvent + normalizeForOpen
            ↓
platform.system.openExternalURL → BrowserOpenURL → OS default handler
```

Right-click path:

```
mousedown right → onContextMenu
            ↓
terminalCellCoords → (col, row)
            ↓
detectLinks(bufferLineAt(row))
            ↓
hit covering col? → menu += [open-link, copy-link]
            ↓
existing menu items appended
```

## Error handling

| Failure                                  | Behavior                                                          |
| ---------------------------------------- | ----------------------------------------------------------------- |
| `detectLinks` receives null/garbage      | Returns `[]`. No throw.                                           |
| `normalizeForOpen` lacks `homeDir` for `~/…` | Returns `null`. Toast `terminal.link.openFailedNoHome`.       |
| `openExternalURL` rejects                | `console.warn` + toast `terminal.link.openFailed`.                |
| `term.registerLinkProvider` throws (test env, no proposed API) | `try/catch`, `console.warn`, continue — terminal still usable. |
| Right-click cell-coord math fails        | Swallow → menu rendered without link items. No throw to UI.       |
| Clipboard write fails (no permission)    | Toast generic copy-failed (already exists).                       |

No silent failures on the user-facing path; all surface as toasts using the
existing `emit('toast', …)` channel.

## Testing

### Unit — `lib/terminalLinks.test.ts`

`detectLinks`:
- Plain `https://example.com`.
- URL followed by `.` / `,` / `)` / `]` / `"` — trailing punct trimmed.
- URL inside balanced parens: `(https://example.com)` — match = URL, parens left in line.
- URL with `)` as part of path: `https://en.wikipedia.org/wiki/Foo_(bar)` — match keeps trailing `)`.
- URL with query + fragment: `https://x.com/p?a=1&b=2#z`.
- Multiple URLs on one line.
- `file:///tmp/x.log` + `file:///C:/Users/foo`.
- Absolute paths: `/usr/local/bin`, `/var/log/x.log`, `/`.
- `~/Projects/foo`, `~/`, `~/.config/x`.
- NOT matched: `./foo`, `foo.go`, `make`, `~foo`, `12/24`, bare `example.com`.

`normalizeForOpen`:
- `http://x` → `http://x`.
- `file:///tmp` → `file:///tmp`.
- `/usr/local` → `file:///usr/local`.
- `~/x` + `/Users/me` → `file:///Users/me/x`.
- `~/x` + `""` → `null`.

`isModClickEvent`:
- mac + `metaKey` → true; mac + `metaKey + shiftKey` → false.
- non-mac + `ctrlKey` → true; non-mac + `ctrlKey + altKey` → false.
- mac + `ctrlKey` only → false; non-mac + `metaKey` only → false.

### Unit — `composables/useTerminalLinkProvider.test.ts` (new)

Mock a minimal `Terminal` with `registerLinkProvider` + `buffer.active.getLine`:
- `provideLinks` for a line with one URL → `cb` called with one ILink whose
  range matches.
- `activate` with no modifier → `openURL` NOT called.
- `activate` with Mod → `openURL` called with normalized text.
- `activate` for `~/x` with empty homeDir → `onError` called, `openURL` not.

### Integration — `TerminalView.test.ts` (extend)

- Render TerminalView, write a line containing a URL via `term.write`, simulate
  right-click on that cell, expect the rendered menu to include the Open/Copy
  items.
- Simulate a Mod+click on the URL cell, expect the mocked
  `platform.system.openExternalURL` to be called with the URL.

No new snapshot tests; no changes to existing snapshots expected.

## Out-of-scope guardrails

- `mobile/MobileTerminal.vue` is NOT touched. A follow-up spec will design the
  selection-popover "Open" action.
- No new dependencies. `xterm-addon-web-links` is rejected (see brainstorm
  notes: only covers http(s), still requires custom provider for file/paths,
  modifier-key gating is awkward).
- No cross-line URL stitching. Hard-wrapped URLs are intentionally
  half-matched; revisit when users complain.

## Rollout

Behind no feature flag (per repo convention: single-user project, replace not
deprecate). The change is purely additive at the UI layer — terminals
without matched lines render identically to today.
