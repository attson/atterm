# Mobile Text Selection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add iOS-style long-press text selection to `MobileTerminal.vue`: hold a word → it selects → drag to extend → tap a floating popover above to **copy** or **send** the text to the PTY.

**Architecture:** Reuse xterm.js v5's public `select` / `selectLines` / `getSelectionPosition` / `onSelectionChange` API — the WebGL renderer keeps owning selection highlight; we only own the gesture, word-boundary math, popover, and toast. Strictly gated by `canSend` (driver + control mode on). Copy delegates to existing `lib/terminalCopy.copyTerminalSelection`; send to `lib/terminalContextMenu.prepareSendPayload`.

**Tech Stack:** Vue 3 `<script setup>` + TypeScript + Vitest for tests; xterm.js public API (no internal-API dependency in T1/T2 — `terminalCellCoords` reads `_core._renderService.dimensions` with a `fontSize × lineHeight` fallback).

**Reference spec:** `docs/superpowers/specs/2026-06-09-mobile-text-selection-design.md`

---

## Task 1: `lib/wordBoundary.ts` — pure word-boundary helper

**Files:**
- Create: `desktop/frontend/src/lib/wordBoundary.ts`
- Create: `desktop/frontend/src/lib/wordBoundary.test.ts`

Smallest, pure unit. Picks the word at a column position using two rules: a run of `[A-Za-z0-9_]` is one word; otherwise a run of non-whitespace non-alnum-underscore characters is one word; whitespace produces an empty result.

- [ ] **Step 1.1: Write the failing tests**

Create `desktop/frontend/src/lib/wordBoundary.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { wordBoundaryAt } from './wordBoundary'

describe('wordBoundaryAt', () => {
  it('selects the alphanumeric word the col is inside', () => {
    expect(wordBoundaryAt('git status -v', 2)).toEqual({ start: 0, len: 3 })
  })
  it('selects the word at the start position', () => {
    expect(wordBoundaryAt('git status -v', 4)).toEqual({ start: 4, len: 6 })
  })
  it('treats a punctuation run as one word', () => {
    expect(wordBoundaryAt('--foo', 1)).toEqual({ start: 0, len: 2 })
  })
  it('returns len=0 when col is on whitespace', () => {
    expect(wordBoundaryAt('hi  there', 2)).toEqual({ start: 2, len: 0 })
  })
  it('handles col at the last character', () => {
    expect(wordBoundaryAt('abc', 2)).toEqual({ start: 0, len: 3 })
  })
  it('returns len=0 when col is past line end', () => {
    expect(wordBoundaryAt('abc', 10)).toEqual({ start: 3, len: 0 })
  })
  it('returns len=0 on an empty line', () => {
    expect(wordBoundaryAt('', 0)).toEqual({ start: 0, len: 0 })
  })
  it('treats a single CJK character as one word', () => {
    expect(wordBoundaryAt('读 hello', 0)).toEqual({ start: 0, len: 1 })
  })
  it('groups underscores and digits into the alnum class', () => {
    expect(wordBoundaryAt('foo_bar123 baz', 5)).toEqual({ start: 0, len: 10 })
  })
})
```

- [ ] **Step 1.2: Run tests and confirm they fail**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npx vitest run src/lib/wordBoundary.test.ts
```

Expected: FAIL — `Cannot find module './wordBoundary'`.

- [ ] **Step 1.3: Implement the helper**

Create `desktop/frontend/src/lib/wordBoundary.ts`:

```ts
// wordBoundaryAt returns the {start, len} of the word at col within line.
//
// Word classes:
//   - alnum-underscore run: matches /[A-Za-z0-9_]/ plus any CJK / non-ASCII
//     letter-like codepoint (anything that is NOT whitespace AND NOT an
//     ASCII punctuation char). Single CJK characters count as a word — this
//     matches what iOS system long-press does on CJK text.
//   - punctuation run: a contiguous sequence of ASCII punctuation
//     (anything in `!"#$%&'()*+,-./:;<=>?@[\]^_\`{|}~` minus underscore).
//   - whitespace: yields len=0 (no word).
//
// col is a 0-based offset into the line's codepoints. If col is past the line
// length, returns { start: line.length, len: 0 }.
export function wordBoundaryAt(line: string, col: number): { start: number; len: number } {
  if (col >= line.length) return { start: line.length, len: 0 }
  const ch = line[col]
  if (isWhitespace(ch)) return { start: col, len: 0 }
  const isAlnum = isAlnumLike(ch)
  let start = col
  while (start > 0 && classOf(line[start - 1]) === (isAlnum ? 'alnum' : 'punct')) start--
  let end = col + 1
  while (end < line.length && classOf(line[end]) === (isAlnum ? 'alnum' : 'punct')) end++
  return { start, len: end - start }
}

function isWhitespace(ch: string): boolean {
  return /\s/.test(ch)
}

// ASCII punctuation set minus underscore (underscore joins alnum, like JS \w).
const ASCII_PUNCT = new Set(`!"#$%&'()*+,-./:;<=>?@[\\]^\`{|}~`.split(''))

function isAlnumLike(ch: string): boolean {
  if (isWhitespace(ch)) return false
  if (ASCII_PUNCT.has(ch)) return false
  return true
}

function classOf(ch: string): 'alnum' | 'punct' | 'ws' {
  if (isWhitespace(ch)) return 'ws'
  if (ASCII_PUNCT.has(ch)) return 'punct'
  return 'alnum'
}
```

- [ ] **Step 1.4: Run tests and confirm they pass**

```bash
npx vitest run src/lib/wordBoundary.test.ts
```

Expected: PASS (9/9).

- [ ] **Step 1.5: Commit**

```bash
git add desktop/frontend/src/lib/wordBoundary.ts desktop/frontend/src/lib/wordBoundary.test.ts
git commit -m "lib: add wordBoundaryAt helper for terminal long-press selection"
```

---

## Task 2: `lib/terminalCellCoords.ts` — (clientX,clientY) → (col,row)

**Files:**
- Create: `desktop/frontend/src/lib/terminalCellCoords.ts`
- Create: `desktop/frontend/src/lib/terminalCellCoords.test.ts`

Pure pixel-to-cell math. Reads xterm's internal `_core._renderService.dimensions` when available; falls back to `fontSize × lineHeight` so a missing internal API degrades rather than crashes.

- [ ] **Step 2.1: Write the failing tests**

Create `desktop/frontend/src/lib/terminalCellCoords.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { cellCoordsAt, type CellSizeReader } from './terminalCellCoords'

// Build a fake viewport element with a known bounding rect and scrollTop.
function viewport({ x, y, w, h, scrollTop = 0 }: { x: number; y: number; w: number; h: number; scrollTop?: number }) {
  const el = document.createElement('div')
  el.getBoundingClientRect = () => ({ x, y, left: x, top: y, width: w, height: h, right: x + w, bottom: y + h, toJSON() { return this } } as DOMRect)
  Object.defineProperty(el, 'scrollTop', { value: scrollTop, configurable: true })
  return el
}

const cellReader: CellSizeReader = () => ({ width: 8, height: 16 })
const term = { cols: 80, rows: 24 } as any

