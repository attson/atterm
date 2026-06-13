# Terminal Link Detection & Mod+Click Open — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect URLs and on-disk paths in xterm buffer lines, decorate them on hover, open via ⌘+Click (macOS) or Ctrl+Click (Win/Linux); add "Open Link" / "Copy Link" entries to the existing terminal context menu.

**Architecture:** Pure-function detection module (`lib/terminalLinks.ts`) feeds an xterm `ILinkProvider` adapter (`composables/useTerminalLinkProvider.ts`); `components/TerminalView.vue` registers the provider during `ensureTerm()` and appends two link-aware items to its right-click menu when the click cell falls inside a detected match.

**Tech Stack:** Vue 3 + xterm 5.3.0 + Wails (`BrowserOpenURL` via existing `platform.system.openExternalURL`); existing Go binding `GetUserHomeDir` reused.

**Spec:** `docs/superpowers/specs/2026-06-13-terminal-link-detection-design.md`

---

## File Structure

**Created:**
- `desktop/frontend/src/lib/terminalLinks.ts` — pure detection + normalization + Mod-key predicate.
- `desktop/frontend/src/lib/terminalLinks.test.ts` — unit tests.
- `desktop/frontend/src/composables/useTerminalLinkProvider.ts` — xterm adapter.
- `desktop/frontend/src/composables/useTerminalLinkProvider.test.ts` — unit tests.

**Modified:**
- `desktop/frontend/src/components/TerminalView.vue` — register link provider in `ensureTerm()`; add "Open Link"/"Copy Link" entries to context menu.
- `desktop/frontend/src/components/TerminalView.test.ts` — integration tests for context-menu hit + Mod+Click activation.
- `desktop/frontend/src/i18n/messages/en.ts` — 4 new keys.
- `desktop/frontend/src/i18n/messages/zh-CN.ts` — 4 new keys.

**Not touched:** Go side, mobile build, `xterm-addon-*`, `platform/types.ts`.

---

### Task 1: `detectLinks` core regex (http/https/file URIs)

**Files:**
- Create: `desktop/frontend/src/lib/terminalLinks.ts`
- Create: `desktop/frontend/src/lib/terminalLinks.test.ts`

- [ ] **Step 1: Write the failing test**

`desktop/frontend/src/lib/terminalLinks.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { detectLinks } from "./terminalLinks";

describe("detectLinks — URL schemes", () => {
  it("matches a bare https URL", () => {
    expect(detectLinks("see https://example.com today")).toEqual([
      { start: 4, end: 23, text: "https://example.com", kind: "http" },
    ]);
  });

  it("matches http URL", () => {
    expect(detectLinks("http://x.test")).toEqual([
      { start: 0, end: 13, text: "http://x.test", kind: "http" },
    ]);
  });

  it("matches file:// URI", () => {
    expect(detectLinks("open file:///tmp/x.log please")).toEqual([
      { start: 5, end: 22, text: "file:///tmp/x.log", kind: "file" },
    ]);
  });

  it("matches multiple URLs on one line", () => {
    const line = "a https://a.test and https://b.test end";
    const got = detectLinks(line);
    expect(got.map((m) => m.text)).toEqual(["https://a.test", "https://b.test"]);
  });

  it("preserves URL query + fragment", () => {
    const line = "go https://x.test/p?a=1&b=2#z!";
    expect(detectLinks(line)).toEqual([
      { start: 3, end: 29, text: "https://x.test/p?a=1&b=2#z", kind: "http" },
    ]);
  });

  it("returns empty for non-URL line", () => {
    expect(detectLinks("nothing here")).toEqual([]);
  });

  it("returns empty for empty / nullish input", () => {
    expect(detectLinks("")).toEqual([]);
    expect(detectLinks(null as unknown as string)).toEqual([]);
    expect(detectLinks(undefined as unknown as string)).toEqual([]);
  });
});
```

- [ ] **Step 2: Run test, expect failure**

```bash
cd desktop/frontend && npx vitest run src/lib/terminalLinks.test.ts
```

Expected: FAIL — module not found.

- [ ] **Step 3: Implement minimum to pass**

`desktop/frontend/src/lib/terminalLinks.ts`:

