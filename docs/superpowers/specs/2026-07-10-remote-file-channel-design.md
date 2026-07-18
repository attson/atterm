# 远程会话文件通道 (PASTE_FILE) 设计

> **Audience**: 实现 PASTE_FILE 帧、desktop 收件路径、三端前端入口的工程师
> **Last updated**: 2026-07-10
> **Status**: proposed
> **See also**: [../../spec/protocol.md](../../spec/protocol.md) · [../../spec/architecture.md](../../spec/architecture.md) · [2026-06-26-paste-image-preview-design.md](./2026-06-26-paste-image-preview-design.md)

## 1. 背景与目标

现在 remote 会话中，attach 侧（web/PWA、Capacitor 手机、另一台 atterm desktop）只能通过 `PASTE_IMAGE (0x33)` 帧把**图片**送给 owner desktop 的 PTY —— 图片写到本地临时目录，然后借剪贴板 + `Ctrl-V` (`\x16`) 让 CLI TUI 能真"贴出图片"。

现实需求：AI 会话在 owner desktop 上运行，用户离开工位后用手机/另一台机器 attach 进来时，往往需要把**任意文件**（PDF、日志、diff、二进制样本）送给远端 AI 读取，比如：

- 用手机拍的照片以外的截图、PDF 文档
- 从另一台机器复制过来的 crash log 让 AI 分析
- 想让远端 codex 打开某个 diff

本设计新增 `PASTE_FILE (0x37)` 帧，把 PASTE_IMAGE 的"上传 + 落盘 + 注入路径"模式**平移**到任意文件，形成 remote → owner 的通用文件通道。

## 2. 非目标

- **不做流式/分片**：单帧封顶，≤10 MiB 原始 bytes。大文件超限直接拒
- **不做双向**：owner → remote 方向不在本轮设计
- **不做飞书 anchor card 入口**：架构层留出扩展路径（IM SDK → PASTE_FILE 转桥），本轮不做
- **不做 file browser 视图**：AI 无法主动"list 文件"、"读任意路径"，只能读用户显式送来的文件
- **不做多文件批送**：一次一个文件，多个文件多次操作
- **不做进度条**：单帧上传，10 MiB 秒级完成，无进度条需求

## 3. 架构

```
[attach client]                [relay]                    [owner desktop]
  Web / Capacitor /             /client                     /uplink
  Desktop-as-attacher              │                             │
       │                           │                             │
       │  1. 用户拖入 / 选择文件                                     │
       │  2. 前端读成 bytes，                                        │
       │     构造 PasteFilePayload                                  │
       │     {filename, content_type, data(≤10 MiB)}               │
       │                                                          │
       │ ────── TypePasteFile(0x37) ──────►                       │
       │        (permission gate: driver only + full)             │
       │        (E2EE: seal bytes, AAD = 0x37)                    │
       │                    │                                     │
       │                    ├───── mirror routing ─────►          │
       │                    │                                     │
       │                                       3. adopt 层解密（如启用 E2EE），
       │                                          交给 desktopPtyHost.PasteFile
       │                                       4. 落盘:
       │                                          <cache-root>/paste-files/<sid>/<safe-name>
       │                                          冲名: foo.pdf → foo (1).pdf
       │                                       5. absPath 写入 PTY 主端 (作为 IN)
       │                                          没有 Enter、没有引号
       │                                       6. AI CLI 看到 "/…/foo (1).pdf" 出现在 prompt，
       │                                          由用户下一次回车提交给 AI
       │
       │◄──── (无回执，前端只本地 toast) ────
```

**复用点：**

