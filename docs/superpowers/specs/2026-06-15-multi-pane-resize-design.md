# Multi-pane width/height adjustment

> **Status**: draft
> **Date**: 2026-06-15
> **Scope**: desktop only (Wails PaneGrid)
> **See also**: [2026-05-10-pane-split-layouts-design.md](./2026-05-10-pane-split-layouts-design.md) · [docs/spec/architecture.md](../../spec/architecture.md) · AGENTS.md 红线 #6

## Problem

`vertical` / `horizontal` / `grid2x2` 三种 layout 现在用 CSS Grid `1fr 1fr` 死分。一旦左右或上下两个 pane 的内容宽度不一致（左边 `nvim` 需要宽，右边 `htop` 只要窄），用户没法手动调整比例。

## Goal

让桌面 driver 端的用户：
- 用鼠标拖动 pane 之间的分隔线调整列宽 / 行高。
- 双击分隔线复位到 50/50。
- 拖动过程中 xterm 即时 reflow，PTY 子进程只在松手时收到一次 `SIGWINCH`。

## Non-goals

- 持久化（关 tab 即丢；新开 tab 总是从 50/50 开始）。
- 移动端 / web viewer 端的拖拽。viewer 锁 PTY 尺寸，不参与 fit。
- 嵌套 split / BSP / 任意比例矩阵。仍只支持现有 4 种 layout。
- 快捷键步进调整（YAGNI；如需再迭代）。
- 跨 tab 比例同步。

## Data model

`desktop/frontend/src/lib/types.ts::Tab` 增加两个字段：

```ts
export interface Tab {
  id: string;
  layout: LayoutKind;
  panes: Pane[];
  activePaneIdx: number;
  colRatio: number; // 0.1..0.9, default 0.5
  rowRatio: number; // 0.1..0.9, default 0.5
}
```

- `colRatio` = 左列宽度占整个 PaneGrid 容器宽度的比例。`vertical` 用它，`grid2x2` 用它。
- `rowRatio` = 上行高度占整个容器高度的比例。`horizontal` 用它，`grid2x2` 用它。
- `single` 下两字段忽略，但仍保留默认值 0.5，省掉所有分支的初始化判断。
- 越界（<0.1 或 >0.9）由视图层 clamp 兜底，存储值本身不强制。
- 不写入 `config.json`，不进 prefs sync。

常量集中在 `layout.ts`：

```ts
export const RATIO_MIN = 0.1;
export const RATIO_MAX = 0.9;
export const RATIO_DEFAULT = 0.5;
```

## Layout state transitions

`layout.ts` 的两条纯函数继续是 layout 的唯一入口。补两条规则：

1. **新 Tab / `transitionLayout` split**：返回的 Tab 继承调用方传入的 `colRatio` / `rowRatio`。新 split 不重置比例——同 tab 内多次 split 复用一致的比例。
2. **`closePane` 收敛**：grid2x2 → vertical / horizontal 时保留两个 ratio；vertical / horizontal → single 时同样保留。下一次同方向 split 会从同一个 ratio 起步。

`TransitionResult` / `CloseResult` 接口因此多两个透传字段。`App.vue` 调用方负责把 `tab.colRatio/rowRatio` 传进来，把返回值写回 tab。

签名变更：

```ts
export interface TransitionResult {
  layout: LayoutKind;
  panes: Pane[];
  activePaneIdx: number;
  newPaneIdx: number;
  noop?: boolean;
  colRatio: number;
  rowRatio: number;
}
```

## View: PaneGrid.vue

把 `<style scoped>` 里写死的 `grid-template` 删掉，改成 computed `:style`：

```ts
const gridStyle = computed<CSSProperties>(() => {
  const c = clamp(props.tab.colRatio, RATIO_MIN, RATIO_MAX);
  const r = clamp(props.tab.rowRatio, RATIO_MIN, RATIO_MAX);
  const cl = c.toFixed(4);
  const cr = (1 - c).toFixed(4);
  const rt = r.toFixed(4);
  const rb = (1 - r).toFixed(4);
  switch (props.tab.layout) {
    case "single":     return {};
    case "vertical":   return { gridTemplate: `"a b" / ${cl}fr ${cr}fr` };
    case "horizontal": return { gridTemplate: `"a" ${rt}fr "b" ${rb}fr / 1fr` };
    case "grid2x2":    return { gridTemplate: `"a b" ${rt}fr "c d" ${rb}fr / ${cl}fr ${cr}fr` };
  }
});
```

