# TabBar 拖拽重排 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `TabBar` 支持鼠标/触摸按住 tab 拖动改变顺序，实时重排 + 边缘自动滚动，结果自动进入 recovery snapshot。

**Architecture:** 交互全部在 `TabBar.vue` 内用 Pointer Events 处理（不用 HTML5 Drag&Drop）。TabBar 只维护本地 drag state，通过新增 emit `reorder(fromId, targetId, position)` 让 `App.vue` 按当前 tabs 顺序做 splice；父级 tabs 数组变化自然触发既有 `useRecoverySnapshot` 落盘。

**Tech Stack:** Vue 3 `<script setup>` + TypeScript, Pointer Events API, vitest + `@vue/test-utils`。

## Global Constraints

- Language of design/spec, code identifiers, and commit messages: English.
- 用户可见 UI 文案：走 `useI18n().t()`；本次功能没有新增文案。
- 不新增依赖包，纯前端逻辑。
- 支持环境：Wails 内嵌 Chromium 与 Capacitor iOS WebView（因此使用 Pointer Events + `touch-action`）。
- Spec 位置：`docs/superpowers/specs/2026-07-24-tab-drag-reorder-design.md`。

## File Structure

- Modify: `desktop/frontend/src/components/TabBar.vue` — 新增 drag state、pointer 事件、命中/去抖/边缘滚动、`.dragging` CSS；新增 emit `reorder`。
- Modify: `desktop/frontend/src/App.vue` — 新增 `onTabReorder(fromId, targetId, position)` handler；`<TabBar>` 上 wire `@reorder`。
- Modify: `desktop/frontend/src/components/TabBar.test.ts` — 新增拖拽相关用例。
- Create: `desktop/frontend/src/App.tabReorder.test.ts` — 覆盖 App 侧 reorder 数组操作纯逻辑。

---

### Task 1: TabBar 声明 `reorder` emit 并接入 pointer 事件框架（TDD）

**Files:**
- Modify: `desktop/frontend/src/components/TabBar.vue`
- Test: `desktop/frontend/src/components/TabBar.test.ts`

**Interfaces:**
- Consumes: 无
- Produces:
  - `emit('reorder', fromId: string, targetId: string, position: 'before' | 'after')`
  - TabBar 组件挂载后能识别 `pointerdown/move/up` 序列。

- [ ] **Step 1: 写失败测试 — 拖动第 1 个 tab 越过第 3 个 tab 中线右侧 → emit `reorder('tab-1','tab-3','after')`**

在 `TabBar.test.ts` 末尾新增：

```ts
describe("TabBar drag reorder", () => {
  function mountThreeTabs() {
    return mount(TabBar, {
      props: {
        tabs: [
          { id: "tab-1", layout: "single", activeSession: null, activeRemote: false, paneCount: 1 },
          { id: "tab-2", layout: "single", activeSession: null, activeRemote: false, paneCount: 1 },
          { id: "tab-3", layout: "single", activeSession: null, activeRemote: false, paneCount: 1 },
        ],
        currentId: "tab-1",
        starting: false,
      },
      attachTo: document.body,
    });
  }

  function stubRect(el: Element, rect: Partial<DOMRect>) {
    (el as HTMLElement).getBoundingClientRect = () => ({
      x: 0, y: 0, width: 0, height: 0, top: 0, left: 0, right: 0, bottom: 0,
      toJSON: () => ({}), ...rect,
    } as DOMRect);
  }

  it("emits reorder when a tab is dragged past another tab's midline", async () => {
    const wrapper = mountThreeTabs();
    const tabsEls = wrapper.findAll(".tab");
    stubRect(wrapper.get(".tabs").element, { left: 0, right: 900, width: 900 });
    stubRect(tabsEls[0].element, { left: 0, right: 100, width: 100 });
    stubRect(tabsEls[1].element, { left: 100, right: 200, width: 100 });
    stubRect(tabsEls[2].element, { left: 200, right: 300, width: 100 });

    await tabsEls[0].trigger("pointerdown", { clientX: 50, pointerId: 1, button: 0 });
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 55, pointerId: 1 } as any));
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 260, pointerId: 1 } as any));
    window.dispatchEvent(new PointerEvent("pointerup", { clientX: 260, pointerId: 1 } as any));

    const events = wrapper.emitted("reorder") as unknown[][] | undefined;
    expect(events).toBeTruthy();
    expect(events![events!.length - 1]).toEqual(["tab-1", "tab-3", "after"]);
    wrapper.unmount();
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

```
cd desktop/frontend && npx vitest run src/components/TabBar.test.ts -t "emits reorder"
```

Expected: FAIL — 因为 TabBar 尚未挂 pointer 事件，无 `reorder` 事件被 emit。

- [ ] **Step 3: 修改 `TabBar.vue`，新增 emit + drag state + 事件监听**

在 `<script setup>` 内：

```ts
import { ref, onBeforeUnmount, onMounted } from "vue";
// ...原有 import 保留

