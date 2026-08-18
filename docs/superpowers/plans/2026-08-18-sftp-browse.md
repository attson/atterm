# SFTP 浏览与传输 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给 fileExplorer 加第三个数据源（SSH 主机），只读浏览 + 单文件上传下载；并先把 FS 执行挪出 uplink 读循环，否则一次慢的远程操作会卡住所有会话的击键。

**Architecture:** 桌面端 FS 请求改为投递给每会话的有界工作池；SFTP 执行器架在第 26 项的 per-host 引用计数连接上（因此天然继承第 27 项的跳板链）；前端复用既有的 `fsBridge` 抽象加第三个实现。

**Tech Stack:** Go + github.com/pkg/sftp + golang.org/x/crypto/ssh + Wails v2 + Vue 3 + Vitest

**Spec:** [`docs/superpowers/specs/2026-08-18-sftp-browse-design.md`](../specs/2026-08-18-sftp-browse-design.md)

## Global Constraints

- **FS 操作绝不能同步跑在 uplink 读循环里。** `desktop/uplink.go:398-471` 的同一个 switch 同时处理 `TypeIn`（**用户击键**）与 `TypeFSRequest`，而 `remote_fs.go:337` 是同步调用。一次慢的 SFTP 列目录会卡住这条 uplink 上**所有会话的击键**，用户体感是终端卡死。
- **工作池必须有界，且满时立即返回明确的「忙」错误，不排队。** 排队会把延迟藏起来，直到用户以为点击没反应；无界 goroutine 则让一台卡住的主机无限堆积请求。
- **不为 SFTP 单开 SSH 连接。** 复用第 26 项的 per-host 引用计数（`hostConn`），引用归零时一起关——单开会在远端多一个登录、多一份 keepalive。
- **上传到已存在路径默认拒绝**，不静默覆盖。远端没有回收站也没有版本，一次误覆盖不可逆；拒绝的代价只是多点一次确认。沿用既有 `ExpectedModTime` 乐观并发模式（`fsaccess_write.go`），不另造一套。
- **大目录必须有上限并明确告知截断。** 用户看到 12 个文件而目录里有 3000 个却不知道，是最糟的形态（同第 25 项跳过条目必须报原因）。
- **带 `ProxyCommand` 的主机不出现在数据源列表里**；判定复用 `hostRunsProxyCommand`，**不得新增第三个调用点**——第 26、27 项两次 review 都专门守着这个不变量。
- **不在 SFTP 层重复解密。** `proto.DecodeFSRequest` 已在上游解开 sealed 路径段，执行器拿到的是明文，与本地执行器一致（`remote_fs.go` 注释明说下游对加密无感知）。
- **不新增权限维度。** 浏览与上传都走既有的 `remotePermission == full` 闸门（`remote_fs.go:322`）。
- `internal/` 不依赖 `desktop/`（红线 #5）。

---

### Task 1: 把 FS 执行挪出读循环

**Files:**
- Modify: `desktop/remote_fs.go`
- Modify: `desktop/uplink.go`（若投递点需要调整）
- Modify: `desktop/remote_fs_test.go`（或新建）

> 本任务**与 SFTP 无关**，它同时修好本地 FS 在慢磁盘（网络挂载、休眠外置盘）下的同一问题。先做它，因为后面所有远程操作都依赖它。

- [ ] **Step 1: 写失败的测试**

本任务最重要的一条：

```go
func TestSlowFSRequestDoesNotBlockKeystrokes(t *testing.T) {
	// 注入一个人为拖慢的 FS 执行器（阻塞在一个 channel 上）。
	// 发一个 FSRequest，再发一个 TypeIn。
	// 断言：TypeIn 在 FS 操作仍被阻塞时就已经被处理。
	// 放开 FS 操作，断言它也正常完成。
}
```

> 这条测试是本任务的全部意义。断言方式以 `desktop/uplink*_test.go` 既有的构造为准——**先读那些文件确认现有的 uplink 测试怎么搭**，不要新造框架。

再加：