模板上：`<div class="pane-grid" :class="tab.layout" :style="gridStyle">`。

按 layout 渲染 `<PaneSplitter>`：

- `vertical` / `grid2x2`：一条 vertical splitter (`orientation="col"`)，绑定 `colRatio`。
- `horizontal` / `grid2x2`：一条 horizontal splitter (`orientation="row"`)，绑定 `rowRatio`。

splitter 是 `position:absolute` 覆盖在 grid gap 上的命中条，**不进入 grid 单元**，因此不影响 `grid-area`。

## Component: PaneSplitter.vue

新增 `desktop/frontend/src/components/PaneSplitter.vue`。

Props:
```ts
{
  orientation: "col" | "row";
  ratio: number;           // 0.1..0.9, parent-controlled
  containerRect: () => DOMRect | null; // PaneGrid 容器的 getBoundingClientRect
}
```

Emits:
```ts
"update:ratio" (next: number)  // 拖动中实时
"commit" ()                    // pointerup / cancel
"reset" ()                     // dblclick
```

DOM 结构：单个 `<div class="pane-splitter" :class="orientation">`，绝对定位：

- `col`：`top:0; bottom:0; left: calc(${ratio*100}% - 3px); width: 6px; cursor: col-resize;`
- `row`：`left:0; right:0; top: calc(${ratio*100}% - 3px); height: 6px; cursor: row-resize;`

z-index 5（grid cell 默认 0；`.cell-controls` 在 cell 内部，不冲突）。`background: transparent`，hover 时 `background: var(--accent-dim, rgba(255,255,255,0.08))` 给一点点反馈。

交互：

- `pointerdown`：记录 `startX/Y`、`startRatio`、`setPointerCapture(e.pointerId)`、`document.body.style.cursor = "col-resize"/"row-resize"`。
- `pointermove`：从 `containerRect()` 取最新 rect（拖动期间窗口可能 resize），算 `nextRatio = clamp(startRatio + delta / size, RATIO_MIN, RATIO_MAX)`，emit `update:ratio`。
- `pointerup` / `pointercancel`：释放 capture、恢复 body cursor、emit `commit`。
- `dblclick`：emit `reset`。
- `onBeforeUnmount`：若仍持有 capture，release。

实现注意：`pointermove` 必须用 `pointerId` 关联的 capture，避免拖出窗口丢事件。

## Dragging coordination

PaneGrid 在拖动期间维护 `const dragging = ref(false)`。pointerdown 置 true，commit 置 false。

把 `dragging` 传到 `<TerminalView :resize-suspended="dragging">`。TerminalView 内部已有 ResizeObserver → fit 路径；新增逻辑：

```ts
// TerminalView.vue
const props = defineProps<{ ...; resizeSuspended?: boolean; }>();

// 在 fitObserver 的 fit() 回调里
if (props.resizeSuspended) {
  // 拖动期间：本地 reflow 但不发协议帧
  fit.fit();
  return;
}
// 正常路径：fit + sendResize
fit.fit();
maybeSendResize();
```

`maybeSendResize` 沿用红线 #6 的 `expectedCols/Rows` skip-no-op 守卫。

mouseup 之后容器尺寸已稳定，ResizeObserver 不会再 fire；因此 commit 路径必须**主动**触发一次 sendResize：`PaneGrid` 在 splitter 的 `commit` 事件里 emit `resize-commit`，TerminalView 监听后 `nextTick(() => maybeSendResize())`。`maybeSendResize` 本身有 skip-no-op 守卫，相邻的两次相同 cols/rows 不会重复发帧。

## App.vue integration

每个 `Tab` 创建口子统一走 helper：

