# 设置页版本线选择器 — design

Date: 2026-06-26
Status: Drafted — awaiting user review before plan.

## 0. Summary

设置页「软件更新」当前只能更新到 GitHub 的 **latest** 单一版本。本设计让用户在设置页选择**更新线**（如 v0.2.x 稳定线 / v0.3.x 特性线），下载并安装该线的**最新正式版**。

atterm 同时维护两条（未来可能更多）并行发布线。用户需要根据需求选择更新到哪条线——稳定线还是特性线。

## 1. Goals

- 设置页显示可选的**更新线**（按 minor 版本分组），每条线展示其最新正式版。
- 用户选线 → 下载并安装该线最新版（不是手选每个 patch）。
- 线**动态**从 GitHub releases 发现（未来 v0.4.x 自动出现，无需改代码）。
- 过滤规则：**只升不降**——显示 minor ≥ 当前 minor 的线，且其最新版 > 当前版本。
- 跳过 pre-release / draft。
- GitHub API 失败时优雅降级到现有 latest 逻辑，不阻断更新。

## 2. Non-goals

- 手选具体 patch（用户选线，系统取该线最新）。
- 降级 / 从高线回退到低线（只升不降）。
- pre-release / beta 版本选择。
- 改变现有下载/安装/校验机制（复用）。

## 3. 现状

- `desktop/updater.go`：`Updater` 拉 `releases/latest`（`githubReleaseAPI()` line 147 写死 `/releases/latest`）。`UpdateState`（line ~40）只有单个 `Latest` 字段。已有 release JSON 解析（`TagName`/`Prerelease`/`Draft`，line 168-170）+ `Prerelease` 跳过逻辑（line 208）+ 各平台 asset 匹配（`assetNameForPlatform`）+ 1h 缓存（`releaseCacheTTL`）+ 测试用 `releaseURL` 覆盖。
- `desktop/app.go`：`GetUpdateState()`（1423）、`CheckUpdate()`（1432）、`InstallUpdate()`（1451）桥接给前端。安装写死 `state.latest`。
- `desktop/frontend/src/components/SettingsUpdates.vue`：显示 current/latest + 「检查/下载 latest/安装」按钮。`SettingsUpdates.test.ts` 已存在。

## 4. 设计

### 4.1 过滤规则（核心）

「只升不降」精确定义：

```
设 current = 当前版本（解析为 minor=M_cur, patch=P_cur）
对每个正式 release（非 prerelease/draft），解析 tag → (minor, patch)：
  按 minor 分组，每组取 patch 最大者作为该线的「最新」
保留满足以下的线：
  - minor > M_cur（更高的线），或
  - minor == M_cur 且该线最新 patch > P_cur（同线有更新）
丢弃 minor < M_cur 的线（不回退到低线）
```

当前为 `dev`/空版本：视为「比任何版本都旧」，显示所有线的最新。

### 4.2 后端 `updater.go`

- 新增类型：
  ```go
  type VersionLine struct {
      Minor    string `json:"minor"`     // "0.2", "0.3"
      Latest   string `json:"latest"`    // "v0.2.155"
      Notes    string `json:"notes"`     // 该版本的 release notes
      AssetURL string `json:"asset_url"` // 该版本本平台 asset 下载地址
  }
  ```
- `UpdateState` 加字段 `Lines []VersionLine`。
- 新增 `githubReleasesAPI()` 返回 `https://api.github.com/repos/<repo>/releases`（列表，非 latest）。
- 新增方法 `fetchLines() ([]VersionLine, error)`：拉 releases 列表 → 跳过 prerelease/draft → 解析 tag → 按 4.1 规则分组过滤 → 每条线匹配本平台 asset。复用现有 asset 匹配 + 缓存。
- `CheckUpdate` 流程：现有 latest 逻辑保留（fallback）；额外调 `fetchLines()` 填 `state.Lines`。`fetchLines` 失败只 log，不影响现有 latest 行为（降级）。
- 版本比较：新增 `parseVersion(tag) (minor string, patch int, ok bool)` + 比较 helper。tag 形如 `v0.2.155`。
- 下载/安装接受指定 tag：现有下载逻辑参数化（`downloadVersion(tag, assetURL)`），不再写死 latest。

