# File Explorer — multi-type highlight + binary previews

Status: design
Author: brainstorming session 2026-06-07
Plugin: `desktop/frontend/src/plugins/fileExplorer`

## Goal

Extend the desktop File Explorer plugin so it:

1. Highlights the languages users actually carry around in a terminal project
   (Go / Rust / C-family / Java / PHP / SQL / XML / YAML / Vue / shell / TOML /
   Dockerfile / Ruby / Lua / properties / diff / Swift, plus C-like fallback).
2. Previews common binary formats inline — images, audio, video, PDF — instead
   of only showing a "binary" banner.

Today the plugin handles 6 languages (js/ts/json/md/css/html/py) and renders
every binary file as a static "binary" banner.

## Non-goals

- Editing. Read-only stays read-only.
- Thumbnail caching, video frame extraction, custom PDF rendering.
- Mobile / web reach. The plugin already requires `caps.pluginHost`, which is
  desktop-only.
- A general media browser surface. The dispatcher fires only from the existing
  tab open flow.

## Architecture

### 1. Dispatcher (frontend)

`FileEditor.vue` becomes a thin router. Today it owns the CodeMirror state;
after this change it inspects the path and delegates to one of four sibling
components.

```
plugins/fileExplorer/
  FileEditor.vue        # dispatcher (kindForPath → child)
  previewKind.ts        # pure: path → 'code' | 'image' | 'svg' | 'video' |
                        #               'audio' | 'pdf' | 'binary-unknown'
  CodeViewer.vue        # ex-FileEditor body (CM6 + readFile + reload-on-change)
  ImagePreview.vue      # <img src=pluginfs://…> centered, fit / 1:1 toggle
  MediaPreview.vue      # <audio|video controls> via pluginfs://
  PdfPreview.vue        # <embed type="application/pdf" src=pluginfs://…>
  languageMap.ts        # extended; dynamic-imports lang packs + legacy modes
  highlight.ts          # unchanged
  theme.css             # unchanged + small additions for preview chrome
```

`previewKind.ts` resolves precedence:

1. Extension lookup against a small static map (image / svg / video / audio /
   pdf).
2. If unmatched, language-map and basename-map decide `code`. Anything routed
   to `code` always opens `CodeViewer`, which itself falls back to the existing
   "binary" banner when `fileMeta.isBinary` is true and no text decoding makes
   sense.
3. Otherwise return `binary-unknown`. The dispatcher shows the existing banner
   plus a new "Open in System" action (wired through `OpenExternal`, already
   bound).

### 2. Asset handler (backend)

PDF, audio, and video frequently exceed the 5 MB `ReadFile` server cap and
need range support for seek. Routing them through the existing JSON `[]byte`
path is wrong: it would either truncate or balloon memory.

Add `desktop/plugin_fs_server.go` next to `plugin_fs.go`:

```go
// ServeHTTP serves bytes for paths the File Explorer has already validated
// through ListDir / FileMeta. Method GET/HEAD only. URL form:
//   /pluginfs/<base64.URLEncoding(path)>
// Decoding failures → 400. resolve() failures → 403/404 (same denylist as
// ReadFile). Then http.ServeFile handles Content-Type, ETag, and Range.
//
// SECURITY: this lives in the same red-line #11 boundary as PluginFS. The CI
// isolation check covers the file via its package membership; no relay-side
// import path can reach it.
func (p *PluginFS) ServeHTTP(w http.ResponseWriter, r *http.Request) { … }
```

Mounted via Wails:

```go
AssetServer: &assetserver.Options{
  Assets:  assets,
  Handler: app.pluginFS,
},
```

Wails routes unmatched asset requests through `Handler`. The handler routes
only `/pluginfs/...` and 404s anything else, so the embedded SPA continues to
take precedence for normal asset paths.

Response headers:

- `Cache-Control: no-store` — files on disk can mutate.
- `Content-Type` — derived by `http.ServeContent` from extension; we override
  to `application/pdf` for `.pdf` (Go stdlib already does, but pin it).
