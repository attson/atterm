# S1: 终端链接与文件树路径动作 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 toast 遮挡，给文件树加 Ctrl-C 复制路径/右键「复制路径」「终端引用」，并让终端 URL 支持纯单击打开与软换行识别。

**Architecture:** 六项低耦合改动，全部在既有前端组件内闭环。文件树通过既有 `PluginContext`（可选 prop，向后兼容现有测试）调用 `send`/`showToast`。终端链接改造 `useTerminalLinkProvider` 的 `provideLinks`（软换行跨行拼接）与 `activate`（单击判据 + 拖选拦截）。

**Tech Stack:** Vue 3 `<script setup>`、TypeScript、xterm.js 5.3、Vitest、@vue/test-utils。工作目录 `desktop/frontend`。

**测试命令：** 单文件 `cd desktop/frontend && npx vitest run <path>`；全量 `npm test`；类型检查 `npm run build`（`vue-tsc --noEmit`）。

---

## 文件结构

| 文件 | 责任 | 新建/修改 |
|------|------|-----------|
| `src/App.vue` | toast 定位样式 | 修改 |
| `src/lib/terminalCopy.ts` | 导出 `fallbackCopyText` 供复用 | 修改 |
| `src/plugins/fileExplorer/shellQuote.ts` | shell 路径转义纯函数 | 新建 |
| `src/plugins/fileExplorer/shellQuote.test.ts` | 转义单测 | 新建 |
| `src/plugins/fileExplorer/FileTree.vue` | tabindex+keydown 复制、右键菜单两项、可选 context | 修改 |
| `src/plugins/fileExplorer/FileTree.test.ts` | 复制/菜单动作测试 | 修改 |
| `src/plugins/fileExplorer/FileExplorer.vue` | 把 context 透传给 FileTree | 修改 |
| `src/i18n/messages/zh-CN.ts` / `en.ts` | 新增三条文案 | 修改 |
| `src/lib/terminalLinks.ts` | 单击判据 `shouldActivateLink`、软换行逻辑行映射 | 修改 |
| `src/lib/terminalLinks.test.ts` | 判据 + 软换行单测 | 修改 |
| `src/composables/useTerminalLinkProvider.ts` | provideLinks 跨行、activate 用新判据 + mousedown 追踪 | 修改 |
| `src/composables/useTerminalLinkProvider.test.ts` | 单击/拖选/软换行测试 | 修改 |

---

## Task 1: toast 抬高避开底部工具栏

**Files:**
- Modify: `src/App.vue`（`.toast` 样式块，约 2159–2164 行）

- [ ] **Step 1: 修改 toast 的 bottom**

把 `.toast` 的 `bottom: 12px;` 改为 `bottom: calc(24px + 12px);`。定位这段：

```css
.toast {
  position: absolute; bottom: 12px; left: 50%; transform: translateX(-50%);
```

改为：

```css
.toast {
  position: absolute; bottom: calc(24px + 12px); left: 50%; transform: translateX(-50%);
```

（`24px` 是 `.bottom-toolbar` 的高度，`12px` 保留原视觉间距。）

- [ ] **Step 2: 类型检查**

