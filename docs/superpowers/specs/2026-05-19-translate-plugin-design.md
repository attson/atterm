# Translate Selection Plugin — Design

**Date**: 2026-05-19
**Status**: Approved (pending user spec review)
**Scope**: Desktop frontend only. No relay / backend changes.

## Problem

Users frequently encounter foreign-language text in terminal output (error
messages, log lines, command output, foreign docs). Today they copy, switch to
a browser tab, paste into Google Translate. The round-trip breaks focus.

Goal: select text in a terminal pane → right-click → "Translate selection" → a
floating panel shows source + translation without leaving the app.

## Non-goals

- Streaming token-by-token rendering (deferred)
- Global keyboard shortcut to translate (deferred — right-click only)
- Persistent translation history across app restarts (in-session memory only)
- "Copy translation" / "Insert into terminal" buttons (deferred)
- DeepL / Google Translate provider (deferred — first version OpenAI-compatible only)
- Caching identical (text, targetLang) requests
- Automatic retry with backoff
- Encrypting the API key at rest (stored plaintext in `~/.config/atterm/config.json`,
  same as other plugin config; deferred until plugin-config encryption lands globally)

## Architecture

### Plugin slot extension

Existing `PluginSlot` is `"right-panel" | "bottom-toolbar"`. Add
`"context-menu"` as a **headless** slot. A context-menu plugin does NOT render
a component into the DOM; it exports a `getMenuItems(ctx, selection)` factory
that returns menu items to merge into the terminal right-click menu.

```ts
// plugins/types.ts (additions)
export type PluginSlot = "right-panel" | "bottom-toolbar" | "context-menu"

export interface MenuItem {
  id: string                    // unique within plugin, e.g. "translate-selection"
  label: string                 // visible text
  disabled?: boolean
  onClick: () => void
}

export interface ContextMenuPlugin {
  getMenuItems(ctx: PluginContext, selection: string): MenuItem[]
}
```

The `PluginDescriptor.load()` contract becomes union-typed: for component
slots it still returns a Vue component module (`{ default: Component }`);
for `context-menu` slots it returns a `ContextMenuPlugin`.

```ts
export interface PluginDescriptor {
  id: string
  slot: PluginSlot
  title: string
  description: string
  load: () =>
    | Promise<{ default: Component }>            // component plugins
    | Promise<{ default: ContextMenuPlugin }>    // headless plugins
  defaultEnabled?: boolean
}
```

### Floating panel placement

The translation result panel is **NOT** rendered into any slot. It is
Teleported (Vue `<Teleport to="body">`) and controlled by a Pinia store
private to the translate plugin. `App.vue` mounts a single
`<TranslatePanelHost />` component at the app root so the panel exists in
the tree once and is driven entirely by the store.

This avoids inventing a third "floating" slot, but means the plugin is not
self-contained — `App.vue` must explicitly mount the host. Acceptable
tradeoff for v1 (one floating-style plugin); revisit if more arrive.

### Components

```
desktop/frontend/src/
  plugins/
    types.ts                          (extend)
    registry.ts                       (register translateDescriptor)
    configStore.ts                    (add translate field)
    PluginHost.vue                    (export collectContextMenuItems helper)
    translate/                        (NEW)
      index.ts                        (descriptor + ContextMenuPlugin impl)
      panelStore.ts                   (Pinia store for panel state)
      TranslatePanelHost.vue          (mounted by App.vue, wraps Teleport)
      TranslatePanel.vue              (the actual floating panel UI)
      TranslateSettings.vue           (settings tab — provider / key / model)
      detectLang.ts                   (heuristic: CJK presence → non-CJK target)
      providers/
        types.ts                      (TranslateProvider interface + errors)
        openai.ts                     (OpenAI-compatible impl)
  components/
    TerminalView.vue                  (call collectContextMenuItems, merge into menu)
  App.vue                             (mount <TranslatePanelHost />)
```

8 new files + 6 modified files.

### Why headless plugins

Existing plugins map 1:1 to a visible region. Context-menu contributions are
event-driven, not region-bound. Forcing them through a component slot would
mean either (a) rendering an invisible component just to register handlers,
or (b) making `slot: "context-menu"` a special case where the loaded
component never mounts. Both are surprising. A typed branch on `load()`'s
return shape makes the contract explicit: "this kind of plugin contributes
menu items; that kind renders into a slot."

## Data flow

```
1.  User selects text in xterm
2.  User right-clicks → TerminalView.onContextMenu(event)
3.  selection = term.getSelection()
4.  pluginItems = await collectContextMenuItems(pluginContext, selection)
5.  menu.items = [...hardcodedCopyPasteClear, ...pluginItems]
6.  User clicks "Translate selection"
7.  translatePanelStore.openWithSource(selection)
      - state.source = selection
      - state.targetLang = computeAutoTargetLang(selection)  (uses detectLang)
      - state.visible = true
      - dispatch doTranslate()
8.  doTranslate():
      - state.loading = true; state.error = null
      - state.currentController?.abort()                     (cancel in-flight)
      - controller = new AbortController()
      - state.currentController = controller
      - try { result = await provider.translate(source, targetLang,
                                               { signal: controller.signal }) }
        catch (e) { state.error = mapError(e); return }
        finally { state.loading = false }
      - state.result = result
      - state.history.unshift({ source, target: targetLang,
                                translated: result.translated, at: Date.now() })
      - state.history = state.history.slice(0, 5)
9.  TranslatePanel renders state reactively
10. User changes targetLang dropdown → doTranslate() with new target
11. User closes panel → state.visible = false (state retained for re-open)
```