const emit = defineEmits<{
  (e: "activate", id: string): void;
  (e: "close", id: string): void;
  (e: "new"): void;
  (e: "reorder", fromId: string, targetId: string, position: "before" | "after"): void;
}>();

const DRAG_THRESHOLD = 4;
const drag = ref<null | {
  fromId: string;
  startX: number;
  active: boolean;
  pointerId: number;
  overId: string | null;
  position: "before" | "after" | null;
  hadDrag: boolean;
}>(null);
const tabsEl = ref<HTMLElement | null>(null);
let lastClientX = 0;

function isCloseTarget(e: PointerEvent): boolean {
  const t = e.target as HTMLElement | null;
  return !!t && !!t.closest?.(".close");
}

function onTabPointerDown(id: string, e: PointerEvent) {
  if (e.button !== 0) return;
  if (isCloseTarget(e)) return;
  drag.value = {
    fromId: id,
    startX: e.clientX,
    active: false,
    pointerId: e.pointerId,
    overId: null,
    position: null,
    hadDrag: false,
  };
}

function hitTest(clientX: number): { id: string; position: "before" | "after" } | null {
  if (!tabsEl.value) return null;
  const nodes = Array.from(tabsEl.value.querySelectorAll<HTMLElement>(".tab"));
  if (nodes.length === 0) return null;
  for (const el of nodes) {
    const r = el.getBoundingClientRect();
    if (clientX >= r.left && clientX <= r.right) {
      const mid = (r.left + r.right) / 2;
      return { id: el.dataset.tabId!, position: clientX < mid ? "before" : "after" };
    }
  }
  const firstR = nodes[0].getBoundingClientRect();
  if (clientX < firstR.left) return { id: nodes[0].dataset.tabId!, position: "before" };
  const lastR = nodes[nodes.length - 1].getBoundingClientRect();
  return { id: nodes[nodes.length - 1].dataset.tabId!, position: "after" };
}

function onWindowPointerMove(e: PointerEvent) {
  const d = drag.value;
  if (!d || e.pointerId !== d.pointerId) return;
  lastClientX = e.clientX;
  if (!d.active) {
    if (Math.abs(e.clientX - d.startX) < DRAG_THRESHOLD) return;
    d.active = true;
    d.hadDrag = true;
  }
  const hit = hitTest(e.clientX);
  if (!hit) return;
  if (hit.id === d.fromId) return;
  if (hit.id === d.overId && hit.position === d.position) return;
  d.overId = hit.id;
  d.position = hit.position;
  emit("reorder", d.fromId, hit.id, hit.position);
}

function suppressNextClick() {
  const stop = (ev: MouseEvent) => {
    ev.stopPropagation();
    ev.preventDefault();
  };
  window.addEventListener("click", stop, { capture: true, once: true });
}

function endDrag(cancelled: boolean) {
  const d = drag.value;
  if (!d) return;
  if (d.hadDrag && !cancelled) suppressNextClick();
  drag.value = null;
}

