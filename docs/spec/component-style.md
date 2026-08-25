# 组件样式规范

> **Audience**: 改 atterm 前端 UI 的工程师
> **Last updated**: 2026-08-24
> **Status**: stable
> **See also**: [conventions.md](./conventions.md) · [protocol.md](./protocol.md) · [session-bar-pin-design](../superpowers/specs/2026-07-20-session-bar-pin-design.md) · [sidebar-search-design](../superpowers/specs/2026-07-24-sidebar-search-design.md)

本文定义 atterm 桌面端与 Web/PWA 前端的组件视觉规则。目标是让后续迭代保持“终端工具 + 低干扰控制台”的一致质感，避免局部组件退回浏览器或系统默认控件。

## 设计方向

- **终端优先**：TerminalView / PaneGrid 是主画面，控制 UI 只提供必要状态和操作，不抢占终端内容。
- **紧凑明确**：默认控件高度 32px，正文 13px，说明文字 12px；优先信息密度和可扫读性，不做大留白营销式布局。
- **低饱和深色基底**：desktop 使用 `--bg` / `--panel` / `--border` / `--fg` / `--fg-dim` / `--accent` 等变量；新增颜色先抽象成 token，避免散落硬编码。
- **边框而非阴影堆叠**：常规层级靠 1px border、轻量背景和 6-8px radius；深阴影只用于浮层菜单、dialog 与临时 overlay。
- **平台像原生 app，但控件不裸奔**：窗口、字体可以顺应系统；下拉、菜单、tab、dialog、toggle 等交互控件必须使用项目组件样式，不直接暴露浏览器/系统原生外观。

## Token 与主题

### Desktop

全局 token 定义在 `desktop/frontend/src/style.css :root`：

| token | 用途 |
|-------|------|
| `--bg` | app 背景、输入框背景、下拉菜单背景 |
| `--panel` | dialog、侧栏、二级面板背景 |
| `--border` | 面板和控件边界 |
| `--fg` | 主文字、控件文字 |
| `--fg-dim` | hint、label、次级状态 |
| `--accent` | active、focus、primary、选中指示 |
| `--good` / `--bad` | 成功/启用、错误/危险 |
| `--terminal-*` | 终端区域专用背景、网格、overlay |

规则：

- `.vue` 内样式默认使用 `var(--token)`，不要在组件里直接写主题色。
- 允许少量语义色硬编码，但必须有稳定语义，例如远程会话 amber、危险 hover 的透明红；新增前先确认现有 token 不适用。
- 终端主题切换通过 `App.vue` 的 `themeStyle` 注入 `--terminal-*`，普通设置组件不要读取 terminal palette 做 UI 主题。

### Web/PWA

- Web 客户端使用 Naive UI + `web/src/shared/tokens.css`，组件优先走 Naive 组件和 shared token。
- 不引入 CDN script/style；外观依赖必须通过 npm + Vite 打包为同源 assets。
- Web 的视觉可以比 desktop 更接近管理后台，但颜色、密度、状态语义要与 desktop 保持同一套命名和含义。

## 基础控件

### 下拉选择

- Desktop 统一使用 `desktop/frontend/src/components/SelectDropdown.vue`，不要在 Settings、工具栏或 plugin 设置里新增原生 `<select>`。
- `SelectDropdown` 必须提供 `ariaLabel`，选项结构为 `{ value, label, description? }`。
- 选项来自 i18n 时用 computed 包装，避免切换语言后 label 不刷新。
- 打开菜单使用 listbox 语义，保留键盘操作：ArrowUp/ArrowDown、Home/End、Enter、Escape、点击外部关闭。
- 如果某处确实需要浏览器原生 select，必须在 PR 里说明原因，并加注释限定范围。

示例：

```vue
<SelectDropdown
  v-model="selectedDefaultShell"
  :options="defaultShellOptions"
  :aria-label="t('settings.general.defaultShell')"
  :disabled="saving"
  @update:modelValue="onDefaultShellChange"
/>
```

### Button

- 普通按钮：透明或 `--bg` 背景、`--border` 边框、6px radius、13px 字号。
- Primary：`--accent` 背景，文字使用深色 `#0d1117`，只用于主动作。
- Danger：边框/文字使用 `--bad`，hover 使用低透明红背景，不用整块高饱和红。
- Icon button 需要有 title/aria-label；hover 背景用 `rgba(255, 255, 255, 0.04-0.08)`。