describe('cellCoordsAt', () => {
  it('maps top-left pixel to (0,0)', () => {
    const vp = viewport({ x: 0, y: 0, w: 640, h: 384 })
    expect(cellCoordsAt(4, 8, term, vp, cellReader)).toEqual({ col: 0, row: 0 })
  })
  it('maps pixel inside cell (3,2)', () => {
    const vp = viewport({ x: 0, y: 0, w: 640, h: 384 })
    expect(cellCoordsAt(28, 40, term, vp, cellReader)).toEqual({ col: 3, row: 2 })
  })
  it('accounts for viewport scrollTop in the row calculation', () => {
    const vp = viewport({ x: 0, y: 0, w: 640, h: 384, scrollTop: 160 })  // 10 rows scrolled
    expect(cellCoordsAt(4, 8, term, vp, cellReader)).toEqual({ col: 0, row: 10 })
  })
  it('returns null when clientX is past the right edge', () => {
    const vp = viewport({ x: 0, y: 0, w: 640, h: 384 })
    expect(cellCoordsAt(900, 40, term, vp, cellReader)).toBeNull()
  })
  it('returns null when clientY is below the bottom edge', () => {
    const vp = viewport({ x: 0, y: 0, w: 640, h: 384 })
    expect(cellCoordsAt(28, 700, term, vp, cellReader)).toBeNull()
  })
  it('returns null when coords are above/left of the viewport', () => {
    const vp = viewport({ x: 100, y: 100, w: 640, h: 384 })
    expect(cellCoordsAt(50, 50, term, vp, cellReader)).toBeNull()
  })
  it('respects a viewport offset within the page', () => {
    const vp = viewport({ x: 100, y: 200, w: 640, h: 384 })
    // pixel (108, 216) is (cellX 1, cellY 1) → col 1, row 1
    expect(cellCoordsAt(108, 216, term, vp, cellReader)).toEqual({ col: 1, row: 1 })
  })
})
```

- [ ] **Step 2.2: Run tests and confirm they fail**

```bash
npx vitest run src/lib/terminalCellCoords.test.ts
```

Expected: FAIL — `Cannot find module './terminalCellCoords'`.

- [ ] **Step 2.3: Implement the helper**

Create `desktop/frontend/src/lib/terminalCellCoords.ts`:

```ts
import type { Terminal } from 'xterm'

export interface CellHit {
  col: number
  row: number
}

export interface CellSize {
  width: number
  height: number
}

// CellSizeReader lets callers (and tests) inject the cell-size source. Production
// callers pass `readXtermCellSize` (below); tests stub it.
export type CellSizeReader = (term: Terminal) => CellSize

// readXtermCellSize reads xterm's CSS-pixel cell size from its renderer's
// dimensions. xterm 5.x exposes this via _core._renderService.dimensions —
// an internal API. Falls back to a degraded `fontSize × lineHeight` estimate
// if the internal path is missing (e.g. before the renderer has measured).
export function readXtermCellSize(term: Terminal): CellSize {
  // Best path: live renderer dimensions.
  const dim = (term as unknown as {
    _core?: { _renderService?: { dimensions?: { css?: { cell?: { width?: number; height?: number } } } } }
  })._core?._renderService?.dimensions?.css?.cell
  if (dim && typeof dim.width === 'number' && typeof dim.height === 'number' && dim.width > 0 && dim.height > 0) {
    return { width: dim.width, height: dim.height }
  }
  // Fallback: estimate from font options. Mono fonts average ~0.6 width/height
  // ratio; xterm defaults lineHeight to 1.0.
  const fontSize = (term.options.fontSize ?? 12) as number
  const lineHeight = (term.options.lineHeight ?? 1.0) as number
  return { width: fontSize * 0.6, height: fontSize * lineHeight }
}

// cellCoordsAt converts viewport-relative client coords into the cell
// at that pixel inside the terminal's scrollback grid. Returns null when
// the coords fall outside the viewport.
export function cellCoordsAt(
  clientX: number,
  clientY: number,
  term: Terminal,
  viewport: HTMLElement,
  readSize: CellSizeReader = readXtermCellSize,
): CellHit | null {
  const rect = viewport.getBoundingClientRect()
  if (clientX < rect.left || clientX >= rect.right) return null
  if (clientY < rect.top || clientY >= rect.bottom) return null

  const { width: cw, height: ch } = readSize(term)
  if (cw <= 0 || ch <= 0) return null

  const localX = clientX - rect.left
  const localY = clientY - rect.top + (viewport.scrollTop ?? 0)
  const col = Math.floor(localX / cw)
  const row = Math.floor(localY / ch)
  return { col, row }
}
```

- [ ] **Step 2.4: Run tests and confirm they pass**

```bash
npx vitest run src/lib/terminalCellCoords.test.ts
```

Expected: PASS (7/7).

- [ ] **Step 2.5: Commit**

```bash
git add desktop/frontend/src/lib/terminalCellCoords.ts desktop/frontend/src/lib/terminalCellCoords.test.ts
git commit -m "lib: add cellCoordsAt for translating touch coords to xterm grid cells"
```

---

## Task 3: i18n keys for selection popover labels and toasts

**Files:**
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`
- Modify: `desktop/frontend/src/i18n/messages/tasks.test.ts` (or similar i18n test if it exists)

Add a `mobile.selection.*` namespace covering popover button labels, copy success toast, and cancel/close aria text. The existing `terminal.copyFailed` key is reused for the copy-fail toast.

- [ ] **Step 3.1: Inspect the existing i18n test file**

```bash
ls /Users/attson/code/github.com.attson/atterm/desktop/frontend/src/i18n/messages/
```

If there's an `*.test.ts` file enforcing key parity between locales, you will be extending it.

- [ ] **Step 3.2: Add the keys to `en.ts`**

In `desktop/frontend/src/i18n/messages/en.ts`, find the `mobile: {` block (around line 326). Add a `selection` sub-block at a sensible position (after the existing mobile sub-blocks, before `taskStates` if alphabetically convenient — match existing ordering):

```ts
    selection: {
      copy: "Copy",
      send: "Send",
      cancel: "Cancel",
      copied: "Copied to clipboard",
    },
```

- [ ] **Step 3.3: Add the matching keys to `zh-CN.ts`**

In `desktop/frontend/src/i18n/messages/zh-CN.ts`, find the matching `mobile: {` block. Add:

```ts
    selection: {
      copy: "复制",
      send: "发送",
      cancel: "取消",
      copied: "已复制到剪贴板",
    },
```

- [ ] **Step 3.4: Add a parity test if one doesn't already exist for `mobile.selection.*`**

If there's already an i18n parity test file (e.g. `i18n.test.ts` or `messages/tasks.test.ts`) that asserts en/zh-CN structures match, add the selection keys to it. If no such test exists for the mobile namespace, write a tiny new test:

Create `desktop/frontend/src/i18n/messages/mobile-selection.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import en from './en'
import zhCN from './zh-CN'

describe('mobile.selection i18n', () => {
  it('en has all selection keys', () => {
    expect(en.mobile.selection.copy).toBeTypeOf('string')
    expect(en.mobile.selection.send).toBeTypeOf('string')
    expect(en.mobile.selection.cancel).toBeTypeOf('string')
    expect(en.mobile.selection.copied).toBeTypeOf('string')
  })
  it('zh-CN has the same selection keys as en', () => {
    expect(Object.keys(zhCN.mobile.selection)).toEqual(Object.keys(en.mobile.selection))
  })
})
```