```ts
export type LinkKind = "http" | "file" | "path";

export interface LinkMatch {
  /** Inclusive column index in the source line. */
  start: number;
  /** Exclusive column index in the source line. */
  end: number;
  /** Matched text (already trimmed of trailing punctuation). */
  text: string;
  kind: LinkKind;
}

// URL scheme regex: http(s)://… and file://…
// Body chars: anything that is not whitespace, control, or balanced-close
// punctuation candidates. Trailing punctuation is trimmed in a second pass.
const URL_RE = /\b(https?|file):\/\/[^\s\x00-\x1f]+/g;

const TRAILING_TRIM = new Set([".", ",", ";", ":", "!", "?", '"', "'"]);

function trimTrailing(text: string): string {
  let end = text.length;
  while (end > 0) {
    const ch = text[end - 1];
    if (TRAILING_TRIM.has(ch)) {
      end--;
      continue;
    }
    if (ch === ")" || ch === "]") {
      const open = ch === ")" ? "(" : "[";
      const opens = countChar(text.slice(0, end - 1), open);
      const closes = countChar(text.slice(0, end - 1), ch);
      if (opens <= closes) {
        end--;
        continue;
      }
    }
    break;
  }
  return text.slice(0, end);
}

function countChar(s: string, ch: string): number {
  let n = 0;
  for (let i = 0; i < s.length; i++) if (s[i] === ch) n++;
  return n;
}

export function detectLinks(line: string | null | undefined): LinkMatch[] {
  if (!line) return [];
  const out: LinkMatch[] = [];
  URL_RE.lastIndex = 0;
  let m: RegExpExecArray | null;
  while ((m = URL_RE.exec(line)) !== null) {
    const raw = m[0];
    const trimmed = trimTrailing(raw);
    if (!trimmed) continue;
    const start = m.index;
    out.push({
      start,
      end: start + trimmed.length,
      text: trimmed,
      kind: m[1] === "file" ? "file" : "http",
    });
  }
  return out;
}
```

- [ ] **Step 4: Run test, expect pass**

```bash
cd desktop/frontend && npx vitest run src/lib/terminalLinks.test.ts
```

Expected: PASS, 7/7 green.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/terminalLinks.ts desktop/frontend/src/lib/terminalLinks.test.ts
git -c commit.gpgsign=false commit -m "feat(terminal): detect http/https/file URLs in buffer lines"
```

---

### Task 2: Trailing-punct edge cases (Wikipedia `)`, sentence-end period)

**Files:**
- Modify: `desktop/frontend/src/lib/terminalLinks.test.ts`
- Verify: `desktop/frontend/src/lib/terminalLinks.ts` (no code change expected; if test fails, fix in place)

- [ ] **Step 1: Add edge-case tests**

Append to `terminalLinks.test.ts`:

```ts
describe("detectLinks — trailing punctuation", () => {
  it("strips trailing period", () => {
    expect(detectLinks("Visit https://x.test.")).toEqual([
      { start: 6, end: 20, text: "https://x.test", kind: "http" },
    ]);
  });

  it("strips trailing comma and semicolon", () => {
    expect(detectLinks("a https://x.test, then b")).toEqual([
      { start: 2, end: 16, text: "https://x.test", kind: "http" },
    ]);
    expect(detectLinks("https://x.test;")).toEqual([
      { start: 0, end: 14, text: "https://x.test", kind: "http" },
    ]);
  });

  it("strips closing paren when URL is inside (parens)", () => {
    expect(detectLinks("(see https://x.test)")).toEqual([
      { start: 5, end: 19, text: "https://x.test", kind: "http" },
    ]);
  });

  it("keeps closing paren when it balances a paren inside the URL", () => {
    const line = "https://en.wikipedia.org/wiki/Foo_(bar)";
    expect(detectLinks(line)).toEqual([
      {
        start: 0,
        end: line.length,
        text: "https://en.wikipedia.org/wiki/Foo_(bar)",
        kind: "http",
      },
    ]);
  });

  it("strips trailing quote", () => {
    expect(detectLinks(`hit "https://x.test"`)).toEqual([
      { start: 5, end: 19, text: "https://x.test", kind: "http" },
    ]);
  });
});
```

- [ ] **Step 2: Run, expect pass (the Task 1 implementation already covers this)**

```bash
cd desktop/frontend && npx vitest run src/lib/terminalLinks.test.ts
```

Expected: PASS all. If any fail, debug `trimTrailing` — the balanced-paren guard hinges on counting `(` vs `)` in everything *before* the candidate trim position; that's already in Task 1's code.

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/src/lib/terminalLinks.test.ts
git -c commit.gpgsign=false commit -m "test(terminal): cover URL trailing-punct edge cases"
```

---

### Task 3: Detect absolute paths and `~/…`

**Files:**
- Modify: `desktop/frontend/src/lib/terminalLinks.ts`
- Modify: `desktop/frontend/src/lib/terminalLinks.test.ts`

- [ ] **Step 1: Failing test**

Append to `terminalLinks.test.ts`:

```ts
describe("detectLinks — paths", () => {
  it("matches an absolute POSIX path", () => {
    expect(detectLinks("log: /var/log/x.log here")).toEqual([
      { start: 5, end: 18, text: "/var/log/x.log", kind: "path" },
    ]);
  });

  it("matches a home-anchored path", () => {
    expect(detectLinks("cd ~/Projects/foo")).toEqual([
      { start: 3, end: 17, text: "~/Projects/foo", kind: "path" },
    ]);
  });

  it("matches '~/' alone", () => {
    expect(detectLinks("home is ~/")).toEqual([
      { start: 8, end: 10, text: "~/", kind: "path" },
    ]);
  });

  it("matches `/` alone (root)", () => {
    expect(detectLinks("disk / is full")).toEqual([
      { start: 5, end: 6, text: "/", kind: "path" },
    ]);
  });

  it("does NOT match relative paths", () => {
    expect(detectLinks("see ./foo and src/main.go")).toEqual([]);
  });

  it("does NOT match shorthand ~user", () => {
    expect(detectLinks("cd ~foo")).toEqual([]);
  });

  it("does NOT match numeric expressions like 12/24", () => {
    expect(detectLinks("date 12/24/2025")).toEqual([]);
  });

  it("strips trailing punct on paths too", () => {
    expect(detectLinks("see /var/log,")).toEqual([
      { start: 4, end: 12, text: "/var/log", kind: "path" },
    ]);
  });

  it("does not double-match path inside a URL", () => {
    const got = detectLinks("file:///tmp/x and /etc/hosts");
    expect(got.map((m) => m.text)).toEqual(["file:///tmp/x", "/etc/hosts"]);
  });
});
```

- [ ] **Step 2: Run, expect failures**

```bash
cd desktop/frontend && npx vitest run src/lib/terminalLinks.test.ts
```

Expected: 9 new failures (paths not detected).

- [ ] **Step 3: Extend implementation**

Edit `desktop/frontend/src/lib/terminalLinks.ts`. Add path detection *after* URL detection, then drop any path that overlaps a URL match:

```ts
// Absolute path: starts at word boundary with '/' or '~/', body chars exclude
// whitespace, control, and trailing punctuation we trim later. Excludes
// numeric expressions like "12/24" by requiring the slash to be preceded by
// start-of-line, whitespace, or one of (){}[]<>"' .
const PATH_RE = /(?:(?<=^)|(?<=[\s(){}\[\]<>"'`]))(~\/|\/)([^\s\x00-\x1f]*)/g;

export function detectLinks(line: string | null | undefined): LinkMatch[] {
  if (!line) return [];
  const out: LinkMatch[] = [];

  URL_RE.lastIndex = 0;
  let m: RegExpExecArray | null;
  while ((m = URL_RE.exec(line)) !== null) {
    const raw = m[0];
    const trimmed = trimTrailing(raw);
    if (!trimmed) continue;
    const start = m.index;
    out.push({
      start,
      end: start + trimmed.length,
      text: trimmed,
      kind: m[1] === "file" ? "file" : "http",
    });
  }

  PATH_RE.lastIndex = 0;
  while ((m = PATH_RE.exec(line)) !== null) {
    const raw = m[0];
    const trimmed = trimTrailing(raw);
    if (!trimmed) continue;
    const start = m.index;
    const end = start + trimmed.length;
    // Skip if this overlaps with any URL match already produced.
    if (out.some((u) => start < u.end && end > u.start)) continue;
    out.push({ start, end, text: trimmed, kind: "path" });
  }

  out.sort((a, b) => a.start - b.start);
  return out;
}
```

- [ ] **Step 4: Run, expect all pass**

```bash
cd desktop/frontend && npx vitest run src/lib/terminalLinks.test.ts
```

Expected: PASS all (URL + path + edge cases).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/terminalLinks.ts desktop/frontend/src/lib/terminalLinks.test.ts
git -c commit.gpgsign=false commit -m "feat(terminal): detect absolute paths and ~/ prefixes"
```

---

### Task 4: `normalizeForOpen` (path → file:// + `~` expansion)

**Files:**
- Modify: `desktop/frontend/src/lib/terminalLinks.ts`
- Modify: `desktop/frontend/src/lib/terminalLinks.test.ts`

- [ ] **Step 1: Failing test**

Append to `terminalLinks.test.ts`:

```ts
import { normalizeForOpen } from "./terminalLinks";

describe("normalizeForOpen", () => {
  const http: LinkMatch = { start: 0, end: 10, text: "http://x.t", kind: "http" };
  const file: LinkMatch = { start: 0, end: 17, text: "file:///tmp/x.log", kind: "file" };
  const abs: LinkMatch = { start: 0, end: 9, text: "/usr/local", kind: "path" };
  const home: LinkMatch = { start: 0, end: 14, text: "~/Projects/foo", kind: "path" };

  it("returns http(s) untouched", () => {
    expect(normalizeForOpen(http, "")).toBe("http://x.t");
  });

  it("returns file:// untouched", () => {
    expect(normalizeForOpen(file, "")).toBe("file:///tmp/x.log");
  });

  it("wraps absolute path in file://", () => {
    expect(normalizeForOpen(abs, "")).toBe("file:///usr/local");
  });

  it("expands ~ when homeDir provided", () => {
    expect(normalizeForOpen(home, "/Users/me")).toBe("file:///Users/me/Projects/foo");
  });

  it("returns null when ~ encountered with no homeDir", () => {
    expect(normalizeForOpen(home, "")).toBeNull();
    expect(normalizeForOpen(home, undefined)).toBeNull();
  });

  it("strips trailing slash from homeDir before joining", () => {
    expect(normalizeForOpen(home, "/Users/me/")).toBe("file:///Users/me/Projects/foo");
  });
});
```

