# Relay Password Toggle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a show/hide eye toggle to the password input in desktop Settings → Relay's "connect to remote relay" form.

**Architecture:** Pure presentational change in one Vue component. Add `showPassword: ref(false)`, wrap the existing `<input>` in a relative container, place an absolutely-positioned `<button>` with inline SVG eye/eye-off icons inside the input on the right. Toggle `:type` and `aria-pressed` from the ref. Two new i18n keys; no new dependencies.

**Tech Stack:** Vue 3 SFC + `<script setup>`, vue-i18n, vitest, scoped CSS.

**Spec:** `docs/superpowers/specs/2026-06-10-relay-password-toggle-design.md`.

---

## File structure

| File | Status | Purpose |
|---|---|---|
| `desktop/frontend/src/components/SettingsRelay.vue` | modify | Add `showPassword` ref, wrap password input, add toggle button + inline SVG icons, scoped CSS |
| `desktop/frontend/src/i18n/messages/en.ts` | modify | Add `settings.relay.passwordShow` / `passwordHide` |
| `desktop/frontend/src/i18n/messages/zh-CN.ts` | modify | Add same keys in Chinese |
| `desktop/frontend/src/components/SettingsRelay.test.ts` | modify | Add test asserting toggle is present and i18n keys are referenced |

Single commit. No new dependencies, no other files touched.

---

## Task 1: Add password toggle to SettingsRelay.vue + i18n + test

### Files

- Modify: `desktop/frontend/src/components/SettingsRelay.vue`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`
- Test: `desktop/frontend/src/components/SettingsRelay.test.ts`

### Step 1: Add the failing test

In `desktop/frontend/src/components/SettingsRelay.test.ts`, append a new test inside the existing `describe("SettingsRelay", () => { ... })` block (find the closing `})` near the end of the file and insert the test just before it).

Use the **source-string assertion** pattern that this file already uses everywhere (see `expect(source).toContain('id="relay-login-password"')` and friends — the test file reads the SFC source as text and asserts substrings):

```ts
  test("password input has show/hide toggle", () => {
    // language-agnostic structural checks against the SFC text
    // — same pattern as the other tests in this file
    const fs = require("node:fs") as typeof import("node:fs");
    const path = require("node:path") as typeof import("node:path");
    const source = fs.readFileSync(
      path.resolve(__dirname, "SettingsRelay.vue"),
      "utf8",
    );
    // toggle ref + binding present
    expect(source).toContain("showPassword");
    expect(source).toContain(':type="showPassword ? \'text\' : \'password\'"');
    // toggle button present with aria-pressed bound to the ref
    expect(source).toContain('class="password-toggle"');
    expect(source).toContain(':aria-pressed="showPassword"');
    // i18n keys for the toggle's accessible label
    expect(source).toContain("settings.relay.passwordShow");
    expect(source).toContain("settings.relay.passwordHide");
  });
```

If the existing tests in this file already use a `source` helper instead of reading the file ad-hoc, mirror that — look at the test right above (`"Log in button calls loginRemoteRelay..."`) for the exact pattern and copy it. **Do NOT** introduce a new test approach; this file uses a single consistent pattern.

### Step 2: Run test, verify it fails

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/SettingsRelay.test.ts -t "password input has show/hide toggle" 2>&1 | tail -20
```

Expected: FAIL — assertions like `expect(source).toContain("showPassword")` will fail because that string doesn't exist in the SFC yet.

### Step 3: Add i18n keys (en.ts)

