# 桌面崩溃窗口会话恢复（仅本地会话）

> **Status**: draft
> **Date**: 2026-06-16
> **Scope**: 桌面端窗口结构 + per-pane cwd + AI 会话外部续接元数据
> **Out of scope**: remote/mirror 会话、scrollback 回放、PTY 真生存（tmux 风格）
> **No protocol change**: 不动 `internal/proto`、`internal/session`、`internal/ringbuf`

## 1. 目标 / 非目标

### 目标

- 桌面 app 退出后再启动，给用户一个 dialog 让 ta 决定要不要恢复上次的 tab/pane 结构。
- 「恢复」= 用同样的 shell 在同样的 cwd 起新 PTY；不是真的让上次的进程活过来。
- 对识别为 AI（claude / codex / aider）的 pane，在 shell prompt 就绪后**自动续接 AI 会话**：
  - claude / codex：复用启动时被动观察到的 AI 端 session id 拼 resume 命令。
  - aider：直接重发上次的命令行（aider 自己读 `.aider.chat.history.md`，cwd 续）。
- 没识别为 AI / AI sid 没抓到的 pane 也照样恢复结构（只是不 inject 命令）；用户用 shell history `↑` 自己调。

### 非目标

- 不持久化 scrollback；恢复出来的 pane 看不到上次崩前的输出。
- 不让 PTY 真的活过崩溃：不引入独立 daemon、不改 `ptyhost` 进程模型。
- 不为 alias / function 做识别（v1 走 graceful degrade；详见 §10）。
- 不恢复 remote/mirror 会话；mirror 会话生命周期由远端 relay 管，本机崩溃不影响。
- 不动 proto；AI sid 是桌面本地恢复元数据，不广播给手机/web。
- 不为 gemini 做适配（v1 当作未知 AI 处理）。

## 2. 背景

### 现状

- `desktop/relay_host.go::NewSession` 在桌面进程内 fork PTY，子 shell 是桌面进程的子进程。桌面进程退出 → 子进程收 SIGHUP 死。
- 窗口结构（tab/pane layout、`Tab.layout`、`Pane.sessionId`、cwd ratio）是纯前端 `App.vue::tabs.value` 的 reactive 状态，**未持久化**。
- AI 命令识别走 `internal/session/applyOSC133Locked` → `ClassifyCommand`：在 OSC 133;C payload 上看 first token basename。
- shell integration（`desktop/shellintegration/snippets/atterm.zsh|bash|fish|ps1`）在 shell preexec 时 emit `\x1b]133;C;<command>\x07`：
  - zsh: `${2:-$1}`（alias 已展开）
  - bash: `$BASH_COMMAND`（alias 已展开）
  - fish: `$cmd`（raw 用户输入）
  - pwsh: 无 payload
- AI title 已有先例（`docs/superpowers/specs/2026-06-10-ai-session-osc-title-design.md`）：纯被动 PTY 字节流解析 OSC 0/1/2，复用 META 广播。本设计对 AI **session id** 不能照搬，因为 claude / codex 不通过 OSC 输出 session id。

### AI session id 的真相

调研 claude / codex / gemini / aider 后：

| CLI | 数据目录 | 文件名形式 | 启动时能否外部观察到 id |
|---|---|---|---|
| claude | `~/.claude/projects/<cwd-编码>/` | `<UUID>.jsonl` | ✓ fs snapshot + poll |
| codex | `~/.codex/sessions/YYYY/MM/DD/` | `rollout-<ISO>-<UUID>.jsonl` | ✓ 同上，UUID 从文件名尾段抽 |
| gemini | 未稳定 | 未稳定 | ✗ v1 不做 |
| aider | cwd 里 `.aider.chat.history.md` | 无 UUID | n/a（cwd 续，不需要 id） |

**关键约束（用户明确）**：atterm 不能"主动给 CLI 注入 id"（即不走 `claude --session-id=<X>` 之类）。AT Term 是**纯观察者**。两层 id 完全解耦：
- AT Term 自己的 `SessionInfo.ID`（PTY/relay UUID）→ proto 层。
- AI 端 session id（claude / codex 自己生成的 UUID）→ 仅 recovery.json 本地元数据，不进 proto。

## 3. 整体架构