Run: `cd desktop/frontend && npm run build`
Expected: 构建通过（无 TS 错误）。若耗时过长可跳过，样式改动不影响类型。

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/src/App.vue
git commit -m "fix(terminal): 提示气泡抬高避开底部工具栏遮挡"
```

---

## Task 2: 导出 fallbackCopyText 供文件树复用

**Files:**
- Modify: `src/lib/terminalCopy.ts:20`（`fallbackCopyText` 加 `export`）

- [ ] **Step 1: 给 fallbackCopyText 加 export**

把 `src/lib/terminalCopy.ts` 第 20 行：

```typescript
function fallbackCopyText(text: string): boolean {
```

改为：

```typescript
export function fallbackCopyText(text: string): boolean {
```

其余不动（函数体、`copyTerminalSelection` 对它的调用都不变）。

- [ ] **Step 2: 跑现有测试确认不回归**

Run: `cd desktop/frontend && npx vitest run src/lib/terminalCopy.test.ts`
Expected: PASS（所有既有用例通过）。

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/src/lib/terminalCopy.ts
git commit -m "refactor(terminal): 导出 fallbackCopyText 供文件树复用"
```

---

## Task 3: shellQuote 转义纯函数（TDD）

**Files:**
- Create: `src/plugins/fileExplorer/shellQuote.ts`
- Test: `src/plugins/fileExplorer/shellQuote.test.ts`

- [ ] **Step 1: 写失败测试**

创建 `src/plugins/fileExplorer/shellQuote.test.ts`：

```typescript
import { describe, expect, it } from "vitest";
import { quoteForShell } from "./shellQuote";

describe("quoteForShell", () => {
  it("returns a simple path unchanged", () => {
    expect(quoteForShell("/home/user/file.txt")).toBe("/home/user/file.txt");
  });

  it("returns a path with dots/dashes/underscores unchanged", () => {
    expect(quoteForShell("/a/b-c_d.e.txt")).toBe("/a/b-c_d.e.txt");
  });

  it("single-quotes a path containing spaces", () => {
    expect(quoteForShell("/a b/c.txt")).toBe("'/a b/c.txt'");
  });

  it("single-quotes a path with shell metacharacters", () => {
    expect(quoteForShell("/a$(b)/c.txt")).toBe("'/a$(b)/c.txt'");
  });

  it("escapes embedded single quotes", () => {
    expect(quoteForShell("/a'b/c.txt")).toBe("'/a'\\''b/c.txt'");
  });

  it("quotes a path with a tilde in the middle", () => {
    expect(quoteForShell("/a~b/c")).toBe("'/a~b/c'");
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/shellQuote.test.ts`
Expected: FAIL（`quoteForShell` 未定义 / 模块不存在）。

- [ ] **Step 3: 写实现**

创建 `src/plugins/fileExplorer/shellQuote.ts`：

```typescript
// Characters that are safe unquoted in a POSIX shell command line. Anything
// outside this set (spaces, $, quotes, glob chars, ~, etc.) forces quoting so
// the path reaches the shell as a single literal argument.
const SAFE = /^[A-Za-z0-9_./@:+,%=-]+$/;

/**
 * Quote a filesystem path for safe insertion into a POSIX shell command line.
 * Safe paths pass through unchanged; anything else is wrapped in single quotes
 * with embedded single quotes escaped as '\'' (close, escaped quote, reopen).
 */
export function quoteForShell(path: string): string {
  if (path === "") return "''";
  if (SAFE.test(path)) return path;
  return "'" + path.replace(/'/g, "'\\''") + "'";
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/shellQuote.test.ts`
Expected: PASS（6 个用例全过）。

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/shellQuote.ts desktop/frontend/src/plugins/fileExplorer/shellQuote.test.ts
git commit -m "feat(fileExplorer): 新增 quoteForShell 路径转义工具"
```

---

## Task 4: i18n 新增三条文案

**Files:**
- Modify: `src/i18n/messages/zh-CN.ts`（`plugins.fileExplorer`，约 712 行 `delete` 附近）
- Modify: `src/i18n/messages/en.ts`（同一块）

- [ ] **Step 1: zh-CN 新增文案**

在 `src/i18n/messages/zh-CN.ts` 的 `fileExplorer` 块里，`delete: "删除",` 之后新增三行：

```typescript
      delete: "删除",
      copyPath: "复制路径",
      sendToTerminal: "终端引用",
      pathCopied: "路径已复制",
```

- [ ] **Step 2: en 新增对应文案**

在 `src/i18n/messages/en.ts` 的 `fileExplorer` 块里，`delete: "Delete",` 之后新增：

```typescript
      delete: "Delete",
      copyPath: "Copy Path",
      sendToTerminal: "Send to Terminal",
      pathCopied: "Path copied",
```

（注意保持与该文件中 `delete` 现有值一致；若现值不同，只在其后追加三行。）

- [ ] **Step 3: 跑 i18n 测试确认键齐全**

Run: `cd desktop/frontend && npx vitest run src/i18n/i18n.test.ts`
Expected: PASS（zh-CN 与 en 键集一致，无缺失）。

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/i18n/messages/zh-CN.ts desktop/frontend/src/i18n/messages/en.ts
git commit -m "i18n(fileExplorer): 新增复制路径/终端引用/路径已复制文案"
```

---

## Task 5: FileTree 接入可选 context + FileExplorer 透传

**Files:**
- Modify: `src/plugins/fileExplorer/FileTree.vue`（`defineProps`，约 26–31 行）
- Modify: `src/plugins/fileExplorer/FileExplorer.vue`（`<FileTree>` 标签，约 230 行）

- [ ] **Step 1: FileTree 导入 PluginContext 类型并加可选 prop**

在 `src/plugins/fileExplorer/FileTree.vue` `<script setup>` 顶部的 import 区加：

```typescript
import type { PluginContext } from "../types";
```

把 `defineProps`（约 26–31 行）：

```typescript
const props = defineProps<{
  fs: FileSystemBridge;
  root: string;
  showHidden: boolean;
  searchQuery?: string;
}>();
```

改为：

```typescript
const props = defineProps<{
  fs: FileSystemBridge;
  root: string;
  showHidden: boolean;
  searchQuery?: string;
  context?: PluginContext;
}>();
```

（可选 prop 保证现有 9 处不传 context 的 `mount(FileTree, ...)` 测试仍通过。）

- [ ] **Step 2: FileExplorer 透传 context**

在 `src/plugins/fileExplorer/FileExplorer.vue` 的 `<FileTree ...>`（约 230–239 行）加一行 `:context="context"`：

```html
          <FileTree
            v-if="root && fs"
            :key="fsGeneration"
            :fs="fs"
            :root="root"
            :show-hidden="showHidden"
            :search-query="fileNameSearch"
            :context="context"
            @file-clicked="onFileClick"
            @file-double-clicked="onFileDoubleClick"
          />
```

- [ ] **Step 3: 类型检查 + 现有 FileTree 测试**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/FileTree.test.ts`
Expected: PASS（现有用例不受影响）。

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/FileTree.vue desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue
git commit -m "feat(fileExplorer): FileTree 接入可选 PluginContext"
```

---

## Task 6: 右键菜单「复制路径」「终端引用」（TDD）

**Files:**
- Modify: `src/plugins/fileExplorer/FileTree.vue`（`onMenuAction` 约 68–86 行、菜单模板 491–494 行、新增复制辅助）
- Modify: `src/plugins/fileExplorer/FileTree.test.ts`

- [ ] **Step 1: 写失败测试**

在 `src/plugins/fileExplorer/FileTree.test.ts` 末尾（最后一个 `describe` 内或新增 describe）追加。先在文件顶部确保导入了 `PluginContext` 构造所需的 `ref`/`computed`：

```typescript
import { ref, computed } from "vue";
import type { PluginContext } from "../types";
```

新增测试（构造一个 stub context，右键 README.md 触发菜单动作）：

```typescript
function makeStubContext(overrides: Partial<PluginContext> = {}): PluginContext {
  return {
    activePane: ref(null),
    activeSessionId: computed(() => null),
    activeIsRemote: computed(() => false),
    activeSessionConnection: computed(() => null),
    activeEndpoint: computed(() => null),
    activeCwd: computed(() => "/proj"),
    terminalThemeId: computed(() => "classic"),
    send: vi.fn(),
    showToast: vi.fn(),
    ...overrides,
  };
}

describe("FileTree — path actions", () => {
  it("context menu 'send to terminal' calls context.send with the node path", async () => {
    const context = makeStubContext();
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false, context } });
    await flushPromises();
    const readme = w.findAll(".node").find((n) => n.text().includes("README.md"))!;
    await readme.trigger("contextmenu");
    const sendBtn = w.find('[data-test="menu-send-to-terminal"]');
    expect(sendBtn.exists()).toBe(true);
    await sendBtn.trigger("click");
    expect(context.send).toHaveBeenCalledWith("/proj/README.md");
  });

  it("context menu 'copy path' writes the node path and toasts", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    const context = makeStubContext();
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false, context } });
    await flushPromises();
    const readme = w.findAll(".node").find((n) => n.text().includes("README.md"))!;
    await readme.trigger("contextmenu");
    await w.find('[data-test="menu-copy-path"]').trigger("click");
    await flushPromises();
    expect(writeText).toHaveBeenCalledWith("/proj/README.md");
    expect(context.showToast).toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/FileTree.test.ts -t "path actions"`
Expected: FAIL（菜单里没有 `menu-copy-path` / `menu-send-to-terminal` 按钮）。

- [ ] **Step 3: 实现 —— 导入工具 + 复制辅助 + 扩展 onMenuAction**

在 `FileTree.vue` `<script setup>` import 区新增：

```typescript
import { fallbackCopyText } from "../../lib/terminalCopy";
import { quoteForShell } from "./shellQuote";
```

在 `<script setup>` 内新增一个复制辅助函数（放在 `onMenuAction` 上方）：

```typescript
async function copyPathToClipboard(path: string) {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(path);
    } else if (!fallbackCopyText(path)) {
      return;
    }
    props.context?.showToast?.(t("plugins.fileExplorer.pathCopied"));
  } catch (err) {
    console.warn("file-explorer: copy path failed", err);
  }
}
```

把 `onMenuAction` 的签名与分支扩展（约 68–86 行）：

```typescript
async function onMenuAction(
  action: "newFile" | "newFolder" | "rename" | "delete" | "copyPath" | "sendToTerminal",
) {
  const anchor = menu.value;
  menu.value = null;
  if (!anchor) return;
  const node = anchor.node;
  if (action === "copyPath") {
    await copyPathToClipboard(node.path);
    return;
  }
  if (action === "sendToTerminal") {
    props.context?.send?.(quoteForShell(node.path));
    return;
  }
  if (action === "newFile" || action === "newFolder") {
    const parentPath = node.isDir ? node.path : parentDir(node.path);
    const parentLevel = node.isDir ? anchor.level + 1 : anchor.level;
    inlineIntent.value = { kind: action, parentPath, parentLevel };
    return;
  }
  if (action === "rename") {
    inlineIntent.value = { kind: "rename", node, level: anchor.level };
    return;
  }
  if (action === "delete") {
    deleteConfirm.value = { node, mode: anchor.shift ? "hard" : "trash" };
  }
}
```

- [ ] **Step 4: 实现 —— 菜单模板加两项**

在 `FileTree.vue` 模板的 `.ctx-menu`（491–494 行）里，`rename` 与 `delete` 之间插入两项：

```html
      <button data-test="menu-new-file" @click="onMenuAction('newFile')">{{ t("plugins.fileExplorer.newFile") }}</button>
      <button data-test="menu-new-folder" @click="onMenuAction('newFolder')">{{ t("plugins.fileExplorer.newFolder") }}</button>
      <button data-test="menu-rename" @click="onMenuAction('rename')">{{ t("plugins.fileExplorer.rename") }}</button>
      <button data-test="menu-copy-path" @click="onMenuAction('copyPath')">{{ t("plugins.fileExplorer.copyPath") }}</button>
      <button data-test="menu-send-to-terminal" @click="onMenuAction('sendToTerminal')">{{ t("plugins.fileExplorer.sendToTerminal") }}</button>
      <button data-test="menu-delete" @click="onMenuAction('delete')">{{ t("plugins.fileExplorer.delete") }}</button>
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/FileTree.test.ts`
Expected: PASS（新增 path actions 用例 + 现有用例全过）。

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/FileTree.vue desktop/frontend/src/plugins/fileExplorer/FileTree.test.ts
git commit -m "feat(fileExplorer): 右键菜单加复制路径与终端引用"
```

---

## Task 7: Ctrl-C 复制选中文件路径（TDD）

**Files:**
- Modify: `src/plugins/fileExplorer/FileTree.vue`（`.tree-wrap` 加 tabindex+keydown，约 446 行；新增 `onKeydown`）
- Modify: `src/plugins/fileExplorer/FileTree.test.ts`

- [ ] **Step 1: 写失败测试**

在 `FileTree.test.ts` 的 "path actions" describe 内追加：

```typescript
  it("Ctrl-C copies the selected file path to the clipboard", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    const context = makeStubContext();
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false, context } });
    await flushPromises();
    const readme = w.findAll(".node").find((n) => n.text().includes("README.md"))!;
    await readme.trigger("click"); // sets selectedPath
    await w.find(".tree-wrap").trigger("keydown", { key: "c", ctrlKey: true });
    await flushPromises();
    expect(writeText).toHaveBeenCalledWith("/proj/README.md");
  });

  it("Ctrl-C does nothing when no file is selected", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    const context = makeStubContext();
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false, context } });
    await flushPromises();
    await w.find(".tree-wrap").trigger("keydown", { key: "c", ctrlKey: true });
    await flushPromises();
    expect(writeText).not.toHaveBeenCalled();
  });
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/FileTree.test.ts -t "Ctrl-C"`
Expected: FAIL（`.tree-wrap` 无 keydown 处理，writeText 未被调用）。

- [ ] **Step 3: 实现 —— 新增 onKeydown**

在 `FileTree.vue` `<script setup>` 内（`copyPathToClipboard` 下方）新增：

```typescript
function onTreeKeydown(e: KeyboardEvent) {
  const isCopyKey = e.key === "c" || e.key === "C";
  if (!isCopyKey || e.altKey || e.shiftKey) return;
  const mod = e.metaKey || e.ctrlKey;
  if (!mod) return;
  if (!selectedPath.value) return;
  e.preventDefault();
  void copyPathToClipboard(selectedPath.value);
}
```

- [ ] **Step 4: 实现 —— 容器加 tabindex + keydown**

把模板根 `.tree-wrap`（约 446 行）：

```html
  <div class="tree-wrap" @click="closeMenu">
