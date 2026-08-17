# macOS 分发改善（P5 第 24 项）— design

Date: 2026-08-17
Status: Drafted — awaiting user review before plan.
Parent: [2026-08-16 sync-layer roadmap](./2026-08-16-sync-layer-roadmap-design.md) §5 P5 第 24 项 · roadmap 第 24 项

## 0. Summary

> **2026-08-17 订正（whole-branch review）**：本文档最初的前提是错的，下面 §0 / §2 / §5.3 已按事实重写，
> 原文保留在 git 历史里。错在哪：**`brew install --cask` 不能免掉 Gatekeeper**。
> Homebrew cask 默认会给装好的 app 打上 `com.apple.quarantine`，这是它有意为之——刻意模仿浏览器下载的行为；
> `--no-quarantine` 在 Homebrew 5.0.0 弃用后已被移除，第三方 tap 也拿不到替代品。
> 未签名的 app 无论走 dmg 还是走 brew，弹的是同一个对话框。
> 顺带订正 §5.3：那里列的 `~/.config/atterm` 与 `~/Library/Logs/atterm` 在 macOS 上都不是真实路径。

让 macOS 用户第一次装 atterm 时**知道**自己撞上的是什么，并有一条干净的安装 / 升级路径。

Gatekeeper 本身只有一种修法：签名 + 公证（roadmap 第 8 项），它卡在 Apple Developer 证书上，不在我们手里。
本项**不解决 Gatekeeper**，本项交付的是：

- Homebrew cask 分发——版本化安装、每次发版自动更新的两架构 sha256、一条顺手的升级路径。
- cask 的 `caveats`：安装完成时当场告诉用户 app 未签名、要自己跑哪条 `xattr`。
- 文档（安装指南 + FAQ）把同一件事对直接下载 dmg 的人讲清楚。

**明确不做 `postflight` 自动去隔离标记。** 技术上可行，但那是在别人的机器上、不打招呼地绕过 Gatekeeper；
和用户读完「为什么需要」之后自己执行 `xattr` 是两回事。宁可多一步手工操作，也不替用户做这个决定。

## 1. spec 与现实的一处不符

母 spec 把本项描述为「Homebrew cask + `install-darwin.sh` 内 `xattr -d com.apple.quarantine` + 文档」。

第二项**已经做了**：`desktop/scripts/install-darwin.sh` 末尾就有 `xattr -dr com.apple.quarantine "$dst"`。

但它只覆盖**自动更新**路径——那个脚本是 updater 解包新版本后调用的。**首次安装的用户根本不会执行到它**：他们下载 dmg、拖进 Applications、双击，然后撞上 Gatekeeper。所以 spec 列的那一项既是已完成的，也解决不了本项要解决的问题。

这里少了一项工作，多了一个认识：**自动更新路径与首次安装路径的 quarantine 处理是两件事**。
（原文此处还写着「结论不变，Homebrew cask 是主要手段」——按 §0 的订正，cask 不是 Gatekeeper 的手段，
首次安装路径的 quarantine 只能由用户自己跑 `xattr` 或等签名。）

## 2. Goals

- 装完 atterm 的 macOS 用户**在被拦住的那一刻就知道原因和下一步**：cask 走 `caveats`，直接下载 dmg 的走文档。
- `brew install --cask <tap>/atterm` 能装上，且版本、两架构 sha256 与每次 release 保持一致。
- 发布新版本时 cask 自动跟上，不需要手工改 sha256。

非目标里特别点名一条：**不追求「装完直接双击就能开」**。在未签名的前提下，达成它只能靠 `postflight`
自动去隔离标记，那等于替用户悄悄绕过 Gatekeeper，见 §0。

## 3. Non-Goals

- 不做签名 / 公证（第 8 项，阻塞于证书）。
- 不加 `postflight` 自动 `xattr`（§0）。
- 不做 Linux 的 rpm / AppImage（Backlog）。
- 不接管 atterm 自己的更新机制。见 §5.2。

## 4. 需要你亲自做的两件事

本项有两处我不会替你执行，因为都是对外动作：

1. **创建 tap 仓库 `attson/homebrew-tap`**。Homebrew 要求 tap 仓库名为 `homebrew-<name>`，`brew install --cask attson/tap/atterm` 对应的就是 `github.com/attson/homebrew-tap`。仓库不存在，必须新建。
2. **配一个能推该仓库的 token**（release workflow 用它同步 cask）。跨仓库推送需要一个 PAT 或 fine-grained token 存进本仓库 secrets 的 `HOMEBREW_TAP_TOKEN`。

