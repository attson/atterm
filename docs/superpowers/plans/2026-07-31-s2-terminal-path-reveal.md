# S2: 终端本地文件路径 → 文件浏览器中定位打开 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`).

**Goal:** 终端点击本地文件路径时，在右侧文件浏览器中逐级展开定位并预览该文件，而非用 file:// 交给系统。

**Architecture:** 常驻 Pinia 单例 `fileReveal` store 缓存 reveal 请求（跨越 file-explorer 插件挂载时机）。App watch → 启用插件 + 展开面板。FileExplorer watch → 调 FileTree.revealPath 逐级展开 + 文件则预览。终端链接打开统一走 `openLink(match)` 回调分流 http vs 本地路径。

**Tech Stack:** Vue 3、Pinia、TypeScript、xterm.js、Vitest。工作目录 `desktop/frontend`。

**测试命令：** 单文件 `cd desktop/frontend && npx vitest run <path>`；全量 `npm test`；类型检查 `npm run build`。

**依赖 S1（PR #281）：** `shouldActivateLink`/`PointerPos`/`mapWrappedLogicalLine` 已在 terminalLinks.ts；`makeLink().activate` 在 useTerminalLinkProvider.ts；`onTerminalMouseUp` + `linkClickDownPos` 在 TerminalView.vue。

---

## 文件结构

| 文件 | 责任 | 新建/修改 |
|------|------|-----------|
| `src/plugins/fileExplorer/fileReveal.ts` | reveal 请求单例 store | 新建 |
| `src/plugins/fileExplorer/fileReveal.test.ts` | store 单测 | 新建 |
| `src/plugins/fileExplorer/FileTree.vue` | revealPath 逐级展开定位 | 修改 |
| `src/plugins/fileExplorer/FileTree.test.ts` | revealPath 测试 | 修改 |
| `src/plugins/fileExplorer/FileExplorer.vue` | 消费 reveal → revealPath + 预览 | 修改 |
| `src/composables/useTerminalLinkProvider.ts` | openURL → openLink(match) 回调 | 修改 |
| `src/composables/useTerminalLinkProvider.test.ts` | 迁移到 openLink | 修改 |
| `src/components/TerminalView.vue` | 分流 http vs 本地路径 | 修改 |
| `src/components/TerminalView.test.ts` | 分流源码断言 | 修改 |
| `src/App.vue` | watch reveal → 启用插件 + 展开面板 | 修改 |

---

## Task 1: fileReveal store（TDD）

**Files:**
- Create: `src/plugins/fileExplorer/fileReveal.ts`
- Test: `src/plugins/fileExplorer/fileReveal.test.ts`

- [ ] **Step 1: 写失败测试**

创建 `src/plugins/fileExplorer/fileReveal.test.ts`：

```typescript
import { describe, expect, it, beforeEach } from "vitest";
import { setActivePinia, createPinia } from "pinia";
import { useFileRevealStore } from "./fileReveal";

describe("fileReveal store", () => {
  beforeEach(() => setActivePinia(createPinia()));

  it("request sets pending", () => {
    const s = useFileRevealStore();
    expect(s.pending).toBe(null);
    s.request("/a/b.txt");
    expect(s.pending).toBe("/a/b.txt");
  });

  it("consume returns and clears pending", () => {
    const s = useFileRevealStore();
    s.request("/a/b.txt");
    expect(s.consume()).toBe("/a/b.txt");
    expect(s.pending).toBe(null);
  });

  it("consume returns null when nothing pending", () => {
    const s = useFileRevealStore();
    expect(s.consume()).toBe(null);
  });
});
```

- [ ] **Step 2: 运行确认失败**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/fileReveal.test.ts`
Expected: FAIL（模块不存在）。

- [ ] **Step 3: 实现**

创建 `src/plugins/fileExplorer/fileReveal.ts`：

```typescript
import { defineStore } from "pinia";
import { ref } from "vue";

/**
 * Bridges a "reveal this path in the file explorer" request from the terminal
 * to the (possibly not-yet-mounted) file-explorer plugin. The terminal calls
 * request(); App.vue reacts by enabling the plugin and opening the panel; the
 * FileExplorer consumes the pending path once mounted.
 */