```
                ┌────────────────────────────────────────────────┐
                │ desktop frontend (App.vue + composables)        │
                │                                                 │
                │  tabs/panes reactive state                      │
                │       │ debounce 500ms on structural change     │
                │       │ + 5s heartbeat on cwd/title change      │
                │       ▼                                         │
                │  buildRecoverySnapshot() → JSON                 │
                └───────┼─────────────────────────────────────────┘
                        │ Wails: SaveRecoverySnapshot(json)
                        ▼
                ┌─────────────────────────────────────────────────┐
                │ desktop/recovery.go (NEW)                        │
                │  - atomic write to ~/.config/atterm/recovery.json│
                │  - load on startup                               │
                │  - clean_shutdown flag two-phase write           │
                └─────────────────────────────────────────────────┘
                        ▲
                        │ Wails: LoadRecoverySnapshot() → snapshot
                        │
                ┌───────┼─────────────────────────────────────────┐
                │ App.vue.onMounted (bootStage = "load_recovery") │
                │  if (snapshot.tabs.length > 0):                 │
                │    RecoveryDialog.open(snapshot)                │
                │  else:                                          │
                │    startNewTab() (existing path)                │
                └─────────────────────────────────────────────────┘
                        │ user picks subset
                        ▼
                ┌─────────────────────────────────────────────────┐
                │ executeRestore(picks): for each pane            │
                │  1. NewSession({shell, args, cwd}) → PTY +      │
                │     optional AI sniffer goroutine started       │
                │  2. wait first OSC 133;A from this pane         │
                │  3. if pane.ai.sid != "" → PTY.Write(resume cmd)│
                └─────────────────────────────────────────────────┘

                ┌─────────────────────────────────────────────────┐
                │ desktop/ai_sid_sniff.go (NEW)                    │
                │  per-CLI snapshot before fork + polling 30s      │
                │  on capture → EventsEmit("recovery:ai-sid", ...) │
                └─────────────────────────────────────────────────┘
```

设计要点：

1. **不动 proto / internal/session / internal/ringbuf**：纯桌面层增量。
2. **AI sid 留在桌面**：是恢复元数据，不进 SessionInfo 广播。
3. **fork 的是 shell，不是 AI**：恢复时同样 fork shell，AI 命令通过 PTY.Write 注入。
4. **AI sid 嗅探独立于 ClassifyCommand**：嗅探目录由 CLI 二进制行为决定，不受别名影响；但**触发嗅探的判定**依赖 ClassifyCommand（识别 miss = 不嗅，恢复时不 inject）。
5. **clean shutdown 也写快照**：每次启动一律弹 dialog（用户选的策略）；clean flag 只用于 dialog 文案区分。

## 4. recovery.json schema

路径：`~/.config/atterm/recovery.json`（沿用 `UserConfigDir/atterm`）。

```json
{
  "version": 1,
  "host_id": "<atterm host_id>",
  "clean_shutdown": true,
  "saved_at_unix": 1750000000,
  "active_tab_id": "t-2",
  "tabs": [
    {
      "id": "t-1",
      "layout": "vertical",
      "active_pane_idx": 0,
      "col_ratio": 0.5,
      "row_ratio": 0.5,
      "panes": [
        {
          "slot": 0,
          "shell": "/bin/zsh",
          "shell_args": [],
          "last_cwd": "/Users/me/code/foo",
          "session_type": "shell",
          "last_command_line": "",
          "title": "code/foo",
          "ai": null
        },
        {
          "slot": 1,
          "shell": "/bin/zsh",
          "shell_args": [],
          "last_cwd": "/Users/me/code/bar",
          "session_type": "ai",
          "last_command_line": "claude --model sonnet",
          "title": "Refactoring auth",
          "ai": {
            "kind": "claude",
            "session_id": "0d03a640-2884-41bb-84b1-be79969a114a",
            "captured_at_unix": 1749999800
          }
        }
      ]
    }
  ]
}
```

字段说明：