### Input / textarea / number

- 高度默认 32px，padding `6px 10px`，背景 `--bg`，边框 `--border`，radius 6px。
- focus 使用 `--accent` 边框或 `box-shadow: 0 0 0 2px var(--accent)`；同一组件内保持一种。
- 输入错误不要只靠边框颜色；需要错误文案 `.error` 或 inline hint。

### Checkbox / switch

- 简单布尔项可以用 checkbox，但 label 与 hint 必须清楚说明影响范围。
- 表示“连接/上传/启用中”的长期状态优先用项目内 toggle switch 样式：track 32x18、thumb 14、checked 用 `--good`。
- 不要只隐藏按钮来表达权限；权限相关交互必须和 relay / desktop host 的实际拦截一致。

## Settings 组件规范

Settings 是最容易跑偏的区域，新增 tab 或字段时遵守以下结构：

- tab 内容根节点使用 `.tab-pane { display:flex; flex-direction:column; gap:12-14px; }`。
- 字段标题使用 `.field-label`：12px、uppercase、letter spacing `0.05em`、`--fg-dim`。
- 说明文字使用 `.hint`：12px、`--fg-dim`、line-height 1.45-1.5、margin 0。
- 错误使用 `.error` 或 `.warning`：12px、`--bad`、margin 0。
- 下拉必须用 `SelectDropdown`；Default Shell、Terminal Theme、Language、Remote Permission 都属于同一类控件。
- 设置保存失败时要回滚 UI 状态，并显示错误；不要乐观更新后静默失败。
- 新增用户可见文案必须同步 `desktop/frontend/src/i18n/messages/en.ts` 和 `zh-CN.ts`。
- 语义相关且较短的字段显式使用 `.settings-field-grid` 分组；双列使用
  `.settings-field-grid--two`，终端外观等短字段行使用
  `.settings-field-grid--compact`。不要用全局 `auto-fit` 自动重排所有设置项。
- 长文本输入、说明、警告、错误、危险操作和列表保持整行；字段内部保持
  label → control → hint 的顺序，不要为了凑列拆散字段语义。
- `640px` 以下字段网格必须降为单列；列表、工具栏和按钮行允许换行，输入控件
  使用 `min-width: 0`，不得把 Settings 内容区撑出横向滚动。

## Dialog / overlay

- modal backdrop 使用 fixed inset 0，黑色透明度 0.5-0.6。
- 主 dialog 使用 `--panel` 背景、`--border` 边框、8px radius、`max-width: calc(100vw - 32px)`、`max-height: calc(100vh - 32px)`。
- Header 标题沿用 14px / 600 / uppercase / letter spacing / `--fg-dim`。
- Footer 动作靠右，按钮高度 32px。
- 嵌套确认弹窗 z-index 要高于父 dialog，并保持同一视觉语言。

## Terminal / Pane UI

- TerminalView 及 PaneGrid 视觉不能被通用表单样式污染；终端背景、overlay、网格线使用 `--terminal-*`。
- Pane gap 维持 2px；调整时必须同步 `predictCellDims` 的尺寸假设，否则新 pane 初始 PTY winsize 会回归。
- Pane 关闭按钮、remote badge、viewer overlay 要使用轻量 overlay，不改变 xterm cell 尺寸。
- Pane 内点击目标必须以事件归属 pane 为准，不能隐式作用于当前 active pane；此类交互需要布局/组件测试覆盖。

## Title bar

`desktop/frontend/src/components/TitleBar.vue` 是 macOS / Windows / Linux 桌面端共用的顶部 chrome。

### 布局与标题居中

- macOS 走 `mac.TitleBarHiddenInset()`，traffic lights 占左 80px，root 用 `padding-left: 80px`；Windows / Linux 渲染 `WindowControls` 在最右。
- 标题对窗口**几何中心**居中，而**不是** flex 中点。两侧不对称（左侧 80px 内边距、右侧 status + 2 个图标按钮 + Win/Linux 还有窗口控制）时，flex 撑开法会把标题压偏 60-80px。
- 正确做法：

  ```css
  .titlebar { position: relative; display: flex; align-items: center; padding: 6px 14px; }
  .window-title {
    position: absolute; left: 50%; top: 50%;
    transform: translate(-50%, -50%);
    max-width: 50%;
    /* 字号、颜色等照常 */
  }
  .status { margin-left: auto; /* 把右侧元素挤到边缘 */ }
  ```