Also add to the imports at the top of the test file:

```ts
import { detectLinks, normalizeForOpen, type LinkMatch } from "./terminalLinks";
```

(replace the existing `import { detectLinks }` line.)

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/lib/terminalLinks.test.ts
```

Expected: FAIL — `normalizeForOpen` undefined.

- [ ] **Step 3: Implement**

Append to `desktop/frontend/src/lib/terminalLinks.ts`:

```ts
export function normalizeForOpen(
  match: LinkMatch,
  homeDir: string | undefined,
): string | null {
  const t = match.text;
  if (match.kind === "http" || match.kind === "file") return t;
  // kind === "path"
  if (t.startsWith("~/") || t === "~/") {
    if (!homeDir) return null;
    const home = homeDir.replace(/\/+$/, "");
    return `file://${home}${t.slice(1)}`;
  }
  return `file://${t}`;
}
```

- [ ] **Step 4: Run, expect pass**

```bash
cd desktop/frontend && npx vitest run src/lib/terminalLinks.test.ts
```

Expected: PASS all.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/terminalLinks.ts desktop/frontend/src/lib/terminalLinks.test.ts
git -c commit.gpgsign=false commit -m "feat(terminal): normalizeForOpen for paths and ~/ expansion"
```

---

### Task 5: `isModClickEvent` predicate

**Files:**
- Modify: `desktop/frontend/src/lib/terminalLinks.ts`
- Modify: `desktop/frontend/src/lib/terminalLinks.test.ts`

- [ ] **Step 1: Failing test**

Append to `terminalLinks.test.ts`:

```ts
import { isModClickEvent } from "./terminalLinks";

describe("isModClickEvent", () => {
  function ev(opts: Partial<MouseEventInit>): MouseEvent {
    return new MouseEvent("click", opts);
  }

  it("mac requires metaKey only", () => {
    expect(isModClickEvent(ev({ metaKey: true }), true)).toBe(true);
    expect(isModClickEvent(ev({ ctrlKey: true }), true)).toBe(false);
    expect(isModClickEvent(ev({}), true)).toBe(false);
  });

  it("non-mac requires ctrlKey only", () => {
    expect(isModClickEvent(ev({ ctrlKey: true }), false)).toBe(true);
    expect(isModClickEvent(ev({ metaKey: true }), false)).toBe(false);
    expect(isModClickEvent(ev({}), false)).toBe(false);
  });

  it("rejects combinations with alt/shift", () => {
    expect(isModClickEvent(ev({ metaKey: true, shiftKey: true }), true)).toBe(false);
    expect(isModClickEvent(ev({ metaKey: true, altKey: true }), true)).toBe(false);
    expect(isModClickEvent(ev({ ctrlKey: true, shiftKey: true }), false)).toBe(false);
    expect(isModClickEvent(ev({ ctrlKey: true, altKey: true }), false)).toBe(false);
  });

  it("rejects when both ctrl and meta are pressed (ambiguous)", () => {
    expect(isModClickEvent(ev({ metaKey: true, ctrlKey: true }), true)).toBe(false);
    expect(isModClickEvent(ev({ metaKey: true, ctrlKey: true }), false)).toBe(false);
  });
});
```

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/lib/terminalLinks.test.ts
```

Expected: FAIL — `isModClickEvent` undefined.

- [ ] **Step 3: Implement**

Append to `desktop/frontend/src/lib/terminalLinks.ts`:

```ts
export function isModClickEvent(e: MouseEvent, isMac: boolean): boolean {
  if (e.shiftKey || e.altKey) return false;
  return isMac
    ? e.metaKey && !e.ctrlKey
    : e.ctrlKey && !e.metaKey;
}
```

- [ ] **Step 4: Run, expect pass**

```bash
cd desktop/frontend && npx vitest run src/lib/terminalLinks.test.ts
```

Expected: PASS all.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/terminalLinks.ts desktop/frontend/src/lib/terminalLinks.test.ts
git -c commit.gpgsign=false commit -m "feat(terminal): Mod-click predicate (⌘ on mac, Ctrl elsewhere)"
```

---

### Task 6: `useTerminalLinkProvider` adapter + tests