```

改为：

```html
  <div class="tree-wrap" tabindex="0" @click="closeMenu" @keydown="onTreeKeydown">
```

为避免 focus 出现可见描边干扰，`<style scoped>` 里给 `.tree-wrap` 加：

```css
.tree-wrap:focus { outline: none; }
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/FileTree.test.ts`
Expected: PASS（Ctrl-C 两个用例 + 其余全过）。

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/FileTree.vue desktop/frontend/src/plugins/fileExplorer/FileTree.test.ts
git commit -m "feat(fileExplorer): 文件树聚焦时 Ctrl-C 复制选中路径"
```

---

## Task 8: 终端链接单击判据 shouldActivateLink（TDD）

**Files:**
- Modify: `src/lib/terminalLinks.ts`（替换 `isModClickEvent`，约 171–174 行）
- Modify: `src/lib/terminalLinks.test.ts`

**判据说明：** `shouldActivateLink(e, downPos, isMac)` —— shift/alt 一律不打开；有 mousedown 起点且位移 > 5px 判为拖选不打开；否则打开（纯单击或 Ctrl/Cmd 单击均放行）。`downPos` 为 `null` 时（无按下记录）当作无拖动。

- [ ] **Step 1: 写失败测试**

在 `src/lib/terminalLinks.test.ts` 顶部 import 增加 `shouldActivateLink`，并把对 `isModClickEvent` 的旧断言替换为新 describe：