- 双击 titlebar 空白区或标题区都走 `windowToggleMaximize`（macOS TitleBarHiddenInset 下系统 zoom 不生效）；`@dblclick.self` 同时挂在 root 和 `.window-title` 上。
- titlebar 整体走 `--wails-draggable: drag`（Linux/Windows frameless），状态文字和图标按钮单独 `--wails-draggable: no-drag` 以保留点击。

### Running indicator

当前 active session 的 `task_state === 'running'` 时，在 titlebar 底部画一条波形指示器：

```css
.titlebar::after {
  content: ""; position: absolute; left: 0; right: 0; bottom: -1px;
  height: 3px;
  background: repeating-linear-gradient(90deg,
    transparent 0px,
    transparent 200px,
    rgba(74, 222, 128, 0.3) 320px,
    #4ade80 500px,
    #bbf7d0 560px,
    #4ade80 620px,
    rgba(74, 222, 128, 0.3) 800px,
    transparent 920px,
    transparent 1120px);
  background-size: 1120px 100%;
  box-shadow: 0 -1px 6px rgba(74, 222, 128, 0.4);
  animation: titlebar-running-sweep 3.5s linear infinite;
  opacity: 0;
  transition: opacity 0.5s ease;
  pointer-events: none;
}
.titlebar.is-running::after { opacity: 1; }
@keyframes titlebar-running-sweep {
  0% { background-position: 0 0; }
  100% { background-position: 1120px 0; }
}
@media (prefers-reduced-motion: reduce) {
  .titlebar::after { animation: none; background: #4ade80; transition: opacity 0.25s ease; }
}
```

设计要点（**别动**，每个数字都有原因）：

- **透明底色，颜色跟着波走**：波到的位置才有绿色，过去就空回去。模拟"水波向右流"的语义而不是"常亮长条"。
- **重复平铺 1120px / 单波 720px / 两侧各 200px 空白**：保证常驻 1-2 道波在屏，一道出右边时下一道已经在进入，**无空窗期**。
- **波形对称渐变**：`transparent → rgba(0.3) → #4ade80 → #bbf7d0 (峰) → #4ade80 → rgba(0.3) → transparent`，三档亮度看得到"明暗起伏"但全部在绿色 spectrum 内，不会让谷地看起来像断条。
- **`animation-timing-function: linear`**：横向流动用线性才不会忽快忽慢。3.5s 周期，正向位移 1120px / 周期 = 320 px/s。
- **伪元素一直挂着**：`.titlebar::after` 默认 `opacity: 0` 让它隐藏，但 `animation` 持续跑。`.is-running` 只翻 opacity；进入 running 时波**已经在流动**，不会出现 cold-start 的某颗波突然出现在屏幕中间的突兀感；退出 running 时波继续流，opacity 渐隐到 0，没有 snap。
- **`bottom: -1px` + `box-shadow: 0 -1px 6px`**：让光带压在 titlebar 与 tab bar 的分隔线上、晕光向上渗进 chrome。这样光带不和下面的 tab bar 抢空间。
- **`prefers-reduced-motion: reduce`**：直接关动画退化成 `background: #4ade80` 静态绿条，过渡时长压到 0.25s。

数据流：

- `App.vue::currentActiveSession` 算出当前 tab 当前 pane 的 `SessionInfo`；
- `App.vue::currentTaskStateForBar = computed(() => currentActiveSession.value?.task_state ?? null)`；
- 作为 `:current-task-state` prop 传进 `TitleBar`；TitleBar 内部 `computed(() => props.currentTaskState === 'running')` 决定 `.is-running` class。

**铁规**：

- 不要把 indicator 改成 pulse-only（纯 opacity 呼吸不带横向移动）—— 那是早期版本，被推翻。波动必须是横向流动。
- 不要把单波换回 `linear-gradient(...) no-repeat`：那会让一道波出去后到下一道波之间出现明显空窗。
- 不要把 indicator 移到 tab bar；红线 #1 要求语义是"当前 active session 在跑"，title bar 是合适的位置。
- 不要 hard-code 颜色到组件外；调色板用 `#4ade80 / #bbf7d0` + `rgba(34, 197, 94, …)` 系列（emerald-400/200 与对应 rgba）。

