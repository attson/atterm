# L1 偏好接入同步 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把终端主题、第 20 项的六个外观设置、快捷键绑定、以及 `default_shell` 接进已有的 `prefssync` 引擎，换台桌面机登同一账号即自动一致。

**Architecture:** 不新建任何机制。引擎（`internal/prefssync`）、adapter（`desktop/prefssync_adapter.go`）、首登播种（`isPrefCustomized` + `SeedFromLocal`）、失效通知（`prefs:changed`，Push 与 Pull 后都发，已桥到平台总线）全部就位。本项加键、加 adapter case、加播种判据，并给前端加不回写的重载监听。

**Tech Stack:** Go 1.23 + Wails v2 + Vue 3 + TypeScript + Vitest

**Spec:** [`docs/superpowers/specs/2026-08-17-prefs-sync-l1-design.md`](../specs/2026-08-17-prefs-sync-l1-design.md)

## Global Constraints

- **九个键，一键一设置**：`terminal_theme`、`terminal_font_head`、`terminal_font_size`、`terminal_line_height`、`terminal_cursor_style`、`terminal_cursor_blink`、`terminal_scrollback`、`default_shell`、`shortcut_bindings`。**不要**把外观打包成一个 blob——prefssync 是 per-key LWW，打包会让 A 机改字号覆盖 B 机改光标样式。
- **`webgl_renderer_enabled` 不接同步。** 它的正确值由本机 GPU 驱动决定（`TerminalView.vue` 记着 NVIDIA + X11 + WebKitGTK 的输入延迟 #48），同步等于传染 bug。母 spec §4 把它列进 L1 是错的。
- **首登播种是防覆盖的唯一防线。** `isPrefCustomized`（`desktop/app.go:1635`）是白名单 switch，**没列的键返回 false**，首登时不会被播种，会被 relay 的空值 pull 覆盖。九个新键都必须加判据，判据是「用户是否**显式设过**」（零值/nil = 未设），不是「是否等于默认值」。
- **重载路径绝不回写。** 监听 `prefs:changed` 重新读取时只能写 ref，不得调 setter、不得 `markPrefDirty`。否则 A pull → 回写 → push → B pull → 回写 → push，两机无限 ping-pong。（同名事件的本地递归已由 `platform/wails.ts` 的 `dispatching` Set 全局挡掉——历史上递归约 1286 层冻死 UI，#334 修的就是它；那条防线管不了「回写并 push」。）
- **`default_shell` 同步但读取侧校验**：pull 到一个本机不存在的**绝对路径** shell 时保留本机值 + 记 warn；非绝对路径（依赖 `$PATH`）不校验直接放行。校验放读取侧，不是写入侧。
- **第 20 项的 `change`-not-`input` 提交时机不许放松。** 填上 key 之后每次提交都会触发后台 Push。
- **迁移写入不打 dirty**，让播种逻辑按「是否已自定义」决定谁上传。
- 用户可见文案中英双份。前端改动要重建 `internal/relay/web-dist/`（pre-commit 钩子只看 `web/`，不会提醒你，但 CI 的 embed drift gate 会红）。
- Go 命令在仓库根跑，需 `export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH`；前端命令在 `desktop/frontend/` 跑。

---

### Task 1: 新字段、迁移与 shell 校验

拆出独立的 `ShortcutBindings` 字段并从旧位置迁移，给 `default_shell` 加存在性校验。本任务不碰 prefssync。

**Files:**
- Modify: `desktop/config.go`（新字段 + 迁移 + `DefaultShellOrDefault` 校验）
- Modify: `desktop/app.go`（`GetShortcutBindings` / `SetShortcutBindings`）
- Test: `desktop/config_test.go`

**Interfaces:**
- Consumes: 无
- Produces:
  - `appConfig.ShortcutBindings map[string]string`（json tag `shortcut_bindings`）
  - `migrateShortcutBindings(*appConfig) bool` —— 就地迁移，返回是否改动过
  - `App.GetShortcutBindings() map[string]string` / `App.SetShortcutBindings(map[string]string) error`

- [ ] **Step 1: 写失败的测试**

追加到 `desktop/config_test.go`：

