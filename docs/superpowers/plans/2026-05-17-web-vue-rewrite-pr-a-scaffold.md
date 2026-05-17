# Web Vue Rewrite — PR-A: Scaffold & Embed Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the existing vanilla web client under `web/legacy/`, scaffold a Vite + Vue 3 + TypeScript + Naive UI project at `web/`, embed the served output into the relay binary via `go:embed`, and switch `atterm-relay --web ""` to use the embedded FS — without changing what end users see.

**Architecture:** `internal/relay/web-dist/` becomes the canonical embedded FS, populated by `scripts/build-web.sh` which rsyncs `web/legacy/` for now and will overlay Vite output in later PRs. `newStaticHandler` switches from `string` (filesystem path) to `fs.FS` so it can serve either the embedded FS (prod) or a disk path (dev). End state: relay served from embed is byte-identical to the vanilla site users have today.

**Tech Stack:** Vite 5, Vue 3.5, TypeScript 5, Vitest, Naive UI 2 (declared but not yet imported), `go:embed`, Node 20.

**Reference spec:** `docs/superpowers/specs/2026-05-17-web-vue-typescript-rewrite-design.md`

---

## File Structure

| File | Created / Modified | Responsibility |
|---|---|---|
| `web/legacy/**` | Moved (git mv from `web/`) | Existing vanilla site, served unchanged during Phases A/B |
| `web/legacy/*.test.mjs` | Modified (rewritten file paths) | Old contract tests; paths fixed to `web/legacy/*` |
| `web/package.json` | Created | Vite + Vue + TS + Naive UI declared; `npm ci` reproducible |
| `web/package-lock.json` | Created (by `npm install`) | Locked dependency graph |
| `web/.npmrc` | Created | `ignore-scripts=true` (Sec-6) |
| `web/.gitignore` | Created | Ignores `dist/`, `node_modules/` |
| `web/tsconfig.json` | Created | Vue + browser TS config |
| `web/tsconfig.node.json` | Created | Build-script TS config (`vite.config.ts`, `vitest.config.ts`) |
| `web/vite.config.ts` | Created | Empty multi-entry config (entries added in PR-B onward) |
| `web/vitest.config.ts` | Created | jsdom env, includes `tests/unit/**` |
| `web/src/shared/api/client.ts` | Created | `safeNext()` (Sec-2) + skeleton `apiFetch` (full body in PR-B) |
| `web/src/shared/tokens.css` | Created | CSS variables copied verbatim from `web/legacy/style.css` `:root` block |
| `web/src/shared/theme/naive-theme.ts` | Created | `getNaiveOverrides()` reading CSS variables into Naive UI theme overrides |
| `web/tests/unit/shared/api/client.test.ts` | Created | TDD coverage for `safeNext()` |
| `scripts/build-web.sh` | Created | Sync `web/legacy/` → `internal/relay/web-dist/`; Vite step added in PR-B |
| `internal/relay/web-dist/.gitkeep` | Created | Anchor; real files synced by `scripts/build-web.sh` |
| `internal/relay/web-dist/**` | Created (generated) | Embedded static FS contents |
| `internal/relay/web_embed.go` | Created | `//go:embed all:web-dist` + `EmbeddedWebFS()` |
| `internal/relay/server.go` | Modified | `Config.WebDir string` → `Config.WebFS fs.FS`; `newStaticHandler` takes `fs.FS` |
| `internal/relay/static_handler_test.go` | Modified | Tests build `fs.FS` (memory or disk) instead of dir path |
| `cmd/atterm-relay/main.go` | Modified | `--web` flag default `""`; empty → embed, non-empty → `os.DirFS` |
| `.github/workflows/build.yml` | Modified | `web-tests` glob → `web/legacy/*.test.mjs`; new `web-vue-tests` job; build-and-diff step before Go jobs |
| `AGENTS.md` | Modified | Update "命令行 relay" snippet to drop `--web web` (now defaults to embed) |

---

## Pre-flight

- [ ] **Step 0.1: Confirm clean baseline**

Run: `git status --short`
Expected: only the in-flight modifications already known to the session (`internal/relay/auth_http.go`, `internal/relay/me_http_test.go`, `web/admin/index.html`, `web/settings.html`, plus `?? atterm-relay` build artifact). No other modified files.

If anything else is dirty, stop and reconcile before proceeding.

- [ ] **Step 0.2: Confirm Node 20 available**

Run: `node --version`
Expected: `v20.x`. If a different major, install Node 20 (e.g. via `nvm install 20 && nvm use 20`) — the CI pins this version (build.yml `NODE_VERSION: '20'`).

- [ ] **Step 0.3: Confirm rsync available**

Run: `rsync --version | head -n1`
Expected: prints a version string. On macOS this is preinstalled; on Linux `sudo apt-get install -y rsync`.

---

## Task 1: Relocate vanilla site under `web/legacy/`

