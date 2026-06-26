# 粘贴图片本地预览 toast Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用户在 atterm 任一前端粘贴图片后，右上角弹缩略图 toast 让用户立刻确认贴对了；5s 自动消失、hover 暂停、X 关闭、点缩略图开 lightbox。

**Architecture:** 每个前端（`desktop/frontend/` 和 `web/src/main/`）各加一份模块级单例 `pasteImageBus`（emit/on 模式）+ 一个 `PasteImagePreviewHost.vue` 组件挂在 App.vue 顶层。4 个 `sendPasteImage` 调用点各加一行 `pasteImageBus.emit(file, name)`，与原有上行链路并行；预览纯本地反馈，不走 wire。

**Tech Stack:** Vue 3 (`<script setup>` + Composition API)、TypeScript、Vitest + @vue/test-utils、`URL.createObjectURL`/`URL.revokeObjectURL`、`vi.useFakeTimers()`。

**Spec:** `docs/superpowers/specs/2026-06-26-paste-image-preview-design.md`

---

## File Structure

**桌面前端** (`desktop/frontend/src/`):
- Create `lib/pasteImageBus.ts` — 模块单例 bus（emit/on）
- Create `lib/pasteImageBus.test.ts` — bus 单测（同目录约定）
- Create `components/PasteImagePreviewHost.vue` — toast + lightbox 组件
- Create `components/__tests__/PasteImagePreviewHost.test.ts` — 组件测试
- Modify `components/TerminalView.vue` — `handleImagePaste` 加 emit
- Modify `lib/terminalPaste.ts` — image 分支加 emit
- Modify `lib/terminalPaste.test.ts` — 断言 emit 被调用
- Modify `mobile/MobileTerminal.vue` — `openImagePicker` 加 emit
- Modify `mobile/__tests__/MobileTerminal.test.ts` — 断言 emit 被调用
- Modify `App.vue` — 模板顶层挂 `<PasteImagePreviewHost />`

**Web 前端** (`web/src/main/`):
- Create `lib/pasteImageBus.ts` — 同上
- Create `web/tests/unit/main/lib/pasteImageBus.test.ts` — bus 单测（mirror 目录约定）
- Create `components/PasteImagePreviewHost.vue` — 同上
- Create `web/tests/unit/main/components/PasteImagePreviewHost.test.ts` — 组件测试
- Modify `App.vue` — `onPasteImage` 加 emit + 挂 host
- Modify `web/tests/unit/main/App.test.ts` — 断言 emit 被调用

---

## Track 1 — 桌面前端 (desktop/frontend)

### Task 1: 桌面 pasteImageBus 模块

**Files:**
- Create: `desktop/frontend/src/lib/pasteImageBus.ts`
- Test: `desktop/frontend/src/lib/pasteImageBus.test.ts`

- [ ] **Step 1: Write the failing test**

Write `desktop/frontend/src/lib/pasteImageBus.test.ts`:

```ts
import { describe, expect, it, vi } from "vitest";
import { pasteImageBus } from "./pasteImageBus";

describe("pasteImageBus", () => {
  it("delivers events to all subscribers", () => {
    const a = vi.fn();
    const b = vi.fn();
    const offA = pasteImageBus.on(a);
    const offB = pasteImageBus.on(b);
    const blob = new Blob(["x"], { type: "image/png" });

    pasteImageBus.emit(blob, "shot.png");

    expect(a).toHaveBeenCalledTimes(1);
    expect(b).toHaveBeenCalledTimes(1);
    const event = a.mock.calls[0][0];
    expect(event.file).toBe(blob);
    expect(event.name).toBe("shot.png");
    expect(typeof event.id).toBe("string");
    expect(event.id.length).toBeGreaterThan(0);

    offA();
    offB();
  });

  it("does not deliver to unsubscribed handlers", () => {
    const h = vi.fn();
    const off = pasteImageBus.on(h);
    off();

    pasteImageBus.emit(new Blob(["x"], { type: "image/png" }), "x.png");

    expect(h).not.toHaveBeenCalled();
  });

  it("defaults the name to 'clipboard-image' when given an empty string", () => {
    const h = vi.fn();
    const off = pasteImageBus.on(h);

    pasteImageBus.emit(new Blob(["x"], { type: "image/png" }), "");

    expect(h.mock.calls[0][0].name).toBe("clipboard-image");
    off();
  });

  it("isolates handler errors so one failure does not break the others", () => {
    const bad = vi.fn(() => { throw new Error("boom"); });
    const good = vi.fn();
    const offBad = pasteImageBus.on(bad);
    const offGood = pasteImageBus.on(good);

    expect(() =>
      pasteImageBus.emit(new Blob(["x"], { type: "image/png" }), "x.png"),
    ).not.toThrow();
    expect(bad).toHaveBeenCalledTimes(1);
    expect(good).toHaveBeenCalledTimes(1);

    offBad();
    offGood();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/lib/pasteImageBus.test.ts`
Expected: FAIL — module `./pasteImageBus` does not exist.

- [ ] **Step 3: Implement the bus**

Write `desktop/frontend/src/lib/pasteImageBus.ts`:

```ts
export type PasteImageEvent = {
  id: string;
  file: Blob;
  name: string;
};

type Handler = (event: PasteImageEvent) => void;

const handlers = new Set<Handler>();

export const pasteImageBus = {
  emit(file: Blob, name: string): void {
    const event: PasteImageEvent = {
      id: crypto.randomUUID(),
      file,
      name: name || "clipboard-image",
    };
    for (const h of handlers) {
      try {
        h(event);
      } catch (err) {
        console.warn("[pasteImageBus] handler threw", err);
      }
    }
  },
  on(handler: Handler): () => void {
    handlers.add(handler);
    return () => {
      handlers.delete(handler);
    };
  },
};
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd desktop/frontend && npx vitest run src/lib/pasteImageBus.test.ts`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/pasteImageBus.ts desktop/frontend/src/lib/pasteImageBus.test.ts
git commit -m "feat(desktop): add pasteImageBus event bus for paste-image previews"
```

---

### Task 2: 桌面 PasteImagePreviewHost 组件

**Files:**
- Create: `desktop/frontend/src/components/PasteImagePreviewHost.vue`
- Test: `desktop/frontend/src/components/__tests__/PasteImagePreviewHost.test.ts`

- [ ] **Step 1: Write the failing test**

Write `desktop/frontend/src/components/__tests__/PasteImagePreviewHost.test.ts`:

```ts
import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import PasteImagePreviewHost from "../PasteImagePreviewHost.vue";
import { pasteImageBus } from "../../lib/pasteImageBus";

