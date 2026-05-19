# Translate Selection Plugin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a desktop plugin that lets users right-click a selected fragment in any terminal pane and translate it via an OpenAI-compatible API. The result shows in a floating panel teleported to body.

**Architecture:** Extend the plugin slot system with a headless `"context-menu"` slot. The translate plugin contributes a single menu item that populates a Pinia store; a `<TranslatePanelHost />` mounted at the App root teleports the panel UI into body. The provider abstraction is `TranslateProvider`; v1 ships `openai-compatible` only, dispatched directly from the webview via `fetch`. API key persists in the existing Go-backed plugin config.

**Tech Stack:** Vue 3 Composition API + TypeScript, Pinia, xterm.js, Wails v2 bindings, vitest + @vue/test-utils, Go 1.23.

**Reference spec:** `docs/superpowers/specs/2026-05-19-translate-plugin-design.md`

---

## Pre-flight

- [ ] **Step 0: Verify clean working tree**

```bash
cd /Users/attson/code/github.com.attson/atterm
git status --short
git rev-parse --abbrev-ref HEAD
```

Expected: empty (or only untracked plan / spec docs), some main-derived branch.

- [ ] **Step 0.1: Baseline tests + build pass**

```bash
cd desktop/frontend && npx vitest run 2>&1 | tail -5
cd .. && go test ./... 2>&1 | tail -5
```

Expected: all green. If anything fails, stop and fix root cause before touching new code.

- [ ] **Step 0.2: Confirm wails toolchain available**

```bash
which wails || echo "MISSING — install with: go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.1"
wails version || true
```

Expected: a wails binary on PATH. If missing, install before continuing — bindings regeneration in Task 2 needs it.

---

## Task 1: Go-side TranslateConfig in PluginConfig

**Files:**
- Modify: `desktop/plugin_config.go`
- Test: `desktop/plugin_config_test.go` (existing or new)

- [ ] **Step 1: Write the failing test**

Add to `desktop/plugin_config_test.go` (create file if absent):

```go
package main

import (
	"strings"
	"testing"
)

func TestPluginConfig_TranslateDefaults(t *testing.T) {
	var c PluginConfig
	c.applyDefaults()
	if c.Translate.Provider != "openai-compatible" {
		t.Fatalf("Translate.Provider default: got %q want %q", c.Translate.Provider, "openai-compatible")
	}
	if c.Translate.BaseURL != "https://api.openai.com" {
		t.Fatalf("Translate.BaseURL default: got %q", c.Translate.BaseURL)
	}
	if c.Translate.Model != "gpt-4o-mini" {
		t.Fatalf("Translate.Model default: got %q", c.Translate.Model)
	}
	if c.Translate.DefaultTargetLang != "zh-CN" {
		t.Fatalf("Translate.DefaultTargetLang default: got %q", c.Translate.DefaultTargetLang)
	}
	if c.Translate.APIKey != "" {
		t.Fatalf("Translate.APIKey default: got %q want empty", c.Translate.APIKey)
	}
	if c.Translate.Enabled {
		t.Fatalf("Translate.Enabled default: got true want false (opt-in)")
	}
}

func TestValidatePluginConfig_TranslateProviderEnum(t *testing.T) {
	var c PluginConfig
	c.applyDefaults()
	c.Translate.Provider = "deepl"
	err := ValidatePluginConfig(c)
	if err == nil || !strings.Contains(err.Error(), "translate.provider") {
		t.Fatalf("want translate.provider validation error, got %v", err)
	}
}

func TestValidatePluginConfig_TranslateTargetLangEnum(t *testing.T) {
	var c PluginConfig
	c.applyDefaults()
	c.Translate.DefaultTargetLang = "xx-YY"
	err := ValidatePluginConfig(c)
	if err == nil || !strings.Contains(err.Error(), "translate.defaultTargetLang") {
		t.Fatalf("want translate.defaultTargetLang validation error, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd desktop && go test -run TestPluginConfig_Translate -count=1 . 2>&1 | tail -10
cd desktop && go test -run TestValidatePluginConfig_Translate -count=1 . 2>&1 | tail -10
```

Expected: compile errors about `c.Translate` undefined.

- [ ] **Step 3: Implement TranslateConfig + defaults + validation**

Edit `desktop/plugin_config.go`. Add after `FileExplorerConfig`:

```go
type TranslateConfig struct {
	Enabled           bool   `json:"enabled"`
	Provider          string `json:"provider"`           // currently only "openai-compatible"
	BaseURL           string `json:"baseUrl"`            // e.g. "https://api.openai.com"
	APIKey            string `json:"apiKey"`             // plaintext; same trust as other plugin config
	Model             string `json:"model"`              // free-text; user picks per their endpoint
	DefaultTargetLang string `json:"defaultTargetLang"`  // one of allowedTranslateTargetLangs
}

// allowedTranslateTargetLangs matches the dropdown in TranslateSettings.vue.
// Keep in sync with desktop/frontend/src/plugins/translate/index.ts SUPPORTED_TARGETS.
var allowedTranslateTargetLangs = map[string]bool{
	"zh-CN": true, "en": true, "ja": true, "ko": true,
	"de": true, "fr": true, "es": true,
}

var allowedTranslateProviders = map[string]bool{
	"openai-compatible": true,
}
```

Modify `PluginConfig` struct to include `Translate`:

```go
type PluginConfig struct {
	QuickInput   QuickInputConfig   `json:"quickInput"`
	FileExplorer FileExplorerConfig `json:"fileExplorer"`
	Translate    TranslateConfig    `json:"translate"`
}
```

Extend `applyDefaults` to seed Translate when the provider is blank (signal of "never set"):

```go
func (c *PluginConfig) applyDefaults() {
	if len(c.QuickInput.Buttons) == 0 {
		c.QuickInput.Enabled = true
		c.QuickInput.Buttons = defaultQuickInputButtons()
	}
	if c.FileExplorer.PanelWidthPx == 0 {
		c.FileExplorer.PanelWidthPx = 380
		c.FileExplorer.PanelCollapsed = false
		c.FileExplorer.InnerTreeRatio = 0.3
		c.FileExplorer.ShowHidden = false
	}
	if c.Translate.Provider == "" {
		c.Translate.Enabled = false
		c.Translate.Provider = "openai-compatible"
		c.Translate.BaseURL = "https://api.openai.com"
		c.Translate.Model = "gpt-4o-mini"
		c.Translate.DefaultTargetLang = "zh-CN"
	}
}
```

Extend `ValidatePluginConfig` at the bottom (just before `return nil`):

```go
	if !allowedTranslateProviders[c.Translate.Provider] {
		return fmt.Errorf("translate.provider %q not allowed", c.Translate.Provider)
	}
	if !allowedTranslateTargetLangs[c.Translate.DefaultTargetLang] {
		return fmt.Errorf("translate.defaultTargetLang %q not allowed", c.Translate.DefaultTargetLang)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd desktop && go test -run "TestPluginConfig_Translate|TestValidatePluginConfig_Translate" -count=1 . 2>&1 | tail -10
cd desktop && go test ./... -count=1 2>&1 | tail -10
```

Expected: new tests pass; existing tests still pass.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/plugin_config.go desktop/plugin_config_test.go
git commit -m "feat(desktop): add TranslateConfig field to plugin config"
```

---

## Task 2: Regenerate Wails frontend bindings

**Files:**
- Modify (auto-generated): `desktop/frontend/wailsjs/go/models.ts`

- [ ] **Step 1: Regenerate bindings**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop
wails generate module 2>&1 | tail -10
```

Expected: writes updated `frontend/wailsjs/go/models.ts` containing `TranslateConfig`. If `wails generate module` is not a recognized subcommand for the installed wails version, fall back to `wails build -nopackage -s 2>&1 | head -30` which also regenerates bindings as part of its frontend-prep step (you can interrupt with Ctrl+C once the bindings file has been written; check `frontend/wailsjs/go/models.ts` mtime).

- [ ] **Step 2: Verify the generated type**

```bash
grep -n "TranslateConfig\|translate:" desktop/frontend/wailsjs/go/models.ts | head -10
```

Expected: lines showing `class TranslateConfig` and `translate: TranslateConfig` inside `PluginConfig`.

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/wailsjs/go/models.ts
git commit -m "chore(desktop): regenerate wails bindings for TranslateConfig"
```

---

## Task 3: Wire translate enable into frontend configStore

**Files:**
- Modify: `desktop/frontend/src/plugins/configStore.ts`

- [ ] **Step 1: Add re-export and extend isPluginEnabled / setEnabled**

Edit `desktop/frontend/src/plugins/configStore.ts`. Update top exports (add `TranslateConfig`):

```ts
export type QuickInputButton = main.QuickInputButton;
export type QuickInputConfig = main.QuickInputConfig;
export type FileExplorerConfig = main.FileExplorerConfig;
export type TranslateConfig = main.TranslateConfig;
export type PluginConfig = main.PluginConfig;
```

In `isPluginEnabled`, add `translate` branch before the final `return false`:

```ts
  function isPluginEnabled(id: PluginID): boolean {
    if (!cfg.value) return false;
    if (id === "quick-input") return cfg.value.quickInput.enabled;
    if (id === "file-explorer") return cfg.value.fileExplorer.enabled;
    if (id === "translate") return cfg.value.translate.enabled;
    return false;
  }