| 字段 | 来源 | 用途 |
|---|---|---|
| `version` | 写死 `1` | 未来 schema bump 拒绝旧版本（视为「无快照」，正常 startNewTab） |
| `host_id` | `hostid.Get()` | 加载时不匹配 → 忽略文件（防同步到别的机器） |
| `clean_shutdown` | OnBeforeClose 成功完成置 `true`；加载时立即写回 `false` | dialog 文案 |
| `saved_at_unix` | 写盘 `time.Now().Unix()` | 显示「N 分钟前」+ 14 天 TTL（超过即视为无快照、删除文件） |
| `active_tab_id` | `currentTabId.value` | 恢复后默认 focus |
| `tabs[].id` | snapshot 当时的 tab id（仅作映射参考；恢复时生成新 id） | active_tab_id 映射 |
| `tabs[].layout` / `active_pane_idx` / `col_ratio` / `row_ratio` | 直接镜像 `Tab` 类型 | 还原 layout |
| `panes[].slot` | pane 在 `Tab.panes[]` 的下标 | 还原插槽 |
| `panes[].shell` / `shell_args` | 创建 PTY 时传入的 `NewSessionReq.Command/Args`（即 shell 本身） | fork shell 用 |
| `panes[].last_cwd` | 最近的 META 带来的 cwd | fork PTY 时设 cwd |
| `panes[].session_type` | 最近一次 OSC 133;C → `ClassifyCommand` 结果 | dialog 标签 + 决定是否 inject resume |
| `panes[].last_command_line` | 最近一次 OSC 133;C 的 payload（即 `MetaPayload.CurrentCommand`） | aider 续接用 / dialog 显示 |
| `panes[].title` | 最近的 `SessionInfo.Title`（OSC 0/1/2 抓的） | dialog 显示 |
| `panes[].ai.kind` | `claude` / `codex` / `aider`；其它为 `null` | 决定 resume 命令拼法 |
| `panes[].ai.session_id` | sniffer 抓到的 AI 端 UUID；超时未抓 = `""` | 拼 resume |
| `panes[].ai.captured_at_unix` | 抓到的时间 | dialog 显示「抓到 X 分钟前」 |

## 5. 落盘协议

### 5.1 写入触发

**「结构性变化」（debounce 500ms 写）**：
- `tabs.value` push / splice（开/关 tab、改顺序）
- `Tab.layout` / `activePaneIdx` / `colRatio` / `rowRatio` 变化
- `Tab.panes` 任何元素变化（split、close pane、picker 选）
- AI sid 嗅探 callback 命中

**「内容性变化」（5s 心跳 debounce 写）**：
- `onMeta` 收到 cwd 变化 → `pane.last_cwd` 更新
- `onMeta` 收到 title 变化 → `pane.title` 更新
- `onMeta` 收到 currentCommand 变化 → `pane.last_command_line` 更新
- `onMeta` 收到 type 变化 → `pane.session_type` 更新

差异化的理由：cwd / title 在 shell 中频繁变（每次 `cd` / 每条命令）；用 5s 兜底而不是 500ms debounce，避免一个活跃 shell 用户每分钟几十次磁盘写。结构性变化是用户显式动作，每次都写得起。

### 5.2 写入实现

`desktop/recovery.go`:

```go
func (r *RecoveryStore) Save(snap RecoverySnapshot) error {
    if err := validate(snap); err != nil { return err }
    blob, err := json.Marshal(snap)
    if err != nil { return err }
    if len(blob) > 256*1024 { return errSnapshotTooLarge }  // sanity cap
    tmp := r.path + ".tmp"
    if err := os.WriteFile(tmp, blob, 0o600); err != nil { return err }
    f, _ := os.Open(tmp)
    _ = f.Sync()  // best-effort fsync
    _ = f.Close()
    return os.Rename(tmp, r.path)  // POSIX atomic
}
```

### 5.3 启动加载

`desktop/recovery.go::Load`:

1. 读文件，JSON parse；任何错误 → 返回空 snapshot + log warn。
2. `version != 1` / `host_id != hostid.Get()` / `saved_at_unix < now - 14d` → 删除文件 + 返回空。
3. 若返回的不是空 snapshot：**立刻**原子写回同一份内容但 `clean_shutdown=false`（防 dialog 期间二次崩溃后下次启动以为是 clean 的）。空 snapshot 跳过这步。
4. 返回 snapshot 给前端。这次写回走独立路径，**不**经 debounce。

### 5.4 clean_shutdown 两阶段