### Auto target-language heuristic

`detectLang.ts` exports `computeAutoTargetLang(text, config) → string`:

- If `text` contains any CJK Unified Ideograph (U+4E00–U+9FFF, also
  catches CJK Extension A–F via a slightly wider range), and config's
  `defaultTargetLang` is `"zh-CN"`, return `"en"`. (The text is already
  Chinese, user probably wants English.)
- Otherwise return `config.defaultTargetLang` (default `"zh-CN"`).

No call to provider's language detection. Provider's
`detectedSrcLang` in the result is informational only — displayed in panel
("Detected: English") but doesn't drive auto-target.

### Provider interface

```ts
// providers/types.ts
export interface TranslateProvider {
  translate(
    text: string,
    targetLang: string,
    opts: { signal: AbortSignal },
  ): Promise<TranslateResult>
}

export interface TranslateResult {
  translated: string
  detectedSrcLang: string         // ISO 639-1 ("en", "zh", "ja"...) or "unknown"
}

export class TranslateError extends Error {
  constructor(public code: TranslateErrorCode, message: string,
              public httpStatus?: number, public providerBody?: string) {
    super(message)
  }
}

export type TranslateErrorCode =
  | "auth"       // 401/403
  | "rate_limit" // 429
  | "server"     // 5xx
  | "network"    // fetch threw
  | "timeout"    // AbortController fired due to 30s timeout
  | "aborted"    // user-triggered cancel (e.g. switched targetLang); silent
  | "parse"      // provider returned non-JSON in JSON mode
  | "unknown"
```

### OpenAI-compatible provider

Endpoint: `POST {baseUrl}/v1/chat/completions` (baseUrl defaults to
`https://api.openai.com`). Auth header: `Authorization: Bearer <apiKey>`.

Request body (preferred path — JSON response format):
```json
{
  "model": "<config.model>",
  "messages": [
    { "role": "system", "content": "You are a translation engine. Detect the source language and translate to {{targetLang}}. Respond with strict JSON: {\"detectedSrcLang\":\"<ISO 639-1 code>\",\"translated\":\"<translation>\"}. No commentary, no markdown." },
    { "role": "user", "content": "<source text>" }
  ],
  "response_format": { "type": "json_object" },
  "temperature": 0.2
}
```

Parse `choices[0].message.content` as JSON, extract `translated` +
`detectedSrcLang`.

**Fallback for endpoints that don't support `response_format`** (some
local vLLM builds, certain proxy providers):

- First request always sends `response_format`
- On HTTP 400 with body matching `/response_format|json_object/i`, OR when
  parse fails, mark the provider instance with `supportsJsonMode = false`
- Subsequent requests drop `response_format` and use a stricter system prompt:
  `"... Output ONLY the translation, no explanation, no quotes."`
- Result built as `{ translated: rawContent.trim(), detectedSrcLang: "unknown" }`

Timeout: 30 seconds. Implemented with `setTimeout(() => controller.abort(), 30000)`.
Disambiguate timeout vs user-cancel by setting a flag on the store before
aborting from the timeout handler.

## Error handling

| Scenario | UI response |
|---|---|
| `selection` empty | `getMenuItems` returns `[]`, menu item absent |
| Provider/key not configured (panel checks before dispatching) | Panel shows "Translate plugin not configured" + button "Open settings" |
| `auth` (401/403) | Panel error banner: `"Auth failed (401)"` + truncated provider body |
| `rate_limit` (429) | Banner: `"Rate limited (429), retry shortly"` + Retry button |
| `server` (5xx) / `network` | Banner: `"Translate failed: <message>"` + Retry |
| `timeout` | Banner: `"Timed out after 30s"` + Retry |
| `aborted` | Silent (user triggered) |
| `parse` | Banner: `"Provider returned non-JSON, showing raw output"` and panel shows raw `content` as `translated` |

Retry button re-dispatches `doTranslate()` with current `source` + `targetLang`.

## Settings UI

A new `TranslateSettings.vue` accessed via the existing plugin settings
mechanism (same path as QuickInput settings — through `SettingsPlugins.vue`'s
per-plugin "Configure" button). Form fields:

| Field | Default | Notes |
|---|---|---|
| Provider | `openai-compatible` | Only option in v1; future providers add to enum |
| Base URL | `https://api.openai.com` | User can point at any OpenAI-compatible host |
| API key | (empty) | Plaintext stored in `~/.config/atterm/config.json` |
| Model | `gpt-4o-mini` | Free-text; user picks per their endpoint |
| Default target language | `zh-CN` | One of the hardcoded codes: `zh-CN`, `en`, `ja`, `ko`, `de`, `fr`, `es` |

