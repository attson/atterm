# TabBar 拖拽重排 — design

Date: 2026-07-24
Status: Drafted — awaiting user review before plan.

## 0. Summary

顶部 `TabBar` 目前只支持点击激活 / × 关闭 / + 新建，tab 顺序由创建先后决定，无法调整。
本设计让用户可以在 TabBar 内按住任一 tab 拖动，实时改变其在 tabs 数组中的位置——
与 Chrome / VS Code 的 tab bar 行为一致：拖动过程中其他 tab 让位、目标位置就是最终位置。
当 tab 数量多、tabbar 已横向滚动时，光标进入左/右边缘触发自动滚动。
新的顺序自然进入 `useRecoverySnapshot` 的 dirty 流，重启后保留。

## 1. Goals

- 单窗口内按住 tab → 拖动 → 释放，即改变 tabs 数组顺序。
- 实时重排：拖动过程中其他 tab 立刻让位；光标所在处即是最终位置。
- 边缘自动滚动：当 tabbar 溢出滚动时，光标进入容器左/右 40px 内自动横向滚动。
- 键盘 shortcut（Cmd+1..9）等定位到 tab 的行为**继续按新顺序生效**。
- 拖动结束后 tabs 数组变化通过既有 `useRecoverySnapshot` watch 落盘。
- 支持 Wails（Chromium 内嵌）与 Capacitor iOS WebView，因此使用 Pointer Events，
  不使用 HTML5 Drag & Drop。

## 2. Non-goals

- **不做跨窗口拖出/合并**：项目当前是单窗口，不引入多窗口。
- **不做 tab ↔ pane 互拖**：pane 拖入 tabbar 转 tab、tab 拖入 PaneGrid 变 pane 均不在本次范围。
- **不做键盘移动 tab 位置**（如 Cmd+Alt+←/→）；如需可后续单独加。
- **不做拖动动画补间**：其他 tab 让位是直接布局重排（CSS 无 transition），后续如觉得
  硬跳可加 `transition: transform 100ms`，本次不加以免与 xterm 内的 pointer 事件时序纠缠。
- **不做拖动过程中的鬼影（ghost image）**：源 tab 本身在光标下就地跟随，无需额外浮层。
- **不改 TabBar 之外的组件**：`+`、`×`、`.plus`、`.close` 按钮的语义不变。
- **不动 `currentTabId`**：拖动仅改顺序，不切换活跃 tab。

## 3. 现状

- `desktop/frontend/src/components/TabBar.vue` — `<div class="tabs"> v-for tab</div>` 布局，
  props: `tabs`, `currentId`, `starting`；emits: `activate`, `close`, `new`。
  `.tabs` 容器 `overflow-x: auto` 已开滚动；无任何 pointer/drag 事件。
- `desktop/frontend/src/App.vue:178` — `const tabs = ref<Tab[]>([])`；tabs 顺序变更由数组
  变异触发下游派生（`tabSummaries`、`tabIndexById` 等）。
- `desktop/frontend/src/composables/useRecoverySnapshot.ts` — watch `tabs.value` 变化后
  批量 dirty → 定时 fsync；顺序变化 == 数组结构变化，自动进入 dirty。
- `desktop/frontend/src/components/TabBar.test.ts` — 现有 vitest + `@vue/test-utils` 套件，
  作为本设计新增测试的落点。

## 4. 交互与实现

### 4.1 事件识别（TabBar.vue 内部）

在 `<script setup>` 内新增 local state（不进 props）：

```ts
const drag = ref<null | {
  fromId: string;
  startX: number;
  active: boolean;       // 是否已过阈值进入拖动态
  pointerId: number;
  overId: string | null;
  position: 'before' | 'after' | null;
}>(null);
const DRAG_THRESHOLD = 4;
const EDGE_SCROLL_ZONE = 40;
const EDGE_SCROLL_SPEED = 8; // px/frame
```

`.tab` 上加 `@pointerdown="onPointerDown(t, $event)"`。逻辑：

1. `onPointerDown(t, e)`：仅接受左键（`e.button === 0`）；忽略落在 `.close` 上的事件
   （在 `.close` 里 `@pointerdown.stop` 已阻断，作为兜底再检查 `e.target.closest('.close')`）；
   初始化 `drag.value`，`active=false`；不阻止默认，让 click 继续正常发生。
2. `pointermove`（挂到 `window`）：
   - 若 `!drag.active` 且 `|e.clientX - startX| >= DRAG_THRESHOLD`，进入拖动态：
     `drag.active = true`；给源 tab DOM 加 `.dragging` class（`opacity: 0.5`）；
     `.tabs` 容器 `setPointerCapture(pointerId)`；window `contextmenu` 一次性 preventDefault。
   - 命中判断：遍历 `.tabs > .tab` 的 `getBoundingClientRect()`，找到 clientX 落在 rect 内的
     tab，计算 `position = clientX < mid ? 'before' : 'after'`；
     未命中且 clientX 右于所有 tab → 目标 = 最右 tab, `after`；左于所有 → 最左, `before`。
   - 若 `overId===fromId` 或 (`overId, position`) 与上次相同 → 不 emit。否则 emit
     `('reorder', fromId, overId, position)` 并更新 local `overId`, `position`。
   - 自动滚动：`.tabs` 容器 rect 已缓存；若 `clientX - rect.left < EDGE_SCROLL_ZONE` 或
     `rect.right - clientX < EDGE_SCROLL_ZONE`，如果 rAF 循环尚未启动就启动。