```
launch:
  snap = recovery.Load()
  // Load 已经把 clean_shutdown 置 false 写回了

OnBeforeClose (用户在 ConfirmQuitDialog 点 Quit 后):
  app.MarkCleanShutdown()
    → 内部一次性 recovery.Save({...currentSnapshot, clean_shutdown: true})
    → 同步等待返回（OnBeforeClose 之后没机会再写盘）
    → 然后 wails 才进入真退出
```

任何崩溃 / 强制 kill / power loss / panic → 文件里 `clean_shutdown` 维持 false，dialog 显示「上次异常结束」。

`MarkCleanShutdown` 是单独的 binding（§7.2），不依赖前端 debounce queue 已 flush — 它读 atterm 进程当前内存里维护的 lastSavedSnapshot 副本，做一次完整 Save。前端的 debounce 仍然继续推迟到清除，但这次写入由 MarkCleanShutdown 保证不丢。

## 6. AI sid 嗅探器（`desktop/ai_sid_sniff.go`）

### 6.1 适配器表

```go
type aiSniffSpec struct {
    Kind       string
    WatchDir   func(cwd string, now time.Time) string  // nil = 不嗅（aider）
    NewFile    func(name string) (sid string, ok bool)
    ResumeArgs func(sid string) []string  // sid="" 时返回 nil
}

var aiSniffers = map[string]aiSniffSpec{
    "claude": {
        Kind:       "claude",
        WatchDir:   claudeWatchDir,
        NewFile:    claudeParseSid,
        ResumeArgs: func(sid string) []string { return []string{"--resume", sid} },
    },
    "codex": {
        Kind:       "codex",
        WatchDir:   codexWatchDir,
        NewFile:    codexParseSid,
        ResumeArgs: func(sid string) []string { return []string{"resume", sid} },
    },
    "aider": {
        Kind:       "aider",
        WatchDir:   nil,  // 不嗅
        NewFile:    nil,
        ResumeArgs: func(_ string) []string { return nil },
    },
}
```

### 6.2 watch dir 推断

```go
// claude: ~/.claude/projects/<cwd-编码>/ ; cwd '/' → '-'
func claudeWatchDir(cwd string, _ time.Time) string {
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".claude", "projects", strings.ReplaceAll(cwd, "/", "-"))
}

// codex: ~/.codex/sessions/YYYY/MM/DD/  (按 wall clock local 日期)
func codexWatchDir(_ string, now time.Time) string {
    home, _ := os.UserHomeDir()
    y, m, d := now.Date()
    return filepath.Join(home, ".codex", "sessions",
        fmt.Sprintf("%04d", y), fmt.Sprintf("%02d", m), fmt.Sprintf("%02d", d))
}
```

### 6.3 文件名 → sid

```go
// "<UUID>.jsonl" → UUID（严格 RFC4122 验证）
func claudeParseSid(name string) (string, bool) {
    if !strings.HasSuffix(name, ".jsonl") { return "", false }
    stem := strings.TrimSuffix(name, ".jsonl")
    if _, err := uuid.Parse(stem); err != nil { return "", false }
    return stem, true
}

// "rollout-<ISO ts>-<UUID>.jsonl" → 末尾 36 字符
func codexParseSid(name string) (string, bool) {
    if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
        return "", false
    }
    body := strings.TrimSuffix(strings.TrimPrefix(name, "rollout-"), ".jsonl")
    if len(body) < 36 { return "", false }
    sid := body[len(body)-36:]
    if _, err := uuid.Parse(sid); err != nil { return "", false }
    return sid, true
}
```

### 6.4 嗅探时序

`relayHost.NewSession` 增量扩展（详见 §7）：

```
NewSession(req):
  if req.AIKind ∈ {claude, codex}:
    spec = aiSniffers[req.AIKind]
    dir  = spec.WatchDir(req.Cwd, time.Now())
    os.MkdirAll(dir, 0o700)
    before = snapshotNames(dir)        // basename set
  ptyhost.Open(...)                     // existing
  // 嗅 enabled = spec.WatchDir != nil (aider 永远 false)
  if spec.WatchDir != nil:
    go sniffLoop(ctx, dir, before, spec, sessionID)
       backoff = 100ms, 200ms, 400ms, 800ms, 1.6s, 3.2s, then cap 3.2s
       total budget = 30s
       each tick:
         now = snapshotNames(dir)
         diff = now - before
         if len(diff) == 1:
           if sid, ok := spec.NewFile(diff[0]); ok:
             app.aiSidCaptured(sessionID, spec.Kind, sid)  // → EventsEmit
             return
         elif len(diff) >= 2:
           log warn "ai sniff ambiguous"
           return  // 保守：不抓
       on timeout: log info, no emit
```