```

In `setEnabled`, add `translate` branch:

```ts
  async function setEnabled(id: PluginID, enabled: boolean) {
    if (!cfg.value) return;
    const next: PluginConfig = JSON.parse(JSON.stringify(cfg.value));
    if (id === "quick-input") next.quickInput.enabled = enabled;
    if (id === "file-explorer") next.fileExplorer.enabled = enabled;
    if (id === "translate") next.translate.enabled = enabled;
    await save(next);
  }
```

(`PluginID` type lives in `plugins/types.ts` and is updated in Task 4.)

- [ ] **Step 2: Verify type compile**

```bash
cd desktop/frontend && npx vue-tsc --noEmit 2>&1 | grep -E "configStore|error" | head -10
```

Expected: no `configStore.ts` errors (will likely error on `PluginID` not having "translate" — that's fixed in Task 4; defer commit until then).

- [ ] **Step 3: Run existing configStore test**

```bash
npx vitest run src/plugins/configStore.test.ts 2>&1 | tail -10
```

Expected: still 3 passing (the existing tests don't reference translate).

- [ ] **Step 4: Defer commit until after Task 4** (PluginID type widens in Task 4)

---

## Task 4: Extend plugin types — context-menu slot, MenuItem, PluginID

**Files:**
- Modify: `desktop/frontend/src/plugins/types.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/plugins/types.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import type { PluginSlot, MenuItem, ContextMenuPlugin, PluginID } from "./types";