```go
func TestMigrateShortcutBindings(t *testing.T) {
	t.Run("moves bindings out of the plugin blob and clears the old slot", func(t *testing.T) {
		c := appConfig{}
		c.Plugins.Shortcuts.Bindings = map[string]string{"tab.new": "Mod+KeyP"}
		if !migrateShortcutBindings(&c) {
			t.Fatal("expected migration to report a change")
		}
		if c.ShortcutBindings["tab.new"] != "Mod+KeyP" {
			t.Errorf("binding not moved: %v", c.ShortcutBindings)
		}
		if len(c.Plugins.Shortcuts.Bindings) != 0 {
			t.Errorf("old slot not cleared: %v", c.Plugins.Shortcuts.Bindings)
		}
	})

	t.Run("is idempotent — a migrated config survives a second run", func(t *testing.T) {
		c := appConfig{ShortcutBindings: map[string]string{"tab.new": "Mod+KeyP"}}
		if migrateShortcutBindings(&c) {
			t.Error("expected no change on an already-migrated config")
		}
		if c.ShortcutBindings["tab.new"] != "Mod+KeyP" {
			t.Error("idempotent run must not clear the migrated value")
		}
	})

	t.Run("new field wins when both are populated", func(t *testing.T) {
		c := appConfig{ShortcutBindings: map[string]string{"tab.new": "Mod+KeyN"}}
		c.Plugins.Shortcuts.Bindings = map[string]string{"tab.new": "Mod+KeyP"}
		migrateShortcutBindings(&c)
		if c.ShortcutBindings["tab.new"] != "Mod+KeyN" {
			t.Errorf("new field must win, got %v", c.ShortcutBindings)
		}
		if len(c.Plugins.Shortcuts.Bindings) != 0 {
			t.Error("old slot must still be cleared")
		}
	})

	t.Run("no-op on an empty config", func(t *testing.T) {
		c := appConfig{}
		if migrateShortcutBindings(&c) {
			t.Error("empty config needs no migration")
		}
	})
}

func TestDefaultShellExistenceCheck(t *testing.T) {
	t.Run("absolute path that does not exist falls back to auto", func(t *testing.T) {
		c := appConfig{DefaultShell: "/nonexistent/bin/fish"}
		if got := c.DefaultShellOrDefault(); got != defaultShellAuto {
			t.Errorf("got %q, want %q", got, defaultShellAuto)
		}
	})

	t.Run("absolute path that exists is used", func(t *testing.T) {
		c := appConfig{DefaultShell: "/bin/sh"}
		if got := c.DefaultShellOrDefault(); got != "/bin/sh" {
			t.Errorf("got %q, want /bin/sh", got)
		}
	})

	t.Run("non-absolute value is passed through unchecked", func(t *testing.T) {
		c := appConfig{DefaultShell: "fish"}
		if got := c.DefaultShellOrDefault(); got != "fish" {
			t.Errorf("got %q, want fish — PATH-resolved shells must not be validated", got)
		}
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./desktop/ -run 'TestMigrateShortcutBindings|TestDefaultShellExistenceCheck'`
Expected: FAIL —— 编译错误，`ShortcutBindings` 与 `migrateShortcutBindings` 不存在。

- [ ] **Step 3: 加字段与迁移**

`desktop/config.go` 的 `appConfig` 里，`PinnedSessionIDs` 附近加：

```go
	// ShortcutBindings holds action-id → binding-string overrides. Hoisted out
	// of Plugins.Shortcuts.Bindings so it can be synced on its own key: the
	// plugins blob mixes in machine-local state (which plugins are enabled)
	// that must not travel between devices.
	ShortcutBindings map[string]string `json:"shortcut_bindings,omitempty"`
```

加迁移函数：

```go
// migrateShortcutBindings hoists bindings out of the plugin blob into the
// top-level field, and reports whether it changed anything.
//
// Idempotent by construction: it only copies when the destination is empty,
// and clearing the old slot is unconditional so a half-migrated config
// (both slots populated, only reachable by hand-editing) converges on the
// new field rather than letting two sources drift apart.
func migrateShortcutBindings(c *appConfig) bool {
	old := c.Plugins.Shortcuts.Bindings
	if len(old) == 0 {
		return false
	}
	changed := false
	if len(c.ShortcutBindings) == 0 {
		c.ShortcutBindings = make(map[string]string, len(old))
		for k, v := range old {
			c.ShortcutBindings[k] = v
		}
		changed = true
	} else {
		logWarn("config", "shortcut bindings present in both the new field and the legacy plugin slot; keeping the new field")
	}
	c.Plugins.Shortcuts.Bindings = nil
	return changed
}
```

在 config 载入路径里调用它（找到 `configStore` 读盘后返回 `appConfig` 的地方，紧接着调用；若已有类似的 normalize/migrate 步骤就并进去）。

- [ ] **Step 4: 给 `DefaultShellOrDefault` 加校验**

替换 `desktop/config.go:437` 的函数体：