- No `Set-Cookie`, no `Access-Control-*` — same-origin webview only.

### 3. Highlight extension (frontend)

`languageMap.ts` gains:

- Official `@codemirror/lang-*`: `go`, `rust`, `cpp`, `java`, `php`, `sql`,
  `xml`, `yaml`, `vue`, `sass`.
- Legacy modes via `@codemirror/legacy-modes/mode/*` wrapped with
  `streamLanguage.define()`: `shell`, `toml`, `dockerfile`, `ruby`, `lua`,
  `properties`, `diff`, `swift`, `clike`.

Extension table (lowercase ext → language):

| Ext / basename                              | Pack |
|---------------------------------------------|------|
| `go`                                        | lang-go |
| `rs`                                        | lang-rust |
| `c, cc, cpp, cxx, h, hpp, hh, m, mm`        | lang-cpp |
| `java`                                      | lang-java |
| `kt, kts, scala`                            | legacy clike |
| `php`                                       | lang-php |
| `sql`                                       | lang-sql |
| `xml, xsd, xsl, plist`                      | lang-xml |
| `svg`                                       | lang-xml (text view; preview toggles to render) |
| `yml, yaml`                                 | lang-yaml |
| `vue`                                       | lang-vue |
| `sass`                                      | lang-sass |
| `sh, bash, zsh, fish, ksh`                  | legacy shell |
| `toml`                                      | legacy toml |
| `rb`                                        | legacy ruby |
| `lua`                                       | legacy lua |
| `ini, properties, conf`                     | legacy properties |
| `diff, patch`                               | legacy diff |
| `swift`                                     | legacy swift |
| Basename `Dockerfile`                       | legacy dockerfile |
| Basename `Gemfile`, `Rakefile`              | legacy ruby |
| Basename `Makefile`, `GNUmakefile`          | legacy clike (close enough; CM6 has no makefile mode) |

Order of resolution in `languageForPath`:

1. Exact basename match (no extension files).
2. Lowercase extension match.
3. Existing 6 builtins (`js/ts/json/md/css/html/py`) come first in the switch
   so they keep their current behavior verbatim.

Every language module is a `await import('…')` inside its `case` so Vite still
code-splits each pack into its own chunk.

### 4. Image / SVG preview

`ImagePreview.vue`:

- `src` is `/pluginfs/<encoded>` — handler streams bytes; no base64 in JSON.
- Layout: viewport-filling centered container; `<img>` uses `object-fit:
  contain` (fit mode) by default. Clicking the image toggles to "native" mode
  — `width: auto; height: auto; image-rendering: pixelated` — and the host
  container becomes a scroll viewport so the user can pan large images and
  inspect pixel art / icons at 1:1.
- Decode error: `<img @error>` → swap to the existing "binary" banner +
  "Open in System" button.

SVG dual mode:

- `previewKind('foo.svg')` returns `'svg'`.
- Dispatcher renders both `CodeViewer` (default, text + highlight) and a
  switcher in the tab header. Toggle re-mounts to `ImagePreview`.
- This is the only kind that has two views; keep the toggle state in
  `tabsModel` so flipping tabs preserves it.

### 5. Audio / video preview

`MediaPreview.vue`:

- `<video controls>` or `<audio controls>` with `preload="metadata"` so the
  webview only requests the byte range it needs.
- `src` is `/pluginfs/<encoded>`. The handler's `http.ServeContent` already
  emits `Accept-Ranges: bytes` and 206 responses, so seek works.
- `error` event → banner + "Open in System".
- Volume / playback rate are intentionally not persisted (YAGNI).

### 6. PDF preview

`PdfPreview.vue`:

- `<object data="/pluginfs/<encoded>" type="application/pdf">…fallback…</object>`
  filling the pane. The fallback child is the standard banner +
  "Open in System" button — the browser shows it automatically when no PDF
  handler is available, so no JS timer / readiness probe is required.
