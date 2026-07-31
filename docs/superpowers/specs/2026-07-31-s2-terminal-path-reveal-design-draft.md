# S2: 终端本地文件路径 → 在文件浏览器中定位打开 — 设计

切片来源：`.workflow-plan/terminal-links-and-file-actions/SLICES.md` 的 S2。
依赖 S1（已合入 PR #281）锁定的接口，见 `.workflow-plan/.../NOTES.md`。

## 背景与问题

终端里点击本地**文件**路径（绝对路径，如 `/home/attson/GolandProjects/atterm/mobile/scripts/sync-web.mjs`）时，当前走 `file://` 交给系统/浏览器打开。用户希望改为：**在右侧文件浏览器中定位并预览该文件**。

范围限定（用户确认）：
- **仅文件**。点击目录路径保持原 `file://` 行为，不 reveal。
- **仅绝对路径**（`/…`、`~/…`、`file://…`）。相对路径（依赖 cwd 解析 + 易误判）不在本切片，另可切 S2b。

## 核心难点：时序鸿沟

`file-explorer` 是 right-panel 插件，`defaultEnabled: false` 且面板 `panelCollapsed` 默认 `true`——点击发生时它很可能**尚未挂载**，无法直接接收指令。需要一个**常驻的中介**缓存 reveal 请求，等面板挂载后消费。参照 translate 插件的 Pinia 单例 store 范式。

## 架构：常驻 reveal store 打通通信链

```
终端点击本地文件路径 (kind = path | file，且非 http/https)
  → TerminalView.openLinkMatch / provider activate 分流：
      本地路径 → fileRevealStore.request(absPath)   （不再走 openExternalURL）
      http/https/无法解析的 ~ → 保持原 openExternalURL
  → App.vue: watch(store.pending) 非空 →
      pluginStore.setEnabled("file-explorer", true) + panelCollapsed = false
  → FileExplorer 挂载后（或已挂载）: watch(store.pending) 非空 →
      await fileTreeRef.revealPath(path)  →  是文件则 openPath 预览
      store.consume()
  → FileTree.revealPath(path): 从 root 逐级展开父目录（复用 toggle 懒加载），
      末级 entry.isDir 已知：文件 → 选中 + 滚动 + 返回 true；目录 → 选中 + 返回 false
```

判断「文件 vs 目录」天然内置在 `revealPath` 的逐级展开里（展开父目录即得 `DirEntry.isDir`），无需额外 stat 调用。`FileMetaInfo` 不含 isDir，故不用 fileMeta 判断。

## 各单元职责

### 1. `fileReveal.ts`（新建，Pinia 单例 store）

```typescript
export const useFileRevealStore = defineStore("fileReveal", () => {
  const pending = ref<string | null>(null);
  function request(path: string) { pending.value = path; }
  function consume(): string | null { const p = pending.value; pending.value = null; return p; }
  return { pending, request, consume };
});
```

- `pending`：待 reveal 的绝对路径（本机或远程 fs 的路径字符串）。
- `request(path)`：终端点击时写入（触发 App 与 FileExplorer 的 watch）。
- `consume()`：FileExplorer 取走并清空，防止重复触发。

### 2. 打开分流（`TerminalView.vue`）

- `openLinkMatch(hit)` 增加分流：`hit.kind === "http"` → `openExternalURL`（不变）；`hit.kind === "path" || "file"` → 解析出本机绝对路径后 `fileRevealStore.request(abs)`。
  - `file://` 前缀剥离为普通路径；`~/` 用已缓存的 `cachedHomeDir` 展开（无 home 时回退原 `openExternalURL` 报错路径，与现状一致）。
