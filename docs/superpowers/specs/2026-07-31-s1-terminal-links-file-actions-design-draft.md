# S1: 终端链接与文件树路径动作 — 设计

切片来源：`.workflow-plan/terminal-links-and-file-actions/SLICES.md` 的 S1。
本片是一组六项低耦合改动，均在既有组件内闭环，不新增跨组件通信。S2（终端本地路径在文件浏览器中定位）另片处理。

## 背景与问题

来自一组 UI/交互反馈：

1. 终端底部的 toast 提示（如「你现在是控制者」）被底部快捷按钮工具栏遮挡。
2. 文件树不支持用 Ctrl-C 复制选中文件的路径。
3. 文件树右键菜单缺少「复制路径」「终端引用」（把路径发到终端）。
4. 终端里的 URL 链接必须 Ctrl/Cmd+单击才能打开，用户希望纯单击即打开。
5. URL 因宽度被终端软换行折成两行时，无法被识别为一条完整链接。

## 六项改动

### 1. toast 抬高，避开底部工具栏

- 文件：`desktop/frontend/src/App.vue`，`.toast` 样式（约 2159–2164 行）。
- 现状：`bottom: 12px`；底部工具栏 `.bottom-toolbar` 高 `24px`，toast 被盖住。
- 改动：`bottom: 12px` → `bottom: calc(24px + 12px)`（36px），抬到工具栏之上，保留原 12px 视觉间距。
- 说明：工具栏 `:empty` 时隐藏（无插件按钮时），此时 toast 会比原来高 24px；但空工具栏下方本就是空白，抬高不影响观感，且避免动态测量工具栏高度。采用固定 `calc`。

### 2. Ctrl-C 复制文件路径（仅文件树聚焦时）

- 文件：`desktop/frontend/src/plugins/fileExplorer/FileTree.vue`。
- 现状：文件树节点是不可聚焦的 `<div>`，`selectedPath` 仅用于高亮；无键盘事件。
- 改动：
  - 根容器 `.tree-wrap` 加 `tabindex="0"` 使其可聚焦，加 `@keydown` 监听。
  - keydown 判据：普通 Ctrl/Cmd + C（跨平台：mac=metaKey，其他=ctrlKey），且**不含 Shift/Alt**，且 `selectedPath` 非空 → 复制**选中项的绝对路径**到剪贴板。
  - 因为监听挂在 `.tree-wrap` 上，只有焦点在文件树内时才触发，天然满足「仅文件树聚焦时生效」，不影响终端的 Ctrl-C（中断信号）。
  - 复制成功后调用 `context.showToast(t('plugins.fileExplorer.pathCopied'))`（context 由改动 4 引入）。
- 剪贴板写入：优先 `navigator.clipboard.writeText`，失败回退到 `fallbackCopyText`。
- 共享重构：`terminalCopy.ts` 中已有的模块私有 `fallbackCopyText` 提取为**导出**函数，供文件树复用，避免复制粘贴。`terminalCopy.ts` 自身行为不变。

### 3. 右键菜单增加「复制路径」「终端引用」

- 文件：`FileTree.vue` 菜单模板（约 491–494 行）与 `onMenuAction`（约 68–86 行）。
- 菜单从 4 项变 6 项，顺序：**新建文件 / 新建文件夹 / 重命名 / 复制路径 / 终端引用 / 删除**（危险项「删除」保留在最末）。
- `onMenuAction` 的 action 联合类型扩展为 `"newFile" | "newFolder" | "rename" | "delete" | "copyPath" | "sendToTerminal"`。
  - `copyPath` → 复制 `node.path`（绝对路径）到剪贴板 → toast（复用改动 2 的复制工具）。
  - `sendToTerminal` → `context.send(quoteForShell(node.path))`，**不自动回车**（`send` 只填入命令行文本）。
- 智能转义：新增纯函数文件 `desktop/frontend/src/plugins/fileExplorer/shellQuote.ts`。
  - `quoteForShell(path: string): string`：路径含空格或 shell 特殊字符（空格、`" ' $ \` \ ( ) ; & | < > * ? [ ] { } ~ # ! 换行` 等）时，用单引号包裹并把内部单引号转义为 `'\''`；否则原样返回。
  - 独立文件便于单测。

### 4. FileTree 接入 PluginContext + i18n

- 问题：改动 2/3 需要 `context.send` 和 `context.showToast`，但 `FileTree` 当前未拿到 context。
- 改动：
  - `FileExplorer.vue`（约 230 行 `<FileTree>` 处）增加 `:context="context"`（FileExplorer 已持有 `props.context`）。
  - `FileTree.vue` 的 `defineProps` 增加 `context: PluginContext`（从 `../types` 导入类型）。
  - 复制成功、终端引用发送后的用户提示统一走 `context.showToast`。
- i18n：`desktop/frontend/src/i18n/messages/zh-CN.ts` 与 `en.ts` 的 `plugins.fileExplorer` 各新增：
  - `copyPath`：「复制路径」/「Copy Path」
  - `sendToTerminal`：「终端引用」/「Send to Terminal」
  - `pathCopied`：「路径已复制」/「Path copied」
- 远程会话：文件树在远程 SSH 会话下，`root` 与 `send` 均指向同一远程终端，语义天然一致；复制到剪贴板落在本机（符合预期）。无需特殊处理。

### 5. 终端 URL 纯单击打开（拖选拦截）