In `desktop/frontend/src/i18n/messages/en.ts`, find the `settings.relay` namespace (search for `password: "Password",` — it's around line 178). Add two new keys directly after the existing `password` key:

```ts
      password: "Password",
      passwordShow: "Show password",
      passwordHide: "Hide password",
```

Preserve trailing commas, indentation (2 spaces × 3 = 6 spaces for these nested keys — match what's already there).

### Step 4: Add i18n keys (zh-CN.ts)

In `desktop/frontend/src/i18n/messages/zh-CN.ts`, find the same `settings.relay.password` key (around line 180). Add:

```ts
      password: "密码",
      passwordShow: "显示密码",
      passwordHide: "隐藏密码",
```

Match indentation.

### Step 5: Add `showPassword` ref to SettingsRelay.vue script

In `desktop/frontend/src/components/SettingsRelay.vue`, find the existing `const password = ref("");` line (around line 30). Add directly after it:

```ts
const showPassword = ref(false);
```

### Step 6: Wrap the password input and add the toggle button

Find the existing password input block (around lines 245-253):

```html
<label class="field-label" for="relay-login-password">{{ t("settings.relay.password") }}</label>
<input
  id="relay-login-password"
  v-model="password"
  type="password"
  autocomplete="current-password"
  :disabled="loginInProgress || saving"
  @keyup.enter="login"
/>
```

Replace **the `<input>` element only** (keep the `<label>` exactly as it is) with this wrapped version:

```html
<div class="password-field">
  <input
    id="relay-login-password"
    v-model="password"
    :type="showPassword ? 'text' : 'password'"
    autocomplete="current-password"
    :disabled="loginInProgress || saving"
    @keyup.enter="login"
  />
  <button
    type="button"
    class="password-toggle"
    :aria-label="showPassword ? t('settings.relay.passwordHide') : t('settings.relay.passwordShow')"
    :aria-pressed="showPassword"
    :disabled="loginInProgress || saving"
    @click="showPassword = !showPassword"
  >
    <svg
      v-if="!showPassword"
      width="18"
      height="18"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
    <svg
      v-else
      width="18"
      height="18"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24" />
      <line x1="1" y1="1" x2="23" y2="23" />
    </svg>
  </button>
</div>
```

### Step 7: Add scoped CSS for the toggle

In the same file, find the `<style scoped>` block (or wherever the `input[type="password"]` rule lives — the spec noted it's around line 463). Add these rules at the end of the scoped style block (just before `</style>`):

```css
.password-field {
  position: relative;
  display: block;
}

.password-field input {
  width: 100%;
  padding-right: 36px;
}

.password-toggle {
  position: absolute;
  top: 50%;
  right: 8px;
  transform: translateY(-50%);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  padding: 0;
  border: 0;
  background: transparent;
  color: rgba(255, 255, 255, 0.55);
  border-radius: 4px;
  cursor: pointer;
  transition: color 0.15s ease, background 0.15s ease;
}

.password-toggle:hover:not(:disabled) {
  color: rgba(255, 255, 255, 0.9);
  background: rgba(255, 255, 255, 0.08);
}

.password-toggle:focus-visible {
  outline: 2px solid rgba(100, 160, 255, 0.8);
  outline-offset: 1px;
}

.password-toggle:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
```

If the existing component uses a different colour scheme (e.g. CSS custom properties like `var(--text-muted)` or `var(--accent)`), substitute the hardcoded `rgba(...)` values with those vars to stay consistent with the rest of the component. Glance at neighbouring rules in the scoped style block before writing and pick whichever pattern matches.

### Step 8: Verify the test now passes

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/SettingsRelay.test.ts -t "password input has show/hide toggle" 2>&1 | tail -15
```

Expected: PASS.

### Step 9: Run full SettingsRelay test file

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/SettingsRelay.test.ts 2>&1 | tail -10
```

Expected: all tests in the file PASS (including the existing ones, none of which the change should have broken).

### Step 10: Type-check + full test suite sanity

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit 2>&1 | tail -5
```

Expected: clean (no output).

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm test 2>&1 | tail -10
```

Expected: all test files PASS.

### Step 11: Commit (stage individually — no `git add -A`)

```bash
cd /Users/attson/code/github.com.attson/atterm && git status
git add desktop/frontend/src/components/SettingsRelay.vue \
        desktop/frontend/src/components/SettingsRelay.test.ts \
        desktop/frontend/src/i18n/messages/en.ts \
        desktop/frontend/src/i18n/messages/zh-CN.ts
git status
git commit -m "$(cat <<'COMMIT'
feat(desktop): password show/hide toggle on Settings → Relay

The remote-relay login form's password field now has an eye icon on
the right that toggles between masked and plaintext. Helps users
spot-check what they actually typed when login keeps failing.

- showPassword ref + bound input :type
- inline SVG eye / eye-off icons (no new dep)
- ARIA: aria-label flips between settings.relay.passwordShow /
  passwordHide, aria-pressed reflects the ref
- Scoped CSS only; no theme variables introduced
- Test asserts the toggle markup + i18n key references

Spec: docs/superpowers/specs/2026-06-10-relay-password-toggle-design.md
COMMIT
)"
```

DO NOT use `git add -A`. The `desktop/frontend/package.json.md5` build artifact tends to be locally dirty and must stay out of this commit.

---

## Final verification

This is a one-task plan; once Task 1's Step 11 commit lands the plan is complete. No PR / tag in this plan — the user will decide whether to ship as a standalone hotfix or batch with other changes.
