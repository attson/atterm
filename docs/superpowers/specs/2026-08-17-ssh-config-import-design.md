# 导入 `~/.ssh/config`（P6 第 25 项）— design

Date: 2026-08-17
Status: Drafted — awaiting user review before plan.
Parent: [2026-08-16 sync-layer roadmap](./2026-08-16-sync-layer-roadmap-design.md) §5 P6 第 25 项 · roadmap 第 25 项

## 0. Summary

从 `~/.ssh/config` 读出主机条目，让用户挑选后导入 atterm 的主机清单——不用把已经在 ssh config 里写好的东西再手敲一遍。

导入是**两步**：先解析出预览（含跳过原因），用户勾选后才写入。不做「一键全导入」。

**本项不建立连接能力**：带 `ProxyJump` / `ProxyCommand` 的主机会被导入并标记，但**明确拒绝直连**，直到第 27 项做出跳板链路。理由见 §5.3。

## 1. 现状

- `desktop/ssh_hosts_store.go` 有 `SSHHost{ID, Alias, Host, Port, User, AuthKind, KeyID, Tags, Note}`，随 `ssh_hosts_encrypted` 走 sealed vault 同步（AAD tag `0xF0`）。
- `desktop/frontend/src/components/SshHostsPanel.vue` 已经有一条**私钥文件导入**的交互（`readPrivateKeyFile`），本项的导入入口挂在同一个面板里。
- 仓库里**没有任何 `ProxyJump` / `ProxyCommand` 的处理**（全仓 grep 无命中）。母 spec 说「一并识别」，识别之后能做什么，本设计给出答案。
- `internal/sshclient` 只有 `Dial` + known_hosts，不支持经由跳板。

## 2. Goals

- 解析 `~/.ssh/config`，列出可导入的主机，用户勾选后写进主机清单。
- 识别 `ProxyJump` / `ProxyCommand` 并**如实标记**，不假装能用。
- 解析逻辑可单测，不依赖桌面端。

## 3. Non-Goals

- 不实现跳板连接（第 27 项）。
- 不读用户的私钥文件。见 §5.2。
- 不做反向导出（atterm → ssh config）。
- 不支持 `Match` 块。见 §5.4。

## 4. 解析器：`internal/sshconfig`

新包，纯函数，输入是 reader + 一个用于解析 `Include` 的文件访问接口，输出是条目列表。放 `internal/` 而不是 `desktop/`，因为它与 GUI 无关，且要能独立测。

```go
type Entry struct {
    Alias        string   // Host 块的名字（具体名，非通配）
    HostName     string   // HostName，缺省时等于 Alias
    User         string
    Port         string
    IdentityFile string   // 原样保留路径，不读取内容
    ProxyJump    string
    ProxyCommand string
}

type Skipped struct {
    Alias  string
    Reason string   // 面向用户的原因文案（英文，后端持有，前端原样展示）
}

func Parse(r io.Reader, opener Opener) ([]Entry, []Skipped, error)
```

`Opener` 是为 `Include` 准备的注入点，测试里给内存实现，生产给受限的文件系统实现。

### 4.1 通配块参与求值，但自身不可导入

ssh_config 的真实语义是**先出现的值胜出**，且 `Host *` 这类块对所有主机生效。所以：

- 含 `*` 或 `?` 的 `Host` 模式**不产生导入条目**（没有具体主机名可连）。
- 但它们的设置**参与求值**：解析 `web1` 时，`Host *` 里的 `User deploy` 会被应用，除非更早的匹配块已经给过 `User`。

不这么做的后果是导入结果与 `ssh web1` 的实际行为不一致——用户看到的 user 是空的，而 ssh 会用 `deploy`。**导入的东西必须和 ssh 自己会做的一致**，否则这个功能只是制造困惑。

### 4.2 `Include`

支持，因为把 config 拆成多个文件是常见做法，不支持等于对这些用户静默导入不出东西。

约束：
- 相对路径按 ssh 的规则解析到 `~/.ssh/`。
- **深度上限 16，且记录已访问路径防环。** 超限或成环时产出一条 `Skipped`，不是静默截断也不是崩溃。
- glob 展开按字典序，保证结果稳定可测。

## 5. 四个需要决断的地方

### 5.1 导入是两步，不是一步

`PreviewSSHConfigImport()` 返回条目 + 跳过原因；`ImportSSHHosts(selected []Entry)` 才写入。

