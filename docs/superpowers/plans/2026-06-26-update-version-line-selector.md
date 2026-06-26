# Update Version-Line Selector Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让设置页「软件更新」支持选择更新线（v0.2.x / v0.3.x），下载并安装所选线的最新正式版，规则为「只升不降」。

**Architecture:** 后端 `updater.go` 新增「列出版本线」能力（拉 GitHub `/releases` 列表 → 按 minor 分组取最新 → 过滤出 minor ≥ 当前的线），把结果放进 `UpdateState.Lines`；新增 `DownloadVersion(tag)` 把所选线的 release 重新解析进 state（复用现有 asset/checksum 解析）再走现有 `Download`。前端 `SettingsUpdates.vue` 在有多条线时渲染 radio 选择器，否则退化到现有单按钮。GitHub API 失败时优雅降级，绝不阻断现有 latest 路径。

**Tech Stack:** Go（`golang.org/x/mod/semver` 版本比较，已在 updater.go 使用）；GitHub Releases API；Vue 3 + TS（wailsjs 自动绑定）；测试用 `httptest` mock release server（`releaseURL` 可覆盖）。

**环境：** 默认 `go` 1.19 不可用，**用 `/home/attson/sdk/go1.24.13/bin/go` 和 `gofmt`**。前端测试用 `npm`（在 `desktop/frontend/`）。gofmt-on-save 钩子：Edit 被拒就重读再编辑。

---

## File Structure

- `desktop/updater.go` — 核心：新增 `VersionLine` 类型、`UpdateState.Lines` 字段、`parseVersionTag`、`groupLines`、`fetchReleases`/`refreshLines`、`DownloadVersion`。复用现有 `fetchLatest` HTTP 模式、`assetNameForPlatform`、asset/checksum 解析、`Download`、`semver`。
- `desktop/updater_test.go` — 版本解析/分组/过滤/fallback 的纯函数 + HTTP mock 测试。
- `desktop/app.go` — 桥接 `DownloadVersion(tag)` 给前端（`CheckUpdate` 已填 Lines，无需改）。
- `desktop/frontend/src/components/SettingsUpdates.vue` — radio 版本线选择器 + 退化逻辑。
- `desktop/frontend/src/components/SettingsUpdates.test.ts` — 选择器渲染/选线/退化测试。
- `desktop/frontend/src/i18n/messages/{en,zh}.ts` — 新增 i18n key（versionLine 等）。

---

## Task 1: 版本 tag 解析（纯函数）

**Files:**
- Modify: `desktop/updater.go`
- Test: `desktop/updater_test.go`

- [ ] **Step 1: 写失败测试**

在 `desktop/updater_test.go` 末尾追加：