## 任务卡片：session type chip 与 error line

mobile / web 的 session 列表是 P2 阶段最容易跑偏的视图，新增信息时遵守以下结构：

### Type chip

`type ∈ {shell, ai, test, build, deploy}`（来自 `MetaPayload.type`，详见
`docs/spec/protocol.md` §META）。卡片头部右上角渲染一颗 6-8px 高的椭圆 chip：

| type | 调色 | 语义 |
|------|------|------|
| `ai` | `--accent` 变体（紫蓝） | 长任务、最有可能需要远程接管 |
| `test` | 青绿（如 `#34d399`） | 短任务、失败时关心 error_lines |
| `build` | 橙（如 `#f59e0b`） | 中等任务 |
| `deploy` | 红（如 `#f87171`） | 高风险任务 |
| `shell` | 不渲染 chip | 普通 shell 不打扰 |

- chip 用 CSS variable 派生颜色，**不要**为每种 type 单独硬编码；统一通过
  `--type-{ai,test,build,deploy}` 在 `style.css` / `tokens.css` 注册。
- chip 文本 11px、letter-spacing `0.04em`、uppercase；padding `1px 6px`；
  圆角 `999px`（pill）。
- chip 不要替代 `task_state`（running / failed / …）的状态视觉，那是另一层；
  二者并排。

### Error line

`summary.error_lines` 仅在 `task_state == 'failed'` 时渲染，且只显示第一条
（`error_lines[0]`）。结构：

```vue
<span class="err-line" :data-testid="`task-err-${session_id}`">
  {{ summary.error_lines[0] }}
</span>
```

约束：

- 颜色 `#f87171`（red-400），mono 字体 (`var(--font-mono)`)，12px。
- 单行省略：`white-space: nowrap; overflow: hidden; text-overflow: ellipsis`。
- 与卡片其它行 `gap: 2-4px`，不要单独占大块空间——这是「一行错误摘要」，
  不是错误详情视图。
- 不带 i18n key——内容是 PTY 原始输出，按字面渲染。

`summary.recent_output` 不在卡片上渲染（会撑爆列表），留给未来的 session
detail 视图。

## 会话侧栏：内部滚动 + 分组折叠

`desktop/frontend/src/components/TaskSidebar.vue` + `TaskGroupedList.vue` +
`SessionRowMenu.vue` 负责左侧任务面板。两条结构性约束**不能弱化**：

### 内部滚动，不撑窗口

侧栏的 flex 链路必须保证里层 `.list-wrap` 的 `flex: 1 1 auto; overflow-y: auto`
真正生效；否则会话多了之后整列把窗口拉得比 viewport 还高（屏幕底部连终端
都被推下去）。要求：

```css
.task-sidebar { display: flex; flex-direction: column; height: 100%; }
.expanded {
  flex: 1 1 auto;       /* 占满 sidebar 主轴可用空间 */
  min-height: 0;        /* 允许 flex item 收缩到内容以下 */
  display: flex;
  flex-direction: column;
}
.list-wrap {
  flex: 1 1 auto;
  overflow-y: auto;
  overflow-x: hidden;
  /* 隐藏 WebKit / Firefox scrollbar gutter——已有"下面还有"的视觉暗示 */
  scrollbar-width: none;
}
.list-wrap::-webkit-scrollbar { display: none; }
```

**反例（千万别）**：

- `.expanded` 没有 `display: flex` —— 那 `.list-wrap` 的 `flex: 1` 就 no-op，
  内容长出 sidebar，整列把外层 flex row（`.main-row`）撑高，窗口跟着变高。
- `.task-sidebar` 漏 `min-height: 0` —— 同样导致 flex item 不能 shrink。

### 按主机 / 状态分组的折叠

`TaskGroupedList` 在 host / state 维度都支持每个 group 单独折叠。语义：

- 状态：`collapsedGroups: ref<Set<string>>`，session-local（关窗口或刷新会重置，
  跟 `completed-fold` 的生命周期一致；目前不持久化）。
- toggle 实现要**替换** Set 实例（`new Set(collapsedGroups.value)`）——
  Vue 浅响应式认 `.add() / .delete()` 是 same instance，会漏更新。
- 折叠时 `v-for` 跑空数组：`v-for="s in (isGroupCollapsed(key) ? [] : groups[key])"`，
  整组 row 不进 DOM；展开时再渲染。
