# File Explorer — 编辑与保存能力

Status: Approved · Date: 2026-07-17

## 背景

`desktop/frontend/src/plugins/fileExplorer` 目前是只读文件浏览器：
CodeMirror 强制 `EditorState.readOnly.of(true)` + `EditorView.editable.of(false)`；
本地路径由 `PluginFS.ReadFile / FileMeta / ListDir`（`desktop/plugin_fs.go`）提供；
远端路径由 `FSRequestPayload` 的 `list_dir / read_file / read_chunk / file_meta / watch_dir / open_external`（`internal/proto/frame.go`）提供，在 `desktop/remote_fs.go` 里落到同一 `fsAccess`（`desktop/fsaccess.go`）。

本次迭代把浏览器变成"能改能存"的轻量编辑器，覆盖本地与远端两条路径，扩展到完整 CRUD（write / create / rename / delete / mkdir + 回收站）。

## 目标 / 非目标

**目标**

- 本地和远端会话都可以在 File Explorer 里编辑、保存文本文件。
- 完整 CRUD：新建文件、新建文件夹、重命名、删除（默认回收站，Shift = 硬删）。
- 明确的脏态 / 冲突 / 失败反馈；不静默覆盖磁盘上被别人改过的内容。
- 与现有 `resolve()` 安全模型统一：写和读走同一套 `allowRoots` + `denyExact/Suffix`。

**非目标**

- 权限/所有者（chmod / chown）不改。
- 编码转换：只支持 UTF-8；非 UTF-8 文件继续走"binary" banner，不解码不写。
- 大文件：> 2 MiB 编辑器不开、也不写；服务端硬上限 5 MiB。
- 多选、拖拽移动、复制、外部拖入不做。
- 移动端 capacitor 无 pluginHost，本次不改动。
- 自动保存 / auto-format on save 不做。

## 已决策决策点

| 决策 | 选择 |
|---|---|
| 覆盖范围 | 本地 + 远端一起开 |
| 保存触发 | 仅 `Cmd/Ctrl+S` 手动 |
| 冲突处理 | 默认拒绝写入，前端 banner Overwrite / Reload / Cancel |
| 可写范围 | 与读一致：`allowRoots` 内 + `deny` 未命中 |
| CRUD | 全套：write / create-file / rename / remove / mkdir |
| 删除 | 默认回收站；`Shift+Delete` = 硬删；回收站不可用则 fallback 硬删并提示 |
| 脏 tab 关闭 | 弹 Save / Don't Save / Cancel |
| 写入上限 | 沿用读的 2 MiB 前端 / 5 MiB 后端硬上限 |
| 写入前处理 | 完全 byte-for-byte，不动行尾、不加末尾 newline |
| 远端权限 | 复用 `remote_permission=full`；不新增 flag |

## 架构

```
┌───────────── frontend ─────────────┐
│ FileEditor.vue (dispatcher)        │
│  └─ CodeEditor.vue  ← 由 CodeViewer  │ 只对 code kind 生效
│     可写化改造                       │  · svg/markdown 的 code 视图
│     · CodeMirror history           │    也走这个组件
│     · dirty flag + Cmd+S           │  · image/pdf/media 仍只读
│     · 冲突/权限 banner              │
│ FileTree.vue                        │
│  └─ 右键菜单 (New File / New Folder │
│      / Rename / Delete / Reveal /   │
│      Open Externally)               │
│ FileTabs.vue                        │
│  └─ 脏点 + 关闭确认                  │
│ fsBridge.ts                         │
│  └─ writeFile / createFile / rename │
│      / remove / mkdir / trash       │
└────────────────────────────────────┘
                 │
┌── local (Wails) ──┐   ┌── remote (proto) ──┐
│ PluginFS.*        │   │ FSRequestPayload    │
│  → fsAccess.*     │◀──│  op: write_file /   │
│  → internal/trash │   │      create_file /  │
└───────────────────┘   │      rename /       │
                        │      remove /       │
                        │      mkdir /        │
                        │      trash          │
                        │ handleRemoteFSRequest│
                        └─────────────────────┘
```