- **路由**：`internal/relay/client_conn.go` 里已有 `TypePasteImage` 的 case，并列加 `TypePasteFile`，共用同一段权限校验与 mirror 转发
- **desktop 端**：`internal/relay.PasteHost` 接口加 `PasteFile(ctx, sid, PasteFilePayload) error` 方法；`desktop/paste_file.go` 与 `paste_image.go` 平级
- **E2EE**：`internal/e2eecrypto/envelope.go` 的 `SealUnsequenced`/`OpenUnsequenced` 接口帧类型无关，只需更新注释加 `TypePasteFile`；AAD = `session_uuid || 0x37`。参见 §7.2 关于前端 seal 缺口
- **权限**：`internal/relay/permissions.go` 里 `full` 权限档同时管 `TypePasteImage` 与 `TypePasteFile`

**接收端只有一个入口** = owner desktop ptyhost。desktop 上的 handler / 落盘 / stdin 注入 / Settings 缓存管理都是**单点**改动。

## 4. 协议

### 4.1 新增帧类型

`internal/proto/frame.go`：

```go
const (
    // ... existing ...
    TypeCommandEvent Type = 0x35
    TypeViewers      Type = 0x36
    TypePasteFile    Type = 0x37 // client -> relay -> desktop PTY host
    TypeAuthInfo     Type = 0x40
)
```

`0x37` 与 mirror/uplink 段（`0x30..0x36`）紧邻，`0x40` 之前留给同类扩展。

### 4.2 payload schema

```go
// PasteFilePayload carries a generic file attachment from a remote client
// to the desktop that owns the PTY. Structurally identical to
// PasteImagePayload but semantically distinct: PASTE_IMAGE is for
// clipboard image data (silent, filename synthesized), PASTE_FILE is for
// explicit user-picked attachments (filename is user-visible).
type PasteFilePayload struct {
    Filename    string `json:"filename"`     // required; base name only, sanitized on desktop
    ContentType string `json:"content_type"` // best-effort MIME; may be ""
    Data        []byte `json:"data"`         // raw bytes; ≤ ~10 MiB
}
```

### 4.3 协议 doc 追加段

在 `docs/spec/protocol.md` §"帧 schema" 里追加：

> ### `PASTE_FILE` (0x37) — client → relay → desktop PTY host
>
> ```json
> { "filename": "foo.pdf", "content_type": "application/pdf", "data": "<base64>" }
> ```
>
> - `filename`：用户可见文件名（不含目录）。desktop 侧强制 sanitize + dedup 才落盘，wire 值可以脏
> - `content_type`：客户端 best-effort，服务器不校验、不据此路由
> - `data`：原始字节。单帧 payload 上限 16 MiB，实际 base64 + JSON 开销后应用层建议 ≤ 10 MiB
> - **E2EE**：当持有 `account_key` 时，整个 `PasteFilePayload` JSON 走 §E2EE 信封 加密，AAD 鉴别字节 = `0x37`
> - **权限**：`remote_permission = "full"` 才允许；`view` / `control` 被 relay 拒绝，与 PASTE_IMAGE 一致
> - **driver**：只有当前 driver subscriber 能发；非 driver 的 PASTE_FILE 被 relay 静默 drop
> - desktop 收到后：sanitize filename → 落盘 `<cache-root>/paste-files/<sid>/<name>` → 冲名追加 ` (N)` → 把最终绝对路径当作 IN 帧内容写进 PTY（无 CR，无引号）

### 4.4 大小限制

- **协议层**：沿用 `payload_len > 16 MiB` 拒绝（`internal/proto/codec.go` 已有）
- **发送侧**：前端预检 `file.size > 10 * 1024 * 1024` → toast「文件超过 10 MiB」→ 不上传
  - 10 MiB 而非 16 MiB：base64 4/3 膨胀 + JSON + envelope header 需 ~2 MiB buffer
- **desktop 侧**：解码 payload 后 `len(Data) > maxPasteFileBytes (10 MiB)` 兜底拒绝（防伪造客户端绕过前端）— 与前端预检同数值

### 4.5 兼容性

- 老 relay / 老 desktop 收到 `0x37` 未知帧 → reader 循环 log + drop（不断连）
- 老前端不发 `0x37`，功能就是关的 — 完全 additive
- 不需要 `Version` bump，`Version = 1` 保持