**Files:**
- Move: every `web/*` (files and subdirectories, including `web/admin/` and `web/vendor/`) → `web/legacy/*`
- Modify: `web/legacy/auth-pages.test.mjs` (path strings)
- Modify: `web/legacy/no-inline-script.test.mjs` (path strings)
- Modify: any other `web/legacy/*.test.mjs` that hard-codes `web/`

- [ ] **Step 1.1: Create `web/legacy/` and move files**

Run:
```bash
mkdir web/legacy
git mv web/admin web/legacy/admin
git mv web/vendor web/legacy/vendor
for f in web/*.html web/*.js web/*.css web/*.mjs web/*.png web/*.svg web/*.webmanifest; do
  git mv "$f" "web/legacy/$(basename "$f")"
done
```

- [ ] **Step 1.2: Verify nothing remains at `web/` root**

Run: `ls web/`
Expected: only `legacy/` (and any pre-existing dotfiles that are still ignored).

- [ ] **Step 1.3: Fix absolute path references inside legacy tests**

For each `.test.mjs` file under `web/legacy/`, replace literal `web/` prefixes that point at the test's own siblings with `web/legacy/`. Concretely:

Edit `web/legacy/auth-pages.test.mjs`: replace every occurrence of the string `"web/login.html"`, `"web/login.js"`, `"web/signup.html"`, `"web/signup.js"` with `"web/legacy/login.html"`, `"web/legacy/login.js"`, `"web/legacy/signup.html"`, `"web/legacy/signup.js"`.

Edit `web/legacy/no-inline-script.test.mjs`: replace `path.join("web", name)` with `path.join("web", "legacy", name)`. Also adjust the directory iteration root from `"web"` to `"web/legacy"`.

Repeat this audit for any other `*.test.mjs` under `web/legacy/`. Quick search:

```bash
grep -rn '"web/\|web", ' web/legacy/*.test.mjs
```
Every hit must point at `web/legacy/` instead.

- [ ] **Step 1.4: Run the migrated legacy tests**

Run: `node --test web/legacy/*.test.mjs`
Expected: all tests pass (same set that passed before the move).

- [ ] **Step 1.5: Confirm relay still builds**

Run: `go build ./...`
Expected: builds clean. The relay still references `web/` as the default `--web` path, which is now empty — that's fine for compile-time. Runtime errors are handled by later tasks.

- [ ] **Step 1.6: Commit**

```bash
git add web/legacy AGENTS.md
git commit -m "$(cat <<'EOF'
refactor(web): relocate vanilla site under web/legacy/

PR-A step 1: move every web/* asset into web/legacy/ so the directory
root can host a Vite project in a follow-up step. Legacy contract tests
have their hard-coded "web/..." path strings rewritten to
"web/legacy/...". Relay behavior is unchanged; --web web still serves
nothing useful until --web flag default flips later in this PR.
EOF
)"
```

Note: `AGENTS.md` will be touched in Task 8; if you have not edited it yet, drop it from this commit and add it in the later commit.

---

## Task 2: Update CI to find legacy tests in their new home

**Files:**
- Modify: `.github/workflows/build.yml`

- [ ] **Step 2.1: Inspect current job**

Run: `grep -n 'node --test web' .github/workflows/build.yml`
Expected output: `node --test web/*.test.mjs`

- [ ] **Step 2.2: Update glob**

Edit `.github/workflows/build.yml`, replace:

```yaml
      - name: run web tests
        run: node --test web/*.test.mjs
```

with:

```yaml
      - name: run legacy web tests
        run: node --test web/legacy/*.test.mjs
```

- [ ] **Step 2.3: Lint workflow file locally**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/build.yml'))"`
Expected: no output (parse OK).

- [ ] **Step 2.4: Commit**

```bash
git add .github/workflows/build.yml
git commit -m "ci(web): point legacy node --test glob at web/legacy/"
```

---

## Task 3: Embed scaffold — `internal/relay/web-dist/`

**Files:**
- Create: `internal/relay/web-dist/.gitkeep`
- Create: `internal/relay/web_embed.go`

- [ ] **Step 3.1: Anchor the embed directory**

Run:
```bash
mkdir -p internal/relay/web-dist
touch internal/relay/web-dist/.gitkeep
```

- [ ] **Step 3.2: Write the embed file**

Create `internal/relay/web_embed.go`:

```go
package relay

import (
	"embed"
	"io/fs"
)

//go:embed all:web-dist
var embeddedWeb embed.FS

// EmbeddedWebFS returns the relay-bundled static web assets rooted at the
// repository's web/ output (built by scripts/build-web.sh).
//
// During PR-A the contents mirror web/legacy/ byte-for-byte; later PRs will
// replace individual entries with Vite output.
func EmbeddedWebFS() fs.FS {
	sub, err := fs.Sub(embeddedWeb, "web-dist")
	if err != nil {
		// fs.Sub only errors on a malformed name; "web-dist" is a constant.
		panic(err)
	}
	return sub
}
```

- [ ] **Step 3.3: Confirm it compiles**

Run: `go build ./internal/relay/...`
Expected: builds clean. (The `embed.FS` will be empty except for `.gitkeep` until Task 5 syncs content.)