- caret 字符 `▼`（展开）/ `▶`（折叠）；CSS 设 `font-size: 9px; opacity: 0.7; width: 9px`
  让宽度稳定不抖。

header 必须可键盘操作：

```vue
<header
  class="host-header"
  role="button"
  tabindex="0"
  :aria-expanded="!isGroupCollapsed(key)"
  @click="toggleGroupCollapsed(key)"
  @keydown.enter.prevent="toggleGroupCollapsed(key)"
  @keydown.space.prevent="toggleGroupCollapsed(key)"
>
  <span class="caret">{{ isGroupCollapsed(key) ? '▶' : '▼' }}</span>
  …
  <button v-if="unreadByGroup[key] > 0" class="mark-all" @click.stop="onMarkGroup(key)">✓</button>
</header>
```

铁规：

- header 内部的 mark-all 按钮**必须** `@click.stop`，否则点 ✓ 一并触发分组折叠。
- header 加 `cursor: pointer` + `:hover` 微底色让"可点"对视觉清楚。
- 状态分组（`groupBy === 'state'`）和 host 分组共用同一套 `collapsedGroups`
  set；切换分组维度不需要保留折叠状态。
- 不要给 group 顶部加滑动展开/收起的高度动画——session 列表数量变化时高度
  动画会和 flex column 的 layout 抢主线程，引起明显抖动。直接 `v-if` 切换即可。

### 📌 置顶虚拟组（pinned virtual group）

`TaskGroupedList` 在 host/state 分组之上叠加一个**虚拟组**：被 `useSessionPins`
标记为置顶的会话会从原本的 host/state 分组里摘出来，统一渲染在列表最前面，
与 host/state 分组共用同一套折叠语义（`collapsedGroups`，sentinel key
`PINNED_KEY = "__pinned__"`）。视觉上与普通 `.group-header` 保持同族但要能一眼
区分：

```css
.pinned-group .pin-icon { font-size: 11px; }
```

结构规则：

- header 结构与 host/state 分组一致（`role="button" tabindex="0"` + caret +
  `.group-title` + `.group-count`），只是在 caret 和标题之间插入一个
  `<span class="pin-icon" aria-hidden="true">📌</span>`。不要另起一套 header
  样式，靠 emoji + `data-test="pinned-group-header"` 区分即可。
- 置顶组是"摘出"而非"复制"——同一 session 只会出现在置顶组或原分组之一，
  不会同时出现两处；`filteredGroups` 过滤时排除已置顶的 session
  （`!pins.isPinned(s.session_id)`）。
- 置顶组为空（无置顶会话）时整段 `<template v-if="pinnedSessions.length > 0">`
  不渲染，不留空 header。
- 置顶/取消置顶的入口是 `SessionRowMenu`（见下）以及行内右键菜单；不提供拖拽
  排序，`pinnedSessions` 内部排序沿用 `STATE_ORDER`（不含 `closed`，closed+
  pinned 会话靠 fallback index 排到最后）。

### 侧栏内联搜索框

`TaskSidebar.vue` header 里用原生 `<input type="search">` 承担会话过滤，不是
另起自定义 combobox：

```css
.sidebar-search {
  flex: 1 1 auto;
  min-width: 60px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid transparent;
  color: inherit;
  border-radius: 3px;
  padding: 1px 6px;
  font-size: 12px;
  font-family: inherit;
  line-height: 20px;
  margin-right: 6px;
  outline: none;
}
.sidebar-search:focus { border-color: var(--border); background: rgba(255, 255, 255, 0.05); }
.sidebar-search::placeholder { opacity: 0.5; }
```

要点：

- 常态透明边框（`border: 1px solid transparent`），只在 `:focus` 时描边，避免
  header 里一排常驻的输入框边框噪音。
- Chromium/WebKit 会给 `type="search"` 渲染原生 × 清除按钮；**不要**用
  `appearance: none` 关掉它——颜色靠 `color: inherit` 跟随主题即可，关掉清除
  按钮反而要重新实现清空逻辑。
- 组件通过 `defineExpose({ focusSearch })` 暴露聚焦方法（选中已有文本），供
  快捷键从别处跳转聚焦搜索框；新增聚焦入口复用这个方法，不要在别处重复
  `querySelector` 找输入框。