按 `no_backward_compat` 惯例：单用户项目，不为老客户端保留降级路径。

## 5. 三端前端入口

三端共享后端 API：新增 `SessionConnection.sendPasteFile(blob, filename)` 于 `web/src/shared/ws/client-conn.ts`。**前端此版本发明文** —— 与现有 `sendPasteImage` 同 posture（前端 E2EE 补齐是独立议题，见 §7.2）。

### 5.1 Web / PWA

- `web/src/main/components/PasteFallback.vue` 现有 `<label for="paste-image-file">` → 姊妹按钮 `<label for="paste-file-file">` "Attach file"，`accept="*/*"`
- 空 terminal 区域 `drop` handler 分流：`file.type.startsWith('image/')` 走 `paste-image`，否则走 `paste-file`
- ctrl+V / cmd+V 里的 files 同样分流
- 新增 `web/src/main/lib/pasteFileBus.ts` + `PasteFilePreviewHost.vue`：简化 toast，只显示 filename + size + close，3s 自动消失
- driver + `full` 权限时才显示按钮，沿用现有 `usePermission()`

### 5.2 桌面 attach 模式

`desktop/frontend/src/` 结构和 web 前端同构，复用同一 shared `sendPasteFile`：

- `desktop/frontend/src/components/TerminalView.vue` 加 `sendPasteFile` 转发
- 拖拽入口：Wails `runtime.OnFileDrop` bridge
- 无 file picker 按钮（drag-drop 已足够）
- Preview toast 同 web

### 5.3 手机 Capacitor

按 `mobile_app_source_root`，Capacitor build 用 `desktop/frontend/dist-capacitor`，5.2 里的 shared 组件自动可用。移动特有：

- Terminal 上方 toolbar 加 📎 attach 按钮（触屏无 drag-drop）
- `<input type=file>` 直接触发系统 file picker（iOS/Android WebView 支持）
- 大小校验前端跑，超过弹 toast
- 复用 `PasteFilePreviewHost`

### 5.4 三端复用矩阵

| 模块 | Web | Desktop-attach | Capacitor |
|---|---|---|---|
| `client-conn.ts::sendPasteFile` | ✅ 共用 | ✅ 共用 | ✅ 共用 |
| `PasteFilePreviewHost.vue` | ✅ 共用 | ✅ 共用 | ✅ 共用 |
| `pasteFileBus.ts` | ✅ 共用 | ✅ 共用 | ✅ 共用 |
| 触发入口 | `<input type=file>` + drag-drop | drag-drop (Wails file drop) | 📎 toolbar → picker |
| 权限门控 | `usePermission()` | `usePermission()` | `usePermission()` |

### 5.5 前端红线

1. **按钮位置**：跟 PASTE_IMAGE 按钮并排，不新开 area
2. **发送成功 toast 内容**：`已发送 foo.pdf (3.2 MiB) → 已粘贴到会话`。**不显示 desktop 落盘路径**（前端不知道 desktop 上 dedup 后的实际路径，只有 PTY 里会出现）
3. **不主动申请相册/文件权限**：`<input type=file>` 用户点击才拉起 picker，无需启动时申请
4. **飞书 anchor card 不做**：spec 明确留 note，未来扩展只需加 anchor card action + relay IM SDK → PASTE_FILE 转桥

## 6. 桌面收件路径

### 6.1 handler 位置

新增 `desktop/paste_file.go`，与 `paste_image.go` 平级：