```go
func TestParseVersionTag(t *testing.T) {
	cases := []struct {
		tag      string
		minor    string
		patch    int
		ok       bool
	}{
		{"v0.2.155", "v0.2", 155, true},
		{"v0.3.0", "v0.3", 0, true},
		{"v1.10.3", "v1.10", 3, true},
		{"0.2.155", "", 0, false},  // 缺 v 前缀
		{"v0.2", "", 0, false},     // 缺 patch
		{"dev", "", 0, false},
		{"", "", 0, false},
	}
	for _, c := range cases {
		minor, patch, ok := parseVersionTag(c.tag)
		if ok != c.ok || minor != c.minor || patch != c.patch {
			t.Errorf("parseVersionTag(%q) = (%q,%d,%v), want (%q,%d,%v)",
				c.tag, minor, patch, ok, c.minor, c.patch, c.ok)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `/home/attson/sdk/go1.24.13/bin/go test ./desktop/ -run TestParseVersionTag -v`
Expected: FAIL — `parseVersionTag` undefined

- [ ] **Step 3: 实现**

在 `desktop/updater.go` 加（放在文件靠近其他 helper 处；确认已 import `"strconv"` 和 `"strings"`，updater.go 已用 strings）：

```go
// parseVersionTag splits a "vMAJOR.MINOR.PATCH" tag into its minor line
// ("vMAJOR.MINOR") and patch number. ok is false for any tag that is not a
// well-formed three-part v-prefixed version (dev, drafts, malformed).
func parseVersionTag(tag string) (minor string, patch int, ok bool) {
	if !strings.HasPrefix(tag, "v") {
		return "", 0, false
	}
	parts := strings.Split(tag[1:], ".")
	if len(parts) != 3 {
		return "", 0, false
	}
	p, err := strconv.Atoi(parts[2])
	if err != nil {
		return "", 0, false
	}
	// Validate major/minor are numeric too.
	if _, err := strconv.Atoi(parts[0]); err != nil {
		return "", 0, false
	}
	if _, err := strconv.Atoi(parts[1]); err != nil {
		return "", 0, false
	}
	return "v" + parts[0] + "." + parts[1], p, true
}
```

- [ ] **Step 4: 运行确认通过**

Run: `/home/attson/sdk/go1.24.13/bin/go test ./desktop/ -run TestParseVersionTag -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add desktop/updater.go desktop/updater_test.go
git commit -m "feat(updater): parse version tag into minor line + patch"
```

---

## Task 2: 版本线分组 + 过滤（纯函数，核心规则）

**Files:**
- Modify: `desktop/updater.go`
- Test: `desktop/updater_test.go`

- [ ] **Step 1: 写失败测试**

`VersionLine` 类型在本任务首次定义。在 `desktop/updater_test.go` 追加：

```go
func TestGroupLines(t *testing.T) {
	// (tag, assetURL, notes) tuples simulating fetched releases.
	releases := []lineCandidate{
		{tag: "v0.2.155", assetURL: "u-2-155", notes: "n2155"},
		{tag: "v0.2.154", assetURL: "u-2-154", notes: "n2154"},
		{tag: "v0.3.0", assetURL: "u-3-0", notes: "n30"},
		{tag: "v0.2.153", assetURL: "u-2-153", notes: "n2153"},
	}

	// current = v0.2.154 → show v0.2 line latest (v0.2.155, >current) + v0.3 (higher line).
	got := groupLines(releases, "v0.2.154")
	if len(got) != 2 {
		t.Fatalf("current v0.2.154: got %d lines, want 2: %+v", len(got), got)
	}
	// Lines sorted by minor descending (highest line first).
	if got[0].Minor != "v0.3" || got[0].Latest != "v0.3.0" {
		t.Errorf("line[0] = %+v, want v0.3 → v0.3.0", got[0])
	}
	if got[1].Minor != "v0.2" || got[1].Latest != "v0.2.155" || got[1].AssetURL != "u-2-155" {
		t.Errorf("line[1] = %+v, want v0.2 → v0.2.155 (u-2-155)", got[1])
	}

	// current = v0.3.0 → only v0.3 line, and its latest is v0.3.0 == current so NOT shown.
	got = groupLines(releases, "v0.3.0")
	if len(got) != 0 {
		t.Fatalf("current v0.3.0 (already highest+latest): got %d lines, want 0: %+v", len(got), got)
	}

	// current = dev → show all lines' latest.
	got = groupLines(releases, "dev")
	if len(got) != 2 {
		t.Fatalf("current dev: got %d lines, want 2: %+v", len(got), got)
	}
	if got[0].Minor != "v0.3" || got[1].Minor != "v0.2" {
		t.Errorf("dev lines order = %q,%q want v0.3,v0.2", got[0].Minor, got[1].Minor)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `/home/attson/sdk/go1.24.13/bin/go test ./desktop/ -run TestGroupLines -v`
Expected: FAIL — `lineCandidate` / `groupLines` / `VersionLine` undefined

- [ ] **Step 3: 实现**

在 `desktop/updater.go` 加（`VersionLine` 是导出类型，会进 UpdateState）：

```go
// VersionLine is one update line (minor version) the user can choose, with
// the latest release on that line. JSON tags mirror the frontend binding.
type VersionLine struct {
	Minor    string `json:"minor"`     // "v0.2"
	Latest   string `json:"latest"`    // "v0.2.155"
	Notes    string `json:"notes"`
	AssetURL string `json:"asset_url"`
}

// lineCandidate is an intermediate fetched-release tuple fed to groupLines.
type lineCandidate struct {
	tag      string
	assetURL string
	notes    string
}

// groupLines applies the "upgrade-only" rule:
//   - group candidates by minor line, keep the highest patch per line
//   - keep a line iff its minor > current's minor, OR (same minor AND its
//     latest patch > current's patch)
//   - when current is dev/unparseable, every line's latest is kept
// Result is sorted by minor descending (highest line first).
func groupLines(cands []lineCandidate, current string) []VersionLine {
	type best struct {
		patch    int
		tag      string
		assetURL string
		notes    string
	}
	byMinor := map[string]best{}
	for _, c := range cands {
		minor, patch, ok := parseVersionTag(c.tag)
		if !ok {
			continue
		}
		if b, exists := byMinor[minor]; !exists || patch > b.patch {
			byMinor[minor] = best{patch: patch, tag: c.tag, assetURL: c.assetURL, notes: c.notes}
		}
	}

	curMinor, curPatch, curOK := parseVersionTag(current)

	var out []VersionLine
	for minor, b := range byMinor {
		keep := false
		if !curOK {
			keep = true // dev / unparseable current: show everything
		} else if semver.Compare(minor, curMinor) > 0 {
			keep = true // higher line
		} else if minor == curMinor && b.patch > curPatch {
			keep = true // same line, newer patch
		}
		if keep {
			out = append(out, VersionLine{
				Minor: minor, Latest: b.tag, Notes: b.notes, AssetURL: b.assetURL,
			})
		}
	}
	// Sort by minor descending (semver.Compare on the "vMAJOR.MINOR" form).
	sort.Slice(out, func(i, j int) bool {
		return semver.Compare(out[i].Minor, out[j].Minor) > 0
	})
	return out
}
```

确认 updater.go 已 import `"sort"` 和 `"golang.org/x/mod/semver"`（semver 已用；sort 可能要加）。

- [ ] **Step 4: 运行确认通过**

Run: `/home/attson/sdk/go1.24.13/bin/go test ./desktop/ -run TestGroupLines -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add desktop/updater.go desktop/updater_test.go
git commit -m "feat(updater): group releases into upgrade-only version lines"
```

---

## Task 3: 拉 releases 列表 + 填充 Lines（HTTP，复用 fetchLatest 模式）

**Files:**
- Modify: `desktop/updater.go`
- Test: `desktop/updater_test.go`

- [ ] **Step 1: 先读现有代码**

`cat desktop/updater.go`，重点看：
- `fetchLatest(ctx)`（GET `githubReleaseAPI()`，set Accept/User-Agent header，decode `githubRelease`）。
- `githubRelease` 结构（`TagName`/`Body`/`Prerelease`/`Draft`/`Assets`），`githubAsset`（`Name`/`DownloadURL`/`Size`）。
- `assetNameForPlatform(GOOS, GOARCH)`。
- `Check(ctx, force)` 主流程末尾（约 line 195-245）填 `state.Latest/AssetURL/Notes` 的那段。
- `UpdateState` 结构、`updaterConfig`（`releaseURL`/`current`/`client`/`now`）、`cachedAt`/`releaseCacheTTL`。

- [ ] **Step 2: 写失败测试**

在 `updater_test.go` 看现有 mock release server 测试（搜 `httptest` / `releaseURL`），照搬其构造方式。追加一个端到端测试：mock 一个 `/releases` 列表 endpoint，返回多版本，断言 `Check` 后 `State().Lines` 正确。

```go
func TestCheck_PopulatesLines(t *testing.T) {
	// releases list JSON (newest first, as GitHub returns).
	listJSON := `[
	  {"tag_name":"v0.3.0","body":"n30","prerelease":false,"draft":false,
	   "assets":[{"name":"ASSET_NAME","browser_download_url":"https://x/v0.3.0","size":10}]},
	  {"tag_name":"v0.2.155","body":"n2155","prerelease":false,"draft":false,
	   "assets":[{"name":"ASSET_NAME","browser_download_url":"https://x/v0.2.155","size":20}]},
	  {"tag_name":"v0.2.154","body":"n2154","prerelease":false,"draft":false,
	   "assets":[{"name":"ASSET_NAME","browser_download_url":"https://x/v0.2.154","size":30}]}
	]`
	// Replace ASSET_NAME with the real per-platform asset so matching works.
	asset, err := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skipf("no asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	listJSON = strings.ReplaceAll(listJSON, "ASSET_NAME", asset)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			// minimal latest for the existing path
			w.Write([]byte(`{"tag_name":"v0.3.0","prerelease":false,"draft":false,"assets":[]}`))
			return
		}
		w.Write([]byte(listJSON))
	}))
	defer srv.Close()

	u := newUpdater(updaterConfig{
		current:    "v0.2.154",
		repo:       "attson/atterm",
		releaseURL: srv.URL + "/releases/latest",
		client:     srv.Client(),
		now:        time.Now,
	})
	// Point the list endpoint at the mock too (Task 3 adds releasesURL override).
	u.cfg.releasesURL = srv.URL + "/releases"

	if err := u.Check(context.Background(), true); err != nil {
		t.Fatalf("Check: %v", err)
	}
	lines := u.State().Lines
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %+v", len(lines), lines)
	}
	if lines[0].Minor != "v0.3" || lines[1].Minor != "v0.2" {
		t.Fatalf("line order = %q,%q", lines[0].Minor, lines[1].Minor)
	}
	if lines[1].Latest != "v0.2.155" || lines[1].AssetURL != "https://x/v0.2.155" {
		t.Fatalf("v0.2 line = %+v, want v0.2.155 / https://x/v0.2.155", lines[1])
	}
}
```

> 注：现有测试构造 `newUpdater`/`updaterConfig` 的方式以 `updater_test.go` 实际为准（字段名/必填项可能不同），照搬现有测试的 config 构造，只加 `releasesURL` 和断言 Lines。

- [ ] **Step 3: 运行确认失败**

Run: `/home/attson/sdk/go1.24.13/bin/go test ./desktop/ -run TestCheck_PopulatesLines -v`
Expected: FAIL — `releasesURL` / `State().Lines` undefined

- [ ] **Step 4: 实现**

(a) `updaterConfig` 加字段 `releasesURL string`（test override，注释仿 `releaseURL`）。

(b) 加 `githubReleasesAPI()`（仿 `githubReleaseAPI`）：
```go
func (u *Updater) githubReleasesAPI() string {
	if u.cfg.releasesURL != "" {
		return u.cfg.releasesURL
	}
	return "https://api.github.com/repos/" + u.cfg.repo + "/releases"
}
```

(c) 加 `fetchReleases(ctx)` — 仿 `fetchLatest`，但 decode 成 `[]githubRelease`：
```go
func (u *Updater) fetchReleases(ctx context.Context) ([]githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u.githubReleasesAPI(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AT-Term/"+u.cfg.current)
	resp, err := u.cfg.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github releases list http %d", resp.StatusCode)
	}
	var rels []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
		return nil, err
	}
	return rels, nil
}
```

**（已核实）**`githubRelease` 当前结构只有 `TagName`/`Body`/`Prerelease`/`Assets`，**没有 `Draft` 字段**。先给它加一个 `Draft bool \`json:"draft"\``（GitHub releases API 返回 draft 字段，draft 也应跳过）：
```go
type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Body       string        `json:"body"`
	Prerelease bool          `json:"prerelease"`
	Draft      bool          `json:"draft"`
	Assets     []githubAsset `json:"assets"`
}
```

(d) 加 `refreshLines(ctx)` — 把 releases 转成 candidates（跳过 prerelease/draft，匹配本平台 asset）再 `groupLines`：
```go
func (u *Updater) refreshLines(ctx context.Context) []VersionLine {
	rels, err := u.fetchReleases(ctx)
	if err != nil {
		log.Printf("updater: fetch releases list: %v", err)
		return nil // graceful degrade — caller leaves Lines nil
	}
	assetName, perr := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
	if perr != nil {
		return nil
	}
	var cands []lineCandidate
	for _, rel := range rels {
		if rel.Prerelease || rel.Draft {
			continue
		}
		var assetURL string
		for _, a := range rel.Assets {
			if a.Name == assetName {
				assetURL = a.DownloadURL
				break
			}
		}
		if assetURL == "" {
			continue // no asset for this platform on this release
		}
		cands = append(cands, lineCandidate{tag: rel.TagName, assetURL: assetURL, notes: rel.Body})
	}
	return groupLines(cands, u.cfg.current)
}
```
（`Draft` 字段已在 (c) 上方加好。）

(e) `UpdateState` 加字段 `Lines []VersionLine \`json:"lines"\``。

(f) 在 `Check` 主流程填完 latest 之后，调 `u.state.Lines = u.refreshLines(ctx)`。注意锁：`refreshLines` 内部不持 `u.mu`（它只读 cfg + 发 HTTP），赋值 `u.state.Lines` 在已持锁的区段内。**确认 `refreshLines` 不会和持锁区死锁**——它发 HTTP（慢），如果在持 `u.mu` 时调会长时间持锁。**改为**：在 `Check` 释放锁后、或在持锁前先 `refreshLines`（不持锁）拿到 lines，再在持锁区赋值。读 `Check` 的锁结构决定插入点；最简：`Check` 开头不持锁时先算 `lines := u.refreshLines(ctx)`，主流程持锁末尾 `u.state.Lines = lines`。

- [ ] **Step 5: 运行确认通过 + 不破坏现有**

Run: `/home/attson/sdk/go1.24.13/bin/go test ./desktop/ -run 'TestCheck|TestParseVersion|TestGroupLines' -v`
Expected: 全 PASS（含现有 Check 相关测试）

- [ ] **Step 6: Commit**

```bash
git add desktop/updater.go desktop/updater_test.go
git commit -m "feat(updater): fetch releases list and populate version lines in state"
```

---

## Task 4: DownloadVersion — 下载指定线（复用 asset/checksum 解析 + Download）

**Files:**
- Modify: `desktop/updater.go`, `desktop/app.go`
- Test: `desktop/updater_test.go`

- [ ] **Step 1: 写失败测试**

下载逻辑依赖 `state.AssetURL` + `checksumURL` + `checksumSigURL`（由 release 的 assets 解析）。`DownloadVersion(ctx, tag)` 要：找到该 tag 的 release → 重新解析 asset/checksum 进 state → 调现有 `Download`。测试断言：调 `DownloadVersion(ctx, "v0.2.155")` 后 `state.AssetURL` 指向该版本的 asset（用 mock，断言到「设置了正确 AssetURL」即可，不必真下载）。

```go
func TestDownloadVersion_SetsAssetForTag(t *testing.T) {
	asset, err := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skipf("no asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	listJSON := `[
	  {"tag_name":"v0.3.0","prerelease":false,"draft":false,
	   "assets":[{"name":"` + asset + `","browser_download_url":"https://x/v0.3.0","size":10}]},
	  {"tag_name":"v0.2.155","prerelease":false,"draft":false,
	   "assets":[{"name":"` + asset + `","browser_download_url":"https://x/v0.2.155","size":20},
	             {"name":"SHA256SUMS","browser_download_url":"https://x/sums-2-155","size":1},
	             {"name":"SHA256SUMS.sig","browser_download_url":"https://x/sig-2-155","size":1}]}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(listJSON))
	}))
	defer srv.Close()
	u := newUpdater(updaterConfig{current: "v0.2.154", repo: "attson/atterm",
		releasesURL: srv.URL + "/releases", client: srv.Client(), now: time.Now})

	if err := u.prepareVersion(context.Background(), "v0.2.155"); err != nil {
		t.Fatalf("prepareVersion: %v", err)
	}
	if u.state.AssetURL != "https://x/v0.2.155" {
		t.Fatalf("AssetURL = %q, want https://x/v0.2.155", u.state.AssetURL)
	}
	if u.checksumURL != "https://x/sums-2-155" || u.checksumSigURL != "https://x/sig-2-155" {
		t.Fatalf("checksum URLs not set: %q / %q", u.checksumURL, u.checksumSigURL)
	}
}
```

> 注：测试直接读 `u.state.AssetURL` / `u.checksumURL`（同包可访问未导出）。`prepareVersion` 是把「解析某 tag 的 release 进 state」抽出来的内部方法；`DownloadVersion` = `prepareVersion` + `Download`。

- [ ] **Step 2: 运行确认失败**

Run: `/home/attson/sdk/go1.24.13/bin/go test ./desktop/ -run TestDownloadVersion_SetsAssetForTag -v`
Expected: FAIL — `prepareVersion` undefined

- [ ] **Step 3: 实现**

(a) 把 `Check` 主流程里「从一个 `githubRelease` 解析 asset/checksum/notes 进 state」的那段（updater.go 约 line 205-240）抽成内部方法 `applyReleaseLocked(rel *githubRelease)`（如果还没抽）。**若改动大，可不抽，直接在 prepareVersion 内联同样逻辑**——但优先 DRY：抽出 `applyReleaseLocked` 让 Check 和 prepareVersion 共用。

(b) 加 `prepareVersion(ctx, tag)`：
```go
// prepareVersion fetches the releases list, finds the given tag, and applies
// its asset/checksum/notes into state so a subsequent Download targets that
// exact version (the chosen line's latest). Used by DownloadVersion.
func (u *Updater) prepareVersion(ctx context.Context, tag string) error {
	rels, err := u.fetchReleases(ctx)
	if err != nil {
		return err
	}
	var found *githubRelease
	for i := range rels {
		if rels[i].TagName == tag {
			found = &rels[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("version %s not found in releases", tag)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.applyReleaseLocked(found) // sets Latest/AssetURL/checksumURL/checksumSigURL/Notes
	return nil
}

// DownloadVersion prepares the given tag then starts the download.
func (u *Updater) DownloadVersion(ctx context.Context, tag string) error {
	if err := u.prepareVersion(ctx, tag); err != nil {
		return err
	}
	return u.Download(ctx)
}
```

> `applyReleaseLocked` 必须设置 `state.Latest`/`state.Notes`/`state.AssetURL`/`state.AssetSize`/`checksumURL`/`checksumSigURL`/`state.Available`，与现有 Check 主流程逻辑一致。把现有那段重构进它，Check 调它，prepareVersion 也调它。

(c) `desktop/app.go` 加桥接（仿 `StartDownload`）：
```go
// DownloadVersion downloads a specific version (the chosen update line's
// latest tag) instead of the default latest.
func (a *App) DownloadVersion(tag string) error {
	if a.updater == nil {
		return nil
	}
	return a.updater.DownloadVersion(a.ctx, tag)
}
```

- [ ] **Step 4: 运行确认通过**

Run: `/home/attson/sdk/go1.24.13/bin/go test ./desktop/ -run 'TestDownloadVersion|TestCheck|TestGroupLines|TestParseVersion' -v`
Expected: 全 PASS（重构 applyReleaseLocked 不破坏现有 Check 测试）

- [ ] **Step 5: 全包 build + vet + gofmt**

Run: `/home/attson/sdk/go1.24.13/bin/go build -tags webkit2_41 ./desktop/ && /home/attson/sdk/go1.24.13/bin/go vet ./desktop/ && /home/attson/sdk/go1.24.13/bin/gofmt -l desktop/updater.go desktop/app.go`
Expected: build OK / vet 干净 / gofmt 无输出

- [ ] **Step 6: Commit**

```bash
git add desktop/updater.go desktop/app.go desktop/updater_test.go
git commit -m "feat(updater): DownloadVersion targets a chosen version line's latest"
```

---

## Task 5: 前端版本线选择器 UI

**Files:**
- Modify: `desktop/frontend/src/components/SettingsUpdates.vue`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`, `desktop/frontend/src/i18n/messages/zh.ts`
- Test: `desktop/frontend/src/components/SettingsUpdates.test.ts`

- [ ] **Step 1: 先读现有代码**

`cat desktop/frontend/src/components/SettingsUpdates.vue` 和 `SettingsUpdates.test.ts`，看：
- `state` ref 的类型 `UpdateState`（从 wailsjs 导入；Task 3/4 给 Go struct 加了 `Lines` 字段，wailsjs binding 需 regenerate —— 见 Step 2）。
- 现有 current/latest/available/downloading 显示逻辑、下载/安装按钮、emit。
- 测试怎么 mock wailsjs API（`getUpdateState` 等）。

- [ ] **Step 2: regenerate wailsjs binding（让前端能拿到 Lines + DownloadVersion）**

Go struct 改了字段、app 加了 `DownloadVersion`，前端 TS 绑定要更新。运行 wails 的 binding 生成（看 `desktop/scripts/` 或 `package.json` 有无对应命令；通常 `wails generate module` 或构建时自动）。若无独立命令，**手动**在 `desktop/frontend/wailsjs/go/main/App.d.ts`/`.js` 加 `DownloadVersion(arg1:string):Promise<void>`，在 `models.ts` 的 `UpdateState` 加 `lines: VersionLine[]` + 定义 `VersionLine`。先看 wailsjs 目录现有结构照搬。

> 注：本仓 `desktop/frontend/wailsjs/` 已存在生成的绑定（git status 里有 runtime 改动）。以实际生成机制为准；若 dev 模式自动生成，重启 wails dev 即可。

- [ ] **Step 3: 写失败测试**

在 `SettingsUpdates.test.ts` 追加（仿现有测试 mock `getUpdateState` 返回带 lines 的 state）：

```ts
it("renders a radio per version line when multiple lines available", async () => {
  // mock getUpdateState 返回 current=v0.2.154 + lines=[v0.3→v0.3.0, v0.2→v0.2.155]
  // 断言：渲染了 2 个 radio，标签含 "v0.3" 和 "v0.2"，各自显示 latest。
  // 默认选中当前线 (v0.2)。
  // 点击 v0.3 radio + 下载按钮 → 调 DownloadVersion("v0.3.0")。
})

it("falls back to single-button UI when only one line", async () => {
  // lines.length === 1 → 不渲染 radio 列表，显示现有「下载 vX」按钮。
})
```

照搬现有测试的 mock/挂载方式（vitest + @vue/test-utils），断言具体以现有测试风格为准。

- [ ] **Step 4: 运行确认失败**

Run（在 `desktop/frontend/`）: `npm run test -- SettingsUpdates`
Expected: FAIL — 新断言失败（无 radio 渲染）

- [ ] **Step 5: 实现 UI**

在 `SettingsUpdates.vue`：
- 加 `selectedLine` ref（选中的 minor）。watch `state.lines`，默认选中当前线（`lines` 里 minor == 当前版本 minor 的那条；当前 dev 则选第一条 = 最高线）。
- template：当 `state.lines && state.lines.length >= 2` 时渲染 radio 列表：
  ```html
  <div v-for="line in state.lines" :key="line.minor" class="version-line">
    <label>
      <input type="radio" :value="line.minor" v-model="selectedLine" />
      {{ t("settings.updates.versionLine", { minor: line.minor }) }}
      → {{ line.latest }}
    </label>
  </div>
  <button class="primary" :disabled="state.downloading" @click="onDownloadSelected">
    {{ t("settings.updates.downloadVersion", { version: selectedLatest }) }}
  </button>
  ```
  其中 `selectedLatest` = 选中线的 latest，`onDownloadSelected` 调 `DownloadVersion(selectedLatest)`。
- `state.lines.length < 2` 时保留现有单 latest 按钮逻辑（退化）。
- import wailsjs 的 `DownloadVersion`。

- [ ] **Step 6: 加 i18n key**

`en.ts` 的 `settings.updates` 加：`versionLine: "{minor} line"`。`zh.ts` 加：`versionLine: "{minor} 线"`。（`downloadVersion` key 已存在，复用。）

- [ ] **Step 7: 运行确认通过**

Run（`desktop/frontend/`）: `npm run test -- SettingsUpdates`
Expected: PASS

- [ ] **Step 8: 前端 lint/type check**

Run（`desktop/frontend/`）: `npm run build`（或项目的 typecheck 命令，看 package.json）
Expected: 无 TS 错误

- [ ] **Step 9: Commit**

```bash
git add desktop/frontend/src/components/SettingsUpdates.vue desktop/frontend/src/components/SettingsUpdates.test.ts desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh.ts desktop/frontend/wailsjs/
git commit -m "feat(settings): version-line radio selector for updates"
```

---

## Task 6: 端到端验证 + 全量回归

**Files:**
- Test: 全量

- [ ] **Step 1: 后端全量**

Run: `/home/attson/sdk/go1.24.13/bin/go test ./desktop/ && /home/attson/sdk/go1.24.13/bin/go build -tags webkit2_41 ./desktop/ && /home/attson/sdk/go1.24.13/bin/go vet ./desktop/`
Expected: 全 PASS / build OK / vet 干净

- [ ] **Step 2: 前端全量**

Run（`desktop/frontend/`）: `npm run test && npm run build`
Expected: 全 PASS / build OK

- [ ] **Step 3: 手动验证（可选，需真实 GitHub）**

起 desktop（`desktop/scripts/dev-no-hmr.sh`），打开设置→软件更新，确认：
- 当前 v0.2.x 时显示 v0.2.x + v0.3.x 两条线 radio
- 选 v0.3.x → 下载按钮变 "下载 v0.3.0"
- GitHub 不可达时退化到现有单按钮（不报错阻断）

- [ ] **Step 4: Commit（若手动验证有微调）**

```bash
git add -A
git commit -m "test(updater): version-line selector regression pass"
```

---

## Self-Review Notes

- **Spec §4.1 过滤规则（只升不降）** → Task 2 `groupLines`，含「同线更新 / 高线 / dev 全显 / 低线丢弃」的精确测试。
- **Spec §4.2 后端 fetchLines/UpdateState.Lines** → Task 3。
- **Spec §4.2 DownloadVersion 参数化下载** → Task 4（复用 Download + applyReleaseLocked 重构，DRY）。
- **Spec §4.3 App 桥接** → Task 4 Step 3c。
- **Spec §4.4 前端 radio + 退化（≥2 显示选择器 / ==1 单按钮 / 空 fallback）** → Task 5。
- **Spec §4.4 线标签用 minor 不写死语义** → Task 5 i18n `versionLine: "{minor} 线"`（不出现「稳定/特性」硬编码）。
- **Spec §5 优雅降级** → Task 3 `refreshLines` 失败返回 nil + Check 不受影响；Task 5 前端 lines 空退化。
- **Spec §6 测试** → 各 Task 的 TDD 测试覆盖解析/分组/过滤/fallback/UI。
- **类型一致性**：`VersionLine{Minor,Latest,Notes,AssetURL}`（Task 2 定义）在 Task 3（填充）、Task 4（prepareVersion 用 tag 不用 VersionLine）、Task 5（前端 lines）一致。`prepareVersion`/`DownloadVersion`/`refreshLines`/`groupLines`/`parseVersionTag` 签名跨任务一致。
- **最大风险点**：Task 3 Step 4(f) 的**锁**——`refreshLines` 发 HTTP 不能在持 `u.mu` 时调（长时间持锁/潜在死锁）。执行时务必先不持锁算 lines，再在持锁区赋值。Task 3 已显式标注。
- **wailsjs binding 再生成**（Task 5 Step 2）是前端能拿到 Lines/DownloadVersion 的前提，执行时优先确认生成机制。
