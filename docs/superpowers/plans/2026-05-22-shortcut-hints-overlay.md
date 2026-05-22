# Shortcut hints overlay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "hold-Mod-to-peek" overlay that surfaces the 12 atterm navigation shortcuts after a 3 s long-press of `Cmd` (mac) / `Ctrl` (other), reading the same registry + user overrides as the Settings → Shortcuts tab.

**Architecture:** Pure-function `formatChord` added to `lib/shortcutBindings.ts`. A new composable `useLongPressModifier` owns timer/state and emits `onShow` / `onHide` callbacks. A new `ShortcutHints.vue` overlay owns `visible` state and pulls bindings from `pluginConfigStore`. `App.vue` mounts the overlay once at the top level. The existing `useTerminalShortcuts` is not touched.

**Tech Stack:** Vue 3 + TypeScript + Vitest + jsdom + Pinia.

**Spec:** `docs/superpowers/specs/2026-05-22-shortcut-hints-overlay-design.md`

---

## File map

**New:**
- `desktop/frontend/src/composables/useLongPressModifier.ts` — long-press detector composable
- `desktop/frontend/src/composables/useLongPressModifier.test.ts`
- `desktop/frontend/src/components/ShortcutHints.vue` — centered overlay component
- `desktop/frontend/src/components/ShortcutHints.test.ts`

**Modified:**
- `desktop/frontend/src/lib/shortcutBindings.ts` — append `formatChord`
- `desktop/frontend/src/lib/shortcutBindings.test.ts` — append `formatChord` tests
- `desktop/frontend/src/App.vue` — import `ShortcutHints` and mount it once at the top level

---

## Conventions

- **Working directory:** `/Users/attson/code/github.com.attson/atterm/.claude/worktrees/desktop-shortcut-settings`
- **Run frontend tests:** `cd desktop/frontend && npm run test`
- **Run a single test file:** `cd desktop/frontend && npx vitest run <path>`
- **Type-check + build:** `cd desktop/frontend && npm run build`

---

## Task 1: `formatChord` — chord-string formatter

**Files:**
- Modify: `desktop/frontend/src/lib/shortcutBindings.ts`
- Modify: `desktop/frontend/src/lib/shortcutBindings.test.ts`

- [ ] **Step 1.1: Write the failing tests**

Append to `desktop/frontend/src/lib/shortcutBindings.test.ts`:

```ts
import { formatChord } from "./shortcutBindings";

describe("formatChord (Meta / mac)", () => {
  it("Mod+KeyN -> ⌘N", () => {
    expect(formatChord("Mod+KeyN", "Meta")).toBe("⌘N");
  });
  it("Mod+Alt+Shift+KeyN -> ⌘⌥⇧N", () => {
    expect(formatChord("Mod+Alt+Shift+KeyN", "Meta")).toBe("⌘⌥⇧N");
  });
  it("Mod+Shift+BracketRight -> ⌘⇧]", () => {
    expect(formatChord("Mod+Shift+BracketRight", "Meta")).toBe("⌘⇧]");
  });
  it("Mod+Alt+ArrowLeft -> ⌘⌥←", () => {
    expect(formatChord("Mod+Alt+ArrowLeft", "Meta")).toBe("⌘⌥←");
  });
  it("empty string -> empty string", () => {
    expect(formatChord("", "Meta")).toBe("");
  });
  it("malformed binding (no modifier) -> returns input unchanged", () => {
    expect(formatChord("KeyN", "Meta")).toBe("KeyN");
  });
});

describe("formatChord (Control / non-mac)", () => {
  it("Mod+KeyN -> Ctrl+N", () => {
    expect(formatChord("Mod+KeyN", "Control")).toBe("Ctrl+N");
  });
  it("Mod+Alt+Shift+KeyN -> Ctrl+Alt+Shift+N", () => {
    expect(formatChord("Mod+Alt+Shift+KeyN", "Control")).toBe("Ctrl+Alt+Shift+N");
  });
  it("Mod+Shift+BracketRight -> Ctrl+Shift+]", () => {
    expect(formatChord("Mod+Shift+BracketRight", "Control")).toBe("Ctrl+Shift+]");
  });
  it("Mod+Alt+ArrowLeft -> Ctrl+Alt+←", () => {
    expect(formatChord("Mod+Alt+ArrowLeft", "Control")).toBe("Ctrl+Alt+←");
  });
  it("punctuation codes map to literal characters", () => {
    expect(formatChord("Mod+Minus", "Control")).toBe("Ctrl+-");
    expect(formatChord("Mod+Comma", "Control")).toBe("Ctrl+,");
    expect(formatChord("Mod+Slash", "Control")).toBe("Ctrl+/");
  });
});
```