- 文件：`desktop/frontend/src/composables/useTerminalLinkProvider.ts`、`desktop/frontend/src/lib/terminalLinks.ts`。
- 现状：`activate` 回调（约 78 行）用 `isModClickEvent` 判断，仅当按了 Ctrl/Cmd 才打开；`isModClickEvent` 只此一处调用。
- 目标：纯单击即打开；拖选（想复制链接文本）时不打开；Ctrl/Cmd+单击仍可打开。
- 改动：
  - 在 link provider 内记录最近一次 `mousedown` 的屏幕坐标：给 `term.element` 挂一个 mousedown 监听，由返回的 `dispose()` 清理（与 registerLinkProvider 的 disposer 合并返回）。
  - 用新判据函数替换 `isModClickEvent`（原函数仅此一处用，直接改造/改名为 `shouldActivateLink`）：
    1. 若 `event.shiftKey || event.altKey` → 不打开。
    2. 计算 mousedown→click 的位移；若超过阈值（`5px`）→ 判定为拖选，不打开。
    3. 否则（纯单击 或 Ctrl/Cmd+单击）→ 打开。
  - `decorations: { underline: true, pointerCursor: true }` 保持不变（链接的下划线 + 手型光标视觉暗示与「可单击」一致）。
- 测试：`useTerminalLinkProvider.test.ts` 原「无修饰键不打开」用例改为：无修饰键 + 无拖动 → 打开；mousedown→click 位移超阈值 → 不打开；shift/alt → 不打开。

### 6. 软换行链接跨行识别

- 文件：`terminalLinks.ts`、`useTerminalLinkProvider.ts`。
- 现状：`provideLinks(y)` 只取单个物理行 `getLine(y-1)`，`detectLinks` 只在单行内匹配；URL 被软换行折成两行时识别成两段残缺片段。
- 术语区分：
  - **软换行**：URL 太长被终端自动折行，xterm 的 `IBufferLine.isWrapped === true`（xterm 5.3 提供），逻辑上同一行 → **应拼接**。
  - **硬换行**：内容本身含换行符，`isWrapped === false` → **不拼接**。
- 核心难点：xterm 的 link `range` 按**物理行 y**描述，必须把逻辑行内链接的字符区间映射回它横跨的每个物理行，为每个物理行片段各生成一个 ILink（共享同一 activate，点哪段都打开完整 URL）。
- 方案（改造 `useTerminalLinkProvider.ts` 的 `provideLinks` + 扩展 `terminalLinks.ts`）：
  1. **每个物理行**的 `provideLinks(y)` 里：先定位它所属逻辑行的**首行**（向上回溯：当前行 `isWrapped` 为 true 则继续上溯，直到某行自身 `isWrapped=false`；回溯上限 ~50 行防御异常缓冲）。
  2. 从首行起顺序拼接所有软换行物理行（下一行 `isWrapped=true` 即续行）的可见文本，得到**逻辑行文本**，并记录每个字符 → `{ y, cellX }`（物理行 + cell 列）。为此从 `mapBufferLineCells`（单行）扩展出 `mapWrappedLogicalLine(buffer, firstY, cols, maxRows)`。
  3. `detectLinks` 在逻辑行文本上跑（逻辑不变），得到跨行的完整链接 match。
  4. 对每个 match，按其字符区间横跨的物理行**切分**，每段用第 2 步映射还原出 `{ startX, endX, y }`，各生成一个 ILink，`text` 均为完整链接，`activate` 共享。
  5. **只返回与当前 `provideLinks(y)` 的 y 相交的片段**——这样每个物理行各自返回自己那段，hover 下划线与点击都正常，且天然去重（不会因逻辑行首多次展开而重复产出）。
  6. `activate` 复用改动 5 的判据。
- 支持逻辑行跨 3+ 行（超长 URL），按行数循环处理，不限两行。
- 测试：`terminalLinks.test.ts` 增用例——构造软换行 URL（两/三物理行 + `isWrapped` 标记），断言：(a) 检测出一条完整链接；(b) 每个物理行返回一个覆盖对应片段的 ILink；(c) 硬换行（`isWrapped=false`）不拼接。

## 影响的文件

| 改动 | 文件 |
|------|------|
| 1 | `App.vue` |
| 2 | `FileTree.vue`、`terminalCopy.ts`（导出 fallbackCopyText） |
| 3 | `FileTree.vue`、新增 `shellQuote.ts` |
| 4 | `FileExplorer.vue`、`FileTree.vue`、`i18n/messages/zh-CN.ts`、`i18n/messages/en.ts` |
| 5 | `useTerminalLinkProvider.ts`、`terminalLinks.ts` |
| 6 | `useTerminalLinkProvider.ts`、`terminalLinks.ts` |

## 测试策略

- 纯函数优先单测：`shellQuote.ts`（转义各种特殊字符 + 无害路径原样）、`terminalLinks.ts`（软换行拼接 + 硬换行不拼 + 单击判据）。
- `terminalCopy.ts` 提取的 `fallbackCopyText` 保持既有测试通过。
- `useTerminalLinkProvider.test.ts` 更新单击/拖选/软换行用例。
- 文件树复制/菜单动作：沿用现有 `FileTree.test.ts` 的组件测试风格补用例（选中 + Ctrl-C → 剪贴板写入被调用；菜单 copyPath/sendToTerminal → context.send/showToast 被调用）。

## 非目标（留给 S2）

- 相对路径识别、终端本地路径在文件浏览器中定位打开、启用插件/展开面板、`FileTree.revealPath`。