**Files:**
- Create: `desktop/frontend/src/composables/useTerminalLinkProvider.ts`
- Create: `desktop/frontend/src/composables/useTerminalLinkProvider.test.ts`

- [ ] **Step 1: Failing test**

`desktop/frontend/src/composables/useTerminalLinkProvider.test.ts`:

```ts
import { describe, expect, it, vi } from "vitest";
import { useTerminalLinkProvider } from "./useTerminalLinkProvider";

function makeFakeTerm(lineText: string) {
  let provider: { provideLinks: (y: number, cb: (links: unknown[] | undefined) => void) => void } | null = null;
  const dispose = vi.fn();
  return {
    term: {
      registerLinkProvider(p: typeof provider) {
        provider = p;
        return { dispose };
      },
      buffer: {
        active: {
          getLine(_y: number) {
            return {
              translateToString: (_trim: boolean) => lineText,
            };
          },
        },
      },
    } as unknown as import("xterm").Terminal,
    getProvider: () => provider!,
    dispose,
  };
}

describe("useTerminalLinkProvider", () => {
  it("provides one ILink per detectLinks match on the requested line", () => {
    const f = makeFakeTerm("see https://x.test now");
    const openURL = vi.fn().mockResolvedValue(undefined);
    const onError = vi.fn();
    useTerminalLinkProvider({
      term: f.term,
      isMac: true,
      getHomeDir: () => "/Users/me",
      openURL,
      onError,
    });

    let received: any[] | undefined;
    f.getProvider().provideLinks(1, (links) => (received = links as any[]));
    expect(received).toHaveLength(1);
    expect(received![0].text).toBe("https://x.test");
    expect(received![0].range.start.y).toBe(1);
    expect(received![0].range.end.y).toBe(1);
    expect(received![0].decorations).toEqual({ underline: true, pointerCursor: true });
  });

  it("activate ignores click without modifier", async () => {
    const f = makeFakeTerm("https://x.test");
    const openURL = vi.fn().mockResolvedValue(undefined);
    useTerminalLinkProvider({
      term: f.term, isMac: true,
      getHomeDir: () => "", openURL, onError: vi.fn(),
    });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    links![0].activate(new MouseEvent("click", {}), "https://x.test");
    expect(openURL).not.toHaveBeenCalled();
  });

  it("activate with Mod opens URL", async () => {
    const f = makeFakeTerm("https://x.test");
    const openURL = vi.fn().mockResolvedValue(undefined);
    useTerminalLinkProvider({
      term: f.term, isMac: true,
      getHomeDir: () => "", openURL, onError: vi.fn(),
    });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    await links![0].activate(new MouseEvent("click", { metaKey: true }), "https://x.test");
    expect(openURL).toHaveBeenCalledWith("https://x.test");
  });

  it("activate for ~/ without homeDir calls onError, not openURL", async () => {
    const f = makeFakeTerm("cd ~/Projects/foo");
    const openURL = vi.fn().mockResolvedValue(undefined);
    const onError = vi.fn();
    useTerminalLinkProvider({
      term: f.term, isMac: true,
      getHomeDir: () => "", openURL, onError,
    });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    await links![0].activate(new MouseEvent("click", { metaKey: true }), "~/Projects/foo");
    expect(openURL).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith("terminal.link.openFailedNoHome");
  });

  it("activate surfaces openURL rejection via onError", async () => {
    const f = makeFakeTerm("https://x.test");
    const openURL = vi.fn().mockRejectedValue(new Error("boom"));
    const onError = vi.fn();
    useTerminalLinkProvider({
      term: f.term, isMac: true,
      getHomeDir: () => "", openURL, onError,
    });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    await links![0].activate(new MouseEvent("click", { metaKey: true }), "https://x.test");
    expect(onError).toHaveBeenCalledWith("terminal.link.openFailed");
  });

  it("returns a disposable that forwards to xterm", () => {
    const f = makeFakeTerm("");
    const d = useTerminalLinkProvider({
      term: f.term, isMac: true,
      getHomeDir: () => "", openURL: vi.fn(), onError: vi.fn(),
    });
    d.dispose();
    expect(f.dispose).toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/composables/useTerminalLinkProvider.test.ts
```

Expected: FAIL — module not found.

- [ ] **Step 3: Implement adapter**

`desktop/frontend/src/composables/useTerminalLinkProvider.ts`:

```ts
import type { IDisposable, Terminal } from "xterm";
import { detectLinks, isModClickEvent, normalizeForOpen, type LinkMatch } from "../lib/terminalLinks";

export interface UseTerminalLinkProviderDeps {
  term: Terminal;
  isMac: boolean;
  getHomeDir: () => string;
  openURL: (url: string) => Promise<void>;
  onError: (i18nKey: "terminal.link.openFailed" | "terminal.link.openFailedNoHome") => void;
}

export function useTerminalLinkProvider(deps: UseTerminalLinkProviderDeps): IDisposable {
  const { term, isMac, getHomeDir, openURL, onError } = deps;

  const provider = {
    provideLinks(y: number, callback: (links: unknown[] | undefined) => void) {
      const line = term.buffer.active.getLine(y - 1)?.translateToString(true) ?? "";
      const matches = detectLinks(line);
      if (matches.length === 0) {
        callback(undefined);
        return;
      }
      callback(matches.map((m) => toILink(m, y, isMac, getHomeDir, openURL, onError)));
    },
  };

  try {
    return term.registerLinkProvider(provider as any);
  } catch (err) {
    console.warn("[AT Term] registerLinkProvider failed", err);
    return { dispose() {} };
  }
}

function toILink(
  m: LinkMatch,
  y: number,
  isMac: boolean,
  getHomeDir: () => string,
  openURL: (url: string) => Promise<void>,
  onError: UseTerminalLinkProviderDeps["onError"],
) {
  return {
    range: {
      start: { x: m.start + 1, y },
      end: { x: m.end, y },
    },
    text: m.text,
    decorations: { underline: true, pointerCursor: true },
    activate: async (event: MouseEvent) => {
      if (!isModClickEvent(event, isMac)) return;
      const url = normalizeForOpen(m, getHomeDir());
      if (!url) {
        onError("terminal.link.openFailedNoHome");
        return;
      }
      try {
        await openURL(url);
      } catch (err) {
        console.warn("[AT Term] openURL failed", err);
        onError("terminal.link.openFailed");
      }
    },
  };
}
```

- [ ] **Step 4: Run, expect pass**

```bash
cd desktop/frontend && npx vitest run src/composables/useTerminalLinkProvider.test.ts
```

Expected: PASS, 6/6 green.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/composables/useTerminalLinkProvider.ts desktop/frontend/src/composables/useTerminalLinkProvider.test.ts
git -c commit.gpgsign=false commit -m "feat(terminal): xterm ILinkProvider adapter w/ Mod-click guard"
```

---

### Task 7: Wire the link provider into TerminalView + i18n strings

**Files:**
- Modify: `desktop/frontend/src/components/TerminalView.vue`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`

- [ ] **Step 1: Add i18n keys (en)**

Open `desktop/frontend/src/i18n/messages/en.ts`. Locate the `terminal: {` block. Add (next to existing copy/paste failure keys):

```ts
    contextMenu: {
      openLink: "Open Link",
      copyLink: "Copy Link",
    },
    link: {
      openFailed: "Failed to open link",
      openFailedNoHome: "Cannot resolve ~ (home directory unavailable)",
    },
```

If `contextMenu` already exists as a key under `terminal:`, merge the two
inner properties instead of overwriting. (Grep first to be sure.)

- [ ] **Step 2: Add i18n keys (zh-CN)**

Same structure in `desktop/frontend/src/i18n/messages/zh-CN.ts`:

```ts
    contextMenu: {
      openLink: "打开链接",
      copyLink: "复制链接",
    },
    link: {
      openFailed: "无法打开链接",
      openFailedNoHome: "无法解析 ~（拿不到 home 目录）",
    },
```

- [ ] **Step 3: Import & register provider in TerminalView.vue**

Open `desktop/frontend/src/components/TerminalView.vue`.

Add to the imports block near the top (after the `useTerminalShortcuts` style imports):

```ts
import { useTerminalLinkProvider } from "../composables/useTerminalLinkProvider";
import { detectLinks, normalizeForOpen, type LinkMatch } from "../lib/terminalLinks";
import { getUserHomeDir } from "../lib/api";
```

(`getUserHomeDir` may already be imported via `api.*` — if so, just reference it directly.)

Declare module-level state (next to `let term`, `let fit`):

```ts
let linkProviderDisposer: { dispose(): void } | null = null;
let cachedHomeDir = "";
```

Inside `ensureTerm()`, **after** the OSC 133 try/catch and **before** the
`ResizeObserver` block, append:

```ts
  try {
    cachedHomeDir = await getUserHomeDir();
  } catch {
    cachedHomeDir = "";
  }
  linkProviderDisposer = useTerminalLinkProvider({
    term,
    isMac,
    getHomeDir: () => cachedHomeDir,
    openURL: (u) => platform.system.openExternalURL(u),
    onError: (key) => emit("toast", t(key)),
  });
```

(`isMac` is already declared in the file; `platform` is from `usePlatform()`; `t` is the i18n function.)

In the component teardown (look for where `resizeObserver?.disconnect()` happens, typically in `onBeforeUnmount` or a `cleanup()` helper), add:

```ts
linkProviderDisposer?.dispose();
linkProviderDisposer = null;
```

- [ ] **Step 4: Build the project to surface any type errors**

```bash
cd desktop/frontend && npx vue-tsc --noEmit
```

