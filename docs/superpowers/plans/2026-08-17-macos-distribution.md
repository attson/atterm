# macOS 分发改善 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 macOS 用户第一次装 atterm 不被 Gatekeeper 拦住——通过 Homebrew cask 分发，加上给直接下载 dmg 的人一份说得清楚的说明。

**Architecture:** cask 模板留在本仓库（`packaging/homebrew/atterm.rb.tmpl`），release 时由一个纯函数式的脚本填入版本号与两个架构的 sha256（从 release 的 `SHA256SUMS` 读），再推到 tap 仓库。tap 仓库与推送 token 都由用户创建；在它们存在之前，同步步骤显式跳过并留日志。

**Tech Stack:** Bash + GitHub Actions + Homebrew cask DSL + VitePress docs

**Spec:** [`docs/superpowers/specs/2026-08-17-macos-distribution-design.md`](../specs/2026-08-17-macos-distribution-design.md)

## Global Constraints

- **本项不是签名的等价物。** roadmap 第 8 项（codesign / notarize）仍阻塞于 Apple 证书。文档措辞不得让人以为签名问题已解决。
- **`auto_updates true` 是必须的，不是可选的。** atterm 自带更新器（`desktop/updater.go`）。不声明的话 `brew upgrade` 会发现已装版本与 cask 记录不符并试图「修复」，把 app 反复降回 cask 里的版本，与自带更新器互相覆盖。声明它等于告诉 brew「这个 app 自己管更新，你只负责首次安装」，与红线 #7 一致。
- **`zap` 不清 Keychain。** 清 `~/.config/atterm` 与 `~/Library/Logs/atterm`；Keychain 里是 `account_key` 相关材料，误删等于丢 E2EE 密钥。zap 的语义是「连配置一起删」，不是「连凭据一起删」。
- **同步步骤在 secret 缺失时必须显式打日志说明跳过原因**，不得静默跳过。第 21 项的教训：`markPrefDirtyAndPush` 吞掉错误让一个 bug 藏了几个月。
- **不修改 `desktop/scripts/install-darwin.sh`。** 它已有 `xattr -dr com.apple.quarantine`，但只覆盖自动更新路径，与首次安装无关（design §1）。母 spec 把它列为本项工作是错的。
- **文档不得教用户关闭 Gatekeeper 或用右键打开**：前者是关掉整个系统防护，后者在新版 macOS 上已不总是有效。只给 `xattr -dr` 这一条，并说明签名到位后它会消失。
- release 产物命名为 `AT-Term-darwin-arm64.zip` / `AT-Term-darwin-amd64.zip`，与 `SHA256SUMS` 同处一个 release。
- 站点文档在 `site/docs/`，中文。

---

### Task 1: cask 模板与生成脚本

**Files:**
- Create: `packaging/homebrew/atterm.rb.tmpl`
- Create: `packaging/homebrew/render-cask.sh`
- Create: `packaging/homebrew/render-cask_test.sh`（或用仓库既有的 bash 测试约定，先看 `scripts/` 下有没有先例）

**Interfaces:**
- Produces: `render-cask.sh <version> <sha256sums-file>` → 把渲染好的 cask 写到 stdout

- [ ] **Step 1: 写失败的测试**

先看 `scripts/` 与 `.github/scripts/` 里有没有既有的 bash 测试写法并沿用；没有就用最朴素的形式：脚本 + 期望输出比对。

测试要覆盖：
- 给定 `v0.4.20` 与一份含两行 darwin zip 的 `SHA256SUMS`，输出里 `version "0.4.20"`（**不带 `v` 前缀**——cask 的 `version` 用于拼 url 时会被 `#{version}` 插值，模板里写 `v#{version}`）
- arm64 与 amd64 的 sha256 各自落在正确的 `arch` 分支上，**不能对调**
- `SHA256SUMS` 里缺某个架构时脚本**失败并说明缺哪个**，而不是产出一个 sha 为空的 cask

最后一条是重点：一个 sha 为空的 cask 会让 `brew install` 在用户机器上失败，而不是在 CI 里失败。

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 写模板**