- provider 的打开路径（`useTerminalLinkProvider.makeLink().activate` → `openURL`）：`openURL` 由 TerminalView 注入（`platform.system.openExternalURL`）。为让 provider 点击也分流，改为注入一个**统一的 openLink 回调**：TerminalView 传入 `(match) => 分流`，provider 调它而非 `openURL(url)`。
  - 即把 `UseTerminalLinkProviderDeps.openURL: (url) => Promise<void>` 改为 `openLink: (match: LinkMatch) => Promise<void>`，由 TerminalView 决定 url vs reveal。normalizeForOpen 的调用移入 TerminalView 的回调。

### 3. App 层接线（`App.vue`）

```typescript
watch(() => fileRevealStore.pending, async (p) => {
  if (!p) return;
  if (!pluginStore.isPluginEnabled("file-explorer")) await pluginStore.setEnabled("file-explorer", true);
  if (panelCollapsed.value) panelCollapsed.value = false;
});
```

- 幂等：已启用/已展开则跳过写入。

### 4. `FileExplorer.vue`

- `const fileTreeRef = ref<{ revealPath: (p: string) => Promise<boolean> } | null>(null)`，模板 `<FileTree ref="fileTreeRef" ... />`。
- `watch(() => fileRevealStore.pending, async (p) => { ... })`：非空且 `fs` 就绪时，`const isFile = await fileTreeRef.value?.revealPath(p)`；`isFile` 为真则 `tabsState.value = openPath(tabsState.value, p, "preview")`；最后 `fileRevealStore.consume()`。
- 若 reveal 的路径不在当前 root 子树内（终端 cwd 与文件树 root 不同），revealPath 返回 false 且不展开——安全降级（后续可优化为切 root，本切片不做）。

### 5. `FileTree.revealPath(path)`（新增，`defineExpose`）

- 计算 path 相对 root 的各级祖先目录。
- 从 root 起，对每级祖先目录：找到对应 `TreeNode`，若未展开则 `await toggle(node)`（复用现有懒加载 + watch 逻辑）。
- 到末级：在父目录的 children 里找到目标 entry。
  - 找到且 `isDir === false`：`selectedPath.value = path`，`await nextTick()` 后 `scrollIntoView`，返回 `true`。
  - 找到且 `isDir === true`：`selectedPath.value = path`，滚动，返回 `false`（目录不预览）。
  - 未找到（不在 root 子树 / 已删除）：返回 `false`。
- 通过 `defineExpose({ refresh: startGeneration, revealPath })` 暴露。

## 影响的文件

| 单元 | 文件 | 新建/修改 |
|------|------|-----------|
| reveal store | `src/plugins/fileExplorer/fileReveal.ts` | 新建 |
| reveal store 测试 | `src/plugins/fileExplorer/fileReveal.test.ts` | 新建 |
| 打开分流 | `src/components/TerminalView.vue` | 修改 |
| provider 回调签名 | `src/composables/useTerminalLinkProvider.ts` + `.test.ts` | 修改 |
| App 接线 | `src/App.vue` | 修改 |
| 消费 reveal | `src/plugins/fileExplorer/FileExplorer.vue` | 修改 |
| revealPath | `src/plugins/fileExplorer/FileTree.vue` + `.test.ts` | 修改 |

## 测试策略

- `fileReveal.test.ts`：request/consume 语义（consume 清空、二次 consume 返回 null）。
- `FileTree.test.ts`：mock listDir 多级目录，调 `revealPath("/proj/src/a.ts")` 断言逐级展开 + selectedPath + 文件返回 true / 目录返回 false / 不存在返回 false。
- `useTerminalLinkProvider.test.ts`：把既有 `openURL` 断言迁移到 `openLink(match)` 回调（activate 调用 openLink 而非直接 openURL）。
- `TerminalView.test.ts`：源码断言 openLinkMatch 分流（http → openExternalURL，path/file → fileRevealStore.request）。

## 非目标

- 相对路径识别（cwd 解析 + 防误判）——另切 S2b。
- reveal 路径不在当前 root 时自动切换 root——本切片安全降级为 no-op。
- 目录在文件浏览器中展开定位（仅文件预览；目录点击仍走 file://）。