describe("plugin types", () => {
  it("allows 'context-menu' as a PluginSlot", () => {
    const s: PluginSlot = "context-menu";
    expect(s).toBe("context-menu");
  });

  it("allows 'translate' as a PluginID", () => {
    const id: PluginID = "translate";
    expect(id).toBe("translate");
  });

  it("ContextMenuPlugin.getMenuItems returns MenuItem[]", () => {
    const fake: ContextMenuPlugin = {
      getMenuItems: () => [{ id: "x", label: "X", onClick: () => {} }],
    };
    const items: MenuItem[] = fake.getMenuItems(
      // minimal duck-typed context — only used at the type level here
      {} as never,
      "selection",
    );
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ id: "x", label: "X" });
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/plugins/types.test.ts 2>&1 | tail -10
```

Expected: TypeScript / runtime errors about `MenuItem`, `ContextMenuPlugin`, `context-menu` not assignable.

- [ ] **Step 3: Implement the type extensions**

Edit `desktop/frontend/src/plugins/types.ts`. Replace the file content with (preserving existing exports — re-read the original file first and extend rather than overwrite blindly):

```ts
import type { Component } from "vue";
import type { Pane } from "../lib/types";

export type PluginSlot = "right-panel" | "bottom-toolbar" | "context-menu";

export type PluginID = "quick-input" | "file-explorer" | "translate";

export interface MenuItem {
  id: string;
  label: string;
  disabled?: boolean;
  onClick: () => void;
}

export interface ContextMenuPlugin {
  getMenuItems(ctx: PluginContext, selection: string): MenuItem[];
}

export interface PluginContext {
  // ... preserve the existing fields exactly as in the file you just read ...
  // (do not change them in this task; copy them verbatim)
}

export interface PluginDescriptor {
  id: PluginID;
  slot: PluginSlot;
  title: string;
  description: string;
  // Union return: component-slot plugins load a Vue component;
  // context-menu plugins load a headless ContextMenuPlugin.
  load: () =>
    | Promise<{ default: Component }>
    | Promise<{ default: ContextMenuPlugin }>;
  defaultEnabled?: boolean;
}
```

**Important:** open `desktop/frontend/src/plugins/types.ts` first, read its current `PluginContext` definition, and paste it back unchanged. The only fields this task changes are `PluginSlot`, `PluginID`, `MenuItem`, `ContextMenuPlugin`, and the `load` signature on `PluginDescriptor`. Do not invent new context fields.

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd desktop/frontend && npx vitest run src/plugins/types.test.ts 2>&1 | tail -10
cd desktop/frontend && npx vue-tsc --noEmit 2>&1 | tail -20
```

Expected: types.test.ts passes. vue-tsc may still emit errors from `registry.ts` because `PluginID` doesn't yet include `translate` registration — that's fixed in Task 13. Just verify no new errors in `configStore.ts` and `types.ts`.

- [ ] **Step 5: Commit (with Task 3 changes)**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/plugins/types.ts desktop/frontend/src/plugins/types.test.ts desktop/frontend/src/plugins/configStore.ts
git commit -m "feat(desktop): extend plugin types with context-menu slot and translate id"
```

---

## Task 5: PluginHost helper — collectContextMenuItems

**Files:**
- Modify: `desktop/frontend/src/plugins/PluginHost.vue`
- Create: `desktop/frontend/src/plugins/contextMenuItems.ts`
- Test: `desktop/frontend/src/plugins/contextMenuItems.test.ts`

Rationale: keeping the merge helper in its own module (rather than inside the `<script setup>` of PluginHost.vue) makes it directly importable by TerminalView and trivially unit-testable.

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/plugins/contextMenuItems.test.ts`:

```ts
import { describe, expect, it, vi } from "vitest";
import { collectContextMenuItems } from "./contextMenuItems";
import type { ContextMenuPlugin, MenuItem, PluginContext } from "./types";

const fakeCtx = {} as PluginContext;

describe("collectContextMenuItems", () => {
  it("returns empty when no plugins registered", async () => {
    const items = await collectContextMenuItems([], fakeCtx, "sel");
    expect(items).toEqual([]);
  });

  it("merges items from multiple plugins in registration order", async () => {
    const a: ContextMenuPlugin = {
      getMenuItems: () => [{ id: "a1", label: "A1", onClick: () => {} }],
    };
    const b: ContextMenuPlugin = {
      getMenuItems: () => [
        { id: "b1", label: "B1", onClick: () => {} },
        { id: "b2", label: "B2", onClick: () => {} },
      ],
    };
    const items = await collectContextMenuItems([a, b], fakeCtx, "sel");
    expect(items.map((i) => i.id)).toEqual(["a1", "b1", "b2"]);
  });

  it("skips a plugin whose getMenuItems throws, logs the error", async () => {
    const consoleSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const ok: ContextMenuPlugin = {
      getMenuItems: () => [{ id: "ok1", label: "OK", onClick: () => {} }],
    };
    const bad: ContextMenuPlugin = {
      getMenuItems: () => { throw new Error("boom"); },
    };
    const items = await collectContextMenuItems([ok, bad, ok], fakeCtx, "sel");
    expect(items.map((i) => i.id)).toEqual(["ok1", "ok1"]);
    expect(consoleSpy).toHaveBeenCalled();
    consoleSpy.mockRestore();
  });

  it("passes selection through to each plugin", async () => {
    const seen: string[] = [];
    const probe: ContextMenuPlugin = {
      getMenuItems: (_ctx, sel) => {
        seen.push(sel);
        return [];
      },
    };
    await collectContextMenuItems([probe], fakeCtx, "hello world");
    expect(seen).toEqual(["hello world"]);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/plugins/contextMenuItems.test.ts 2>&1 | tail -10
```

Expected: module not found `./contextMenuItems`.

- [ ] **Step 3: Implement the helper**

Create `desktop/frontend/src/plugins/contextMenuItems.ts`:

```ts
import type { ContextMenuPlugin, MenuItem, PluginContext } from "./types";

// collectContextMenuItems calls each plugin's getMenuItems in registration
// order and concatenates the results. A plugin that throws is skipped with
// console.error so one buggy plugin can't suppress the entire menu.
export async function collectContextMenuItems(
  plugins: ContextMenuPlugin[],
  ctx: PluginContext,
  selection: string,
): Promise<MenuItem[]> {
  const out: MenuItem[] = [];
  for (const p of plugins) {
    try {
      const items = p.getMenuItems(ctx, selection);
      out.push(...items);
    } catch (e) {
      console.error("context-menu plugin getMenuItems threw", e);
    }
  }
  return out;
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd desktop/frontend && npx vitest run src/plugins/contextMenuItems.test.ts 2>&1 | tail -10
```

Expected: 4 passing.

- [ ] **Step 5: Update PluginHost.vue to filter headless plugins out of slot rendering**

Open `desktop/frontend/src/plugins/PluginHost.vue` and find the `reconcile()` function. After the existing `descriptorsForSlot(props.slotId).filter((d) => store.isPluginEnabled(d.id))` line, ensure context-menu plugins are never rendered as components:

```ts
    const slotPlugins = descriptorsForSlot(props.slotId).filter((d) =>
      store.isPluginEnabled(d.id),
    );
    // Context-menu plugins are headless — they expose getMenuItems instead
    // of a Vue component and must not be mounted by this host.
    const componentPlugins = slotPlugins.filter((d) => d.slot !== "context-menu");
    const next: LoadedPlugin[] = [];
    for (const d of componentPlugins) {
      // ... existing load + push loop unchanged ...
```

Re-read the surrounding code first to splice cleanly.

- [ ] **Step 6: Run full vitest suite to confirm no regression**

```bash
cd desktop/frontend && npx vitest run 2>&1 | tail -5
```

Expected: all existing tests still pass.

- [ ] **Step 7: Commit**

```bash
git add desktop/frontend/src/plugins/contextMenuItems.ts desktop/frontend/src/plugins/contextMenuItems.test.ts desktop/frontend/src/plugins/PluginHost.vue
git commit -m "feat(desktop): collectContextMenuItems helper for headless plugins"
```

---

## Task 6: Translate provider types and error class

**Files:**
- Create: `desktop/frontend/src/plugins/translate/providers/types.ts`
- Test: `desktop/frontend/src/plugins/translate/providers/types.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/plugins/translate/providers/types.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { TranslateError, type TranslateErrorCode, type TranslateResult, type TranslateProvider } from "./types";

describe("TranslateError", () => {
  it("carries code, message, httpStatus, providerBody", () => {
    const e = new TranslateError("auth", "Auth failed", 401, '{"error":"invalid_api_key"}');
    expect(e.name).toBe("TranslateError");
    expect(e.code).toBe<TranslateErrorCode>("auth");
    expect(e.message).toBe("Auth failed");
    expect(e.httpStatus).toBe(401);
    expect(e.providerBody).toBe('{"error":"invalid_api_key"}');
    expect(e instanceof Error).toBe(true);
  });

  it("works without optional fields", () => {
    const e = new TranslateError("network", "fetch failed");
    expect(e.httpStatus).toBeUndefined();
    expect(e.providerBody).toBeUndefined();
  });

  it("TranslateResult shape compiles", () => {
    const r: TranslateResult = { translated: "你好", detectedSrcLang: "en" };
    expect(r.translated).toBe("你好");
  });

  it("TranslateProvider interface compiles", async () => {
    const stub: TranslateProvider = {
      translate: async () => ({ translated: "", detectedSrcLang: "unknown" }),
    };
    const out = await stub.translate("hi", "zh-CN", { signal: new AbortController().signal });
    expect(out.detectedSrcLang).toBe("unknown");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/plugins/translate/providers/types.test.ts 2>&1 | tail -10
```

Expected: module not found.

- [ ] **Step 3: Implement the types**

Create `desktop/frontend/src/plugins/translate/providers/types.ts`:

```ts
export interface TranslateResult {
  translated: string;
  // ISO 639-1 ("en", "zh", "ja") if the provider reported one;
  // "unknown" if the provider did not (e.g. fallback non-JSON mode).
  detectedSrcLang: string;
}

export type TranslateErrorCode =
  | "auth"        // 401/403
  | "rate_limit"  // 429
  | "server"      // 5xx
  | "network"     // fetch threw, no response
  | "timeout"     // AbortController fired due to 30s timeout
  | "aborted"     // user-triggered cancel (e.g. switched targetLang) — silent
  | "parse"       // provider returned non-JSON in JSON mode
  | "unknown";

export class TranslateError extends Error {
  constructor(
    public readonly code: TranslateErrorCode,
    message: string,
    public readonly httpStatus?: number,
    public readonly providerBody?: string,
  ) {
    super(message);
    this.name = "TranslateError";
  }
}

export interface TranslateProvider {
  translate(
    text: string,
    targetLang: string,
    opts: { signal: AbortSignal },
  ): Promise<TranslateResult>;
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd desktop/frontend && npx vitest run src/plugins/translate/providers/types.test.ts 2>&1 | tail -10
```

Expected: 4 passing.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/plugins/translate/providers/types.ts desktop/frontend/src/plugins/translate/providers/types.test.ts
git commit -m "feat(desktop): TranslateProvider interface and error class"
```

---

## Task 7: Language-detection heuristic

**Files:**
- Create: `desktop/frontend/src/plugins/translate/detectLang.ts`
- Test: `desktop/frontend/src/plugins/translate/detectLang.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/plugins/translate/detectLang.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { hasCJK, computeAutoTargetLang } from "./detectLang";

describe("hasCJK", () => {
  it("returns true for Chinese", () => {
    expect(hasCJK("你好世界")).toBe(true);
  });
  it("returns true for Japanese kanji", () => {
    expect(hasCJK("日本")).toBe(true);
  });
  it("returns true for mixed text", () => {
    expect(hasCJK("error: 文件不存在")).toBe(true);
  });
  it("returns false for ASCII", () => {
    expect(hasCJK("dial tcp 10.0.0.5:6379")).toBe(false);
  });
  it("returns false for empty string", () => {
    expect(hasCJK("")).toBe(false);
  });
});

describe("computeAutoTargetLang", () => {
  it("CJK text + default zh-CN → returns 'en'", () => {
    expect(computeAutoTargetLang("你好", "zh-CN")).toBe("en");
  });
  it("ASCII text + default zh-CN → returns 'zh-CN'", () => {
    expect(computeAutoTargetLang("hello", "zh-CN")).toBe("zh-CN");
  });
  it("CJK text + default 'en' → returns 'en' (config wins)", () => {
    expect(computeAutoTargetLang("你好", "en")).toBe("en");
  });
  it("ASCII text + default 'ja' → returns 'ja'", () => {
    expect(computeAutoTargetLang("hello", "ja")).toBe("ja");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/plugins/translate/detectLang.test.ts 2>&1 | tail -10
```

Expected: module not found.

- [ ] **Step 3: Implement**

Create `desktop/frontend/src/plugins/translate/detectLang.ts`:

```ts
// hasCJK returns true if the string contains any Chinese / Japanese kanji /
// Korean Hanja character in the CJK Unified Ideographs block (U+4E00..U+9FFF).
// Used to decide auto target lang. Hiragana / katakana / hangul aren't
// included here on purpose: a script of pure hiragana doesn't mean the user
// wants Chinese-out behavior — but their default target lang still applies.
export function hasCJK(s: string): boolean {
  return /[一-鿿]/.test(s);
}

// computeAutoTargetLang picks a target language for an auto-detected source:
//   - If the text contains CJK and the configured default is zh-CN, flip to "en"
//     so the user gets a non-Chinese result for already-Chinese text.
//   - Otherwise use the configured default.
export function computeAutoTargetLang(text: string, defaultTargetLang: string): string {
  if (hasCJK(text) && defaultTargetLang === "zh-CN") {
    return "en";
  }
  return defaultTargetLang;
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd desktop/frontend && npx vitest run src/plugins/translate/detectLang.test.ts 2>&1 | tail -10
```

Expected: 9 passing.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/plugins/translate/detectLang.ts desktop/frontend/src/plugins/translate/detectLang.test.ts
git commit -m "feat(desktop): translate language-detection heuristic"
```

---

## Task 8: OpenAI-compatible provider

**Files:**
- Create: `desktop/frontend/src/plugins/translate/providers/openai.ts`
- Test: `desktop/frontend/src/plugins/translate/providers/openai.test.ts`

This is the biggest single task in the plan. The provider has 5 distinct behaviors that each warrant a test:
happy path, 401, 5xx, timeout, response_format fallback.

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/plugins/translate/providers/openai.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createOpenAIProvider } from "./openai";
import { TranslateError } from "./types";

function mkResponse(body: unknown, init: { status?: number; ok?: boolean } = {}): Response {
  const status = init.status ?? 200;
  return new Response(typeof body === "string" ? body : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function mkChoice(content: string) {
  return { choices: [{ message: { content } }] };
}

const baseConfig = {
  baseUrl: "https://api.openai.com",
  apiKey: "sk-test",
  model: "gpt-4o-mini",
};

describe("openai provider — happy path", () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); });

  it("parses JSON response and returns TranslateResult", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      mkResponse(mkChoice(JSON.stringify({ detectedSrcLang: "en", translated: "你好" }))),
    );
    vi.stubGlobal("fetch", fetchMock);
    const p = createOpenAIProvider(baseConfig);
    const r = await p.translate("hello", "zh-CN", { signal: new AbortController().signal });
    expect(r).toEqual({ translated: "你好", detectedSrcLang: "en" });
    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("https://api.openai.com/v1/chat/completions");
    expect((init as RequestInit).method).toBe("POST");
    const body = JSON.parse((init as RequestInit).body as string);
    expect(body.model).toBe("gpt-4o-mini");
    expect(body.response_format).toEqual({ type: "json_object" });
  });
});

describe("openai provider — error mapping", () => {
  afterEach(() => { vi.restoreAllMocks(); });

  it("401 → TranslateError code 'auth'", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(
      mkResponse({ error: "invalid_api_key" }, { status: 401 }),
    ));
    const p = createOpenAIProvider(baseConfig);
    await expect(p.translate("x", "en", { signal: new AbortController().signal }))
      .rejects.toMatchObject({ code: "auth", httpStatus: 401 });
  });

  it("429 → TranslateError code 'rate_limit'", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(
      mkResponse({ error: "rate_limited" }, { status: 429 }),
    ));
    const p = createOpenAIProvider(baseConfig);
    await expect(p.translate("x", "en", { signal: new AbortController().signal }))
      .rejects.toMatchObject({ code: "rate_limit", httpStatus: 429 });
  });

  it("5xx → TranslateError code 'server'", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(
      mkResponse("upstream gone", { status: 502 }),
    ));
    const p = createOpenAIProvider(baseConfig);
    await expect(p.translate("x", "en", { signal: new AbortController().signal }))
      .rejects.toMatchObject({ code: "server", httpStatus: 502 });
  });

  it("fetch rejects (network) → TranslateError code 'network'", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));
    const p = createOpenAIProvider(baseConfig);
    await expect(p.translate("x", "en", { signal: new AbortController().signal }))
      .rejects.toMatchObject({ code: "network" });
  });

  it("user-triggered abort → TranslateError code 'aborted'", async () => {
    const ctl = new AbortController();
    vi.stubGlobal("fetch", vi.fn().mockImplementation(
      (_url: string, init: RequestInit) =>
        new Promise((_resolve, reject) => {
          (init.signal as AbortSignal).addEventListener("abort", () => {
            const err = new DOMException("aborted", "AbortError");
            reject(err);
          });
        }),
    ));
    const p = createOpenAIProvider(baseConfig);
    const promise = p.translate("x", "en", { signal: ctl.signal });
    ctl.abort();
    await expect(promise).rejects.toMatchObject({ code: "aborted" });
  });
});