function onWindowPointerUp(e: PointerEvent) {
  if (!drag.value || e.pointerId !== drag.value.pointerId) return;
  endDrag(false);
}

function onWindowPointerCancel(e: PointerEvent) {
  if (!drag.value || e.pointerId !== drag.value.pointerId) return;
  endDrag(true);
}

onMounted(() => {
  window.addEventListener("pointermove", onWindowPointerMove);
  window.addEventListener("pointerup", onWindowPointerUp);
  window.addEventListener("pointercancel", onWindowPointerCancel);
});
onBeforeUnmount(() => {
  window.removeEventListener("pointermove", onWindowPointerMove);
  window.removeEventListener("pointerup", onWindowPointerUp);
  window.removeEventListener("pointercancel", onWindowPointerCancel);
});
```

在 template 里，把 `.tabs` 容器加 `ref`，`.tab` 加 pointerdown，`.dragging` class 与源比较：

```vue
<div class="tabs" ref="tabsEl">
  <div
    v-for="(t, idx) in tabs"
    :key="t.id"
    :data-tab-id="t.id"
    class="tab"
    :class="{
      active: t.id === currentId,
      remote: t.activeRemote,
      disconnected: t.disconnected,
      dragging: drag?.active && drag?.fromId === t.id,
    }"
    ...
    @click="emit('activate', t.id)"
    @pointerdown="onTabPointerDown(t.id, $event)"
  >
    ...
    <button class="close" @pointerdown.stop @click="onClose($event, t.id)">×</button>
  </div>
</div>
```

样式补：

```css
.tab.dragging { opacity: 0.5; }
.tab { touch-action: pan-y; }
```

- [ ] **Step 4: 运行测试确认通过**

```
cd desktop/frontend && npx vitest run src/components/TabBar.test.ts -t "emits reorder"
```

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/TabBar.vue desktop/frontend/src/components/TabBar.test.ts
git commit -m "feat(tabbar): pointer-based drag reorder scaffold + emit"
```

---

### Task 2: 去抖 + 阈值门槛 + 拖到自身不 emit（TDD）

**Files:**
- Modify: `desktop/frontend/src/components/TabBar.test.ts` (new cases)
- Modify: `desktop/frontend/src/components/TabBar.vue`（若需微调）

**Interfaces:**
- Consumes: Task 1 的 `reorder` emit 与 drag state。
- Produces: 保证同一 (targetId, position) 在拖动过程中只 emit 一次；未过阈值不 emit；拖到自己不 emit。

- [ ] **Step 1: 写失败测试 — 小于 4px 的 pointermove 不 emit reorder，且 click 仍正常触发 activate**

```ts
it("does not emit reorder for sub-threshold movement and still activates on click", async () => {
  const wrapper = mountThreeTabs();
  const tabsEls = wrapper.findAll(".tab");
  stubRect(wrapper.get(".tabs").element, { left: 0, right: 900, width: 900 });
  stubRect(tabsEls[0].element, { left: 0, right: 100, width: 100 });
  stubRect(tabsEls[1].element, { left: 100, right: 200, width: 100 });
  stubRect(tabsEls[2].element, { left: 200, right: 300, width: 100 });

  await tabsEls[1].trigger("pointerdown", { clientX: 150, pointerId: 2, button: 0 });
  window.dispatchEvent(new PointerEvent("pointermove", { clientX: 152, pointerId: 2 } as any));
  window.dispatchEvent(new PointerEvent("pointerup", { clientX: 152, pointerId: 2 } as any));
  await tabsEls[1].trigger("click");

  expect(wrapper.emitted("reorder")).toBeUndefined();
  expect(wrapper.emitted("activate")).toBeTruthy();
  wrapper.unmount();
});
```

- [ ] **Step 2: 写失败测试 — 同一目标位置反复 pointermove 只 emit 一次**