关键选择：

1. **同一后端 `fsAccess`**。写、CAS、trash 都只实现一次；本地与远端共享。
2. **CAS 用 `expected_modtime`**（不用内容 hash）。write 请求带客户端加载时的 `ModTime`；后端在 open→写 tmp→rename 期间对文件加 flock，落盘后 stat 拿到新 `ModTime` 回给前端。够挡住 IDE / 终端里另一手编辑，成本低。
3. **原子替换**：写入落盘用 `tmp + rename`。tmp 与目标同目录避免跨卷 `rename` 失败；崩溃不会残半个文件。
4. **回收站** 独立 `internal/trash` 小包，三平台各自 shell out（macOS `osascript`、Linux `gio trash`、Windows `powershell Shell.Application`）；不引原生依赖。

## proto / RPC 契约

新增 6 个 op，走现有 `FSRequestPayload` / `FSResponsePayload`：

| Op | 请求字段（新增） | 响应字段 |
|---|---|---|
| `write_file` | `Path`, `Data []byte`, `ExpectedModTime int64`, `CreateIfMissing bool` | `Meta` |
| `create_file` | `Path`（不能已存在） | `Meta` |
| `rename` | `Path` (from), `NewPath string` | `Meta` |
| `remove` | `Path`, `Recursive bool` | — |
| `mkdir` | `Path` | `Meta` |
| `trash` | `Path` | — |

`FSRequestPayload` 增字段（都带 `omitempty` 兼容旧 payload）：

```go
Data            []byte `json:"data,omitempty"`
ExpectedModTime int64  `json:"expected_modtime,omitempty"`
NewPath         string `json:"new_path,omitempty"`
Recursive       bool   `json:"recursive,omitempty"`
CreateIfMissing bool   `json:"create_if_missing,omitempty"`
```

`FSResponsePayload` 新增：

```go
Meta *FileMetaInfo `json:"meta,omitempty"` // write/create/rename/mkdir 都填
```

错误码前缀（`OK=false` 时 `Error` 字段开头）：

- `stale_modtime: current=<n>` — CAS 冲突
- `path_forbidden: <p>` — allowRoots / deny 命中
- `already_exists: <p>` — create / mkdir 目标已存在
- `not_found: <p>` — rename / remove / trash 源不存在
- `is_directory` / `not_a_directory`
- `write_denied: <reason>` — 其他系统错

前端按前缀 switch，展开对应 banner；未识别前缀时直接展示 `error.message`。

## 后端 (Go)

**新文件**

- `desktop/fsaccess_write.go`
  - `writeFile(path string, data []byte, expectedModTime int64, createIfMissing bool) (FileMetaInfo, error)`
  - `createFile(path string) (FileMetaInfo, error)`
  - `renamePath(from, to string) (FileMetaInfo, error)`
  - `remove(path string, recursive bool) error`
  - `mkdir(path string) (FileMetaInfo, error)`
  - `trash(path string) error`（委托 `internal/trash`）

  所有入口先 `resolve()`。对于 `rename`：源和目标各自 resolve、两侧都必须落在 allowRoots；目标不能 deny。写入用 `os.CreateTemp(sameDir, ".atterm-tmp-*")` → `Write` → `Sync` → `os.Rename`。CAS 检查：`os.OpenFile(target, O_RDONLY, 0)` → `fstat().ModTime().UnixMilli()` 与 `expectedModTime` 比对（`expectedModTime == 0` 视为跳过 CAS，仅用于 `CreateIfMissing`）。文件级 `flock` 用 `golang.org/x/sys/unix.Flock` / Windows `LockFileEx` 抽象成 `fslock` 内部小助手；测试可注入。

- `internal/trash/trash.go` + `trash_darwin.go / trash_linux.go / trash_windows.go`
  - `Send(path string) error`；`ErrUnavailable` 表示当前平台无可用命令。