```go
const maxPasteFileBytes = 10 * 1024 * 1024

func (h *desktopPtyHost) PasteFile(ctx context.Context, sessionID uuid.UUID, p proto.PasteFilePayload) error {
    log.Printf("desktop-paste-file: request %s", pasteFileLogDetails(sessionID, p))
    absPath, err := savePastedFile(sessionID, p)
    if err != nil {
        log.Printf("desktop-paste-file: save_failed %s error=%v", pasteFileLogDetails(sessionID, p), err)
        return err
    }
    log.Printf("desktop-paste-file: saved %s path=%q", pasteFileLogDetails(sessionID, p), absPath)
    if _, err := h.Write([]byte(absPath)); err != nil {
        log.Printf("desktop-paste-file: path_write_failed session=%s path=%q error=%v", sessionID, absPath, err)
        return err
    }
    return nil
}
```

**与 PasteImage 的差异：**

- **无 native clipboard 分支**：文件不塞剪贴板，直接注入路径（PasteImage 走剪贴板是为了 TUI 里能真"贴出图片"；file 不需要）
- **无 mime 白名单**：`ContentType` 只 log，不过滤
- **文件名保留**：PasteImage 用 `time.UnixNano() + ext` 完全丢原名；PasteFile **保留 sanitized 原名**，AI 靠文件名推断类型/意图

### 6.2 落盘与 sanitize

```go
func savePastedFile(sessionID uuid.UUID, p proto.PasteFilePayload) (string, error) {
    if len(p.Data) == 0 {
        return "", fmt.Errorf("paste file: empty")
    }
    if len(p.Data) > maxPasteFileBytes {
        return "", fmt.Errorf("paste file: too large (%d bytes)", len(p.Data))
    }
    base, err := appdir.CacheDir()
    if err != nil {
        return "", err
    }
    dir := filepath.Join(base, "paste-files", sessionID.String())
    if err := os.MkdirAll(dir, 0o700); err != nil {
        return "", err
    }
    name := sanitizeAttachmentName(p.Filename)
    absPath, err := dedupFilename(dir, name)
    if err != nil {
        return "", err
    }
    if err := os.WriteFile(absPath, p.Data, 0o600); err != nil {
        return "", err
    }
    return absPath, nil
}
```

**`sanitizeAttachmentName` 规则：**

1. `filepath.Base(name)` — 剥所有目录部分（防 `../../../etc/passwd`）
2. 剥控制字符（`< 0x20`）和 `\` `/` `\0`
3. Unicode NFC normalize
4. 空字符串 → `"file"`
5. 长度截断到 **128 chars**（保留 ext：`filepath.Ext` + base 部分裁剪）
6. Windows 保留名 (`CON` `PRN` `NUL` `COM1..9` `LPT1..9`)：前缀加 `_`

**`dedupFilename` 规则：**

- `foo.pdf` 已存在 → `foo (1).pdf`；再撞 → `foo (2).pdf`
- 用 `os.OpenFile(path, O_CREATE|O_EXCL|O_WRONLY, 0o600)` 原子创建（防并发同名 race）—— 拿到的 file handle 关掉后由 `WriteFile` 覆盖写
- 上限 999 次；999 撞完 → 报错

### 6.3 落盘位置

`appdir.CacheDir()` = `~/Library/Caches/atterm/` (macOS) / `~/.cache/atterm/` (linux) / `%LOCALAPPDATA%\atterm\Cache\` (windows)。

具体：`<cache-root>/paste-files/<session_id>/<name>`

与 PASTE_IMAGE 用同一 root（`paste-images/`），OS 层清 cache 语义正确。

### 6.4 注入 PTY

- 写入内容：`os.WriteFile` 后拿到的 `absPath` 原样写进 PTY，一次 `h.Write([]byte(absPath))`
- **不做 shell 转义**、不加引号、不加 space、不加 CR
- 用户看到：`> read this file: /Users/attson/Library/Caches/atterm/paste-files/<sid>/foo.pdf█`
- 用户手动按 Enter 提交给 AI

### 6.5 Settings → 接收文件缓存

在 `desktop/frontend/src/components/settings/` 加 tab "接收文件"：

- 概览：总大小、文件数、按 session 分组清单
- 每 session 一行：`<session preview> — N 个文件 (M MiB) — [清空]`
- 单文件级：展开列表，`<filename> · <size> · <received_at> · [删除]`
- 顶部按钮：`清空全部` + `打开缓存目录`

**后端 API**（`desktop/app.go`）：

```go
type ReceivedFilesSummary struct {
    TotalBytes int64                       `json:"total_bytes"`
    Sessions   []ReceivedFilesSessionEntry `json:"sessions"`
}
type ReceivedFilesSessionEntry struct {
    SessionID   string              `json:"session_id"`
    SessionName string              `json:"session_name"` // best-effort from open sessions
    Bytes       int64               `json:"bytes"`
    Files       []ReceivedFileEntry `json:"files"`
}
type ReceivedFileEntry struct {
    Name       string `json:"name"`
    Bytes      int64  `json:"bytes"`
    ReceivedAt int64  `json:"received_at"` // ns since epoch, stat.ModTime()
}