回调流向：

```
sniffLoop → app.aiSidCaptured(sid, kind, aiSid)
          → wailsruntime.EventsEmit("recovery:ai-sid", { session_id, kind, ai_session_id })
          → 前端 useRecoverySnapshot.ts handler 更新内存 pane.ai
          → 触发结构性变化 → debounce 500ms 写盘
```

### 6.5 嗅探取消

- pane 关闭 / `ctx.Done()` / `NewSession` 失败 → 立即 cancel goroutine。
- 嗅探目标目录不可读（权限、不存在且无法 mkdir）→ log info 后退出，不嗅。
- 误抓防护：同一时刻同目录冒出 ≥ 2 个新文件视为"歧义"放弃；用户最多丢一次续接，下次重启再来过。

## 7. Wails binding 改动

### 7.1 扩展现有 `NewSession`

`desktop/app.go::NewSessionReq` 加两个 optional 字段，零值时行为完全等价：

```go
type NewSessionReq struct {
    Command string   `json:"command"`
    Args    []string `json:"args,omitempty"`
    Cwd     string   `json:"cwd,omitempty"`
    Cols    uint16   `json:"cols,omitempty"`
    Rows    uint16   `json:"rows,omitempty"`

    // AIKind: 前端基于用户输入的命令调本地 classifyAIKind() 算出，命中
    // {claude, codex, aider} 时填；为空时 NewSession 行为不变。
    AIKind string `json:"ai_kind,omitempty"`
    // InitialAISessionID: 恢复路径下，前端把上次抓到的 AI sid 传回来。Go 端
    // 直接把它放进 ResumeContext，不会主动用来"指定" CLI 的 id（只在恢复
    // 后 PTY.Write 时拼 resume 命令）。
    InitialAISessionID string `json:"initial_ai_session_id,omitempty"`
}
```

理由：在 `NewSession` 旁加新方法是误工。`NewSessionReq` 是 Wails binding 输入参数，**不是 proto 帧**，扩字段不受红线 #4 约束。

### 7.2 新增 binding

```go
// LoadRecoverySnapshot 返回 recovery.json 的内容，若不存在或不合法
// 则返回 zero value。同时 side-effect: 把 clean_shutdown 写回 false。
func (a *App) LoadRecoverySnapshot() (RecoverySnapshot, error)

// SaveRecoverySnapshot 把前端传来的 JSON 原子写到磁盘。
// 用 JSON 字符串而非结构体，前端直接 stringify 内存里的 snapshot。
func (a *App) SaveRecoverySnapshot(payload string) error

// DiscardRecoverySnapshot 删除 recovery.json（用户在 dialog 选「全部丢弃」）。
func (a *App) DiscardRecoverySnapshot() error

// MarkCleanShutdown 在 ConfirmQuit / OnBeforeClose 真退出前调一次，
// 把 clean_shutdown 置 true 后再让 wails 关窗。
func (a *App) MarkCleanShutdown() error
```

### 7.3 新事件

```
"recovery:ai-sid"  →  { session_id: string, kind: string, ai_session_id: string }
```

前端 `useRecoverySnapshot.ts` 订阅这个事件，关联到 pane 后更新 `pane.ai`。

## 8. 前端实现要点

### 8.1 新文件

- `desktop/frontend/src/composables/useRecoverySnapshot.ts` — 监听 tabs/pane 变化、订阅 `recovery:ai-sid`、维护内存中的 snapshot 结构、debounce 写盘。
- `desktop/frontend/src/components/RecoveryDialog.vue` — 启动弹窗。
- `desktop/frontend/src/lib/aiKind.ts` — `classifyAIKind(command: string): "claude" | "codex" | "aider" | ""`，与 Go 端 `ClassifyCommand` 同步表。

### 8.2 启动流程改动

`App.vue::onMounted`：