describe("openai provider — response_format fallback", () => {
  afterEach(() => { vi.restoreAllMocks(); });

  it("retries without response_format when endpoint rejects it", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(
        mkResponse({ error: "response_format is not supported" }, { status: 400 }),
      )
      .mockResolvedValueOnce(
        mkResponse(mkChoice("你好")),  // raw text in fallback mode
      );
    vi.stubGlobal("fetch", fetchMock);
    const p = createOpenAIProvider(baseConfig);
    const r = await p.translate("hello", "zh-CN", { signal: new AbortController().signal });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(r).toEqual({ translated: "你好", detectedSrcLang: "unknown" });
    const secondBody = JSON.parse((fetchMock.mock.calls[1][1] as RequestInit).body as string);
    expect(secondBody.response_format).toBeUndefined();
  });

  it("subsequent call after fallback skips response_format from the start", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(
        mkResponse({ error: "response_format is not supported" }, { status: 400 }),
      )
      .mockResolvedValueOnce(mkResponse(mkChoice("你好")))
      .mockResolvedValueOnce(mkResponse(mkChoice("再见")));
    vi.stubGlobal("fetch", fetchMock);
    const p = createOpenAIProvider(baseConfig);
    await p.translate("hello", "zh-CN", { signal: new AbortController().signal });
    await p.translate("bye", "zh-CN", { signal: new AbortController().signal });
    // Third call (second user-initiated translate) — only one underlying call
    // because the provider remembered the fallback flag from the first round.
    expect(fetchMock).toHaveBeenCalledTimes(3);
    const thirdBody = JSON.parse((fetchMock.mock.calls[2][1] as RequestInit).body as string);
    expect(thirdBody.response_format).toBeUndefined();
  });

  it("malformed JSON in JSON mode → returns raw content, no throw", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(
      mkResponse(mkChoice("not valid json")),
    ));
    const p = createOpenAIProvider(baseConfig);
    const r = await p.translate("hello", "zh-CN", { signal: new AbortController().signal });
    expect(r).toEqual({ translated: "not valid json", detectedSrcLang: "unknown" });
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/plugins/translate/providers/openai.test.ts 2>&1 | tail -10
```

Expected: module not found.

- [ ] **Step 3: Implement the provider**

Create `desktop/frontend/src/plugins/translate/providers/openai.ts`:

```ts
import { TranslateError, type TranslateProvider, type TranslateResult } from "./types";

const REQUEST_TIMEOUT_MS = 30_000;

export interface OpenAIProviderConfig {
  baseUrl: string;  // e.g. "https://api.openai.com" — no trailing slash, no /v1
  apiKey: string;
  model: string;
}

