# Desktop shortcut settings — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `Shortcuts` tab to the desktop `SettingsDialog` that lets users view, rebind, disable, and reset the 12 terminal navigation shortcuts currently hard-coded in `useTerminalShortcuts.ts`.

**Architecture:** Persist a sparse `shortcuts.bindings` map (action ID → `"Mod+Alt+Shift+<code>"` string) inside `PluginConfig`. The frontend owns the registry of default bindings; the backend only validates string format. `useTerminalShortcuts.ts` is refactored to accept an injected `bindings` ref and routes via a `computed` table built from defaults + user overrides. A new `SettingsShortcuts.vue` plus small `HotkeyCaptureCell.vue` provide the editing UI on top of the existing pluginConfigStore / Save flow.

**Tech Stack:** Wails (Go) backend; Vue 3 + Pinia + Vitest + jsdom on the frontend; vue-tsc for type checking.

**Spec:** `docs/superpowers/specs/2026-05-22-desktop-shortcut-settings-design.md`

---

## File map

**New:**
- `desktop/frontend/src/lib/shortcutBindings.ts` — registry, types, serialize/parse/conflictsWith/buildRoutingTable
- `desktop/frontend/src/lib/shortcutBindings.test.ts`
- `desktop/frontend/src/components/HotkeyCaptureCell.vue` — single-cell capture
- `desktop/frontend/src/components/HotkeyCaptureCell.test.ts`
- `desktop/frontend/src/components/SettingsShortcuts.vue` — the new tab
- `desktop/frontend/src/components/SettingsShortcuts.test.ts`

**Modified:**
- `desktop/plugin_config.go` — add `ShortcutsConfig`, defaults, validation
- `desktop/plugin_config_test.go` — extend
- `desktop/frontend/src/composables/useTerminalShortcuts.ts` — accept injected bindings, replace hard-coded routing with table lookup
- `desktop/frontend/src/composables/useTerminalShortcuts.test.ts` — extend
- `desktop/frontend/src/App.vue` — bridge `pluginConfigStore.cfg.shortcuts.bindings` into the composable
- `desktop/frontend/src/components/SettingsDialog.vue` — add `Shortcuts` nav item + pane
- `desktop/frontend/src/components/SettingsDialog.test.ts` — extend

---

## Conventions

- **Working directory:** `/Users/attson/code/github.com.attson/atterm`
- **Run go tests:** `cd desktop && go test ./...`
- **Run frontend tests:** `cd desktop/frontend && npm run test`
- **Run frontend type check:** `cd desktop/frontend && npm run build` (this also produces a build, which is fine)
- **One file at a time:** each step shows the full final content for any file it touches if the file is small, or a precise edit (old → new) for larger files.

---

## Task 1: backend `ShortcutsConfig` field + defaults + validation

**Files:**
- Modify: `desktop/plugin_config.go`
- Test: `desktop/plugin_config_test.go`

- [ ] **Step 1.1: Write the failing tests**

Open `desktop/plugin_config_test.go` and append these tests at the end of the file:

```go
func TestPluginConfig_ShortcutsDefaults(t *testing.T) {
	var c PluginConfig
	c.applyDefaults()
	if c.Shortcuts.Bindings == nil {
		t.Fatal("Shortcuts.Bindings should be initialized to an empty map by applyDefaults")
	}
	if len(c.Shortcuts.Bindings) != 0 {
		t.Fatalf("Shortcuts.Bindings should default to empty, got %d entries", len(c.Shortcuts.Bindings))
	}
}

func TestValidatePluginConfig_ShortcutsAcceptsValid(t *testing.T) {
	c := PluginConfig{}
	c.applyDefaults()
	c.Shortcuts.Bindings = map[string]string{
		"pane.split-vertical-new":   "Mod+KeyN",
		"pane.focus-left":           "Mod+Alt+ArrowLeft",
		"pane.split-horizontal-new": "Mod+Alt+Shift+KeyJ",
		"pane.close":                "", // disabled is valid
	}
	if err := ValidatePluginConfig(c); err != nil {
		t.Fatalf("valid bindings should validate, got %v", err)
	}
}

func TestValidatePluginConfig_ShortcutsRejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"missing modifier":   "KeyN",
		"missing code":       "Mod+Alt+",
		"unknown token":      "Foo+KeyN",
		"two codes":          "Mod+KeyN+KeyM",
		"code only":          "ArrowLeft",
		"only modifiers":     "Mod+Alt+Shift",
		"wrong code name":    "Mod+keya",
		"empty action id":    "Mod+KeyN", // checked via empty key below
	}
	for label, binding := range cases {
		c := PluginConfig{}
		c.applyDefaults()
		c.Shortcuts.Bindings = map[string]string{"some.action": binding}
		if label == "empty action id" {
			c.Shortcuts.Bindings = map[string]string{"": binding}
		}
		if err := ValidatePluginConfig(c); err == nil {
			t.Errorf("case %q: expected validation error for binding %q", label, binding)
		}
	}
}
```

- [ ] **Step 1.2: Run tests to verify they fail**

Run: `cd desktop && go test ./... -run 'TestPluginConfig_ShortcutsDefaults|TestValidatePluginConfig_Shortcuts'`

Expected: compile error — `c.Shortcuts` and `ShortcutsConfig` undefined.

- [ ] **Step 1.3: Add the struct field, default, and validator**

Edit `desktop/plugin_config.go`.

Find the `PluginConfig` struct (lines 11–15) and replace it with:

```go
// PluginConfig is the persisted plugin-system configuration block.
type PluginConfig struct {
	QuickInput   QuickInputConfig   `json:"quickInput"`
	FileExplorer FileExplorerConfig `json:"fileExplorer"`
	Translate    TranslateConfig    `json:"translate"`
	Shortcuts    ShortcutsConfig    `json:"shortcuts"`
}
```

Add the new type immediately after `TranslateConfig` (around line 46), before `allowedTranslateTargetLangs`:

```go
// ShortcutsConfig is the persisted shortcut-binding overrides. The map is
// sparse: only actions the user has explicitly changed appear here. Absent
// keys mean "use the frontend registry default for this action"; an empty
// value means "this action is disabled".
type ShortcutsConfig struct {
	Bindings map[string]string `json:"bindings"`
}

// shortcutBindingRe matches a serialized binding string. Tokens are in fixed
// order: optional Mod, optional Alt, optional Shift (at least one modifier
// required), then exactly one code token from the whitelist. The empty
// string is also a valid binding (means "disabled") and is handled by the
// caller before applying this regex.
var shortcutBindingRe = regexp.MustCompile(
	`^(?:Mod\+)?(?:Alt\+)?(?:Shift\+)?(?:KeyA|KeyB|KeyC|KeyD|KeyE|KeyF|KeyG|KeyH|KeyI|KeyJ|KeyK|KeyL|KeyM|KeyN|KeyO|KeyP|KeyQ|KeyR|KeyS|KeyT|KeyU|KeyV|KeyW|KeyX|KeyY|KeyZ|Digit0|Digit1|Digit2|Digit3|Digit4|Digit5|Digit6|Digit7|Digit8|Digit9|ArrowLeft|ArrowRight|ArrowUp|ArrowDown|BracketLeft|BracketRight|Minus|Equal|Backquote|Comma|Period|Slash|Semicolon|Quote|Backslash)$`,
)

func isValidShortcutBinding(s string) bool {
	if s == "" {
		return true
	}
	// Regex permits "no modifier" because Go's RE2 doesn't have lookaheads;
	// enforce the "at least one modifier" rule separately.
	if !strings.HasPrefix(s, "Mod+") && !strings.HasPrefix(s, "Alt+") && !strings.HasPrefix(s, "Shift+") {
		return false
	}
	return shortcutBindingRe.MatchString(s)
}
```

Add `"regexp"` and `"strings"` to the import block at the top (line 3–8):

```go
import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)
```

In `applyDefaults` (currently ends at line 82), append before the closing brace:

```go
	if c.Shortcuts.Bindings == nil {
		c.Shortcuts.Bindings = map[string]string{}
	}
```

In `ValidatePluginConfig` (currently lines 100–127), append before the final `return nil`:

```go
	for actionID, binding := range c.Shortcuts.Bindings {
		if actionID == "" {
			return errors.New("shortcuts.bindings: action id must be non-empty")
		}
		if !isValidShortcutBinding(binding) {
			return fmt.Errorf("shortcuts.bindings[%q]: malformed binding %q", actionID, binding)
		}
	}
```

- [ ] **Step 1.4: Run tests to verify they pass**

Run: `cd desktop && go test ./...`

Expected: all tests pass, including the new ones.