- 过滤逻辑（`sessionMatch.ts`）作用于命令、cwd、标题、host 名等字段，大小写
  不敏感；置顶组和普通分组各自独立过滤（见上一节），搜索词变化不影响
  折叠状态。

### 会话行右键菜单：视口翻转 popover（SessionRowMenu）

`SessionRowMenu.vue` 是右键菜单（目前只有一个"置顶/取消置顶"菜单项），走
`position: fixed` + 视口边缘翻转，不是简单地贴在鼠标坐标：

```css
.session-row-menu {
  position: fixed;
  z-index: 1000;
  min-width: 140px;
  padding: 4px;
  background: var(--menu-bg, #1f1f22);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 6px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
}
```

行为规则：

- 打开时先按鼠标坐标 `(x, y)` 定位，`requestAnimationFrame` 后用真实
  `getBoundingClientRect()` 尺寸判断是否超出右/下边界，超出则翻转锚定到
  `(x - width, y)` / `(x, y - height)`。翻转判定必须等元素挂载出尺寸后再算，
  不能用估算宽高，否则窄侧栏靠右的行右键会把菜单顶到屏幕外。
- 关闭触发点有三处，缺一不可：`Escape` 键、菜单外 `mousedown`、菜单子树
  `focusout`（相关目标不在菜单内时）。后者是为了 Tab 移出菜单或菜单项被移除
  导致 focus 跳到 `<body>` 时也能收起，不依赖鼠标事件。
- 菜单项按钮 `@click.stop`，避免冒泡到外层触发 `mousedown` 关闭逻辑先于
  `click` 触发，导致点击穿透。
- 复用 Dialog / overlay 一节的阴影语言（深阴影 + 6px radius），但不需要
  backdrop——popover 级别的浮层不挡住其余 UI。

## 快捷模板：bar + hotkey + 隐藏开关

桌面 / web / mobile 三端共用 `QuickTemplate { id, label, text, hotkey? }` 模型
（`desktop/frontend/src/lib/templates.ts`，`hotkey` 仅桌面用），UI 也保持一致：

**核心约束**：

- **点击模板按钮 = 直接发送**（`sendInput(text + '\r')`），不弹预览对话框。
  mobile 上靠"防误触模式" banner + `canSend` gate 防止误触发，不需要预览。
- **桌面端 hotkey**：每个模板可配 `Mod+1` / `Alt+Shift+P` 之类字符串。
  `desktop TerminalView` 在 `document` 的 capture 阶段注册 keydown listener，
  解析 hotkey + 判断 `props.focused` 后触发 `sendTemplate(tpl)`。
  只有 focused pane 响应，避免多个 mount 的 TerminalView 同时触发。
- **隐藏 bar**：用户可在设置页关掉整个 template-bar。bridge 加
  `loadHidden / saveHidden`，三端都走 localStorage `atterm.templates.hidden`。
  bar 用 `v-if="!templatesHidden"` 渲染。
- **默认 9 项**：`yes / ok / continue / commit / push / release / 1 / 2 / 3`。



### Template bar

横向滚动按钮条，渲染在共享 `TerminalView.vue` 的 status bar 上方；desktop /
web / Capacitor 都走同一个组件。

- 高度 28px、最小宽度 34px、padding `0 9px`、圆角 7px、字体 `var(--font-mono)`
  12px。
- 颜色：背景 `#11182b` / 边框 `#1e2638` / 文字 `#cbd5e1`；disabled 时
  `opacity: .45` + `color: #64748b`。
- `:disabled` 直接绑 `canSend`（driver + controlMode + 非 view-only），与
  paste 按钮同一 gate；**不要**为模板单独写权限分支。
- 横向溢出 `overflow-x: auto`，按钮 `flex: 0 0 auto`；不要换行。
- **隐藏 scrollbar gutter**：bar 只有 30px 高，scrollbar 占 12-15px 视觉 noisy。
  加 `scrollbar-width: none` + `::-webkit-scrollbar { display: none }`，滚动行
  为保留（鼠标 wheel / 触控板 swipe 都还能用）。
- **鼠标滚轮转横向**：触控板会原生发 `deltaX`，鼠标滚轮在 30px 高条上只有
  `deltaY`。给 `.template-bar` 挂 `@wheel.passive="onTemplateBarWheel"`，把
  `deltaY` 折成 `scrollLeft`（`el.scrollLeft += e.deltaY !== 0 ? e.deltaY : e.deltaX`）。
  passive listener 不 `preventDefault`，xterm 自己的 wheel pipeline 不受影响。