```ts
it("debounces reorder while cursor stays over the same slot", async () => {
  const wrapper = mountThreeTabs();
  const tabsEls = wrapper.findAll(".tab");
  stubRect(wrapper.get(".tabs").element, { left: 0, right: 900, width: 900 });
  stubRect(tabsEls[0].element, { left: 0, right: 100, width: 100 });
  stubRect(tabsEls[1].element, { left: 100, right: 200, width: 100 });
  stubRect(tabsEls[2].element, { left: 200, right: 300, width: 100 });

  await tabsEls[0].trigger("pointerdown", { clientX: 50, pointerId: 3, button: 0 });
  window.dispatchEvent(new PointerEvent("pointermove", { clientX: 55, pointerId: 3 } as any));
  window.dispatchEvent(new PointerEvent("pointermove", { clientX: 155, pointerId: 3 } as any));
  window.dispatchEvent(new PointerEvent("pointermove", { clientX: 160, pointerId: 3 } as any));
  window.dispatchEvent(new PointerEvent("pointermove", { clientX: 165, pointerId: 3 } as any));
  window.dispatchEvent(new PointerEvent("pointerup", { clientX: 165, pointerId: 3 } as any));

  const events = wrapper.emitted("reorder") as unknown[][] | undefined;
  expect(events).toBeTruthy();
  expect(events!.length).toBe(1);
  expect(events![0]).toEqual(["tab-1", "tab-2", "before"]);
  wrapper.unmount();
});
```

- [ ] **Step 3: 写失败测试 — 光标未离开源 tab 时不 emit（拖到自己）**

```ts
it("does not emit reorder while the cursor stays over the source tab", async () => {
  const wrapper = mountThreeTabs();
  const tabsEls = wrapper.findAll(".tab");
  stubRect(wrapper.get(".tabs").element, { left: 0, right: 900, width: 900 });
  stubRect(tabsEls[0].element, { left: 0, right: 100, width: 100 });
  stubRect(tabsEls[1].element, { left: 100, right: 200, width: 100 });
  stubRect(tabsEls[2].element, { left: 200, right: 300, width: 100 });

  await tabsEls[0].trigger("pointerdown", { clientX: 20, pointerId: 4, button: 0 });
  window.dispatchEvent(new PointerEvent("pointermove", { clientX: 40, pointerId: 4 } as any));
  window.dispatchEvent(new PointerEvent("pointermove", { clientX: 70, pointerId: 4 } as any));
  window.dispatchEvent(new PointerEvent("pointerup", { clientX: 70, pointerId: 4 } as any));

  expect(wrapper.emitted("reorder")).toBeUndefined();
  wrapper.unmount();
});
```

- [ ] **Step 4: 运行三个测试，全部应通过（Task 1 已实现的逻辑覆盖了这些用例）**

```
cd desktop/frontend && npx vitest run src/components/TabBar.test.ts -t "drag reorder"
```

