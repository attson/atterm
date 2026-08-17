# macOS 分发改善（P5 第 24 项）— design

Date: 2026-08-17
Status: Drafted — awaiting user review before plan.
Parent: [2026-08-16 sync-layer roadmap](./2026-08-16-sync-layer-roadmap-design.md) §5 P5 第 24 项 · roadmap 第 24 项

## 0. Summary

让 macOS 用户第一次装 atterm 时不被 Gatekeeper 拦住。

真正的修法是签名 + 公证（roadmap 第 8 项），但它卡在 Apple Developer 证书上，不在我们手里。本项做的是**不依赖证书的替代路径**：Homebrew cask 分发，加上给直接下载 dmg 的人一份说得清楚的说明。

**这不是签名的等价物**，只是把「第一秒被劝退」的概率压下去。

## 1. spec 与现实的一处不符

母 spec 把本项描述为「Homebrew cask + `install-darwin.sh` 内 `xattr -d com.apple.quarantine` + 文档」。

第二项**已经做了**：`desktop/scripts/install-darwin.sh` 末尾就有 `xattr -dr com.apple.quarantine "$dst"`。

但它只覆盖**自动更新**路径——那个脚本是 updater 解包新版本后调用的。**首次安装的用户根本不会执行到它**：他们下载 dmg、拖进 Applications、双击，然后撞上 Gatekeeper。所以 spec 列的那一项既是已完成的，也解决不了本项要解决的问题。

结论不变（Homebrew cask 是主要手段），但少了一项工作、多了一个认识：**自动更新路径与首次安装路径的 quarantine 处理是两件事**。

## 2. Goals

- `brew install --cask <tap>/atterm` 能装上并直接运行，不触发 Gatekeeper 提示。
- 直接下载 dmg 的用户能在文档里找到一句话说清怎么过，而不是自己搜。
- 发布新版本时 cask 自动跟上，不需要手工改 sha256。

## 3. Non-Goals

- 不做签名 / 公证（第 8 项，阻塞于证书）。
- 不做 Linux 的 rpm / AppImage（Backlog）。
- 不接管 atterm 自己的更新机制。见 §5.2。

## 4. 需要你亲自做的两件事

本项有两处我不会替你执行，因为都是对外动作：

1. **创建 tap 仓库 `attson/homebrew-tap`**。Homebrew 要求 tap 仓库名为 `homebrew-<name>`，`brew install --cask attson/tap/atterm` 对应的就是 `github.com/attson/homebrew-tap`。仓库不存在，必须新建。
2. **配一个能推该仓库的 token**（release workflow 用它同步 cask）。跨仓库推送需要一个 PAT 或 fine-grained token 存进本仓库的 secrets。

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

`brew uninstall --zap` 应清掉 `~/.config/atterm`（配置、host_id、recovery.json）与 `~/Library/Logs/atterm`。**不清 Keychain 条目**——那里存着 `account_key` 相关材料，误删等于丢 E2EE 密钥，而 zap 的语义是「连配置一起删」，不是「连凭据一起删」。用户要清凭据应显式操作。

### 5.4 直接下载的人怎么办

文档（安装指南 + FAQ）给一条命令：

```
xattr -dr com.apple.quarantine "/Applications/AT Term.app"
```

并说清为什么需要它（未签名 → Gatekeeper 隔离），以及签名到位后这一步会消失（指向第 8 项）。**不要**教用户关掉 Gatekeeper 或右键打开——前者是把系统防护整体关掉，后者在新版 macOS 上已不总是有效。

## 6. 风险

1. **cask 与自带更新器的版本漂移**：用户用 brew 装、之后被 atterm 自己更新到更新的版本，`brew list --cask` 显示的版本就是旧的。`auto_updates true` 让 brew 不去纠正它，但显示仍不准。可接受——这是所有自更新 app 在 brew 下的常态。
2. **同步步骤在 secret 缺失时静默跳过**：必须**显式打日志说明跳过原因**，否则第一次发布时没人知道 cask 没更新。这条是从第 21 项学到的——`markPrefDirtyAndPush` 吞掉错误让一个 bug 藏了几个月。
3. **tap 仓库的第一次 cask 需要手工放**：自动同步只在下一次 release 时触发。设计里包含一个可手动跑的脚本，避免「等下次发版才知道对不对」。

## 7. 验证

- 脚本单测：给定版本号与 `SHA256SUMS`，产出的 cask 文件内容正确（两个架构的 sha 各就各位、url 版本号正确）。
- workflow 步骤在 secret 缺失时跳过并留下日志（可用 `act` 或直接读步骤条件断言）。
- 手动（需你做）：tap 建好后 `brew install --cask attson/tap/atterm`，确认装上直接能开、无 Gatekeeper 提示；`brew uninstall --zap` 后确认 `~/.config/atterm` 已清、Keychain 条目仍在。

## 8. 与母 spec 的差异

- 母 spec 列的「`install-darwin.sh` 内 `xattr -d`」已存在且只覆盖自动更新路径，与首次安装无关（§1）。本项不重复做。
- 母 spec 未提 `auto_updates true`，而它是 cask 与自带更新器共存的必要条件（§5.2）。
- 母 spec 未提 tap 仓库与 token 都需要用户亲自创建（§4）——这是本项无法自足完成的部分，应在 roadmap 勾选时如实标注为「已准备、待启用」而非「已完成」。