```go
// DefaultShellOrDefault resolves the configured shell, falling back to auto
// when it cannot be used on THIS machine.
//
// The existence check exists because this preference syncs across devices
// (see the L1 prefs-sync design): an absolute path like
// /opt/homebrew/bin/fish is valid on the Mac it was set on and an unopenable
// path everywhere else, and a bad shell means new sessions fail to start.
// Non-absolute values are PATH-resolved at spawn time, so validating them
// here would reject perfectly good configs.
//
// Validation lives on the read side deliberately. Rejecting on write would
// leave the synced value sitting in config.json while silently not taking
// effect, which is harder to diagnose than falling back.
func (c appConfig) DefaultShellOrDefault() string {
	shell := strings.TrimSpace(c.DefaultShell)
	if shell == "" || strings.EqualFold(shell, defaultShellAuto) {
		return defaultShellAuto
	}
	if filepath.IsAbs(shell) {
		if _, err := os.Stat(shell); err != nil {
			logWarn("config", "configured shell %q is not present on this machine; falling back to auto", shell)
			return defaultShellAuto
		}
	}
	return shell
}
```

确认 `desktop/config.go` 已 import `os` 和 `path/filepath`。

- [ ] **Step 5: 加 Wails 绑定**

`desktop/app.go`，放在 `GetDefaultShell` 附近：

```go
// GetShortcutBindings returns the user's action-id → binding overrides.
func (a *App) GetShortcutBindings() map[string]string {
	if a == nil || a.cfgStore == nil {
		return map[string]string{}
	}
	b := a.cfgStore.Get().ShortcutBindings
	out := make(map[string]string, len(b))
	for k, v := range b {
		out[k] = v
	}
	return out
}

// SetShortcutBindings replaces the override map wholesale.
func (a *App) SetShortcutBindings(bindings map[string]string) error {
	return a.updatePref("shortcut_bindings", func(cfg *appConfig) error {
		cfg.ShortcutBindings = bindings
		return nil
	})
}
```

- [ ] **Step 6: 验证并提交**

Run: `go vet ./... && go test ./desktop/ -timeout 180s`

```bash
git add desktop/config.go desktop/app.go desktop/config_test.go
git commit -m "feat(desktop): hoist shortcut bindings to their own field, validate synced shell"
```

---

### Task 2: 接入 prefssync

九个键进白名单、进 adapter、进播种判据，并把第 20 项六个 setter 的空 key 填上。

**Files:**
- Modify: `internal/prefssync/sync.go`（`syncedKeys`）
- Modify: `desktop/prefssync_adapter.go`（`ReadValue` / `WriteValue`）
- Modify: `desktop/app.go`（`isPrefCustomized`；六个 `updatePref("")` 填 key；`SetTerminalTheme` / `SetDefaultShell` 改走 `updatePref`）
- Test: `internal/prefssync/sync_test.go`、`desktop/prefssync_adapter_test.go`（不存在则创建）

**Interfaces:**
- Consumes: Task 1 的 `ShortcutBindings` 字段
- Produces: 九个新键在 `prefssync.SyncedKeys()` 中可见

- [ ] **Step 1: 写失败的测试**

`desktop/prefssync_adapter_test.go`：

```go
func TestAdapterRoundTripsL1Keys(t *testing.T) {
	keys := []string{
		"terminal_theme", "terminal_font_head", "terminal_font_size",
		"terminal_line_height", "terminal_cursor_style", "terminal_cursor_blink",
		"terminal_scrollback", "default_shell", "shortcut_bindings",
	}
	for _, k := range keys {
		if !slices.Contains(prefssync.SyncedKeys(), k) {
			t.Errorf("%s is not in SyncedKeys()", k)
		}
	}
}

func TestIsPrefCustomizedCoversL1Keys(t *testing.T) {
	blink := false
	customized := appConfig{
		TerminalTheme:       "nord",
		TerminalFontHead:    "JetBrains Mono",
		TerminalFontSize:    16,
		TerminalLineHeight:  1.2,
		TerminalCursorStyle: "bar",
		TerminalCursorBlink: &blink,
		TerminalScrollback:  9000,
		DefaultShell:        "/bin/zsh",
		ShortcutBindings:    map[string]string{"tab.new": "Mod+KeyP"},
	}
	isCustom := isPrefCustomized(customized)
	isVirgin := isPrefCustomized(appConfig{})
	for _, k := range []string{
		"terminal_theme", "terminal_font_head", "terminal_font_size",
		"terminal_line_height", "terminal_cursor_style", "terminal_cursor_blink",
		"terminal_scrollback", "default_shell", "shortcut_bindings",
	} {
		if !isCustom(k) {
			t.Errorf("%s: explicitly set value must count as customized — otherwise first login pulls a remote value over it", k)
		}
		if isVirgin(k) {
			t.Errorf("%s: an untouched config must not count as customized", k)
		}
	}
}
```