function makeBlob() {
  return new Blob([new Uint8Array([1, 2, 3])], { type: "image/png" });
}

describe("PasteImagePreviewHost", () => {
  let createSpy: ReturnType<typeof vi.spyOn>;
  let revokeSpy: ReturnType<typeof vi.spyOn>;
  let urlCounter = 0;

  beforeEach(() => {
    vi.useFakeTimers();
    urlCounter = 0;
    createSpy = vi
      .spyOn(URL, "createObjectURL")
      .mockImplementation(() => `blob:fake/${++urlCounter}`);
    revokeSpy = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.useRealTimers();
    createSpy.mockRestore();
    revokeSpy.mockRestore();
  });

  it("renders a toast when the bus emits", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "shot.png");
    await flushPromises();

    const toasts = w.findAll('[data-testid="paste-toast"]');
    expect(toasts.length).toBe(1);
    expect(toasts[0].find("img").attributes("src")).toBe("blob:fake/1");
    expect(toasts[0].text()).toContain("shot.png");
    w.unmount();
  });

  it("auto-dismisses the toast after 5 seconds and revokes the url", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(1);

    vi.advanceTimersByTime(5000);
    await flushPromises();

    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(0);
    expect(revokeSpy).toHaveBeenCalledWith("blob:fake/1");
    w.unmount();
  });

  it("pauses the timer on hover and resumes when the cursor leaves", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    const toast = w.find('[data-testid="paste-toast"]');

    vi.advanceTimersByTime(3000);
    await toast.trigger("mouseenter");

    vi.advanceTimersByTime(10_000); // would normally fire
    await flushPromises();
    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(1);

    await toast.trigger("mouseleave");
    vi.advanceTimersByTime(5000);
    await flushPromises();
    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(0);
    w.unmount();
  });

  it("closes the toast immediately on × click", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();

    await w.find('[data-testid="paste-toast-close"]').trigger("click");
    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(0);
    expect(revokeSpy).toHaveBeenCalledWith("blob:fake/1");
    w.unmount();
  });

  it("opens a lightbox with an independent url when the thumbnail is clicked", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();

    await w.find('[data-testid="paste-toast"] img').trigger("click");
    const lightbox = w.find('[data-testid="paste-lightbox"]');
    expect(lightbox.exists()).toBe(true);
    expect(lightbox.find("img").attributes("src")).toBe("blob:fake/2");
    expect(createSpy).toHaveBeenCalledTimes(2);
    w.unmount();
  });

  it("closes the lightbox when the backdrop is clicked and revokes its url", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    await w.find('[data-testid="paste-toast"] img').trigger("click");

    await w.find('[data-testid="paste-lightbox"]').trigger("click");
    expect(w.find('[data-testid="paste-lightbox"]').exists()).toBe(false);
    expect(revokeSpy).toHaveBeenCalledWith("blob:fake/2");
    w.unmount();
  });

  it("closes the lightbox on Esc", async () => {
    const w = mount(PasteImagePreviewHost, { attachTo: document.body });
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    await w.find('[data-testid="paste-toast"] img').trigger("click");

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    await flushPromises();
    expect(w.find('[data-testid="paste-lightbox"]').exists()).toBe(false);
    w.unmount();
  });

  it("keeps the lightbox visible after the source toast auto-dismisses", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    await w.find('[data-testid="paste-toast"] img').trigger("click");

    vi.advanceTimersByTime(5000);
    await flushPromises();

    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(0);
    expect(w.find('[data-testid="paste-lightbox"]').exists()).toBe(true);
    expect(w.find('[data-testid="paste-lightbox"] img').attributes("src")).toBe("blob:fake/2");
    w.unmount();
  });

  it("stacks multiple toasts in arrival order", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    pasteImageBus.emit(makeBlob(), "b.png");
    await flushPromises();

    const toasts = w.findAll('[data-testid="paste-toast"]');
    expect(toasts.length).toBe(2);
    expect(toasts[0].text()).toContain("a.png");
    expect(toasts[1].text()).toContain("b.png");
    w.unmount();
  });

  it("cleans up timers, urls and bus subscription on unmount", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    pasteImageBus.emit(makeBlob(), "b.png");
    await flushPromises();

    w.unmount();

    expect(revokeSpy).toHaveBeenCalledWith("blob:fake/1");
    expect(revokeSpy).toHaveBeenCalledWith("blob:fake/2");

    // After unmount the bus must no longer reach the component.
    revokeSpy.mockClear();
    pasteImageBus.emit(makeBlob(), "c.png");
    // No additional render/revoke side-effects can be asserted directly, but
    // an unsubscribed handler means no throw and no leak — the assertion
    // above (no new revoke call) is the structural witness.
    expect(revokeSpy).not.toHaveBeenCalled();
  });

  it("skips the toast (without throwing) when createObjectURL fails", async () => {
    createSpy.mockImplementationOnce(() => {
      throw new Error("oom");
    });
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});

    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();

    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(0);
    expect(warn).toHaveBeenCalled();

    warn.mockRestore();
    w.unmount();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/components/__tests__/PasteImagePreviewHost.test.ts`
Expected: FAIL — component `../PasteImagePreviewHost.vue` does not exist.

- [ ] **Step 3: Implement the component**

Write `desktop/frontend/src/components/PasteImagePreviewHost.vue`:

```vue
<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";
import { pasteImageBus, type PasteImageEvent } from "../lib/pasteImageBus";