- `desktop/plugin_fs_write.go`
  - `PluginFS.WriteFile / CreateFile / Rename / Remove / Mkdir / Trash` 都是 `access.*` 的透传，只在这里做 Wails 绑定的错误消息规整。

**变更**

- `desktop/fsaccess.go`：新增 `osCreateTemp / osRename / osRemove / osRemoveAll / osMkdir` 变量（与现有 `osOpenFile / osStat / osReadDir` 同风格），供测试替换。
- `desktop/remote_fs.go` `handle()` switch 新增 6 个 case，共用 `access`；返回 payload 里带 `Meta`。`remote_permission=full` gate 已在 `handleRemoteFSRequest` 顶层，无需再改。
- `desktop/app.go`（或绑定处）：把 `PluginFS` 的新增方法登记到 Wails；`wailsjs/go/models` 会自动生成新签名。

**测试**（先写测试再落实现，走 TDD）

- `desktop/fsaccess_write_test.go`
  - allowRoots 越界 → `ErrPathForbidden`
  - denyExact / denySuffix 命中 → `ErrPathDenied`
  - CAS 命中/miss 分支
  - tmp+rename 原子性（模拟 rename 前 crash：tmp 存在但 target 不动）
  - `remove(recursive=false)` 对非空目录报错
- `desktop/plugin_fs_write_test.go`
  - 每个绑定方法都过一遍成功 + 一个失败路径。
- `desktop/remote_fs_test.go`
  - 6 个 op 的成功 / CAS 失败 / permission-denied gate 路径。
- `internal/trash/trash_darwin_test.go`
  - Mock 掉 `exec.Command` 变量，断言参数正确；`ErrUnavailable` 走 fallback。

## 前端 (Vue / TS)

**`fsBridge.ts` 接口扩展**

```ts
export interface FileSystemBridge {
  // …existing readonly methods…
  writeFile(
    path: string,
    data: Uint8Array,
    expectedModTime: number | null,
  ): Promise<FileMetaInfo>;
  createFile(path: string): Promise<FileMetaInfo>;
  rename(from: string, to: string): Promise<FileMetaInfo>;
  remove(path: string, recursive: boolean): Promise<void>;
  mkdir(path: string): Promise<FileMetaInfo>;
  trash(path: string): Promise<void>;
}
```

- `createLocalFSBridge`：直接透传 `pluginHost.fs.*`。
- `createRemoteSessionFS`：`data` → base64（`Uint8Array` → binary string → `btoa`）→ 走 `sendFSRequest`；response `Meta` 直接返回。

**`CodeViewer.vue` → `CodeEditor.vue`**

- 去掉 `EditorView.editable.of(false)` / `EditorState.readOnly.of(true)`。
- 追踪 `dirty`：`EditorView.updateListener.of(v => { if (v.docChanged) recomputeDirty(v.state.doc) })`；`recomputeDirty` 比较 `doc.toString()` 与 `originalText`；变更时 `emit('dirty-change', dirty)`。
- 保存命令：`keymap.of([{ key: "Mod-s", preventDefault: true, run: () => { void save(); return true } }])`；也提供 `save()` 方法透过 `defineExpose` 供父组件调用。
- `save()`：`fs.writeFile(path, new TextEncoder().encode(text), loadedModTime.value)`；成功后 `loadedModTime = newMeta.modTime`、`originalText = text`、`dirty = false`、`emit('dirty-change', false)`。失败：按错误码前缀分支；`stale_modtime` 时展示 3 键 banner。
- 处理 `reloadPending`：如果 `dirty`，reload 按钮加副标题 "Discard changes"；否则跟现有行为一致。

**`tabsModel.ts`**

- `Tab` 加 `dirty: boolean`（默认 false）。
- 新增 `setDirty(state, path, dirty): TabsState`（用 path 定位而不是 index，避免 tab 顺序变化后错位）。
- `closeTab` 保持现状；关闭前的确认由 `FileExplorer.vue` 处理。