func (a *App) ReceivedFilesList() (ReceivedFilesSummary, error)
func (a *App) ReceivedFilesClearAll() error
func (a *App) ReceivedFilesClearSession(sessionID string) error
func (a *App) ReceivedFilesDelete(sessionID, filename string) error
func (a *App) ReceivedFilesOpenDir() error
```

**路径校验：**

- `ReceivedFilesDelete` 校验 `filepath.Clean(name) == name && !strings.ContainsRune(name, filepath.Separator)`，只允许纯 basename
- `sessionID` 走 `uuid.Parse` 校验

### 6.6 手动清理

- 无自动 TTL、无 session-close 自动清
- Session `CLOSE` 时后端不动目录，让用户在 Settings 里控
- 空 session 目录不自动 rmdir；前端在 `0 个文件` 时隐藏该 session 行

## 7. 权限 / E2EE / 错误

### 7.1 权限（relay 层）

`internal/relay/permissions.go`：

```go
switch f.Type {
case proto.TypeIn, proto.TypeResize:
    // 需要 "control" 及以上
case proto.TypePasteImage, proto.TypePasteFile:
    // 需要 "full"
}
```

- **driver-only**：`client_conn.go::relayFromClient` 里对 `IN / RESIZE / PASTE_IMAGE` 非 driver drop 的 switch 加 `PASTE_FILE`
- 非 driver 发 PASTE_FILE → silently drop（不回错，防探测型客户端）
- 权限不够 → 同 PASTE_IMAGE：drop + log `permission_denied`
- 前端按钮层已 gate（非 driver / 非 full 隐藏），双保险

### 7.2 E2EE

`internal/e2eecrypto/envelope.go` 的 `SealUnsequenced` / `OpenUnsequenced` 是**帧类型无关**的通用接口（AAD = `session_uuid || frame_type`），没有代码级白名单，注释里列的 `TypeIn` / `TypePasteImage` 只是当前用到的。加 `PASTE_FILE` 不改 envelope 代码，只：

- 更新 envelope.go 注释加上 `TypePasteFile`
- Go 端 attach client（另一台 atterm desktop）在发 PASTE_FILE 时按 `SealUnsequenced(sk, sid, byte(TypePasteFile), json)` 加密
- desktop（`adopt.go`）在解 PASTE_FILE 时按 `OpenUnsequenced(sk, sid, byte(TypePasteFile), env)` 解密 → parse JSON → 交给 `PasteHost.PasteFile`
- relay：拿到密文不解密，只看 payload 长度

**web / Capacitor 前端的 E2EE 缺口：** 现在 web 端 `sendPasteImage` 不 seal（`web/src/shared/ws/client-conn.ts::139`），说明 PASTE_IMAGE 在 web 客户端**并未真正 E2EE 保护**。PASTE_FILE 沿用同一模式 —— 本 spec **不**引入新的前端 seal 路径。补齐前端 E2EE 是独立议题（下一 spec）。

**E2EE 红线：**

- desktop 端解密后 log filename 才写日志；不解密前不 log 明文字段（沿用 PASTE_IMAGE 现有模式）
- Go attach 客户端在 `account_key` 已解锁时必须 seal，不留明文 fallback

### 7.3 错误处理

| 场景 | 前端 | 后端 |
|---|---|---|
| 前端选文件 > 10 MiB | toast「文件超过 10 MiB」，不上传 | — |
| 前端 WS 断开 | toast「发送失败：连接已断开」 | — |
| relay 权限拒 | 前端无回执 → 3s toast「未收到确认」 | log `permission_denied` |
| E2EE unseal 失败（desktop 侧） | — | log `paste_file: unseal_failed`，drop |
| 落盘 disk full | 3s 无回执后 toast | log err，adopt 层 log 后不断流 |
| filename 全非法 → sanitize 后空 | — | 落盘名 `file`；log `sanitized_empty=true` |
| 冲名 dedup > 999 次 | — | 落盘失败，log 报错，帧 drop |

**无 ACK 帧**：单向 fire-and-forget，与 PASTE_IMAGE 一致。加 ACK 引入 request/response 语义，对单文件没必要。

### 7.4 观测

- relay：`paste_file_received` counter (session_id, bytes, e2ee_on)
- desktop：`paste_file_saved` counter (session_id, bytes, ext)；`paste_file_save_error` counter (reason)
- 前端：`paste_file_sent` event (bytes, mime, source: `dnd`|`picker`|`toolbar`)

### 7.5 会话已关闭时收到 PASTE_FILE

- relay：mirror 已删 → drop + log `paste_file_no_session`
- desktop：不会收到（relay 是唯一路由源）
- 前端：session close 时按钮已隐藏，理论上不会发送

## 8. 测试

### 8.1 proto 层

`internal/proto/frame_test.go` / `codec_test.go`：

- `TestPasteFilePayloadRoundtrip`：JSON marshal/unmarshal 保留字段
- `TestPasteFileFrameCodec`：`Marshal(Frame{Type: TypePasteFile, ...})` → `Unmarshal` 一致
- `TestPasteFileFrameOversize`：payload_len > 16 MiB decoder 拒绝

### 8.2 E2EE

`internal/e2eecrypto/envelope_test.go`：

- `TestEnvelopeAADPasteFile`：seal + unseal `TypePasteFile`，用 `TypePasteImage` AAD unseal 必须失败
- `TestEnvelopePasteFileTampered`：篡改一 byte → unseal 失败

### 8.3 relay 路由 / 权限

`internal/relay/permissions_test.go`：

- `TestPasteFilePermissionMatrix`：`view` `control` `full` × driver / 非 driver，6 case
- `TestPasteFileDropForNonDriver`：driver = A，B 发 → drop + log
- `TestPasteFileDropForMissingSession`：session close，发 → drop

`internal/relay/adopt_test.go`：

- `TestAdoptRoutesPasteFileToPasteHost`：mock `PasteHost.PasteFile`，验证被调用一次且 payload 完整（E2EE 关）
- `TestAdoptDecryptsPasteFileWithE2EE`：encrypted payload → adopt 解密 → host 收到明文

### 8.4 desktop handler

`desktop/paste_file_test.go`：

- `TestSavePastedFileHappyPath`：3 KiB PDF → 落盘位置、内容、权限 `0o600`
- `TestSanitizeAttachmentName` 表驱动：
  - `../../../etc/passwd` → `passwd`
  - `foo\x00bar.txt` → `foobar.txt`
  - `` → `file`
  - `CON` → `_CON`
  - 300-char → 128
  - 日文/中文 NFC normalize
- `TestDedupFilename`：
  - 空目录 → `foo.pdf`
  - 已有 `foo.pdf` → `foo (1).pdf`
  - 已有 `foo.pdf`, `foo (1).pdf` → `foo (2).pdf`
  - race：两 goroutine 同名并发 create，都成功且落到不同 basename
- `TestPasteFileEmpty`：`Data` 长 0 → 错误
- `TestPasteFileTooLarge`：> 10 MiB → 错误
- `TestPasteFileInjectsAbsPath`：fake `*ptyhost.Host` 记录 `Write`，收到 bytes 恰等 absPath，无 CR

### 8.5 desktop Settings bindings

`desktop/received_files_test.go`：

- `TestReceivedFilesList`：两 session 目录 → 概览 total_bytes + 分组正确
- `TestReceivedFilesClearSession`：删除指定 session 目录
- `TestReceivedFilesDeletePathTraversal`：`../foo` → 拒绝
- `TestReceivedFilesDeleteSubdir`：`bar/foo` → 拒绝
- `TestReceivedFilesOpenDir`：mock `runtime.BrowserOpenURL`，参数 `file://…paste-files`