### Settings → Templates tab（desktop）

桌面端 Settings 的 Templates tab 容器结构：

1. 顶部一个 `<label class="show-toggle">`：「显示快捷模板栏」复选框，绑
   `platform.templates.loadHidden/saveHidden`，改后 emit
   `quickTemplates:changed` 让所有打开的 TerminalView 立即同步。
2. 列表 `<ul class="list">`：每行 grid `7rem 1fr 5rem auto`
   = 标签 / 发送文本 / **hotkey** / actions（↑↓编辑删除）。hotkey 列空时
   显示 `—`。
3. 内联编辑行 `<div class="edit-row">`：三个输入（label / text / hotkey）+
   Save / Cancel。hotkey 输入框 placeholder `Mod+1` 之类。
4. 底部 footer：「新增」+「恢复默认」，reset 走二级确认 dialog（同 reset 模式）。

testid 契约（**不要**改，自动测试依赖）：

- `template-show-toggle`（顶部开关）
- `template-row-${id}` / `template-edit-${id}` / `template-delete-${id}`
- `template-edit-label` / `template-edit-text` / `template-edit-hotkey` / `template-edit-save`
- `template-add` / `template-reset` / `template-reset-confirm`

mobile 设置页有独立的模板编辑器（见下方 §移动端设置页），逻辑相同，但
样式适配触屏（行高更高、按钮更大）。web 主 App 复用桌面 Settings 的
Templates tab，读写 `platform.templates`（当前 web bridge 落到 localStorage +
prefs sync）；不要在 `web/src/` 另起第二套模板编辑 UI。

## Web 终端辅助键与文件选择

浏览器和 Capacitor 没有桌面原生菜单，因此 `TerminalView.vue` 在
non-Wails / non-local-PTY capability 下渲染一条紧凑的 terminal aux row。它和
template bar 同属终端工具面，不是 Settings 表单控件：

- 只在 `!platform.caps.wailsBindings && !platform.caps.localPty` 时显示；桌面 Wails
  不显示，web 和 Capacitor 显示。
- 按钮顺序固定为文件类动作在前（图片、文件），后接 `effectiveAuxKeys`（Enter /
  Esc / Tab / `Ctrl-C` / `Ctrl-D` / 方向键等）。文件类动作使用 hidden
  `<input type="file">`，可见按钮只负责触发 picker。
- row 高 30px，按钮高 22px、最小宽度 34px、padding `0 9px`、6px radius、
  12px mono；不要做大块卡片或第二层 toolbar。
- 普通 aux key 的可用条件是 attached + driver + 非 view-only；图片/文件按钮还必须
  `remote_permission === "full"`。UI disabled 只是提示，relay/desktop host 仍必须做
  同样拦截。
- 文案来自 desktop i18n（`terminal.pickImage` / `terminal.pickFile`），中英文同步；
  不在模板里硬编码 "image/file"。
- 文件 picker 不接受目录；图片 picker `accept="image/*"`。选择后 input value 要清空，
  这样同一文件可连续选择两次触发 change。

## 移动端设置页

Capacitor 不再维护 `src/mobile/MobileSettings.vue` / `MobileSetup.vue` 第二套 UI。
移动端设置复用共享 `SettingsDialog.vue`，窄屏时 dialog 全屏，左侧 nav 变成横向
tab，并保留 safe-area padding。

约束：

- 登录、退出、扫码配对在 `SettingsAccount.vue`。扫码按钮调用
  `platform/qrScanner.ts` 的 app-local `AttermQRScanner` plugin，消费流程在
  `SettingsPairingConsume.vue`，成功后 emit `relay:auth-restored` 让 `App.vue`
  重新加载 endpoint / session list。
- 模板和 aux 键仍走 `SettingsTemplates.vue` / `platform.templates` /
  `platform.auxKeys`。改动后 emit `quickTemplates:changed`；Capacitor 也保留
  `mobile:shortcutsChanged` 订阅以兼容旧事件名。
- 退出登录走 `platform.relay.logout()`；Capacitor 实现会清 token 和 account key，
  但保留 url / last_email / saved password 供下一次登录预填。