**顺序不能反：先建仓库，再配 token。** 同步步骤是靠「secret 不存在就跳过」保持无害的；
token 一旦配上，步骤就会真的执行，然后在 `git clone` 一个不存在的仓库时失败，把下一次发版的 release job 弄红。

**不需要手工放第一个 cask 文件。** 下一次打 tag 发版时，release job 会渲染出 `Casks/atterm.rb` 并
自己 commit 上去；tap 仓库建成空仓库即可。

在这两件事完成之前，本项的产物是「准备就绪但未启用」：cask 文件、同步脚本、文档都在，release workflow 里的同步步骤按 secret 是否存在自动跳过。

## 5. 设计

### 5.1 cask 的源在本仓库，tap 只是投递目标

cask 文件（`packaging/homebrew/atterm.rb`）作为模板留在本仓库，release 时由脚本填入版本号与两个架构的 sha256，再推到 tap 仓库。

理由：sha256 只有在构件产出后才知道，手工维护必然漂；而把模板放在本仓库意味着改 cask 与改发布流程在同一个 PR 里可见。

双架构走 cask 的 `arch` 机制：

```ruby
arch arm: "arm64", intel: "amd64"
sha256 arm: "…", intel: "…"
url "https://github.com/attson/atterm/releases/download/v#{version}/AT-Term-darwin-#{arch}.zip"
```

用 zip 而非 dmg：release 已经产出 `AT-Term-darwin-{arm64,amd64}.zip`，cask 解压后直接 `app "AT Term.app"`，比挂载 dmg 少一层。

### 5.2 `auto_updates true` 是必须的，不是可选的

atterm 自带更新器（`desktop/updater.go`）。如果 cask 不声明 `auto_updates true`，`brew upgrade` 会发现已安装版本与 cask 记录的版本不符，试图「修复」——两套更新机制互相覆盖，用户会看到 brew 反复把 app 降回 cask 里写的版本。

声明 `auto_updates true` 是在告诉 brew：这个 app 自己管更新，你只负责首次安装。这与红线 #7（更新流程不打扰用户、只手动触发）一致——brew 不该替用户决定何时更新。

### 5.3 `zap` 卸载清理

> 本节原先写的是「清掉 `~/.config/atterm` 与 `~/Library/Logs/atterm`」。**两个路径在 macOS 上都不对**，
> 而且照着写会顺手删掉凭据。下面是从代码里重新推出来的实际路径。

macOS 实际路径（都从代码推导，不能照 XDG 的名字猜）：

| 类别 | macOS 路径 | 出处 |
|------|-----------|------|
| config | `~/Library/Application Support/atterm/` | `internal/appdir/appdir.go:88` `ConfigDir()` → `os.UserConfigDir()`，darwin 上就是 `~/Library/Application Support` |
| cache | `~/Library/Caches/atterm/` | `internal/appdir/appdir.go:97` `CacheDir()` → `os.UserCacheDir()`；内含 `paste-images/`、`paste-files/`、`shell-integration/`、`updates/` |
| logs | `~/Library/Logs/AT-Term/` | `desktop/logging.go:226`（含 `desktop.log.1`…`.5` 轮转备份） |
| hook-endpoint | `~/.config/atterm/hook-endpoint` | `desktop/feishu/endpoint_file.go:30` —— 这一个**确实**在 `~/.config` 下，即便在 macOS 上；它是 `desktop/feishu` 与 `cmd/atterm-hook` 之间唯一的约定路径，没走 `appdir` |

config 目录里躺着这些文件：

- `config.json`（`desktop/config.go:576`）、`host_id`（`internal/appdir/hostid.go:38`）、`recovery.json`（`desktop/recovery_store.go:34`）——**可以删**。
  `config.json` 里确实有 `relay_session_token` 与 `local_admin_password`（`desktop/config.go:50,99`），但前者是可重新登录换发的会话令牌，
  后者只是本地 mini-relay 那个 `local@atterm.local` 用户的密码，离开这台机器上的 app 没有意义——都不是 `account_key` 材料。
  副作用要知道：删掉 `config.json` 而保留 `users.db` 之后，本地 admin 那一条 wrap 就再也打不开了；
  远端账号的 `account_key` 不受影响（锚在 keychain / `keyring-fallback.json`，用户自己的密码仍能解开对应 wrap）。