```typescript
// import 行改为包含 shouldActivateLink（移除 isModClickEvent 若不再用）
import {
  cellInLink,
  detectLinks,
  shouldActivateLink,
  linkCellRange,
  mapBufferLineCells,
  normalizeForOpen,
  type BufferLineLike,
  type LinkMatch,
} from "./terminalLinks";

describe("shouldActivateLink", () => {
  const ev = (o: Partial<MouseEvent>) =>
    ({ shiftKey: false, altKey: false, ctrlKey: false, metaKey: false, clientX: 0, clientY: 0, ...o }) as MouseEvent;

  it("opens on a plain click with no drag", () => {
    expect(shouldActivateLink(ev({ clientX: 10, clientY: 10 }), { x: 10, y: 10 }, false)).toBe(true);
  });

  it("opens on a plain click when there is no mousedown record", () => {
    expect(shouldActivateLink(ev({}), null, false)).toBe(true);
  });

  it("does not open when the pointer dragged more than the threshold", () => {
    expect(shouldActivateLink(ev({ clientX: 40, clientY: 10 }), { x: 10, y: 10 }, false)).toBe(false);
  });

  it("opens on a small sub-threshold movement", () => {
    expect(shouldActivateLink(ev({ clientX: 12, clientY: 11 }), { x: 10, y: 10 }, false)).toBe(true);
  });

  it("does not open when shift is held", () => {
    expect(shouldActivateLink(ev({ shiftKey: true }), null, false)).toBe(false);
  });

  it("does not open when alt is held", () => {
    expect(shouldActivateLink(ev({ altKey: true }), null, false)).toBe(false);
  });

  it("opens on ctrl-click (non-mac)", () => {
    expect(shouldActivateLink(ev({ ctrlKey: true }), null, false)).toBe(true);
  });
});
```

同时删除文件中原有的 `describe("isModClickEvent", ...)` 整块（若存在）。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd desktop/frontend && npx vitest run src/lib/terminalLinks.test.ts -t "shouldActivateLink"`
Expected: FAIL（`shouldActivateLink` 未定义）。

- [ ] **Step 3: 实现 —— 替换 isModClickEvent**

把 `src/lib/terminalLinks.ts` 的 `isModClickEvent`（171–174 行）整体替换为：

```typescript
export interface PointerPos {
  x: number;
  y: number;
}