Stored under `config.translate = { provider, baseUrl, apiKey, model, defaultTargetLang }`.

## Config persistence

Add `translate` field to plugin config:

```ts
// plugins/configStore.ts
export interface TranslateConfig {
  provider: "openai-compatible"
  baseUrl: string
  apiKey: string
  model: string
  defaultTargetLang: string       // one of the hardcoded target codes
}

export interface PluginConfig {
  quickInput: QuickInputConfig
  fileExplorer: FileExplorerConfig
  translate: TranslateConfig      // NEW
}
```

`getDefaultConfig()` returns:
```ts
translate: {
  provider: "openai-compatible",
  baseUrl: "https://api.openai.com",
  apiKey: "",
  model: "gpt-4o-mini",
  defaultTargetLang: "zh-CN",
}
```

Backward compatibility: on load, if the stored config lacks `translate`,
merge in defaults. Same pattern existing code uses for new plugin fields.

## Panel UX

- Default position: centered horizontally in the viewport, 80px from top of the viewport (panel is `position: fixed` since it's Teleported to body)
- Drag handle: panel header bar
- Position clamping: `clamp(8, x, viewport.width - panelWidth - 8)` likewise for y
- Width: 480px, height: auto (min 240px)
- Close button (×) top-right
- Source section: read-only, monospace (terminal output is often code-like), max-height 200px scrollable
- Detected language label: `Detected: <name>` (`<unknown>` if provider didn't say)
- Target dropdown: zh-CN / en / ja / ko / de / fr / es (hardcoded v1 list — same as
  settings options). Changing → re-translate.
- Result section: sans-serif (natural-language readability), preserve newlines, max-height 240px scrollable
- Loading state: spinner replaces result section
- History: collapsed by default behind "Recent (N)" disclosure; clicking a
  history row restores it to the active source/target/result

## Testing strategy

**Unit (vitest)**

- `providers/openai.ts` with mocked `fetch`:
  - happy path: returns `{ translated, detectedSrcLang }` from valid JSON
  - 401 → throws `TranslateError("auth", ...)`
  - 429 → throws `TranslateError("rate_limit", ...)`
  - 5xx → throws `TranslateError("server", ...)`
  - timeout (mock fetch hangs + advance timer past 30s) → throws `TranslateError("timeout", ...)`
  - non-JSON body in JSON mode → returns `{ translated: raw, detectedSrcLang: "unknown" }` + sets `supportsJsonMode = false`
  - subsequent call after fallback flag set → does NOT send `response_format`

- `panelStore.ts`:
  - `openWithSource(text)` sets state and dispatches translate
  - successful translate updates `result` + pushes to `history`
  - failed translate sets `error`, leaves history alone
  - second `openWithSource` while first in flight aborts first
  - changing `targetLang` triggers re-translate with new target

- `detectLang.ts`:
  - CJK input + default zh-CN → returns "en"
  - ASCII input + default zh-CN → returns "zh-CN"
  - CJK input + default "en" → returns "en" (config wins, no flip)

- `PluginHost.collectContextMenuItems`:
  - merges items from multiple plugins in registration order
  - one plugin throwing in `getMenuItems` is logged and skipped, others still contribute
  - empty selection still calls each plugin (contract: plugin decides whether to contribute)

**Component (vue-test-utils)**

- `TranslatePanel.vue` with mocked store:
  - renders source + translated when state has both
  - shows spinner when `loading`
  - shows error banner + Retry when `error`
  - dropdown change dispatches re-translate
  - close button hides panel (state retained)

- `TerminalView.vue` menu extension:
  - registers fake context-menu plugin returning one item
  - right-click triggers menu containing that item
  - clicking the item invokes the plugin's `onClick`

**Manual smoke test** (no e2e in v1)
- Configure with real OpenAI key, translate `"hello world"` → Chinese
- Configure with bogus key → see Auth failed banner
- Disable network → see network error
- Open settings without apiKey, trigger menu → see "Configure plugin" panel
- Drag panel to screen edge → see clamping

## Open questions resolved during brainstorm

| Question | Decision |
|---|---|
| Provider backend | Multi-provider abstraction; v1 ships OpenAI-compatible only, DeepL deferred |
| Where the HTTP request runs | Frontend webview `fetch` (no Go binding); API key stored plaintext in plugin config |
| Menu integration | Extend plugin slot system with headless `context-menu` slot |
| Result UI | Floating panel (Teleported), not in any slot |
| Language direction | Auto-detect (heuristic) with default target lang; user can switch in-panel |
| Scope | A (minimal): no global hotkey, no persistent history, no streaming, no DeepL, no Copy/Insert buttons |
| `getMenuItems` sync | Sync — pure computation, no async |
| Concurrency | AbortController, one request in flight per panel; new request aborts old |
| Timeout | 30s |
| History length | 5 in-memory (cleared on app close) |
