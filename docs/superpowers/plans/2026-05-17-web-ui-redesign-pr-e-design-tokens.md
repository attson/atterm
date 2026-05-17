# Web UI Redesign — PR E: Design tokens audit + login/signup polish

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Establish a complete design-token system, replace raw `#rgb`/`rgba()` literals in `web/style.css` and inline page styles with token references, and apply final polish to `/login.html` + `/signup.html`.

**Architecture:** Token additions only — the current palette (defined in `style.css`'s `:root`) stays. Four new tokens are added (`--surface-2`, `--border-mute`, `--accent-bg`, `--warn`) plus a spacing scale (`--space-1`…`--space-8`). Each raw color found in CSS or in inline `<style>` blocks of HTML pages is mapped to either an existing or a new token. A regression test (`web/no-raw-colors.test.mjs`) blocks future hex/rgba literals from sneaking back into `style.css` and the inline page styles.

**Tech Stack:** Pure CSS edits + Node-test additions. No new deps; no Go changes.

**Spec:** `docs/superpowers/specs/2026-05-17-web-ui-redesign-design.md` (section "Visual Design Tokens").

**Scope intentionally NOT included:** Flipping the palette to the spec's literal GitHub-Dark hex values (`#0d1117` / `#161b22` / etc.) — the current site has been live for the entire PR A–D rollout under the existing palette; surprising users with a re-colored UI in a "polish" PR isn't in scope. Future PR can do a separate visual refresh.

---

## File Map

**Modify:**
- `web/style.css` — add tokens; replace raw colors with token references
- `web/settings.html` — replace raw colors in inline `<style>` with tokens
- `web/admin/index.html` — same
- `web/login.html` — apply polish (no-op for now if file is unchanged; the polish goes through `auth-page` rules in style.css)
- `web/signup.html` — same
- `web/sw.js` — hash bump (style.css and the HTML pages change)
- (Possibly) `web/style.css` — `.auth-page` / `.auth-card` polish rules (padding / border / focus state / error state)

**Create:**
- `web/no-raw-colors.test.mjs` — regression test blocking raw hex/rgba in style.css (with a narrow allow-list for the gradient backgrounds that intentionally use raw values, e.g. body radial-gradient)

**Delete:** none.

---

## Task 1 — Add the new design tokens

**Files:**
- Modify: `web/style.css`

- [ ] **Step 1: Edit `:root`**

Find the `:root { ... }` block at the top of `web/style.css` and add the new tokens. The existing block is roughly:

```css
:root {
  --bg: #05070d;
  --panel: #0f172a;
  --panel-2: #111827;
  --border: #263244;
  --fg: #e5eefb;
  --fg-dim: #93a4ba;
  --accent: #38bdf8;
  --accent-2: #facc15;
  --good: #4ade80;
  --bad: #fb7185;
  --topbar-h: 58px;
  --shortcut-h: 58px;
  --app-height: 100dvh;
}
```

Extend it to:

```css
:root {
  /* surfaces (existing names kept; aliases for spec-aligned names) */
  --bg: #05070d;
  --panel: #0f172a;
  --panel-2: #111827;
  --surface: var(--panel);
  --surface-2: var(--panel-2);

  /* borders */
  --border: #263244;
  --border-mute: rgba(148, 163, 184, 0.18);

  /* text */
  --fg: #e5eefb;
  --fg-dim: #93a4ba;
  --fg-mute: var(--fg-dim);

  /* accent + status */
  --accent: #38bdf8;
  --accent-2: #facc15;
  --accent-bg: rgba(56, 189, 248, 0.12);
  --good: #4ade80;
  --bad: #fb7185;
  --warn: #facc15;

  /* spacing scale */
  --space-1: 4px;
  --space-2: 8px;
  --space-3: 12px;
  --space-4: 16px;
  --space-6: 24px;
  --space-8: 32px;

  /* layout dims (existing) */
  --topbar-h: 58px;
  --shortcut-h: 58px;
  --app-height: 100dvh;
}
```

Notes:
- `--surface` / `--surface-2` are aliases of the existing `--panel` / `--panel-2`. Keeping the old names avoids touching every existing reference; new code can use either.
- `--fg-mute` aliases `--fg-dim` for the same reason.
- `--border-mute` is a new color (the rgba value already appears 4 times in the file as a literal — Task 2 replaces those uses).
- `--accent-bg` already appears as a literal `rgba(88, 166, 255, 0.12)` in `style.css:87` — note that's the GitHub-Dark accent value, NOT this site's current accent (#38bdf8). The new token uses the current accent's rgba expansion `rgba(56, 189, 248, 0.12)` so the active-tab pill stays visually consistent with the rest of the site.
- `--warn` reuses the existing `--accent-2` value (matches the install-hint and admin warning treatments).

- [ ] **Step 2: Run tests, expect no failures yet**

```bash
node --test web/*.test.mjs web/admin/*.test.mjs 2>&1 | tail -8
```

Only `sw-cache-bump.test.mjs` should be at risk (style.css content changed) — let it fail; Task 6 fixes.

- [ ] **Step 3: Commit**

```bash
git add web/style.css
git commit -m "web(css): add design tokens (surface / border-mute / accent-bg / warn / spacing scale)"
```

---

## Task 2 — Replace raw colors in `web/style.css` with tokens

**Files:**
- Modify: `web/style.css`

- [ ] **Step 1: Survey raw colors**

```bash
grep -nE '#[0-9a-fA-F]{6}|#[0-9a-fA-F]{3}\b|rgba?\(' web/style.css | grep -v "^.*:[0-9]\+:[ ]*--" | head -40
```

(The `grep -v ":-"` filters out the `--bg: #...` token-declaration lines — we want only USAGE sites.)

You'll see ~30+ matches. Group by intent:

**Topbar / page-bar translucent backgrounds** (lines ~53, 93):
```css
background: rgba(5, 7, 13, 0.86);
```
→ no exact token (would need `--bg` with alpha; CSS can't do that directly). Keep as raw OR add `--topbar-bg: rgba(5, 7, 13, 0.86)` token. **Recommend: add a `--topbar-bg` token.**

**Topbar/page-bar bottom border** (lines ~54, 94):
```css
border-bottom: 1px solid rgba(148, 163, 184, 0.18);
```
→ `border-bottom: 1px solid var(--border-mute);` ✓

**Topnav hover background** (line 86):
```css
.topnav a:hover { ... background: rgba(148, 163, 184, 0.10); }
```
→ ad-hoc; add `--surface-hover: rgba(148, 163, 184, 0.10)` token OR accept the raw value with a comment. **Recommend: add `--surface-hover`.**

**Topnav active background** (line 87):
```css
.topnav a.active { ... background: rgba(88, 166, 255, 0.12); }
```
→ This is the GitHub-Dark blue! Doesn't match `--accent: #38bdf8`. Either:
  - Update to `var(--accent-bg)` so it matches the current accent (visual change: pill becomes cyan instead of blue tint)
  - Leave alone (visual fidelity to existing design)
  
  **Recommend: switch to `var(--accent-bg)` — the inconsistency was a bug.**

**Ghost button / refresh / token-save / paste-actions** (lines ~101, 112):
```css
background: rgba(15, 23, 42, 0.9);
background: rgba(15, 23, 42, 0.96);
```
→ Both are alpha variants of `--panel`. Add `--surface-button: rgba(15, 23, 42, 0.9)` token; the .96 variant uses the same token.

**Body radial gradient** (line 21):
```css
background: radial-gradient(circle at top left, #172554 0, #05070d 42%);
```
→ Background gradient with two literal stops — `#05070d` IS `--bg`. The other stop `#172554` is an accent dark blue. Replace `#05070d` with `var(--bg)`. Leave `#172554` as a literal (it's a one-off accent that's not worth a token).

**Install-hint cluster** (lines ~145-152):
Several rgba()s for the install-hint banner. They're a one-off visual treatment. Leave them as-is — adding tokens for one-shot styles inflates the token system without payoff.

- [ ] **Step 2: Apply the replacements**

Edit `web/style.css`:

In `:root`, ADD the new tokens you decided to introduce (extending Task 1's set):

```css
  --surface-hover: rgba(148, 163, 184, 0.10);
  --surface-button: rgba(15, 23, 42, 0.9);
  --topbar-bg: rgba(5, 7, 13, 0.86);
```

Then replace each USAGE site:

```css
/* line 21 — body background */
background: radial-gradient(circle at top left, #172554 0, var(--bg) 42%);

/* line 53 — #topbar */
background: var(--topbar-bg);
border-bottom: 1px solid var(--border-mute);

/* line 86 — .topnav a:hover */
.topnav a:hover { color: var(--fg); background: var(--surface-hover); }

/* line 87 — .topnav a.active */
.topnav a.active { color: var(--accent); background: var(--accent-bg); }

/* line 93 — #page-bar */
background: var(--topbar-bg);
border-bottom: 1px solid var(--border-mute);

/* line 101 — .ghost-btn (rgba .9) */
background: var(--surface-button);

/* line 112 — same family (rgba .96) — leave or also tokenize */
background: var(--surface-button); /* visual diff: .9 vs .96; if the .96 alpha matters, keep raw */
```

For the .9 vs .96 difference, if the visual is intentional, keep it raw with a comment. If indistinguishable in practice, fold to one token.

**Leave alone** (raw, with intent):
- The `#172554` gradient stop in body background — one-off accent dark.
- The install-hint inline rgba palette (lines ~132-152) — one-off banner; tokenizing here costs more than it saves.

- [ ] **Step 3: Run tests + commit**

```bash
node --test web/*.test.mjs web/admin/*.test.mjs 2>&1 | tail -8
```

Only sw-cache-bump should be failing.

```bash
git add web/style.css
git commit -m "web(css): replace raw colors with tokens (border-mute / accent-bg / surface-button etc.)"
```

---

## Task 3 — Replace raw colors in `web/settings.html` inline `<style>`

**Files:**
- Modify: `web/settings.html`

- [ ] **Step 1: Survey**

```bash
grep -nE '#[0-9a-fA-F]{6}|rgba?\(' web/settings.html | head -20
```

Mapping guide:
- `background: radial-gradient(circle at top left, #172554 0, var(--bg) 42%);` (line 11) — leave; consistent with body background.
- `box-shadow: 0 18px 60px rgba(0, 0, 0, 0.36);` (line 30) — add `--card-shadow: 0 18px 60px rgba(0, 0, 0, 0.36)` token OR leave raw. **Leave raw** — single-use shadow.
- `color: #05070d;` (line 61) — that's `--bg`. Replace with `var(--bg)`.
- `background: rgba(251, 113, 133, 0.12);` (line 81) — alpha of `--bad`. Add `--bad-bg: rgba(251, 113, 133, 0.12)` token (or leave raw if only one use).
- `background: rgba(2, 6, 23, 0.48);` (line 106) — `--surface` darker. Leave raw; one-off.
- `background: rgba(74, 222, 128, 0.08);` (line 116) and `rgba(74, 222, 128, 0.3)` (line 117) — alpha of `--good`. Add `--good-bg` and `--good-border` tokens? Or leave. **Leave raw**.

Apply only the safe replacements:
- `color: #05070d;` → `color: var(--bg);`
- `background: rgba(251, 113, 133, 0.12);` → add `--bad-bg` to style.css `:root`, then `background: var(--bad-bg);`. (Touches both files; do it.)

- [ ] **Step 2: Add the new token to style.css**

```css
/* in :root */
--bad-bg: rgba(251, 113, 133, 0.12);
```

- [ ] **Step 3: Apply replacements in settings.html**

Make the two specific edits described above.

- [ ] **Step 4: Commit**

```bash
git add web/settings.html web/style.css
git commit -m "web(settings): use --bg + --bad-bg tokens in inline styles"
```

---

## Task 4 — Replace raw colors in `web/admin/index.html` inline `<style>`

**Files:**
- Modify: `web/admin/index.html`

- [ ] **Step 1: Survey**

```bash
grep -nE '#[0-9a-fA-F]{6}|rgba?\(' web/admin/index.html | head -20
```

Mapping:
- `background: radial-gradient(circle at top left, #1e1b4b 0, var(--bg) 42%);` — one-off violet-tinted gradient for admin page; leave raw.
- `box-shadow: 0 18px 60px rgba(0, 0, 0, 0.36);` — leave raw (same as settings).
- `background: rgba(217, 153, 33, 0.12);` — alpha of `--warn` (#facc15-ish in current palette). Replace with `var(--warn-bg)` after adding `--warn-bg: rgba(217, 153, 33, 0.12)` to `:root`.
- `border: 1px solid var(--warn, #d29922);` — already uses `var(--warn)` with a fallback. Drop the `#d29922` fallback now that `--warn` is defined in `:root` (Task 1).
- `background: rgba(0, 0, 0, 0.3);` — secret display background. Add `--code-bg: rgba(0, 0, 0, 0.3)` token or leave raw. **Leave raw** — one-off.

- [ ] **Step 2: Add `--warn-bg` to style.css**

```css
--warn-bg: rgba(217, 153, 33, 0.12);
```

- [ ] **Step 3: Edits in admin/index.html**

- `background: rgba(217, 153, 33, 0.12);` → `background: var(--warn-bg);`
- `border: 1px solid var(--warn, #d29922);` → `border: 1px solid var(--warn);`

- [ ] **Step 4: Commit**

```bash
git add web/admin/index.html web/style.css
git commit -m "web(admin): use --warn-bg token; drop --warn fallback (now defined globally)"
```

---

## Task 5 — Polish `web/login.html` + `web/signup.html`

The HTML files themselves are minimal (~17 lines each). The polish lives in `style.css`'s `.auth-page` / `.auth-card` / `.auth-card input` / `.alt` rules.

**Files:**
- Modify: `web/style.css` (auth-page section)

- [ ] **Step 1: Locate the auth rules**

```bash
grep -n "auth-page\|auth-card\|\.alt\b" web/style.css | head -10
```

Find the cluster of `.auth-*` rules — likely a contiguous block.

- [ ] **Step 2: Polish (in `style.css`)**

Reasonable tightening:
- `.auth-card` — increase padding from likely `1.5rem 2rem` to `var(--space-6) var(--space-8)`; subtle border using `var(--border-mute)` instead of `var(--border)` for a softer card edge.
- `.auth-card input:focus` — add an `outline: 2px solid var(--accent-bg)` so focus is visible without being garish.
- `.auth-card #error` — when not hidden, show with `color: var(--bad); background: var(--bad-bg); padding: var(--space-2) var(--space-3); border-radius: 6px;`.
- `.auth-card button[type="submit"]` — full width with `padding: var(--space-3) var(--space-4)`; hover state.
- `.alt` — already styled; consider `color: var(--fg-mute)`.

The exact existing rules may differ — read what's there first and apply incremental polish using tokens. Don't rewrite everything.

Concrete diff suggestion (adapt to what's actually in the file):

```css
.auth-card {
  /* existing display/gap/etc kept */
  padding: var(--space-6) var(--space-8);
  border: 1px solid var(--border-mute);
}
.auth-card input:focus {
  outline: 2px solid var(--accent-bg);
  outline-offset: 1px;
}
.auth-card #error:not([hidden]) {
  color: var(--bad);
  background: var(--bad-bg);
  padding: var(--space-2) var(--space-3);
  border-radius: 6px;
  margin: 0;
}
.auth-card button[type="submit"] {
  padding: var(--space-3) var(--space-4);
  margin-top: var(--space-2);
}
.alt {
  color: var(--fg-mute);
  font-size: 13px;
}
```

- [ ] **Step 3: Visual smoke**

```bash
go build -o /tmp/atterm-relay-pre ./cmd/atterm-relay
/tmp/atterm-relay-pre --addr 127.0.0.1:18141 --web web --dev-insecure > /tmp/relay-pre.log 2>&1 &
PID=$!
sleep 1
echo "open http://127.0.0.1:18141/login.html in a browser"
# Leave it running; user opens the URL and visually inspects
# Press Ctrl+C when done; for the agent: just confirm the server is up:
curl -sI http://127.0.0.1:18141/login.html | head -3
kill $PID; wait $PID 2>/dev/null
```

(Real visual smoke is done by the human reviewer; the agent just confirms the file serves.)

- [ ] **Step 4: Commit**

```bash
git add web/style.css
git commit -m "web(auth): polish login/signup cards — focus outline, error styling, spacing tokens"
```

---

## Task 6 — Regression test: no raw colors in token-driven files

**Files:**
- Create: `web/no-raw-colors.test.mjs`

- [ ] **Step 1: Write the test**

```js
// PR E established a design-token system. Going forward, raw #rgb /
// rgba() literals in style.css and the inline <style> blocks of the
// authenticated pages are a smell — they break the "one source of
// truth" invariant the tokens give us. Allow a narrow set of
// intentional exceptions (gradient stops, shadows, one-off visual
// treatments documented in the PR E plan).

import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const HEX_RGBA = /#[0-9a-fA-F]{3,8}\b|rgba?\([^)]*\)/g;

// File → patterns it's allowed to keep raw. Matchers are substrings the
// problematic value contains; if a hit matches any of these, it's OK.
const ALLOWED = {
    "web/style.css": [
        // Token declarations themselves.
        "--",
        // Body / page background gradients use one-off accent dark stops.
        "#172554",
        // Install-hint banner — one-shot visual treatment, see plan task 2.
        "install-hint",
        // .ghost-btn etc. translucent button family (kept raw deliberately).
        "rgba(15, 23, 42, 0.96)",
        // Scrollbar rgb overlays.
        "scrollbar",
        // Drop-shadows and box-shadow values are one-off intentional rgbas.
        "box-shadow",
        // Replay progress / vendor xterm overlays — out of scope for PR E.
        "replay-progress",
        "xterm",
    ],
    "web/settings.html": [
        "--",
        "#172554", // shared gradient stop
        "box-shadow",
        "rgba(2, 6, 23, 0.48)",      // dialog backdrop
        "rgba(74, 222, 128",          // one-off good-toned alpha (plain rgba kept)
    ],
    "web/admin/index.html": [
        "--",
        "#1e1b4b",                    // admin gradient violet stop
        "box-shadow",
        "rgba(0, 0, 0, 0.3)",         // code box backdrop
    ],
    "web/login.html": [],
    "web/signup.html": [],
};

function leakLines(path) {
    const src = readFileSync(path, "utf8");
    const lines = src.split("\n");
    const allowed = ALLOWED[path] ?? [];
    const leaks = [];
    for (let i = 0; i < lines.length; i++) {
        const line = lines[i];
        if (!HEX_RGBA.test(line)) { HEX_RGBA.lastIndex = 0; continue; }
        HEX_RGBA.lastIndex = 0;
        if (allowed.some((tag) => line.includes(tag))) continue;
        leaks.push(`${path}:${i + 1}: ${line.trim()}`);
    }
    return leaks;
}

test("web/style.css has no raw #hex / rgba() outside the allow-list", () => {
    const leaks = leakLines("web/style.css");
    assert.equal(leaks.length, 0, "raw color literals found:\n" + leaks.join("\n"));
});

test("web/settings.html inline <style> has no raw colors outside the allow-list", () => {
    const leaks = leakLines("web/settings.html");
    assert.equal(leaks.length, 0, "raw color literals found:\n" + leaks.join("\n"));
});

test("web/admin/index.html inline <style> has no raw colors outside the allow-list", () => {
    const leaks = leakLines("web/admin/index.html");
    assert.equal(leaks.length, 0, "raw color literals found:\n" + leaks.join("\n"));
});

test("web/login.html + web/signup.html stay color-free (rely on style.css tokens)", () => {
    for (const path of ["web/login.html", "web/signup.html"]) {
        const leaks = leakLines(path);
        assert.equal(leaks.length, 0, `${path}: ${leaks.join("\n")}`);
    }
});
```

- [ ] **Step 2: Run**

```bash
node --test web/no-raw-colors.test.mjs 2>&1 | tail -10
```

If this test FAILS, the failure messages tell you what colors remain unaccounted for. Either:
- Add the missing pattern to the file's ALLOWED list (with a comment explaining why it's intentional), OR
- Go back and replace the raw color with a token in the appropriate task.

Aim for a small, well-documented ALLOWED list — every entry is a deliberate exception.

- [ ] **Step 3: Commit**

```bash
git add web/no-raw-colors.test.mjs
git commit -m "web(test): forbid raw #hex/rgba() in token-driven files (allow-list documented)"
```

---

## Task 7 — sw cache bump + ship `v0.1.77`

**Files:**
- Modify: `web/sw.js` (hash bump only)

- [ ] **Step 1: Bump cache**

```bash
node --test web/sw-cache-bump.test.mjs 2>&1 | grep -A2 "CACHE = " | head -5
```

Paste the new 8-hex hash into `web/sw.js`:

```js
const CACHE = "at-term-web-<new hash>";
```

```bash
node --test web/*.test.mjs web/admin/*.test.mjs 2>&1 | tail -8
```

Expected: 0 failures.

```bash
git add web/sw.js
git commit -m "web(sw): cache hash bump for design-token + auth polish changes"
```

- [ ] **Step 2: Full Go + web sweep**

```bash
go test -count=1 -timeout 120s ./... 2>&1 | tail -10
node --test web/*.test.mjs web/admin/*.test.mjs 2>&1 | tail -8
```

Expected: ALL green.

- [ ] **Step 3: Push, PR, merge, tag**

```bash
git push -u origin feat/design-tokens

gh pr create --title "feat(web): design-token audit + login/signup polish" --body "$(cat <<'EOF'
## Summary

PR E — final piece of the web UI redesign. Establishes a complete design-token system so every page (user + admin + auth) consumes the same palette via `--*` custom properties.

**New tokens** (in `:root`):
- Surfaces: `--surface` / `--surface-2` (aliases of `--panel` / `--panel-2`), `--surface-hover`, `--surface-button`, `--topbar-bg`
- Text: `--fg-mute` (alias of `--fg-dim`)
- Borders: `--border-mute`
- Accent / status: `--accent-bg`, `--warn`, `--warn-bg`, `--bad-bg`
- Spacing scale: `--space-1` through `--space-8`

**Raw-color cleanup**:
- `web/style.css` — translucent topbar + page-bar + topnav active/hover backgrounds now reference tokens instead of literal rgba()
- `web/settings.html` and `web/admin/index.html` inline styles — likewise
- Body radial-gradient now uses `var(--bg)` instead of duplicating the literal

**Login/signup polish** (via `.auth-card` rules in style.css):
- Card padding/border use spacing + border-mute tokens for a softer treatment
- Focus outline on inputs is now accent-tinted instead of browser default
- Error messages have a dedicated `--bad` + `--bad-bg` styling so they read as proper alerts
- "Sign up here" alt text uses `--fg-mute` for hierarchy

**Regression guard**: `web/no-raw-colors.test.mjs` (new) blocks raw `#hex` / `rgba()` from creeping back into the token-driven files, with a narrow documented allow-list (gradient stops, install-hint banner, one-off shadows).

Scope intentionally NOT in this PR: flipping the palette to the spec's literal GitHub-Dark hex values — the current colors have been live across PRs A–D and changing them in a polish PR would surprise users. A separate palette refresh can do that.

## Test plan

- [x] `node --test web/*.test.mjs web/admin/*.test.mjs` — all PASS including `no-raw-colors`
- [x] `go test ./...` — unchanged
- [ ] After deploy: hard-reload to pick up new sw cache; login page shows polished form (focus outline, error alert styling); active topnav tab background matches current accent (no GitHub-blue inconsistency); admin warn-banner uses defined `--warn` token
EOF
)"
```

- [ ] **Step 4: Squash + tag**

```bash
gh pr merge <number> --squash
git fetch origin main
SHA=$(gh pr view <number> --json mergeCommit -q .mergeCommit.oid)
git tag v0.1.77 $SHA
git push origin v0.1.77
git push origin --delete feat/design-tokens
gh run list --limit 3
```

---

## Done Criteria

- All 7 tasks complete with green commits.
- `node --test web/*.test.mjs web/admin/*.test.mjs` includes `no-raw-colors.test.mjs` passing.
- `v0.1.77` tag pushed; Release workflow succeeded.
- Web UI redesign series (PRs A–E) complete.

## Out of Scope

- Flipping the literal palette to the spec's GitHub-Dark hex values.
- Vendor xterm theme tokens (handled by `desktop/frontend/src/lib/terminalThemes.ts`, not the web frontend).
- Push-notification icon redesign / index topbar bell button.