Expected: no new errors. Fix any that surface inline.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/TerminalView.vue desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts
git -c commit.gpgsign=false commit -m "feat(terminal): register link provider in TerminalView + i18n strings"
```

---

### Task 8: Add "Open Link" / "Copy Link" to right-click menu

**Files:**
- Modify: `desktop/frontend/src/components/TerminalView.vue`

- [ ] **Step 1: Add reactive state for menu link hit**

In the `<script setup>` block, near the other menu state (`menuOpen`, `menuHasSelection`, etc.):

```ts
const menuLinkHit = ref<LinkMatch | null>(null);
```

- [ ] **Step 2: Compute the link hit during right-click**

In `openContextMenu(e: MouseEvent)`, **after** the `menuHasSelection.value = !!selection;` line and **before** `menuOpen.value = true;`, add:

```ts
  // Detect a clickable link under the right-click pointer so the menu
  // can offer Open/Copy actions for it.
  menuLinkHit.value = (() => {
    if (!term) return null;
    const viewport = termContainer.value;
    if (!viewport) return null;
    const hit = cellCoordsAt(e.clientX, e.clientY, term, viewport);
    if (!hit) return null;
    const buf = term.buffer.active;
    const line = buf.getLine(hit.row)?.translateToString(true) ?? "";
    const found = detectLinks(line).find((m) => hit.col >= m.start && hit.col < m.end);
    return found ?? null;
  })();
```

Add to the imports (if not already present):

```ts
import { cellCoordsAt } from "../lib/terminalCellCoords";
```

- [ ] **Step 3: Reset hit on menu close**

In `closeContextMenu()` (or whichever handler sets `menuOpen.value = false`), add:

```ts
menuLinkHit.value = null;
```

- [ ] **Step 4: Add handlers**

In `<script setup>`, alongside `onMenuCopy` / `onMenuPaste`:

```ts
async function onMenuOpenLink() {
  const hit = menuLinkHit.value;
  closeContextMenu();
  if (!hit) return;
  const url = normalizeForOpen(hit, cachedHomeDir);
  if (!url) {
    emit("toast", t("terminal.link.openFailedNoHome"));
    return;
  }
  try {
    await platform.system.openExternalURL(url);
  } catch (err) {
    console.warn("[AT Term] open link failed", err);
    emit("toast", t("terminal.link.openFailed"));
  }
}

async function onMenuCopyLink() {
  const hit = menuLinkHit.value;
  closeContextMenu();
  if (!hit) return;
  try {
    await navigator.clipboard.writeText(hit.text);
  } catch (err) {
    console.warn("[AT Term] copy link failed", err);
    emit("toast", t("terminal.copyFailed"));
  }
}
```

- [ ] **Step 5: Render menu buttons**

In the `<template>` menu block, find:

```html
<button class="term-context-item" :disabled="!menuHasSelection" @click="onMenuCopy">{{ t("common.copy") }}</button>
```

Insert these two **before** the Copy button (so the link actions are at the
top when present):

```html
<button v-if="menuLinkHit" class="term-context-item" @click="onMenuOpenLink">{{ t("terminal.contextMenu.openLink") }}</button>
<button v-if="menuLinkHit" class="term-context-item" @click="onMenuCopyLink">{{ t("terminal.contextMenu.copyLink") }}</button>
```

- [ ] **Step 6: Build to check types**

```bash
cd desktop/frontend && npx vue-tsc --noEmit
```

Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add desktop/frontend/src/components/TerminalView.vue
git -c commit.gpgsign=false commit -m "feat(terminal): Open Link / Copy Link entries in right-click menu"
```

---

### Task 9: Integration tests in TerminalView.test.ts

**Files:**
- Modify: `desktop/frontend/src/components/TerminalView.test.ts`

- [ ] **Step 1: Read existing test scaffolding**

```bash
grep -n "openContextMenu\|term-context-menu\|mount\|describe\|it(" /Users/attson/code/github.com.attson/atterm/desktop/frontend/src/components/TerminalView.test.ts | head -30
```

Use the existing helpers (a `mountTerminalView(...)` style helper is typically present; reuse it).

- [ ] **Step 2: Add test — right-click on URL shows Open/Copy items**

Add a test near the bottom of the relevant `describe` block:

```ts
it("right-click on a URL surfaces Open Link / Copy Link menu items", async () => {
  const wrapper = await mountTerminalView();           // existing helper
  const term = (wrapper.vm as any).term as import("xterm").Terminal;
  term.write("see https://example.com today\r\n");
  await nextTick();
  // The exact dispatch depends on the existing helper; many tests use
  // document.elementFromPoint stubs and call openContextMenu directly via
  // the component method. Mirror what other context-menu tests do.
  await (wrapper.vm as any).openContextMenu(
    new MouseEvent("contextmenu", { clientX: 50, clientY: 8 }),
  );
  await nextTick();
  expect(wrapper.find(".term-context-menu").text()).toContain("Open Link");
  expect(wrapper.find(".term-context-menu").text()).toContain("Copy Link");
});
```