- [ ] **Step 1.2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/lib/shortcutBindings.test.ts`

Expected: failing on missing export `formatChord`.

- [ ] **Step 1.3: Implement `formatChord`**

Append to `desktop/frontend/src/lib/shortcutBindings.ts`:

```ts
// Maps KeyboardEvent.code values to display characters. Mirrors CODE_WHITELIST
// (above). Keep the two in sync — adding a code to the whitelist requires
// adding it here too.
const CODE_DISPLAY: Record<string, string> = {
  KeyA: "A", KeyB: "B", KeyC: "C", KeyD: "D", KeyE: "E", KeyF: "F", KeyG: "G",
  KeyH: "H", KeyI: "I", KeyJ: "J", KeyK: "K", KeyL: "L", KeyM: "M", KeyN: "N",
  KeyO: "O", KeyP: "P", KeyQ: "Q", KeyR: "R", KeyS: "S", KeyT: "T", KeyU: "U",
  KeyV: "V", KeyW: "W", KeyX: "X", KeyY: "Y", KeyZ: "Z",
  Digit0: "0", Digit1: "1", Digit2: "2", Digit3: "3", Digit4: "4",
  Digit5: "5", Digit6: "6", Digit7: "7", Digit8: "8", Digit9: "9",
  ArrowLeft: "←", ArrowRight: "→", ArrowUp: "↑", ArrowDown: "↓",
  BracketLeft: "[", BracketRight: "]",
  Minus: "-", Equal: "=", Backquote: "`",
  Comma: ",", Period: ".", Slash: "/",
  Semicolon: ";", Quote: "'", Backslash: "\\",
};

// formatChord renders a binding string for human display. On mac (mod === "Meta")
// modifiers use Unicode symbols concatenated with no separator (⌘⌥⇧N). On
// other platforms (mod === "Control") modifiers are written as words joined
// with "+" (Ctrl+Alt+Shift+N). Empty string maps to empty string (the caller
// can render a disabled-state placeholder). Malformed bindings pass through
// unchanged — formatChord is purely cosmetic and never throws.
export function formatChord(binding: string, mod: Mod): string {
  if (binding === "") return "";
  const parsed = parse(binding);
  if (parsed === null || parsed.code === null) return binding;
  const display = CODE_DISPLAY[parsed.code] ?? parsed.code;
  if (mod === "Meta") {
    let out = "";
    if (parsed.mod) out += "⌘";
    if (parsed.alt) out += "⌥";
    if (parsed.shift) out += "⇧";
    return out + display;
  }
  const parts: string[] = [];
  if (parsed.mod) parts.push("Ctrl");
  if (parsed.alt) parts.push("Alt");
  if (parsed.shift) parts.push("Shift");
  parts.push(display);
  return parts.join("+");
}
```

- [ ] **Step 1.4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/lib/shortcutBindings.test.ts`

Expected: all pass (existing tests + the new `formatChord` group).

- [ ] **Step 1.5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/desktop-shortcut-settings
git add desktop/frontend/src/lib/shortcutBindings.ts desktop/frontend/src/lib/shortcutBindings.test.ts
git commit -m "feat(desktop): formatChord for human-readable shortcut display"
```

---

## Task 2: `useLongPressModifier` composable

**Files:**
- Create: `desktop/frontend/src/composables/useLongPressModifier.ts`
- Create: `desktop/frontend/src/composables/useLongPressModifier.test.ts`

- [ ] **Step 2.1: Write the failing tests**

Create `desktop/frontend/src/composables/useLongPressModifier.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { effectScope } from "vue";
import { useLongPressModifier } from "./useLongPressModifier";