`internal/prefssync/sync_test.go` 追加一条锁排除项：

```go
func TestWebglRendererIsNotSynced(t *testing.T) {
	// Its correct value depends on the local GPU driver — syncing it would
	// spread the #48 input-lag bug rather than a preference.
	for _, k := range SyncedKeys() {
		if k == "webgl_renderer_enabled" {
			t.Fatal("webgl_renderer_enabled must never be synced")
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./desktop/ ./internal/prefssync/ -run 'TestAdapter|TestIsPrefCustomized|TestWebgl'`
Expected: FAIL —— 键不在白名单里。

- [ ] **Step 3: 扩白名单**

`internal/prefssync/sync.go` 的 `syncedKeys` 追加九项（保持现有风格，一行一个）。

- [ ] **Step 4: 扩 adapter**

`desktop/prefssync_adapter.go` 的 `ReadValue` switch 加九个 case。标量直接 `json.Marshal`；
`terminal_cursor_blink` 走既有的 `marshalPtr(c.TerminalCursorBlink)`。

`WriteValue` switch 对称加九个 case，各自 `json.Unmarshal` 进对应字段；
`terminal_cursor_blink` 解成 `*bool`。

**注意**：`ReadValue` 读的是**原始字段**而不是 `*OrDefault()` 访问器。同步的是「用户设了什么」，
不是「解析后是什么」——否则一台没设过的机器会把默认值当作显式选择推上去，把另一台的真实选择覆盖掉。

- [ ] **Step 5: 扩播种判据**

`desktop/app.go` 的 `isPrefCustomized` switch 加九个 case，判据一律是「显式设过」：

```go
		case "terminal_theme":
			return c.TerminalTheme != ""
		case "terminal_font_head":
			return c.TerminalFontHead != ""
		case "terminal_font_size":
			return c.TerminalFontSize != 0
		case "terminal_line_height":
			return c.TerminalLineHeight != 0
		case "terminal_cursor_style":
			return c.TerminalCursorStyle != ""
		case "terminal_cursor_blink":
			return c.TerminalCursorBlink != nil
		case "terminal_scrollback":
			return c.TerminalScrollback != 0
		case "default_shell":
			return c.DefaultShell != ""
		case "shortcut_bindings":
			return len(c.ShortcutBindings) > 0
```

- [ ] **Step 6: 填上 setter 的 key**

第 20 项的六个 setter 现在是 `a.updatePref("", ...)`，把空串换成对应键名。
同时把 `SetTerminalTheme`（当前直接 `cfgStore.Set`）和 `SetDefaultShell` 改走
`a.updatePref("terminal_theme", ...)` / `a.updatePref("default_shell", ...)`，保留各自的校验逻辑。

- [ ] **Step 7: 验证并提交**

Run: `go vet ./... && go test ./desktop/ ./internal/prefssync/ -timeout 180s`

```bash
git add internal/prefssync/sync.go internal/prefssync/sync_test.go \
        desktop/prefssync_adapter.go desktop/prefssync_adapter_test.go desktop/app.go
git commit -m "feat(prefssync): sync the L1 preference layer"
```

---

### Task 3: 快捷键前端改读新字段

`SettingsShortcuts.vue` 当前经 `usePluginConfigStore` 读写绑定，改成走新的 Wails 绑定。

**Files:**
- Modify: `desktop/frontend/src/lib/api.ts`（两个包装）
- Modify: `desktop/frontend/src/components/SettingsShortcuts.vue`
- Modify: `desktop/frontend/src/composables/useTerminalShortcuts.ts` 的调用方（`App.vue` 传 bindings 的地方）
- Test: `desktop/frontend/src/components/SettingsShortcuts.test.ts`

**Interfaces:**
- Consumes: Task 1 的 `GetShortcutBindings` / `SetShortcutBindings`
- Produces: `lib/api.ts` 导出 `getShortcutBindings(): Promise<Record<string,string>>` / `setShortcutBindings(b: Record<string,string>): Promise<void>`

- [ ] **Step 1: 加 api 包装**（照 `getTerminalThemePreference` 的一行风格）

- [ ] **Step 2: 写失败的测试**

先读 `SettingsShortcuts.test.ts` 顶部沿用它既有的 mount 与 bindings mock 方式，再加：