Expected: 全部 PASS。若某项失败，调整 `onWindowPointerMove` 里的 diff 条件与 `hit.id === d.fromId` 分支。

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/TabBar.test.ts desktop/frontend/src/components/TabBar.vue
git commit -m "test(tabbar): drag reorder threshold + debounce + self-hover"
```

---

### Task 3: pointercancel、close 阻断、右键忽略（TDD）

**Files:**
- Modify: `desktop/frontend/src/components/TabBar.test.ts`
- Modify: `desktop/frontend/src/components/TabBar.vue`（若测试暴露缺陷）

**Interfaces:**
- Consumes: Task 1 的事件监听。
- Produces: 保证：pointercancel 后不留残留 emit；close 按钮上的 pointerdown 不进入拖动态；右键（button != 0）不触发拖动。

- [ ] **Step 1: 写失败测试 — pointercancel 中止拖动，不追加新 emit**

```ts
it("stops emitting after pointercancel", async () => {
  const wrapper = mountThreeTabs();
  const tabsEls = wrapper.findAll(".tab");
  stubRect(wrapper.get(".tabs").element, { left: 0, right: 900, width: 900 });
  stubRect(tabsEls[0].element, { left: 0, right: 100, width: 100 });
  stubRect(tabsEls[1].element, { left: 100, right: 200, width: 100 });
  stubRect(tabsEls[2].element, { left: 200, right: 300, width: 100 });

  await tabsEls[0].trigger("pointerdown", { clientX: 50, pointerId: 5, button: 0 });
  window.dispatchEvent(new PointerEvent("pointermove", { clientX: 60, pointerId: 5 } as any));
  window.dispatchEvent(new PointerEvent("pointermove", { clientX: 160, pointerId: 5 } as any));
  const beforeCancel = (wrapper.emitted("reorder") ?? []).length;
  window.dispatchEvent(new PointerEvent("pointercancel", { clientX: 160, pointerId: 5 } as any));
  window.dispatchEvent(new PointerEvent("pointermove", { clientX: 260, pointerId: 5 } as any));
  const afterCancel = (wrapper.emitted("reorder") ?? []).length;
  expect(afterCancel).toBe(beforeCancel);
  wrapper.unmount();
});
```

- [ ] **Step 2: 写失败测试 — 在 `.close` 上 pointerdown 不进入拖动态，close emit 正常**

```ts
it("does not initiate drag when pointerdown lands on the close button", async () => {
  const wrapper = mountThreeTabs();
  const tabsEls = wrapper.findAll(".tab");
  stubRect(wrapper.get(".tabs").element, { left: 0, right: 900, width: 900 });
  stubRect(tabsEls[0].element, { left: 0, right: 100, width: 100 });
  stubRect(tabsEls[1].element, { left: 100, right: 200, width: 100 });
  stubRect(tabsEls[2].element, { left: 200, right: 300, width: 100 });

  const closeBtn = tabsEls[0].get(".close");
  await closeBtn.trigger("pointerdown", { clientX: 90, pointerId: 6, button: 0 });
  window.dispatchEvent(new PointerEvent("pointermove", { clientX: 260, pointerId: 6 } as any));
  window.dispatchEvent(new PointerEvent("pointerup", { clientX: 260, pointerId: 6 } as any));
  await closeBtn.trigger("click");

  expect(wrapper.emitted("reorder")).toBeUndefined();
  expect(wrapper.emitted("close")).toBeTruthy();
  wrapper.unmount();
});
```

- [ ] **Step 3: 写失败测试 — button != 0（右键/中键）忽略**

```ts
it("ignores non-primary buttons for drag", async () => {
  const wrapper = mountThreeTabs();
  const tabsEls = wrapper.findAll(".tab");
  stubRect(wrapper.get(".tabs").element, { left: 0, right: 900, width: 900 });
  stubRect(tabsEls[0].element, { left: 0, right: 100, width: 100 });
  stubRect(tabsEls[1].element, { left: 100, right: 200, width: 100 });
  stubRect(tabsEls[2].element, { left: 200, right: 300, width: 100 });

  await tabsEls[0].trigger("pointerdown", { clientX: 50, pointerId: 7, button: 2 });
  window.dispatchEvent(new PointerEvent("pointermove", { clientX: 260, pointerId: 7 } as any));
  window.dispatchEvent(new PointerEvent("pointerup", { clientX: 260, pointerId: 7 } as any));

  expect(wrapper.emitted("reorder")).toBeUndefined();
  wrapper.unmount();
});
```

- [ ] **Step 4: 运行三个测试，确认通过（Task 1 已实现的逻辑覆盖）**

```
cd desktop/frontend && npx vitest run src/components/TabBar.test.ts -t "drag reorder"
```

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/TabBar.test.ts
git commit -m "test(tabbar): pointercancel + close-button + non-primary-button guards"
```

---

### Task 4: 边缘自动滚动（TDD）

**Files:**
- Modify: `desktop/frontend/src/components/TabBar.vue` — 新增 `EDGE_SCROLL_ZONE`, `EDGE_SCROLL_SPEED`, `startEdgeScroll()`.
- Modify: `desktop/frontend/src/components/TabBar.test.ts`