```ts
bootStage = "refreshTerminalTheme"; await refreshTerminalTheme();
bootStage = "getEndpoint";          endpoint.value = await getEndpoint();
bootStage = "getHostInfo";          host.value = await getHostInfo();
bootStage = "loadRecoverySnapshot"; const recoverySnap = await loadRecoverySnapshot();
bootStage = "connectLocalSessionList"; localSessionListConn = ...;
bootStage = "refreshRelayConfig";   await refreshRelayConfig();

if (!autoStarted) {
  autoStarted = true;
  if (recoverySnap.tabs.length > 0 && recoveryEnabled.value) {
    recoveryDialogState.value = { open: true, snapshot: recoverySnap };
    // 不调 startNewTab() —— 让 dialog 决定
  } else {
    await startNewTab();
  }
}
```

`loadRecoverySnapshot` 失败按现有 bootStage 错误隔离（红线 #19）报错；不阻塞启动。

### 8.3 dialog 行为

```
RecoveryDialog props:
  snapshot: RecoverySnapshot

state:
  picks: Map<tabId, boolean>  // 默认全 true
  expandedTabs: Set<tabId>    // sessionStorage 记忆

emits:
  restore(picks: TabPick[])  // 主按钮
  discardAll()               // 次按钮 + ✕ 都等价

主按钮文案: 选满 → "恢复全部 N 个" / 部分 → "恢复 X 个" / 空 → 禁用
```

每个 tab 一行；展开后列 pane 详情：
- shell pane：`/bin/zsh · code/foo · (无 AI)`
- ai pane 抓到 sid：`claude · Refactoring auth · 可续接(1h 前)`
- ai pane 没抓到：`codex · 无续接(超时) · 重启后用 ↑ 调上次命令`
- 未识别 AI（别名 miss）：`/bin/zsh · 上次跑了 "cc --foo" · 不自动续接`

### 8.4 executeRestore

```ts
async function executeRestore(picks: TabPick[]) {
  recoveryDialogState.value = { open: false };
  for (const tab of picks) {
    const t: Tab = {
      id: newId(),  // 新 id，不复用旧的
      layout: tab.layout,
      activePaneIdx: tab.active_pane_idx,
      colRatio: tab.col_ratio,
      rowRatio: tab.row_ratio,
      panes: [],
    };
    const want = PANE_COUNT[tab.layout];
    for (let i = 0; i < want; i++) {
      const snap = tab.panes.find(p => p.slot === i);
      if (!snap) {
        t.panes[i] = { sessionId: null, remote: false };
        continue;
      }
      try {
        const resp = await newSession({
          command: snap.shell,
          args: snap.shell_args,
          cwd: snap.last_cwd,
          cols: predictCellDims(tab.layout).cols,
          rows: predictCellDims(tab.layout).rows,
          ai_kind: snap.ai?.kind ?? "",
          initial_ai_session_id: snap.ai?.session_id ?? "",
        });
        t.panes[i] = { sessionId: resp.session_id, remote: false };
        scheduleResumeInject(resp.session_id, snap);  // §8.5
      } catch (e) {
        t.panes[i] = { sessionId: null, remote: false };
        // pane 留空，dialog 关后用户能看到 "(已失败)"
      }
    }
    tabs.value.push(t);
  }
  // 还原 active tab：通过 snapshot.active_tab_id → 新 tab id
  const activeIdx = picks.findIndex(p => p.id === recoverySnap.activeTabId);
  if (activeIdx >= 0) gotoTab(tabs.value[tabs.value.length - picks.length + activeIdx].id);
  else if (tabs.value.length > 0) gotoTab(tabs.value[0].id);
}
```

**串行**而不是并行：避免同时 fork 多个 PTY 撞 winsize / terminfo 竞争（红线 #6）。N=10 大约 500ms 总开销，可接受。

### 8.5 AI resume 命令注入