```ts
it("loads bindings from the dedicated Go field, not the plugin config", async () => {
  const w = await mountShortcuts();
  expect(getShortcutBindingsMock).toHaveBeenCalled();
});

it("saves through setShortcutBindings", async () => {
  const w = await mountShortcuts();
  // …改一个绑定并保存…
  expect(setShortcutBindingsMock).toHaveBeenCalledWith(
    expect.objectContaining({ "tab.new": "Mod+KeyP" }),
  );
});
```

- [ ] **Step 3: 改组件**

把 `usePluginConfigStore` 的读写换成新 api。`App.vue` 里给 `useTerminalShortcuts` 传的
`bindings` ref 也改成从新来源加载。

- [ ] **Step 4: 验证并提交**

Run: `npx vitest run src/components/SettingsShortcuts.test.ts && npm test && npm run build`

```bash
git add src/lib/api.ts src/components/SettingsShortcuts.vue src/components/SettingsShortcuts.test.ts src/App.vue
git commit -m "feat(frontend): read shortcut bindings from their own preference key"
```

---

### Task 4: pull 后刷新界面（且不回写）

**Files:**
- Modify: `desktop/frontend/src/App.vue`
- Modify: `desktop/frontend/src/components/SettingsTerminalAppearance.vue`
- Modify: `desktop/frontend/src/components/SettingsShortcuts.vue`
- Test: `desktop/frontend/src/components/TerminalView.test.ts`（App 侧源码断言）+ 组件 mount 测试
- Modify: `docs/roadmap.md`、`internal/relay/web-dist/`

**Interfaces:**
- Consumes: Task 2 的同步、Task 3 的新 api
- Produces: 无下游

- [ ] **Step 1: 写失败的测试**

`TerminalView.test.ts` 追加：

```ts
describe("prefs:changed reload", () => {
  test("App re-reads theme and appearance when prefs change remotely", () => {
    expect(appSource).toMatch(/events\.on\(\s*["']prefs:changed["']/);
    const body = appSource.match(/events\.on\(\s*["']prefs:changed["'][\s\S]*?\n\s*\}\)/);
    expect(body).not.toBeNull();
    expect(body![0]).toMatch(/refreshTerminalTheme\(\)/);
    expect(body![0]).toMatch(/refreshTerminalAppearance\(\)/);
  });

  test("the reload path never writes back — that would ping-pong between devices", () => {
    const body = appSource.match(/events\.on\(\s*["']prefs:changed["'][\s\S]*?\n\s*\}\)/);
    expect(body![0]).not.toMatch(/setTerminal|setDefaultShell|setShortcutBindings/);
  });
});
```

`SettingsTerminalAppearance.test.ts` 加一条真正的行为测试：

```ts
it("reloads from Go on prefs:changed without persisting", async () => {
  const w = factory();
  setTerminalFontSizeMock.mockClear();
  events.emit("prefs:changed", undefined);
  await flushPromises();
  expect(getTerminalFontSizeMock).toHaveBeenCalled();
  expect(setTerminalFontSizeMock).not.toHaveBeenCalled();
});
```

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 实现监听**

`App.vue`：在既有的平台事件订阅处加 `prefs:changed` 监听，调
`refreshTerminalTheme()` + `refreshTerminalAppearance()`。**只读不写。**

`SettingsTerminalAppearance.vue` 与 `SettingsShortcuts.vue`：同样订阅并重新加载，
用与 `onMounted` 相同的加载函数；确保加载路径不触发任何 `set*`。

- [ ] **Step 4: 验证**

Run: `npm test && npm run build`

- [ ] **Step 5: 重建 web 产物**

Run（仓库根，Node 20）：`nvm use 20 && ./scripts/build-web.sh`

- [ ] **Step 6: 勾 roadmap**

`docs/roadmap.md` 第 21 项六个复选框改 `[x]`。同时按 design §10 修正母 spec
`2026-08-16-sync-layer-roadmap-design.md` §4 的 L1 表——把「渲染器」从同步内容物里去掉并注明原因。

- [ ] **Step 7: 手动验证**（能跑 GUI 才做；跑不了就如实报告未执行）

两台桌面机登同一账号：A 改字号 / 主题 / 一个快捷键 → B 在数秒内自动跟上；
B 上未被改动的设置保持不变；A 上设一个 B 没有的 shell 绝对路径 → B 不受影响、回落 auto。

- [ ] **Step 8: 提交**

```bash
cd "$(git rev-parse --show-toplevel)"
git add desktop/frontend/src internal/relay/web-dist docs/roadmap.md \
        docs/superpowers/specs/2026-08-16-sync-layer-roadmap-design.md
git commit -m "feat(prefs): reload the UI when preferences change remotely"
```