- [ ] **Step 3.4: Commit**

```bash
git add internal/relay/web-dist/.gitkeep internal/relay/web_embed.go
git commit -m "feat(relay): add go:embed for web-dist static FS"
```

---

## Task 4: Switch `newStaticHandler` and `Config` to `fs.FS`

**Files:**
- Modify: `internal/relay/server.go`
- Modify: `internal/relay/static_handler_test.go`
- Modify: `cmd/atterm-relay/main.go`

This is the biggest behavioral change in PR-A. We refactor the static handler to accept any `fs.FS`. The flag-default flip happens in Task 6.

- [ ] **Step 4.1: Migrate the existing tests to `fs.FS`**

The existing `internal/relay/static_handler_test.go` has 4 tests (anonymous-root redirect, non-admin-redirect, admin-OK, admin-subresource ungated) that all use `fakeWebDir(t) string`. The minimal patch is to replace `fakeWebDir` with an `fs.FS`-returning helper and update each call site.

Replace lines 1–35 (imports + `fakeWebDir`) with:

```go
package relay

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/attson/atterm/internal/userstore"
)

// fakeWebFS returns an in-memory fs.FS that mimics what web-dist/
// holds: an index.html, a login.html, an admin/index.html, plus one
// admin subresource so the "ungated subresources" test has a target.
func fakeWebFS(t *testing.T) fs.FS {
	t.Helper()
	return fstest.MapFS{
		"index.html":       {Data: []byte("<html>home</html>")},
		"admin/index.html": {Data: []byte("<html>admin</html>")},
		"admin/admin.js":   {Data: []byte("/* admin */")},
		"login.html":       {Data: []byte("<html>login</html>")},
	}
}
```

Then in each of the four test functions (`TestStaticHandler_AdminGate_AnonymousRedirectsToLogin`, `..._NonAdminRedirectsToHome`, `..._AdminServesPage`, `TestStaticHandler_AdminSubresources_NotGated`) replace:

```go
	dir := fakeWebDir(t)
	...
	handler := newStaticHandler(resolver, dir)
```

with:

```go
	fsys := fakeWebFS(t)
	...
	handler := newStaticHandler(resolver, fsys)
```

The rest of every test body — including the userstore-backed resolver wiring — stays exactly as it is.

Run: `grep -n 'fakeWebDir' internal/relay/static_handler_test.go`
Expected: zero matches when you're done.

- [ ] **Step 4.2: Run tests to verify they fail (signature mismatch)**

Run: `go test ./internal/relay/ -run TestNewStaticHandler -v`
Expected: build fails because `newStaticHandler` still takes a `string`, not `fs.FS`. This is the red phase of TDD — failure here proves the tests are exercising the new signature.

- [ ] **Step 4.3: Update `newStaticHandler` signature**

Edit `internal/relay/server.go`. Find:

```go
func newStaticHandler(resolver *IdentityResolver, webDir string) http.Handler {
	fs := http.FileServer(http.Dir(webDir))
```

Replace with:

```go
func newStaticHandler(resolver *IdentityResolver, webFS fs.FS) http.Handler {
	fileSrv := http.FileServer(http.FS(webFS))
```

Then in the same function body, replace every reference to `fs.ServeHTTP` (the local variable that was named `fs`) with `fileSrv.ServeHTTP`. We rename it because `fs` would shadow the imported package.

Add `"io/fs"` to the import block (alongside the existing `"io"` etc.).

- [ ] **Step 4.4: Update `Config` and its consumer**

Edit `internal/relay/server.go`:

Replace:
```go
	// WebDir is the filesystem path to the static web client. Empty disables /.
	WebDir string
```

with:
```go
	// WebFS is the static web client filesystem. Empty disables /.
	// Callers typically pass relay.EmbeddedWebFS() (prod) or os.DirFS(path) (dev).
	WebFS fs.FS
```

Find the call site:
```go
	if cfg.WebDir != "" {
		s.mux.Handle("/", newStaticHandler(cfg.Resolver, cfg.WebDir))
	}
```

Replace with:
```go
	if cfg.WebFS != nil {
		s.mux.Handle("/", newStaticHandler(cfg.Resolver, cfg.WebFS))
	}
```

- [ ] **Step 4.5: Update `cmd/atterm-relay/main.go`**

Edit the file. Find:
```go
	webDir := flag.String("web", "web", "static web client directory (empty to disable)")
```

Replace with:
```go
	webDir := flag.String("web", "", "static web client directory; empty uses the embedded FS (production default)")
```

Find:
```go
	cfg := relay.Config{
		...
		WebDir:               *webDir,
```

Replace the `WebDir` line with logic that picks between embed and disk. Insert before the `cfg := relay.Config{...}` block:

```go
	var webFS fs.FS
	if *webDir == "" {
		webFS = relay.EmbeddedWebFS()
	} else {
		webFS = os.DirFS(*webDir)
	}
```

Then inside the `cfg` literal replace `WebDir: *webDir,` with `WebFS: webFS,`.

Add `"io/fs"` and (if not already) `"os"` to the import block. `flag` already imports `os`.

