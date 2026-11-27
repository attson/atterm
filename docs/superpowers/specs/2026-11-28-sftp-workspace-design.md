# SFTP 双栏工作区与 SSH 列表搜索 - Design

## 1. 背景

AT Term 已经有保存的 SSH 主机、密钥、跳板链、原生代理、host-key 校验，以及一套
桌面本地文件和 SFTP 文件操作实现。`desktop/sftp_source.go`、`internal/sftpfs/` 和
`desktop/frontend/src/plugins/fileExplorer/sshHostFS.ts` 仍在，但 2026-10 的文件浏览器
改版移除了已保存 SSH 主机的数据源选择入口，导致 SFTP 能力在当前 UI 中不可达。

本次不把 SFTP 重新塞回终端右侧插件，而是增加一个固定、单例的顶层 `SFTP` 工作区。
工作区采用双栏布局，每栏都可以独立选择本机或任意可连接的已保存 SSH 主机，因此支持
本机到远端、远端到本机，以及两台远端主机之间的文件传输。远端到远端的数据始终由
当前 AT Term 桌面进程中转；服务器之间不需要互通，也不会互相获得凭据。

同时补齐 SSH 主机管理窗口的搜索体验：主机页支持窗口内直接键入搜索，密钥页新增
搜索框并支持相同的 type-to-search 行为。

## 2. Goals

1. 桌面端顶栏提供固定的单例 `SFTP` 页签，与终端页签并列，但不进入终端 `Tab` 模型。
2. SFTP 工作区左右两栏均可选择本机或可连接的 saved SSH host。
3. 支持目录导航、当前目录筛选、刷新、创建目录、重命名、删除、文本预览/编辑。
4. 支持文件和目录的双向传输，包括 remote-to-remote，提供进度、取消和逐项结果。
5. 大文件通过 Go 后端流式中转，不把完整内容装进 WebView 内存。
6. 复用现有 SSH 凭据、ProxyJump、原生 HTTP/SOCKS 代理和 known_hosts 安全模型。
7. 主机页和密钥页都能通过显式搜索框和 type-to-search 快速过滤。

## 3. Non-goals

- 不给 Web 或 Capacitor 客户端暴露 saved-host SFTP。saved-host 凭据只在桌面进程中。
- 不修改 Relay `FS_REQUEST` / `FS_RESPONSE` wire 协议；本功能是本机 Wails IPC 路径。
- 不让远端 A 主动连接远端 B，也不把 B 的密码、私钥或代理凭据发送给 A。
- 不把 SFTP 工作区写入终端 recovery snapshot，也不为它创建伪 session。
- 不做断点续传或应用重启后的任务恢复。取消或崩溃后重新开始传输。
- 不跟随或复制符号链接。首版把 symlink 作为明确的 skipped item，避免循环、越界和
  跨平台语义漂移。

## 4. 顶层视图模型

### 4.1 固定页签，不扩展终端 Tab

`desktop/frontend/src/components/TabBar.vue` 增加一个桌面专属的固定 `SFTP` 页签，接收
`sftpOpen` 状态并发出 `toggle-sftp`。它显示在终端动态 tabs 之前，不参与终端 tab 编号、
拖拽排序、关闭、分屏或 session unread 状态。

`App.vue` 把主区域收敛成三种互斥视图：terminal、SFTP、admin。进入任意终端 tab 会关闭
SFTP/admin 视图；进入 SFTP 会关闭 admin；进入 admin 会关闭 SFTP。现有 `tabs`、
`currentTabId`、Web tab snapshot、desktop recovery snapshot 和 `computeCloseTabState` 均
保持只处理终端。

SFTP 视图占用完整工作区，不显示 `TaskSidebar`、右侧插件栏或其 resize handle。TitleBar、
TabBar、Settings 和更新提示仍保留。组件在本次应用运行期间保持挂载，因此切回终端后
两栏的来源、路径和选择仍在；退出应用后不恢复。

### 4.2 组件边界

新增 `desktop/frontend/src/components/sftp/`：

