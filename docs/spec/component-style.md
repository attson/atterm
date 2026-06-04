# 组件样式规范

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

## 快捷模板：bar + preview dialog

桌面 / web / mobile 三端共用 `QuickTemplate { id, label, text }` 模型
（`desktop/frontend/src/lib/templates.ts`），UI 也保持一致：

### Template bar

横向滚动按钮条，渲染在 desktop TerminalView / web TerminalView 的 status
bar 上方，以及 mobile MobileTerminal 替换原 `quickbar`。

- 高度 28px、最小宽度 34px、padding `0 9px`、圆角 7px、字体 `var(--font-mono)`
  12px。
- 颜色：背景 `#11182b` / 边框 `#1e2638` / 文字 `#cbd5e1`；disabled 时
  `opacity: .45` + `color: #64748b`。
- `:disabled` 直接绑 `canSend`（driver + controlMode + 非 view-only），与
  paste 按钮同一 gate；**不要**为模板单独写权限分支。
- 横向溢出 `overflow-x: auto`，按钮 `flex: 0 0 auto`；不要换行。

### Preview dialog

`TemplatePreviewDialog.vue`（桌面 / 移动共享）。点击模板按钮 → 弹出预览 →
Enter / 点击 Send 触发 `sendInput(text + '\r')`。

约束：

- backdrop 用 dialog overlay 规则（fixed inset 0、黑色透明度 0.5-0.6）；
  z-index 高于 settings dialog，避免 settings 打开时模板预览被盖。
- 卡片宽度约 280px（mobile）/ 360px（desktop）；`max-width: calc(100vw - 32px)`。
- `<pre class="preview">` 渲染待发送文本，禁止 word wrap（mono 字体，预览
  内容应当与实际发到 PTY 的字节一致；wrap 会误导）。
- footer 两个 button：Cancel（次要）+ Send（primary `--accent`），后者 `autofocus`。
- 键盘：Enter = confirm，Escape = cancel；点击 backdrop = cancel。
- testid 契约（用于自动测试，**不要**改）：`template-preview` /
  `template-preview-confirm` / `template-preview-cancel` / `template-btn-${id}`。

### Settings → Templates tab（desktop only）

桌面端 Settings 增加 Templates tab（紧跟 Diagnostics 之后），遵循 Settings
组件规范的 label/hint/error/button 样式。增删改行 inline 编辑（替换为两个文
本输入 + Save/Cancel），重置走二级确认对话框。debounce 保存 300ms，避免
快速键入触发频繁 disk write。

mobile / web 仅渲染默认 10 项 + 用户自己 desktop 编辑过的列表（各端
localStorage / config 独立），不提供编辑界面。

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