- [ ] **Step 4.6: Run all tests**

Run: `go test ./...`
Expected: all green. If `cmd/atterm-relay/main_test.go` references `WebDir`, fix the field name there too — find first:

```bash
grep -n 'WebDir' cmd/atterm-relay/*.go internal/relay/*.go
```

Should be zero hits after this task.

- [ ] **Step 4.7: Commit**

```bash
git add internal/relay/server.go internal/relay/static_handler_test.go cmd/atterm-relay/main.go
git commit -m "$(cat <<'EOF'
refactor(relay): newStaticHandler takes fs.FS, flag --web empty=embed

Switch Config.WebDir (string) to Config.WebFS (fs.FS) and refactor
newStaticHandler accordingly. cmd/atterm-relay's --web flag now defaults
to empty, which resolves to the embedded FS via relay.EmbeddedWebFS();
passing a non-empty path uses os.DirFS for dev iteration.

Existing static-handler tests are rewritten to construct an fstest.MapFS
in place of fakeWebDir.
EOF
)"
```

---

## Task 5: Sync `web/legacy/` into `internal/relay/web-dist/`

**Files:**
- Create: `scripts/build-web.sh`
- Create (generated): `internal/relay/web-dist/**`

- [ ] **Step 5.1: Write the build script**

Create `scripts/build-web.sh`:

```bash
#!/usr/bin/env bash
# Build the embedded web assets for atterm-relay.
#
# Output: internal/relay/web-dist/ is overwritten to match
#   - web/legacy/ (verbatim copy; the existing vanilla site)
#   - web/dist/   (Vite output; added in PR-B and later)
#
# CI relies on this script being deterministic. Do not introduce
# timestamps, environment-dependent paths, or unpinned tooling.

set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$here"

dist=internal/relay/web-dist

# Wipe the embed dir without removing the .gitkeep anchor.
mkdir -p "$dist"
find "$dist" -mindepth 1 -not -name '.gitkeep' -delete

# Layer 1: copy the legacy vanilla site so the relay keeps serving the
# UI users have today, byte-identical, while later PRs replace entries
# one at a time.
if [ -d web/legacy ]; then
  rsync -a --exclude='*.test.mjs' web/legacy/ "$dist/"
fi

# Layer 2 (PR-B onward): overlay Vite output on top of legacy. The
# build is skipped when node_modules is absent (caller forgot npm ci)
# or when web/dist is empty (PR-A placeholder). The _placeholder*
# artifact from PR-A is filtered out so it never lands in the embed.
if [ -f web/package.json ] && [ -d web/node_modules ]; then
  (cd web && npm run build)
  if [ -d web/dist ] && [ -n "$(ls -A web/dist 2>/dev/null)" ]; then
    rsync -a --exclude='_placeholder*' web/dist/ "$dist/"
  fi
fi

echo "web-dist synced from web/legacy/ ($(find "$dist" -type f | wc -l | tr -d ' ') files)"
```

- [ ] **Step 5.2: Make it executable**

Run: `chmod +x scripts/build-web.sh`

- [ ] **Step 5.3: Run it and inspect**

Run: `./scripts/build-web.sh`
Expected: prints a summary like `web-dist synced from web/legacy/ (~30 files)`. The exact count matches `find web/legacy -type f -not -name '*.test.mjs' | wc -l`.

Run: `find internal/relay/web-dist -type f | head -20`
Expected: lists HTML/JS/CSS/icon files, plus `vendor/xterm/...`. No `.test.mjs`.

- [ ] **Step 5.4: Stage the synced output**

Run: `git add internal/relay/web-dist/`
Expected: `git diff --cached --stat internal/relay/web-dist/` shows new files mirroring `web/legacy/` minus `*.test.mjs`.

- [ ] **Step 5.5: Smoke run the relay with the embedded FS**

Run (in a separate terminal):
```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
go run ./cmd/atterm-relay --addr 127.0.0.1:18080 --dev-insecure
```

Then from another shell:
```bash
curl -i http://127.0.0.1:18080/login.html
```
Expected: `HTTP/1.1 200 OK`, body begins with `<!doctype html>` and includes `id="login-form"` (or equivalent legacy markup; whichever the file actually has).

Stop the relay (Ctrl+C).

- [ ] **Step 5.6: Commit**

```bash
git add scripts/build-web.sh internal/relay/web-dist
git commit -m "$(cat <<'EOF'
build(web): scripts/build-web.sh syncs legacy/ to web-dist

scripts/build-web.sh becomes the single entry point for populating the
embedded web FS. In PR-A it rsyncs web/legacy/ verbatim (minus
*.test.mjs); the Vite overlay branch is a no-op until web/ declares a
real build script. The synced contents are committed under
internal/relay/web-dist/ so CI's diff gate works without needing Node
during go test.
EOF
)"
```

---

## Task 6: Vite + TypeScript scaffold at `web/`