- [ ] **Step 1.5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/plugin_config.go desktop/plugin_config_test.go
git commit -m "feat(desktop): persist shortcut bindings in plugin config"
```

---

## Task 2: regenerate Wails bindings + verify TS sees the new field

**Files:**
- Generated: `desktop/frontend/wailsjs/go/models.ts`

- [ ] **Step 2.1: Regenerate Wails bindings**

Run: `cd desktop && wails generate module 2>/dev/null || true`

If `wails generate module` is not available, the bindings will be regenerated automatically on the next `wails dev` / `wails build`. Inspect manually:

Run: `grep -n "ShortcutsConfig\|shortcuts" /Users/attson/code/github.com.attson/atterm/desktop/frontend/wailsjs/go/models.ts`

If `ShortcutsConfig` is not present, run a development build to trigger regeneration:

Run: `cd desktop/frontend && npm run build`

If `models.ts` still lacks `ShortcutsConfig`, regenerate by running `wails dev` once briefly (Ctrl-C after it prints "Vite Server starting"):

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop && timeout 8 wails dev || true
```

Then verify with grep again.

- [ ] **Step 2.2: Confirm the TS shape**

Run: `grep -n -A 4 "class ShortcutsConfig" /Users/attson/code/github.com.attson/atterm/desktop/frontend/wailsjs/go/models.ts`

Expected: a class definition with a `bindings: {[key: string]: string}` field (the exact TypeScript form may vary; what matters is that it exists).

- [ ] **Step 2.3: Update `configStore.ts` re-export to surface the new type**

Edit `desktop/frontend/src/plugins/configStore.ts` and add the type re-export to the block at lines 9–13:

```ts
// Re-export Wails types for convenience
export type QuickInputButton = main.QuickInputButton;
export type QuickInputConfig = main.QuickInputConfig;
export type FileExplorerConfig = main.FileExplorerConfig;
export type TranslateConfig = main.TranslateConfig;
export type ShortcutsConfig = main.ShortcutsConfig;
export type PluginConfig = main.PluginConfig;
```

- [ ] **Step 2.4: Type-check the frontend**

Run: `cd desktop/frontend && npm run build`

Expected: build succeeds. No new TypeScript errors.

- [ ] **Step 2.5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/wailsjs/go/models.ts desktop/frontend/src/plugins/configStore.ts
git commit -m "chore(desktop): regenerate wails bindings for shortcut config"
```

---

## Task 3: `shortcutBindings.ts` — types and registry

**Files:**
- Create: `desktop/frontend/src/lib/shortcutBindings.ts`
- Create: `desktop/frontend/src/lib/shortcutBindings.test.ts`

- [ ] **Step 3.1: Write the failing tests**

Create `desktop/frontend/src/lib/shortcutBindings.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { ACTIONS, ACTION_BY_ID, DEFAULT_BINDINGS } from "./shortcutBindings";