### 4.3 App 桥接 `app.go`

- `CheckUpdate()` 同时填充 `Lines`（无需新方法，State 自带）。
- 新增 `DownloadVersion(tag string) error`：下载指定线的版本（前端选线后传该线 Latest tag）。
- `InstallUpdate()` 已有；确认它安装的是已下载的版本（下载时记录 tag → 安装该 tag）。

### 4.4 前端 `SettingsUpdates.vue`

- `UpdateState` 类型加 `lines: VersionLine[]`。
- 显示条件：`lines.length >= 2` 时渲染 radio 列表（多条线可选才需要选择器）。`lines.length == 1` 时退化为「有更新 → 下载该版本」的单按钮（等价现有 UI，但版本来自该线）。`lines` 为空时显示「已是最新」/现有 latest fallback。
- radio 列表形态（`lines.length >= 2`）：
  ```
  更新线:
    ( ) v0.2.x 稳定线  → v0.2.155
    (•) v0.3.x 特性线  → v0.3.0
    [下载 v0.3.0]
  ```
  默认选中：当前线（minor == 当前 minor）的那条；当前是 dev 则选最高线。
- 选中线后，下载按钮调 `DownloadVersion(选中线.latest)`，安装走现有 `InstallUpdate`。
- 退化：`lines` 为空（API 失败/无更高版本）→ 保留现有单 latest UI（fallback）。
- 「稳定线/特性线」这种标签：**不写死**——线标签就用 minor（`v0.2.x` / `v0.3.x`），避免硬编码语义（哪条是稳定线由用户/团队认知，UI 不臆断）。i18n key 用 `settings.updates.versionLine` 等。

## 5. 错误处理

- `fetchLines` GitHub API 失败 / 超配额 / 解析失败 → log + `state.Lines = nil` → 前端退化到现有 latest UI。**绝不阻断现有更新路径**。
- tag 解析失败的单个 release → 跳过该 release，不影响其他。
- 无更高版本（已是各线最新）→ `Lines` 为空或只含当前线（前端显示「已是最新」）。

## 6. 测试

- `updater_test.go`（复用 mock release server，`releaseURL` 覆盖指向 `/releases` 列表 mock）：
  - 版本解析 `parseVersion`：`v0.2.155` → (0.2, 155)；非法 tag → !ok。
  - 分组取最新：多个 v0.2.x patch → 取最大。
  - 过滤规则：当前 v0.2.154 → 显示 v0.2.155 + v0.3.x；当前 v0.3.0 → 不显示 v0.2.x（低线）；当前 dev → 显示所有线。
  - prerelease/draft 跳过。
  - 各平台 asset 匹配进 `VersionLine.AssetURL`。
  - API 失败 → `Lines` 空 + 现有 latest 逻辑不受影响（fallback）。
- `SettingsUpdates.test.ts`：
  - `lines.length > 1` → 渲染 radio 列表，每条线显示 minor + latest。
  - 选线 → 下载按钮文案/调用带选中 tag。
  - `lines` 空 → 退化到现有单 latest UI。
  - 默认选中当前线。

## 7. 实现顺序建议

1. 后端版本解析 + 过滤逻辑（纯函数，易测）。
2. 后端 `fetchLines` + `UpdateState.Lines` + CheckUpdate 接线 + fallback。
3. 后端 `DownloadVersion(tag)` 参数化下载。
4. App 桥接 `DownloadVersion`。
5. 前端 radio 列表 UI + 选线下载 + 退化。
6. 端到端：选线 → 下载 → 安装。