**Files:**
- Create: `web/package.json`
- Create: `web/.npmrc`
- Create: `web/.gitignore`
- Create: `web/tsconfig.json`, `web/tsconfig.node.json`
- Create: `web/vite.config.ts`
- Create: `web/vitest.config.ts`
- Create (after `npm install`): `web/package-lock.json`

- [ ] **Step 6.1: Write `package.json`**

Create `web/package.json` (exact pinned versions; do not switch to `^` ranges):

```json
{
  "name": "atterm-web",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "build": "vue-tsc --noEmit && vite build",
    "dev": "vite",
    "preview": "vite preview",
    "test": "vitest run",
    "test:contract": "node --test tests/contract/*.mjs"
  },
  "dependencies": {
    "naive-ui": "2.40.4",
    "vfonts": "0.1.0",
    "vue": "3.5.13"
  },
  "devDependencies": {
    "@types/node": "20.17.10",
    "@vitejs/plugin-vue": "5.2.1",
    "@vue/test-utils": "2.4.6",
    "happy-dom": "15.11.7",
    "typescript": "5.6.3",
    "vite": "5.4.11",
    "vite-plugin-pwa": "0.21.1",
    "vitest": "2.1.8",
    "vue-tsc": "2.1.10"
  },
  "engines": {
    "node": ">=20.0.0 <21"
  }
}
```

Rationale for version pins is in spec Sec-6 (supply chain). Major version changes require a follow-up spec.

- [ ] **Step 6.2: Write `.npmrc`**

Create `web/.npmrc`:

```
ignore-scripts=true
fund=false
audit=false
```

`fund=false` and `audit=false` silence noisy `npm install` output without disabling the security checks that run in CI (`npm audit --omit=dev --audit-level=high` will still execute when invoked explicitly there).

- [ ] **Step 6.3: Write `.gitignore`**

Create `web/.gitignore`:

```
node_modules/
dist/
.vite/
*.tsbuildinfo
```

- [ ] **Step 6.4: Write `tsconfig.json`**

Create `web/tsconfig.json`:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "strict": true,
    "noImplicitAny": true,
    "noUncheckedIndexedAccess": true,
    "exactOptionalPropertyTypes": true,
    "skipLibCheck": true,
    "esModuleInterop": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "verbatimModuleSyntax": true,
    "useDefineForClassFields": true,
    "jsx": "preserve",
    "types": ["vite/client", "vitest/globals"],
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "baseUrl": ".",
    "paths": {
      "@shared/*": ["src/shared/*"]
    }
  },
  "include": ["src/**/*.ts", "src/**/*.vue", "tests/unit/**/*.ts"],
  "references": [{ "path": "./tsconfig.node.json" }]
}
```

- [ ] **Step 6.5: Write `tsconfig.node.json`**

Create `web/tsconfig.node.json`:

```json
{
  "compilerOptions": {
    "composite": true,
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "strict": true,
    "skipLibCheck": true,
    "esModuleInterop": true,
    "types": ["node"]
  },
  "include": ["vite.config.ts", "vitest.config.ts"]
}
```

- [ ] **Step 6.6: Write `vite.config.ts`**

Create `web/vite.config.ts`:

```ts
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// PR-A wires up Vite with a single placeholder entry; `npm run build`
// emits a harmless artifact that build-web.sh filters out before
// rsyncing into web-dist. Real entries arrive in PR-B (login/signup),
// PR-C (settings), PR-D (admin), PR-E (terminal home); the placeholder
// is deleted when PR-B lands.
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@shared': fileURLToPath(new URL('./src/shared', import.meta.url)),
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    assetsInlineLimit: 0,
    rollupOptions: {
      input: {
        _placeholder: fileURLToPath(new URL('./src/_placeholder.html', import.meta.url)),
      },
    },
  },
})
```

- [ ] **Step 6.7: Write `vitest.config.ts`**

Create `web/vitest.config.ts`:

```ts
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@shared': fileURLToPath(new URL('./src/shared', import.meta.url)),
    },
  },
  test: {
    environment: 'happy-dom',
    include: ['tests/unit/**/*.test.ts'],
    globals: false,
    css: true,
  },
})
```

- [ ] **Step 6.8: Install dependencies**

Run:
```bash
cd web && npm install --ignore-scripts
```
Expected: `package-lock.json` is created; no postinstall side effects (the `.npmrc` enforces this). The command may print `npm warn deprecated` for transitive packages — those are advisory, not errors.

- [ ] **Step 6.9: Create the placeholder entry referenced by vite.config.ts**

Create `web/src/_placeholder.html`:

```html
<!doctype html>
<!-- PR-A placeholder entry. Removed when PR-B lands real entries.
     build-web.sh filters _placeholder* out of the embed sync. -->
<html><head><meta charset="utf-8"><title>placeholder</title></head><body></body></html>
```

- [ ] **Step 6.10: Verify the build script works**

Run: `cd web && npm run build`
Expected: `vue-tsc --noEmit` exits 0, then `vite build` exits 0 and writes `web/dist/_placeholder.html` plus possibly an `assets/` directory. Inspect briefly:

```bash
ls web/dist
```
Expected: `_placeholder.html` (and maybe an empty `assets/`).

- [ ] **Step 6.11: Commit**

```bash
git add web/package.json web/package-lock.json web/.npmrc web/.gitignore web/tsconfig.json web/tsconfig.node.json web/vite.config.ts web/vitest.config.ts web/src/_placeholder.html