describe("shortcutBindings registry", () => {
  it("declares 12 actions", () => {
    expect(ACTIONS).toHaveLength(12);
  });

  it("has unique action IDs", () => {
    const ids = ACTIONS.map((a) => a.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("groups actions under pane or tab", () => {
    for (const a of ACTIONS) {
      expect(["pane", "tab"]).toContain(a.group);
    }
  });

  it("ACTION_BY_ID looks up actions by id", () => {
    expect(ACTION_BY_ID["pane.split-vertical-new"]?.defaultBinding).toBe("Mod+KeyN");
    expect(ACTION_BY_ID["tab.next"]?.defaultBinding).toBe("Mod+Shift+BracketRight");
  });

  it("DEFAULT_BINDINGS reverse-maps binding -> actionId for every action", () => {
    for (const a of ACTIONS) {
      expect(DEFAULT_BINDINGS[a.defaultBinding]).toBe(a.id);
    }
    expect(Object.keys(DEFAULT_BINDINGS)).toHaveLength(12);
  });
});
```

- [ ] **Step 3.2: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/lib/shortcutBindings.test.ts`

Expected: fail — module not found.

- [ ] **Step 3.3: Create the registry file**

Create `desktop/frontend/src/lib/shortcutBindings.ts`:

```ts
// Pure, framework-agnostic helpers for terminal shortcut bindings.
//
// A binding string has the form "Mod+Alt+Shift+<code>" where:
//   - "Mod" is a platform-agnostic modifier (Meta on mac, Control elsewhere).
//   - Token order is fixed: Mod, Alt, Shift, code — only the modifiers
//     actually pressed appear.
//   - <code> is a KeyboardEvent.code from a known whitelist.
//   - The empty string means "disabled" — this action will not be routed.

export type ShortcutGroup = "pane" | "tab";

export interface ShortcutAction {
  id: string;
  group: ShortcutGroup;
  label: string;
  defaultBinding: string;
}

export const ACTIONS: readonly ShortcutAction[] = [
  { id: "pane.split-vertical-new",    group: "pane", label: "Split pane vertically",                defaultBinding: "Mod+KeyN" },
  { id: "pane.split-vertical-pick",   group: "pane", label: "Split pane vertically (pick target)",  defaultBinding: "Mod+Alt+KeyN" },
  { id: "pane.split-horizontal-new",  group: "pane", label: "Split pane horizontally",              defaultBinding: "Mod+Shift+KeyN" },
  { id: "pane.split-horizontal-pick", group: "pane", label: "Split pane horizontally (pick target)", defaultBinding: "Mod+Alt+Shift+KeyN" },
  { id: "pane.close",                 group: "pane", label: "Close pane",                            defaultBinding: "Mod+KeyW" },
  { id: "pane.focus-left",            group: "pane", label: "Focus pane left",                       defaultBinding: "Mod+Alt+ArrowLeft" },
  { id: "pane.focus-right",           group: "pane", label: "Focus pane right",                      defaultBinding: "Mod+Alt+ArrowRight" },
  { id: "pane.focus-up",              group: "pane", label: "Focus pane up",                         defaultBinding: "Mod+Alt+ArrowUp" },
  { id: "pane.focus-down",            group: "pane", label: "Focus pane down",                       defaultBinding: "Mod+Alt+ArrowDown" },
  { id: "tab.new",                    group: "tab",  label: "New tab",                               defaultBinding: "Mod+KeyT" },
  { id: "tab.prev",                   group: "tab",  label: "Previous tab",                          defaultBinding: "Mod+Shift+BracketLeft" },
  { id: "tab.next",                   group: "tab",  label: "Next tab",                              defaultBinding: "Mod+Shift+BracketRight" },
] as const;

export const ACTION_BY_ID: Record<string, ShortcutAction> = Object.fromEntries(
  ACTIONS.map((a) => [a.id, a]),
);

// Reverse map of defaults: binding -> actionId. Used by buildRoutingTable as
// the starting layer beneath user overrides.
export const DEFAULT_BINDINGS: Record<string, string> = Object.fromEntries(
  ACTIONS.map((a) => [a.defaultBinding, a.id]),
);
```

- [ ] **Step 3.4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/lib/shortcutBindings.test.ts`

Expected: all pass.

- [ ] **Step 3.5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/lib/shortcutBindings.ts desktop/frontend/src/lib/shortcutBindings.test.ts
git commit -m "feat(desktop): shortcut action registry"
```

---

## Task 4: `shortcutBindings.ts` — `serialize` and `parse`

**Files:**
- Modify: `desktop/frontend/src/lib/shortcutBindings.ts`
- Modify: `desktop/frontend/src/lib/shortcutBindings.test.ts`

- [ ] **Step 4.1: Write the failing tests**

Append to `desktop/frontend/src/lib/shortcutBindings.test.ts`:

```ts
import { serialize, parse, type Mod } from "./shortcutBindings";

function ev(opts: KeyboardEventInit & { key: string; code: string }): KeyboardEvent {
  return new KeyboardEvent("keydown", { ...opts, bubbles: true, cancelable: true });
}

describe("serialize", () => {
  it("Ctrl+N (mod=Control) -> 'Mod+KeyN'", () => {
    const e = ev({ key: "n", code: "KeyN", ctrlKey: true });
    expect(serialize(e, "Control")).toBe("Mod+KeyN");
  });

  it("Meta+N (mod=Meta) -> 'Mod+KeyN'", () => {
    const e = ev({ key: "n", code: "KeyN", metaKey: true });
    expect(serialize(e, "Meta")).toBe("Mod+KeyN");
  });

  it("Ctrl+Alt+Shift+N preserves token order Mod,Alt,Shift,code", () => {
    const e = ev({ key: "N", code: "KeyN", ctrlKey: true, altKey: true, shiftKey: true });
    expect(serialize(e, "Control")).toBe("Mod+Alt+Shift+KeyN");
  });

  it("Alt+Shift+ArrowLeft (no Mod) -> 'Alt+Shift+ArrowLeft'", () => {
    const e = ev({ key: "ArrowLeft", code: "ArrowLeft", altKey: true, shiftKey: true });
    expect(serialize(e, "Control")).toBe("Alt+Shift+ArrowLeft");
  });

  it("wrong modifier (Meta on Control platform) returns null", () => {
    const e = ev({ key: "n", code: "KeyN", metaKey: true });
    expect(serialize(e, "Control")).toBeNull();
  });

  it("modifier-only keypress returns null", () => {
    // Ctrl alone — code is ControlLeft (not in whitelist) and there's
    // effectively no key. We model "modifier-only" by passing a code that
    // is not in the whitelist.
    const e = ev({ key: "Control", code: "ControlLeft", ctrlKey: true });
    expect(serialize(e, "Control")).toBeNull();
  });

  it("bare letter (no modifier) returns null", () => {
    const e = ev({ key: "n", code: "KeyN" });
    expect(serialize(e, "Control")).toBeNull();
  });

  it("mac dead key (key='˜', code='KeyN') still produces 'Mod+Alt+KeyN'", () => {
    const e = ev({ key: "˜", code: "KeyN", ctrlKey: true, altKey: true });
    expect(serialize(e, "Control")).toBe("Mod+Alt+KeyN");
  });

  it("code outside the whitelist returns null", () => {
    const e = ev({ key: "Tab", code: "Tab", ctrlKey: true });
    expect(serialize(e, "Control")).toBeNull();
  });
});

describe("parse", () => {
  it("round-trips a normal binding", () => {
    expect(parse("Mod+Alt+Shift+KeyN")).toEqual({
      mod: true, alt: true, shift: true, code: "KeyN",
    });
  });

  it("parses no-modifier-error correctly (returns null)", () => {
    expect(parse("KeyN")).toBeNull();
  });

  it("parses empty as empty (sentinel for disabled)", () => {
    expect(parse("")).toEqual({ mod: false, alt: false, shift: false, code: null });
  });

  it("rejects unknown tokens", () => {
    expect(parse("Hyper+KeyN")).toBeNull();
  });

  it("rejects out-of-order tokens", () => {
    expect(parse("Alt+Mod+KeyN")).toBeNull();
  });

  it("rejects two codes", () => {
    expect(parse("Mod+KeyN+KeyM")).toBeNull();
  });

  // Mod type is exported for callers; this just ensures it compiles.
  const _m: Mod = "Control";
  void _m;
});
```

- [ ] **Step 4.2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/lib/shortcutBindings.test.ts`

Expected: failing on missing exports `serialize`, `parse`, `Mod`.

- [ ] **Step 4.3: Implement `serialize` and `parse`**

Append to `desktop/frontend/src/lib/shortcutBindings.ts`:

```ts
export type Mod = "Meta" | "Control";

const CODE_WHITELIST: ReadonlySet<string> = new Set([
  "KeyA","KeyB","KeyC","KeyD","KeyE","KeyF","KeyG","KeyH","KeyI","KeyJ","KeyK","KeyL","KeyM",
  "KeyN","KeyO","KeyP","KeyQ","KeyR","KeyS","KeyT","KeyU","KeyV","KeyW","KeyX","KeyY","KeyZ",
  "Digit0","Digit1","Digit2","Digit3","Digit4","Digit5","Digit6","Digit7","Digit8","Digit9",
  "ArrowLeft","ArrowRight","ArrowUp","ArrowDown",
  "BracketLeft","BracketRight",
  "Minus","Equal","Backquote","Comma","Period","Slash","Semicolon","Quote","Backslash",
]);

export interface ParsedBinding {
  mod: boolean;
  alt: boolean;
  shift: boolean;
  code: string | null;
}

// serialize converts a KeyboardEvent into a binding string. Returns null if
//   - the event's "wrong modifier" is pressed (Meta on Control platforms,
//     Control on Meta platforms),
//   - the code is not in the whitelist,
//   - no modifier is held (we never bind a bare key — would intercept typing).
export function serialize(e: KeyboardEvent, mod: Mod): string | null {
  const isMod = mod === "Meta" ? e.metaKey : e.ctrlKey;
  const wrongMod = mod === "Meta" ? e.ctrlKey : e.metaKey;
  if (wrongMod) return null;
  if (!CODE_WHITELIST.has(e.code)) return null;
  if (!isMod && !e.altKey && !e.shiftKey) return null;
  const parts: string[] = [];
  if (isMod) parts.push("Mod");
  if (e.altKey) parts.push("Alt");
  if (e.shiftKey) parts.push("Shift");
  parts.push(e.code);
  return parts.join("+");
}

// parse converts a binding string into a structured ParsedBinding, or returns
// null for malformed input. The empty string is treated as the sentinel
// "disabled" binding and parses successfully with all flags false and code=null.
export function parse(s: string): ParsedBinding | null {
  if (s === "") return { mod: false, alt: false, shift: false, code: null };
  const tokens = s.split("+");
  if (tokens.length < 2) return null;
  const code = tokens[tokens.length - 1];
  if (!CODE_WHITELIST.has(code)) return null;
  const modifiers = tokens.slice(0, -1);
  // Enforce fixed token order: Mod, Alt, Shift.
  const expected = ["Mod", "Alt", "Shift"];
  let i = 0;
  const flags = { mod: false, alt: false, shift: false };
  for (const tok of modifiers) {
    while (i < expected.length && tok !== expected[i]) i++;
    if (i === expected.length) return null;
    if (tok === "Mod") flags.mod = true;
    else if (tok === "Alt") flags.alt = true;
    else if (tok === "Shift") flags.shift = true;
    i++;
  }
  if (!flags.mod && !flags.alt && !flags.shift) return null;
  return { ...flags, code };
}
```

- [ ] **Step 4.4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/lib/shortcutBindings.test.ts`

Expected: all pass.

- [ ] **Step 4.5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/lib/shortcutBindings.ts desktop/frontend/src/lib/shortcutBindings.test.ts
git commit -m "feat(desktop): shortcut binding serialize/parse"
```

---

## Task 5: `shortcutBindings.ts` — `conflictsWith`

**Files:**
- Modify: `desktop/frontend/src/lib/shortcutBindings.ts`
- Modify: `desktop/frontend/src/lib/shortcutBindings.test.ts`

- [ ] **Step 5.1: Write the failing tests**

Append to `desktop/frontend/src/lib/shortcutBindings.test.ts`:

```ts
import { conflictsWith, resolvedBindings } from "./shortcutBindings";

describe("resolvedBindings", () => {
  it("returns defaults for unset actions", () => {
    const r = resolvedBindings({});
    expect(r["pane.split-vertical-new"]).toBe("Mod+KeyN");
    expect(r["tab.next"]).toBe("Mod+Shift+BracketRight");
  });

  it("user overrides win", () => {
    const r = resolvedBindings({ "tab.next": "Mod+KeyL" });
    expect(r["tab.next"]).toBe("Mod+KeyL");
    expect(r["tab.prev"]).toBe("Mod+Shift+BracketLeft"); // unchanged
  });

  it("user can disable an action with empty string", () => {
    const r = resolvedBindings({ "pane.close": "" });
    expect(r["pane.close"]).toBe("");
  });
});

describe("conflictsWith", () => {
  it("detects a conflict between two actions on the same binding", () => {
    const bindings = resolvedBindings({ "pane.close": "Mod+KeyT" });
    // pane.close now collides with tab.new (default Mod+KeyT)
    const c = conflictsWith(bindings, "pane.close");
    expect(c).toEqual(["tab.new"]);
  });

  it("excludes the action being checked", () => {
    const bindings = resolvedBindings({});
    expect(conflictsWith(bindings, "pane.close")).toEqual([]);
  });

  it("ignores empty bindings (disabled actions)", () => {
    const bindings = resolvedBindings({ "pane.close": "", "tab.new": "" });
    expect(conflictsWith(bindings, "pane.close")).toEqual([]);
  });

  it("returns multiple conflicting action ids if 3 collide", () => {
    const bindings = resolvedBindings({
      "pane.close": "Mod+KeyT",
      "pane.focus-left": "Mod+KeyT",
    });
    const c = conflictsWith(bindings, "pane.close").sort();
    expect(c).toEqual(["pane.focus-left", "tab.new"]);
  });
});
```

- [ ] **Step 5.2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/lib/shortcutBindings.test.ts`

Expected: failing on missing exports.

- [ ] **Step 5.3: Implement `resolvedBindings` and `conflictsWith`**

Append to `desktop/frontend/src/lib/shortcutBindings.ts`:

```ts
// resolvedBindings merges the user overrides with the registry defaults and
// returns the resulting action -> binding map (containing all 12 actions).
export function resolvedBindings(
  overrides: Record<string, string>,
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const a of ACTIONS) {
    out[a.id] = a.defaultBinding;
  }
  for (const [id, binding] of Object.entries(overrides)) {
    if (id in ACTION_BY_ID) out[id] = binding;
    // Unknown action IDs are silently dropped (forward/backward compat).
  }
  return out;
}

// conflictsWith inspects a fully-resolved bindings map and returns the IDs
// of other actions that share the same non-empty binding as `actionId`.
export function conflictsWith(
  bindings: Record<string, string>,
  actionId: string,
): string[] {
  const target = bindings[actionId];
  if (!target) return [];
  const result: string[] = [];
  for (const [id, b] of Object.entries(bindings)) {
    if (id === actionId) continue;
    if (b === target) result.push(id);
  }
  return result;
}
```

- [ ] **Step 5.4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/lib/shortcutBindings.test.ts`

Expected: all pass.

- [ ] **Step 5.5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/lib/shortcutBindings.ts desktop/frontend/src/lib/shortcutBindings.test.ts
git commit -m "feat(desktop): shortcut binding conflict detection"
```

---

## Task 6: `shortcutBindings.ts` — `buildRoutingTable`

**Files:**
- Modify: `desktop/frontend/src/lib/shortcutBindings.ts`
- Modify: `desktop/frontend/src/lib/shortcutBindings.test.ts`

- [ ] **Step 6.1: Write the failing tests**

Append to `desktop/frontend/src/lib/shortcutBindings.test.ts`:

```ts
import { buildRoutingTable } from "./shortcutBindings";

describe("buildRoutingTable", () => {
  it("with empty overrides returns the default 12-entry table", () => {
    const t = buildRoutingTable({});
    expect(t["Mod+KeyN"]).toBe("pane.split-vertical-new");
    expect(t["Mod+Alt+KeyN"]).toBe("pane.split-vertical-pick");
    expect(t["Mod+KeyT"]).toBe("tab.new");
    expect(Object.keys(t)).toHaveLength(12);
  });

  it("override removes the action's previous binding entry from the table", () => {
    const t = buildRoutingTable({ "tab.new": "Mod+KeyL" });
    expect(t["Mod+KeyT"]).toBeUndefined();
    expect(t["Mod+KeyL"]).toBe("tab.new");
  });

  it("empty override removes the action from the table entirely", () => {
    const t = buildRoutingTable({ "pane.close": "" });
    expect(t["Mod+KeyW"]).toBeUndefined();
    expect(Object.keys(t)).toHaveLength(11);
  });

  it("two actions colliding on the same binding: last one written wins", () => {
    // This is degenerate config (the UI prevents it via conflicts), but the
    // function should be deterministic. We don't promise order beyond "the
    // override wins over the default", so just assert the override is present.
    const t = buildRoutingTable({ "pane.close": "Mod+KeyT" });
    // Mod+KeyT default was tab.new. Override puts pane.close on Mod+KeyT.
    // Since defaults are seeded first, the override overwrites tab.new.
    expect(t["Mod+KeyT"]).toBe("pane.close");
    // tab.new now has no binding (its default slot was taken)
    expect(Object.values(t)).not.toContain("tab.new");
  });

  it("unknown action IDs in overrides are dropped", () => {
    const t = buildRoutingTable({ "ghost.action": "Mod+KeyN" });
    // Defaults intact, override ignored
    expect(t["Mod+KeyN"]).toBe("pane.split-vertical-new");
    expect(Object.keys(t)).toHaveLength(12);
  });
});
```

- [ ] **Step 6.2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/lib/shortcutBindings.test.ts`

Expected: failing on missing `buildRoutingTable`.

- [ ] **Step 6.3: Implement `buildRoutingTable`**

Append to `desktop/frontend/src/lib/shortcutBindings.ts`:

```ts
// buildRoutingTable produces a binding-string -> actionId map used by the
// runtime keydown router. It seeds with registry defaults, then applies user
// overrides: each override first clears the action's previous slot, then
// (if non-empty) installs the new binding. Unknown action IDs are dropped.
export function buildRoutingTable(
  overrides: Record<string, string>,
): Record<string, string> {
  const table: Record<string, string> = { ...DEFAULT_BINDINGS };
  for (const [id, binding] of Object.entries(overrides)) {
    if (!(id in ACTION_BY_ID)) continue;
    // Find and remove the previous slot for this action (whether default or
    // a prior override) — there is at most one because table values are
    // unique per round of insertions.
    for (const key of Object.keys(table)) {
      if (table[key] === id) {
        delete table[key];
        break;
      }
    }
    if (binding !== "") table[binding] = id;
  }
  return table;
}
```

- [ ] **Step 6.4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/lib/shortcutBindings.test.ts`

Expected: all pass.

- [ ] **Step 6.5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/lib/shortcutBindings.ts desktop/frontend/src/lib/shortcutBindings.test.ts
git commit -m "feat(desktop): shortcut runtime routing table builder"
```

---

## Task 7: refactor `useTerminalShortcuts.ts` to read injected bindings

**Files:**
- Modify: `desktop/frontend/src/composables/useTerminalShortcuts.ts`
- Modify: `desktop/frontend/src/composables/useTerminalShortcuts.test.ts`

- [ ] **Step 7.1: Update the existing test file to drive the new shape**

Replace the entire contents of `desktop/frontend/src/composables/useTerminalShortcuts.test.ts` with:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { effectScope, ref, type Ref } from "vue";
import { useTerminalShortcuts } from "./useTerminalShortcuts";

function fireKey(opts: KeyboardEventInit & { key: string }) {
  const ev = new KeyboardEvent("keydown", { ...opts, bubbles: true, cancelable: true });
  document.dispatchEvent(ev);
  return ev;
}

describe("useTerminalShortcuts (default bindings)", () => {
  let scope: ReturnType<typeof effectScope>;
  let bindings: Ref<Record<string, string>>;
  const handlers = {
    onSplitVertical: vi.fn(),
    onSplitHorizontal: vi.fn(),
    onClosePane: vi.fn(),
    onFocusPane: vi.fn(),
    onNewTab: vi.fn(),
    onSwitchTab: vi.fn(),
  };

  beforeEach(() => {
    Object.values(handlers).forEach((h) => h.mockReset());
    bindings = ref({});
    scope = effectScope();
    scope.run(() => useTerminalShortcuts(handlers, { mod: "Control", bindings }));
  });

  afterEach(() => {
    scope.stop();
  });

  it("Ctrl+N -> onSplitVertical('new')", () => {
    fireKey({ key: "n", code: "KeyN", ctrlKey: true });
    expect(handlers.onSplitVertical).toHaveBeenCalledWith("new");
  });

  it("Ctrl+Shift+N -> onSplitHorizontal('new')", () => {
    fireKey({ key: "N", code: "KeyN", ctrlKey: true, shiftKey: true });
    expect(handlers.onSplitHorizontal).toHaveBeenCalledWith("new");
  });

  it("Ctrl+Alt+N -> onSplitVertical('pick')", () => {
    fireKey({ key: "n", code: "KeyN", ctrlKey: true, altKey: true });
    expect(handlers.onSplitVertical).toHaveBeenCalledWith("pick");
  });

  it("Ctrl+Alt+Shift+N -> onSplitHorizontal('pick')", () => {
    fireKey({ key: "N", code: "KeyN", ctrlKey: true, altKey: true, shiftKey: true });
    expect(handlers.onSplitHorizontal).toHaveBeenCalledWith("pick");
  });

  it("mac dead key key='˜' but code='KeyN' still routes via code", () => {
    fireKey({ key: "˜", code: "KeyN", ctrlKey: true, altKey: true });
    expect(handlers.onSplitVertical).toHaveBeenCalledWith("pick");
  });

  it("Ctrl+W -> onClosePane", () => {
    fireKey({ key: "w", code: "KeyW", ctrlKey: true });
    expect(handlers.onClosePane).toHaveBeenCalled();
  });

  it("Ctrl+T -> onNewTab", () => {
    fireKey({ key: "t", code: "KeyT", ctrlKey: true });
    expect(handlers.onNewTab).toHaveBeenCalled();
  });

  it("Ctrl+Alt+ArrowLeft -> onFocusPane('left')", () => {
    fireKey({ key: "ArrowLeft", code: "ArrowLeft", ctrlKey: true, altKey: true });
    expect(handlers.onFocusPane).toHaveBeenCalledWith("left");
  });

  it("Ctrl+Alt+ArrowRight -> onFocusPane('right')", () => {
    fireKey({ key: "ArrowRight", code: "ArrowRight", ctrlKey: true, altKey: true });
    expect(handlers.onFocusPane).toHaveBeenCalledWith("right");
  });

  it("Ctrl+Shift+] (key='}') -> onSwitchTab(+1)", () => {
    fireKey({ key: "}", code: "BracketRight", ctrlKey: true, shiftKey: true });
    expect(handlers.onSwitchTab).toHaveBeenCalledWith(1);
  });

  it("Ctrl+Shift+[ (key='{') -> onSwitchTab(-1)", () => {
    fireKey({ key: "{", code: "BracketLeft", ctrlKey: true, shiftKey: true });
    expect(handlers.onSwitchTab).toHaveBeenCalledWith(-1);
  });

  it("plain N (no modifier) is ignored", () => {
    fireKey({ key: "n", code: "KeyN" });
    expect(handlers.onSplitVertical).not.toHaveBeenCalled();
  });

  it("Ctrl+N preventDefault + stopPropagation", () => {
    const ev = fireKey({ key: "n", code: "KeyN", ctrlKey: true });
    expect(ev.defaultPrevented).toBe(true);
  });

  it("scope.stop unbinds the listener", () => {
    scope.stop();
    fireKey({ key: "n", code: "KeyN", ctrlKey: true });
    expect(handlers.onSplitVertical).not.toHaveBeenCalled();
  });
});

describe("useTerminalShortcuts (user overrides)", () => {
  let scope: ReturnType<typeof effectScope>;
  let bindings: Ref<Record<string, string>>;
  const handlers = {
    onSplitVertical: vi.fn(),
    onSplitHorizontal: vi.fn(),
    onClosePane: vi.fn(),
    onFocusPane: vi.fn(),
    onNewTab: vi.fn(),
    onSwitchTab: vi.fn(),
  };

  beforeEach(() => {
    Object.values(handlers).forEach((h) => h.mockReset());
  });

  afterEach(() => {
    scope.stop();
  });

  it("remapped binding routes to the new key, the old key no longer fires", () => {
    bindings = ref({ "pane.split-vertical-new": "Mod+KeyJ" });
    scope = effectScope();
    scope.run(() => useTerminalShortcuts(handlers, { mod: "Control", bindings }));
    fireKey({ key: "j", code: "KeyJ", ctrlKey: true });
    expect(handlers.onSplitVertical).toHaveBeenCalledWith("new");
    fireKey({ key: "n", code: "KeyN", ctrlKey: true });
    expect(handlers.onSplitVertical).toHaveBeenCalledTimes(1); // not called again
  });

  it("empty binding disables the action", () => {
    bindings = ref({ "pane.close": "" });
    scope = effectScope();
    scope.run(() => useTerminalShortcuts(handlers, { mod: "Control", bindings }));
    fireKey({ key: "w", code: "KeyW", ctrlKey: true });
    expect(handlers.onClosePane).not.toHaveBeenCalled();
  });

  it("updating bindings.value live changes routing without re-mounting", () => {
    bindings = ref({});
    scope = effectScope();
    scope.run(() => useTerminalShortcuts(handlers, { mod: "Control", bindings }));
    fireKey({ key: "t", code: "KeyT", ctrlKey: true });
    expect(handlers.onNewTab).toHaveBeenCalledTimes(1);
    bindings.value = { "tab.new": "Mod+KeyP" };
    fireKey({ key: "t", code: "KeyT", ctrlKey: true });
    expect(handlers.onNewTab).toHaveBeenCalledTimes(1); // old key no longer fires
    fireKey({ key: "p", code: "KeyP", ctrlKey: true });
    expect(handlers.onNewTab).toHaveBeenCalledTimes(2); // new key fires
  });
});
```

- [ ] **Step 7.2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/composables/useTerminalShortcuts.test.ts`

Expected: tests fail because `useTerminalShortcuts` doesn't yet accept a `bindings` ref.

- [ ] **Step 7.3: Replace the composable**

Replace the entire contents of `desktop/frontend/src/composables/useTerminalShortcuts.ts` with:

```ts
// Document-level capture-phase keydown router. Listens before xterm.js so
// Mod-combos we care about never reach the terminal.
//
// The composable accepts an optional `bindings` ref — a sparse map of
// actionId -> binding string (see lib/shortcutBindings.ts). Defaults from
// the action registry apply when the ref is omitted or an action is absent.

import { computed, onScopeDispose, type Ref } from "vue";
import type { FocusDir } from "../lib/types";
import { buildRoutingTable, serialize, type Mod } from "../lib/shortcutBindings";

export type SplitMode = "new" | "pick";

export interface ShortcutHandlers {
  onSplitVertical: (mode: SplitMode) => void;
  onSplitHorizontal: (mode: SplitMode) => void;
  onClosePane: () => void;
  onFocusPane: (dir: FocusDir) => void;
  onNewTab: () => void;
  onSwitchTab: (delta: number) => void;
}

export interface ShortcutOptions {
  // Override the modifier-key detection. Default: "Meta" on Mac, "Control"
  // elsewhere. Tests use this to force "Control" for portability.
  mod?: Mod;
  // Optional reactive bindings. Defaults are used for any unset action.
  bindings?: Ref<Record<string, string>>;
}

function detectMod(): Mod {
  if (typeof navigator === "undefined") return "Control";
  return navigator.platform?.toLowerCase().includes("mac") ? "Meta" : "Control";
}

function dispatch(actionId: string, h: ShortcutHandlers): boolean {
  switch (actionId) {
    case "pane.split-vertical-new":    h.onSplitVertical("new"); return true;
    case "pane.split-vertical-pick":   h.onSplitVertical("pick"); return true;
    case "pane.split-horizontal-new":  h.onSplitHorizontal("new"); return true;
    case "pane.split-horizontal-pick": h.onSplitHorizontal("pick"); return true;
    case "pane.close":                 h.onClosePane(); return true;
    case "pane.focus-left":            h.onFocusPane("left"); return true;
    case "pane.focus-right":           h.onFocusPane("right"); return true;
    case "pane.focus-up":              h.onFocusPane("up"); return true;
    case "pane.focus-down":            h.onFocusPane("down"); return true;
    case "tab.new":                    h.onNewTab(); return true;
    case "tab.prev":                   h.onSwitchTab(-1); return true;
    case "tab.next":                   h.onSwitchTab(1); return true;
  }
  return false;
}

export function useTerminalShortcuts(
  handlers: ShortcutHandlers,
  opts: ShortcutOptions = {},
): void {
  const mod = opts.mod ?? detectMod();

  const route = computed(() => {
    const overrides = opts.bindings?.value ?? {};
    return buildRoutingTable(overrides);
  });

  function handler(e: KeyboardEvent) {
    const key = serialize(e, mod);
    if (key === null) return;
    const actionId = route.value[key];
    if (!actionId) return;
    if (!dispatch(actionId, handlers)) return;
    e.preventDefault();
    e.stopPropagation();
  }

  document.addEventListener("keydown", handler, { capture: true });
  onScopeDispose(() => {
    document.removeEventListener("keydown", handler, { capture: true } as EventListenerOptions);
  });
}
```

- [ ] **Step 7.4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/composables/useTerminalShortcuts.test.ts`

Expected: all pass (including the new override / live-update suites).

- [ ] **Step 7.5: Type-check the whole frontend**

Run: `cd desktop/frontend && npm run build`

Expected: success. `App.vue` still compiles because it calls `useTerminalShortcuts(handlers)` with no second arg — that path remains valid (bindings default to empty → defaults are used).

- [ ] **Step 7.6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/composables/useTerminalShortcuts.ts desktop/frontend/src/composables/useTerminalShortcuts.test.ts
git commit -m "feat(desktop): drive terminal shortcuts from injected bindings ref"
```

---

## Task 8: wire `pluginConfigStore.cfg.shortcuts.bindings` into `App.vue`

**Files:**
- Modify: `desktop/frontend/src/App.vue`

- [ ] **Step 8.1: Find the current `useTerminalShortcuts` call**

Run: `grep -n "useTerminalShortcuts\|import.*pluginConfigStore\|usePluginConfigStore" /Users/attson/code/github.com.attson/atterm/desktop/frontend/src/App.vue`

This identifies the call site (around line 659–666) and whether the store is already imported.

- [ ] **Step 8.2: Ensure the store is imported and loaded**

If `usePluginConfigStore` is not already imported in `App.vue`, add this near the other imports:

```ts
import { computed } from "vue";
import { usePluginConfigStore } from "./plugins/configStore";
```

(Skip the `computed` import line if it is already present.)

In the `<script setup>` body, near other store setup, ensure:

```ts
const pluginConfigStore = usePluginConfigStore();
```

(Skip if a `pluginConfigStore` const already exists — App.vue already calls `store.load()` somewhere; reuse the existing binding name if so. If it uses a different name like `cfgStore`, use that consistently in the next step.)

- [ ] **Step 8.3: Build the bindings ref and pass it to the composable**

Just before the existing `useTerminalShortcuts({ ... })` call, add:

```ts
const shortcutBindings = computed<Record<string, string>>(() => {
  return pluginConfigStore.cfg?.shortcuts?.bindings ?? {};
});
```

Then update the `useTerminalShortcuts({...})` call (currently lines 659–666) to add the `bindings` option as a second argument. Find:

```ts
useTerminalShortcuts({
  onSplitVertical: (mode) => onSplit("vertical", mode),
  onSplitHorizontal: (mode) => onSplit("horizontal", mode),
  onClosePane,
  onFocusPane,
  onNewTab: startNewTab,
  onSwitchTab,
});
```

Replace with:

```ts
useTerminalShortcuts(
  {
    onSplitVertical: (mode) => onSplit("vertical", mode),
    onSplitHorizontal: (mode) => onSplit("horizontal", mode),
    onClosePane,
    onFocusPane,
    onNewTab: startNewTab,
    onSwitchTab,
  },
  { bindings: shortcutBindings },
);
```

`computed` returns a `ComputedRef<T>`, which is assignable to `Ref<T>` for read-only consumption in `useTerminalShortcuts`.

- [ ] **Step 8.4: Type-check**

Run: `cd desktop/frontend && npm run build`

Expected: build succeeds.

- [ ] **Step 8.5: Run the full frontend test suite**

Run: `cd desktop/frontend && npm run test`

Expected: all green.

- [ ] **Step 8.6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/App.vue
git commit -m "feat(desktop): bridge plugin config shortcuts into terminal router"
```

---

## Task 9: `HotkeyCaptureCell.vue` — single-cell capture component

**Files:**
- Create: `desktop/frontend/src/components/HotkeyCaptureCell.vue`
- Create: `desktop/frontend/src/components/HotkeyCaptureCell.test.ts`

- [ ] **Step 9.1: Write the failing tests**

Create `desktop/frontend/src/components/HotkeyCaptureCell.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import HotkeyCaptureCell from "./HotkeyCaptureCell.vue";

function makeKey(opts: KeyboardEventInit & { key: string; code: string }) {
  return new KeyboardEvent("keydown", { ...opts, bubbles: true, cancelable: true });
}

describe("HotkeyCaptureCell", () => {
  it("renders the current binding (with default mod injected)", () => {
    const wrapper = mount(HotkeyCaptureCell, {
      props: { value: "Mod+KeyN", mod: "Control" },
    });
    expect(wrapper.text()).toContain("Mod+KeyN");
  });

  it("displays placeholder text for empty binding", () => {
    const wrapper = mount(HotkeyCaptureCell, {
      props: { value: "", mod: "Control" },
    });
    expect(wrapper.text().toLowerCase()).toContain("disabled");
  });

  it("clicking enters capturing state", async () => {
    const wrapper = mount(HotkeyCaptureCell, {
      props: { value: "Mod+KeyN", mod: "Control" },
    });
    await wrapper.find(".hotkey-cell").trigger("click");
    expect(wrapper.find(".hotkey-cell").classes()).toContain("capturing");
    expect(wrapper.text().toLowerCase()).toContain("press");
  });

  it("captures Ctrl+Shift+T and emits update", async () => {
    const wrapper = mount(HotkeyCaptureCell, {
      props: { value: "Mod+KeyN", mod: "Control" },
      attachTo: document.body,
    });
    await wrapper.find(".hotkey-cell").trigger("click");
    document.dispatchEvent(makeKey({ key: "T", code: "KeyT", ctrlKey: true, shiftKey: true }));
    await flushPromises();
    expect(wrapper.emitted("update")).toBeTruthy();
    expect(wrapper.emitted("update")![0]).toEqual(["Mod+Shift+KeyT"]);
    // Exits capturing
    expect(wrapper.find(".hotkey-cell").classes()).not.toContain("capturing");
    wrapper.unmount();
  });

  it("Esc cancels capture without emitting update", async () => {
    const wrapper = mount(HotkeyCaptureCell, {
      props: { value: "Mod+KeyN", mod: "Control" },
      attachTo: document.body,
    });
    await wrapper.find(".hotkey-cell").trigger("click");
    document.dispatchEvent(makeKey({ key: "Escape", code: "Escape" }));
    await flushPromises();
    expect(wrapper.emitted("update")).toBeFalsy();
    expect(wrapper.emitted("cancel")).toBeTruthy();
    expect(wrapper.find(".hotkey-cell").classes()).not.toContain("capturing");
    wrapper.unmount();
  });

  it("Backspace emits update with empty string (disables)", async () => {
    const wrapper = mount(HotkeyCaptureCell, {
      props: { value: "Mod+KeyN", mod: "Control" },
      attachTo: document.body,
    });
    await wrapper.find(".hotkey-cell").trigger("click");
    document.dispatchEvent(makeKey({ key: "Backspace", code: "Backspace" }));
    await flushPromises();
    expect(wrapper.emitted("update")![0]).toEqual([""]);
    wrapper.unmount();
  });

  it("modifier-only press does not emit", async () => {
    const wrapper = mount(HotkeyCaptureCell, {
      props: { value: "Mod+KeyN", mod: "Control" },
      attachTo: document.body,
    });
    await wrapper.find(".hotkey-cell").trigger("click");
    document.dispatchEvent(makeKey({ key: "Control", code: "ControlLeft", ctrlKey: true }));
    await flushPromises();
    expect(wrapper.emitted("update")).toBeFalsy();
    expect(wrapper.find(".hotkey-cell").classes()).toContain("capturing");
    wrapper.unmount();
  });

  it("bare letter without modifier does not emit", async () => {
    const wrapper = mount(HotkeyCaptureCell, {
      props: { value: "Mod+KeyN", mod: "Control" },
      attachTo: document.body,
    });
    await wrapper.find(".hotkey-cell").trigger("click");
    document.dispatchEvent(makeKey({ key: "t", code: "KeyT" }));
    await flushPromises();
    expect(wrapper.emitted("update")).toBeFalsy();
    wrapper.unmount();
  });
});
```

- [ ] **Step 9.2: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/components/HotkeyCaptureCell.test.ts`

Expected: failing — module not found.

- [ ] **Step 9.3: Create the component**

Create `desktop/frontend/src/components/HotkeyCaptureCell.vue`:

```vue
<script lang="ts" setup>
import { onBeforeUnmount, ref } from "vue";
import { serialize, type Mod } from "../lib/shortcutBindings";

const props = defineProps<{
  value: string;
  mod: Mod;
}>();

const emit = defineEmits<{
  (e: "update", value: string): void;
  (e: "cancel"): void;
}>();

const capturing = ref(false);
let listener: ((e: KeyboardEvent) => void) | null = null;

function startCapture() {
  if (capturing.value) return;
  capturing.value = true;
  listener = (e: KeyboardEvent) => {
    // Eat navigation/confirm keys while capturing so the surrounding dialog
    // doesn't act on them.
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      stopCapture();
      emit("cancel");
      return;
    }
    if (e.key === "Backspace") {
      e.preventDefault();
      e.stopPropagation();
      stopCapture();
      emit("update", "");
      return;
    }
    if (e.key === "Tab" || e.key === "Enter") {
      e.preventDefault();
      e.stopPropagation();
      return;
    }
    const binding = serialize(e, props.mod);
    if (binding === null) return; // modifier-only, bare letter, or non-whitelisted code
    e.preventDefault();
    e.stopPropagation();
    stopCapture();
    emit("update", binding);
  };
  document.addEventListener("keydown", listener, { capture: true });
}

function stopCapture() {
  capturing.value = false;
  if (listener) {
    document.removeEventListener("keydown", listener, { capture: true } as EventListenerOptions);
    listener = null;
  }
}

onBeforeUnmount(stopCapture);
</script>

<template>
  <button
    type="button"
    class="hotkey-cell"
    :class="{ capturing, empty: !capturing && value === '' }"
    @click="startCapture"
  >
    <template v-if="capturing">Press a key…</template>
    <template v-else-if="value === ''">disabled</template>
    <template v-else>{{ value }}</template>
  </button>
</template>

<style scoped>
.hotkey-cell {
  font-family: "SF Mono", Menlo, monospace;
  font-size: 12px;
  background: #0d1117;
  border: 1px solid #2d333b;
  color: #c9d1d9;
  padding: 3px 8px;
  border-radius: 4px;
  cursor: pointer;
  min-width: 160px;
  text-align: left;
}
.hotkey-cell:hover {
  background: #161b22;
}
.hotkey-cell.capturing {
  background: rgba(88, 166, 255, 0.12);
  border-color: var(--accent);
  color: var(--accent);
}
.hotkey-cell.empty {
  color: #6e7681;
  font-style: italic;
}
</style>
```

- [ ] **Step 9.4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/components/HotkeyCaptureCell.test.ts`

Expected: all pass.

- [ ] **Step 9.5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/HotkeyCaptureCell.vue desktop/frontend/src/components/HotkeyCaptureCell.test.ts
git commit -m "feat(desktop): hotkey capture cell component"
```

---

## Task 10: `SettingsShortcuts.vue` — the new tab

**Files:**
- Create: `desktop/frontend/src/components/SettingsShortcuts.vue`
- Create: `desktop/frontend/src/components/SettingsShortcuts.test.ts`

- [ ] **Step 10.1: Write the failing tests**

Create `desktop/frontend/src/components/SettingsShortcuts.test.ts`:

```ts
import { describe, it, expect, beforeEach, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";
import SettingsShortcuts from "./SettingsShortcuts.vue";
import { usePluginConfigStore } from "../plugins/configStore";

// Stub away the Wails layer so the store's load/save can be invoked without
// touching a real backend.
vi.mock("../../wailsjs/go/main/App", () => ({
  GetPluginConfig: vi.fn(async () => ({
    quickInput: { enabled: true, buttons: [] },
    fileExplorer: { enabled: false, panelWidthPx: 380, panelCollapsed: false, innerTreeRatio: 0.3, showHidden: false, showLineNumbers: false },
    translate: { enabled: false, provider: "openai-compatible", baseUrl: "", apiKey: "", model: "gpt-4o-mini", defaultTargetLang: "zh-CN" },
    shortcuts: { bindings: {} },
  })),
  SetPluginConfig: vi.fn(async () => {}),
}));

vi.mock("../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => () => {}),
}));

async function setupStore(initial: Record<string, string>) {
  setActivePinia(createPinia());
  const store = usePluginConfigStore();
  await store.load();
  store.cfg!.shortcuts = { bindings: initial };
  return store;
}

describe("SettingsShortcuts", () => {
  it("renders 12 rows grouped under 'pane' and 'tab'", async () => {
    await setupStore({});
    const wrapper = mount(SettingsShortcuts, { props: { mod: "Control" } });
    await flushPromises();
    const rows = wrapper.findAll(".shortcut-row");
    expect(rows).toHaveLength(12);
    expect(wrapper.text()).toContain("Pane");
    expect(wrapper.text()).toContain("Tab");
  });

  it("each row's hotkey cell shows the current binding (default when not overridden)", async () => {
    await setupStore({});
    const wrapper = mount(SettingsShortcuts, { props: { mod: "Control" } });
    await flushPromises();
    expect(wrapper.text()).toContain("Mod+KeyN");
    expect(wrapper.text()).toContain("Mod+KeyT");
  });

  it("editing a row to collide with another shows a conflict notice and disables Save", async () => {
    await setupStore({});
    const wrapper = mount(SettingsShortcuts, { props: { mod: "Control" } });
    await flushPromises();
    // Simulate emitting update on the first row's HotkeyCaptureCell — easier
    // to drive the data flow than to fake a real capture.
    const cells = wrapper.findAllComponents({ name: "HotkeyCaptureCell" });
    await cells[0].vm.$emit("update", "Mod+KeyT"); // collides with tab.new (default)
    await flushPromises();
    expect(wrapper.text().toLowerCase()).toContain("conflicts with");
    const saveBtn = wrapper.find("button.save");
    expect((saveBtn.element as HTMLButtonElement).disabled).toBe(true);
  });

  it("Reset all marks draft empty (defaults shown) and sets dirty", async () => {
    const store = await setupStore({ "pane.close": "Mod+KeyL" });
    const wrapper = mount(SettingsShortcuts, { props: { mod: "Control" } });
    await flushPromises();
    // Before reset, pane.close shows the override
    expect(wrapper.text()).toContain("Mod+KeyL");
    await wrapper.find("button.reset-all").trigger("click");
    await flushPromises();
    // Now pane.close shows the default
    expect(wrapper.text()).toContain("Mod+KeyW");
    const saveBtn = wrapper.find("button.save");
    expect((saveBtn.element as HTMLButtonElement).disabled).toBe(false);
    void store;
  });

  it("Save calls store.save with only the modified entries (defaults stripped)", async () => {
    const store = await setupStore({});
    const saveSpy = vi.spyOn(store, "save").mockResolvedValue();
    const wrapper = mount(SettingsShortcuts, { props: { mod: "Control" } });
    await flushPromises();
    const cells = wrapper.findAllComponents({ name: "HotkeyCaptureCell" });
    // Index 9 is tab.new (after pane.* 0..8). Change it to a non-default.
    await cells[9].vm.$emit("update", "Mod+KeyP");
    await flushPromises();
    await wrapper.find("button.save").trigger("click");
    await flushPromises();
    expect(saveSpy).toHaveBeenCalledTimes(1);
    const payload = saveSpy.mock.calls[0][0];
    expect(payload.shortcuts.bindings).toEqual({ "tab.new": "Mod+KeyP" });
  });

  it("Discard reverts draft to current store value, clears dirty", async () => {
    await setupStore({ "tab.new": "Mod+KeyP" });
    const wrapper = mount(SettingsShortcuts, { props: { mod: "Control" } });
    await flushPromises();
    const cells = wrapper.findAllComponents({ name: "HotkeyCaptureCell" });
    await cells[9].vm.$emit("update", "Mod+KeyQ");
    await flushPromises();
    expect(wrapper.text()).toContain("Mod+KeyQ");
    await wrapper.find("button.discard").trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("Mod+KeyP");
    expect(wrapper.text()).not.toContain("Mod+KeyQ");
  });
});
```

- [ ] **Step 10.2: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/components/SettingsShortcuts.test.ts`

Expected: failing — module not found.

- [ ] **Step 10.3: Create the component**

Create `desktop/frontend/src/components/SettingsShortcuts.vue`:

```vue
<script lang="ts" setup>
import { computed, onMounted, ref, watch } from "vue";
import { usePluginConfigStore } from "../plugins/configStore";
import {
  ACTIONS,
  ACTION_BY_ID,
  conflictsWith,
  resolvedBindings,
  type Mod,
  type ShortcutAction,
} from "../lib/shortcutBindings";
import HotkeyCaptureCell from "./HotkeyCaptureCell.vue";

function detectMod(): Mod {
  if (typeof navigator === "undefined") return "Control";
  return navigator.platform?.toLowerCase().includes("mac") ? "Meta" : "Control";
}

const props = defineProps<{
  // Allow tests to inject Control; production uses detectMod().
  mod?: Mod;
}>();

const mod = computed<Mod>(() => props.mod ?? detectMod());

const store = usePluginConfigStore();
const draft = ref<Record<string, string>>({});

function loadDraft() {
  draft.value = JSON.parse(JSON.stringify(store.cfg?.shortcuts?.bindings ?? {}));
}

onMounted(async () => {
  if (!store.cfg) await store.load();
  loadDraft();
});

watch(
  () => store.cfg?.shortcuts?.bindings,
  () => { if (!dirty.value) loadDraft(); },
  { deep: true },
);

const dirty = computed(() => {
  const cur = store.cfg?.shortcuts?.bindings ?? {};
  return JSON.stringify(cur) !== JSON.stringify(draft.value);
});

// Fully-resolved bindings (defaults + draft overrides) for display and
// conflict detection.
const resolved = computed(() => resolvedBindings(draft.value));

function bindingFor(action: ShortcutAction): string {
  return resolved.value[action.id] ?? action.defaultBinding;
}

function conflictsFor(action: ShortcutAction): string[] {
  return conflictsWith(resolved.value, action.id);
}

const anyConflict = computed(() =>
  ACTIONS.some((a) => conflictsFor(a).length > 0),
);

function onCellUpdate(action: ShortcutAction, value: string) {
  if (value === action.defaultBinding) {
    // Equal to default — remove the override so the entry stays clean.
    const next = { ...draft.value };
    delete next[action.id];
    draft.value = next;
    return;
  }
  draft.value = { ...draft.value, [action.id]: value };
}

function resetRow(action: ShortcutAction) {
  const next = { ...draft.value };
  delete next[action.id];
  draft.value = next;
}

function resetAll() {
  draft.value = {};
}

async function save() {
  if (!store.cfg) return;
  const next = JSON.parse(JSON.stringify(store.cfg));
  // Strip entries equal to defaults (defensive — already handled in onCellUpdate
  // but a user could have reached this state via resetRow + manual save flow).
  const normalized: Record<string, string> = {};
  for (const [id, value] of Object.entries(draft.value)) {
    if (id in ACTION_BY_ID && value !== ACTION_BY_ID[id]!.defaultBinding) {
      normalized[id] = value;
    }
  }
  next.shortcuts.bindings = normalized;
  await store.save(next);
}

function discard() {
  loadDraft();
}

const paneActions = ACTIONS.filter((a) => a.group === "pane");
const tabActions = ACTIONS.filter((a) => a.group === "tab");

defineExpose({ dirty });
</script>

<template>
  <div class="shortcut-settings">
    <section class="shortcut-group">
      <h3>Pane</h3>
      <div v-for="action in paneActions" :key="action.id" class="shortcut-row">
        <div class="label">{{ action.label }}</div>
        <HotkeyCaptureCell
          :value="bindingFor(action)"
          :mod="mod"
          @update="(v) => onCellUpdate(action, v)"
        />
        <button class="reset-row" :title="'Reset to ' + action.defaultBinding" @click="resetRow(action)">↺</button>
        <div class="conflict" v-if="conflictsFor(action).length">
          Conflicts with: {{ conflictsFor(action).map((id) => ACTION_BY_ID[id]?.label ?? id).join(", ") }}
        </div>
      </div>
    </section>

    <section class="shortcut-group">
      <h3>Tab</h3>
      <div v-for="action in tabActions" :key="action.id" class="shortcut-row">
        <div class="label">{{ action.label }}</div>
        <HotkeyCaptureCell
          :value="bindingFor(action)"
          :mod="mod"
          @update="(v) => onCellUpdate(action, v)"
        />
        <button class="reset-row" :title="'Reset to ' + action.defaultBinding" @click="resetRow(action)">↺</button>
        <div class="conflict" v-if="conflictsFor(action).length">
          Conflicts with: {{ conflictsFor(action).map((id) => ACTION_BY_ID[id]?.label ?? id).join(", ") }}
        </div>
      </div>
    </section>

    <div class="actions-row">
      <button class="reset-all" @click="resetAll">Reset all to defaults</button>
      <div class="spacer" />
      <button class="discard" :disabled="!dirty" @click="discard">Discard</button>
      <button class="save" :disabled="!dirty || anyConflict" @click="save">Save</button>
    </div>
  </div>
</template>

<style scoped>
.shortcut-settings { padding: 8px 4px; font-size: 12px; color: var(--fg); }
.shortcut-group { margin-bottom: 18px; }
.shortcut-group h3 {
  margin: 4px 0 8px;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--fg-dim);
}
.shortcut-row {
  display: grid;
  grid-template-columns: 1fr auto auto;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
}
.label { color: var(--fg); }
.reset-row {
  background: transparent;
  border: 1px solid #2d333b;
  color: var(--fg-dim);
  padding: 2px 8px;
  border-radius: 4px;
  cursor: pointer;
}
.reset-row:hover { background: rgba(255, 255, 255, 0.04); color: var(--fg); }
.conflict {
  grid-column: 1 / -1;
  color: #f85149;
  font-size: 11px;
  padding-left: 4px;
}
.actions-row {
  display: flex;
  gap: 8px;
  margin-top: 12px;
  align-items: center;
}
.spacer { flex: 1; }
.actions-row button {
  background: #21262d;
  border: 1px solid #2d333b;
  color: #c9d1d9;
  padding: 4px 12px;
  border-radius: 4px;
  cursor: pointer;
}
.actions-row button:disabled { opacity: 0.4; cursor: default; }
.actions-row .save { background: var(--accent); color: #0d1117; border-color: var(--accent); }
.actions-row .save:disabled { background: #21262d; color: #c9d1d9; border-color: #2d333b; }
</style>
```

- [ ] **Step 10.4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/components/SettingsShortcuts.test.ts`

Expected: all pass.

- [ ] **Step 10.5: Type-check the frontend**

Run: `cd desktop/frontend && npm run build`

Expected: build succeeds.

- [ ] **Step 10.6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/SettingsShortcuts.vue desktop/frontend/src/components/SettingsShortcuts.test.ts
git commit -m "feat(desktop): shortcut settings tab"
```

---

## Task 11: wire `SettingsShortcuts` into `SettingsDialog`

**Files:**
- Modify: `desktop/frontend/src/components/SettingsDialog.vue`
- Modify: `desktop/frontend/src/components/SettingsDialog.test.ts`

- [ ] **Step 11.1: Extend the source-grep tests**

Append to `desktop/frontend/src/components/SettingsDialog.test.ts` (inside the `describe("SettingsDialog shell", () => { ... })` block, add additional `test` blocks):

```ts
  test("imports the SettingsShortcuts subcomponent", () => {
    expect(source).toContain('import SettingsShortcuts from "./SettingsShortcuts.vue"');
  });

  test("renders the Shortcuts label in the sidebar", () => {
    expect(source).toContain(">Shortcuts<");
  });

  test("activeTab union includes 'shortcuts'", () => {
    expect(source).toMatch(/activeTab[\s\S]*?["']shortcuts["']/);
  });

  test("clicking the Shortcuts nav switches to that tab", () => {
    expect(source).toContain("@click=\"switchTab('shortcuts')\"");
  });
```

Also update the existing tab-union test (currently around line 28) to include `shortcuts`. Replace:

```ts
    expect(source).toMatch(/activeTab\s*=\s*ref<["']general["']\s*\|\s*["']relay["']\s*\|\s*["']logging["']\s*\|\s*["']updates["']\s*\|\s*["']plugins["']/);
```

with:

```ts
    expect(source).toMatch(/activeTab\s*=\s*ref<["']general["']\s*\|\s*["']relay["']\s*\|\s*["']logging["']\s*\|\s*["']updates["']\s*\|\s*["']plugins["']\s*\|\s*["']shortcuts["']/);
```

- [ ] **Step 11.2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/components/SettingsDialog.test.ts`

Expected: the new tests fail.

- [ ] **Step 11.3: Update `SettingsDialog.vue`**

Edit `desktop/frontend/src/components/SettingsDialog.vue`.

(A) Imports — find line 14 (the last `Settings*` import) and add below it:

```ts
import SettingsShortcuts from "./SettingsShortcuts.vue";
```

(B) `initialTab` prop union (line 22) — update to:

```ts
  initialTab?: "general" | "relay" | "logging" | "updates" | "shortcuts";
```

(C) `activeTab` ref and `pendingTab` ref unions (lines 32 and 37) — update each to include `"shortcuts"`:

```ts
const activeTab = ref<"general" | "relay" | "logging" | "updates" | "plugins" | "shortcuts">(props.initialTab ?? "general");
```

```ts
const pendingTab = ref<"general" | "relay" | "logging" | "updates" | "plugins" | "shortcuts" | null>(null);
```

(D) `switchTab` parameter type (line 57) — update to:

```ts
function switchTab(next: "general" | "relay" | "logging" | "updates" | "plugins" | "shortcuts") {
```

(E) Sidebar — find the `<aside class="settings-nav">` block (lines 149–175) and add a new nav button before the `</aside>`:

```html
          <button
            class="settings-nav-item"
            :class="{ active: activeTab === 'shortcuts' }"
            @click="switchTab('shortcuts')"
          >Shortcuts</button>
```

(F) Pane section — find the `<section class="settings-pane">` block (lines 177–199) and add the new pane before `</section>`:

```html
          <SettingsShortcuts v-show="activeTab === 'shortcuts'" />
```

- [ ] **Step 11.4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/components/SettingsDialog.test.ts`

Expected: all pass.

- [ ] **Step 11.5: Type-check the frontend**

Run: `cd desktop/frontend && npm run build`

Expected: build succeeds.

- [ ] **Step 11.6: Full test sweep**

Run: `cd desktop/frontend && npm run test`

Expected: every frontend test passes.

Run: `cd desktop && go test ./...`

Expected: every backend test passes.

- [ ] **Step 11.7: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/SettingsDialog.vue desktop/frontend/src/components/SettingsDialog.test.ts
git commit -m "feat(desktop): mount shortcut settings tab in settings dialog"
```

---

## Task 12: end-to-end manual verification

**Files:** none (manual smoke test in the running desktop app)

- [ ] **Step 12.1: Launch the desktop app**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop && wails dev`

Wait for the window to appear and a session to load.

- [ ] **Step 12.2: Verify default shortcuts still work**

In the running app:
- `⌘N` (mac) or `Ctrl+N` (other) → opens a vertical split (new pane).
- `⌘W` / `Ctrl+W` → closes the current pane.
- `⌘T` / `Ctrl+T` → opens a new tab.
- `⌘Shift+]` / `Ctrl+Shift+]` → moves to the next tab.

Expected: all behaviors match what they did before this PR.

- [ ] **Step 12.3: Open Settings → Shortcuts**

Open the settings dialog, click the new `Shortcuts` sidebar item.

Expected: 12 rows visible across two groups (Pane / Tab), each row showing its current binding.

- [ ] **Step 12.4: Rebind one action and save**

Click the hotkey cell for "New tab", press `⌘L` (mac) or `Ctrl+L` (other), confirm cell shows the new binding, click Save.

Expected:
- Save succeeds without error.
- `⌘T` no longer opens a new tab.
- `⌘L` now opens a new tab.

- [ ] **Step 12.5: Disable an action**

Click the hotkey cell for "Close pane", press Backspace, confirm cell now reads "disabled" in italic. Click Save.

Expected: `⌘W` no longer closes the current pane.

- [ ] **Step 12.6: Trigger a conflict**

Click the hotkey cell for "New tab" again, press `⌘W`. The cell should show `Mod+KeyW`. A "Conflicts with: Close pane" notice should appear below the row, and Save should be disabled.

Click Discard. Conflict notice clears; Save and Discard become disabled again.

- [ ] **Step 12.7: Reset all**

Click "Reset all to defaults". All rows show their original bindings; Save is enabled. Click Save.

Expected: `⌘W` works again (close pane); `⌘T` opens a new tab.

- [ ] **Step 12.8: Persistence**

Quit and reopen the app. Open Settings → Shortcuts. Bindings should reflect the last saved state (defaults if Step 12.7 was completed).

- [ ] **Step 12.9: Commit notes if any small fixes were required**

If Steps 12.1–12.8 surfaced issues, make targeted fixes and commit with descriptive messages. If everything passed clean, no commit is needed for this task.

---

## Closing

After Task 12 passes, the feature is ready. The branch can be pushed and a PR opened (use the `ship-release` skill for that flow).