const DRAG_THRESHOLD_PX = 5;

/**
 * Decide whether a click on a detected link should open it. A plain click opens
 * the link (single-click to open); shift/alt clicks never open (reserved for
 * selection); and a click preceded by a mousedown that moved more than
 * DRAG_THRESHOLD_PX is treated as a text drag-select, not an activation. isMac
 * is currently unused but kept for signature stability / future per-OS tweaks.
 */
export function shouldActivateLink(
  e: Pick<MouseEvent, "shiftKey" | "altKey" | "clientX" | "clientY">,
  downPos: PointerPos | null,
  _isMac: boolean,
): boolean {
  if (e.shiftKey || e.altKey) return false;
  if (downPos) {
    const dx = e.clientX - downPos.x;
    const dy = e.clientY - downPos.y;
    if (Math.hypot(dx, dy) > DRAG_THRESHOLD_PX) return false;
  }
  return true;
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd desktop/frontend && npx vitest run src/lib/terminalLinks.test.ts`
Expected: PASS（shouldActivateLink 全过；detectLinks 等既有用例不受影响）。

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/terminalLinks.ts desktop/frontend/src/lib/terminalLinks.test.ts
git commit -m "feat(terminal): 链接单击判据 shouldActivateLink 替换修饰键判据"
```

---

## Task 9: useTerminalLinkProvider 接入单击判据 + mousedown 追踪（TDD）

**Files:**
- Modify: `src/composables/useTerminalLinkProvider.ts`
- Modify: `src/composables/useTerminalLinkProvider.test.ts`

- [ ] **Step 1: 更新测试 —— 单击打开 / 拖选不打开**

在 `useTerminalLinkProvider.test.ts` 里，`makeFakeTerm` 返回的 `term` 需要一个 `element`（挂 mousedown 用）。给 fake term 增加 element 支持：在 `makeFakeTerm` 的返回 `term` 对象里加：

```typescript
      element: (() => {
        const listeners: Record<string, ((e: any) => void)[]> = {};
        return {
          addEventListener: (type: string, cb: (e: any) => void) => {
            (listeners[type] ||= []).push(cb);
          },
          removeEventListener: (type: string, cb: (e: any) => void) => {
            listeners[type] = (listeners[type] || []).filter((f) => f !== cb);
          },
          __emit: (type: string, e: any) => (listeners[type] || []).forEach((f) => f(e)),
        };
      })(),
```

并在返回对象里暴露 `term` 的 element 便于测试触发（`getEl: () => (f.term as any).element`）——或直接在用例里通过 `(f.term as any).element.__emit(...)` 访问。

把原「activate ignores click without modifier」用例替换为：

```typescript
  it("activate opens on a plain click (no modifier, no drag)", async () => {
    const f = makeFakeTerm("https://x.test");
    const openURL = vi.fn().mockResolvedValue(undefined);
    useTerminalLinkProvider({ term: f.term, isMac: true, getHomeDir: () => "", openURL, onError: vi.fn() });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    (f.term as any).element.__emit("mousedown", { clientX: 5, clientY: 5 });
    await links![0].activate(Object.assign(new MouseEvent("click"), { clientX: 5, clientY: 5 }), "https://x.test");
    expect(openURL).toHaveBeenCalledWith("https://x.test");
  });

  it("activate does not open when the click followed a drag", async () => {
    const f = makeFakeTerm("https://x.test");
    const openURL = vi.fn().mockResolvedValue(undefined);
    useTerminalLinkProvider({ term: f.term, isMac: true, getHomeDir: () => "", openURL, onError: vi.fn() });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    (f.term as any).element.__emit("mousedown", { clientX: 5, clientY: 5 });
    await links![0].activate(Object.assign(new MouseEvent("click"), { clientX: 60, clientY: 5 }), "https://x.test");
    expect(openURL).not.toHaveBeenCalled();
  });
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd desktop/frontend && npx vitest run src/composables/useTerminalLinkProvider.test.ts -t "activate"`
Expected: FAIL（当前 activate 用旧 `isModClickEvent`，纯单击不打开）。

- [ ] **Step 3: 实现 —— 追踪 mousedown + 用 shouldActivateLink**

改 `src/composables/useTerminalLinkProvider.ts`：

import 区把 `isModClickEvent` 换成 `shouldActivateLink`、`PointerPos`：

```typescript
import {
  detectLinks,
  shouldActivateLink,
  type PointerPos,
  linkCellRange,
  mapBufferLineCells,
  normalizeForOpen,
  type LinkMatch,
} from "../lib/terminalLinks";
```

在 `useTerminalLinkProvider` 函数体内、`provider` 定义前，追踪最近一次 mousedown：

```typescript
  let lastDownPos: PointerPos | null = null;
  const onMouseDown = (e: MouseEvent) => {
    lastDownPos = { x: e.clientX, y: e.clientY };
  };
  const el = (term as unknown as { element?: HTMLElement }).element;
  el?.addEventListener("mousedown", onMouseDown);
```

把 `toILink` 调用处传入一个读取 `lastDownPos` 的取值函数（把 `isMac` 之外再传 `() => lastDownPos`）。修改 `toILink` 签名与 activate：

在 `provideLinks` 里 `toILink(...)` 调用改为：

```typescript
      callback(
        matches.map((m) =>
          toILink(m, y, cellStart, term, isMac, () => lastDownPos, getHomeDir, openURL, onError),
        ),
      );
```

`toILink` 签名与 activate 判据：

```typescript
function toILink(
  m: LinkMatch,
  y: number,
  cellStart: number[],
  term: Terminal,
  isMac: boolean,
  getDownPos: () => PointerPos | null,
  getHomeDir: () => string,
  openURL: (url: string) => Promise<void>,
  onError: (key: LinkErrorKey) => void,
) {
  const { startX, endX } = linkCellRange(m, cellStart);
  return {
    range: { start: { x: startX, y }, end: { x: endX, y } },
    text: m.text,
    decorations: { underline: true, pointerCursor: true },
    activate: async (event: MouseEvent) => {
      if (!shouldActivateLink(event, getDownPos(), isMac)) return;
      event.preventDefault();
      event.stopPropagation();
      event.stopImmediatePropagation();
      term.clearSelection();
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

把 registerLinkProvider 的返回 disposer 与 mousedown 清理合并。原 return：

```typescript
  try {
    return term.registerLinkProvider(provider as unknown as Parameters<Terminal["registerLinkProvider"]>[0]);
  } catch (err) {
    console.warn("[AT Term] registerLinkProvider failed", err);
    return { dispose() {} };
  }
```

改为：

```typescript
  let providerDisposable: IDisposable = { dispose() {} };
  try {
    providerDisposable = term.registerLinkProvider(
      provider as unknown as Parameters<Terminal["registerLinkProvider"]>[0],
    );
  } catch (err) {
    console.warn("[AT Term] registerLinkProvider failed", err);
  }
  return {
    dispose() {
      el?.removeEventListener("mousedown", onMouseDown);
      providerDisposable.dispose();
    },
  };
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd desktop/frontend && npx vitest run src/composables/useTerminalLinkProvider.test.ts`
Expected: PASS（activate 单击/拖选 + 既有 provideLinks 用例全过）。

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/composables/useTerminalLinkProvider.ts desktop/frontend/src/composables/useTerminalLinkProvider.test.ts
git commit -m "feat(terminal): 链接纯单击打开,拖选拦截"
```

---

## Task 10: 软换行链接跨行识别（TDD）

**Files:**
- Modify: `src/lib/terminalLinks.ts`（新增 `mapWrappedLogicalLine`）
- Modify: `src/lib/terminalLinks.test.ts`
- Modify: `src/composables/useTerminalLinkProvider.ts`（provideLinks 跨行）
- Modify: `src/composables/useTerminalLinkProvider.test.ts`

**设计：** `mapWrappedLogicalLine(buffer, firstY, cols, maxRows)` 从逻辑行首行 `firstY`（1-based，与 xterm getLine 的 y-1 对应关系保持）拼接软换行物理行文本，返回 `{ text, cellStart, cellY }`，其中 `cellStart[i]`/`cellY[i]` 给出第 i 个字符所在物理行内 cell 列与物理行号。`provideLinks(y)` 先上溯到逻辑行首，展开检测，再只返回与本行 y 相交的片段。

- [ ] **Step 1: 写 mapWrappedLogicalLine 失败测试**

`terminalLinks.test.ts` 加一个能构造多物理行 + isWrapped 的 fake buffer helper 与用例：

```typescript
import { mapWrappedLogicalLine } from "./terminalLinks";

// Build a fake xterm buffer from an array of {text, wrapped} physical lines.
function fakeBuffer(rows: Array<{ text: string; wrapped: boolean }>, cols: number) {
  function lineAt(idx: number) {
    const row = rows[idx];
    if (!row) return undefined;
    const cells: Array<{ chars: string; width: number }> = [];
    for (const ch of row.text) {
      const cp = ch.codePointAt(0) ?? 0;
      const w = cp >= 0x1100 && cp <= 0x9fff ? 2 : 1;
      cells.push({ chars: ch, width: w });
      for (let i = 1; i < w; i++) cells.push({ chars: "", width: 0 });
    }
    return {
      isWrapped: row.wrapped,
      getCell(x: number) {
        const c = cells[x];
        if (!c) return undefined;
        return { getChars: () => c.chars, getWidth: () => c.width };
      },
    };
  }
  return { getLine: (y: number) => lineAt(y) };
}

describe("mapWrappedLogicalLine", () => {
  it("joins a URL split across two soft-wrapped physical lines", () => {
    const cols = 20;
    const buf = fakeBuffer(
      [
        { text: "http://ex.com/aaaaa", wrapped: false }, // firstY = 0
        { text: "bbb/ccc", wrapped: true },
      ],
      cols,
    );
    const { text, cellY } = mapWrappedLogicalLine(buf, 0, cols, 50);
    expect(text.startsWith("http://ex.com/aaaaa")).toBe(true);
    expect(text).toContain("bbb/ccc");
    // the joined text detects as one link
    expect(detectLinks(text).map((m) => m.text)).toEqual(["http://ex.com/aaaaabbb/ccc"]);
    // characters from the second physical line report physical row 1
    const idx = text.indexOf("bbb");
    expect(cellY[idx]).toBe(1);
  });

  it("does not join when the next line is a hard newline (isWrapped=false)", () => {
    const cols = 20;
    const buf = fakeBuffer(
      [
        { text: "http://ex.com/aaaaa", wrapped: false },
        { text: "bbb/ccc", wrapped: false },
      ],
      cols,
    );
    const { text } = mapWrappedLogicalLine(buf, 0, cols, 50);
    expect(text).toBe("http://ex.com/aaaaa");
  });
});
```

- [ ] **Step 2: 运行确认失败**

Run: `cd desktop/frontend && npx vitest run src/lib/terminalLinks.test.ts -t "mapWrappedLogicalLine"`
Expected: FAIL（未定义）。

- [ ] **Step 3: 实现 mapWrappedLogicalLine**

在 `terminalLinks.ts` 追加（复用现有 cell 遍历逻辑）：

```typescript
export interface WrappedLineLike {
  isWrapped: boolean;
  getCell(x: number): CellLike | undefined;
}

export interface BufferLike {
  getLine(y: number): WrappedLineLike | undefined;
}

export interface MappedLogicalLine {
  /** Joined visible text across all soft-wrapped physical rows. */
  text: string;
  /** cellStart[i] = 0-based cell column within its physical row for text[i]. */
  cellStart: number[];
  /** cellY[i] = 0-based physical row index (0 = firstY) for text[i]. */
  cellY: number[];
  /** Number of physical rows this logical line spans. */
  rowCount: number;
}

/**
 * Walk a soft-wrapped logical line starting at physical row firstY, joining each
 * continuation row (the row whose isWrapped is true) into one string while
 * recording, per character, its cell column and which physical row it lands on.
 * Stops at the first non-wrapped continuation or after maxRows rows.
 */
export function mapWrappedLogicalLine(
  buffer: BufferLike,
  firstY: number,
  cols: number,
  maxRows: number,
): MappedLogicalLine {
  let text = "";
  const cellStart: number[] = [];
  const cellY: number[] = [];
  let rowCount = 0;
  for (let row = 0; row < maxRows; row++) {
    const line = buffer.getLine(firstY + row);
    if (!line) break;
    if (row > 0 && !line.isWrapped) break; // next row is a hard line, stop
    rowCount = row + 1;
    for (let x = 0; x < cols; x++) {
      const cell = line.getCell(x);
      if (!cell) continue;
      const width = cell.getWidth();
      if (width === 0) continue;
      const chars = cell.getChars() || " ";
      for (let k = 0; k < chars.length; k++) {
        cellStart.push(x);
        cellY.push(row);
      }
      text += chars;
    }
  }
  return { text, cellStart, cellY, rowCount };
}
```

- [ ] **Step 4: 运行确认通过**

Run: `cd desktop/frontend && npx vitest run src/lib/terminalLinks.test.ts`
Expected: PASS。

- [ ] **Step 5: provideLinks 跨行改造 —— 先写测试**

`useTerminalLinkProvider.test.ts` 的 `makeFakeTerm` 目前单行。新增一个多行 fake（带 isWrapped），并加用例断言两物理行各返回一个片段、text 均为完整 URL：

```typescript
function makeWrappedTerm(rows: Array<{ text: string; wrapped: boolean }>, cols: number) {
  let provider: any = null;
  const dispose = vi.fn();
  function lineAt(idx: number) {
    const row = rows[idx];
    if (!row) return undefined;
    const cells: Array<{ chars: string; width: number }> = [];
    for (const ch of row.text) { cells.push({ chars: ch, width: 1 }); }
    return {
      isWrapped: row.wrapped,
      translateToString: () => row.text,
      getCell(x: number) {
        const c = cells[x];
        return c ? { getChars: () => c.chars, getWidth: () => c.width } : undefined;
      },
    };
  }
  const el = { addEventListener() {}, removeEventListener() {} };
  return {
    term: {
      cols,
      element: el,
      registerLinkProvider(p: any) { provider = p; return { dispose }; },
      clearSelection: vi.fn(),
      buffer: { active: { getLine: (y: number) => lineAt(y) } },
    } as unknown as import("xterm").Terminal,
    getProvider: () => provider,
  };
}

it("stitches a soft-wrapped URL and returns a segment on each physical row", () => {
  const cols = 20;
  const f = makeWrappedTerm(
    [
      { text: "http://ex.com/aaaaa", wrapped: false },
      { text: "bbb/ccc", wrapped: true },
    ],
    cols,
  );
  useTerminalLinkProvider({ term: f.term, isMac: false, getHomeDir: () => "", openURL: vi.fn(), onError: vi.fn() });
  let row1: any[] | undefined; let row2: any[] | undefined;
  f.getProvider().provideLinks(1, (l: any) => (row1 = l));
  f.getProvider().provideLinks(2, (l: any) => (row2 = l));
  expect(row1).toHaveLength(1);
  expect(row2).toHaveLength(1);
  expect(row1![0].text).toBe("http://ex.com/aaaaabbb/ccc");
  expect(row2![0].text).toBe("http://ex.com/aaaaabbb/ccc");
  expect(row1![0].range.start.y).toBe(1);
  expect(row2![0].range.start.y).toBe(2);
});
```

- [ ] **Step 6: 运行确认失败**

Run: `cd desktop/frontend && npx vitest run src/composables/useTerminalLinkProvider.test.ts -t "soft-wrapped"`
Expected: FAIL（当前 provideLinks 单行，row2 拿到的是残缺片段或空）。

- [ ] **Step 7: 实现 provideLinks 跨行**

改 `useTerminalLinkProvider.ts` 的 `provider.provideLinks`。用 `mapWrappedLogicalLine` + 上溯逻辑行首 + 切回物理行片段。替换整个 `provider` 定义为：

```typescript
  const MAX_LOGICAL_ROWS = 50;

  const provider = {
    provideLinks(y: number, callback: (links: unknown[] | undefined) => void) {
      const active = term.buffer.active as unknown as {
        getLine(i: number): { isWrapped: boolean; getCell(x: number): unknown } | undefined;
      };
      // Walk up to the first row of this logical line (row whose own isWrapped
      // is false). y is 1-based; xterm's getLine is 0-based.
      let firstIdx = y - 1;
      for (let steps = 0; steps < MAX_LOGICAL_ROWS; steps++) {
        const line = active.getLine(firstIdx);
        if (!line || !line.isWrapped) break;
        firstIdx--;
      }
      const mapped = mapWrappedLogicalLine(
        active as unknown as import("../lib/terminalLinks").BufferLike,
        firstIdx,
        term.cols,
        MAX_LOGICAL_ROWS,
      );
      const matches = detectLinks(mapped.text);
      if (matches.length === 0) {
        callback(undefined);
        return;
      }
      const wantRow = y - 1 - firstIdx; // 0-based physical row within logical line
      const links: unknown[] = [];
      for (const m of matches) {
        // Emit one ILink per physical row this match spans; keep only the row
        // matching the requested y so each physical line renders its own piece.
        let segStart = m.start;
        while (segStart < m.end) {
          const row = mapped.cellY[segStart];
          let segEnd = segStart;
          while (segEnd < m.end && mapped.cellY[segEnd] === row) segEnd++;
          if (row === wantRow) {
            const startX = mapped.cellStart[segStart] + 1;
            const endX = mapped.cellStart[segEnd - 1] + 1;
            links.push(
              makeLink(m, firstIdx + row + 1, startX, endX, term, isMac, () => lastDownPos, getHomeDir, openURL, onError),
            );
          }
          segStart = segEnd;
        }
      }
      callback(links.length ? links : undefined);
    },
  };
```

并把原 `toILink` 改名/改造为 `makeLink`（接受已算好的 startX/endX/y，而非重新用 cellStart）：

```typescript
function makeLink(
  m: LinkMatch,
  y: number,
  startX: number,
  endX: number,
  term: Terminal,
  isMac: boolean,
  getDownPos: () => PointerPos | null,
  getHomeDir: () => string,
  openURL: (url: string) => Promise<void>,
  onError: (key: LinkErrorKey) => void,
) {
  return {
    range: { start: { x: startX, y }, end: { x: endX, y } },
    text: m.text,
    decorations: { underline: true, pointerCursor: true },
    activate: async (event: MouseEvent) => {
      if (!shouldActivateLink(event, getDownPos(), isMac)) return;
      event.preventDefault();
      event.stopPropagation();
      event.stopImmediatePropagation();
      term.clearSelection();
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

删除旧的 `toILink`（其 cellStart/linkCellRange 逻辑已由 mapWrappedLogicalLine + 上面的切分取代）。若 `linkCellRange` 不再被任何文件引用，保留其定义即可（仍被单测覆盖），无需删除。

> 注意：`lastDownPos` 定义于 `useTerminalLinkProvider` 闭包内（Task 9 已加），`provideLinks` 在同一闭包内可直接访问。

- [ ] **Step 8: 运行确认通过 + 全量该文件测试**

Run: `cd desktop/frontend && npx vitest run src/composables/useTerminalLinkProvider.test.ts`
Expected: PASS（软换行 + 单行 + 宽字符 + activate 全过）。

- [ ] **Step 9: Commit**

```bash
git add desktop/frontend/src/lib/terminalLinks.ts desktop/frontend/src/lib/terminalLinks.test.ts desktop/frontend/src/composables/useTerminalLinkProvider.ts desktop/frontend/src/composables/useTerminalLinkProvider.test.ts
git commit -m "feat(terminal): 软换行 URL 跨物理行识别为完整链接"
```

---

## Task 11: 全量验证

- [ ] **Step 1: 全量测试**

Run: `cd desktop/frontend && npm test`
Expected: 所有测试 PASS。

- [ ] **Step 2: 类型检查**

Run: `cd desktop/frontend && npm run build`
Expected: `vue-tsc --noEmit` 无错误，vite build 成功。

- [ ] **Step 3: 若有失败**

按报错定位修复；常见点：i18n 键遗漏（en/zh 不对称）、FileTree 现有 mount 未传 context 但内部误用非可选调用（应全部用 `?.`）、terminalLinks import 残留 `isModClickEvent`。

---

## Self-Review 结论

- **Spec 覆盖**：六项改动逐一对应 Task 1（toast）/2+3+4+5+6+7（文件树复制/菜单/context/i18n/shellQuote/fallbackCopyText）/8+9（单击判据）/10（软换行）。全覆盖。
- **占位符**：无 TBD/TODO；每步含完整代码与命令。
- **类型一致**：`shouldActivateLink`（Task 8 定义，Task 9/10 使用签名一致）；`PointerPos`（Task 8 定义，Task 9/10 引用）；`mapWrappedLogicalLine`/`BufferLike`（Task 10 定义并使用）；`quoteForShell`（Task 3 定义，Task 6 使用）；`fallbackCopyText`（Task 2 导出，Task 6 使用）；`copyPathToClipboard`（Task 6 定义，Task 7 使用）。一致。
- **向后兼容**：FileTree `context` 为可选 prop，内部一律 `?.` 调用，现有 9 处 mount 不破坏。