export const useFileRevealStore = defineStore("fileReveal", () => {
  const pending = ref<string | null>(null);
  function request(path: string) {
    pending.value = path;
  }
  function consume(): string | null {
    const p = pending.value;
    pending.value = null;
    return p;
  }
  return { pending, request, consume };
});
```

- [ ] **Step 4: 运行确认通过**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/fileReveal.test.ts`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/fileReveal.ts desktop/frontend/src/plugins/fileExplorer/fileReveal.test.ts
git commit -m "feat(fileExplorer): 新增 fileReveal 请求单例 store"
```

---

## Task 2: FileTree.revealPath（TDD）

**Files:**
- Modify: `src/plugins/fileExplorer/FileTree.vue`（新增 revealPath + defineExpose）
- Modify: `src/plugins/fileExplorer/FileTree.test.ts`

**语义：** `revealPath(path): Promise<boolean>` — 逐级展开 path 的祖先目录，选中 path。返回 true 当且仅当 path 是 root 子树内的一个**文件**（非目录、存在）。

- [ ] **Step 1: 写失败测试**

在 `FileTree.test.ts` 的 "path actions" describe 之后新增 describe。先确保 beforeEach 的 listDir mock 覆盖嵌套目录——新增用例内用局部 mock：

```typescript
describe("FileTree — revealPath", () => {
  beforeEach(() => {
    (platform.pluginHost!.fs.listDir as ReturnType<typeof vi.fn>).mockImplementation(async (path: string) => {
      if (path === "/proj") return [{ name: "src", isDir: true }, { name: "README.md", isDir: false }];
      if (path === "/proj/src") return [{ name: "app.ts", isDir: false }, { name: "sub", isDir: true }];
      return [];
    });
  });

  it("expands ancestors and selects a nested file, returning true", async () => {
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    const isFile = await (w.vm as any).revealPath("/proj/src/app.ts");
    await flushPromises();
    expect(isFile).toBe(true);
    // the src dir got expanded, so app.ts is now rendered
    const names = w.findAll(".node-name").map((n) => n.text());
    expect(names).toContain("app.ts");
  });

  it("returns false for a directory path", async () => {
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    const isFile = await (w.vm as any).revealPath("/proj/src");
    expect(isFile).toBe(false);
  });

  it("returns false for a path outside the root subtree", async () => {
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    const isFile = await (w.vm as any).revealPath("/other/x.ts");
    expect(isFile).toBe(false);
  });

  it("returns false for a non-existent nested path", async () => {
    const w = mount(FileTree, { props: { fs, root: "/proj", showHidden: false } });
    await flushPromises();
    const isFile = await (w.vm as any).revealPath("/proj/src/missing.ts");
    expect(isFile).toBe(false);
  });
});
```

- [ ] **Step 2: 运行确认失败**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/FileTree.test.ts -t "revealPath"`
Expected: FAIL（revealPath 未定义）。

- [ ] **Step 3: 实现 revealPath**

在 `FileTree.vue` `<script setup>` 内（`findNode` 之后）新增。它复用现有 `rootNodes`、`findNode`、`toggle`、`selectedPath`、`joinPath`：

```typescript
function ancestorChain(root: string, path: string): string[] {
  // Produce the list of directory paths from just-below-root down to the parent
  // of `path`, e.g. root=/proj, path=/proj/a/b/c.ts -> ["/proj/a", "/proj/a/b"].
  const base = root.endsWith("/") ? root.slice(0, -1) : root;
  if (path === base || !path.startsWith(base + "/")) return [];
  const rest = path.slice(base.length + 1); // "a/b/c.ts"
  const parts = rest.split("/");
  const dirs: string[] = [];
  let cur = base;
  for (let i = 0; i < parts.length - 1; i++) {
    cur = cur + "/" + parts[i];
    dirs.push(cur);
  }
  return dirs;
}

async function revealPath(path: string): Promise<boolean> {
  const base = props.root.endsWith("/") ? props.root.slice(0, -1) : props.root;
  if (path !== base && !path.startsWith(base + "/")) return false;
  // Expand each ancestor directory in turn (toggle lazily loads children).
  for (const dir of ancestorChain(props.root, path)) {
    const node = findNode(rootNodes.value, dir);
    if (!node) return false;
    if (!node.expanded) await toggle(node);
  }
  const target = findNode(rootNodes.value, path);
  if (!target) return false;
  selectedPath.value = path;
  await nextTick();
  const el = document.querySelector<HTMLElement>(`.node[title="${cssEscape(path)}"]`);
  el?.scrollIntoView?.({ block: "nearest" });
  return !target.isDir;
}

// Minimal CSS attribute-selector escaper for the title lookup above.
function cssEscape(s: string): string {
  return s.replace(/["\\]/g, "\\$&");
}
```

