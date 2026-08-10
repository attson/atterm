# 桌面挂件（Desk Widget）：置顶悬浮会话窗口

Date: 2026-08-10
Status: Implemented

## Background

atterm 已经拥有一套完整的会话状态模型：`internal/session` 通过 OSC 133 推导命令生命周期
（`running` / `waiting_input` / `completed` / `failed` / `idle`），`desktop/ai_sid_sniff.go`
识别 claude / codex / aider，META 帧把 `type` + `summary` 同步到所有客户端。

但这套状态**只在 AT Term 窗口里可见**。用户启动一个长任务后切到浏览器/编辑器，就必须周期性
切回来确认"跑完没有 / 是不是在等我确认"。这正是桌面宠物类工具解决的问题。

同类实现调研（2026-08-10）：

| 项目 | 形态 | 状态来源 |
|---|---|---|
| [SHIN620265/claude-pet](https://github.com/SHIN620265/claude-pet) | 宠物 + 最多 3 张状态卡 | Claude Code hooks（PowerShell） |
| [handsome-rich/claude-buddy](https://github.com/handsome-rich/claude-buddy) | 悬浮列表 + 灵动岛 | `~/.claude/settings.json` hooks |
| [rullerzhou-afk/clawd-on-desk](https://github.com/rullerzhou-afk/clawd-on-desk) | 像素宠物 + HUD + dashboard | 20+ agent 各自的 hooks / JSONL 轮询 |
| [gmr/claude-status](https://github.com/gmr/claude-status) | 菜单栏 + WidgetKit | Claude Code plugin 写 `.cstatus` |

**共同点：它们都得逆向猜状态**——装 hook、轮询 JSONL、解析 statusline payload；换一个 agent
就要再适配一套。atterm 自己就是 PTY 宿主，状态是一等数据：不装任何 hook、不认 agent 品牌，
`vim` / `make` / 任意脚本一视同仁，并且**远程会话**（relay 上其它机器）也在同一个列表里。

## Goals

1. 一个**系统级置顶**的小窗口，盖在其它 app 之上，显示宠物形象 + 会话列表。
2. 生命周期绑 AT Term：主 app 退出，宠物一起退出。
3. 走**插件模式**，在 Settings → 插件 里开关，与 fileExplorer / translate 一致。
4. 单击会话行 → 拉起主窗口并定位到该 tab/pane。
5. 展开 / 折叠两态，位置与折叠态持久化。

## Non-Goals

- **不做**可对话 AI 伙伴（不接任何 LLM API），挂件只反映状态。
- **不做**养成 / 抽卡 / 等级。
- **不做**形态 B（宠物+卡片叠）与形态 C（药丸）——抽象按可扩展设计，但本期只落地形态 A。
- **不做**权限气泡（在宠物窗口里 Allow/Deny）。atterm 的 `waiting_input` 是正则推导的，
  不像 Claude Code hooks 那样有结构化的 permission request 可以回执。
- **不做** iOS / web 端宠物。悬浮窗是桌面 OS 概念。

## Architecture

### 进程模型：同一个二进制，`--pet` 分叉

```
AT Term (主进程)  ──exec──►  AT Term --pet (宠物进程)
   wails.Run(主窗口)            wails.Run(Frameless+AlwaysOnTop+Transparent)
   dist/index.html              dist/pet.html
```

**为什么是同一个二进制而不是第二个可执行文件：** 第二个二进制会要求 CI 产物矩阵
（3 平台）、macOS 单独签名公证、以及自动更新分发它——红线 #8 规定 release asset 必须
过 Ed25519 + SHA256 验签，多一个二进制就多一条必须验签的链路。复用同一个已签名的可执行
文件，这些**全部零改动**。

前端多入口已有先例（`main.web.ts` / `main.capacitor.ts`），新增 `index.pet.html` +
`src/main.pet.ts`，`vite.config.ts` 的 wails target 从单入口改为 `{ index, pet }`，
`go:embed all:frontend/dist` 自动收进去。

### 数据流：宠物进程什么都不连

```
主 app 进程                                      宠物进程
───────────                                      ────────
App.vue 合并后的 sessions                          Go: bufio.Scanner(os.Stdin)
  (local 流 + remote 流 + localListMerge)            → EventsEmit("pet:state", json)
       │                                              → PetApp.vue 渲染
       ├─ projectPetState()  纯函数，可单测
       ├─ platform.petHost.pushState(state)
       └──────────► child.stdin (NDJSON) ─────►

       ◄────────── child.stdout (NDJSON) ──────  {"type":"activate","sessionId":"…"}
   复用通知深链的 session 路由 → 定位 tab/pane
```

**这是被红线 #21 逼出来的，但结果更干净。** 远程会话列表是独立的第二条 WS 流
（`useSessionListStreams`），内容可能是 E2EE 封的、要 `account_key` 才能解；而红线 #21
规定 account_key 不出主线程、不进 URL / 日志 / 第二个进程。让宠物自己去连远程 relay 就
必须把 token 甚至 account_key 交给子进程。

反过来由主 app **推送已合并、已解封的视图**，换来四件事：

1. token / `account_key` 一律不出主进程，宠物只拿到渲染好的字符串。
2. 远程会话照常显示——主 app 早就合并过了。
3. `projectPetState()` 是纯函数（`SessionInfo[] → PetState`），排序、汇总文案、状态归并
   全都能脱离窗口单测，与 `lib/layout.ts` / `lib/sessionMatch.ts` 同一路数。
4. 传输走 stdin/stdout 管道：不开端口、不需鉴权、OS 保证只有父子可见。

### 状态投影

`PetState` 是宠物渲染所需的最小完整快照：

```ts
type PetMood = "idle" | "running" | "waiting" | "failed";

interface PetSessionRow {
  sessionId: string;
  title: string;      // 已经 fallback 过的显示名
  subtitle: string;   // 当前命令 / exit code / 摘要
  state: PetMood;
  kind: string;       // "claude" | "codex" | "" …
  remoteHost: string; // 非空 = 跑在别的机器上
  ageMs: number;      // 运行时长，宠物侧本地 tick 不重算
}

interface PetState {
  mood: PetMood;          // 全局聚合，优先级 waiting > failed > running > idle
  waitingCount: number;
  runningCount: number;
  completedCount: number;
  headline: string;       // "1 个等你输入"
  subline: string;        // "2 个在跑 · 1 个已完成"
  rows: PetSessionRow[];  // 已排序、已截断
}
```

优先级排序：`waiting` → `failed` → `running` → `completed`；同级按最近活动时间倒序。
`rows` 截断到 `PET_MAX_ROWS = 6`，超出部分在汇总行体现。

### 插件化

新增 slot `"companion-window"`。它与现有三个 slot 的区别是**不往主窗口挂 Vue 组件**，
所以 `PluginHost.vue` 必须跳过它——沿用已有的 context-menu 跳过模式（那个 slot 同样是
headless）。

进程生命周期由 `useCompanionWindow.ts` 驱动：watch `store.isPluginEnabled("pet")`，
true 就 `platform.petHost.start()`，false 就 `stop()`。配置持久化复用现成的
`PluginConfig` → `config.json` → `plugin-config-changed` 事件链路。

### 平台细节

Wails v2.12 的 `mac.Options.ActivationPolicy` **整块被注释掉**（`pkg/options/mac/mac.go`
第 3-9 行与第 26 行，全仓无实现），所以宠物进程默认会多一个 Dock 图标。

| 平台 | 问题 | 修法 |
|---|---|---|
| macOS | 多一个 Dock 图标 + 出现在 Cmd-Tab | cgo 调 `[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory]` |
| macOS | 圆角外有白边 | `setOpaque:NO` + `setBackgroundColor:clearColor` |
| Windows | 出现在 Alt-Tab / 任务栏 | `SetWindowLong` 加 `WS_EX_TOOLWINDOW`（`x/sys/windows`，无需 cgo） |
| Linux | 出现在任务栏 | `gtk_window_set_skip_taskbar_hint`（Wails v2 未暴露，本期不做） |

cgo + AppKit 已在 `desktop/pasteboard_files_darwin.go` 使用（`#cgo LDFLAGS: -framework AppKit`），
`main_{darwin,linux,windows}.go` + `platformOptions()` 的分文件模式也是现成的，所以这三段
落进既有结构，不引入新的构建能力。

### macOS 三个"看起来该生效但不生效"的坑（实测踩过）

都在 `desktop/pet_window_darwin.go::atterm_pet_configure`，**改这块前必读**：

1. **必须 `dispatch_async` 到主队列。** Wails 的 `OnStartup` 是从 Go goroutine 回调的，
   AppKit 调用在非主线程上被静默忽略——直接在 `OnStartup` 里调 `setActivationPolicy`
   看起来完全没反应。这是第一次修复失败的原因。
2. **必须在 `wails.Run` 之后设 activation policy。** Wails 在
   `AppDelegate.m::applicationWillFinishLaunching` 里硬编码
   `setActivationPolicy:Regular`，在 `wails.Run` 之前设会被它覆盖。
3. **`mac.WindowIsTranslucent` 不是透明开关，反而是白边的成因。** 它插入一个
   `NSVisualEffectView`（`WailsContext.m:185`，`BlendingModeBehindWindow`），浅色材质
   从圆角外透出来就是白边。真正的透明来自 `setOpaque:NO`——Wails 全程不调它。
   窗口非 opaque 后，macOS 会按内容 alpha 自己算投影并贴合圆角，所以 CSS 里
   **不要**再写 `box-shadow`（会叠加，且被窗口边缘裁切）。

验证方式不要靠截图：终端进程通常没有屏幕录制权限，`screencapture` 只能拍到壁纸和菜单栏，
会得出"窗口没显示"的错误结论。用 PID 对照 System Events 的前台进程列表：

```bash
osascript -e 'tell application "System Events" to get unix id of every process whose background only is false'
```

宠物 PID 不在其中 = accessory 生效。

## Interaction

| 动作 | 行为 |
|---|---|
| 单击宠物 | 展开 ⇄ 折叠，窗口 resize，写进 config |
| 悬停折叠态 | peek 临时展开（不改持久态） |
| 单击会话行 | 主窗口 Show + Focus，定位到该 tab/pane |
| 拖动 | 移动窗口，位置写进 config |
| 右键 | 折叠/展开 · 静音 15/60 分钟 · 隐藏宠物 |
| 升级到 waiting | 宠物起跳 + 角标；折叠态自动 peek 3 秒 |

尺寸：展开 `252 × 172`，折叠 `252 × 54`。

## Error Handling

- **子进程起不来**：`start()` 返回错误 → 前端 toast + 自动把插件置回 disabled，
  与 `PluginHost.reconcile()` 处理加载失败的策略一致（不让用户卡在每次 reconcile 都失败）。
- **子进程意外退出**：主 app 侧 `cmd.Wait()` 返回即标记 stopped。**不自动重启**——
  自动重启会把一个崩溃循环变成无限 fork 循环。用户重新开关一次即可。
- **主 app 退出**：`app.shutdown` 里 kill 子进程；子进程另有 stdin EOF 兜底自杀，
  防止父进程被 SIGKILL 时留下孤儿窗口。
- **推送节流**：状态推送最快 200ms 一次，且内容未变则跳过，避免高频 META 打爆管道。

## Testing

- `lib/petState.test.ts` — 投影纯函数：优先级排序、聚合计数、文案、截断、远程标记。
- `desktop/pet_config_test.go` — PetConfig 默认值 / 校验边界 / JSON 往返。
- `desktop/pet_process_test.go` — 进程管理器的 NDJSON 编解码与 stdout 事件解析
  （不起真窗口，用假的 stdin/stdout pipe）。
- 手动验证：三平台各确认「不出现在 Dock/任务栏/Alt-Tab」+ 置顶 + 透明。

## 命名

插件对用户显示为 **桌面挂件 / Desk Widget**，不是"AI 宠物"。它的状态模型来自 OSC 133，
对 `vim` / `make` / 任意脚本一视同仁——普通 shell 会话同样会出现在列表里，叫"AI 宠物"
既不准确，也把它相对同类工具（只认某一家 agent 的 hook）的优势说小了。

想要纯 AI 视图的用户可以打开 `PetConfig.AIOnly`（Settings → 插件，或挂件自己的右键菜单）。
过滤在主 app 的投影里完成，所以计数、标题、溢出提示描述的都是过滤后的同一批会话；过滤后
为空时标题是"没有 AI 会话"而不是"没有会话"。

**代码内标识仍叫 `pet`**（`PluginID` / `PetConfig` / `pet_*.go` / `petState.ts` / `--pet`）。
这是个短小的内部代号，不出现在任何 UI 文案里；重命名要动 20 多个文件，收益为零。

## Open Questions

无。形态 B / C 与自定义形象包是明确的后续项，不阻塞本期。