git commit -m "$(cat <<'EOF'
build(web): scaffold vite + typescript project root

PR-A step: declare pinned Vue 3 / Naive UI / Vite 5 / Vitest deps and
land tsconfig + vite.config + vitest.config. No entries yet — npm run
build is a deliberate no-op until PR-B starts replacing pages.
.npmrc disables install scripts (Sec-6); .gitignore excludes dist/.
EOF
)"
```

---

## Task 7: Shared modules — tokens, theme, `safeNext()` (TDD)

**Files:**
- Create: `web/src/shared/tokens.css`
- Create: `web/src/shared/theme/naive-theme.ts`
- Create: `web/src/shared/api/client.ts`
- Create: `web/tests/unit/shared/api/client.test.ts`

- [ ] **Step 7.1: Extract `:root` tokens from legacy CSS**

Read the `:root { ... }` block at the top of `web/legacy/style.css`. Copy it verbatim into a new file `web/src/shared/tokens.css`. Add a one-line comment at the top:

```css
/* Single source of truth for design tokens; mirror of legacy :root. */
```

Do not modify any variable values. The visual look must remain identical to today.

- [ ] **Step 7.2: Write the Naive UI theme override stub**

Create `web/src/shared/theme/naive-theme.ts`:

```ts
import type { GlobalThemeOverrides } from 'naive-ui'

// readVar returns the trimmed value of a CSS custom property, or the
// supplied fallback if the variable is not defined on the document root.
function readVar(name: string, fallback: string): string {
  if (typeof document === 'undefined') return fallback
  const raw = getComputedStyle(document.documentElement).getPropertyValue(name)
  return raw.trim() || fallback
}

// getNaiveOverrides resolves the design tokens from tokens.css and
// maps them onto Naive UI's GlobalThemeOverrides shape. Called at
// app mount time by each entry's App.vue (added in PR-B onward).
export function getNaiveOverrides(): GlobalThemeOverrides {
  const bg = readVar('--bg', '#0b1020')
  const fg = readVar('--fg', '#e2e8f0')
  const fgDim = readVar('--fg-dim', '#94a3b8')
  const panel = readVar('--panel', '#0f172a')
  const border = readVar('--border', '#1e293b')
  const accent = readVar('--accent', '#60a5fa')
  const bad = readVar('--bad', '#f87171')

  return {
    common: {
      bodyColor: bg,
      textColorBase: fg,
      textColor1: fg,
      textColor2: fg,
      textColor3: fgDim,
      primaryColor: accent,
      primaryColorHover: accent,
      primaryColorPressed: accent,
      borderColor: border,
      cardColor: panel,
      errorColor: bad,
    },
  }
}
```

- [ ] **Step 7.3: Write the failing `safeNext` test**

Create `web/tests/unit/shared/api/client.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { safeNext } from '@shared/api/client'

describe('safeNext', () => {
  it('returns / when input is null', () => {
    expect(safeNext(null)).toBe('/')
  })

  it('returns / when input is empty', () => {
    expect(safeNext('')).toBe('/')
  })

  it('accepts a same-origin path', () => {
    expect(safeNext('/settings.html')).toBe('/settings.html')
  })

  it('preserves query and hash on same-origin paths', () => {
    expect(safeNext('/admin/?tab=users#row-7')).toBe('/admin/?tab=users#row-7')
  })

  it('rejects protocol-relative URLs', () => {
    expect(safeNext('//evil.example')).toBe('/')
  })

  it('rejects backslash quirk', () => {
    expect(safeNext('/\\evil.example')).toBe('/')
  })

  it('rejects absolute URLs to other origins', () => {
    expect(safeNext('https://evil.example/login')).toBe('/')
  })

  it('rejects javascript: URLs', () => {
    expect(safeNext('javascript:alert(1)')).toBe('/')
  })

  it('rejects non-leading-slash paths (relative)', () => {
    expect(safeNext('settings.html')).toBe('/')
  })
})
```

- [ ] **Step 7.4: Run the test to verify it fails**

Run: `cd web && npm test`
Expected: vitest fails with `Cannot find module '@shared/api/client'` (or similar — `safeNext` does not exist yet).

- [ ] **Step 7.5: Implement `safeNext` and an apiFetch stub**

Create `web/src/shared/api/client.ts`:

```ts
// safeNext validates a post-login redirect target. The frontend never
// follows ?next= verbatim — every consumer routes through this guard.
// See spec Sec-2.
export function safeNext(raw: string | null): string {
  if (!raw) return '/'
  if (!raw.startsWith('/')) return '/'
  if (raw.startsWith('//')) return '/'
  if (raw.startsWith('/\\')) return '/'
  if (typeof location === 'undefined') return '/'
  try {
    const u = new URL(raw, location.origin)
    if (u.origin !== location.origin) return '/'
    return u.pathname + u.search + u.hash
  } catch {
    return '/'
  }
}