- `SftpWorkspace.vue`：拥有左右栏状态、传输队列、TOFU/冲突/删除确认对话框。
- `SftpPane.vue`：单栏来源选择、路径导航、排序、筛选和文件列表。
- `SftpSourcePicker.vue`：本机 + `ListSFTPHosts` 返回的可连接主机。
- `SftpTransferQueue.vue`：活动、失败、完成任务及取消操作。
- `sftpWorkspaceModel.ts`：路径、选择、排序、传输方向和冲突决策的纯函数。

文件预览与文本编辑复用 fileExplorer 的 `FileEditor.vue` / preview components 和
`FileSystemBridge` 形状；双栏列表不直接复用当前 `FileExplorer.vue`，因为该组件与活动
terminal pane、右侧插件宽度和单一 bridge 生命周期耦合。可复用的路径、内容解码和
preview 判定下沉为独立 helper，避免复制实现。

## 5. 双栏交互

### 5.1 来源与导航

每栏持有：

```ts
interface SftpPaneState {
  source: { kind: "local" } | { kind: "ssh"; hostId: string };
  cwd: string;
  back: string[];
  forward: string[];
  selectedPaths: Set<string>;
  query: string;
  sort: "name" | "modified" | "size";
  direction: "asc" | "desc";
}
```

本机默认目录为用户 home；SSH 主机通过 `pkg/sftp.Client.Getwd()` 取得 login directory，
只有服务器不支持 realpath/getwd 或返回非绝对路径时才回退到 `/`。切换来源会清空该栏
选择与导航历史，并释放旧 SFTP browser 引用，但不会取消已经启动的传输；任务持有
不可变的 endpoint 快照和自己的连接引用。

列表按目录优先展示，支持单选、多选、双击进入目录、Back/Forward/Up、直接输入绝对
路径、刷新和当前目录即时筛选。加载、空目录、筛选无结果、截断目录和连接失败分别有
明确状态。

### 5.2 文件操作

每栏提供图标按钮和右键菜单：新建目录、重命名、删除、刷新、预览/编辑。远端删除没有
回收站：文件和空目录需普通永久删除确认；非空目录必须二次确认后才以 recursive=true
执行。路径落入 `.ssh`、`.gnupg`、`.aws` 等现有 deny list 时继续拒绝，专用 SFTP 页面
不绕开现有安全门。

文本编辑沿用 5 MiB 上限和 modtime CAS。发生 stale write 时不覆盖，提示重新加载或另存。
二进制及超限文件显示 metadata，不进入文本编辑器。

### 5.3 传输入口

以下入口创建同一种 transfer request：

- 中间的左箭头/右箭头按钮，把当前栏选择复制到另一栏当前目录。
- 把一栏的内部文件拖到另一栏。
- 右键菜单“复制到左侧/右侧”。
- OS 文件拖入某栏时，从本机原始路径上传到该栏当前目录。

内部 drag payload 只携带来源 endpoint id 和 path，不携带文件内容或凭据。目标和来源为
同一 endpoint + 同一路径时拒绝；目录不能复制进自身或后代。

## 6. 流式传输后端

### 6.1 API

新增 `desktop/file_transfer.go`，由 `App` 暴露：

```go
type FileEndpoint struct {
    Kind   string `json:"kind"`    // local | ssh
    HostID string `json:"host_id"` // only for ssh
}

type StartFileTransferReq struct {
    Source        FileEndpoint `json:"source"`
    Destination   FileEndpoint `json:"destination"`
    Paths         []string     `json:"paths"`
    DestinationDir string      `json:"destination_dir"`
    Conflict      string       `json:"conflict"` // skip | replace | rename
    AcceptedHostKey AcceptedHostKey `json:"accepted_host_key"`
}

func (a *App) StartFileTransfer(req StartFileTransferReq) (FileTransferJob, error)
func (a *App) CancelFileTransfer(id string) error
func (a *App) ListFileTransfers() []FileTransferJob
```