**Interfaces:**
- Consumes: Task 1 的 drag state + `tabsEl`。
- Produces: 光标在 `.tabs` 容器左/右 40px 内且处于拖动态时，`scrollLeft` 按 `EDGE_SCROLL_SPEED` 步进。

- [ ] **Step 1: 写失败测试 — 拖动至 tabs 容器右边缘 20px 处 → scrollLeft 增加**

```ts
it("auto-scrolls the tabs container when dragging near the right edge", async () => {
  const wrapper = mountThreeTabs();
  const tabsEls = wrapper.findAll(".tab");
  const tabsContainer = wrapper.get(".tabs").element as HTMLElement;
  stubRect(tabsContainer, { left: 0, right: 300, width: 300 });
  stubRect(tabsEls[0].element, { left: 0, right: 100, width: 100 });
  stubRect(tabsEls[1].element, { left: 100, right: 200, width: 100 });
  stubRect(tabsEls[2].element, { left: 200, right: 300, width: 100 });
  tabsContainer.scrollLeft = 0;

  const rafSpy = vi.spyOn(window, "requestAnimationFrame").mockImplementation((cb) => {
    return setTimeout(() => cb(0), 0) as unknown as number;
  });

  try {
    await tabsEls[0].trigger("pointerdown", { clientX: 50, pointerId: 8, button: 0 });
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 60, pointerId: 8 } as any));
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 290, pointerId: 8 } as any));
    await new Promise((r) => setTimeout(r, 30));
    expect(tabsContainer.scrollLeft).toBeGreaterThan(0);
    window.dispatchEvent(new PointerEvent("pointerup", { clientX: 290, pointerId: 8 } as any));
  } finally {
    rafSpy.mockRestore();
    wrapper.unmount();
  }
});
```

在测试文件顶部 import `vi`：

```ts
import { afterEach, describe, expect, it, test, vi } from "vitest";
```

- [ ] **Step 2: 运行测试，确认失败**

```
cd desktop/frontend && npx vitest run src/components/TabBar.test.ts -t "auto-scrolls"
```

Expected: FAIL — scrollLeft 未被修改。

- [ ] **Step 3: 修改 `TabBar.vue`，加入 edge auto-scroll**

在 `<script setup>` 内 `DRAG_THRESHOLD` 附近新增：

```ts
const EDGE_SCROLL_ZONE = 40;
const EDGE_SCROLL_SPEED = 8;
let rafId: number | null = null;

function tickEdgeScroll() {
  const d = drag.value;
  if (!d || !d.active || !tabsEl.value) { rafId = null; return; }
  const rect = tabsEl.value.getBoundingClientRect();
  if (lastClientX - rect.left < EDGE_SCROLL_ZONE) {
    tabsEl.value.scrollLeft = Math.max(0, tabsEl.value.scrollLeft - EDGE_SCROLL_SPEED);
  } else if (rect.right - lastClientX < EDGE_SCROLL_ZONE) {
    tabsEl.value.scrollLeft = tabsEl.value.scrollLeft + EDGE_SCROLL_SPEED;
  } else {
    rafId = null;
    return;
  }
  rafId = requestAnimationFrame(tickEdgeScroll);
}
```

在 `onWindowPointerMove` 里更新 `lastClientX` 之后、命中判断之前插入：

```ts
if (d.active && tabsEl.value) {
  const rect = tabsEl.value.getBoundingClientRect();
  const inLeft = e.clientX - rect.left < EDGE_SCROLL_ZONE;
  const inRight = rect.right - e.clientX < EDGE_SCROLL_ZONE;
  if ((inLeft || inRight) && rafId === null) {
    rafId = requestAnimationFrame(tickEdgeScroll);
  }
}
```

`endDrag` 中追加：

```ts
if (rafId !== null) {
  cancelAnimationFrame(rafId);
  rafId = null;
}
```

- [ ] **Step 4: 运行测试确认通过**

```
cd desktop/frontend && npx vitest run src/components/TabBar.test.ts -t "auto-scrolls"
```

Expected: PASS。

- [ ] **Step 5: 运行全部 TabBar 测试确认无回归**