function fireKey(type: "keydown" | "keyup", init: KeyboardEventInit & { key: string }) {
  const ev = new KeyboardEvent(type, { ...init, bubbles: true, cancelable: true });
  document.dispatchEvent(ev);
  return ev;
}

function fireBlur() {
  window.dispatchEvent(new Event("blur"));
}

describe("useLongPressModifier", () => {
  let scope: ReturnType<typeof effectScope>;
  const onShow = vi.fn();
  const onHide = vi.fn();

  beforeEach(() => {
    vi.useFakeTimers();
    onShow.mockReset();
    onHide.mockReset();
    scope = effectScope();
    scope.run(() => useLongPressModifier({ mod: "Control", thresholdMs: 3000, onShow, onHide }));
  });

  afterEach(() => {
    scope.stop();
    vi.useRealTimers();
  });

  it("3s hold of Control -> onShow", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(3000);
    expect(onShow).toHaveBeenCalledTimes(1);
    expect(onHide).not.toHaveBeenCalled();
  });

  it("release before 3s -> neither callback fires", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(2500);
    fireKey("keyup", { key: "Control" });
    expect(onShow).not.toHaveBeenCalled();
    expect(onHide).not.toHaveBeenCalled();
  });

  it("press non-modifier before 3s -> timer canceled, onShow not called", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(1000);
    fireKey("keydown", { key: "n", code: "KeyN", ctrlKey: true });
    vi.advanceTimersByTime(3000);
    expect(onShow).not.toHaveBeenCalled();
  });

  it("press non-modifier after 3s -> onHide fires", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(3000);
    expect(onShow).toHaveBeenCalledTimes(1);
    fireKey("keydown", { key: "n", code: "KeyN", ctrlKey: true });
    expect(onHide).toHaveBeenCalledTimes(1);
  });

  it("release after 3s -> onHide fires", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(3000);
    fireKey("keyup", { key: "Control" });
    expect(onShow).toHaveBeenCalledTimes(1);
    expect(onHide).toHaveBeenCalledTimes(1);
  });

  it("window blur after 3s -> onHide fires", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(3000);
    fireBlur();
    expect(onHide).toHaveBeenCalledTimes(1);
  });

  it("e.repeat=true Control while no timer -> no timer started", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true, repeat: true });
    vi.advanceTimersByTime(3000);
    expect(onShow).not.toHaveBeenCalled();
  });

  it("e.repeat=true while timer running -> timer keeps running", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(1500);
    fireKey("keydown", { key: "Control", ctrlKey: true, repeat: true });
    vi.advanceTimersByTime(1500);
    expect(onShow).toHaveBeenCalledTimes(1);
  });

  it("Alt joining before 3s cancels the timer", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(1000);
    fireKey("keydown", { key: "Alt", ctrlKey: true, altKey: true });
    vi.advanceTimersByTime(3000);
    expect(onShow).not.toHaveBeenCalled();
  });

  it("Control press while Alt already held does not start timer", () => {
    fireKey("keydown", { key: "Control", ctrlKey: true, altKey: true });
    vi.advanceTimersByTime(3000);
    expect(onShow).not.toHaveBeenCalled();
  });

  it("Mod=Meta variant: 3s Meta hold -> onShow", () => {
    scope.stop();
    onShow.mockReset();
    onHide.mockReset();
    scope = effectScope();
    scope.run(() => useLongPressModifier({ mod: "Meta", thresholdMs: 3000, onShow, onHide }));
    fireKey("keydown", { key: "Meta", metaKey: true });
    vi.advanceTimersByTime(3000);
    expect(onShow).toHaveBeenCalledTimes(1);
  });

  it("scope.stop unbinds all listeners", () => {
    scope.stop();
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(3000);
    expect(onShow).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2.2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/composables/useLongPressModifier.test.ts`

Expected: failing — module not found.

- [ ] **Step 2.3: Implement the composable**

Create `desktop/frontend/src/composables/useLongPressModifier.ts`:

```ts
// useLongPressModifier — detect a "pure long-press" of the platform mod key
// (Meta on mac, Control elsewhere) and emit show/hide callbacks. "Pure" means
// no other modifier is co-held and no non-modifier key is pressed during the
// hold window. Used to drive the shortcut-hints overlay.
//
// Listeners run in capture phase so they observe events even when other
// capture listeners (e.g. useTerminalShortcuts) stopPropagation()-ate normal
// chords — capture-phase handlers all fire before bubble-phase ones; calling
// stopPropagation on a different capture listener does not affect ours.

import { onScopeDispose } from "vue";
import type { Mod } from "../lib/shortcutBindings";

export interface LongPressOptions {
  mod: Mod;
  thresholdMs?: number;
  onShow: () => void;
  onHide: () => void;
}

export function useLongPressModifier(opts: LongPressOptions): void {
  const threshold = opts.thresholdMs ?? 3000;
  const modKeyName: "Meta" | "Control" = opts.mod;

  let timer: number | null = null;
  let showing = false;

  function clearTimer() {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  }

  function hideIfShowing() {
    if (showing) {
      showing = false;
      opts.onHide();
    }
  }

  function fireShow() {
    timer = null;
    showing = true;
    opts.onShow();
  }

  // Returns true if this keydown event represents the platform mod key being
  // pressed without any other modifier already held.
  function isPureModKeydown(e: KeyboardEvent): boolean {
    if (e.key !== modKeyName) return false;
    // The mod-down keydown reports its own flag as true (e.ctrlKey for
    // Control, e.metaKey for Meta). The other modifiers must all be false —
    // otherwise the user is building a chord like Ctrl+Alt.
    if (e.altKey) return false;
    if (e.shiftKey) return false;
    // wrong-platform modifier held (e.g. Meta on a Control platform) also
    // disqualifies — the user is doing something layered.
    const wrongModifier = modKeyName === "Meta" ? e.ctrlKey : e.metaKey;
    if (wrongModifier) return false;
    return true;
  }

  function onKeydown(e: KeyboardEvent) {
    if (isPureModKeydown(e)) {
      if (e.repeat) return;        // OS auto-repeat: keep state as-is
      if (timer !== null) return;  // already counting
      if (showing) return;         // already shown (shouldn't happen since
                                   // keydown of the mod that's already held
                                   // arrives with repeat=true, but defensive)
      timer = setTimeout(fireShow, threshold) as unknown as number;
      return;
    }
    // Any other keydown — cancel pending timer, hide if showing.
    clearTimer();
    hideIfShowing();
  }

  function onKeyup(e: KeyboardEvent) {
    if (e.key !== modKeyName) return;
    clearTimer();
    hideIfShowing();
  }

  function onBlur() {
    clearTimer();
    hideIfShowing();
  }

  document.addEventListener("keydown", onKeydown, { capture: true });
  document.addEventListener("keyup", onKeyup, { capture: true });
  window.addEventListener("blur", onBlur);

  onScopeDispose(() => {
    clearTimer();
    document.removeEventListener("keydown", onKeydown, { capture: true } as EventListenerOptions);
    document.removeEventListener("keyup", onKeyup, { capture: true } as EventListenerOptions);
    window.removeEventListener("blur", onBlur);
  });
}
```

- [ ] **Step 2.4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/composables/useLongPressModifier.test.ts`

Expected: all pass.

- [ ] **Step 2.5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/desktop-shortcut-settings
git add desktop/frontend/src/composables/useLongPressModifier.ts desktop/frontend/src/composables/useLongPressModifier.test.ts
git commit -m "feat(desktop): useLongPressModifier composable"
```

---

## Task 3: `ShortcutHints.vue` overlay component

**Files:**
- Create: `desktop/frontend/src/components/ShortcutHints.vue`
- Create: `desktop/frontend/src/components/ShortcutHints.test.ts`

- [ ] **Step 3.1: Write the failing tests**

Create `desktop/frontend/src/components/ShortcutHints.test.ts`:

```ts
import { describe, it, expect, beforeEach, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { setActivePinia, createPinia } from "pinia";
import ShortcutHints from "./ShortcutHints.vue";
import { usePluginConfigStore } from "../plugins/configStore";

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

function fireKey(type: "keydown" | "keyup", init: KeyboardEventInit & { key: string }) {
  document.dispatchEvent(new KeyboardEvent(type, { ...init, bubbles: true, cancelable: true }));
}

describe("ShortcutHints", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  it("renders nothing initially (overlay hidden)", async () => {
    await setupStore({});
    const wrapper = mount(ShortcutHints, { props: { mod: "Control", thresholdMs: 100 } });
    await flushPromises();
    expect(wrapper.find(".hints-backdrop").exists()).toBe(false);
  });

  it("after 100ms long-press of Control, shows 12 rows in 2 groups with Ctrl+* chords", async () => {
    await setupStore({});
    const wrapper = mount(ShortcutHints, { props: { mod: "Control", thresholdMs: 100 }, attachTo: document.body });
    await flushPromises();
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(100);
    await flushPromises();
    expect(wrapper.find(".hints-backdrop").exists()).toBe(true);
    const rows = wrapper.findAll(".hint-row");
    expect(rows).toHaveLength(12);
    expect(wrapper.text()).toContain("Pane");
    expect(wrapper.text()).toContain("Tab");
    expect(wrapper.text()).toContain("Ctrl+N");
    expect(wrapper.text()).toContain("Ctrl+T");
    wrapper.unmount();
  });

  it("mac variant uses ⌘ symbols", async () => {
    await setupStore({});
    const wrapper = mount(ShortcutHints, { props: { mod: "Meta", thresholdMs: 100 }, attachTo: document.body });
    await flushPromises();
    fireKey("keydown", { key: "Meta", metaKey: true });
    vi.advanceTimersByTime(100);
    await flushPromises();
    expect(wrapper.text()).toContain("⌘N");
    expect(wrapper.text()).toContain("⌘T");
    wrapper.unmount();
  });

  it("releasing the mod hides the overlay", async () => {
    await setupStore({});
    const wrapper = mount(ShortcutHints, { props: { mod: "Control", thresholdMs: 100 }, attachTo: document.body });
    await flushPromises();
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(100);
    await flushPromises();
    expect(wrapper.find(".hints-backdrop").exists()).toBe(true);
    fireKey("keyup", { key: "Control" });
    await flushPromises();
    expect(wrapper.find(".hints-backdrop").exists()).toBe(false);
    wrapper.unmount();
  });

  it("disabled action renders em-dash and dimmed class", async () => {
    await setupStore({ "pane.close": "" });
    const wrapper = mount(ShortcutHints, { props: { mod: "Control", thresholdMs: 100 }, attachTo: document.body });
    await flushPromises();
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(100);
    await flushPromises();
    const disabledRow = wrapper.findAll(".hint-row").find((r) => r.text().includes("Close pane"));
    expect(disabledRow).toBeTruthy();
    expect(disabledRow!.classes()).toContain("disabled");
    expect(disabledRow!.text()).toContain("—");
    wrapper.unmount();
  });

  it("user-overridden binding is rendered using formatChord", async () => {
    await setupStore({ "tab.new": "Mod+KeyL" });
    const wrapper = mount(ShortcutHints, { props: { mod: "Control", thresholdMs: 100 }, attachTo: document.body });
    await flushPromises();
    fireKey("keydown", { key: "Control", ctrlKey: true });
    vi.advanceTimersByTime(100);
    await flushPromises();
    expect(wrapper.text()).toContain("Ctrl+L");
    expect(wrapper.text()).not.toContain("Ctrl+T");
    wrapper.unmount();
  });
});
```

- [ ] **Step 3.2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/components/ShortcutHints.test.ts`

Expected: failing — module not found.

- [ ] **Step 3.3: Create the component**

Create `desktop/frontend/src/components/ShortcutHints.vue`:

```vue
<script lang="ts" setup>
import { computed, ref } from "vue";
import { usePluginConfigStore } from "../plugins/configStore";
import {
  ACTIONS,
  formatChord,
  resolvedBindings,
  type Mod,
  type ShortcutAction,
} from "../lib/shortcutBindings";
import { useLongPressModifier } from "../composables/useLongPressModifier";

function detectMod(): Mod {
  if (typeof navigator === "undefined") return "Control";
  return navigator.platform?.toLowerCase().includes("mac") ? "Meta" : "Control";
}

const props = defineProps<{
  mod?: Mod;
  thresholdMs?: number;
}>();

const mod: Mod = props.mod ?? detectMod();
const store = usePluginConfigStore();
const visible = ref(false);

useLongPressModifier({
  mod,
  thresholdMs: props.thresholdMs ?? 3000,
  onShow: () => { visible.value = true; },
  onHide: () => { visible.value = false; },
});

const resolved = computed(() => resolvedBindings(store.cfg?.shortcuts?.bindings ?? {}));

const paneActions = ACTIONS.filter((a) => a.group === "pane");
const tabActions = ACTIONS.filter((a) => a.group === "tab");

function chordFor(action: ShortcutAction): string {
  return formatChord(resolved.value[action.id] ?? "", mod);
}
function isDisabled(action: ShortcutAction): boolean {
  return (resolved.value[action.id] ?? "") === "";
}
</script>

<template>
  <Transition name="fade">
    <div v-if="visible" class="hints-backdrop">
      <div class="hints-panel" role="dialog" aria-label="Keyboard Shortcuts">
        <div class="hints-header">Keyboard Shortcuts</div>

        <section class="hints-group">
          <h3>Pane</h3>
          <div
            v-for="action in paneActions"
            :key="action.id"
            class="hint-row"
            :class="{ disabled: isDisabled(action) }"
          >
            <div class="chord">{{ isDisabled(action) ? "—" : chordFor(action) }}</div>
            <div class="label">{{ action.label }}</div>
          </div>
        </section>

        <section class="hints-group">
          <h3>Tab</h3>
          <div
            v-for="action in tabActions"
            :key="action.id"
            class="hint-row"
            :class="{ disabled: isDisabled(action) }"
          >
            <div class="chord">{{ isDisabled(action) ? "—" : chordFor(action) }}</div>
            <div class="label">{{ action.label }}</div>
          </div>
        </section>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.hints-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 200;
  pointer-events: none;
}
.hints-panel {
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 20px 24px;
  width: 480px;
  max-width: calc(100vw - 32px);
  max-height: calc(100vh - 32px);
  overflow-y: auto;
  color: var(--fg);
  pointer-events: auto;
}
.hints-header {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--fg-dim);
  margin-bottom: 12px;
}
.hints-group { margin-bottom: 14px; }
.hints-group:last-child { margin-bottom: 0; }
.hints-group h3 {
  margin: 0 0 6px;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--fg-dim);
}
.hint-row {
  display: grid;
  grid-template-columns: 120px 1fr;
  gap: 12px;
  padding: 3px 0;
  font-size: 12px;
}
.hint-row.disabled { color: var(--fg-dim); }
.chord {
  font-family: "SF Mono", Menlo, monospace;
  text-align: right;
}
.label { color: inherit; }

.fade-enter-active, .fade-leave-active { transition: opacity 100ms ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
```

- [ ] **Step 3.4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/components/ShortcutHints.test.ts`

Expected: all pass.

- [ ] **Step 3.5: Type-check the frontend**

Run: `cd desktop/frontend && npm run build`

Expected: build succeeds.

- [ ] **Step 3.6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/desktop-shortcut-settings
git add desktop/frontend/src/components/ShortcutHints.vue desktop/frontend/src/components/ShortcutHints.test.ts
git commit -m "feat(desktop): shortcut hints overlay component"
```

---

## Task 4: mount `<ShortcutHints />` in `App.vue`

**Files:**
- Modify: `desktop/frontend/src/App.vue`

- [ ] **Step 4.1: Find existing imports and top-level mount points**

Run: `grep -n "import SettingsDialog\|ConfirmQuitDialog\|^</template>" /Users/attson/code/github.com.attson/atterm/.claude/worktrees/desktop-shortcut-settings/desktop/frontend/src/App.vue | head -10`

Expected output: lines for the `SettingsDialog` import (~9), `ConfirmQuitDialog` import (~12), and the closing `</template>` (~850). Use these to anchor the edits below.

- [ ] **Step 4.2: Add the import**

Edit `desktop/frontend/src/App.vue`. After the line `import ConfirmQuitDialog from "./components/ConfirmQuitDialog.vue";` (around line 12), add:

```ts
import ShortcutHints from "./components/ShortcutHints.vue";
```

- [ ] **Step 4.3: Mount the overlay**

In the template, find the `<ConfirmQuitDialog ... />` block (around lines 842-848). Immediately after its closing `/>`, and before the parent `</div>` that closes the `.app` wrapper, add:

```html
    <ShortcutHints />
```

The exact insertion: replace

```html
    <ConfirmQuitDialog
      v-if="quitDialogOpen"
      :local-count="localSessionCount"
      :remote-count="remoteSessionCount"
      @confirm="onConfirmQuit"
      @cancel="onCancelQuit"
    />
  </div>
```

with

```html
    <ConfirmQuitDialog
      v-if="quitDialogOpen"
      :local-count="localSessionCount"
      :remote-count="remoteSessionCount"
      @confirm="onConfirmQuit"
      @cancel="onCancelQuit"
    />
    <ShortcutHints />
  </div>
```

- [ ] **Step 4.4: Type-check + full test sweep**

Run: `cd desktop/frontend && npm run build`

Expected: build succeeds.

Run: `cd desktop/frontend && npm run test`

Expected: all tests pass (419+ tests).

- [ ] **Step 4.5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/desktop-shortcut-settings
git add desktop/frontend/src/App.vue
git commit -m "feat(desktop): mount shortcut hints overlay in app"
```

---

## Task 5: end-to-end manual verification

**Files:** none (manual smoke test in the running desktop app)

- [ ] **Step 5.1: Launch the app**

Run: `cd /Users/attson/code/github.com.attson/atterm/.claude/worktrees/desktop-shortcut-settings/desktop && wails dev`

Wait for the window to appear and a session to load.

- [ ] **Step 5.2: Default 3 s long-press**

Hold `⌘` (mac) or `Ctrl` (other) for 3 seconds without pressing anything else.

Expected: a centered overlay appears with 12 rows. Release the key — overlay disappears.

- [ ] **Step 5.3: Cancel by chord**

Hold `⌘`. Before 3 s elapses, press `N` (forming `⌘N` which splits a pane). Expected: pane splits as normal; overlay does NOT appear.

- [ ] **Step 5.4: Dismiss by pressing a key while shown**

Long-press `⌘` until the overlay appears. Then press `T` while still holding `⌘`. Expected: overlay disappears immediately; the `⌘T` chord opens a new tab.

- [ ] **Step 5.5: Disabled action shows em-dash**

Open Settings → Shortcuts. Set "Close pane" to disabled (Backspace in the capture cell), Save. Close Settings. Long-press `⌘` for 3 s. Expected: the "Close pane" row shows `—` in dim color.

Restore via Reset all to defaults before exiting.

- [ ] **Step 5.6: Edited binding reflects in overlay**

Open Settings → Shortcuts. Set "New tab" to `⌘L`, Save. Close Settings. Long-press `⌘` for 3 s. Expected: the "New tab" row shows `⌘L` (not `⌘T`).

Restore to default.

- [ ] **Step 5.7: Window blur dismisses**

Long-press `⌘` until overlay shows. Switch to a different app (Cmd-Tab or click another window). Switch back. Expected: overlay is gone; settings/state is unchanged.

---

## Closing

After Task 5 passes the manual checks, the feature is ready. Push commits to the existing PR branch — they will join the open #67.