## 移动端终端输入

移动端终端现在复用共享 `TerminalView.vue`，不再有独立 `MobileTerminal.vue`。

- IME 兼容层只在 xterm textarea 的 capture-phase `input` 上处理
  `inputType === "insertText" && !isComposing && data`，并在 driver + 非 view-only
  时 `sendInput(data)`。composition 路径一律交给 xterm。
- fit 仍由 `ResizeObserver` 驱动；driver 才跑 `safeFit()`，viewer 从 META 的
  `cols/rows` 锁定 `term.resize(cols, rows)`。
- viewer / view-only 继续依赖共享 viewer overlay 和权限 gate；已删除旧移动端
  tap-protect banner，不要重新引入第二套控制模式。

## 移动端键盘可见

iOS WKWebView 默认弹键盘时会在键盘上方画一条 `✓ ↑ ↓` 辅助条
（input accessory view），会把控制面板挡掉。在 `main.capacitor.ts`
启动时调一次：

```ts
if (Capacitor.getPlatform() === 'ios') {
  Keyboard.setAccessoryBarVisible({ isVisible: false }).catch(() => {})
}
```

要求 `@capacitor/keyboard` 装到 `mobile/package.json`（见
conventions.md §Capacitor 8 plugin 注册）。

## Pairing UI

Settings → Pairing 是桌面端为移动端引导的入口：

- 主区块 `<PairingPanel>` 一个按钮 `Generate QR code`，旁边显示剩余有效时间
  （倒计时到 0 自动隐藏 QR + 重新启用按钮）。
- QR canvas 用 `qrcode-svg`（已在依赖里）；不要再引入 raster QR 库。
- 二维码下方一行小字显示 `pair_…` token 的人类可读前缀，方便手动粘贴
  fallback；完整 token 在 copy 按钮里。
- 失败统一文案 "pairing failed, please try again"（不区分 4xx / 5xx）；
  rate-limited (429) 单独显示 "too many requests, wait a minute"。

## 插件与扩展面板

- 插件 UI 放在 `desktop/frontend/src/plugins/<plugin>/`，可以有 plugin-local token（例如 file explorer 的 `--ed-*`），但必须从全局 token 派生。
- 插件设置页嵌入 Settings 时，使用 Settings 的 label/hint/error/button/dropdown 规则，不自建另一套表单风格。
- 右侧面板和浮动面板可以更有插件个性，但要保留 13px 主字号、6px radius、1px border、低饱和 hover 的基础质感。

## 可访问性与交互状态

- 自定义控件必须支持键盘路径；至少覆盖 Tab focus、Enter/Space 激活、Escape 关闭浮层。
- focus-visible 不能移除；使用 `--accent` ring 或 border。
- disabled 状态使用 `opacity: 0.5-0.6` + `cursor: not-allowed/default`，并阻止交互逻辑。
- hover 只增强边框、文字或轻背景；不要改变布局尺寸。
- 动画控制在 120-180ms，使用 transform/opacity/border-color；不要给终端滚动和 PTY 输出路径加昂贵动画。

## 测试与评审门槛

新增或修改组件样式时，至少满足：

1. 组件逻辑用 Vitest 覆盖；纯样式约束可用 `?raw` source test 保护关键选择器和禁用项。
2. 替换原生控件时加回归测试，例如断言 Settings 不包含 `<select>`，并包含 `SelectDropdown`。
3. 自定义下拉、菜单、dialog 要测打开/关闭、键盘操作、disabled 状态。
4. 修改 terminal/pane 尺寸、overlay 或滚动条时跑对应组件测试，并确认不会影响 xterm fit。
5. 提交前至少运行：`cd desktop/frontend && npm test` 与 `npm run build`；如果只改文档，可运行 `git diff --check`。

## Do / Don’t

| Do | Don’t |
|----|-------|
| 复用 `SelectDropdown`、Settings label/hint/error 样式 | 新增裸 `<select>` 或浏览器默认菜单 |
| 用 CSS variables 表达语义 | 在组件里散落主题硬编码色 |
| 保持 32px 控件高、13px 主字号、6px radius | 每个组件自定义一套尺寸系统 |
| 给 custom control 写键盘和 aria | 只满足鼠标点击路径 |
| 用测试锁住容易回归的视觉/交互约束 | 只靠截图或人工记忆维护规范 |