// apiFetch is the single network entry point for the browser client.
// PR-A only ships safeNext; the full implementation (401 redirect,
// CSRF header injection, ApiError) lands in PR-B alongside the first
// real consumer.
export async function apiFetch<T = unknown>(_path: string, _init?: RequestInit): Promise<{ data: T; status: number; headers: Headers }> {
  throw new Error('apiFetch not implemented yet; arrives in PR-B')
}
```

- [ ] **Step 7.6: Run the test to verify it passes**

Run: `cd web && npm test`
Expected: 9 passed, 0 failed.

- [ ] **Step 7.7: Run the type check**

Run: `cd web && npx vue-tsc --noEmit`
Expected: no errors.

- [ ] **Step 7.8: Commit**

```bash
git add web/src/shared web/tests/unit
git commit -m "$(cat <<'EOF'
feat(web): shared tokens.css + naive theme + safeNext

Lands the foundation modules that later PRs build on:
- tokens.css mirrors legacy :root variables; visual is unchanged
- getNaiveOverrides() resolves CSS variables into Naive UI theme overrides
- safeNext() guards post-login redirects against open-redirect abuse,
  with vitest coverage of //, /\\, https, javascript:, and relative
  forms (spec Sec-2)
apiFetch is stubbed; the real implementation arrives in PR-B with the
first consumer.
EOF
)"
```

---

## Task 8: CI — Vue tests + integrity diff gate

**Files:**
- Modify: `.github/workflows/build.yml`

- [ ] **Step 8.1: Open `build.yml` and locate the `web-tests` job**

Run: `grep -n 'name: web tests\|name: legacy web tests' .github/workflows/build.yml`

This should now point at the job edited in Task 2.

- [ ] **Step 8.2: Add a new `web-vue-tests` job after `web-tests`**

Insert the following YAML block immediately after the closing of the `web-tests` job and before `build-linux:` (use the same indentation as the existing job):

```yaml
  web-vue-tests:
    # Vue 3 / TypeScript / Vitest suite for the new web/ project.
    # Also rebuilds the embedded FS and fails on any uncommitted diff
    # under internal/relay/web-dist/ (spec Sec-7: build determinism).
    name: web vue tests
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: ${{ env.NODE_VERSION }}
          cache: npm
          cache-dependency-path: web/package-lock.json
      - name: install web deps
        working-directory: web
        run: npm ci --ignore-scripts
      - name: type check + build
        working-directory: web
        run: npm run build
      - name: vitest
        working-directory: web
        run: npm test
      - name: sync embed
        run: ./scripts/build-web.sh
      - name: verify embed has no drift
        run: git diff --exit-code -- internal/relay/web-dist
```

- [ ] **Step 8.3: Lint the workflow**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/build.yml'))"`
Expected: no output (parse OK).

- [ ] **Step 8.4: Locally simulate the integrity gate**

Run:
```bash
./scripts/build-web.sh
git diff --exit-code -- internal/relay/web-dist
```
Expected: exit 0 (no diff). If anything appears, debug determinism before continuing — either the script or the source has drifted.

- [ ] **Step 8.5: Commit**

```bash
git add .github/workflows/build.yml
git commit -m "$(cat <<'EOF'
ci(web): add web-vue-tests job + embed drift gate

Runs npm ci, type-check, build, and vitest in web/, then rebuilds
internal/relay/web-dist/ and fails the job if the committed embed
contents drift from what scripts/build-web.sh produces. The drift
check enforces build determinism (spec Sec-7) and prevents hand-edits
to web-dist/.
EOF
)"
```

---

## Task 9: Documentation refresh

**Files:**
- Modify: `AGENTS.md`

- [ ] **Step 9.1: Update the relay run snippet**

Open `AGENTS.md`. Find the "## 开发命令" section. The current snippet reads:

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
  go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --web web --dev-insecure
```

Replace with:

```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
  go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --dev-insecure