浏览端增加 `OpenSFTPSource(hostID, acceptedHostKey)`，它完成连接/TOFU、打开 SFTP channel
并返回 `Getwd()` 解析出的初始目录。后续 list/stat/read/write 复用该 host 的 browser entry；
连接失效后的 Retry 再走 Open。这样 browse 与 transfer 都能呈现相同的精确 TOFU 流程，
而不是要求用户先额外打开一次终端来写 known_hosts。

任务事件通过 Wails `sftp:transfer-progress` 发布。事件包含 job id、阶段、当前相对路径、
已处理/总字节、已处理/总条目、速度、错误摘要和终态。前端 mount 后先订阅事件，再调用
List 并按 job id/最新状态合并快照，避免订阅窗口或组件切换期间漏掉状态。

### 6.2 Endpoint 适配器

传输 manager 只依赖内部 `transferEndpoint`：stat、list、open reader、open staged writer、
mkdir、rename、remove。实现两个 adapter：

- local adapter：每个 manifest entry 先 `Lstat`，symlink 直接标记 skipped；普通文件再通过
  `fsAccess.resolve` 执行 allowRoots、最终真实路径和 deny policy 检查。
- ssh adapter：复用 `connectableSSHHost`、`tunnelManager.acquireConn`、`sftpBrowser` 和
  `sftpResolvePath`。ProxyJump/原生代理/credential 读取只有这一条现役路径。

现有 `SFTPReadFile` / `SFTPWriteFile` 保留给预览和小文本编辑。大文件传输新增流式 reader/
writer，不提高现有 5 MiB binding hard cap，也不通过 `[]byte` Wails 序列化。

### 6.3 执行与原子性

任务在桌面进程中运行，使用固定 256 KiB buffer。最多并行两个 job；每个 job 内逐文件
复制，避免同一 SFTP channel 上无界并发。目录先扫描 manifest 计算条目和可得的总字节，
目录 listing 若被 2000-entry cap 截断则整个任务拒绝，绝不悄悄复制一个不完整目录。

文件先写同目录 `.atterm-transfer-<uuid>.partial`，完成并 fsync/close 后再 rename 到目标名。
取消或失败必须 best-effort 删除 partial。`replace` 只在用户已选择冲突策略时允许替换；
`skip` 是默认；`rename` 使用稳定的 `name copy N.ext` 规则。目录 merge 逐项应用同一策略。

源文件在复制中变化时不承诺快照一致性；复制前后重新 stat，发现 size/modtime 改变就把
该条目标记为 source-changed，保留已写结果并在终态明确警告。目标写入绝不暴露半文件。
传输只复制普通文件和目录；symlink、socket、device 等记录为 skipped，不跟随。

### 6.4 取消与生命周期

每个 job 有独立 context.CancelFunc。取消会停止扫描或当前 copy、清理 partial、释放任务
持有的 SFTP connection ref，并发出唯一 cancelled 终态。关闭 SFTP 视图或切回 terminal
不取消任务；应用 shutdown 统一 cancel 并等待有限时间清理。

## 7. Host-key 与错误处理

SFTP browse 和 transfer 都必须支持与终端/隧道一致的精确 TOFU：首次请求传空
`AcceptedHostKey`；遇到 `HostKeyUnknownError` 时 UI 展示实际 dial address、fingerprint、
hop name/index，用户接受后只把该精确 pair 回传重试。不得增加 `InsecureIgnoreHostKey`，
不得用“接受下一个未知 key”的 bool。跳板链可能连续弹出多个 hop，按现有队列模型逐个
处理。

连接错误只影响对应栏或任务。栏内保留来源与路径并提供 Retry；任务失败保留在队列，
用户可重试。credential missing 提供“编辑主机”入口。SFTP subsystem 不可用时说明 SSH
已连通但远端未启用 SFTP，而不是显示通用网络错误。

## 8. SSH 主机与密钥搜索