在 import 区确保 `nextTick` 已从 vue 导入（现有 import 为 `import { computed, ref, watch, onMounted, onBeforeUnmount } from "vue";` → 加 `nextTick`）。

- [ ] **Step 4: 暴露 revealPath**

把文件末尾的 `defineExpose({ refresh: startGeneration });` 改为：

```typescript
defineExpose({ refresh: startGeneration, revealPath });
```

- [ ] **Step 5: 运行确认通过**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/FileTree.test.ts`
Expected: PASS（revealPath 4 用例 + 现有全过）。

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/FileTree.vue desktop/frontend/src/plugins/fileExplorer/FileTree.test.ts
git commit -m "feat(fileExplorer): FileTree.revealPath 逐级展开定位文件"
```

---

## Task 3: provider openURL → openLink(match) 回调（TDD）

**Files:**
- Modify: `src/composables/useTerminalLinkProvider.ts`
- Modify: `src/composables/useTerminalLinkProvider.test.ts`

**动机：** 让 provider 的点击也能分流 http vs 本地路径。把 `openURL: (url) => Promise<void>` 换成 `openLink: (match: LinkMatch) => Promise<void>`，normalizeForOpen 的决定权交回 TerminalView 注入的回调。

- [ ] **Step 1: 更新测试到 openLink**

`useTerminalLinkProvider.test.ts`：把所有 `openURL: vi.fn()...` 的 dep 改名为 `openLink`，断言从 `openURL` 改为 `openLink` 被调用并收到 match（其 `.text` 为 URL 文本）。示例（改「activate opens on a plain click」）：

```typescript
  it("activate opens on a plain click (no modifier, no drag)", async () => {
    const f = makeFakeTerm("https://x.test");
    const openLink = vi.fn().mockResolvedValue(undefined);
    useTerminalLinkProvider({ term: f.term, isMac: true, getHomeDir: () => "", openLink, onError: vi.fn() });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    f.element.__emit("mousedown", { clientX: 5, clientY: 5 });
    await links![0].activate(new MouseEvent("click", { clientX: 5, clientY: 5 }), "https://x.test");
    expect(openLink).toHaveBeenCalled();
    expect(openLink.mock.calls[0][0].text).toBe("https://x.test");
  });
```

对其余引用 `openURL` 的用例（drag 不触发、Mod 打开、reject→onError、~/ 无 home→onError 等）做同样替换：
- 把 dep 名 `openURL` → `openLink`。
- 「opens URL」类断言 → `expect(openLink).toHaveBeenCalled()`。
- 「不打开」类断言 → `expect(openLink).not.toHaveBeenCalled()`。
- reject 用例：`openLink` mockRejectedValue，断言 `onError` 收到 `"terminal.link.openFailed"`。
- `~/ 无 home` 用例：改为断言 `onError` 收到 `"terminal.link.openFailedNoHome"` 且 `openLink` 未被调用（normalizeForOpen 判断移入回调，故 provider 不再判 home；见下方实现说明）。

**实现说明（影响该用例）：** normalizeForOpen 的 home 判断从 provider 移到 TerminalView 的回调。因此 provider 的 activate 里不再调用 normalizeForOpen/onError("...NoHome")——它只调 `openLink(match)`，由回调自行处理 URL 归一化与错误。所以 `~/ 无 home` 用例应改为验证 openLink 被调用并收到该 match（错误在回调侧），而非在 provider 侧。据此调整该用例断言为：

```typescript
  it("activate passes ~/ match to openLink (home handling is the callback's job)", async () => {
    const f = makeFakeTerm("cd ~/Projects/foo");
    const openLink = vi.fn().mockResolvedValue(undefined);
    useTerminalLinkProvider({ term: f.term, isMac: true, getHomeDir: () => "", openLink, onError: vi.fn() });
    let links: any[] | undefined;
    f.getProvider().provideLinks(1, (l) => (links = l as any[]));
    await links![0].activate(new MouseEvent("click", { metaKey: true }), "~/Projects/foo");
    expect(openLink).toHaveBeenCalled();
  });
```

- [ ] **Step 2: 运行确认失败**

Run: `cd desktop/frontend && npx vitest run src/composables/useTerminalLinkProvider.test.ts`
Expected: FAIL（dep 名/断言不匹配）。

- [ ] **Step 3: 实现 —— 换 dep 与 activate**

`useTerminalLinkProvider.ts`：

改接口：