### 8.6 前端单测（Vitest）

- `sendPasteFile` unit (`web/src/shared/ws/__tests__/client-conn-paste-file.test.ts`)：
  - blob → mock WS 收到 frame（type=0x37, payload JSON 反解正确 filename / content_type / data base64）
  - 前端不 seal（同 `sendPasteImage`），无 E2EE 分支需测
- `PasteFilePreviewHost`：toast 显示、3s 自动隐、close 立即隐
- `PasteFallback.vue` 分流：drop image/png → `paste-image`；drop application/pdf → `paste-file`
- 权限门控：mock `usePermission()` = `view` → 按钮不渲染

### 8.7 端到端手工验证

（按 `verification-before-completion` 精神，UI 改动必须真跑）

1. **Web → local desktop 同机**：Chrome 里选 3 MiB PDF，terminal 里出现绝对路径；Finder 打开确认内容一致
2. **Capacitor → desktop**：iPhone 里点 📎 选相册照片，terminal 出现路径；desktop 打开路径确认
3. **另一台 desktop → owner desktop**：拖 Finder log 到 terminal
4. **超限**：选 15 MiB → toast + 不上传
5. **非 driver**：viewer 登录 → 按钮不显示
6. **E2EE**：另一台 atterm desktop attach（Go 侧走 seal 路径），账号解锁 E2EE 后跑 #3 — relay log 无 filename，owner desktop 收到明文
7. **Settings 缓存**：Settings → 接收文件 → 列表；清空后目录空
8. **文件名边界**：`../`、emoji、中文空格 → 落盘名合理，AI 能 cat 到