- `users.db`（+ `-wal` / `-shm`，`desktop/relay_host.go:236`）——**不能删**：`user_account_key_wraps` 表里是 `account_key` 的包装材料（`internal/userstore/opaque.go:52,71`）。
- `keyring-fallback.json`（`internal/safekeyring/safekeyring.go:146`）——**不能删**：OS keychain 不可用时的 0600 兜底凭据存储。

所以 `zap` **逐项列举要删的文件，不删父目录**。`zap trash:` 写成 `~/Library/Application Support/atterm`
一把梭是最容易犯的「简化」，代价是用户丢掉所有 sealed 会话的解密能力且无从恢复。cache 与 log 目录里没有凭据，
可以整目录删。**同样不清 Keychain 条目**——理由一致：zap 的语义是「连配置一起删」，不是「连凭据一起删」，
清凭据必须是用户显式的、单独的动作。

模板里那段注释就是给下一个想「简化」它的人看的，别删。

### 5.4 所有人都要跑的那条命令

不分安装路径——dmg 也好、brew 也好——都是这一条：

```
xattr -dr com.apple.quarantine "/Applications/AT Term.app"
```

投递位置有两处：

- **cask 的 `caveats`**：brew 装完当场打印。这是 cask 唯一被允许触碰这件事的方式，见 §0（不加 `postflight`）。
- **文档**（安装指南 + FAQ）：给直接下载 dmg 的人，且必须明说 brew 也躲不掉——旧版文档写反了。

两处都要说清为什么需要它（未签名 → Gatekeeper 隔离），以及签名到位后这一步会消失（指向第 8 项）。**不要**教用户关掉 Gatekeeper 或右键打开——前者是把系统防护整体关掉，后者在新版 macOS 上已不总是有效。

## 6. 风险

1. **cask 与自带更新器的版本漂移**：用户用 brew 装、之后被 atterm 自己更新到更新的版本，`brew list --cask` 显示的版本就是旧的。`auto_updates true` 让 brew 不去纠正它，但显示仍不准。可接受——这是所有自更新 app 在 brew 下的常态。
2. **同步步骤在 secret 缺失时静默跳过**：必须**显式打日志说明跳过原因**，否则第一次发布时没人知道 cask 没更新。这条是从第 21 项学到的——`markPrefDirtyAndPush` 吞掉错误让一个 bug 藏了几个月。
3. ~~**tap 仓库的第一次 cask 需要手工放**~~ —— 订正：不需要。下一次 tag 发版时同步步骤会自己创建
   `Casks/atterm.rb`。`render-cask.sh` 仍可手动跑，但那是给「发版前先看一眼渲染结果」用的，不是必经步骤。
4. **模板回归只在发版时才暴露**：`render-cask_test.sh` 已接进 `build-linux`（PR 触发），
   否则一处模板笔误要等到 release job 跑到那一步才发现，而那时 GitHub release 已经发出去了。

## 7. 验证

- 脚本单测：给定版本号与 `SHA256SUMS`，产出的 cask 文件内容正确（两个架构的 sha 各就各位、url 版本号正确、
  版本号格式非法时在渲染前就失败）。跑法：`./packaging/homebrew/render-cask_test.sh`；CI 里挂在 `build-linux`。
- workflow 步骤在 secret 缺失时跳过并留下日志（可用 `act` 或直接读步骤条件断言）。
- 手动（需你做）：tap 建好后 `brew install --cask attson/tap/atterm`，确认装上、`caveats` 里的 `xattr` 提示有打出来
  （**预期仍会有 Gatekeeper 提示**，跑完 `xattr` 才能开——这是正常的，不是 bug）；
  `brew uninstall --zap` 后确认 `~/Library/Application Support/atterm/config.json` 已清，而
  **`users.db` 与 `keyring-fallback.json` 仍在**、Keychain 条目仍在。

## 8. 与母 spec 的差异

- 母 spec 列的「`install-darwin.sh` 内 `xattr -d`」已存在且只覆盖自动更新路径，与首次安装无关（§1）。本项不重复做。
- 母 spec 未提 `auto_updates true`，而它是 cask 与自带更新器共存的必要条件（§5.2）。
- 母 spec 未提 tap 仓库与 token 都需要用户亲自创建（§4）——这是本项无法自足完成的部分，应在 roadmap 勾选时如实标注为「已准备、待启用」而非「已完成」。
- 母 spec（以及本文档的初稿）假设 Homebrew 能免掉 Gatekeeper。**这是错的**，见 §0。本项因此从
  「绕开 Gatekeeper」缩水成「装得好、说得准」，Gatekeeper 完整地留给第 8 项。