```ts
function scheduleResumeInject(sessionId: string, snap: PaneSnapshot) {
  if (snap.session_type !== "ai") return;
  if (!snap.ai) return;
  const args = computeResumeArgs(snap.ai.kind, snap.ai.session_id, snap.last_command_line);
  if (args === null) return;  // 不 inject
  const line = args.join(" ") + "\n";
  // 等这条 session 的第一个 OSC 133;A
  awaitFirstPromptReady(sessionId).then(() => sendInputToSession(localEndpoint, sessionId, line));
}

function computeResumeArgs(
  kind: string, sid: string, lastCmdLine: string,
): string[] | null {
  if (kind === "claude" && sid) return ["claude", "--resume", sid];
  if (kind === "codex"  && sid) return ["codex", "resume", sid];
  if (kind === "aider" && lastCmdLine) return [lastCmdLine];  // 直发原命令
  return null;
}

function awaitFirstPromptReady(sessionId: string): Promise<"ready" | "timeout"> {
  // 监听 SessionConnection 上的 onMeta，task_state 第一次变成 `waiting_input`
  // 即 resolve("ready")。
  // 5s 超时 → resolve("timeout")，scheduleResumeInject 收到 timeout 后 **abort**
  // （不 inject，避免把字符发到还没就绪的 shell 上丢字）。abort 时 toast 一条
  // "${kind} 自动续接超时，请按 ↑ 调出上次命令"。
}
```

「prompt ready」用 OSC 133;A 的简单代理：`SessionInfo.task_state` 从 `disconnected/running` 变成 `waiting_input` 即可（OSC 133;A 在 atterm 现状下会更新 `task_state`）。super-slow shell（cold cache / OPAQUE 卡顿）走 abort 路径，不丢命令字符。

### 8.6 Settings 入口

`SettingsGeneral.vue` 加一项：

```
启动时检测并提示恢复上次会话      [ on / off ]
```

`appConfig` 加一个 nil-able pointer 字段，默认 true：

```go
RecoveryDialogEnabled *bool `json:"recovery_dialog_enabled,omitempty"`

func (c appConfig) RecoveryDialogEnabledOrDefault() bool {
    if c.RecoveryDialogEnabled == nil { return true }
    return *c.RecoveryDialogEnabled
}
```

关掉就跳过 dialog 流，直接 `startNewTab()`；recovery.json 仍然写（用户随时打开开关就有得恢复）。

### 8.7 i18n

按红线 #11 在 `desktop/frontend/src/i18n/messages/zh-CN.ts` 与 `en.ts` 同时加：

```
recovery.dialog.title
recovery.dialog.subtitleClean
recovery.dialog.subtitleUnclean
recovery.dialog.savedAgo  ({ minutes })
recovery.dialog.btnRestoreAll  ({ count })
recovery.dialog.btnRestoreSelected  ({ count })
recovery.dialog.btnDiscard
recovery.dialog.badgeResumable      // "可续接(N 前)" — pane.ai.session_id 非空 或 aider
recovery.dialog.badgeFresh          // "不自动续接(超时)" — session_type=ai 但 sid 空
recovery.dialog.badgeShell          // "(无 AI)" — session_type=shell
recovery.dialog.badgeUnclassified   // "不自动续接 (未识别为 AI)" — alias miss
recovery.pane.failed
settings.general.recoveryEnabled
settings.general.recoveryEnabledDesc
```

## 9. 与现有红线的兼容性核对

| 红线 | 影响 | 处理 |
|---|---|---|
| #1 本地优先 | 本设计仅本地，与远端无关 | ✓ |
| #2 Lazy 上传 | snapshot 不发到 relay | ✓ |
| #3 session id 是权威 | atterm sid 不变；AI sid 独立 namespace | ✓ |
| #4 协议向后兼容 | 不动 proto | ✓ |
| #5 internal 包不依赖 desktop | 所有新代码在 desktop/ 下 | ✓ |
| #6 winsize at fork | NewSession 路径不变，仍走 predictCellDims | ✓ |
| #11 i18n 双语 | dialog 与 Settings 文案中英双套 | ✓（§8.7） |
| #14 OSC 133 单点 sticky | 不改 `applyOSC133Locked`；仅在前端读取既有 type / currentCommand / task_state | ✓ |
| #19 启动 bootStage 分阶段 | `loadRecoverySnapshot` 加一个阶段标签 | ✓（§8.2） |
| #21 account_key 不出主线程 | recovery.json 不含密钥；AI sid 是 CLI 自己写出来的 UUID，非密钥材料 | ✓ |

## 10. 别名 / wrapper 已知限制

**别名识别不到 AI** → graceful degrade：
- `ClassifyCommand` 不变，v1 不引入用户级 allowlist。
- 命中 case：fish alias、shell function、wrapper script、自定义 PATH 名。
- 影响：sniff 不启动 → 恢复时 `pane.ai = null` → 只 fork shell 到 cwd，不 inject。
- dialog 诚实显示 `不自动续接`，要求用户用 `↑` 调 shell history。