```ts
// App.vue or new util
function makeTab(initial: Partial<Tab> = {}): Tab {
  return {
    id: crypto.randomUUID(),
    layout: "single",
    panes: [EMPTY_PANE],
    activePaneIdx: 0,
    colRatio: RATIO_DEFAULT,
    rowRatio: RATIO_DEFAULT,
    ...initial,
  };
}
```

`transitionLayout` / `closePane` 调用点把 `tab.colRatio` / `tab.rowRatio` 传入并把返回值写回。

`PaneGrid` 对外暴露 `@update:col-ratio` / `@update:row-ratio`，`App.vue` 把这两个写回 tab。`dragging` 状态和 `commit-resize` 事件是 `PaneGrid` 内部状态，直接传给子组件 `TerminalView`，不冒泡到 `App.vue`。

## Error handling

- **ratio 越界**：computed 内 clamp 兜底，永不渲染非法 grid-template。
- **拖动中 pane 被 close**：`PaneSplitter.onBeforeUnmount` 释放 pointer capture；父组件 `dragging` 在 unmount 时被销毁，无副作用。
- **pointer 抓不到 / 拖出窗口**：`setPointerCapture` 即使 cursor 离开窗口仍能 receive `pointermove`；`pointercancel` 兜底走 `commit`。
- **快速连击 dblclick**：第一次 click → pointerdown/up（无位移所以 ratio 不变）；浏览器 dblclick 事件触发 reset 到 0.5。可能在第二个 click 时 ratio 已经是 0.5，reset 仍正确。
- **layout 切换中拖动**：理论上不会发生（splitter 跟 layout 同源渲染，切换瞬间 splitter 重建）。若中途切换，`v-if` 卸载会 release capture。

## Testing

新建 `desktop/frontend/src/components/PaneSplitter.test.ts`：
- pointerdown → pointermove emit `update:ratio` 数值与 delta/size 一致
- clamp 在 0.1 / 0.9 端点
- pointerup emit `commit`
- dblclick emit `reset`
- onBeforeUnmount release capture（用 spy）

扩展 `PaneGrid.test.ts`：
- 四种 layout 渲染出预期 `grid-template` 字符串（包含正确 fr 值）
- vertical / horizontal 渲染 1 个 splitter；grid2x2 渲染 2 个
- 拖动期间 `resize-suspended` prop 为 true，commit 后回 false

扩展 `layout.test.ts`：
- `transitionLayout` 透传 colRatio / rowRatio
- `closePane` grid2x2 → vertical / horizontal 时保留比例
- `closePane` vertical / horizontal → single 时保留比例

`TerminalView.test.ts` 添加：
- `resize-suspended=true` 时 fit 仍跑、但 `sendResize` 不调
- 收到 `resize-commit` 事件后下一个 tick 触发 `sendResize`

## Out of scope

- 持久化、移动端、Web 端、嵌套 split、快捷键调整。
- mirror viewer 的 splitter（viewer 锁尺寸，不让本地 fit 跑）。

## Risks

1. **红线 #6 三件耦合**：predictCellDims + sendResize 排队 + skip-no-op。本设计在 `resize-suspended` 期间仍跑 FitAddon（保持 xterm 视觉一致），但短路 sendResize。pointerup 后一次性触发的 RESIZE 会走 skip-no-op 守卫，不会向已 ANNOUNCE 的尺寸重复发帧。
2. **CSS Grid 与 splitter 的几何对齐**：splitter `left/top: calc(${ratio*100}% - 3px)` 假设 grid gap 等于 splitter 半宽（`gap: 2px`，splitter 6px）。视觉上可以对齐；如果不齐，把 splitter 半宽 = `(splitterWidth - gap) / 2` 修一下即可。
3. **拖动过程中 ResizeObserver 高频触发**：FitAddon 每帧跑一次 `term.resize`，对 xterm.js 而言是 O(rows) 操作。8 列 × 60 帧 ≈ 480 次/秒；实测应可承受，必要时上 `requestAnimationFrame` throttle。
4. **viewer 端兼容**：viewer 收到 META 锁尺寸，本设计不动 viewer 路径，无回归风险。