理由：主机清单是**同步到其它设备**的。一次误导入会把几十条垃圾推到所有设备上，而删除要一条条来。预览的成本是一次点击，代价不对称。

### 5.2 不读私钥，只记路径

`IdentityFile` 指向的私钥**不读取、不导入 keychain**。

给 `SSHHost` 加一个字段记录路径，导入后该主机 `AuthKind="key"` 而 `KeyID` 为空。这不是缺陷——`NewSshSessionByID` 在 `KeyID` 取不到密钥时已经返回 `errKeyMissing`，前端会提示补密钥，用户走既有的私钥导入流程。

**理由：读私钥文件是一个需要用户明确授权的动作**，不该作为「导入主机清单」的副作用发生。面板里已有的私钥导入是用户主动选文件，那个授权语义要保住。

### 5.3 带 ProxyJump / ProxyCommand 的主机导入后拒绝直连

这是本设计最重要的一条。

这类主机被导入、被标记，但**连接时明确报错**说「需要跳板，第 27 项尚未实现」，而不是忽略跳板配置直接连 `HostName`。

理由：ssh config 里写 `ProxyJump bastion` 的主机，通常**在网络上就不可直达**。直连要么超时（用户困惑），要么——更糟——在某些网络里真的连上了一个同名的其它东西。而且直连会把连接尝试暴露给一条本不该走的路径。**忽略一条安全相关的配置项去「尽力而为」，是错的默认值。**

`ProxyCommand` 同理，且它是任意命令，永远不会被 atterm 执行（那是 RCE 面）。第 27 项只做 `ProxyJump`。

### 5.4 `Match` 块跳过，但要说出来

`Match` 的条件（`exec`、`host`、`user`、`final`）依赖运行时状态，`Match exec` 还要执行命令。静态解析给不出正确答案。

处理：遇到 `Match` 块，其中的主机产出 `Skipped{Reason: "Match block depends on runtime conditions, not imported"}`，并在预览里显示。**不是静默忽略**——用户 config 里有 20 台机器却只导入 12 台，必须看得见另外 8 台去哪了。

## 6. 写入语义

- **按 `Alias` 去重。** 已存在同名主机时，更新 `Host` / `Port` / `User` / `IdentityFile` / 代理字段，**保留** `KeyID` / `Tags` / `Note`（用户在 atterm 里加的东西不该被 config 覆盖）。
- **不碰任何凭据。** 导入不写 keychain。
- 新主机分配新 UUID。
- 写入后照既有路径 `markSSHHostsDirty()`，随 vault 同步。

## 7. 风险

1. **新字段与同步的兼容。** `ssh_hosts_encrypted` 是整块 sealed JSON，加字段不需要动 relay 的 `allowedPreferenceKeys`——但第 21 项的教训是这个假设必须**验证**而不是假定：那次 relay 白名单拒收所有新键，功能整条链断了几个月无人知晓。计划里要有一条真正跑通「加了字段的主机记录能同步到另一台设备并读回」的验证。
2. **`~/.ssh/config` 读取权限。** 文件可能不存在或不可读。两种情况都要给出可读的说明，不是空列表。
3. **解析器与真实 ssh 行为的偏差。** 本设计只覆盖 6 个关键词；用户 config 里的其它关键词被忽略。预览里不显示「我们看不懂的部分」会让人以为导入是完整的。缓解：预览页脚注明「只导入 atterm 用得到的字段」。
4. **大 config。** 上千条目的 config 存在（自动生成的）。预览要能承受，且导入是勾选制而非全选默认。

## 8. 验证

- 解析器单测：通配块参与求值且自身不导入、first-wins 顺序、`Include` 深度与成环、`Match` 跳过并给原因、缺 `HostName` 时回落到别名。
- 写入单测：重名更新时保住 `KeyID` / `Tags` / `Note`；不写 keychain。
- 拒绝直连：带 `ProxyJump` 的主机调用连接时返回明确错误，且**没有发起任何 dial**。
- 手动（需你做）：拿你自己的 `~/.ssh/config` 跑预览，核对导入结果与 `ssh -G <alias>` 的输出是否一致。

## 9. 与母 spec 的差异

- 母 spec 只说「一并识别 `ProxyJump` / `ProxyCommand`」。本设计明确识别之后的行为是**拒绝直连**（§5.3），而不是导入后当普通主机用。
- 母 spec 未提 `Match` 与 `Include`。前者跳过并报告，后者支持但设深度与环上限（§4.2 / §5.4）。
- 母 spec 未提私钥。本设计明确**不读**（§5.2）。