```typescript
export interface UseTerminalLinkProviderDeps {
  term: Terminal;
  isMac: boolean;
  getHomeDir: () => string;
  openLink: (match: LinkMatch) => Promise<void>;
  onError: (key: LinkErrorKey) => void;
}
```

解构 `const { term, isMac, getHomeDir, openLink, onError } = deps;`（getHomeDir 仍传入 makeLink 以保持签名，但不再在 provider 内用于 normalize；可保留以最小改动，或移除——保留）。

`makeLink` 的 activate 简化为调用 openLink：

```typescript
    activate: async (event: MouseEvent) => {
      if (!shouldActivateLink(event, getDownPos(), isMac)) return;
      event.preventDefault();
      event.stopPropagation();
      event.stopImmediatePropagation();
      term.clearSelection();
      try {
        await openLink(m);
      } catch (err) {
        console.warn("[AT Term] openLink failed", err);
        onError("terminal.link.openFailed");
      }
    },
```

`makeLink`/provideLinks 里把传参 `openURL` 改为 `openLink`，`getHomeDir` 参数可删（activate 不再用）。相应更新 `makeLink` 签名：移除 `getHomeDir`、`openURL`，新增 `openLink: (m: LinkMatch) => Promise<void>`。normalizeForOpen 的 import 若不再使用则移除。

- [ ] **Step 4: 运行确认通过**