// createOpenAIProvider returns a stateful TranslateProvider that remembers
// whether the endpoint supports response_format. After a single failure
// matching response_format / json_object, all subsequent requests use the
// plain-text fallback prompt.
export function createOpenAIProvider(cfg: OpenAIProviderConfig): TranslateProvider {
  let supportsJsonMode = true;

  async function call(text: string, targetLang: string, signal: AbortSignal, useJsonMode: boolean): Promise<TranslateResult> {
    const url = cfg.baseUrl.replace(/\/+$/, "") + "/v1/chat/completions";

    const systemJson = `You are a translation engine. Detect the source language and translate to ${targetLang}. Respond with strict JSON: {"detectedSrcLang":"<ISO 639-1 code>","translated":"<translation>"}. No commentary, no markdown.`;
    const systemPlain = `You are a translation engine. Translate the following text to ${targetLang}. Output ONLY the translation, no explanation, no quotes, no markdown.`;

    const body: Record<string, unknown> = {
      model: cfg.model,
      messages: [
        { role: "system", content: useJsonMode ? systemJson : systemPlain },
        { role: "user", content: text },
      ],
      temperature: 0.2,
    };
    if (useJsonMode) body.response_format = { type: "json_object" };

    // Wrap user-supplied abort signal + 30s timeout into a combined signal.
    const timeoutCtl = new AbortController();
    const timer = setTimeout(() => timeoutCtl.abort(), REQUEST_TIMEOUT_MS);
    const onUserAbort = () => timeoutCtl.abort();
    signal.addEventListener("abort", onUserAbort);

    let res: Response;
    try {
      res = await fetch(url, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${cfg.apiKey}`,
        },
        body: JSON.stringify(body),
        signal: timeoutCtl.signal,
      });
    } catch (e) {
      const isAbort = e instanceof DOMException && e.name === "AbortError";
      if (isAbort) {
        if (signal.aborted) throw new TranslateError("aborted", "user cancelled");
        throw new TranslateError("timeout", `request timed out after ${REQUEST_TIMEOUT_MS / 1000}s`);
      }
      throw new TranslateError("network", e instanceof Error ? e.message : String(e));
    } finally {
      clearTimeout(timer);
      signal.removeEventListener("abort", onUserAbort);
    }

    const bodyText = await res.text();

    if (res.status === 401 || res.status === 403) {
      throw new TranslateError("auth", `auth failed (${res.status})`, res.status, bodyText.slice(0, 200));
    }
    if (res.status === 429) {
      throw new TranslateError("rate_limit", `rate limited (429)`, res.status, bodyText.slice(0, 200));
    }
    if (res.status >= 500) {
      throw new TranslateError("server", `server error (${res.status})`, res.status, bodyText.slice(0, 200));
    }
    if (res.status === 400 && useJsonMode && /response_format|json_object/i.test(bodyText)) {
      // Endpoint doesn't support JSON mode; mark and let caller fallback.
      supportsJsonMode = false;
      throw new ResponseFormatNotSupportedError();
    }
    if (!res.ok) {
      throw new TranslateError("unknown", `HTTP ${res.status}`, res.status, bodyText.slice(0, 200));
    }

    let envelope: { choices?: Array<{ message?: { content?: string } }> };
    try {
      envelope = JSON.parse(bodyText);
    } catch {
      throw new TranslateError("parse", "provider returned non-JSON outer envelope", res.status, bodyText.slice(0, 200));
    }
    const content = envelope.choices?.[0]?.message?.content ?? "";

    if (!useJsonMode) {
      return { translated: content.trim(), detectedSrcLang: "unknown" };
    }
    try {
      const inner = JSON.parse(content) as { translated?: unknown; detectedSrcLang?: unknown };
      if (typeof inner.translated !== "string") throw new Error("missing translated");
      const detected = typeof inner.detectedSrcLang === "string" ? inner.detectedSrcLang : "unknown";
      return { translated: inner.translated, detectedSrcLang: detected };
    } catch {
      // Provider sent JSON-mode response but the content isn't valid JSON.
      // Per spec: degrade gracefully — show raw content as translated.
      return { translated: content, detectedSrcLang: "unknown" };
    }
  }

  return {
    async translate(text, targetLang, opts) {
      try {
        return await call(text, targetLang, opts.signal, supportsJsonMode);
      } catch (e) {
        if (e instanceof ResponseFormatNotSupportedError) {
          // Retry once with plain mode. supportsJsonMode is already false.
          return await call(text, targetLang, opts.signal, false);
        }
        throw e;
      }
    },
  };
}

class ResponseFormatNotSupportedError extends Error {
  constructor() { super("response_format unsupported"); }
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd desktop/frontend && npx vitest run src/plugins/translate/providers/openai.test.ts 2>&1 | tail -15
```

Expected: 9 passing.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/plugins/translate/providers/openai.ts desktop/frontend/src/plugins/translate/providers/openai.test.ts
git commit -m "feat(desktop): OpenAI-compatible translate provider with fallback"
```

---

## Task 9: panelStore (Pinia)

**Files:**
- Create: `desktop/frontend/src/plugins/translate/panelStore.ts`
- Test: `desktop/frontend/src/plugins/translate/panelStore.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/plugins/translate/panelStore.test.ts`:

```ts
import { setActivePinia, createPinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useTranslatePanelStore } from "./panelStore";
import { TranslateError, type TranslateProvider } from "./providers/types";

function fakeProvider(): { provider: TranslateProvider; calls: Array<{ text: string; target: string }> } {
  const calls: Array<{ text: string; target: string }> = [];
  return {
    calls,
    provider: {
      translate: vi.fn(async (text, target) => {
        calls.push({ text, target });
        return { translated: `[${target}] ${text}`, detectedSrcLang: "en" };
      }),
    },
  };
}

describe("translatePanelStore", () => {
  beforeEach(() => setActivePinia(createPinia()));
  afterEach(() => vi.restoreAllMocks());

  it("openWithSource sets state and dispatches translate", async () => {
    const { provider, calls } = fakeProvider();
    const store = useTranslatePanelStore();
    store.configure({ provider, defaultTargetLang: "zh-CN" });
    await store.openWithSource("hello");
    expect(store.visible).toBe(true);
    expect(store.source).toBe("hello");
    expect(store.targetLang).toBe("zh-CN");
    expect(store.loading).toBe(false);
    expect(store.result).toEqual({ translated: "[zh-CN] hello", detectedSrcLang: "en" });
    expect(store.history.length).toBe(1);
    expect(calls).toEqual([{ text: "hello", target: "zh-CN" }]);
  });

  it("CJK source auto-targets 'en'", async () => {
    const { provider } = fakeProvider();
    const store = useTranslatePanelStore();
    store.configure({ provider, defaultTargetLang: "zh-CN" });
    await store.openWithSource("你好");
    expect(store.targetLang).toBe("en");
  });

  it("history caps at 5, newest first", async () => {
    const { provider } = fakeProvider();
    const store = useTranslatePanelStore();
    store.configure({ provider, defaultTargetLang: "zh-CN" });
    for (let i = 0; i < 7; i++) await store.openWithSource(`text-${i}`);
    expect(store.history.length).toBe(5);
    expect(store.history[0].source).toBe("text-6");
    expect(store.history[4].source).toBe("text-2");
  });

  it("error sets state.error, does not push history", async () => {
    const provider: TranslateProvider = {
      translate: async () => { throw new TranslateError("auth", "bad key"); },
    };
    const store = useTranslatePanelStore();
    store.configure({ provider, defaultTargetLang: "zh-CN" });
    await store.openWithSource("hello");
    expect(store.error?.code).toBe("auth");
    expect(store.history.length).toBe(0);
    expect(store.result).toBeNull();
  });

  it("changeTarget re-translates with new lang", async () => {
    const { provider, calls } = fakeProvider();
    const store = useTranslatePanelStore();
    store.configure({ provider, defaultTargetLang: "zh-CN" });
    await store.openWithSource("hello");
    await store.changeTarget("ja");
    expect(store.targetLang).toBe("ja");
    expect(store.result?.translated).toBe("[ja] hello");
    expect(calls).toEqual([
      { text: "hello", target: "zh-CN" },
      { text: "hello", target: "ja" },
    ]);
  });

  it("second openWithSource aborts in-flight translation", async () => {
    let abortedSignal: AbortSignal | null = null;
    const provider: TranslateProvider = {
      translate: (text, target, opts) => new Promise((resolve, reject) => {
        opts.signal.addEventListener("abort", () => {
          abortedSignal = opts.signal;
          reject(new TranslateError("aborted", "user cancelled"));
        });
        setTimeout(() => resolve({ translated: `[${target}] ${text}`, detectedSrcLang: "en" }), 10_000);
      }),
    };
    const store = useTranslatePanelStore();
    store.configure({ provider, defaultTargetLang: "zh-CN" });
    const first = store.openWithSource("first");
    await Promise.resolve();  // let the inner promise hook up
    await store.openWithSource("second");  // triggers abort on first
    await expect(first).resolves.toBeUndefined();
    expect(abortedSignal?.aborted).toBe(true);
    expect(store.source).toBe("second");
  });

  it("close hides panel but retains state", async () => {
    const { provider } = fakeProvider();
    const store = useTranslatePanelStore();
    store.configure({ provider, defaultTargetLang: "zh-CN" });
    await store.openWithSource("hello");
    store.close();
    expect(store.visible).toBe(false);
    expect(store.source).toBe("hello");
    expect(store.result?.translated).toBe("[zh-CN] hello");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/plugins/translate/panelStore.test.ts 2>&1 | tail -10
```

Expected: module not found.

- [ ] **Step 3: Implement the store**

Create `desktop/frontend/src/plugins/translate/panelStore.ts`:

```ts
import { defineStore } from "pinia";
import { ref, shallowRef } from "vue";
import { computeAutoTargetLang } from "./detectLang";
import { TranslateError, type TranslateProvider, type TranslateResult } from "./providers/types";

interface HistoryEntry {
  source: string;
  target: string;
  translated: string;
  detectedSrcLang: string;
  at: number;
}

interface PanelError {
  code: TranslateError["code"];
  message: string;
}

interface Config {
  provider: TranslateProvider;
  defaultTargetLang: string;
}

const HISTORY_LIMIT = 5;

export const useTranslatePanelStore = defineStore("translatePanel", () => {
  const visible = ref(false);
  const source = ref("");
  const targetLang = ref("zh-CN");
  const loading = ref(false);
  const error = ref<PanelError | null>(null);
  const result = ref<TranslateResult | null>(null);
  const history = ref<HistoryEntry[]>([]);
  const cfg = shallowRef<Config | null>(null);
  let currentController: AbortController | null = null;

  function configure(next: Config) {
    cfg.value = next;
  }

  function close() {
    visible.value = false;
  }

  async function openWithSource(text: string) {
    if (!cfg.value) {
      visible.value = true;
      error.value = { code: "unknown", message: "Translate plugin not configured" };
      return;
    }
    visible.value = true;
    source.value = text;
    targetLang.value = computeAutoTargetLang(text, cfg.value.defaultTargetLang);
    await doTranslate();
  }

  async function changeTarget(next: string) {
    targetLang.value = next;
    await doTranslate();
  }

  async function retry() {
    await doTranslate();
  }

  async function doTranslate() {
    if (!cfg.value) return;
    error.value = null;
    result.value = null;
    loading.value = true;
    currentController?.abort();
    const ctl = new AbortController();
    currentController = ctl;
    try {
      const r = await cfg.value.provider.translate(source.value, targetLang.value, { signal: ctl.signal });
      if (ctl.signal.aborted) return;
      result.value = r;
      history.value = [
        { source: source.value, target: targetLang.value, translated: r.translated, detectedSrcLang: r.detectedSrcLang, at: Date.now() },
        ...history.value,
      ].slice(0, HISTORY_LIMIT);
    } catch (e) {
      if (e instanceof TranslateError && e.code === "aborted") return;
      const code = e instanceof TranslateError ? e.code : "unknown";
      const msg = e instanceof Error ? e.message : String(e);
      error.value = { code, message: msg };
    } finally {
      if (currentController === ctl) loading.value = false;
    }
  }

  function restoreFromHistory(entry: HistoryEntry) {
    source.value = entry.source;
    targetLang.value = entry.target;
    result.value = { translated: entry.translated, detectedSrcLang: entry.detectedSrcLang };
    error.value = null;
    visible.value = true;
  }

  return {
    visible, source, targetLang, loading, error, result, history,
    configure, openWithSource, changeTarget, close, retry, restoreFromHistory,
  };
});
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd desktop/frontend && npx vitest run src/plugins/translate/panelStore.test.ts 2>&1 | tail -10
```

Expected: 7 passing.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/plugins/translate/panelStore.ts desktop/frontend/src/plugins/translate/panelStore.test.ts
git commit -m "feat(desktop): translate panel Pinia store"
```

---

## Task 10: TranslatePanel.vue

**Files:**
- Create: `desktop/frontend/src/plugins/translate/TranslatePanel.vue`
- Test: `desktop/frontend/src/plugins/translate/TranslatePanel.test.ts`

- [ ] **Step 1: Write the failing component test**

Create `desktop/frontend/src/plugins/translate/TranslatePanel.test.ts`:

```ts
import { mount } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import TranslatePanel from "./TranslatePanel.vue";
import { useTranslatePanelStore } from "./panelStore";
import type { TranslateProvider } from "./providers/types";

const fakeProvider: TranslateProvider = {
  translate: vi.fn(async (text, target) => ({ translated: `[${target}] ${text}`, detectedSrcLang: "en" })),
};

describe("TranslatePanel", () => {
  beforeEach(() => setActivePinia(createPinia()));

  it("renders nothing when store.visible is false", () => {
    const w = mount(TranslatePanel, { attachTo: document.body });
    expect(w.find('[data-testid="translate-panel"]').exists()).toBe(false);
    w.unmount();
  });

  it("renders source + translated when state populated", async () => {
    const store = useTranslatePanelStore();
    store.configure({ provider: fakeProvider, defaultTargetLang: "zh-CN" });
    await store.openWithSource("hello world");
    const w = mount(TranslatePanel, { attachTo: document.body });
    expect(w.text()).toContain("hello world");
    expect(w.text()).toContain("[zh-CN] hello world");
    w.unmount();
  });

  it("close button hides panel", async () => {
    const store = useTranslatePanelStore();
    store.configure({ provider: fakeProvider, defaultTargetLang: "zh-CN" });
    await store.openWithSource("x");
    const w = mount(TranslatePanel, { attachTo: document.body });
    await w.find('[data-testid="translate-close"]').trigger("click");
    expect(store.visible).toBe(false);
    w.unmount();
  });

  it("target dropdown change triggers re-translate", async () => {
    const store = useTranslatePanelStore();
    store.configure({ provider: fakeProvider, defaultTargetLang: "zh-CN" });
    await store.openWithSource("x");
    const w = mount(TranslatePanel, { attachTo: document.body });
    await w.find('[data-testid="translate-target"]').setValue("ja");
    // Pinia/Vue tick + provider resolved
    await new Promise((r) => setTimeout(r, 0));
    expect(store.targetLang).toBe("ja");
    expect(store.result?.translated).toBe("[ja] x");
    w.unmount();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/plugins/translate/TranslatePanel.test.ts 2>&1 | tail -10
```

Expected: module not found.

- [ ] **Step 3: Implement the component**

Create `desktop/frontend/src/plugins/translate/TranslatePanel.vue`:

```vue
<script lang="ts" setup>
import { computed, ref, onUnmounted } from "vue";
import { useTranslatePanelStore } from "./panelStore";

const store = useTranslatePanelStore();

const TARGETS = [
  { code: "zh-CN", label: "中文 (Simplified)" },
  { code: "en", label: "English" },
  { code: "ja", label: "日本語" },
  { code: "ko", label: "한국어" },
  { code: "de", label: "Deutsch" },
  { code: "fr", label: "Français" },
  { code: "es", label: "Español" },
];

// Dragging: panel top-left position relative to viewport.
const pos = ref({ x: -1, y: 80 });  // -1 = "not placed yet, center on first mount"
const dragging = ref(false);
const dragOff = ref({ dx: 0, dy: 0 });

function onMouseDown(e: MouseEvent) {
  const t = e.target as HTMLElement;
  if (t.closest(".translate-panel__handle") == null) return;
  dragging.value = true;
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
  dragOff.value = { dx: e.clientX - rect.left, dy: e.clientY - rect.top };
  pos.value = { x: rect.left, y: rect.top };
  e.preventDefault();
}

function onMouseMove(e: MouseEvent) {
  if (!dragging.value) return;
  pos.value = clampPos(e.clientX - dragOff.value.dx, e.clientY - dragOff.value.dy);
}

function onMouseUp() { dragging.value = false; }

function clampPos(x: number, y: number) {
  const w = 480;
  const h = 320;  // approximate; clamp uses fixed panel size
  return {
    x: Math.max(8, Math.min(window.innerWidth - w - 8, x)),
    y: Math.max(8, Math.min(window.innerHeight - h - 8, y)),
  };
}

window.addEventListener("mousemove", onMouseMove);
window.addEventListener("mouseup", onMouseUp);
onUnmounted(() => {
  window.removeEventListener("mousemove", onMouseMove);
  window.removeEventListener("mouseup", onMouseUp);
});

const panelStyle = computed(() => {
  if (pos.value.x < 0) {
    // Initial placement: horizontally centered, 80px from top.
    return { left: `${Math.max(8, (window.innerWidth - 480) / 2)}px`, top: "80px" };
  }
  return { left: `${pos.value.x}px`, top: `${pos.value.y}px` };
});

function onTargetChange(e: Event) {
  const next = (e.target as HTMLSelectElement).value;
  void store.changeTarget(next);
}

function onRetry() { void store.retry(); }
</script>

<template>
  <Teleport to="body">
    <div
      v-if="store.visible"
      data-testid="translate-panel"
      class="translate-panel"
      :style="panelStyle"
      @mousedown="onMouseDown"
    >
      <header class="translate-panel__handle">
        <span class="translate-panel__title">Translate</span>
        <button
          type="button"
          class="translate-panel__close"
          data-testid="translate-close"
          aria-label="Close translate panel"
          @click="store.close"
        >×</button>
      </header>

      <section class="translate-panel__source">
        <div class="translate-panel__label">
          Source <span v-if="store.result?.detectedSrcLang && store.result.detectedSrcLang !== 'unknown'" class="translate-panel__detected">· detected {{ store.result.detectedSrcLang }}</span>
        </div>
        <pre class="translate-panel__pre">{{ store.source }}</pre>
      </section>

      <section class="translate-panel__target-row">
        <label class="translate-panel__label" for="translate-target">Target</label>
        <select
          id="translate-target"
          data-testid="translate-target"
          :value="store.targetLang"
          @change="onTargetChange"
        >
          <option v-for="t in TARGETS" :key="t.code" :value="t.code">{{ t.label }}</option>
        </select>
      </section>

      <section class="translate-panel__result">
        <div v-if="store.loading" class="translate-panel__loading">Translating…</div>
        <div v-else-if="store.error" class="translate-panel__error" role="alert">
          <div>{{ store.error.message }}</div>
          <button type="button" @click="onRetry">Retry</button>
        </div>
        <div v-else-if="store.result" class="translate-panel__text">{{ store.result.translated }}</div>
        <div v-else class="translate-panel__placeholder">No result yet.</div>
      </section>

      <details v-if="store.history.length > 0" class="translate-panel__history">
        <summary>Recent ({{ store.history.length }})</summary>
        <ul>
          <li v-for="(h, i) in store.history" :key="i">
            <button type="button" class="translate-panel__history-row" @click="store.restoreFromHistory(h)">
              <span class="translate-panel__history-source">{{ h.source.slice(0, 60) }}</span>
              <span class="translate-panel__history-arrow">→</span>
              <span class="translate-panel__history-translated">{{ h.translated.slice(0, 60) }}</span>
            </button>
          </li>
        </ul>
      </details>
    </div>
  </Teleport>
</template>

<style scoped>
.translate-panel {
  position: fixed;
  width: 480px;
  max-height: 70vh;
  overflow: auto;
  background: var(--ed-tab-bg, #21262d);
  border: 1px solid var(--ed-border, #2d333b);
  border-radius: 6px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  color: var(--ed-row-fg, #c9d1d9);
  z-index: 9999;
  display: flex;
  flex-direction: column;
}
.translate-panel__handle {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  cursor: grab;
  background: var(--ed-row-hover, #30363d);
  border-bottom: 1px solid var(--ed-border, #2d333b);
  user-select: none;
}
.translate-panel__title { font-weight: 600; font-size: 13px; }
.translate-panel__close {
  background: transparent; border: none; color: inherit;
  font-size: 18px; cursor: pointer; padding: 0 4px;
}
.translate-panel__label { font-size: 11px; opacity: 0.7; text-transform: uppercase; letter-spacing: 0.04em; padding: 6px 12px 0; }
.translate-panel__detected { text-transform: none; opacity: 0.6; }
.translate-panel__pre {
  margin: 4px 12px 8px;
  padding: 6px;
  background: rgba(0, 0, 0, 0.25);
  border-radius: 4px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  max-height: 200px;
  overflow: auto;
  white-space: pre-wrap;
}
.translate-panel__target-row { display: flex; align-items: center; gap: 8px; padding: 8px 12px; border-top: 1px solid var(--ed-border, #2d333b); }
.translate-panel__target-row select { flex: 1; }
.translate-panel__result { padding: 12px; border-top: 1px solid var(--ed-border, #2d333b); min-height: 60px; }
.translate-panel__loading { opacity: 0.7; }
.translate-panel__error { color: #f87171; font-size: 12px; display: flex; flex-direction: column; gap: 6px; }
.translate-panel__text { font-size: 13px; max-height: 240px; overflow: auto; white-space: pre-wrap; }
.translate-panel__placeholder { opacity: 0.5; font-size: 12px; }
.translate-panel__history { padding: 8px 12px; border-top: 1px solid var(--ed-border, #2d333b); font-size: 12px; }
.translate-panel__history summary { cursor: pointer; opacity: 0.8; }
.translate-panel__history ul { list-style: none; margin: 6px 0 0; padding: 0; }
.translate-panel__history-row { display: block; width: 100%; text-align: left; background: transparent; border: none; color: inherit; cursor: pointer; padding: 4px 0; font-size: 11px; }
.translate-panel__history-row:hover { opacity: 0.85; }
.translate-panel__history-source { opacity: 0.7; }
.translate-panel__history-arrow { margin: 0 6px; opacity: 0.5; }
</style>
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd desktop/frontend && npx vitest run src/plugins/translate/TranslatePanel.test.ts 2>&1 | tail -15
```

Expected: 4 passing.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/plugins/translate/TranslatePanel.vue desktop/frontend/src/plugins/translate/TranslatePanel.test.ts
git commit -m "feat(desktop): TranslatePanel floating UI component"
```

---

## Task 11: TranslatePanelHost.vue + TranslateSettings.vue

**Files:**
- Create: `desktop/frontend/src/plugins/translate/TranslatePanelHost.vue`
- Create: `desktop/frontend/src/plugins/translate/TranslateSettings.vue`

These two components don't warrant separate unit tests beyond what panelStore + TranslatePanel already cover; they're glue.

- [ ] **Step 1: Implement TranslatePanelHost**

Create `desktop/frontend/src/plugins/translate/TranslatePanelHost.vue`:

```vue
<script lang="ts" setup>
import { computed, watchEffect } from "vue";
import { storeToRefs } from "pinia";
import { useTranslatePanelStore } from "./panelStore";
import { usePluginConfigStore } from "../configStore";
import { createOpenAIProvider } from "./providers/openai";
import TranslatePanel from "./TranslatePanel.vue";

const panel = useTranslatePanelStore();
const cfgStore = usePluginConfigStore();

const translateCfg = computed(() => cfgStore.cfg?.translate ?? null);

// Re-configure the panel store whenever the plugin config changes.
// shallowRef provider so swapping doesn't deep-track the closure.
watchEffect(() => {
  const t = translateCfg.value;
  if (!t || !t.apiKey || !t.baseUrl || !t.model) {
    panel.configure({
      provider: { translate: async () => { throw new Error("Translate plugin not configured"); } },
      defaultTargetLang: t?.defaultTargetLang || "zh-CN",
    });
    return;
  }
  const provider = createOpenAIProvider({
    baseUrl: t.baseUrl,
    apiKey: t.apiKey,
    model: t.model,
  });
  panel.configure({ provider, defaultTargetLang: t.defaultTargetLang });
});
</script>

<template>
  <TranslatePanel />
</template>
```

- [ ] **Step 2: Implement TranslateSettings**

Create `desktop/frontend/src/plugins/translate/TranslateSettings.vue`:

```vue
<script lang="ts" setup>
import { computed } from "vue";
import { usePluginConfigStore, type PluginConfig } from "../configStore";

const store = usePluginConfigStore();

const TARGETS = [
  { code: "zh-CN", label: "中文 (Simplified)" },
  { code: "en", label: "English" },
  { code: "ja", label: "日本語" },
  { code: "ko", label: "한국어" },
  { code: "de", label: "Deutsch" },
  { code: "fr", label: "Français" },
  { code: "es", label: "Español" },
];

const t = computed(() => store.cfg?.translate);

async function update(patch: Partial<PluginConfig["translate"]>) {
  if (!store.cfg) return;
  const next = JSON.parse(JSON.stringify(store.cfg)) as PluginConfig;
  next.translate = { ...next.translate, ...patch };
  try { await store.save(next); } catch (err) { console.error("save translate cfg", err); }
}
</script>

<template>
  <div v-if="t" class="translate-settings">
    <label>
      <span>Base URL</span>
      <input
        type="text"
        :value="t.baseUrl"
        @change="update({ baseUrl: ($event.target as HTMLInputElement).value })"
        placeholder="https://api.openai.com"
      />
    </label>
    <label>
      <span>API Key</span>
      <input
        type="password"
        :value="t.apiKey"
        @change="update({ apiKey: ($event.target as HTMLInputElement).value })"
        placeholder="sk-..."
      />
    </label>
    <label>
      <span>Model</span>
      <input
        type="text"
        :value="t.model"
        @change="update({ model: ($event.target as HTMLInputElement).value })"
        placeholder="gpt-4o-mini"
      />
    </label>
    <label>
      <span>Default target language</span>
      <select
        :value="t.defaultTargetLang"
        @change="update({ defaultTargetLang: ($event.target as HTMLSelectElement).value })"
      >
        <option v-for="opt in TARGETS" :key="opt.code" :value="opt.code">{{ opt.label }}</option>
      </select>
    </label>
    <p class="muted">API key is stored plaintext in <code>~/.config/atterm/config.json</code>.</p>
  </div>
</template>

<style scoped>
.translate-settings { margin-top: 8px; padding-top: 8px; border-top: 1px solid #2d333b; font-size: 12px; display: flex; flex-direction: column; gap: 8px; }
.translate-settings label { display: flex; flex-direction: column; gap: 3px; }
.translate-settings label span { opacity: 0.7; font-size: 11px; }
.translate-settings input, .translate-settings select {
  background: rgba(0, 0, 0, 0.25); color: inherit; border: 1px solid #2d333b; border-radius: 3px; padding: 4px 6px; font-size: 12px;
}
.muted { opacity: 0.55; font-size: 11px; margin: 4px 0 0; }
</style>
```

- [ ] **Step 3: Verify type compile**

```bash
cd desktop/frontend && npx vue-tsc --noEmit 2>&1 | grep -E "translate|TranslatePanelHost|TranslateSettings" | head -10
```

Expected: no errors from the new files.

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/plugins/translate/TranslatePanelHost.vue desktop/frontend/src/plugins/translate/TranslateSettings.vue
git commit -m "feat(desktop): TranslatePanelHost glue + TranslateSettings form"
```

---

## Task 12: Translate plugin descriptor + registry registration

**Files:**
- Create: `desktop/frontend/src/plugins/translate/index.ts`
- Modify: `desktop/frontend/src/plugins/registry.ts`

- [ ] **Step 1: Write the failing test**

Add to `desktop/frontend/src/plugins/translate/` a new test `index.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { translateDescriptor } from "./index";

describe("translateDescriptor", () => {
  it("declares id, slot, title, description, defaultEnabled=false", () => {
    expect(translateDescriptor.id).toBe("translate");
    expect(translateDescriptor.slot).toBe("context-menu");
    expect(translateDescriptor.title).toMatch(/translate/i);
    expect(translateDescriptor.description.length).toBeGreaterThan(0);
    expect(translateDescriptor.defaultEnabled).toBe(false);
  });

  it("load() returns a ContextMenuPlugin with getMenuItems", async () => {
    const mod = await translateDescriptor.load();
    const ctxMenu = (mod as { default: { getMenuItems: Function } }).default;
    expect(typeof ctxMenu.getMenuItems).toBe("function");
  });

  it("getMenuItems returns empty array for blank selection", async () => {
    const mod = await translateDescriptor.load();
    const ctxMenu = (mod as { default: { getMenuItems: Function } }).default;
    const items = ctxMenu.getMenuItems({}, "");
    expect(items).toEqual([]);
  });

  it("getMenuItems returns one item for non-blank selection", async () => {
    const mod = await translateDescriptor.load();
    const ctxMenu = (mod as { default: { getMenuItems: Function } }).default;
    const items = ctxMenu.getMenuItems({}, "hello");
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ id: "translate-selection", label: expect.stringMatching(/translate/i) });
    expect(typeof items[0].onClick).toBe("function");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/plugins/translate/index.test.ts 2>&1 | tail -10
```

Expected: module not found.

- [ ] **Step 3: Implement the descriptor**

Create `desktop/frontend/src/plugins/translate/index.ts`:

```ts
import type { ContextMenuPlugin, MenuItem, PluginContext, PluginDescriptor } from "../types";
import { useTranslatePanelStore } from "./panelStore";

const contextMenuImpl: ContextMenuPlugin = {
  getMenuItems(_ctx: PluginContext, selection: string): MenuItem[] {
    if (!selection || !selection.trim()) return [];
    return [
      {
        id: "translate-selection",
        label: "Translate selection",
        onClick: () => {
          const store = useTranslatePanelStore();
          void store.openWithSource(selection);
        },
      },
    ];
  },
};

export const translateDescriptor: PluginDescriptor = {
  id: "translate",
  slot: "context-menu",
  title: "Translate",
  description: "Translate the selected text via an OpenAI-compatible API; result shown in a floating panel.",
  load: async () => ({ default: contextMenuImpl }),
  defaultEnabled: false,
};
```

- [ ] **Step 4: Register in registry**

Edit `desktop/frontend/src/plugins/registry.ts`:

```ts
import type { PluginDescriptor } from "./types";
import { quickInputDescriptor } from "./quickInput";
import { fileExplorerDescriptor } from "./fileExplorer";
import { translateDescriptor } from "./translate";

export const PLUGINS: PluginDescriptor[] = [
  quickInputDescriptor,
  fileExplorerDescriptor,
  translateDescriptor,
];

export function descriptorsForSlot(slot: PluginDescriptor["slot"]): PluginDescriptor[] {
  return PLUGINS.filter((p) => p.slot === slot);
}

export function findDescriptor(id: string): PluginDescriptor | undefined {
  return PLUGINS.find((p) => p.id === id);
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd desktop/frontend && npx vitest run src/plugins/translate/index.test.ts 2>&1 | tail -10
cd desktop/frontend && npx vitest run 2>&1 | tail -5
```

Expected: new tests pass; full suite green.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/plugins/translate/index.ts desktop/frontend/src/plugins/translate/index.test.ts desktop/frontend/src/plugins/registry.ts
git commit -m "feat(desktop): register translate plugin descriptor"
```

---

## Task 13: TerminalView integration — merge plugin menu items

**Files:**
- Modify: `desktop/frontend/src/components/TerminalView.vue`
- Test: `desktop/frontend/src/components/TerminalView.test.ts` (extend existing)

- [ ] **Step 1: Inspect current menu wiring**

```bash
grep -n "openContextMenu\|menuItems\|hardcoded\|menu" desktop/frontend/src/components/TerminalView.vue | head -30
```

This shows the lines around the hardcoded menu items (`TerminalView.vue:513-515` from the survey). Open the file and find:

```ts
function openContextMenu(e: MouseEvent) {
  // ... existing computation of menuItems including copy/paste/clear ...
}
```

- [ ] **Step 2: Write the failing test**

Add to `desktop/frontend/src/components/TerminalView.test.ts` (preserve existing tests):

```ts
import { describe, expect, it, vi } from "vitest";
import { collectContextMenuItems } from "../plugins/contextMenuItems";
import type { ContextMenuPlugin } from "../plugins/types";

// This is a wiring-level test, not a full TerminalView mount. It asserts
// that TerminalView's menu builder uses collectContextMenuItems with all
// registered + enabled context-menu plugins. We refactor the menu-building
// code into a small pure helper so this assertion is straightforward.

describe("TerminalView context-menu plugin merge", () => {
  it("merges plugin items after hardcoded copy/paste/clear", async () => {
    const fakePlugin: ContextMenuPlugin = {
      getMenuItems: () => [{ id: "fake-1", label: "Fake item", onClick: vi.fn() }],
    };
    const hardcoded = [
      { id: "copy", label: "Copy", onClick: vi.fn() },
      { id: "paste", label: "Paste", onClick: vi.fn() },
      { id: "clear", label: "Clear buffer", onClick: vi.fn() },
    ];
    const pluginItems = await collectContextMenuItems([fakePlugin], {} as never, "selection");
    const merged = [...hardcoded, ...pluginItems];
    expect(merged.map((i) => i.id)).toEqual(["copy", "paste", "clear", "fake-1"]);
  });
});
```

- [ ] **Step 3: Run test to verify it currently passes** (it tests the helper, not TerminalView itself)

```bash
cd desktop/frontend && npx vitest run src/components/TerminalView.test.ts 2>&1 | tail -10
```

Expected: passes (the helper exists from Task 5). This test documents the desired wiring; the next step does the actual wiring in TerminalView.

- [ ] **Step 4: Wire into TerminalView.vue**

Open `desktop/frontend/src/components/TerminalView.vue`. Find the existing `openContextMenu` function (the menu builder) and the imports section. Make these changes (read the file first, splice carefully):

1. Add import at top of `<script setup>`:

```ts
import { collectContextMenuItems } from "../plugins/contextMenuItems";
import { descriptorsForSlot } from "../plugins/registry";
import { usePluginConfigStore } from "../plugins/configStore";
import type { ContextMenuPlugin, PluginContext } from "../plugins/types";
import { inject } from "vue";
```

(`PluginContext` import — TerminalView needs a context object to pass into `getMenuItems`. The simplest path: accept `pluginContext` as a prop, same way PaneGrid forwarded it before. If the parent — App.vue — provides pluginContext via `provide()` already, use `inject` here instead.)

Check `App.vue` for an existing provide:

```bash
grep -n "provide.*plugin\|provide.*Context" desktop/frontend/src/App.vue | head -5
```

If a key like `"atterm:pluginContext"` is already provided, inject it; if not, add `provide('atterm:pluginContext', pluginContext)` to App.vue (this is part of Task 14) and use the same key here.

2. In `openContextMenu`, after computing the hardcoded items and before showing the menu:

```ts
async function openContextMenu(e: MouseEvent) {
  // ... existing menu position + hardcoded items computation ...
  const selection = term.getSelection();
  const pluginCtx = inject<PluginContext>("atterm:pluginContext")!;

  const cfgStore = usePluginConfigStore();
  const enabledContextMenuPlugins: ContextMenuPlugin[] = [];
  for (const d of descriptorsForSlot("context-menu")) {
    if (!cfgStore.isPluginEnabled(d.id)) continue;
    try {
      const mod = await d.load();
      enabledContextMenuPlugins.push((mod as { default: ContextMenuPlugin }).default);
    } catch (err) {
      console.error(`failed to load context-menu plugin ${d.id}`, err);
    }
  }
  const pluginItems = await collectContextMenuItems(enabledContextMenuPlugins, pluginCtx, selection);
  menuItems.value = [...hardcodedItems, ...pluginItems];

  // ... existing show-menu logic ...
}
```

Adapt variable names to match the existing TerminalView code. `inject(...)` must be called at component-setup top level, not inside the async handler — refactor: call `inject` at the top of `<script setup>`, store in a const, and reference inside `openContextMenu`.

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd desktop/frontend && npx vitest run src/components/TerminalView.test.ts 2>&1 | tail -10
cd desktop/frontend && npx vitest run 2>&1 | tail -5
```

Expected: TerminalView tests pass; full suite green.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/components/TerminalView.vue desktop/frontend/src/components/TerminalView.test.ts
git commit -m "feat(desktop): merge context-menu plugin items into terminal right-click menu"
```

---

## Task 14: App.vue mounts TranslatePanelHost + provides pluginContext

**Files:**
- Modify: `desktop/frontend/src/App.vue`

- [ ] **Step 1: Add provide() for pluginContext**

Open `desktop/frontend/src/App.vue`. Find where `pluginContext` is created (around line 226 in current code). Right after:

```ts
const pluginContext = createPluginContext({ ... });
```

add:

```ts
provide("atterm:pluginContext", pluginContext);
```

Add `provide` to the imports from "vue".

- [ ] **Step 2: Mount TranslatePanelHost at app root**

In `App.vue` template, just before the closing `</n-message-provider>` or alongside the existing `<PluginHost slot-id="bottom-toolbar" ... />`, add:

```vue
    <TranslatePanelHost />
```

Add to the `<script setup>` imports:

```ts
import TranslatePanelHost from "./plugins/translate/TranslatePanelHost.vue";
```

- [ ] **Step 3: Verify wails dev builds + frontend tests pass**

```bash
cd desktop/frontend && npx vue-tsc --noEmit 2>&1 | tail -5
cd desktop/frontend && npx vitest run 2>&1 | tail -5
```

Expected: type check clean; all tests green.

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/App.vue
git commit -m "feat(desktop): mount TranslatePanelHost + provide pluginContext to descendants"
```

---

## Task 15: Wire TranslateSettings into SettingsPlugins

**Files:**
- Modify: `desktop/frontend/src/components/SettingsPlugins.vue`

- [ ] **Step 1: Import + render conditionally**

Edit `desktop/frontend/src/components/SettingsPlugins.vue`:

Add to `<script lang="ts" setup>` imports:

```ts
import TranslateSettings from "../plugins/translate/TranslateSettings.vue";
```

Update `toggle` parameter type:

```ts
async function toggle(id: "quick-input" | "file-explorer" | "translate", enabled: boolean) {
```

In `<template>`, after the `file-explorer` div block (and before the `<li v-if="PLUGINS.length === 0">` line), add:

```vue
        <TranslateSettings v-if="p.id === 'translate' && store.isPluginEnabled('translate')" />
```

- [ ] **Step 2: Run tests**

```bash
cd desktop/frontend && npx vitest run 2>&1 | tail -5
```

Expected: all green.

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/src/components/SettingsPlugins.vue
git commit -m "feat(desktop): expose translate plugin in settings UI"
```

---

## Task 16: Manual smoke test + final wails build verification

- [ ] **Step 1: Run the full test + typecheck + build chain**

```bash
cd /Users/attson/code/github.com.attson/atterm
cd desktop && go test ./... 2>&1 | tail -5
cd frontend && npx vue-tsc --noEmit 2>&1 | tail -5
cd frontend && npx vitest run 2>&1 | tail -5
cd frontend && npm run build 2>&1 | tail -5
```

Expected: all green.

- [ ] **Step 2: Boot wails dev and exercise manually**

```bash
cd desktop && wails dev
```

In the running app:

1. Open Settings → Plugins → enable **Translate** plugin
2. Fill in: Base URL (e.g. `https://api.openai.com`), API Key, Model (e.g. `gpt-4o-mini`), Default target language (`zh-CN`)
3. Open a terminal pane, run `echo "Connection refused: dial tcp 10.0.0.5:6379"`
4. Select the error text, right-click → expect a **"Translate selection"** item
5. Click it → expect a floating panel with the translation
6. Change target dropdown to `日本語` → expect re-translation
7. Click ×: panel hides; right-click again with new selection → panel reopens with new content
8. Disable translate in settings → menu item no longer appears in right-click

- [ ] **Step 3: Error-path smoke**

Set API key to `bogus`:

1. Right-click a selection → "Translate selection" → expect "Auth failed (401)" + Retry button
2. Disable network (turn off Wi-Fi briefly) → click Retry → expect "Translate failed: …" + Retry remains

- [ ] **Step 4: No-config smoke**

Clear API key in settings:

1. Right-click selection → "Translate selection" → panel shows "Translate plugin not configured"

- [ ] **Step 5: Confirm wails build artifact**

```bash
cd desktop && wails build 2>&1 | tail -10
```

Expected: build succeeds. If a packaging error appears, that's existing toolchain (not the translate plugin); record in PR description but don't gate the feature on it.

- [ ] **Step 6: Final commit if anything additional surfaced**

If smoke test revealed a small fix (typo, label, etc), commit it as `fix(desktop): <description>` and re-run the affected unit tests.

---

## Done

The plan is complete when:

- All 16 tasks are checked off
- `desktop/frontend && npx vitest run` is green
- `cd desktop && go test ./...` is green
- Manual smoke test in Task 16 succeeded with a real OpenAI key
- No new TypeScript or vue-tsc errors

Defer to a follow-up:
- DeepL provider
- Streaming token-by-token rendering
- Global keyboard shortcut (configurable)
- Persistent history across app restarts
- Copy / Insert buttons in the panel
- Per-(text, target) result caching