# --web flag omitted ⇒ uses the embedded FS at internal/relay/web-dist/.
# For frontend dev: cd web && npm run dev (5173, proxies API to 8080);
# or rebuild and run with --web web/dist after `npm run build`.
```

- [ ] **Step 9.2: Update the "何时改哪里" row for web safety headers / static assets**

Find the line:

```
| 改 web 安全头 / 静态资源 | `internal/relay/server.go` + `web/index.html` + `web/sw.js` + `web/*test.mjs` |
```

Replace with:

```
| 改 web 安全头 / 静态资源 | `internal/relay/server.go` + `web/src/...` (Vue 3 + Naive UI) + `web/legacy/` (during migration) + `web/tests/contract/*.mjs` |
```

- [ ] **Step 9.3: Add a one-liner pointing readers at the rewrite spec**

Insert near the top of `AGENTS.md`, right after the existing one-liner about `docs/spec/`:

```markdown
- Web 客户端正在从 vanilla JS 迁移到 Vue 3 + TypeScript + Naive UI；spec 见 `docs/superpowers/specs/2026-05-17-web-vue-typescript-rewrite-design.md`，本 PR 是 PR-A（脚手架）。
```

- [ ] **Step 9.4: Commit**

```bash
git add AGENTS.md
git commit -m "docs(agents): note web vue rewrite scaffold + flag default change"
```

---

## Task 10: Final smoke

- [ ] **Step 10.1: Run the full Go test suite**

Run: `go vet ./... && go test ./...`
Expected: all green.

- [ ] **Step 10.2: Run the web suites**

Run: `cd web && npm run build && npm test`
Expected: vue-tsc passes, vite emits an empty/placeholder dist, vitest reports 9 passed.

- [ ] **Step 10.3: Run the legacy contract suite**

Run: `node --test web/legacy/*.test.mjs`
Expected: same passing count as before this PR.

- [ ] **Step 10.4: Smoke the relay end-to-end**

Run (one terminal):
```bash
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
go run ./cmd/atterm-relay --addr 127.0.0.1:18080 --dev-insecure
```

Run (another terminal):
```bash
curl -sI http://127.0.0.1:18080/                 # expect 302 → /login.html
curl -sI http://127.0.0.1:18080/login.html       # expect 200
curl -sI http://127.0.0.1:18080/admin/           # expect 302 → /login.html
curl -s   http://127.0.0.1:18080/login.html | head -5  # legacy HTML
```

Each `curl -sI` line must produce the expected status. Stop the relay (Ctrl+C).

- [ ] **Step 10.5: Confirm embed drift gate is green**

Run: `./scripts/build-web.sh && git diff --exit-code -- internal/relay/web-dist`
Expected: exit 0.

- [ ] **Step 10.6: Confirm working tree is clean and ready for PR**

Run: `git status --short`
Expected: empty (or only the unrelated files that were dirty at start of PR-A).

- [ ] **Step 10.7: Push and open the PR**

```bash
branch=$(git rev-parse --abbrev-ref HEAD)
git push -u origin "$branch"
gh pr create --title "web: PR-A scaffold (vite + ts + embed)" --body "$(cat <<'EOF'
## Summary
- Move existing vanilla site to `web/legacy/`; relay still serves it byte-identical
- Add `go:embed all:web-dist` so the relay binary self-contains the static FS
- Refactor `newStaticHandler` to take `fs.FS`; `--web ""` (new default) uses embed, `--web <path>` uses disk
- Scaffold Vite + Vue 3 + TypeScript + Naive UI at `web/`; no entries yet
- `safeNext()` + vitest coverage (spec Sec-2); other shared modules stubbed for PR-B
- CI: new `web-vue-tests` job + embed drift gate (spec Sec-7)

## Test plan
- [x] `go vet ./... && go test ./...`
- [x] `cd web && npm run build && npm test`
- [x] `node --test web/legacy/*.test.mjs`
- [x] Manual smoke: `--web ""` boot, curl `/`, `/login.html`, `/admin/`
- [x] `./scripts/build-web.sh && git diff --exit-code -- internal/relay/web-dist`
EOF
)"
```

If the user prefers to review locally before pushing, skip this step and let them decide.

---

## Self-Review Notes

**Spec coverage check** (validated during plan writing):

- §Architecture / Directory layout → Task 6 (Vite root, tsconfig, src/shared)
- §Architecture / Routing (MPA) → deferred; no entries land in PR-A
- §Architecture / State → deferred; no Vue apps yet
- §Architecture / Auth & API → Task 7 (`safeNext` only; full `apiFetch` in PR-B)
- §Architecture / WebSocket / proto → deferred to PR-E
- §Architecture / UI theme → Task 7 (`tokens.css`, `getNaiveOverrides`)
- §Architecture / PWA → deferred; `vite-plugin-pwa` declared in `package.json` but not configured
- §Architecture / Build & deploy → Tasks 3–5 (embed, build script, flag refactor)
- §Architecture / Dev workflow → Task 9 (AGENTS.md update)
- §Testing / Contract tests → Task 1 (paths fixed) + Task 2 (CI glob fixed)
- §Testing / Unit tests → Task 7 (`client.test.ts`, first vitest case)
- §Phasing / Phase A → covered by this entire plan
- §Invariants / `internal/relay/web-dist/` is generated → Task 8 drift gate
- §Invariants / no `v-html` → deferred (no Vue files yet)
- §Security / Sec-1 (CSP) → already in place; spec updated to reflect this
- §Security / Sec-2 (safeNext) → Task 7
- §Security / Sec-3 (CSWSH) → server-side; no work in PR-A
- §Security / Sec-4 (CSRF double cookie) → server-side; consumer arrives in PR-B
- §Security / Sec-5 (SW precache) → deferred to PR-B (first plugin config)
- §Security / Sec-6 (supply chain) → Task 6 (`.npmrc`, pinned versions, lockfile)
- §Security / Sec-7 (build determinism) → Task 8 drift gate
- §Security / Sec-8 (paste size) → deferred to PR-E (image paste consumer)