(If `import en from './en'` doesn't match the existing export style — e.g. the messages use a named export `enMessages` — adapt to match. Look at the existing `tasks.test.ts` pattern.)

- [ ] **Step 3.5: Run all i18n tests**

```bash
npx vitest run src/i18n/
```

Expected: PASS.

Also run `npx vue-tsc --noEmit` to confirm no MessageKey union violations.

- [ ] **Step 3.6: Commit**

```bash
git add desktop/frontend/src/i18n/
git commit -m "i18n: add mobile.selection.{copy,send,cancel,copied} keys"
```

---

## Task 4: `MobileSelectionPopover.vue` — floating iOS-style popover

**Files:**
- Create: `desktop/frontend/src/mobile/MobileSelectionPopover.vue`
- Create: `desktop/frontend/src/mobile/__tests__/MobileSelectionPopover.test.ts`

Purely presentational. Single responsibility: render copy / send / × buttons in a floating dark popover at a parent-supplied (x, y); emit events on tap.

- [ ] **Step 4.1: Write the failing component tests**

Create `desktop/frontend/src/mobile/__tests__/MobileSelectionPopover.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import MobileSelectionPopover from '../MobileSelectionPopover.vue'

const baseProps = {
  visible: true,
  x: 120,
  y: 80,
  arrowDir: 'down' as const,
  copying: false,
  sending: false,
}

describe('MobileSelectionPopover', () => {
  it('does not render when visible=false', () => {
    const w = mount(MobileSelectionPopover, { props: { ...baseProps, visible: false } })
    expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
  })

  it('renders copy / send / cancel buttons', () => {
    const w = mount(MobileSelectionPopover, { props: baseProps })
    expect(w.find('[data-testid="selection-popover-copy"]').exists()).toBe(true)
    expect(w.find('[data-testid="selection-popover-send"]').exists()).toBe(true)
    expect(w.find('[data-testid="selection-popover-cancel"]').exists()).toBe(true)
  })

  it('emits copy on copy tap', async () => {
    const w = mount(MobileSelectionPopover, { props: baseProps })
    await w.find('[data-testid="selection-popover-copy"]').trigger('click')
    expect(w.emitted('copy')).toBeTruthy()
  })

  it('emits send on send tap', async () => {
    const w = mount(MobileSelectionPopover, { props: baseProps })
    await w.find('[data-testid="selection-popover-send"]').trigger('click')
    expect(w.emitted('send')).toBeTruthy()
  })

  it('emits cancel on × tap', async () => {
    const w = mount(MobileSelectionPopover, { props: baseProps })
    await w.find('[data-testid="selection-popover-cancel"]').trigger('click')
    expect(w.emitted('cancel')).toBeTruthy()
  })

  it('disables copy when copying=true', () => {
    const w = mount(MobileSelectionPopover, { props: { ...baseProps, copying: true } })
    expect((w.find('[data-testid="selection-popover-copy"]').element as HTMLButtonElement).disabled).toBe(true)
  })

  it('disables send when sending=true', () => {
    const w = mount(MobileSelectionPopover, { props: { ...baseProps, sending: true } })
    expect((w.find('[data-testid="selection-popover-send"]').element as HTMLButtonElement).disabled).toBe(true)
  })

  it('positions itself using x and y when arrowDir=down', () => {
    const w = mount(MobileSelectionPopover, { props: { ...baseProps, x: 120, y: 80, arrowDir: 'down' } })
    const styleAttr = w.find('[data-testid="selection-popover"]').attributes('style') || ''
    expect(styleAttr).toContain('left: 120px')
    expect(styleAttr).toContain('bottom: 80px')
  })

  it('positions itself using top when arrowDir=up', () => {
    const w = mount(MobileSelectionPopover, { props: { ...baseProps, x: 120, y: 80, arrowDir: 'up' } })
    const styleAttr = w.find('[data-testid="selection-popover"]').attributes('style') || ''
    expect(styleAttr).toContain('left: 120px')
    expect(styleAttr).toContain('top: 80px')
  })
})
```

- [ ] **Step 4.2: Run tests and confirm they fail**

```bash
npx vitest run src/mobile/__tests__/MobileSelectionPopover.test.ts
```

Expected: FAIL — component does not exist.

- [ ] **Step 4.3: Implement the component**

Create `desktop/frontend/src/mobile/MobileSelectionPopover.vue`:

```vue
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '../i18n/useI18n'

const props = defineProps<{
  visible: boolean
  x: number               // viewport px — popover horizontal CENTER (we translateX(-50%))
  y: number               // viewport px — interpreted as `bottom` when arrowDir='down', `top` when 'up'
  arrowDir: 'down' | 'up'
  copying: boolean
  sending: boolean
}>()

defineEmits<{
  (e: 'copy'): void
  (e: 'send'): void
  (e: 'cancel'): void
}>()

const { t } = useI18n()

const popStyle = computed(() => {
  const base = `left: ${props.x}px;`
  return props.arrowDir === 'down'
    ? `${base} bottom: ${props.y}px;`
    : `${base} top: ${props.y}px;`
})
</script>

<template>
  <div
    v-if="visible"
    class="popover"
    :class="[`arrow-${arrowDir}`]"
    :style="popStyle"
    data-testid="selection-popover"
    role="toolbar"
    :aria-label="t('mobile.selection.copy') + ' / ' + t('mobile.selection.send')"
    @pointerdown.stop
    @pointerup.stop
    @click.stop
  >
    <button
      type="button"
      class="btn"
      :disabled="copying"
      data-testid="selection-popover-copy"
      @click="$emit('copy')"
    >{{ t('mobile.selection.copy') }}</button>
    <button
      type="button"
      class="btn send"
      :disabled="sending"
      data-testid="selection-popover-send"
      @click="$emit('send')"
    >{{ t('mobile.selection.send') }}</button>
    <button
      type="button"
      class="btn cancel"
      :aria-label="t('mobile.selection.cancel')"
      data-testid="selection-popover-cancel"
      @click="$emit('cancel')"
    >×</button>
  </div>
</template>

<style scoped>
/* Container: dark bar, ~21 px tall visual; transform centers horizontally on x. */
.popover {
  position: absolute;
  transform: translateX(-50%);
  background: #2b2c30;
  color: #fff;
  border-radius: 8px;
  display: flex;
  box-shadow: 0 6px 20px rgba(0, 0, 0, .4);
  overflow: hidden;
  font-family: -apple-system, system-ui, sans-serif;
  z-index: 1000;            /* above terminal canvas + control panel */
  pointer-events: auto;
}
/* Arrow rendered as a pseudo-element pointing at the selection */
.popover::after {
  content: '';
  position: absolute;
  left: 50%;
  transform: translateX(-50%);
  width: 0;
  height: 0;
  border-left: 5px solid transparent;
  border-right: 5px solid transparent;
}
.popover.arrow-down::after {
  bottom: -5px;
  border-top: 5px solid #2b2c30;
}
.popover.arrow-up::after {
  top: -5px;
  border-bottom: 5px solid #2b2c30;
}
/* Buttons: 11 px visual + 4×11 padding ≈ 21 px tall; transparent hit-slop
   margin doubles the actual tap target without changing visual layout. */
.btn {
  position: relative;
  background: none;
  border: none;
  color: #fff;
  padding: 4px 11px;
  font-size: 11px;
  font-family: inherit;
  cursor: pointer;
  border-right: 1px solid #3f4046;
}
.btn:last-child { border-right: none; }
.btn.send { color: #60a5fa; font-weight: 600; }
.btn.cancel { font-size: 14px; line-height: 1; padding: 4px 8px; }
.btn:disabled { opacity: .5; cursor: not-allowed; }
/* Hit-slop: transparent box extends tap target above/below visual without
   shifting layout. ::before is the hit area; pointer-events:auto on parent
   .popover passes clicks through to the underlying <button>. */
.btn::before {
  content: '';
  position: absolute;
  top: -8px;
  bottom: -8px;
  left: 0;
  right: 0;
}
</style>
```

- [ ] **Step 4.4: Run tests and confirm they pass**

```bash
npx vitest run src/mobile/__tests__/MobileSelectionPopover.test.ts
npx vue-tsc --noEmit
```

Expected: 9 tests pass; tsc clean.

- [ ] **Step 4.5: Commit**

```bash
git add desktop/frontend/src/mobile/MobileSelectionPopover.vue \
        desktop/frontend/src/mobile/__tests__/MobileSelectionPopover.test.ts
git commit -m "mobile: add MobileSelectionPopover (copy / send / cancel)"
```

---

## Task 5: Integrate — long-press → word selection + popover + exit paths

**Files:**
- Modify: `desktop/frontend/src/mobile/MobileTerminal.vue`
- Modify: `desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts`

Wire the entry side of the gesture state machine: `pointerdown` starts a 500 ms timer (gated by `canSend`); the timer fires word-boundary selection; xterm draws the highlight; the popover appears. Also wire the "outside" exit paths (outside-click, scroll, canSend flips false, cancel button). Drag-extend lands in Task 6; copy / send / toast in Task 7.

- [ ] **Step 5.1: Extend the test fixture to support selection APIs**

In `desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts`, the existing `vi.mock('xterm', ...)` block (around lines 68-85) needs to expose `select`, `selectLines`, `clearSelection`, `hasSelection`, `getSelection`, `getSelectionPosition`, `scrollLines`, `onSelectionChange`, `buffer.active.getLine`, and an `options.disableStdin` flag. Replace the existing mock with:

```ts
const termWrite = vi.fn()
const termDispose = vi.fn()
const termFit = vi.fn()
const termResize = vi.fn()
const termScrollToBottom = vi.fn()
const termSelect = vi.fn()
const termSelectLines = vi.fn()
const termClearSelection = vi.fn()
const termGetSelection = vi.fn().mockReturnValue('')
const termGetSelectionPosition = vi.fn().mockReturnValue(undefined as any)
const termScrollLines = vi.fn()
let selectionChangeCb: (() => void) | null = null
let bufferLineText = ''
let lastTerm: any = null

vi.mock('xterm', () => ({
  Terminal: class {
    options: Record<string, unknown> = {}
    cols = 80
    rows = 24
    textarea = document.createElement('textarea')
    buffer = {
      active: {
        getLine: (_row: number) => ({ translateToString: () => bufferLineText }),
      },
    }
    constructor() { lastTerm = this }
    onData(cb: (s: string) => void) { (this as any)._onData = cb }
    onResize() {}
    onSelectionChange(cb: () => void) { selectionChangeCb = cb; return { dispose() {} } }
    open() {}
    write(d: unknown, cb?: () => void) { termWrite(d); cb?.() }
    dispose() { termDispose() }
    focus() {}
    loadAddon() {}
    resize(c: number, r: number) { termResize(c, r) }
    scrollToBottom() { termScrollToBottom() }
    select(c: number, r: number, len: number) { termSelect(c, r, len) }
    selectLines(s: number, e: number) { termSelectLines(s, e) }
    clearSelection() { termClearSelection() }
    hasSelection() { return termGetSelection.getMockImplementation()?.()?.length ? true : Boolean(termGetSelection()) }
    getSelection() { return termGetSelection() }
    getSelectionPosition() { return termGetSelectionPosition() }
    scrollLines(n: number) { termScrollLines(n) }
  },
}))
```

Also add a tiny helper near the top of the test file for the new tests:

```ts
// setBufferLine lets a test stage what term.buffer.active.getLine(row) returns.
function setBufferLine(text: string) { bufferLineText = text }
```

And in the existing `beforeEach`, reset:

```ts
beforeEach(() => {
  vi.clearAllMocks()
  lastHandlers = null
  lastArgs = null
  eventHandlers.clear()
  selectionChangeCb = null
  bufferLineText = ''
  termGetSelection.mockReturnValue('')
  termGetSelectionPosition.mockReturnValue(undefined)
})
```

- [ ] **Step 5.2: Add failing tests for long-press → select → popover**

Append these tests at the end of the `describe('MobileTerminal', () => { ... })` block in `MobileTerminal.test.ts`:

```ts
  describe('long-press selection', () => {
    function info(over: Partial<RemoteSession> = {}): RemoteSession {
      return { session_id: 's1', host_id: 'h', host: 'box', user: 'me', title: 't', cols: 80, rows: 24, remote_permission: 'full', ...over }
    }

    async function mountReady(extra: Partial<RemoteSession> = {}) {
      const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info: info(extra), active: true } })
      await flushPromises()
      return w
    }

    function viewportEl(w: ReturnType<typeof mount>) {
      // jsdom returns null for querySelector on inserted children of mocked xterm.
      // For these tests we attach our own div with the xterm-viewport class and
      // dispatch events on it.
      const root = w.element as HTMLElement
      let vp = root.querySelector('.xterm-viewport') as HTMLElement | null
      if (!vp) {
        vp = document.createElement('div')
        vp.className = 'xterm-viewport'
        Object.defineProperty(vp, 'getBoundingClientRect', { value: () => ({ left: 0, top: 0, right: 800, bottom: 600, width: 800, height: 600, x: 0, y: 0, toJSON() { return this } }) })
        Object.defineProperty(vp, 'scrollTop', { value: 0, configurable: true })
        ;(root.querySelector('.term') as HTMLElement).appendChild(vp)
      }
      return vp
    }

    function pointerEvent(type: string, x: number, y: number): PointerEvent {
      // jsdom lacks PointerEvent; build a minimal stand-in via MouseEvent + extra props.
      const ev = new MouseEvent(type, { clientX: x, clientY: y, bubbles: true })
      Object.defineProperty(ev, 'pointerId', { value: 1 })
      return ev as unknown as PointerEvent
    }

    it('does not select when canSend is false (control mode off)', async () => {
      const w = await mountReady()
      // controlMode is false by default; canSend is therefore false.
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 100, 80))
      await new Promise((r) => setTimeout(r, 600))
      expect(termSelect).not.toHaveBeenCalled()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })

    it('selects the word at the press position after 500 ms when canSend is true', async () => {
      const w = await mountReady()
      // Engage control mode (this also requires being driver — true by default in mock).
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')              // 80-wide row; cell (3,0) is inside "git" at col 3 — actually whitespace, so use col 2 ("t")
      // viewportEl uses cellW=8 cellH=16 from the dimensions fallback (fontSize=12 → 7.2 wide, 16 tall) — use coords that map to col 2 row 0: x = 2 * 7.2 + 1 ≈ 15
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      expect(termSelect).toHaveBeenCalledWith(0, 0, 3)  // selects "git"
      // Popover should now be visible (mock getSelectionPosition to return a non-null bbox so updatePopoverFromSelection produces a valid result)
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(true)
    })

    it('exits selection when the cancel button is tapped', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()

      await w.find('[data-testid="selection-popover-cancel"]').trigger('click')
      expect(termClearSelection).toHaveBeenCalled()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })

    it('exits selection when control mode is turned off mid-selection', async () => {
      const w = await mountReady()
      const toggle = w.find('[data-testid="mobile-control-toggle"]')
      await toggle.setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(true)

      await toggle.setValue(false)
      await flushPromises()
      expect(termClearSelection).toHaveBeenCalled()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })

    it('exits selection on viewport scroll', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()

      vp.dispatchEvent(new Event('scroll', { bubbles: true }))
      await flushPromises()
      expect(termClearSelection).toHaveBeenCalled()
    })

    it('exits selection on outside (document) pointerdown', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(true)

      document.dispatchEvent(pointerEvent('pointerdown', 500, 500))
      await flushPromises()
      expect(termClearSelection).toHaveBeenCalled()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })

    it('does NOT enter selection on whitespace (wordBoundary len=0)', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git   status')             // whitespace at cols 3,4,5
      vp.dispatchEvent(pointerEvent('pointerdown', 32, 8))  // col 4 with cellW≈7.2 → whitespace
      await new Promise((r) => setTimeout(r, 600))
      expect(termSelect).not.toHaveBeenCalled()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })
  })
```

- [ ] **Step 5.3: Run the new tests and confirm they fail**

```bash
npx vitest run src/mobile/__tests__/MobileTerminal.test.ts
```

Expected: the 7 new tests in `describe('long-press selection')` fail (component doesn't implement the gesture yet).

- [ ] **Step 5.4: Implement the long-press / selection state in `MobileTerminal.vue`**

In `desktop/frontend/src/mobile/MobileTerminal.vue`, add the following pieces.

**Imports (add to the existing import block at the top of `<script setup>`):**

```ts
import { reactive } from 'vue'
import { wordBoundaryAt } from '../lib/wordBoundary'
import { cellCoordsAt } from '../lib/terminalCellCoords'
import { copyTerminalSelection } from '../lib/terminalCopy'
import { prepareSendPayload } from '../lib/terminalContextMenu'
import MobileSelectionPopover from './MobileSelectionPopover.vue'
```

(The `vue` import already has `computed, onMounted, onBeforeUnmount, ref, watch`; ensure `reactive` is added.)

**State (place after the existing `protectClearTimer` declaration):**

```ts
type SelMode = 'idle' | 'pressing' | 'selecting' | 'dragging'
const selMode = ref<SelMode>('idle')
const popover = reactive({
  visible: false,
  x: 0,
  y: 0,
  arrowDir: 'down' as 'down' | 'up',
  copying: false,
  sending: false,
})
let pressTimer: ReturnType<typeof setTimeout> | null = null
let pressAnchor: { x: number; y: number } | null = null
let selectionDisposer: { dispose: () => void } | null = null
let viewportEl: HTMLElement | null = null
const LONG_PRESS_MS = 500
const PRESS_JITTER_PX = 8
```

**Functions (place near the bottom of `<script setup>`, before `onMounted`):**

> **Plan note:** Task 5 ships `onCopy` / `onSend` as bare stubs that just call `exitSelection()`. Task 7 replaces those stubs with the real clipboard / sendInput logic. This split keeps Task 5's review focused on gesture entry / exit without coupling to clipboard or send semantics. The stubs do NOT carry a comment in the code referencing "Task 7" — keep them bare.


```ts
function updatePopoverFromSelection() {
  if (!term || !viewportEl) return
  const pos = term.getSelectionPosition?.()
  if (!pos) { popover.visible = false; return }
  // Convert cell coords → pixel rect. Use cellCoordsAt's reverse: read cell size
  // via the same helper; multiply by cell dims; add viewport rect; subtract scroll.
  const dim = readSelBbox(pos)
  if (!dim) { popover.visible = false; return }
  const popH = 28  // visual height incl. arrow padding; conservative for flip decision
  const fitsAbove = dim.y - 6 - popH >= 8
  popover.arrowDir = fitsAbove ? 'down' : 'up'
  popover.x = dim.x + dim.w / 2
  popover.y = fitsAbove
    ? (window.innerHeight - dim.y + 6)        // CSS `bottom` distance from viewport bottom
    : (dim.y + dim.h + 6)                     // CSS `top` distance from viewport top
  popover.visible = true
}

function readSelBbox(pos: { start: { x: number; y: number }; end: { x: number; y: number } }): { x: number; y: number; w: number; h: number } | null {
  if (!term || !viewportEl) return null
  // Reuse cellCoordsAt's cell-size reader by importing readXtermCellSize would
  // be ideal — for now duplicate the read inline to avoid expanding the public
  // API of terminalCellCoords beyond what tests cover.
  const dim = (term as unknown as { _core?: { _renderService?: { dimensions?: { css?: { cell?: { width?: number; height?: number } } } } } })
    ._core?._renderService?.dimensions?.css?.cell
  const cw = dim?.width ?? (term.options.fontSize ?? 12) * 0.6
  const ch = dim?.height ?? (term.options.fontSize ?? 12) * (term.options.lineHeight ?? 1.0)
  if (!cw || !ch) return null
  const rect = viewportEl.getBoundingClientRect()
  const sTop = viewportEl.scrollTop ?? 0
  const left = rect.left + pos.start.x * cw
  const top = rect.top + pos.start.y * ch - sTop
  // Width: if start.y === end.y, use (end.x - start.x); else span the full
  // viewport width (multi-row selection rect is approximate, popover centers
  // on its midpoint regardless).
  const w = pos.start.y === pos.end.y ? (pos.end.x - pos.start.x) * cw : rect.width
  const h = (pos.end.y - pos.start.y + 1) * ch
  return { x: left, y: top, w, h }
}

function exitSelection() {
  if (term) {
    try { term.clearSelection() } catch { /* ignore */ }
    term.options.disableStdin = !canSend.value
  }
  if (viewportEl) viewportEl.style.touchAction = 'pan-y'
  popover.visible = false
  selMode.value = 'idle'
  if (pressTimer) { clearTimeout(pressTimer); pressTimer = null }
  pressAnchor = null
}

function onSelPointerDown(ev: PointerEvent) {
  if (!canSend.value) return  // strict gate (covers viewer + control-off)
  if (!term || !viewportEl) return
  pressAnchor = { x: ev.clientX, y: ev.clientY }
  selMode.value = 'pressing'
  if (pressTimer) clearTimeout(pressTimer)
  pressTimer = setTimeout(() => {
    pressTimer = null
    if (selMode.value !== 'pressing' || !pressAnchor || !term || !viewportEl) return
    const hit = cellCoordsAt(pressAnchor.x, pressAnchor.y, term, viewportEl)
    if (!hit) { selMode.value = 'idle'; return }
    let line = ''
    try { line = term.buffer.active.getLine(hit.row)?.translateToString(true) ?? '' } catch { line = '' }
    const wb = wordBoundaryAt(line, hit.col)
    if (wb.len === 0) { selMode.value = 'idle'; return }
    try { term.select(wb.start, hit.row, wb.len) } catch { selMode.value = 'idle'; return }
    term.options.disableStdin = true
    if (viewportEl) viewportEl.style.touchAction = 'none'
    selMode.value = 'selecting'
    updatePopoverFromSelection()
  }, LONG_PRESS_MS)
}

function onSelPointerMove(ev: PointerEvent) {
  if (selMode.value === 'pressing' && pressAnchor) {
    const dx = ev.clientX - pressAnchor.x
    const dy = ev.clientY - pressAnchor.y
    if (dx * dx + dy * dy > PRESS_JITTER_PX * PRESS_JITTER_PX) {
      if (pressTimer) { clearTimeout(pressTimer); pressTimer = null }
      selMode.value = 'idle'
      pressAnchor = null
    }
  }
  // dragging logic lands in Task 6
}

function onSelPointerUp() {
  if (selMode.value === 'pressing') {
    // plain tap — cancel pending timer
    if (pressTimer) { clearTimeout(pressTimer); pressTimer = null }
    selMode.value = 'idle'
    pressAnchor = null
  }
  // when in 'selecting', the selection persists; popover waits for user action.
}

function onSelPointerCancel() { onSelPointerUp() }

function onDocumentPointerDown(ev: PointerEvent) {
  if (selMode.value === 'idle') return
  const target = ev.target as Node | null
  // Tap inside the popover or the viewport itself? Ignore the viewport (it owns
  // the gesture) but treat popover taps as in-bounds; everything else exits.
  if (target && viewportEl?.contains(target)) return
  // popover stops propagation in its own template; any pointerdown reaching
  // document is by definition outside the popover, so exit.
  exitSelection()
}

function onCopy() { exitSelection() }
function onSend() { exitSelection() }
function onCancel() { exitSelection() }
```

**Wire listeners in `onMounted`** (after the existing `container.value!.querySelector('.xterm-viewport') ?.addEventListener('touchmove', …)` line):

```ts
  viewportEl = container.value!.querySelector('.xterm-viewport') as HTMLElement | null
  if (viewportEl) {
    viewportEl.addEventListener('pointerdown', onSelPointerDown)
    viewportEl.addEventListener('pointermove', onSelPointerMove)
    viewportEl.addEventListener('pointerup', onSelPointerUp)
    viewportEl.addEventListener('pointercancel', onSelPointerCancel)
    viewportEl.addEventListener('scroll', exitSelection)
  }
  document.addEventListener('pointerdown', onDocumentPointerDown, { capture: true })
  if (term && (term as any).onSelectionChange) {
    selectionDisposer = (term as any).onSelectionChange(() => {
      if (selMode.value === 'selecting' || selMode.value === 'dragging') updatePopoverFromSelection()
    })
  }
```

**Add a `watch(canSend)`** (after the existing `watch(canSend, refreshInputMode)`):

```ts
watch(canSend, (now) => { if (!now && selMode.value !== 'idle') exitSelection() })
```

**Cleanup in `onBeforeUnmount`** (extend the existing block):

```ts
  if (viewportEl) {
    viewportEl.removeEventListener('pointerdown', onSelPointerDown)
    viewportEl.removeEventListener('pointermove', onSelPointerMove)
    viewportEl.removeEventListener('pointerup', onSelPointerUp)
    viewportEl.removeEventListener('pointercancel', onSelPointerCancel)
    viewportEl.removeEventListener('scroll', exitSelection)
  }
  document.removeEventListener('pointerdown', onDocumentPointerDown, { capture: true } as any)
  selectionDisposer?.dispose?.()
  selectionDisposer = null
  if (pressTimer) { clearTimeout(pressTimer); pressTimer = null }
  pressAnchor = null
```

**Template**: add the popover at the top level of the existing `<div class="mobile-term">`, right after the `<div ref="container" class="term" …>` and before `<div v-if="!isDriver" class="viewer-overlay">`:

```vue
    <MobileSelectionPopover
      :visible="popover.visible"
      :x="popover.x"
      :y="popover.y"
      :arrow-dir="popover.arrowDir"
      :copying="popover.copying"
      :sending="popover.sending"
      @copy="onCopy"
      @send="onSend"
      @cancel="onCancel"
    />
```

**Style**: add to the `<style scoped>` block to suppress the iOS system long-press menu and selection callout on the viewport:

```css
/* Suppress iOS system long-press menu / text-selection callout on the terminal
   viewport — we render our own selection UI via xterm + MobileSelectionPopover. */
.term :deep(.xterm-viewport) {
  user-select: none;
  -webkit-user-select: none;
  -webkit-touch-callout: none;
}
```

- [ ] **Step 5.5: Run the tests and confirm they pass**

```bash
npx vitest run src/mobile/__tests__/MobileTerminal.test.ts
npx vue-tsc --noEmit
```

Expected: all tests pass (existing + new); tsc clean.

If the "exits selection on outside (document) pointerdown" test fails because jsdom's `document.dispatchEvent` doesn't reach the capture-phase listener, change the listener registration to `{ capture: false }` and verify the test setup uses bubbling pointerdown correctly. If the listener absolutely must be capture-phase for production (to win against xterm's own pointerdown), keep capture but adjust the test to dispatch on a child element so bubbling reaches the document.

- [ ] **Step 5.6: Commit**

```bash
git add desktop/frontend/src/mobile/MobileTerminal.vue \
        desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts
git commit -m "$(cat <<'EOF'
mobile: wire long-press → word selection + popover entry / exit paths

Pointerdown on .xterm-viewport starts a 500 ms timer (gated by canSend);
on fire, the word at the press cell is selected via term.select(), xterm
draws the highlight, and MobileSelectionPopover appears above. Outside
pointerdown, viewport scroll, control-mode-off mid-selection, or the
cancel button all clear the selection and hide the popover. Copy / send
wiring lands in Task 7; drag-extend in Task 6.
EOF
)"
```

---

## Task 6: Drag to extend the selection + edge auto-scroll

**Files:**
- Modify: `desktop/frontend/src/mobile/MobileTerminal.vue`
- Modify: `desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts`

While the user holds and drags after the long-press has committed (state = `selecting`), each pointermove updates the selection range. Dragging into the top / bottom 24 px of the viewport triggers `term.scrollLines(±3)` every 60 ms so the selection can extend past the visible area.

- [ ] **Step 6.1: Add failing tests for drag-extend + edge-scroll**

Append to the `describe('long-press selection')` block in `MobileTerminal.test.ts`:

```ts
    it('extends the selection within a row on pointermove > 4 px', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status -v')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))    // col 2 of "git"
      await new Promise((r) => setTimeout(r, 600))
      expect(termSelect).toHaveBeenCalledWith(0, 0, 3)         // selects "git"
      termSelect.mockClear()

      // Drag 60 px to the right → about 8 cells over.
      vp.dispatchEvent(pointerEvent('pointermove', 75, 8))
      // Single-row drag uses term.select; multi-row uses term.selectLines.
      expect(termSelect).toHaveBeenCalled()
      const lastCall = termSelect.mock.calls[termSelect.mock.calls.length - 1]
      expect(lastCall[0]).toBe(0)      // start col stays at anchor word start
      expect(lastCall[1]).toBe(0)      // single row
      expect(lastCall[2]).toBeGreaterThan(3)  // grew past "git"
    })

    it('uses selectLines when the drag crosses rows', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))    // row 0
      await new Promise((r) => setTimeout(r, 600))
      termSelectLines.mockClear()

      // Drag down past one row (cellH≈16 → y=40 is row 2-3)
      vp.dispatchEvent(pointerEvent('pointermove', 60, 50))
      expect(termSelectLines).toHaveBeenCalled()
      const lastCall = termSelectLines.mock.calls[termSelectLines.mock.calls.length - 1]
      expect(lastCall[0]).toBe(0)      // start row
      expect(lastCall[1]).toBeGreaterThan(0)  // end row > start row
    })

    it('auto-scrolls when the drag is near the top edge', async () => {
      vi.useFakeTimers()
      try {
        const w = await mountReady()
        await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
        const vp = viewportEl(w)
        setBufferLine('git status')
        vp.dispatchEvent(pointerEvent('pointerdown', 15, 80))
        await vi.advanceTimersByTimeAsync(600)
        termScrollLines.mockClear()

        // Drag into top 24 px zone.
        vp.dispatchEvent(pointerEvent('pointermove', 100, 10))
        await vi.advanceTimersByTimeAsync(120)
        expect(termScrollLines).toHaveBeenCalledWith(-3)
      } finally {
        vi.useRealTimers()
      }
    })

    it('auto-scrolls when the drag is near the bottom edge', async () => {
      vi.useFakeTimers()
      try {
        const w = await mountReady()
        await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
        const vp = viewportEl(w)
        setBufferLine('git status')
        vp.dispatchEvent(pointerEvent('pointerdown', 15, 80))
        await vi.advanceTimersByTimeAsync(600)
        termScrollLines.mockClear()

        // Drag into bottom 24 px zone (viewport bottom is 600).
        vp.dispatchEvent(pointerEvent('pointermove', 100, 590))
        await vi.advanceTimersByTimeAsync(120)
        expect(termScrollLines).toHaveBeenCalledWith(3)
      } finally {
        vi.useRealTimers()
      }
    })

    it('stops auto-scrolling on pointerup', async () => {
      vi.useFakeTimers()
      try {
        const w = await mountReady()
        await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
        const vp = viewportEl(w)
        setBufferLine('git status')
        vp.dispatchEvent(pointerEvent('pointerdown', 15, 80))
        await vi.advanceTimersByTimeAsync(600)
        vp.dispatchEvent(pointerEvent('pointermove', 100, 10))
        await vi.advanceTimersByTimeAsync(120)
        termScrollLines.mockClear()
        vp.dispatchEvent(pointerEvent('pointerup', 100, 10))
        await vi.advanceTimersByTimeAsync(300)
        expect(termScrollLines).not.toHaveBeenCalled()
      } finally {
        vi.useRealTimers()
      }
    })
```

- [ ] **Step 6.2: Run the new tests and confirm they fail**

```bash
npx vitest run src/mobile/__tests__/MobileTerminal.test.ts
```

Expected: the 5 new drag/scroll tests fail.

- [ ] **Step 6.3: Implement drag-extend + edge-scroll in `MobileTerminal.vue`**

Add the following state (alongside the Task 5 state):

```ts
let dragAnchor: { col: number; row: number } | null = null
let edgeScrollTimer: ReturnType<typeof setInterval> | null = null
let edgeScrollDir: -1 | 1 | 0 = 0
const POST_PRESS_DRAG_PX = 4
const EDGE_PX = 24
const EDGE_SCROLL_LINES = 3
const EDGE_SCROLL_INTERVAL_MS = 60
```

Replace the existing `onSelPointerMove` body with:

```ts
function onSelPointerMove(ev: PointerEvent) {
  if (selMode.value === 'pressing' && pressAnchor) {
    const dx = ev.clientX - pressAnchor.x
    const dy = ev.clientY - pressAnchor.y
    if (dx * dx + dy * dy > PRESS_JITTER_PX * PRESS_JITTER_PX) {
      if (pressTimer) { clearTimeout(pressTimer); pressTimer = null }
      selMode.value = 'idle'
      pressAnchor = null
    }
    return
  }

  if (selMode.value !== 'selecting' && selMode.value !== 'dragging') return
  if (!term || !viewportEl) return

  // Decide drag entry: from `selecting`, the first jitter > POST_PRESS_DRAG_PX
  // commits to `dragging`. dragAnchor stores the long-press cell (selection
  // start) so we can compute the new range from it.
  if (selMode.value === 'selecting' && pressAnchor) {
    const dx = ev.clientX - pressAnchor.x
    const dy = ev.clientY - pressAnchor.y
    if (dx * dx + dy * dy < POST_PRESS_DRAG_PX * POST_PRESS_DRAG_PX) return
    const hit = cellCoordsAt(pressAnchor.x, pressAnchor.y, term, viewportEl)
    if (!hit) return
    dragAnchor = hit
    selMode.value = 'dragging'
  }
  if (!dragAnchor) return

  const cur = cellCoordsAt(ev.clientX, ev.clientY, term, viewportEl)
  if (!cur) return
  const [r0, r1] = dragAnchor.row <= cur.row ? [dragAnchor.row, cur.row] : [cur.row, dragAnchor.row]
  try {
    if (r0 === r1) {
      const [c0, c1] = dragAnchor.col <= cur.col ? [dragAnchor.col, cur.col] : [cur.col, dragAnchor.col]
      term.select(c0, r0, Math.max(1, c1 - c0 + 1))
    } else {
      term.selectLines(r0, r1)
    }
  } catch { /* defensive: bad coords leave selection unchanged */ }

  // Edge auto-scroll
  const rect = viewportEl.getBoundingClientRect()
  let dir: -1 | 1 | 0 = 0
  if (ev.clientY - rect.top < EDGE_PX) dir = -1
  else if (rect.bottom - ev.clientY < EDGE_PX) dir = 1
  if (dir === 0) stopEdgeScroll()
  else ensureEdgeScroll(dir)
}

function ensureEdgeScroll(dir: -1 | 1) {
  if (edgeScrollDir === dir && edgeScrollTimer) return
  stopEdgeScroll()
  edgeScrollDir = dir
  edgeScrollTimer = setInterval(() => {
    try { term?.scrollLines(dir * EDGE_SCROLL_LINES) } catch { /* ignore */ }
  }, EDGE_SCROLL_INTERVAL_MS)
}

function stopEdgeScroll() {
  if (edgeScrollTimer) { clearInterval(edgeScrollTimer); edgeScrollTimer = null }
  edgeScrollDir = 0
}
```

Extend `onSelPointerUp` and `onSelPointerCancel` to stop scrolling and end the drag:

```ts
function onSelPointerUp() {
  if (selMode.value === 'pressing') {
    if (pressTimer) { clearTimeout(pressTimer); pressTimer = null }
    selMode.value = 'idle'
    pressAnchor = null
    return
  }
  if (selMode.value === 'dragging') {
    selMode.value = 'selecting'
    dragAnchor = null
    stopEdgeScroll()
  }
}

function onSelPointerCancel() {
  stopEdgeScroll()
  dragAnchor = null
  if (selMode.value !== 'idle') selMode.value = 'selecting'
}
```

Extend `exitSelection` to also stop edge-scroll and clear dragAnchor:

```ts
function exitSelection() {
  if (term) {
    try { term.clearSelection() } catch { /* ignore */ }
    term.options.disableStdin = !canSend.value
  }
  if (viewportEl) viewportEl.style.touchAction = 'pan-y'
  popover.visible = false
  selMode.value = 'idle'
  if (pressTimer) { clearTimeout(pressTimer); pressTimer = null }
  pressAnchor = null
  dragAnchor = null
  stopEdgeScroll()
}
```

Extend `onBeforeUnmount` to stop the scroll interval:

```ts
  stopEdgeScroll()
```

- [ ] **Step 6.4: Run the tests and confirm they pass**

```bash
npx vitest run src/mobile/__tests__/MobileTerminal.test.ts
npx vue-tsc --noEmit
```

Expected: all tests pass (existing + new); tsc clean.

- [ ] **Step 6.5: Commit**

```bash
git add desktop/frontend/src/mobile/MobileTerminal.vue \
        desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts
git commit -m "mobile: extend selection on drag + auto-scroll near viewport edges"
```

---

## Task 7: Copy + Send + Toast

**Files:**
- Modify: `desktop/frontend/src/mobile/MobileTerminal.vue`
- Modify: `desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts`

Wire the popover actions. `onCopy` delegates to `copyTerminalSelection` and shows a toast; `onSend` calls `prepareSendPayload` then `sendRaw`. Both call `exitSelection` on completion. A small local toast is added inside `MobileTerminal.vue` (no global toast component — YAGNI per spec).

- [ ] **Step 7.1: Add failing tests**

Append to the `describe('long-press selection')` block:

```ts
    it('copy: writes the selection to the clipboard and shows a toast', async () => {
      const writeText = vi.fn().mockResolvedValue(undefined)
      Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })

      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      termGetSelection.mockReturnValue('git')
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()

      await w.find('[data-testid="selection-popover-copy"]').trigger('click')
      await flushPromises()
      expect(writeText).toHaveBeenCalledWith('git')
      expect(w.find('[data-testid="mobile-selection-toast"]').exists()).toBe(true)
      expect(w.find('[data-testid="mobile-selection-toast"]').text()).toBe('Copied to clipboard')
      expect(termClearSelection).toHaveBeenCalled()
    })

    it('copy: shows a failure toast when clipboard write rejects', async () => {
      const writeText = vi.fn().mockRejectedValue(new Error('denied'))
      Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })

      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('git status')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      termGetSelection.mockReturnValue('git')
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()

      await w.find('[data-testid="selection-popover-copy"]').trigger('click')
      await flushPromises()
      expect(w.find('[data-testid="mobile-selection-toast"]').text()).toBe('copy failed')
    })

    it('send: prepares payload and forwards to conn.sendInput, then exits selection', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('ls -la')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      termGetSelection.mockReturnValue('ls -la')
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 6, y: 0 } })
      selectionChangeCb?.()
      await flushPromises()

      await w.find('[data-testid="selection-popover-send"]').trigger('click')
      expect(sendInput).toHaveBeenCalledWith('ls -la\r')
      expect(termClearSelection).toHaveBeenCalled()
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })

    it('send: silently no-ops when prepareSendPayload returns null', async () => {
      const w = await mountReady()
      await w.find('[data-testid="mobile-control-toggle"]').setValue(true)
      const vp = viewportEl(w)
      setBufferLine('   ')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      // long-press on whitespace → no selection; fake a selection state to drive
      // the test, then have getSelection return whitespace only.
      termGetSelection.mockReturnValue('   ')
      termGetSelectionPosition.mockReturnValue({ start: { x: 0, y: 0 }, end: { x: 3, y: 0 } })
      // Force selMode to 'selecting' by calling the long-press path with a non-empty line.
      setBufferLine('foo')
      vp.dispatchEvent(pointerEvent('pointerdown', 15, 8))
      await new Promise((r) => setTimeout(r, 600))
      selectionChangeCb?.()
      await flushPromises()
      // Now swap the selection text back to whitespace and tap send.
      termGetSelection.mockReturnValue('   ')

      sendInput.mockClear()
      await w.find('[data-testid="selection-popover-send"]').trigger('click')
      expect(sendInput).not.toHaveBeenCalled()
      // The selection still exits even though no send happened.
      expect(w.find('[data-testid="selection-popover"]').exists()).toBe(false)
    })
```

- [ ] **Step 7.2: Run the new tests and confirm they fail**

```bash
npx vitest run src/mobile/__tests__/MobileTerminal.test.ts
```

Expected: 4 new tests fail.

- [ ] **Step 7.3: Implement copy / send / toast in `MobileTerminal.vue`**

Add toast state and helper near the other selection state:

```ts
const toastText = ref<string | null>(null)
let toastTimer: ReturnType<typeof setTimeout> | null = null
function showToast(msg: string) {
  toastText.value = msg
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { toastText.value = null; toastTimer = null }, 1800)
}
```

Replace the stub `onCopy` and `onSend`:

```ts
async function onCopy() {
  if (!term) { exitSelection(); return }
  if (popover.copying) return
  popover.copying = true
  let ok = false
  try {
    ok = await copyTerminalSelection(term)
  } catch (e) {
    console.warn('[AT Term] copy failed', e)
  }
  popover.copying = false
  showToast(ok ? t('mobile.selection.copied') : t('terminal.copyFailed'))
  exitSelection()
}

function onSend() {
  if (!term || !conn) { exitSelection(); return }
  if (popover.sending) return
  popover.sending = true
  try {
    const payload = prepareSendPayload(term.getSelection())
    if (payload) sendRaw(payload)
  } finally {
    popover.sending = false
    exitSelection()
  }
}
```

Add the toast to the template, immediately after `<MobileSelectionPopover ...>`:

```vue
    <div v-if="toastText" class="sel-toast" data-testid="mobile-selection-toast">{{ toastText }}</div>
```

And add styling at the bottom of the `<style scoped>`:

```css
.sel-toast {
  position: absolute;
  left: 50%;
  transform: translateX(-50%);
  bottom: calc(80px + env(safe-area-inset-bottom));
  background: rgba(20, 22, 30, 0.92);
  color: #fff;
  padding: 6px 14px;
  border-radius: 14px;
  font-size: 12px;
  z-index: 1001;
  pointer-events: none;
}
```

Clean up `toastTimer` in `onBeforeUnmount`:

```ts
  if (toastTimer) { clearTimeout(toastTimer); toastTimer = null }
```

- [ ] **Step 7.4: Run the tests and confirm they pass**

```bash
npx vitest run src/mobile/__tests__/MobileTerminal.test.ts
npx vue-tsc --noEmit
```

Expected: all tests pass.

- [ ] **Step 7.5: Commit**

```bash
git add desktop/frontend/src/mobile/MobileTerminal.vue \
        desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts
git commit -m "$(cat <<'EOF'
mobile: wire selection popover copy / send + add inline toast

Copy delegates to lib/terminalCopy.copyTerminalSelection and toasts
"Copied to clipboard" (or "copy failed" via the existing i18n key).
Send delegates to lib/terminalContextMenu.prepareSendPayload — strip C1
+ LF→CR + append \r — then sendRaw → conn.sendInput. Both clear the
xterm selection and hide the popover. Toast is local to MobileTerminal
(no global toast component yet — YAGNI).
EOF
)"
```

---

## Task 8: End-to-end verification

**Files:** none changed; this task verifies everything holds together.

- [ ] **Step 8.1: Run the full frontend test suite**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend
npx vitest run
```

Expected: PASS. The new tests should add up roughly: `wordBoundary.test.ts` +9, `terminalCellCoords.test.ts` +7, `MobileSelectionPopover.test.ts` +9, `mobile-selection.test.ts` +2, `MobileTerminal.test.ts` +13.

- [ ] **Step 8.2: Type-check**

```bash
npx vue-tsc --noEmit
```

Expected: no errors.

- [ ] **Step 8.3: Manual iOS verification**

Build the Capacitor bundle and load it in the iOS simulator (or on device via TestFlight build, per project workflow — see `desktop/frontend/dist-capacitor`). Confirm each item:

1. driver + control on → long-press a word → blue highlight + popover appears above
2. Without lifting, drag horizontally → selection extends within the row
3. Without lifting, drag down past a row → selection extends multi-row
4. Drag into the top or bottom 24 px → scrollback auto-scrolls
5. Lift, then tap outside the selection → selection clears, popover disappears
6. Lift, tap Copy → switch to Notes → paste → text matches
7. Lift, tap Send `ls -la` → terminal runs the command
8. Toast `Copied to clipboard` flashes for ~1.8 s after copy
9. Toggle control mode off → long-press → nothing happens (existing protect-banner shake still fires from `onTermPointerDown`)
10. View-only remote session (where `remote_permission='view'`) → long-press → nothing happens
11. Long-press while the on-screen keyboard is up → keyboard collapses (existing `collapseKeyboardIfOpen`), then long-press fires after 500 ms and popover appears
12. Non-selection touch (short tap on viewport) → `.xterm-viewport` scroll continues to work; no selection state side-effects

If you cannot run the simulator (no macOS GUI access), explicitly note that step 8.3 is deferred and ask the user to verify before merging — do NOT claim the change is complete.

- [ ] **Step 8.4: If a regression appeared during 8.1–8.3, fix it inline and commit; otherwise no commit needed**

```bash
git status
```

If clean, nothing to do. If a fix was needed, stage the change and commit with a short message describing the regression and fix.

---

## Spec coverage check

- Spec §Goal "long-press selects a word" → Task 5 (gesture + word boundary + `term.select`)
- Spec §Goal "drag to extend" → Task 6
- Spec §Goal "popover copy / send" → Task 4 (component) + Task 7 (wiring)
- Spec §Architecture "reuse xterm public select API" → Tasks 1-6 (no internal API except the optional dimensions read in Task 2, with a documented fallback)
- Spec §Gesture state machine → Task 5 (pressing → selecting → exit paths) + Task 6 (dragging + edge-scroll)
- Spec §"strict canSend gate" → Task 5 (returns early on `!canSend.value` in `onSelPointerDown`)
- Spec §Popover styling + flip logic → Task 4 (component + `arrowDir`)
- Spec §Popover positioning math → Task 5 (`updatePopoverFromSelection`, `readSelBbox`)
- Spec §`disableStdin` + `touchAction` toggling → Task 5
- Spec §Error handling (copy reject, whitespace, out-of-viewport, Haptics unavailable, term.select throws) → Task 5 (whitespace, out-of-viewport, term.select throw) + Task 7 (copy reject). Haptics is **not implemented in this plan** — the spec listed it best-effort and its inclusion would require adding `@capacitor/haptics` to package.json. Adding it is out of scope; the spec note remains a deferred polish. The plan is correct in omitting it; mention as known omission.
- Spec §Testing (word boundary 9 cases, cellCoords 7 cases, popover 9 cases, MobileTerminal 13 cases) → Tasks 1, 2, 4, 5-7
- Spec §Manual verification → Task 8.3
- Spec §Open risk "xterm internal touch behaviour" → addressed by the document-level capture-phase `onDocumentPointerDown` and the `viewportEl.style.touchAction = 'none'` toggle. If the manual verification (8.3 step 1) reveals xterm intercepts before our handler fires, the fix is to register the viewport listeners with `{ capture: true }`. The plan leaves this as a manual-verification adjustment rather than guessing now.
- Spec §`user-select: none; -webkit-touch-callout: none` on `.xterm-viewport` → Task 5 style block

**Known deferral**: Haptics (Capacitor Haptics plugin not currently in dependencies). The spec called this best-effort; deferring keeps the plan dependency-neutral. If the user wants haptic feedback now, add a follow-up task to install `@capacitor/haptics` and call `Haptics.impact({ style: ImpactStyle.Light }).catch(()=>{})` in the long-press timer fire path.