**fish 用户特别说明**：fish 的 `$cmd` 是 raw input，不展开 alias。这是 shell integration 限制，v1 不解决；fish 用户敲 `claude` 直接命令则一切正常。

**未来增强**（不在 v1）：
- Settings 加 AI 别名映射表（用户配 `c → claude`）。
- 用 `pgrep -P` / `/proc/<pid>/children` 反查 shell 子进程 argv。

## 11. 边界与失败模式

| 场景 | 行为 |
|---|---|
| recovery.json 损坏 / version mismatch | 删除文件 + log warn + 走 startNewTab |
| host_id 不匹配（云同步过来的） | 同上 |
| `saved_at_unix < now - 14d` | 同上 |
| sniffer 30s 超时 | log info；pane.ai.session_id 留空；恢复时不 inject |
| sniffer 看到 ≥2 个新文件 | log warn；不抓；恢复时不 inject |
| sniffer 目录无法 mkdir | log info；不嗅；该 pane 永远 `ai = null` |
| 恢复时 shell 二进制不存在（用户换了 shell / 卸载了） | NewSession 错 errno；该 pane 留空，UI 显示 `(已失败)` |
| 恢复时 last_cwd 不存在 | NewSession 用 home 兜底（`relayHost.NewSession` 现有行为） |
| 恢复时 AI CLI 不存在（用户卸载了 codex） | shell 起得来；inject `codex resume <sid>` 后 shell 报 `command not found`，用户看到 |
| 多个 atterm 实例同时跑（罕见） | 每个实例都向同一 recovery.json 写；最后一个 Save 胜出；崩溃恢复只对最后写入的实例有意义。v1 接受这个限制 |
| dialog 期间用户直接退 app | 视为 discard，下次启动还会看到同一 dialog（snapshot 仍然存在） |
| 用户在 Settings 关掉 recovery_dialog | dialog 流跳过；snapshot 继续写；重新打开开关后下次启动还能恢复一次 |

## 12. 测试计划

### 12.1 Go 单元测试

- `desktop/recovery_test.go`：
  - Save/Load round-trip（version、host_id、clean_shutdown、TTL、损坏处理）
  - 原子写：写一半进程被 kill 不会留下半截 recovery.json
  - clean_shutdown 两阶段：Load 立刻写回 false
- `desktop/ai_sid_sniff_test.go`：
  - claudeParseSid / codexParseSid 各种合法 + 非法文件名
  - sniffLoop 在 fake dir 上能命中、能 timeout、能识别 ambiguous
  - cancel 路径

### 12.2 前端单元测试

- `useRecoverySnapshot.test.ts`：
  - debounce 500ms（结构性）/ 5s（cwd/title）行为
  - `recovery:ai-sid` 事件正确合入 snapshot
- `RecoveryDialog.test.ts`：
  - 全选 / 取消勾选 / 主按钮文案 / 主按钮禁用条件
  - discard 后 emit
- `aiKind.test.ts`：
  - 与 Go 端 `ClassifyCommand` 对齐的常见 case

### 12.3 手动 / e2e

- 启动 atterm → 开 3 个 tab（含 vertical split + claude + codex）→ `kill -9` 桌面进程 → 重启 → dialog 显示 → 恢复 → claude 在 prompt 后看到 `--resume <sid>` 自动跑、续上历史会话；codex 同理；shell tab 起到 home 不带 inject
- 反向：clean Quit → 同样 dialog 出现，文案显「上次正常退出」
- fish + alias `c='claude'` → 敲 `c` 跑 claude → kill → 重启 → dialog 标「不自动续接」，shell 起来按 ↑ 出 `c`

## 13. 不做的事（YAGNI）

- 多 atterm 实例的 snapshot 隔离（按 PID 多文件 / lockfile）
- snapshot 加密（不含密钥材料，本地文件靠 OS 权限）
- 用 fsnotify 替换 polling（macOS FSEvent 2s 合并延迟，反而更糟）
- 对 gemini / mistral / ollama / 其他 AI CLI 做适配
- alias 用户级 allowlist UI（§10 留作 follow-up）
- 自动按 stack trace 上报「上次崩溃原因」
- 让 PTY 进程真正 survive crash（架构性改动，留作未来）