Run: `cd desktop/frontend && npx vitest run src/composables/useTerminalLinkProvider.test.ts`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/composables/useTerminalLinkProvider.ts desktop/frontend/src/composables/useTerminalLinkProvider.test.ts
git commit -m "refactor(terminal): link provider 用 openLink(match) 回调替代 openURL"
```

---

## Task 4: TerminalView 分流 http vs 本地路径

**Files:**
- Modify: `src/components/TerminalView.vue`
- Modify: `src/components/TerminalView.test.ts`

- [ ] **Step 1: 实现 —— reveal store + 分流回调**

`TerminalView.vue`：

import store：

```typescript
import { useFileRevealStore } from "../plugins/fileExplorer/fileReveal";
```

在 `<script setup>` 内（靠近其它 store/composable）：

```typescript
const fileRevealStore = useFileRevealStore();
```

把 `openLinkMatch(hit)` 改造为分流：http → openExternalURL；本地路径（path/file）→ 解析绝对路径 → reveal：

```typescript
async function openLinkMatch(hit: LinkMatch) {
  if (hit.kind === "path" || hit.kind === "file") {
    const abs = localPathFromMatch(hit);
    if (abs) {
      fileRevealStore.request(abs);
      return;
    }
    // no home to resolve ~/ → fall through to the file:// error path below
  }
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

// Resolve a detected path/file match to an absolute local path, or null when a
// ~/ path can't be resolved (no cached home).
function localPathFromMatch(hit: LinkMatch): string | null {
  const t = hit.text;
  if (hit.kind === "file") return t.startsWith("file://") ? t.slice("file://".length) : t;
  if (t.startsWith("/")) return t;
  if (t.startsWith("~/") || t === "~/") {
    if (!cachedHomeDir) return null;
    const home = cachedHomeDir.replace(/\/+$/, "");
    return home + t.slice(1);
  }
  return null;
}
```

把 link provider 的注入从 `openURL` 改为 `openLink`：找到 `useTerminalLinkProvider({... openURL: (u) => platform.system.openExternalURL(u) ...})`，改为：

```typescript
  linkProviderDisposer = useTerminalLinkProvider({
    term,
    isMac,
    getHomeDir: () => cachedHomeDir,
    openLink: (m) => openLinkMatch(m),
    onError: (key) => emit("toast", t(key)),
  });
```

（`openLinkMatch` 现同时服务右键菜单打开、mouseup fallback、hover-provider 三条路径——统一分流。）

- [ ] **Step 2: 更新源码断言测试**

`TerminalView.test.ts`：新增/调整断言。找到 link provider wiring 的 describe，加：

```typescript
  test("routes local file paths to the file reveal store, http to external URL", () => {
    expect(source).toContain("useFileRevealStore");
    expect(source).toContain("fileRevealStore.request(abs)");
    expect(source).toContain("platform.system.openExternalURL(url)");
  });

  test("injects openLink into the link provider", () => {
    expect(source).toContain("openLink: (m) => openLinkMatch(m)");
  });
```

若旧断言里有 `openURL:` 字样，一并更新为 `openLink:`。

- [ ] **Step 3: 运行相关测试**

Run: `cd desktop/frontend && npx vitest run src/components/TerminalView.test.ts`
Expected: PASS。

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/components/TerminalView.vue desktop/frontend/src/components/TerminalView.test.ts
git commit -m "feat(terminal): 本地文件路径点击改走文件浏览器 reveal,http 仍走浏览器"
```

---

## Task 5: App 接线 —— 启用插件 + 展开面板

**Files:**
- Modify: `src/App.vue`

- [ ] **Step 1: 实现 watch**

`App.vue`：import store + watch。

import：

```typescript
import { useFileRevealStore } from "./plugins/fileExplorer/fileReveal";
```

在 `<script setup>` 内（`pluginStore` 定义之后）：

```typescript
const fileRevealStore = useFileRevealStore();
watch(
  () => fileRevealStore.pending,
  async (p) => {
    if (!p) return;
    if (!pluginStore.isPluginEnabled("file-explorer")) {
      await pluginStore.setEnabled("file-explorer", true);
    }
    if (panelCollapsed.value) panelCollapsed.value = true === false ? true : false;
  },
);
```

（注意：`panelCollapsed` 是可写 computed，直接 `panelCollapsed.value = false` 即可。上面写法等价于 `= false`；实现时用 `panelCollapsed.value = false;`。）

确认 `watch` 已从 vue 导入（App.vue 顶部通常已导入；若无则补）。

- [ ] **Step 2: 类型检查**

Run: `cd desktop/frontend && npm run build`
Expected: 无 TS 错误。

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/src/App.vue
git commit -m "feat(app): reveal 请求到达时自动启用文件浏览器并展开面板"
```

---

## Task 6: FileExplorer 消费 reveal → revealPath + 预览

**Files:**
- Modify: `src/plugins/fileExplorer/FileExplorer.vue`

- [ ] **Step 1: 实现**

`FileExplorer.vue`：

import store：

```typescript
import { useFileRevealStore } from "./fileReveal";
```

在 `<script setup>`：

```typescript
const fileRevealStore = useFileRevealStore();
const fileTreeRef = ref<{ revealPath: (p: string) => Promise<boolean> } | null>(null);

watch(
  () => fileRevealStore.pending,
  async (p) => {
    if (!p || !fs.value) return;
    await nextTick(); // let the tree mount if the panel just opened
    const isFile = (await fileTreeRef.value?.revealPath(p)) ?? false;
    if (isFile) {
      tabsState.value = openPath(tabsState.value, p, "preview");
    }
    fileRevealStore.consume();
  },
  { immediate: true },
);
```

`nextTick` 已在 FileExplorer 顶部 import（现有 `import { computed, nextTick, onBeforeUnmount, ref, shallowRef, watch } from "vue";`）。

模板给 FileTree 加 ref：

```html
          <FileTree
            v-if="root && fs"
            ref="fileTreeRef"
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

- [ ] **Step 2: 类型检查 + FileExplorer 测试**

Run: `cd desktop/frontend && npx vitest run src/plugins/fileExplorer/FileExplorer.remote.test.ts && npm run build`
Expected: PASS + 无 TS 错误。

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue
git commit -m "feat(fileExplorer): 消费 reveal 请求,定位并预览文件"
```

---

## Task 7: 全量验证

- [ ] **Step 1: 全量测试**

Run: `cd desktop/frontend && npm test`
Expected: 所有测试 PASS。

- [ ] **Step 2: 类型检查**

Run: `cd desktop/frontend && npm run build`
Expected: `vue-tsc --noEmit` 无错误。

- [ ] **Step 3: 修复任何失败**

常见点：Pinia 未初始化（测试用 setActivePinia）；FileExplorer.remote 测试新增 watch 触发（immediate:true 时 pending 为 null 直接 return，安全）；openLink 断言残留 openURL。

---

## Self-Review 结论

- **Spec 覆盖**：store（Task1）/ revealPath（Task2）/ provider 回调（Task3）/ TerminalView 分流（Task4）/ App 接线（Task5）/ FileExplorer 消费（Task6）。全覆盖。
- **占位符**：无。每步含完整代码。
- **类型一致**：`useFileRevealStore`（Task1 定义，Task4/5/6 使用）；`revealPath(p): Promise<boolean>`（Task2 定义 + expose，Task6 使用）；`openLink: (match: LinkMatch) => Promise<void>`（Task3 定义，Task4 注入）；`localPathFromMatch`（Task4 定义并使用）。一致。
- **注意**：Task5 的 `panelCollapsed.value = false;`（不是那段等价绕写）。