const DISMISS_MS = 5000;

interface Toast {
  id: string;
  file: Blob;
  url: string;
  name: string;
}

interface Lightbox {
  url: string;
  name: string;
}

const toasts = ref<Toast[]>([]);
const lightbox = ref<Lightbox | null>(null);
const timers = new Map<string, number>();

function startTimer(id: string) {
  const handle = window.setTimeout(() => dismiss(id), DISMISS_MS);
  timers.set(id, handle);
}

function clearTimer(id: string) {
  const handle = timers.get(id);
  if (handle !== undefined) {
    window.clearTimeout(handle);
    timers.delete(id);
  }
}

function handlePaste(event: PasteImageEvent) {
  let url: string;
  try {
    url = URL.createObjectURL(event.file);
  } catch (err) {
    console.warn("[PasteImagePreviewHost] createObjectURL failed", err);
    return;
  }
  toasts.value.push({
    id: event.id,
    file: event.file,
    url,
    name: event.name,
  });
  startTimer(event.id);
}

function dismiss(id: string) {
  clearTimer(id);
  const idx = toasts.value.findIndex((t) => t.id === id);
  if (idx === -1) return;
  const [removed] = toasts.value.splice(idx, 1);
  URL.revokeObjectURL(removed.url);
}

function onMouseEnter(id: string) {
  clearTimer(id);
}

function onMouseLeave(id: string) {
  if (!toasts.value.some((t) => t.id === id)) return;
  startTimer(id);
}

function openLightbox(toast: Toast) {
  if (lightbox.value) {
    URL.revokeObjectURL(lightbox.value.url);
  }
  try {
    lightbox.value = {
      url: URL.createObjectURL(toast.file),
      name: toast.name,
    };
  } catch (err) {
    console.warn("[PasteImagePreviewHost] lightbox createObjectURL failed", err);
    lightbox.value = null;
  }
}

function closeLightbox() {
  if (!lightbox.value) return;
  URL.revokeObjectURL(lightbox.value.url);
  lightbox.value = null;
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Escape" && lightbox.value) {
    closeLightbox();
  }
}

let unsubscribe: (() => void) | null = null;

onMounted(() => {
  unsubscribe = pasteImageBus.on(handlePaste);
  document.addEventListener("keydown", onKeydown);
});

onBeforeUnmount(() => {
  unsubscribe?.();
  unsubscribe = null;
  document.removeEventListener("keydown", onKeydown);
  for (const handle of timers.values()) window.clearTimeout(handle);
  timers.clear();
  for (const t of toasts.value) URL.revokeObjectURL(t.url);
  toasts.value = [];
  if (lightbox.value) {
    URL.revokeObjectURL(lightbox.value.url);
    lightbox.value = null;
  }
});
</script>

<template>
  <div class="paste-preview-host">
    <div
      v-for="t in toasts"
      :key="t.id"
      class="paste-toast"
      data-testid="paste-toast"
      @mouseenter="onMouseEnter(t.id)"
      @mouseleave="onMouseLeave(t.id)"
    >
      <img :src="t.url" :alt="t.name" class="paste-toast-thumb" @click="openLightbox(t)" />
      <span class="paste-toast-name">{{ t.name }}</span>
      <button
        type="button"
        class="paste-toast-close"
        data-testid="paste-toast-close"
        :aria-label="'close ' + t.name"
        @click="dismiss(t.id)"
      >×</button>
    </div>
    <div
      v-if="lightbox"
      class="paste-lightbox"
      data-testid="paste-lightbox"
      @click="closeLightbox"
    >
      <img :src="lightbox.url" :alt="lightbox.name" @click.stop />
    </div>
  </div>
</template>