3. `pointerup / pointercancel`（挂到 `window`）：`drag = null`，卸掉 DOM class，
   停 rAF 循环，取消 pointercapture。
   若 `hasDragged`（曾进入 active），在 `window.addEventListener('click', prevent, {capture:true, once:true})`
   注册一次性拦截，阻止本次 pointerup 尾随的 click 触发 `activate`。

### 4.2 自动滚动循环

```ts
let rafId: number | null = null;
function tickEdgeScroll() {
  if (!drag.value?.active) { rafId = null; return; }
  const rect = tabsEl.value!.getBoundingClientRect();
  const x = lastClientX;
  if (x - rect.left < EDGE_SCROLL_ZONE)         tabsEl.value!.scrollLeft -= EDGE_SCROLL_SPEED;
  else if (rect.right - x < EDGE_SCROLL_ZONE)   tabsEl.value!.scrollLeft += EDGE_SCROLL_SPEED;
  else { rafId = null; return; }
  rafId = requestAnimationFrame(tickEdgeScroll);
}
```

`lastClientX` 在 `pointermove` 中更新。滚动导致 rect 变化会在下一帧的命中判断里被自然读到。

### 4.3 emit 语义

```ts
const emit = defineEmits<{
  (e: 'activate', id: string): void;
  (e: 'close', id: string): void;
  (e: 'new'): void;
  (e: 'reorder', fromId: string, targetId: string, position: 'before' | 'after'): void;
}>();
```

选 `(fromId, targetId, position)` 而不是 `newIds: string[]`：
- 父组件基于当前 tabs 顺序做 splice，避免 TabBar 本地"预览顺序"与父组件不同步。
- 幂等：如果 fromId==targetId 或位置未变，父组件收到也是 no-op（下面 App.vue 实现里做校验）。
- 便于测试：不依赖数组序列化。

### 4.4 App.vue 侧

```ts
function onTabReorder(fromId: string, targetId: string, position: 'before' | 'after') {
  if (fromId === targetId) return;
  const arr = tabs.value.slice();
  const from = arr.findIndex(t => t.id === fromId);
  if (from < 0) return;
  const [moved] = arr.splice(from, 1);
  let to = arr.findIndex(t => t.id === targetId);
  if (to < 0) { tabs.value = arr; return; } // 目标已消失，退化为把 moved 追加到末尾
  if (position === 'after') to += 1;
  arr.splice(to, 0, moved);
  tabs.value = arr;
}
```

模板：`<TabBar ... @reorder="onTabReorder" />`。`currentTabId` 不动。

### 4.5 CSS

```css
.tab.dragging { opacity: 0.5; }
.tab { touch-action: pan-y; } /* 允许纵向滚动，横向由 pointer 事件接管 */
```

`.dragging` 只需半透明区分正在拖的项；不加 `pointer-events: none`——命中判断已经过滤了自身。

## 5. 边界与错误情况

| 场景 | 行为 |
|---|---|
| 只有 1 个 tab | pointerdown 记 state；move 无其他目标 → 不 emit；up 时 click 正常激活 |
| 拖到自己 | `fromId === targetId` → 不 emit（4.3 幂等，`fromId === overId` 时局部提前 return） |
| 拖出 TabBar 到主体区域 | 最后一次 emit 的顺序即为最终顺序（无回滚，符合"实时应用"语义） |
| pointercancel（系统夺焦、iOS 滑动） | drag 清空；顺序保留在最后 emit 值 |
| 拖动过程中新 tab 到达（`startNewTab` 完成） | tabs 数组末尾新增；命中判断每帧读 `.tabs > .tab` DOM 快照，兼容 |
| 拖动中某 tab 被 `sweepMissingSessions` 删掉 | 若被删的是 `fromId` → 视同 pointercancel；否则命中判断自然跳过 |
| .close 按钮误触 | `.close` 上的 `@pointerdown.stop` + `closest('.close')` 兜底 |
| 右键 / 中键按下 | `e.button !== 0` → 不进入拖动逻辑 |

## 6. 测试

沿用 `desktop/frontend/src/components/TabBar.test.ts`（vitest + `@vue/test-utils`）。
新增用例：

1. `pointerdown` + 小于阈值的 `pointermove` + `pointerup` → 不发 `reorder`；click 仍触发
   `activate`。
2. 拖动第 1 个 tab 越过第 3 个 tab 中线右侧 → emit `reorder('tab-1', 'tab-3', 'after')`。
3. 拖动过程中在同一目标位置多次 pointermove → 只 emit 一次（去抖）。
4. 拖到自身 → 不 emit。
5. `pointercancel` → drag 状态清空，不 emit（若已 emit 过则保留最后一次）。
6. `pointerdown` 落在 `.close` → 不进入拖动态；`close` emit 正常发生。
7. 手动 mock `.tabs` 容器的 `scrollLeft` 与 rect，光标进入右侧边缘 → 触发滚动分支
   （用 rAF stub 验证 `scrollLeft` 被增加）。

App.vue 侧新增 `App.tabReorder.test.ts`（或加入现有 App 测试文件）：`onTabReorder('t1','t3','after')`
在 `[t1, t2, t3, t4]` 上产出 `[t2, t3, t1, t4]`；`currentTabId` 不变；从不存在的 fromId 触发时
tabs 不变。

## 7. 回滚

功能局限在 TabBar 本地状态 + App.vue 一个 handler + emit 类型扩展。回滚就是
`git revert`；`useRecoverySnapshot` 的 tab 顺序字段本来就存在，回滚不需要 migration。