- On macOS the webview embeds the system PDF plugin; same on Windows; the
  chromium-based webview on Linux usually does too.
- Acceptance: macOS CI fixture renders inline; Linux fallback path is asserted
  in a unit test by mocking `HTMLObjectElement` to refuse the MIME type.

### 7. Error handling matrix

| Failure | Surface |
|---|---|
| Path outside allowRoots / denylisted | handler 403; component shows error banner |
| Path missing (race after move) | handler 404; component shows error banner |
| URL base64 decode fail | handler 400; component shows error banner |
| Image decode fail | `<img @error>` → binary banner + Open in System |
| Media playback fail | `<video|audio @error>` → banner + Open in System |
| PDF render fail | `<object>` fallback child → banner + Open in System |
| `fs.readFile` >2 MB (CodeViewer) | existing `tooLarge` banner |
| `fileMeta.isBinary` for a path routed to `code` | existing `binary` banner |

A new `BinaryBanner.vue` snippet (banner text + "Open in System" button calling
`OpenExternal`) is shared by every preview component for its failure path and
by the dispatcher for the `binary-unknown` case. The existing `.banner` styles
move from `FileEditor.vue` into `CodeViewer.vue` and `BinaryBanner.vue`
unchanged.

### 8. Tests

New / changed test files:

- `previewKind.test.ts` (new) — table-driven over each ext / basename.
- `languageMap.test.ts` (extend) — assert non-null Extension for each new
  ext / basename, keep the existing null assertion for `.zzz` / `LICENSE`.
- `FileEditor.test.ts` (rewrite) — assert the dispatcher mounts the right
  child component per `previewKind` outcome. CodeViewer's own behavior is
  asserted in a new `CodeViewer.test.ts` that copies the existing assertions.
- `plugin_fs_server_test.go` (new) — table over: valid path → 200 + content
  type, denied path → 403, missing path → 404, bad base64 → 400, Range header
  → 206, POST → 405.

E2E: extend the manual `verify` smoke check to open one fixture per kind
(`fixture.png`, `fixture.mp4`, `fixture.pdf`, `fixture.svg`) and confirm each
viewport. Fixtures live under
`desktop/frontend/src/plugins/fileExplorer/__fixtures__/` and are <100 KB each.

## Security

- All file reads still go through `PluginFS.resolve()` → same allowRoots,
  same denylist (`.ssh`, `.gnupg`, `.aws`, `.env*`).
- The new handler is on `*PluginFS` so the existing isolation CI check
  (`.github/scripts/check-plugin-fs-isolation.sh`) keeps it inside the
  desktop boundary; no relay package can import it.
- `/pluginfs/...` is rejected for methods other than GET/HEAD.
- No CORS headers — same-origin webview only. The handler is unreachable
  externally; the asset server binds to localhost on a random port.
- Base64 URL encoding means the URL itself never carries raw `/` characters
  that could be confused for routing.

## YAGNI

- No directory icon set, no per-extension file icons in the tree.
- No video poster generation.
- No PDF page navigation overlay (the system plugin provides its own).
- No drag-to-open from the tree into a media player.
- No "open in editor" gesture (still a terminal app).
- No streaming of `code` files larger than the 2 MB frontend cap — the cap
  is fine for editor-style review.

## Open questions

None. The choices that were on the table (highlight scope, preview types,
delivery channel) are locked from the brainstorming session.

## Implementation order (preview)

1. Backend: `plugin_fs_server.go` + `main.go` wiring + tests.
2. `previewKind.ts` + tests.
3. Dispatcher refactor: extract `CodeViewer.vue` from `FileEditor.vue`, rewrite
   `FileEditor.vue` as router, port existing tests.
4. `ImagePreview.vue` + SVG dual-mode + tabsModel field for view-mode.
5. `MediaPreview.vue`.
6. `PdfPreview.vue`.
7. `languageMap.ts` expansion + tests.
8. Manual `verify` pass over fixtures.

The plan file produced by `writing-plans` will turn each of these into an
isolated commit / sub-PR.