**`FileTabs.vue`**

- 脏 tab title 后加 `•`。
- 关闭点击时若 `tab.dirty`，emit `confirm-close`；否则直接 emit `close`。
- 新增无障碍 `aria-label`：`"{filename} — unsaved"`。

**`FileExplorer.vue`**

- 用 `<CodeEditor @dirty-change="onDirtyChange">` 替换 `<CodeViewer>`（只在 kind === 'code' 或 svg/markdown 的 code 视图；其它 preview 保持只读）。
- 提供简单的确认对话框组件 `ConfirmDialog.vue`（若已有类似组件，复用），承接 Save / Don't Save / Cancel 与删除确认。
- 处理关 tab 时的三态：
  - Save：`await editorRef.value.save()`；成功再 `closeTabAt(idx)`；失败留在原位。
  - Don't Save：`closeTabAt(idx)`。
  - Cancel：no-op。

**`FileTree.vue` / `FileTreeNode.vue`**

- 右键菜单集成 `plugins/contextMenuItems.ts` 现有工具；新增条目：`newFile`, `newFolder`, `rename`, `delete`。
- `newFile / newFolder`：inline 输入框，回车提交、Esc 取消；调用 `fs.createFile` / `fs.mkdir`；watchDir 事件会自动刷新列表。
- `rename`：inline 编辑当前节点名字；调用 `fs.rename(oldFullPath, newFullPath)`。
- `delete`：读键盘 modifier；`Shift` 按下 → 弹"永久删除"确认 → `fs.remove(path, isDir)`；否则弹"移到回收站"确认 → `fs.trash(path)`。若 `trash` 返回 `trash_unavailable`，fallback 弹硬删确认。

**i18n**

新增 key（`en.json` + `zh.json`）：

```
plugins.fileExplorer.save            "Save" / "保存"
plugins.fileExplorer.dirty           "Unsaved changes" / "有未保存的更改"
plugins.fileExplorer.staleModTime    "File was modified on disk since you opened it." / "文件在你打开后已被外部修改。"
plugins.fileExplorer.overwrite       "Overwrite" / "覆盖"
plugins.fileExplorer.reloadDiscard   "Reload (discard changes)" / "重新加载（放弃更改）"
plugins.fileExplorer.confirmCloseTitle "Save changes to {name}?" / "是否保存对 {name} 的更改？"
plugins.fileExplorer.dontSave        "Don't Save" / "不保存"
plugins.fileExplorer.cancel          "Cancel" / "取消"
plugins.fileExplorer.newFile         "New File" / "新建文件"
plugins.fileExplorer.newFolder       "New Folder" / "新建文件夹"
plugins.fileExplorer.rename          "Rename" / "重命名"
plugins.fileExplorer.delete          "Delete" / "删除"
plugins.fileExplorer.confirmTrash    "Move {name} to Trash?" / "把 {name} 移到回收站？"
plugins.fileExplorer.confirmHardDelete "Permanently delete {name}? This cannot be undone." / "永久删除 {name}？此操作无法撤销。"
plugins.fileExplorer.trashUnavailable "Trash is not available on this system. Delete permanently instead?" / "此系统上不支持回收站。改为永久删除？"
plugins.fileExplorer.saveFailed      "Save failed: {message}" / "保存失败：{message}"
```

**测试**

- `CodeEditor.test.ts`：dirty tracking、Cmd+S、save 成功清除 dirty、stale_modtime 展示 banner + 三键路径。
- `FileTabs.test.ts`：脏点渲染、关闭 dirty tab emit `confirm-close`。
- `FileTree.test.ts`：右键菜单项分发；Shift+Delete 走 `remove`、单 Delete 走 `trash`。
- `fsBridge.test.ts` / `remoteSessionFS.test.ts`：新增 6 方法的 request 序列化 + response 解析。
- `tabsModel.test.ts`：`setDirty` 语义（按 path 更新）。