```go
func TestFSWorkerPoolFullReturnsBusy(t *testing.T)   // 池满 → 明确错误，不排队、不 panic
func TestFSResponsesStillMatchTheirRequestID(t *testing.T) // 并发完成后仍一一对应
```

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 实现**

`handleRemoteFSRequest` 改为投递到每会话的有界池，读循环立即返回。池满立即回一个 `OK:false` 且 Error 说明「忙」的响应。

- [ ] **Step 4: 跑全套并提交**

```bash
go test ./desktop/ -tags webkit2_41 -race
git add desktop/
git commit -m "fix(fs): run filesystem requests off the uplink read loop"
```

---

### Task 2: relay 路由表回收

**Files:**
- Modify: `internal/relay/fs_router.go`
- Modify: `internal/relay/fs_router_test.go`

- [ ] **Step 1: 写失败的测试**

`fs_router.go` 是纯路由表，按 `requestID` 注册出口 channel，**没有 TTL 也没有超时**（全文件 grep `timeout`/`context` 零命中）。一个永远收不到响应的请求会占着注册项，直到客户端断开时 `unregisterClient` 兜底。

测试：注册一个请求，不给响应，断言超过 TTL 后注册项被回收。

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 实现** — 给注册项加时间戳与回收。TTL 取值要比最慢的合理 FS 操作宽裕（建议 ≥ 60s），**回收时要留日志**，否则一个被误回收的请求会表现为「点了没反应」且无从查起。

- [ ] **Step 4: 提交**

---

### Task 3: SFTP 执行器

**Files:**
- Create: `internal/sftpfs/sftpfs.go`
- Create: `internal/sftpfs/sftpfs_test.go`
- Modify: `go.mod` / `go.sum`（新增 `github.com/pkg/sftp v1.13.11`，已确认可取）

**Interfaces:**
- Consumes: 一个已建立的 `*ssh.Client`（或 `sshclient.Conn` 暴露的等价物——**先看 `Conn` 是否已导出足够的东西，不够就在 Task 3 里补，并说明**）
- Produces: 与 `fsaccess` 同形的 `listDir` / `fileMeta` / `readChunk` / 写路径

- [ ] **Step 1: 写失败的测试**

`pkg/sftp` 自带一个可在内存中跑的 server（`sftp.NewServer` 架在 `io.ReadWriteCloser` 上），用它做被测对象，**不要**为此起真实 SSH。

覆盖：列目录、读分块、写、目标已存在时拒绝、目录超上限时截断且带标记。

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 实现**

- [ ] **Step 4: 提交**

---

### Task 4: 接成第三个数据源

**Files:**
- Modify: `desktop/remote_fs.go`（按源分派）
- Create/Modify: `desktop/sftp_source.go`
- Modify: `desktop/frontend/src/plugins/fileExplorer/fsBridge.ts` 与新增的 SSH 源实现
- Modify: 对应 `.test.ts`
- Modify: `desktop/frontend/src/lib/api/_bindings.ts`、`lib/api/ssh.ts`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`、`zh-CN.ts`

> binding shim 手工维护（第 25 项在此 BLOCKED 过）；面板已迁 i18n，新文案走 `t()` 并补两个 locale。

- [ ] **Step 1: 写失败的测试**

- 数据源选择器列出已保存主机，**带 `ProxyCommand` 的不出现**。
- 选中一台 SSH 主机后能列目录。
- 上传到已存在路径被拒并给出可读原因。
- 目录被截断时界面**明确显示**被截断。

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 实现** — SFTP 会话复用第 26 项的 `hostConn` 引用计数。

- [ ] **Step 4: 跑前端全套 + `npx vue-tsc --noEmit` 并提交**

---

### Task 5: roadmap

**Files:**
- Modify: `docs/roadmap.md`

- [ ] **Step 1: 如实标注**

第 28 项四条勾选，并注明：只读浏览 + 单文件传输（无递归下载、无断点续传、无目录同步）；上传默认拒绝覆盖；大目录会被截断且有上限。读第 24–27 项的写法对齐诚实度。

- [ ] **Step 2: 提交**