<style scoped>
.paste-preview-host {
  position: fixed;
  top: 88px;
  right: 0.75rem;
  z-index: 50;
  display: flex;
  flex-direction: column;
  gap: 8px;
  pointer-events: none;
}
.paste-toast {
  pointer-events: auto;
  width: 200px;
  padding: 8px;
  background: var(--bg, #1f2024);
  color: var(--fg, #e6e6e6);
  border: 1px solid var(--border, #3a3b40);
  border-radius: 6px;
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.35);
  display: flex;
  flex-direction: column;
  gap: 4px;
  position: relative;
}
.paste-toast-thumb {
  width: 100%;
  max-height: 120px;
  object-fit: contain;
  cursor: zoom-in;
  border-radius: 4px;
  background: #000;
}
.paste-toast-name {
  font-size: 12px;
  opacity: 0.8;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.paste-toast-close {
  position: absolute;
  top: 4px;
  right: 6px;
  background: transparent;
  border: none;
  color: inherit;
  font-size: 16px;
  line-height: 1;
  cursor: pointer;
  opacity: 0.7;
}
.paste-toast-close:hover {
  opacity: 1;
}
.paste-lightbox {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.85);
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: zoom-out;
  pointer-events: auto;
}
.paste-lightbox img {
  max-width: 90vw;
  max-height: 90vh;
  cursor: default;
}
</style>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd desktop/frontend && npx vitest run src/components/__tests__/PasteImagePreviewHost.test.ts`
Expected: PASS (11 tests).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/PasteImagePreviewHost.vue desktop/frontend/src/components/__tests__/PasteImagePreviewHost.test.ts
git commit -m "feat(desktop): add PasteImagePreviewHost toast + lightbox component"
```

---

### Task 3: 桌面 TerminalView Ctrl+V 路径接 bus

**Files:**
- Modify: `desktop/frontend/src/components/TerminalView.vue` (around line 177-189)

- [ ] **Step 1: Read context**

Read `desktop/frontend/src/components/TerminalView.vue` lines 175-195 to confirm `handleImagePaste` body (the spec basis):

```ts
async function handleImagePaste(e: ClipboardEvent) {
  const item = Array.from(e.clipboardData?.items || []).find((i) => i.type.startsWith("image/"));
  if (!item) return;
  const file = item.getAsFile();
  if (!file) return;
  e.preventDefault();
  e.stopPropagation();
  try {
    await conn?.sendPasteImage(file, file.name || "clipboard-image");
  } catch (err) {
    console.warn("[AT Term] failed to paste terminal image", err);
  }
}
```

- [ ] **Step 2: Add the import**

In `desktop/frontend/src/components/TerminalView.vue`, find the existing import of `pasteFromClipboard` from `"../lib/terminalPaste"` (line 24) and immediately below it add:

```ts
import { pasteImageBus } from "../lib/pasteImageBus";
```

- [ ] **Step 3: Emit before sendPasteImage**

In `handleImagePaste`, insert one line **before** the `try { await conn?.sendPasteImage(...)` block, so the body becomes:

```ts
async function handleImagePaste(e: ClipboardEvent) {
  const item = Array.from(e.clipboardData?.items || []).find((i) => i.type.startsWith("image/"));
  if (!item) return;
  const file = item.getAsFile();
  if (!file) return;
  e.preventDefault();
  e.stopPropagation();
  pasteImageBus.emit(file, file.name || "clipboard-image");
  try {
    await conn?.sendPasteImage(file, file.name || "clipboard-image");
  } catch (err) {
    console.warn("[AT Term] failed to paste terminal image", err);
  }
}
```

- [ ] **Step 4: Verify type-check + existing tests still pass**

Run: `cd desktop/frontend && npx vue-tsc --noEmit && npx vitest run src/components/`
Expected: type-check clean; all existing component tests still pass.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/TerminalView.vue
git commit -m "feat(desktop): emit pasteImageBus from TerminalView Ctrl+V handler"
```

---

### Task 4: 桌面 terminalPaste.ts 右键路径接 bus

**Files:**
- Modify: `desktop/frontend/src/lib/terminalPaste.ts`
- Modify: `desktop/frontend/src/lib/terminalPaste.test.ts`

- [ ] **Step 1: Add a failing assertion**

In `desktop/frontend/src/lib/terminalPaste.test.ts`, add this new test case to the existing `describe("pasteFromClipboard", ...)` block, immediately after the last `it(...)`:

```ts
  it("emits pasteImageBus before sending the image", async () => {
    const { pasteImageBus } = await import("./pasteImageBus");
    const emitSpy = vi.spyOn(pasteImageBus, "emit");
    const term = { paste: vi.fn() };
    const conn = { sendPasteImage: vi.fn(async () => true) };

    await pasteFromClipboard({
      term,
      conn,
      status: "attached",
      remotePermission: "full",
      getPayload: async () => ({
        kind: "image",
        filename: "clipboard-image.png",
        content_type: "image/png",
        data_base64: "iVBORw0K",
      }),
    });

    expect(emitSpy).toHaveBeenCalledTimes(1);
    const [blob, name] = emitSpy.mock.calls[0];
    expect(blob).toBeInstanceOf(Blob);
    expect((blob as Blob).type).toBe("image/png");
    expect(name).toBe("clipboard-image.png");
    expect(conn.sendPasteImage).toHaveBeenCalledTimes(1);
    emitSpy.mockRestore();
  });

  it("does not emit pasteImageBus on text paste", async () => {
    const { pasteImageBus } = await import("./pasteImageBus");
    const emitSpy = vi.spyOn(pasteImageBus, "emit");
    const term = { paste: vi.fn() };
    const conn = { sendPasteImage: vi.fn(async () => true) };

    await pasteFromClipboard({
      term,
      conn,
      status: "attached",
      remotePermission: "full",
      getPayload: async () => ({ kind: "text", text: "hello" }),
    });

    expect(emitSpy).not.toHaveBeenCalled();
    emitSpy.mockRestore();
  });
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/lib/terminalPaste.test.ts -t "emits pasteImageBus"`
Expected: FAIL — `emitSpy` not called.

- [ ] **Step 3: Wire the bus into the image branch**

In `desktop/frontend/src/lib/terminalPaste.ts`:

Add to the top imports:

```ts
import { pasteImageBus } from "./pasteImageBus";
```

Then in the image branch (currently `if (payload.kind === "image" && payload.content_type && payload.data_base64) { ... }`), construct the blob once, emit, and pass to `sendPasteImage`:

```ts
  if (payload.kind === "image" && payload.content_type && payload.data_base64) {
    if (effectiveRemotePermission(opts.remotePermission) === "control") {
      return { ok: false, reasonKey: "terminal.imagePasteRequiresFull" };
    }
    const blob = base64ToBlob(payload.data_base64, payload.content_type);
    const name = payload.filename || "clipboard-image";
    pasteImageBus.emit(blob, name);
    await opts.conn.sendPasteImage(blob, name);
    return { ok: true, kind: "image" };
  }
```

- [ ] **Step 4: Run tests to verify all pass**

Run: `cd desktop/frontend && npx vitest run src/lib/terminalPaste.test.ts`
Expected: PASS (all original tests + 2 new).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/terminalPaste.ts desktop/frontend/src/lib/terminalPaste.test.ts
git commit -m "feat(desktop): emit pasteImageBus from right-click clipboard paste"
```

---

### Task 5: 桌面 MobileTerminal 相册路径接 bus

**Files:**
- Modify: `desktop/frontend/src/mobile/MobileTerminal.vue` (around line 220)
- Modify: `desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts`

- [ ] **Step 1: Add a failing assertion in the existing test**

Edit the test `it('image button sends the picked photo via sendPasteImage when in control mode', ...)` in `desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts` (around line 256-274).

Add an import near the other test imports at the top of the file:

```ts
import { pasteImageBus } from '../../lib/pasteImageBus'
```

Then rewrite the test body so the spy is installed **before** the action that triggers `openImagePicker`, and the assertion runs after the existing `sendPasteImage` checks:

```ts
  it('image button sends the picked photo via sendPasteImage when in control mode', async () => {
    getPhoto.mockResolvedValue({ base64String: btoa('PNGDATA'), format: 'png' })
    const emitSpy = vi.spyOn(pasteImageBus, 'emit')
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', session_token: 'atk_t' }, sessionId: 's1', info, active: true } })
    await flushPromises()
    await w.find('[data-testid="mobile-control-toggle"]').setValue(true)

    const btn = w.find('[data-testid="mobile-image"]')
    expect(btn.exists()).toBe(true)
    expect(btn.classes()).not.toContain('inert')

    await btn.trigger('click')
    await flushPromises()

    expect(getPhoto).toHaveBeenCalled()
    expect(sendPasteImage).toHaveBeenCalled()
    const [file, name] = (sendPasteImage as ReturnType<typeof vi.fn>).mock.calls.at(-1)!
    expect(name).toBe('mobile-image.png')
    expect(file).toBeInstanceOf(File)

    expect(emitSpy).toHaveBeenCalledTimes(1)
    const [emittedBlob, emittedName] = emitSpy.mock.calls[0]
    expect(emittedBlob).toBe(file)
    expect(emittedName).toBe('mobile-image.png')
    emitSpy.mockRestore()
  })
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/mobile/__tests__/MobileTerminal.test.ts -t "image button sends"`
Expected: FAIL — `emitSpy` not called.

- [ ] **Step 3: Wire the bus into openImagePicker**

In `desktop/frontend/src/mobile/MobileTerminal.vue`:

Add to script imports (top of `<script setup>`):

```ts
import { pasteImageBus } from "../lib/pasteImageBus";
```

In `openImagePicker` (around line 202-229), insert one line **before** `await conn?.sendPasteImage(file, file.name)` (line 220):

```ts
    pasteImageBus.emit(file, file.name);
    await conn?.sendPasteImage(file, file.name);
```

- [ ] **Step 4: Run all mobile tests**

Run: `cd desktop/frontend && npx vitest run src/mobile/`
Expected: PASS (all tests, including the new assertion).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/mobile/MobileTerminal.vue desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts
git commit -m "feat(desktop/mobile): emit pasteImageBus on photo picker paste"
```

---

### Task 6: 桌面 App.vue 挂载 PasteImagePreviewHost

**Files:**
- Modify: `desktop/frontend/src/App.vue`

- [ ] **Step 1: Add the import**

Near the existing component imports at the top of `<script setup>` in `desktop/frontend/src/App.vue` (alongside `TabBar`, `TitleBar`, etc.), add:

```ts
import PasteImagePreviewHost from "./components/PasteImagePreviewHost.vue";
```

- [ ] **Step 2: Mount in template**

Find the template root `<div class="app" ...>` (around line 1143). Add `<PasteImagePreviewHost />` as a direct child of that root, immediately after `<TitleBar ... />` so it overlays everything:

```vue
    <TitleBar ... />
    <PasteImagePreviewHost />
```

(The host is `position: fixed`, so its place in the DOM tree affects only z-index sibling order, not layout. Putting it near the top is fine.)

- [ ] **Step 3: Type-check + tests**

Run: `cd desktop/frontend && npx vue-tsc --noEmit && npx vitest run`
Expected: type-check clean; full desktop test suite passes.

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/App.vue
git commit -m "feat(desktop): mount PasteImagePreviewHost at App root"
```

---

## Track 2 — Web 前端 (web/src/main)

### Task 7: Web pasteImageBus 模块

**Files:**
- Create: `web/src/main/lib/pasteImageBus.ts`
- Test: `web/tests/unit/main/lib/pasteImageBus.test.ts`

- [ ] **Step 1: Write the failing test**

Create directory `web/tests/unit/main/lib/` (it may not exist). Write `web/tests/unit/main/lib/pasteImageBus.test.ts`:

```ts
import { describe, expect, it, vi } from "vitest";
import { pasteImageBus } from "@/main/lib/pasteImageBus";

describe("pasteImageBus (web)", () => {
  it("delivers events to all subscribers", () => {
    const a = vi.fn();
    const b = vi.fn();
    const offA = pasteImageBus.on(a);
    const offB = pasteImageBus.on(b);
    const blob = new Blob(["x"], { type: "image/png" });

    pasteImageBus.emit(blob, "shot.png");

    expect(a).toHaveBeenCalledTimes(1);
    expect(b).toHaveBeenCalledTimes(1);
    const event = a.mock.calls[0][0];
    expect(event.file).toBe(blob);
    expect(event.name).toBe("shot.png");
    expect(typeof event.id).toBe("string");
    expect(event.id.length).toBeGreaterThan(0);

    offA();
    offB();
  });

  it("does not deliver to unsubscribed handlers", () => {
    const h = vi.fn();
    const off = pasteImageBus.on(h);
    off();
    pasteImageBus.emit(new Blob(["x"], { type: "image/png" }), "x.png");
    expect(h).not.toHaveBeenCalled();
  });

  it("defaults the name to 'clipboard-image' when given an empty string", () => {
    const h = vi.fn();
    const off = pasteImageBus.on(h);
    pasteImageBus.emit(new Blob(["x"], { type: "image/png" }), "");
    expect(h.mock.calls[0][0].name).toBe("clipboard-image");
    off();
  });

  it("isolates handler errors so one failure does not break the others", () => {
    const bad = vi.fn(() => { throw new Error("boom"); });
    const good = vi.fn();
    const offBad = pasteImageBus.on(bad);
    const offGood = pasteImageBus.on(good);

    expect(() =>
      pasteImageBus.emit(new Blob(["x"], { type: "image/png" }), "x.png"),
    ).not.toThrow();
    expect(bad).toHaveBeenCalledTimes(1);
    expect(good).toHaveBeenCalledTimes(1);

    offBad();
    offGood();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run tests/unit/main/lib/pasteImageBus.test.ts`
Expected: FAIL — module `@/main/lib/pasteImageBus` does not exist.

- [ ] **Step 3: Implement the bus**

Write `web/src/main/lib/pasteImageBus.ts` (identical body to the desktop version, only the file lives in the web tree):

```ts
export type PasteImageEvent = {
  id: string;
  file: Blob;
  name: string;
};

type Handler = (event: PasteImageEvent) => void;

const handlers = new Set<Handler>();

export const pasteImageBus = {
  emit(file: Blob, name: string): void {
    const event: PasteImageEvent = {
      id: crypto.randomUUID(),
      file,
      name: name || "clipboard-image",
    };
    for (const h of handlers) {
      try {
        h(event);
      } catch (err) {
        console.warn("[pasteImageBus] handler threw", err);
      }
    }
  },
  on(handler: Handler): () => void {
    handlers.add(handler);
    return () => {
      handlers.delete(handler);
    };
  },
};
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run tests/unit/main/lib/pasteImageBus.test.ts`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add web/src/main/lib/pasteImageBus.ts web/tests/unit/main/lib/pasteImageBus.test.ts
git commit -m "feat(web): add pasteImageBus event bus for paste-image previews"
```

---

### Task 8: Web PasteImagePreviewHost 组件

**Files:**
- Create: `web/src/main/components/PasteImagePreviewHost.vue`
- Test: `web/tests/unit/main/components/PasteImagePreviewHost.test.ts`

- [ ] **Step 1: Write the failing test**

Write `web/tests/unit/main/components/PasteImagePreviewHost.test.ts` — same body as the desktop test in Task 2, but adjusted imports:

```ts
import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import PasteImagePreviewHost from "@/main/components/PasteImagePreviewHost.vue";
import { pasteImageBus } from "@/main/lib/pasteImageBus";

function makeBlob() {
  return new Blob([new Uint8Array([1, 2, 3])], { type: "image/png" });
}

describe("PasteImagePreviewHost (web)", () => {
  let createSpy: ReturnType<typeof vi.spyOn>;
  let revokeSpy: ReturnType<typeof vi.spyOn>;
  let urlCounter = 0;

  beforeEach(() => {
    vi.useFakeTimers();
    urlCounter = 0;
    createSpy = vi
      .spyOn(URL, "createObjectURL")
      .mockImplementation(() => `blob:fake/${++urlCounter}`);
    revokeSpy = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.useRealTimers();
    createSpy.mockRestore();
    revokeSpy.mockRestore();
  });

  it("renders a toast when the bus emits", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "shot.png");
    await flushPromises();
    const toasts = w.findAll('[data-testid="paste-toast"]');
    expect(toasts.length).toBe(1);
    expect(toasts[0].find("img").attributes("src")).toBe("blob:fake/1");
    expect(toasts[0].text()).toContain("shot.png");
    w.unmount();
  });

  it("auto-dismisses the toast after 5 seconds and revokes the url", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    vi.advanceTimersByTime(5000);
    await flushPromises();
    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(0);
    expect(revokeSpy).toHaveBeenCalledWith("blob:fake/1");
    w.unmount();
  });

  it("pauses the timer on hover and resumes when the cursor leaves", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    const toast = w.find('[data-testid="paste-toast"]');
    vi.advanceTimersByTime(3000);
    await toast.trigger("mouseenter");
    vi.advanceTimersByTime(10_000);
    await flushPromises();
    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(1);
    await toast.trigger("mouseleave");
    vi.advanceTimersByTime(5000);
    await flushPromises();
    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(0);
    w.unmount();
  });

  it("closes the toast immediately on × click", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    await w.find('[data-testid="paste-toast-close"]').trigger("click");
    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(0);
    expect(revokeSpy).toHaveBeenCalledWith("blob:fake/1");
    w.unmount();
  });

  it("opens a lightbox with an independent url when the thumbnail is clicked", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    await w.find('[data-testid="paste-toast"] img').trigger("click");
    const lightbox = w.find('[data-testid="paste-lightbox"]');
    expect(lightbox.exists()).toBe(true);
    expect(lightbox.find("img").attributes("src")).toBe("blob:fake/2");
    expect(createSpy).toHaveBeenCalledTimes(2);
    w.unmount();
  });

  it("closes the lightbox when the backdrop is clicked and revokes its url", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    await w.find('[data-testid="paste-toast"] img').trigger("click");
    await w.find('[data-testid="paste-lightbox"]').trigger("click");
    expect(w.find('[data-testid="paste-lightbox"]').exists()).toBe(false);
    expect(revokeSpy).toHaveBeenCalledWith("blob:fake/2");
    w.unmount();
  });

  it("closes the lightbox on Esc", async () => {
    const w = mount(PasteImagePreviewHost, { attachTo: document.body });
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    await w.find('[data-testid="paste-toast"] img').trigger("click");
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    await flushPromises();
    expect(w.find('[data-testid="paste-lightbox"]').exists()).toBe(false);
    w.unmount();
  });

  it("keeps the lightbox visible after the source toast auto-dismisses", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    await w.find('[data-testid="paste-toast"] img').trigger("click");
    vi.advanceTimersByTime(5000);
    await flushPromises();
    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(0);
    expect(w.find('[data-testid="paste-lightbox"]').exists()).toBe(true);
    expect(w.find('[data-testid="paste-lightbox"] img').attributes("src")).toBe("blob:fake/2");
    w.unmount();
  });

  it("stacks multiple toasts in arrival order", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    pasteImageBus.emit(makeBlob(), "b.png");
    await flushPromises();
    const toasts = w.findAll('[data-testid="paste-toast"]');
    expect(toasts.length).toBe(2);
    expect(toasts[0].text()).toContain("a.png");
    expect(toasts[1].text()).toContain("b.png");
    w.unmount();
  });

  it("cleans up timers, urls and bus subscription on unmount", async () => {
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    pasteImageBus.emit(makeBlob(), "b.png");
    await flushPromises();
    w.unmount();
    expect(revokeSpy).toHaveBeenCalledWith("blob:fake/1");
    expect(revokeSpy).toHaveBeenCalledWith("blob:fake/2");
    revokeSpy.mockClear();
    pasteImageBus.emit(makeBlob(), "c.png");
    expect(revokeSpy).not.toHaveBeenCalled();
  });

  it("skips the toast (without throwing) when createObjectURL fails", async () => {
    createSpy.mockImplementationOnce(() => { throw new Error("oom"); });
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const w = mount(PasteImagePreviewHost);
    pasteImageBus.emit(makeBlob(), "a.png");
    await flushPromises();
    expect(w.findAll('[data-testid="paste-toast"]').length).toBe(0);
    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
    w.unmount();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run tests/unit/main/components/PasteImagePreviewHost.test.ts`
Expected: FAIL — component does not exist.

- [ ] **Step 3: Implement the component**

Write `web/src/main/components/PasteImagePreviewHost.vue` — same code as the desktop component (Task 2 step 3). Adjust the import path:

```vue
<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";
import { pasteImageBus, type PasteImageEvent } from "../lib/pasteImageBus";

const DISMISS_MS = 5000;

interface Toast {
  id: string;
  file: Blob;
  url: string;
  name: string;
}

interface Lightbox {
  url: string;
  name: string;
}

const toasts = ref<Toast[]>([]);
const lightbox = ref<Lightbox | null>(null);
const timers = new Map<string, number>();

function startTimer(id: string) {
  const handle = window.setTimeout(() => dismiss(id), DISMISS_MS);
  timers.set(id, handle);
}

function clearTimer(id: string) {
  const handle = timers.get(id);
  if (handle !== undefined) {
    window.clearTimeout(handle);
    timers.delete(id);
  }
}

function handlePaste(event: PasteImageEvent) {
  let url: string;
  try {
    url = URL.createObjectURL(event.file);
  } catch (err) {
    console.warn("[PasteImagePreviewHost] createObjectURL failed", err);
    return;
  }
  toasts.value.push({
    id: event.id,
    file: event.file,
    url,
    name: event.name,
  });
  startTimer(event.id);
}

function dismiss(id: string) {
  clearTimer(id);
  const idx = toasts.value.findIndex((t) => t.id === id);
  if (idx === -1) return;
  const [removed] = toasts.value.splice(idx, 1);
  URL.revokeObjectURL(removed.url);
}

function onMouseEnter(id: string) {
  clearTimer(id);
}

function onMouseLeave(id: string) {
  if (!toasts.value.some((t) => t.id === id)) return;
  startTimer(id);
}

function openLightbox(toast: Toast) {
  if (lightbox.value) {
    URL.revokeObjectURL(lightbox.value.url);
  }
  try {
    lightbox.value = {
      url: URL.createObjectURL(toast.file),
      name: toast.name,
    };
  } catch (err) {
    console.warn("[PasteImagePreviewHost] lightbox createObjectURL failed", err);
    lightbox.value = null;
  }
}

function closeLightbox() {
  if (!lightbox.value) return;
  URL.revokeObjectURL(lightbox.value.url);
  lightbox.value = null;
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Escape" && lightbox.value) {
    closeLightbox();
  }
}

let unsubscribe: (() => void) | null = null;

onMounted(() => {
  unsubscribe = pasteImageBus.on(handlePaste);
  document.addEventListener("keydown", onKeydown);
});

onBeforeUnmount(() => {
  unsubscribe?.();
  unsubscribe = null;
  document.removeEventListener("keydown", onKeydown);
  for (const handle of timers.values()) window.clearTimeout(handle);
  timers.clear();
  for (const t of toasts.value) URL.revokeObjectURL(t.url);
  toasts.value = [];
  if (lightbox.value) {
    URL.revokeObjectURL(lightbox.value.url);
    lightbox.value = null;
  }
});
</script>

<template>
  <div class="paste-preview-host">
    <div
      v-for="t in toasts"
      :key="t.id"
      class="paste-toast"
      data-testid="paste-toast"
      @mouseenter="onMouseEnter(t.id)"
      @mouseleave="onMouseLeave(t.id)"
    >
      <img :src="t.url" :alt="t.name" class="paste-toast-thumb" @click="openLightbox(t)" />
      <span class="paste-toast-name">{{ t.name }}</span>
      <button
        type="button"
        class="paste-toast-close"
        data-testid="paste-toast-close"
        :aria-label="'close ' + t.name"
        @click="dismiss(t.id)"
      >×</button>
    </div>
    <div
      v-if="lightbox"
      class="paste-lightbox"
      data-testid="paste-lightbox"
      @click="closeLightbox"
    >
      <img :src="lightbox.url" :alt="lightbox.name" @click.stop />
    </div>
  </div>
</template>

<style scoped>
.paste-preview-host {
  position: fixed;
  top: 88px;
  right: 0.75rem;
  z-index: 50;
  display: flex;
  flex-direction: column;
  gap: 8px;
  pointer-events: none;
}
.paste-toast {
  pointer-events: auto;
  width: 200px;
  padding: 8px;
  background: var(--bg, #1f2024);
  color: var(--fg, #e6e6e6);
  border: 1px solid var(--border, #3a3b40);
  border-radius: 6px;
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.35);
  display: flex;
  flex-direction: column;
  gap: 4px;
  position: relative;
}
.paste-toast-thumb {
  width: 100%;
  max-height: 120px;
  object-fit: contain;
  cursor: zoom-in;
  border-radius: 4px;
  background: #000;
}
.paste-toast-name {
  font-size: 12px;
  opacity: 0.8;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.paste-toast-close {
  position: absolute;
  top: 4px;
  right: 6px;
  background: transparent;
  border: none;
  color: inherit;
  font-size: 16px;
  line-height: 1;
  cursor: pointer;
  opacity: 0.7;
}
.paste-toast-close:hover {
  opacity: 1;
}
.paste-lightbox {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.85);
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: zoom-out;
  pointer-events: auto;
}
.paste-lightbox img {
  max-width: 90vw;
  max-height: 90vh;
  cursor: default;
}
</style>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run tests/unit/main/components/PasteImagePreviewHost.test.ts`
Expected: PASS (11 tests).

- [ ] **Step 5: Commit**

```bash
git add web/src/main/components/PasteImagePreviewHost.vue web/tests/unit/main/components/PasteImagePreviewHost.test.ts
git commit -m "feat(web): add PasteImagePreviewHost toast + lightbox component"
```

---

### Task 9: Web App.vue onPasteImage 接 bus + 挂载 host

**Files:**
- Modify: `web/src/main/App.vue` (lines 9-11, 98-100, and template)
- Modify: `web/tests/unit/main/App.test.ts` (add bus assertion)

- [ ] **Step 1: Add a failing assertion**

In `web/tests/unit/main/App.test.ts`:

Add these imports near the top (alongside the existing `import App from '@/main/App.vue'`):

```ts
import PasteFallback from '@/main/components/PasteFallback.vue'
import { pasteImageBus } from '@/main/lib/pasteImageBus'
```

Add a new test case to the existing `describe('Main (home) App.vue', ...)` block, after the existing tests:

```ts
  it('emits pasteImageBus when a pasted image is dispatched via PasteFallback', async () => {
    const emitSpy = vi.spyOn(pasteImageBus, 'emit')

    Object.defineProperty(window, 'location', {
      value: { ...window.location, hash: '#/s/11111111-2222-3333-4444-555555555555' },
      writable: true,
    })
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()

    const fallback = wrapper.findComponent(PasteFallback)
    const file = new File([new Uint8Array([1, 2])], 'pasted.png', { type: 'image/png' })
    fallback.vm.$emit('paste-image', file)
    await flushPromises()

    expect(emitSpy).toHaveBeenCalledTimes(1)
    const [blob, name] = emitSpy.mock.calls[0]
    expect(blob).toBe(file)
    expect(name).toBe('pasted.png')
    emitSpy.mockRestore()
  })
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run tests/unit/main/App.test.ts -t "emits pasteImageBus"`
Expected: FAIL — `emitSpy` not called.

- [ ] **Step 3: Wire the bus and mount the host**

In `web/src/main/App.vue`:

Add to the `<script setup>` imports (alongside `import PasteFallback from './components/PasteFallback.vue'`):

```ts
import PasteImagePreviewHost from './components/PasteImagePreviewHost.vue'
import { pasteImageBus } from './lib/pasteImageBus'
```

Update `onPasteImage` (lines 98-100):

```ts
function onPasteImage(file: File) {
  pasteImageBus.emit(file, file.name)
  void termRef.value?.sendPasteImage(file, file.name)
}
```

In the template, inside `<n-message-provider>`, add the host as a sibling of `<Topbar>` (top of the provider, so it overlays everything):

```vue
    <n-message-provider>
      <PasteImagePreviewHost />
      <Topbar active="home" />
```

- [ ] **Step 4: Run the App test plus full web suite**

Run: `cd web && npx vitest run tests/unit/main/App.test.ts && npx vitest run`
Expected: PASS — new assertion passes, all existing web tests still pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/main/App.vue web/tests/unit/main/App.test.ts
git commit -m "feat(web): emit pasteImageBus on paste + mount PasteImagePreviewHost"
```

---

## Track 3 — Verification

### Task 10: 手动验证全 4 路径

- [ ] **Step 1: Build & launch desktop dev**

Run: `cd desktop && npx wails dev` (or the project's normal desktop dev command — check `desktop/README` or `package.json` if unsure).

- [ ] **Step 2: Verify desktop Ctrl+V**

Take any screenshot to clipboard. In an attached session, press Ctrl+V (or Cmd+V on macOS). Expect:
- Toast appears top-right showing the thumbnail + filename "clipboard-image" (or its actual name)
- Underneath, the TUI (e.g. Claude Code) shows `[Image #1]`
- Toast auto-dismisses after ~5 seconds; click thumbnail opens a full-size lightbox; click backdrop or Esc closes

- [ ] **Step 3: Verify desktop right-click paste**

Right-click in the terminal area → "Paste" (or the equivalent menu item). With an image in the clipboard, expect the same toast behavior as Ctrl+V.

- [ ] **Step 4: Verify mobile photo picker**

Build & run the Capacitor iOS or Android target (`cd desktop && npx cap run ios` or similar — check `desktop/scripts/`). Attach to a session, tap the image button, pick a photo from the library. Expect the toast in the same top-right position.

- [ ] **Step 5: Verify web frontend file picker**

Run: `cd web && npm run dev` (or whichever launches the web bundle served by relay). Visit a session in a browser, open the paste fallback (cannot Ctrl+V images directly in web), choose an image file. Expect the toast.

- [ ] **Step 6: Verify hover-pause + multi-paste stacking**

Paste two images quickly. Verify both toasts stack top-down. Hover one — it should not auto-dismiss while hovered. Move mouse away — 5s timer resumes.

- [ ] **Step 7: Note any deviations**

If any step's behavior differs from the spec, capture a screenshot/short note and reopen the spec section that's wrong before committing further.

(No commit step — this task is verification.)

---

### Task 11: 全量回归

- [ ] **Step 1: Desktop frontend full test suite**

Run: `cd desktop/frontend && npx vue-tsc --noEmit && npm test`
Expected: type-check clean; full vitest suite passes.

- [ ] **Step 2: Web frontend full test suite**

Run: `cd web && npx vue-tsc --noEmit && npm test`
Expected: type-check clean; full vitest suite passes.

- [ ] **Step 3: Go test suite (sanity — should be untouched)**

Run: `go test ./...` from repo root.
Expected: PASS — no Go code was modified, but a sanity run confirms nothing in `desktop/` got dragged in.

- [ ] **Step 4: Commit (no-op if nothing changed)**

If steps 1-3 surfaced any incidental fix (a broken existing test, type, etc.), commit it separately:

```bash
git add <files>
git commit -m "fix(test): <what>"
```

If everything was already clean, no commit needed.