`SshHostsPanel.vue` 保留主机页已有 `query`，新增 `keyQuery` 和 `filteredKeys`。密钥匹配
`name`、`key_type`，大小写不敏感且 trim query。主机匹配继续覆盖 alias、host、user、
tags 和 group path；搜索时不受当前 group 精确层级限制，使输入搜索可发现全部主机，
结果卡片仍显示所属 group/tag。

hosts/keys tab 都在 header 显示搜索框。面板根节点监听 printable `keydown`：当目标不是
input/textarea/select/contenteditable、没有 Meta/Ctrl/Alt、没有 drawer/dialog 正在接收
输入时，把字符追加到当前 tab query、聚焦并把 caret 放到末尾。Backspace 只在搜索框已
聚焦时编辑，不做全局劫持；Escape 清空搜索并保持面板开启。切换 tab 保留各自 query。

Tunnels tab 已有独立搜索，保持现状。

## 9. 平台与安全边界

- 固定 SFTP tab 仅在 `caps.wailsBindings && caps.pluginHost` 时显示。
- Web/mobile 不 import Wails SFTP bindings，也不显示空入口。
- saved-host credential 仍只从 desktop keyring 读取，不进入 Vue state、事件或日志。
- remote-to-remote 字节只存在于桌面 Go buffer，不经过 relay、浏览器 JS 或临时明文文件
  （目标端 partial 是最终目标目录中的正常写入阶段）。
- 本地端点继续受 `PluginFS` home/active-cwd allowRoots 约束；SFTP 端点继续受现有 deny
  list 约束。新 transfer manager 加入 `.github/scripts/check-plugin-fs-isolation.sh` 的
  local-only 守卫，禁止从 uplink/internal relay 路径引用。

## 10. 测试与验证

### Go

- endpoint validation、同源自复制拒绝、目录复制进后代拒绝。
- local-to-local、local-to-SSH、SSH-to-local、SSH-to-SSH 流式复制。
- 大文件不会调用 5 MiB `WriteFile`，buffer 保持有界。
- skip/replace/rename 三种冲突策略与 nested conflict。
- partial 原子 rename；read/write error 和 cancel 都清理 partial。
- recursive directory、空目录、截断 listing 拒绝、symlink skipped。
- progress 单调、终态唯一、并发上限、shutdown cancel。
- TOFU 精确 pair、连续 jump hop、错误 pair 不能通过。
- transfer 结束或取消后 host connection ref 正确释放。

### Frontend

- SFTP 固定 tab 不进入 terminal summaries、reorder、close 和 recovery。
- terminal/SFTP/admin 三视图互斥，切换回来保持双栏状态。
- 两栏来源可分别为 local/SSH，支持 SSH-to-SSH。
- 路径导航、当前目录筛选、多选和排序。
- 箭头、拖拽、菜单都生成相同 request；active job 可取消。
- 冲突与 permanent recursive delete 确认。
- browse/transfer TOFU retry 与 credential/SFTP unavailable 错误态。
- hosts type-to-search 不截获表单输入；keys 搜索 name/type；Escape 清空。

### 手工与浏览器验证

- 用两台测试 SSH server 验证 remote-to-remote，观察网络流量经过 desktop。
- 至少传输一个大于 100 MiB 的文件，确认 WebView 内存不随文件大小线性增长。
- 传输期间切回 terminal，确认 PTY 输入、lazy stream 订阅和传输都继续工作。
- macOS/Windows/Linux 各检查一次路径分隔、本地 home 和 permanent delete 文案。
- 用 Playwright/AiPageGo 在 1440x900、1024x768 和窄窗口截图，检查双栏不重叠、长主机
  名/路径正确截断、传输队列不挤压文件列表。

## 11. 文档同步

实现时同步更新：

- `docs/spec/architecture.md` 当前能力与桌面前端视图模型。
- `docs/spec/conventions.md` saved-host SFTP 的 local-only、TOFU 和 transfer partial 规则。
- `.claude/skills/atterm-map/SKILL.md` 的 SFTP 修改地图。
- 中英文 i18n；不在 UI 中硬编码新文案。