```
cd desktop/frontend && npx vitest run src/components/TabBar.test.ts
```

Expected: 全部 PASS。

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/components/TabBar.vue desktop/frontend/src/components/TabBar.test.ts
git commit -m "feat(tabbar): edge auto-scroll while dragging"
```

---

### Task 5: `App.vue` 接 `@reorder` 并做数组 splice（TDD）

**Files:**
- Modify: `desktop/frontend/src/App.vue`
- Create: `desktop/frontend/src/App.tabReorder.test.ts`

**Interfaces:**
- Consumes: TabBar 的 `reorder(fromId, targetId, position)` emit。
- Produces: `onTabReorder(fromId, targetId, position)` 纯函数——根据 tabs 数组做 splice。

因为 App.vue 内部 `tabs` 是文件内闭包，我们把 splice 逻辑抽成纯函数 `applyTabReorder(list, fromId, targetId, position)` 便于单测。

- [ ] **Step 1: 写失败测试 — 纯函数 splice 行为**

Create `desktop/frontend/src/App.tabReorder.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { applyTabReorder } from "./lib/tabReorder";

interface T { id: string }

describe("applyTabReorder", () => {
  const base = (): T[] => [{ id: "a" }, { id: "b" }, { id: "c" }, { id: "d" }];

  it("moves a tab after a later target", () => {
    expect(applyTabReorder(base(), "a", "c", "after").map((t) => t.id)).toEqual(["b", "c", "a", "d"]);
  });

  it("moves a tab before a later target", () => {
    expect(applyTabReorder(base(), "a", "c", "before").map((t) => t.id)).toEqual(["b", "a", "c", "d"]);
  });

  it("moves a tab before an earlier target", () => {
    expect(applyTabReorder(base(), "d", "b", "before").map((t) => t.id)).toEqual(["a", "d", "b", "c"]);
  });

  it("moves a tab after an earlier target", () => {
    expect(applyTabReorder(base(), "d", "b", "after").map((t) => t.id)).toEqual(["a", "b", "d", "c"]);
  });

  it("is a no-op when fromId equals targetId", () => {
    expect(applyTabReorder(base(), "b", "b", "after").map((t) => t.id)).toEqual(["a", "b", "c", "d"]);
  });

  it("is a no-op when fromId is missing", () => {
    expect(applyTabReorder(base(), "zz", "b", "after").map((t) => t.id)).toEqual(["a", "b", "c", "d"]);
  });

  it("appends when targetId is missing (target disappeared mid-drag)", () => {
    expect(applyTabReorder(base(), "a", "zz", "after").map((t) => t.id)).toEqual(["b", "c", "d", "a"]);
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

```
cd desktop/frontend && npx vitest run src/App.tabReorder.test.ts
```

Expected: FAIL — `./lib/tabReorder` 不存在。

- [ ] **Step 3: 实现 `desktop/frontend/src/lib/tabReorder.ts`**

Create:

```ts
export function applyTabReorder<T extends { id: string }>(
  list: T[],
  fromId: string,
  targetId: string,
  position: "before" | "after",
): T[] {
  if (fromId === targetId) return list;
  const arr = list.slice();
  const from = arr.findIndex((t) => t.id === fromId);
  if (from < 0) return list;
  const [moved] = arr.splice(from, 1);
  const to = arr.findIndex((t) => t.id === targetId);
  if (to < 0) {
    arr.push(moved);
    return arr;
  }
  const insertAt = position === "after" ? to + 1 : to;
  arr.splice(insertAt, 0, moved);
  return arr;
}
```

- [ ] **Step 4: 运行测试确认通过**

```
cd desktop/frontend && npx vitest run src/App.tabReorder.test.ts
```

Expected: PASS。

- [ ] **Step 5: 在 App.vue 内 wire handler**

修改 `desktop/frontend/src/App.vue`：

顶部 import 追加：

```ts
import { applyTabReorder } from "./lib/tabReorder";
```

在 `closeTab` / `gotoTab` 附近新增（`tabs` ref 已在文件内定义）：

```ts
function onTabReorder(fromId: string, targetId: string, position: "before" | "after") {
  tabs.value = applyTabReorder(tabs.value, fromId, targetId, position);
}
```

模板里给 `<TabBar>` 加事件：

```vue
<TabBar
  v-if="!startupFatal"
  :tabs="tabSummaries"
  :current-id="currentTabId"
  :starting="starting"
  @activate="gotoTab"
  @close="closeTab"
  @new="startNewTab"
  @reorder="onTabReorder"
/>
```

- [ ] **Step 6: 类型检查**

```
cd desktop/frontend && npx vue-tsc --noEmit
```

Expected: 无报错。

- [ ] **Step 7: 运行 App 层单测确认无回归**

```
cd desktop/frontend && npx vitest run src/App.merge.test.ts src/App.theme.test.ts src/App.test.ts src/App.tabReorder.test.ts
```

Expected: 全部 PASS。

- [ ] **Step 8: Commit**

```bash
git add desktop/frontend/src/App.vue desktop/frontend/src/lib/tabReorder.ts desktop/frontend/src/App.tabReorder.test.ts
git commit -m "feat(app): wire TabBar reorder emit into tabs state"
```

---

### Task 6: 全量测试 + 手动 UI 验证

**Files:**
- 无修改。

**Interfaces:**
- Consumes: 前 5 个 task。
- Produces: 全部单测绿色 + 手动确认桌面端拖动可用。

- [ ] **Step 1: 运行 desktop/frontend 全量测试**

```
cd desktop/frontend && npx vitest run
```

Expected: 全部 PASS。

- [ ] **Step 2: 运行 Go 侧构建检查（保守）**

```
cd desktop && go build ./...
```

Expected: 无报错（本次未改 Go，作为烟雾测试）。

- [ ] **Step 3: 手动 UI 验证 — 使用 `run` skill 启动应用**

按项目 `run` skill 启动桌面 app（或 `cd desktop && wails dev`），验证：
- 至少开 3 个 tab，按住第 1 个拖到第 3 个中线右侧后释放 → 顺序变成 [2,3,1]。
- 只按下不移动松开 → 仍激活该 tab（click 未被误抑制）。
- 按 × 关闭不误触发拖动。
- Tab 多到出现横向滚动条时，拖到右边缘 → tabbar 自动向右滚动。
- 重启 app（`Cmd+Q` 后重开）→ 新顺序保留（recovery snapshot 已落盘）。

若某项不符，回到相关 task 修正后重跑本 task。

- [ ] **Step 4: Commit（若手工调整了任何文件）**

若无改动跳过。若有：

```bash
git commit -am "fix(tabbar): manual QA adjustments"
```

---

## Self-Review Checklist Results

**Spec coverage**
- §1 Goals 全部覆盖：
  - 单窗口拖动改序 → Task 1 + Task 5。
  - 实时重排 → Task 1（每次跨越目标时 emit + 父级 splice）。
  - 边缘自动滚动 → Task 4。
  - Cmd+1..9 按新顺序生效 → 无代码修改（quick shortcut 已按 `tabs.value` 索引），Task 6 手工验证。
  - Recovery snapshot 落盘 → 无代码修改（`tabs.value` 变化触发既有 watch），Task 6 手工验证。
  - Wails + Capacitor 兼容（Pointer Events + `touch-action`）→ Task 1 CSS。
- §5 边界表全部覆盖：单 tab / 拖自己 / pointercancel / .close 屏蔽 / 右键忽略 → Task 2 & 3；tabs 数组内 fromId 或 targetId 消失 → Task 5 单测。
- §6 测试用例：全部落入 Task 1–5。

**Placeholder scan**：无 TODO / TBD；每一步都有可运行代码或命令。

**Type consistency**：`applyTabReorder` 签名统一；`reorder` emit 签名 `(fromId, targetId, position)` 在 Task 1、Task 5、App.vue wire 中一致。