### 8.8 CI

- `go test ./internal/... ./desktop/...` 全跑
- `pnpm -C web test` / `pnpm -C desktop/frontend test`
- 无新 CI job

## 9. 待办清单（供 writing-plans 展开）

按依赖顺序，工作单元大致如下：

1. proto：新增 `TypePasteFile` + `PasteFilePayload` + 单测
2. E2EE：`envelope.go` 注释加 `TypePasteFile`；AAD 单测（Go 侧 seal/unseal 用 `SealUnsequenced` 走 PASTE_FILE frame_type）
3. relay：`permissions.go` / `client_conn.go` / `adopt.go` 加 PASTE_FILE 路由 + 权限 + driver gate + 单测
4. relay: `PasteHost` 接口加 `PasteFile` 方法
5. desktop：`paste_file.go` 落盘 + sanitize + dedup + 注入 + 单测
6. desktop：`ReceivedFiles*` bindings + 单测
7. shared 前端：`SessionConnection.sendPasteFile` + `pasteFileBus.ts` + `PasteFilePreviewHost.vue` + 单测
8. Web：`PasteFallback.vue` 分流 + drop handler 分流
9. Desktop frontend：`TerminalView.vue` 转发 + Wails file drop
10. Capacitor：toolbar 📎 按钮
11. Settings：接收文件 tab UI + bindings 接线
12. 更新 `docs/spec/protocol.md` PASTE_FILE 段
13. 端到端 checklist（8.7）逐项跑