## 边界

- 编码：只 UTF-8。非文本文件 `previewKind` 返回 `binary-unknown` / `image` / …，那些视图不进入 `CodeEditor`。
- CAS 只保护"内容"：外部工具刚 rename 掉了源文件时，保存会返回 `not_found`，前端弹 "文件已不存在，是否新建到该路径？" 二选一。
- Trash 不可用（如 Linux 无 `gio`）：前端弹 fallback 提示（硬删 or 取消），不会静默降级。
- 移动端：capacitor 无 `pluginHost.fs`，`FileEditor` 之前就未在移动端加载，无回归。
- 大文件：>2 MiB 编辑器仍走 `tooLarge` banner，无 save 按钮。

## 变更清单

**新增**
- `desktop/fsaccess_write.go`
- `desktop/fsaccess_write_test.go`
- `desktop/plugin_fs_write.go`
- `desktop/plugin_fs_write_test.go`
- `internal/trash/trash.go` + `trash_darwin.go` / `trash_linux.go` / `trash_windows.go` + `trash_darwin_test.go`
- `desktop/frontend/src/plugins/fileExplorer/CodeEditor.vue`（由 `CodeViewer.vue` 演化 —— 保留同名 test 迁移过去）
- `desktop/frontend/src/plugins/fileExplorer/ConfirmDialog.vue`（如复用现有组件可省）
- 新增 unit test 文件

**修改**
- `internal/proto/frame.go`（`FSRequestPayload` / `FSResponsePayload` 扩字段）
- `desktop/fsaccess.go`（`osCreateTemp / osRename / osRemove / osRemoveAll / osMkdir` mock hook）
- `desktop/remote_fs.go`（`handle()` switch 追加 6 case）
- `desktop/plugin_fs.go`（新绑定方法，或分离到 `plugin_fs_write.go`）
- `desktop/plugin_fs_server.go`（若有相关路由，跟 wailsjs 生成配合）
- `desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue`（换编辑器组件、关 tab 确认）
- `desktop/frontend/src/plugins/fileExplorer/FileEditor.vue`（dispatcher 引用改名）
- `desktop/frontend/src/plugins/fileExplorer/FileTabs.vue`（脏点 + 关闭确认）
- `desktop/frontend/src/plugins/fileExplorer/FileTree.vue` + `FileTreeNode.vue`（右键菜单）
- `desktop/frontend/src/plugins/fileExplorer/fsBridge.ts`（接口扩展）
- `desktop/frontend/src/plugins/fileExplorer/remoteSessionFS.ts`（write 侧编码）
- `desktop/frontend/src/plugins/fileExplorer/tabsModel.ts`（`dirty` 字段 + `setDirty`）
- `desktop/frontend/src/i18n/locales/en.json` + `zh.json`

**删除**
- `desktop/frontend/src/plugins/fileExplorer/CodeViewer.vue`（改名成 CodeEditor）

## 里程碑

按小步串行，每个里程碑独立可验：

1. **M1 · proto + fsAccess 写路径**：`FSRequestPayload` 扩字段、`fsaccess_write.go`、`internal/trash`，配套测试。
2. **M2 · 本地绑定 + 前端 CodeEditor 保存**：`plugin_fs_write.go` 上 Wails；前端 `CodeEditor` 可写 + Cmd+S + dirty；本地路径 e2e 手动验证。
3. **M3 · remote_fs write 分发 + 远端 e2e**：`remote_fs.go` switch；`fsBridge`/`remoteSessionFS` 写方法；远端会话手动验证。
4. **M4 · CRUD UI**：`FileTree` 右键菜单、`FileTabs` 脏点 + 关闭确认、trash fallback。
5. **M5 · i18n + 打磨 + 发布**：i18n、`docs/plugins/file-explorer.md` 更新、`ship-release` 走 Phase 1–6。