`packaging/homebrew/atterm.rb.tmpl`：

```ruby
cask "atterm" do
  arch arm: "arm64", intel: "amd64"

  version "__VERSION__"
  sha256 arm:   "__SHA_ARM64__",
         intel: "__SHA_AMD64__"

  url "https://github.com/attson/atterm/releases/download/v#{version}/AT-Term-darwin-#{arch}.zip"
  name "AT Term"
  desc "Cross-platform terminal with remote takeover"
  homepage "https://github.com/attson/atterm"

  # AT Term ships its own updater (desktop/updater.go, manual-trigger only).
  # Without this, `brew upgrade` would keep reverting the app to whatever
  # version this cask records, fighting the built-in updater.
  auto_updates true

  app "AT Term.app"

  # Deliberately does NOT remove Keychain entries: those hold account_key
  # material, and losing them means losing E2EE access. `zap` means "take the
  # config too", not "take the credentials too".
  zap trash: [
    "~/.config/atterm",
    "~/Library/Logs/atterm",
  ]
end
```

- [ ] **Step 4: 写脚本**

`render-cask.sh` 读 `SHA256SUMS`，按文件名匹配出两个架构的 sha，替换模板里的三个占位符，写 stdout。缺任一架构则 `exit 1` 并在 stderr 说明缺的是哪个。

- [ ] **Step 5: 跑测试确认通过并提交**

```bash
git add packaging/homebrew/
git commit -m "feat(packaging): render a Homebrew cask from release checksums"
```

---

### Task 2: release workflow 同步步骤

**Files:**
- Modify: `.github/workflows/build.yml`（release job 末尾）

- [ ] **Step 1: 加同步步骤**

在 release job 的 upload 之后加一步，行为：

1. 若 secret（比如 `HOMEBREW_TAP_TOKEN`）缺失 → **打印一条明确的跳过说明**（含「tap 未配置，见 docs/superpowers/specs/2026-08-17-macos-distribution-design.md §4」）并成功退出。**不要**静默 `continue-on-error`。
2. 存在则：下载本次 release 的 `SHA256SUMS`，跑 `render-cask.sh`，clone tap 仓库，写入 `Casks/atterm.rb`，commit & push。

步骤本身必须 `if: startsWith(github.ref, 'refs/tags/v')`，与既有 release job 的条件一致。

- [ ] **Step 2: 验证条件表达式**

不需要真跑 workflow。断言方式：读步骤条件，确认 secret 缺失时走的是「打印并成功」而非失败或静默。若仓库里已有 workflow 的静态检查脚本就跑一遍。

- [ ] **Step 3: 提交**

---

### Task 3: 文档与 roadmap

**Files:**
- Modify: `site/docs/guide/index.md`（安装一节）
- Modify: `site/docs/guide/faq.md`（新增一条）
- Modify: `docs/roadmap.md`

- [ ] **Step 1: 安装指南加 Homebrew 路径**

在现有「到 Releases 下载」之上加 Homebrew 方式，并注明它需要 tap（在 tap 建好之前，这段以「即将可用」措辞或先不写命令——**由实现者判断哪种更诚实，并在报告里说明选择**。写一条现在跑不通的命令进文档是最差的选项）。

- [ ] **Step 2: FAQ 加 Gatekeeper 条目**

标题形如「macOS 提示「无法打开，因为无法验证开发者」?」。内容：

- 原因：安装包尚未签名 / 公证（指向 roadmap 第 8 项，阻塞于 Apple 证书）
- 解法：`xattr -dr com.apple.quarantine "/Applications/AT Term.app"`
- 一句话说明这条命令做了什么（去掉下载隔离标记）
- 说明用 Homebrew 安装不需要这一步
- **不写**「关闭 Gatekeeper」或「右键打开」

- [ ] **Step 3: roadmap**

第 24 项勾选，但**如实标注为「已准备、待启用」**——tap 仓库与 token 由用户创建，在那之前 cask 不可安装。不要标成无条件完成。

- [ ] **Step 4: 提交**