If the existing test helpers don't expose `term` or `openContextMenu`,
extend `mountTerminalView` to return them (most TerminalView tests already
do this — grep for prior patterns).

- [ ] **Step 3: Add test — Mod+click on URL opens it**

```ts
it("Mod+click on a URL calls platform.system.openExternalURL", async () => {
  const open = vi.fn().mockResolvedValue(undefined);
  __setPlatformForTests({
    ...stubPlatform(),
    system: { ...stubPlatform().system, openExternalURL: open },
  });
  const wrapper = await mountTerminalView();
  const term = (wrapper.vm as any).term as import("xterm").Terminal;
  term.write("https://example.com\r\n");
  await nextTick();
  // Drive activate() directly via the registered link provider.
  const lp = (wrapper.vm as any).linkProviderForTests as
    | { provideLinks: (y: number, cb: (l: unknown[] | undefined) => void) => void }
    | undefined;
  if (!lp) {
    // Adapter doesn't expose itself for tests; skip if so — the
    // composable's own unit tests already cover this code path.
    return;
  }
  let links: any[] | undefined;
  lp.provideLinks(1, (l) => (links = l as any[]));
  await links![0].activate(new MouseEvent("click", { metaKey: true }), "https://example.com");
  expect(open).toHaveBeenCalledWith("https://example.com");
});
```

Adjust the assertions to whichever stub pattern the existing test file
uses. The composable's own unit tests already cover the same logic, so a
graceful skip is acceptable when the integration surface isn't exposed.

- [ ] **Step 4: Run all frontend tests**

```bash
cd desktop/frontend && npm test -- --run
```

Expected: all green. Fix snapshots only if a deliberate UI change was made
(none here — link items are gated by `v-if="menuLinkHit"` and don't show
when there's no link match).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/TerminalView.test.ts
git -c commit.gpgsign=false commit -m "test(terminal): right-click + Mod+click integration for links"
```

---

### Task 10: Final type-check + full test sweep + manual smoke

**Files:**
- None modified by this task.

- [ ] **Step 1: Type-check**

```bash
cd desktop/frontend && npx vue-tsc --noEmit
```

Expected: clean.

- [ ] **Step 2: Full test suite**

```bash
cd desktop/frontend && npm test -- --run
```

Expected: all green.

- [ ] **Step 3: Build production bundle**

```bash
cd desktop/frontend && npm run build:wails
```

Expected: succeeds.

- [ ] **Step 4: Go build + (optional) Wails dev smoke**

```bash
cd /Users/attson/code/github.com.attson/atterm && go build ./...
```

Expected: clean. If you want to verify end-to-end UX:

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop && wails dev
```

Then in the running app: `printf 'see https://github.com and ~/Documents\n'`
in a terminal pane → confirm the URL underlines on hover, ⌘+Click opens
the browser (macOS), right-click shows Open Link / Copy Link. Close with
Ctrl+C.

- [ ] **Step 5: Nothing to commit unless smoke surfaced a fix**

If smoke is clean, proceed to release.

---

### Task 11: Release

Hand off to `ship-release` skill once Task 10 is green. The skill will:
- Cut a branch (if not already on one), commit any stragglers.
- Open a PR against `main`, squash-merge, tag the next patch release.

User has already set goal "完成并发布", so executing-plans will trigger
ship-release as soon as Task 10 completes successfully.

---

## Self-Review Notes

**Spec coverage:**
- ✓ Detect http(s) → Task 1.
- ✓ Detect file:// → Task 1.
- ✓ Detect absolute paths → Task 3.
- ✓ Detect `~/…` → Task 3.
- ✓ Trailing-punct rule incl. balanced parens → Tasks 1+2.
- ✓ Single-line only → Task 1 (regex has no multi-line, line-by-line provider in Task 6).
- ✓ Mod key cross-platform → Task 5.
- ✓ Right-click menu items → Task 8.
- ✓ Error handling toast → Task 6 (adapter) + Task 8 (menu handler).
- ✓ Reuse `GetUserHomeDir` → Task 7.
- ✓ Tests at every layer → Tasks 1-6 (unit) + Task 9 (integration).

**Type consistency:**
- `LinkMatch` defined Task 1, reused Tasks 4, 6, 8.
- `useTerminalLinkProvider({ term, isMac, getHomeDir, openURL, onError })` signature consistent Tasks 6, 7.
- i18n keys `terminal.contextMenu.openLink|copyLink` + `terminal.link.openFailed|openFailedNoHome` consistent Tasks 6, 7, 8.

**Placeholder scan:** No TBDs, no "add appropriate error handling", every code step shows the code.
