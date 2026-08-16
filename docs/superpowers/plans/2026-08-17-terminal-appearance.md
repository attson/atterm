# 终端外观设置 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把终端的字体族、字号、行高、光标样式/闪烁、scrollback 行数从硬编码常量提成用户设置，改完所有 pane 立即生效。

**Architecture:** 沿用 `terminal_theme` 已跑通的链路——`appConfig` 字段 → `App.Get*/Set*`（Wails 绑定）→ `lib/api.ts` 包装 → `App.vue` 响应式状态 → 一个 `appearance` 对象 prop 下钻 `PaneGrid` → `TerminalView` watch 写进 `term.options.*`。字体族只让用户换链首，尾部锁死以保住红线 #13。

**Tech Stack:** Go 1.23 + Wails v2 + Vue 3 + TypeScript + Vitest + `xterm@5.3`

**Spec:** [`docs/superpowers/specs/2026-08-17-terminal-appearance-design.md`](../specs/2026-08-17-terminal-appearance-design.md)（母 spec：[2026-08-16 sync-layer roadmap](../specs/2026-08-16-sync-layer-roadmap-design.md) §5 P5 第 20 项）

## Global Constraints

- **默认值必须精确等于今天的行为**：`fontSize: 13`、`cursorBlink: true`、`scrollback: 5000`（三者是 `TerminalView.vue` 里 `new Terminal({...})` 的现有字面量）、`lineHeight: 1.0`（xterm 不设时的默认）、字体族 = 现有 `TERMINAL_FONT_FAMILY` 原链。升级上来的用户不得看出任何变化。
- **红线 #13（CJK-first 字体栈）不可绕过**：ASCII 等宽族在前、CJK 族在后、`monospace` 收尾的顺序是 iOS 26 WebKit 的 `[?]` 方框与错宽重叠的唯一防线。用户只能选**链首**，既有整条链永远作为尾部拼在后面。**不提供自由文本输入字体名。**
- **红线 #17（viewer 锁尺寸）**：viewer 模式下不得跑 FitAddon、不得发 RESIZE；viewer 的 cols/rows 由 `onMeta` 的 PTY 尺寸决定。改字号在 viewer pane 上只改本地渲染。
- **红线 #6（RESIZE 路径）**：需要 fit 时走既有的 `sendResize`，它已在 WS CONNECTING 时排队并比对 `expectedCols/Rows` 跳过无意义 RESIZE。不要另开一条发 RESIZE 的路。
- **scrollback 默认 5000、上限 20000**。20000 是 #343 之前的值——已知单 pane 能活、十几个 pane 会痛。设置项旁必须按 2.75 KB/行估算并显示**每个 pane** 的内存开销。
- **本项不接 `prefssync`**。同步是第 21 项。Go 侧写入统一走 `a.updatePref("", ...)`——空 key 表示不触发 dirty-push，第 21 项只需把 key 填上。
- **web / Capacitor 不在范围内**：`App.vue` 的 `refreshTerminalTheme()` 本来就包在 `if (caps.wailsBindings)` 里，web 分支不调它，所以外观偏好今天就是桌面专属。新设置沿用同一门控，web 与移动端拿默认值。
- **用户可见文案必须中英双份**：`src/i18n/messages/en.ts` 与 `zh-CN.ts` 同步新增，键名一致。
- **`desktop/frontend/` 的改动会改变 web 构建产物**。`.githooks/pre-commit` 只监听 `web/` 路径不会拦，但 CI 的 embed drift job 会红。**最后一个 task 必须 `nvm use 20 && ./scripts/build-web.sh` 并提交 `internal/relay/web-dist/`。**
- Go 命令在仓库根跑；前端命令在 `desktop/frontend/` 跑。Go 需要 `export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH`。

---

### Task 1: Go 配置字段与绑定

五个字段落进 `appConfig`，各带一个 `*OrDefault()` 访问器做钳制，再暴露 `Get*/Set*` 给 Wails。本任务不碰前端。

**Files:**
- Modify: `desktop/config.go`（`appConfig` struct + 新增 `*OrDefault()` 访问器）
- Modify: `desktop/app.go`（五对 `Get*/Set*`，放在 `SetTerminalTheme` 之后）
- Test: `desktop/config_test.go`（若不存在则创建）

**Interfaces:**
- Consumes: 无
- Produces（前端 task 依赖这些**确切**的方法名与类型）：
  - `GetTerminalFontHead() string` / `SetTerminalFontHead(head string) error`
  - `GetTerminalFontSize() int` / `SetTerminalFontSize(px int) error`
  - `GetTerminalLineHeight() float64` / `SetTerminalLineHeight(v float64) error`
  - `GetTerminalCursorStyle() string` / `SetTerminalCursorStyle(style string) error`
  - `GetTerminalCursorBlink() bool` / `SetTerminalCursorBlink(on bool) error`
  - `GetTerminalScrollback() int` / `SetTerminalScrollback(lines int) error`

- [ ] **Step 1: 写失败的测试**

创建或追加 `desktop/config_test.go`：

```go
func TestTerminalAppearanceDefaults(t *testing.T) {
	var c appConfig
	if got := c.TerminalFontSizeOrDefault(); got != 13 {
		t.Errorf("font size default = %d, want 13", got)
	}
	if got := c.TerminalLineHeightOrDefault(); got != 1.0 {
		t.Errorf("line height default = %v, want 1.0", got)
	}
	if got := c.TerminalCursorStyleOrDefault(); got != "block" {
		t.Errorf("cursor style default = %q, want block", got)
	}
	if got := c.TerminalCursorBlinkOrDefault(); got != true {
		t.Errorf("cursor blink default = %v, want true", got)
	}
	if got := c.TerminalScrollbackOrDefault(); got != 5000 {
		t.Errorf("scrollback default = %d, want 5000", got)
	}
	if got := c.TerminalFontHeadOrDefault(); got != "" {
		t.Errorf("font head default = %q, want empty (system default)", got)
	}
}

func TestTerminalAppearanceClamping(t *testing.T) {
	cases := []struct {
		name string
		set  func(*appConfig)
		want func(appConfig) bool
	}{
		{"font size below floor", func(c *appConfig) { c.TerminalFontSize = 2 },
			func(c appConfig) bool { return c.TerminalFontSizeOrDefault() == 8 }},
		{"font size above ceiling", func(c *appConfig) { c.TerminalFontSize = 999 },
			func(c appConfig) bool { return c.TerminalFontSizeOrDefault() == 32 }},
		{"line height below floor", func(c *appConfig) { c.TerminalLineHeight = 0.1 },
			func(c appConfig) bool { return c.TerminalLineHeightOrDefault() == 1.0 }},
		{"line height above ceiling", func(c *appConfig) { c.TerminalLineHeight = 9 },
			func(c appConfig) bool { return c.TerminalLineHeightOrDefault() == 2.0 }},
		{"scrollback above ceiling", func(c *appConfig) { c.TerminalScrollback = 10_000_000 },
			func(c appConfig) bool { return c.TerminalScrollbackOrDefault() == 20000 }},
		{"scrollback below floor", func(c *appConfig) { c.TerminalScrollback = -5 },
			func(c appConfig) bool { return c.TerminalScrollbackOrDefault() == 5000 }},
		{"unknown cursor style falls back", func(c *appConfig) { c.TerminalCursorStyle = "spiral" },
			func(c appConfig) bool { return c.TerminalCursorStyleOrDefault() == "block" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c appConfig
			tc.set(&c)
			if !tc.want(c) {
				t.Errorf("clamp failed for %s", tc.name)
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH && go test ./desktop/ -run TestTerminalAppearance`
Expected: FAIL —— 编译错误，`appConfig` 没有这些字段和方法。

- [ ] **Step 3: 加字段与访问器**

在 `desktop/config.go` 的 `appConfig` struct 里，`TerminalTheme` 那行之后加：

```go
	// Terminal appearance. Zero values mean "never set" and resolve to the
	// hardcoded defaults that shipped before these became settings, so an
	// upgrading user sees no change. Accessors clamp rather than reject:
	// a corrupt config.json should degrade to a usable terminal, not a
	// broken one.
	TerminalFontHead   string  `json:"terminal_font_head,omitempty"`
	TerminalFontSize   int     `json:"terminal_font_size,omitempty"`
	TerminalLineHeight float64 `json:"terminal_line_height,omitempty"`
	TerminalCursorStyle string `json:"terminal_cursor_style,omitempty"`
	TerminalCursorBlink *bool  `json:"terminal_cursor_blink,omitempty"`
	TerminalScrollback int     `json:"terminal_scrollback,omitempty"`
```

注意 `TerminalCursorBlink` 用 `*bool`：默认是 `true`，普通 `bool` 的零值 `false`
无法与「用户显式关掉」区分。这与文件里 `AutoCheckUpdates` / `NotificationsEnabled`
等既有布尔偏好的写法一致。

在 `TerminalThemeOrDefault` 之后加访问器与常量：

```go
const (
	terminalFontSizeDefault = 13
	terminalFontSizeMin     = 8
	terminalFontSizeMax     = 32

	terminalLineHeightDefault = 1.0
	terminalLineHeightMin     = 1.0
	terminalLineHeightMax     = 2.0

	terminalCursorStyleDefault = "block"

	// terminalScrollbackDefault is the value #343 settled on after output
	// floods pushed memory to ~600 MB across a dozen panes. The ceiling is
	// the pre-#343 value: known to survive on one pane, known to hurt across
	// many. Roughly 2.75 KB/line at 200 columns, per pane.
	terminalScrollbackDefault = 5000
	terminalScrollbackMax     = 20000
)

func isSupportedCursorStyle(s string) bool {
	switch s {
	case "block", "underline", "bar":
		return true
	}
	return false
}

func (c appConfig) TerminalFontHeadOrDefault() string { return strings.TrimSpace(c.TerminalFontHead) }

func (c appConfig) TerminalFontSizeOrDefault() int {
	if c.TerminalFontSize == 0 {
		return terminalFontSizeDefault
	}
	if c.TerminalFontSize < terminalFontSizeMin {
		return terminalFontSizeMin
	}
	if c.TerminalFontSize > terminalFontSizeMax {
		return terminalFontSizeMax
	}
	return c.TerminalFontSize
}

func (c appConfig) TerminalLineHeightOrDefault() float64 {
	if c.TerminalLineHeight == 0 {
		return terminalLineHeightDefault
	}
	if c.TerminalLineHeight < terminalLineHeightMin {
		return terminalLineHeightMin
	}
	if c.TerminalLineHeight > terminalLineHeightMax {
		return terminalLineHeightMax
	}
	return c.TerminalLineHeight
}

func (c appConfig) TerminalCursorStyleOrDefault() string {
	if isSupportedCursorStyle(c.TerminalCursorStyle) {
		return c.TerminalCursorStyle
	}
	return terminalCursorStyleDefault
}

func (c appConfig) TerminalCursorBlinkOrDefault() bool {
	if c.TerminalCursorBlink == nil {
		return true
	}
	return *c.TerminalCursorBlink
}

func (c appConfig) TerminalScrollbackOrDefault() int {
	if c.TerminalScrollback <= 0 {
		return terminalScrollbackDefault
	}
	if c.TerminalScrollback > terminalScrollbackMax {
		return terminalScrollbackMax
	}
	return c.TerminalScrollback
}
```

确认 `desktop/config.go` 已 import `strings`；没有就加。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./desktop/ -run TestTerminalAppearance`
Expected: PASS

- [ ] **Step 5: 加 Wails 绑定**

在 `desktop/app.go` 的 `SetTerminalTheme` 之后加六对方法。写入一律走 `a.updatePref("", ...)`
（空 key = 暂不参与 prefssync，见 Global Constraints）：

```go
// GetTerminalFontHead returns the user's chosen leading monospace family,
// or "" for the built-in chain. Only the head is user-controlled — the
// CJK-aware tail is composed on the frontend (redline #13).
func (a *App) GetTerminalFontHead() string {
	if a == nil || a.cfgStore == nil {
		return ""
	}
	return a.cfgStore.Get().TerminalFontHeadOrDefault()
}

func (a *App) SetTerminalFontHead(head string) error {
	return a.updatePref("", func(cfg *appConfig) error {
		cfg.TerminalFontHead = strings.TrimSpace(head)
		return nil
	})
}

func (a *App) GetTerminalFontSize() int {
	if a == nil || a.cfgStore == nil {
		return terminalFontSizeDefault
	}
	return a.cfgStore.Get().TerminalFontSizeOrDefault()
}

func (a *App) SetTerminalFontSize(px int) error {
	if px < terminalFontSizeMin || px > terminalFontSizeMax {
		return fmt.Errorf("font size out of range: %d", px)
	}
	return a.updatePref("", func(cfg *appConfig) error {
		cfg.TerminalFontSize = px
		return nil
	})
}

func (a *App) GetTerminalLineHeight() float64 {
	if a == nil || a.cfgStore == nil {
		return terminalLineHeightDefault
	}
	return a.cfgStore.Get().TerminalLineHeightOrDefault()
}

func (a *App) SetTerminalLineHeight(v float64) error {
	if v < terminalLineHeightMin || v > terminalLineHeightMax {
		return fmt.Errorf("line height out of range: %v", v)
	}
	return a.updatePref("", func(cfg *appConfig) error {
		cfg.TerminalLineHeight = v
		return nil
	})
}

func (a *App) GetTerminalCursorStyle() string {
	if a == nil || a.cfgStore == nil {
		return terminalCursorStyleDefault
	}
	return a.cfgStore.Get().TerminalCursorStyleOrDefault()
}

func (a *App) SetTerminalCursorStyle(style string) error {
	style = strings.TrimSpace(style)
	if !isSupportedCursorStyle(style) {
		return fmt.Errorf("bad cursor style: %s", style)
	}
	return a.updatePref("", func(cfg *appConfig) error {
		cfg.TerminalCursorStyle = style
		return nil
	})
}

func (a *App) GetTerminalCursorBlink() bool {
	if a == nil || a.cfgStore == nil {
		return true
	}
	return a.cfgStore.Get().TerminalCursorBlinkOrDefault()
}

func (a *App) SetTerminalCursorBlink(on bool) error {
	return a.updatePref("", func(cfg *appConfig) error {
		cfg.TerminalCursorBlink = &on
		return nil
	})
}

func (a *App) GetTerminalScrollback() int {
	if a == nil || a.cfgStore == nil {
		return terminalScrollbackDefault
	}
	return a.cfgStore.Get().TerminalScrollbackOrDefault()
}

func (a *App) SetTerminalScrollback(lines int) error {
	if lines <= 0 || lines > terminalScrollbackMax {
		return fmt.Errorf("scrollback out of range: %d", lines)
	}
	return a.updatePref("", func(cfg *appConfig) error {
		cfg.TerminalScrollback = lines
		return nil
	})
}
```

- [ ] **Step 6: 验证**

Run:
```
go vet ./... && go test ./desktop/ -timeout 120s
```
Expected: vet 干净、测试全过。

- [ ] **Step 7: 提交**

```bash
git add desktop/config.go desktop/app.go desktop/config_test.go
git commit -m "feat(desktop): persist terminal appearance preferences"
```

---

### Task 2: 字体链组合纯函数

把 `terminalFont.ts` 从「导出一个常量」改成「导出原链 + 预设列表 + 组合函数」。红线 #13 的
CJK 尾部由此在构造上无法被绕过。本任务不碰任何组件。

**Files:**
- Modify: `desktop/frontend/src/lib/terminalFont.ts`
- Test: `desktop/frontend/src/lib/terminalFont.test.ts`（新建）

**Interfaces:**
- Consumes: 无
- Produces:
  - `TERMINAL_FONT_FAMILY: string` —— **保持导出且值不变**（现有 import 方不能坏）
  - `TERMINAL_FONT_PRESETS: readonly { id: string; label: string }[]` —— `id` 是 CSS font-family 名，`id: ""` 表示系统默认
  - `composeFontFamily(head: string): string`

- [ ] **Step 1: 写失败的测试**

创建 `src/lib/terminalFont.test.ts`：

```ts
import { describe, expect, it } from "vitest";
import {
  TERMINAL_FONT_FAMILY,
  TERMINAL_FONT_PRESETS,
  composeFontFamily,
} from "./terminalFont";

describe("composeFontFamily", () => {
  it("returns the built-in chain unchanged for the system default", () => {
    expect(composeFontFamily("")).toBe(TERMINAL_FONT_FAMILY);
    expect(composeFontFamily("   ")).toBe(TERMINAL_FONT_FAMILY);
  });

  it("prepends the chosen head to the built-in chain", () => {
    expect(composeFontFamily("JetBrains Mono")).toBe(
      `"JetBrains Mono", ${TERMINAL_FONT_FAMILY}`,
    );
  });

  it("does not double-quote a head that is already quoted", () => {
    expect(composeFontFamily('"Fira Code"')).toBe(`"Fira Code", ${TERMINAL_FONT_FAMILY}`);
  });

  it("keeps the CJK tail present for every preset — redline #13", () => {
    for (const p of TERMINAL_FONT_PRESETS) {
      const chain = composeFontFamily(p.id);
      expect(chain).toContain("PingFang SC");
      expect(chain).toContain("Microsoft YaHei");
      expect(chain).toContain("Noto Sans Mono CJK SC");
      expect(chain.endsWith("monospace")).toBe(true);
    }
  });

  it("never lets the head displace the ASCII-mono families", () => {
    const chain = composeFontFamily("JetBrains Mono");
    expect(chain.indexOf("JetBrains Mono")).toBeLessThan(chain.indexOf("Menlo"));
    expect(chain.indexOf("Menlo")).toBeLessThan(chain.indexOf("PingFang SC"));
  });

  it("offers a system-default preset with an empty id", () => {
    expect(TERMINAL_FONT_PRESETS.some((p) => p.id === "")).toBe(true);
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `npx vitest run src/lib/terminalFont.test.ts`
Expected: FAIL —— `composeFontFamily` / `TERMINAL_FONT_PRESETS` 未导出。

- [ ] **Step 3: 实现**

在 `src/lib/terminalFont.ts` 末尾追加（**不要改动既有注释块与 `TERMINAL_FONT_FAMILY` 的值**）：

```ts
// User-selectable heads for the font chain. The user only ever chooses what
// goes in FRONT of TERMINAL_FONT_FAMILY — the CJK-aware tail above is always
// appended, so redline #13's fallback order cannot be bypassed by a setting.
// An id of "" means "no head": use the built-in chain as-is.
//
// Labels are font names, not translated strings: a font is called
// "JetBrains Mono" in every locale.
export const TERMINAL_FONT_PRESETS: readonly { id: string; label: string }[] = [
  { id: "", label: "System default" },
  { id: "SF Mono", label: "SF Mono" },
  { id: "JetBrains Mono", label: "JetBrains Mono" },
  { id: "Fira Code", label: "Fira Code" },
  { id: "Cascadia Code", label: "Cascadia Code" },
  { id: "Source Code Pro", label: "Source Code Pro" },
  { id: "IBM Plex Mono", label: "IBM Plex Mono" },
] as const;

// composeFontFamily prepends the user's chosen family to the built-in chain.
// A missing font simply falls through to the chain at per-glyph fallback time,
// so no availability probing is needed (and document.fonts.check reports false
// positives for monospace families on WebKit anyway).
export function composeFontFamily(head: string): string {
  const trimmed = head.trim();
  if (!trimmed) return TERMINAL_FONT_FAMILY;
  const quoted = trimmed.startsWith('"') ? trimmed : `"${trimmed}"`;
  return `${quoted}, ${TERMINAL_FONT_FAMILY}`;
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `npx vitest run src/lib/terminalFont.test.ts`
Expected: PASS（6 个用例）

- [ ] **Step 5: 验证既有 import 方没坏**

Run: `npm run build`
Expected: 构建成功。`TERMINAL_FONT_FAMILY` 仍被 `TerminalView.vue` 等处 import，值未变。

- [ ] **Step 6: 提交**

```bash
git add src/lib/terminalFont.ts src/lib/terminalFont.test.ts
git commit -m "feat(frontend): add font-chain presets and composeFontFamily"
```

---

### Task 3: Settings UI

在 Settings → General 的终端主题下面加五个控件，中英文案齐备。

**Files:**
- Modify: `desktop/frontend/src/lib/api.ts`（六对包装函数）
- Modify: `desktop/frontend/src/components/SettingsGeneral.vue`
- Modify: `desktop/frontend/src/i18n/messages/en.ts` + `zh-CN.ts`
- Test: `desktop/frontend/src/components/SettingsGeneral.test.ts`

**Interfaces:**
- Consumes: Task 1 的六对 Wails 方法名；Task 2 的 `TERMINAL_FONT_PRESETS`
- Produces:
  - `lib/api.ts` 导出 `getTerminalFontHead` / `setTerminalFontHead` / `getTerminalFontSize` / `setTerminalFontSize` / `getTerminalLineHeight` / `setTerminalLineHeight` / `getTerminalCursorStyle` / `setTerminalCursorStyle` / `getTerminalCursorBlink` / `setTerminalCursorBlink` / `getTerminalScrollback` / `setTerminalScrollback`
  - `SettingsGeneral.vue` 新增 emit `appearance-changed`，载荷 `{ fontHead, fontSize, lineHeight, cursorStyle, cursorBlink, scrollback }`（Task 4 消费）

- [ ] **Step 1: 加 api 包装**

在 `src/lib/api.ts` 的 `setTerminalThemePreference` 之后，按该文件既有风格加十二个一行包装：

```ts
export function getTerminalFontHead(): Promise<string> {
  return bindings().GetTerminalFontHead();
}

export function setTerminalFontHead(head: string): Promise<void> {
  return bindings().SetTerminalFontHead(head);
}
```

其余五对同构（`GetTerminalFontSize`/`SetTerminalFontSize`、`GetTerminalLineHeight`/
`SetTerminalLineHeight`、`GetTerminalCursorStyle`/`SetTerminalCursorStyle`、
`GetTerminalCursorBlink`/`SetTerminalCursorBlink`、`GetTerminalScrollback`/
`SetTerminalScrollback`），返回类型分别为 `Promise<number>` / `Promise<number>` /
`Promise<string>` / `Promise<boolean>` / `Promise<number>`。

Wails 会在 `wails dev` / `wails build` 时重新生成 `wailsjs/go/main/App.*`；若类型报
「方法不存在」，先跑一次 `cd desktop && wails build -tags webkit2_41`（macOS 省略 `-tags`）
让它重生成。

- [ ] **Step 2: 加 i18n 键**

`src/i18n/messages/en.ts` 的 `settings.general` 块里，`terminalThemeHint` 之后加：

```ts
      terminalFont: "terminal font",
      terminalFontHint: "Your choice is placed ahead of the built-in fallback chain; CJK glyphs keep using the bundled families.",
      terminalFontSize: "font size",
      terminalLineHeight: "line height",
      terminalCursorStyle: "cursor style",
      terminalCursorStyleBlock: "Block",
      terminalCursorStyleUnderline: "Underline",
      terminalCursorStyleBar: "Bar",
      terminalCursorBlink: "blink the cursor",
      terminalScrollback: "scrollback lines",
      terminalScrollbackHint: "Roughly {mb} MB per pane at this setting. Every open pane pays it.",
```

`zh-CN.ts` 同一位置：

```ts
      terminalFont: "终端字体",
      terminalFontHint: "所选字体会排在内置回退链之前;中日韩字符仍由内置字体族渲染。",
      terminalFontSize: "字号",
      terminalLineHeight: "行高",
      terminalCursorStyle: "光标样式",
      terminalCursorStyleBlock: "方块",
      terminalCursorStyleUnderline: "下划线",
      terminalCursorStyleBar: "竖线",
      terminalCursorBlink: "光标闪烁",
      terminalScrollback: "回滚行数",
      terminalScrollbackHint: "当前设置下每个 pane 约占 {mb} MB,开几个就是几份。",
```

- [ ] **Step 3: 写失败的测试**

追加到 `src/components/SettingsGeneral.test.ts`（沿用该文件既有的 mount factory 与
bindings mock 方式；先读该文件顶部再照抄它的 setup）：

```ts
describe("SettingsGeneral terminal appearance", () => {
  it("renders all five appearance controls", async () => {
    const w = await mountSettings();
    expect(w.find('[data-test="terminal-font"]').exists()).toBe(true);
    expect(w.find('[data-test="terminal-font-size"]').exists()).toBe(true);
    expect(w.find('[data-test="terminal-line-height"]').exists()).toBe(true);
    expect(w.find('[data-test="terminal-cursor-style"]').exists()).toBe(true);
    expect(w.find('[data-test="terminal-cursor-blink"]').exists()).toBe(true);
    expect(w.find('[data-test="terminal-scrollback"]').exists()).toBe(true);
  });

  it("shows a per-pane memory estimate that tracks the scrollback value", async () => {
    const w = await mountSettings();
    const input = w.find('[data-test="terminal-scrollback"]');
    await input.setValue("20000");
    // 20000 lines * 2.75 KB ≈ 55 MB
    expect(w.find('[data-test="terminal-scrollback-hint"]').text()).toContain("55");
  });

  it("emits appearance-changed when the font size changes", async () => {
    const w = await mountSettings();
    await w.find('[data-test="terminal-font-size"]').setValue("16");
    await w.find('[data-test="terminal-font-size"]').trigger("change");
    const ev = w.emitted("appearance-changed");
    expect(ev).toBeTruthy();
    expect((ev!.at(-1)![0] as any).fontSize).toBe(16);
  });
});
```

- [ ] **Step 4: 跑测试确认失败**

Run: `npx vitest run src/components/SettingsGeneral.test.ts`
Expected: FAIL —— 找不到这些 `data-test` 节点。

- [ ] **Step 5: 实现控件**

在 `SettingsGeneral.vue` 的终端主题字段之后加五组控件，沿用该文件既有的
`.field-label` + 控件 + hint 结构，并复用 `SelectDropdown.vue` 做下拉（字体、光标样式）。
数值项用 `<input type="number">` 并设 `min`/`max`/`step`：字号 8–32 step 1、
行高 1.0–2.0 step 0.1、scrollback 500–20000 step 500。

**提交时机用 `change` 而非 `input`**（Global Constraints / spec §6 风险 2）：连续拖动
数字输入会连发，每次都触发一轮 fit + SIGWINCH。

每个控件的 `data-test` 用上面测试里的名字。内存估算显示在
`[data-test="terminal-scrollback-hint"]`，按 `lines * 2.75 / 1024` 四舍五入到整数 MB，
填进 `terminalScrollbackHint` 的 `{mb}` 占位。

每次改动：调对应的 `set*` api 持久化，然后 `emit("appearance-changed", {...})` 带上
全部六个当前值，让 Task 4 的 `App.vue` 一次更新。

- [ ] **Step 6: 跑测试确认通过**

Run: `npx vitest run src/components/SettingsGeneral.test.ts && npm run build`
Expected: PASS + 构建成功。

- [ ] **Step 7: 提交**

```bash
git add src/lib/api.ts src/components/SettingsGeneral.vue src/components/SettingsGeneral.test.ts \
        src/i18n/messages/en.ts src/i18n/messages/zh-CN.ts
git commit -m "feat(frontend): terminal appearance controls in Settings"
```

---

### Task 4: 即时生效

把设置接到活着的终端上：`App.vue` 读取并持有状态，一个对象 prop 下钻到 `TerminalView`，
`TerminalView` watch 后写进 `term.options.*` 并在必要时重新 fit。

**Files:**
- Modify: `desktop/frontend/src/App.vue`
- Modify: `desktop/frontend/src/components/PaneGrid.vue`
- Modify: `desktop/frontend/src/components/TerminalView.vue`
- Test: `desktop/frontend/src/components/TerminalView.test.ts`
- Modify: `docs/roadmap.md`（勾选第 20 项）
- Modify: `internal/relay/web-dist/`（重建产物）

**Interfaces:**
- Consumes: Task 2 的 `composeFontFamily`；Task 3 的 `appearance-changed` 事件与 api getter
- Produces: 无下游

- [ ] **Step 1: 写失败的测试**

追加到 `src/components/TerminalView.test.ts`（该文件用 `?raw` 源码正则断言，**不要**改成 mount）：

```ts
describe("TerminalView appearance", () => {
  test("seeds the terminal from the appearance prop instead of literals", () => {
    expect(source).toContain("appearance?: TerminalAppearance");
    expect(source).toMatch(/fontFamily:\s*composeFontFamily\(/);
    expect(source).toMatch(/fontSize:\s*props\.appearance/);
    expect(source).toMatch(/scrollback:\s*props\.appearance/);
  });

  test("re-fits only for metrics-affecting changes, and only as driver", () => {
    const body = source.match(/function\s+applyAppearance\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(body).not.toBeNull();
    expect(body![0]).toMatch(/isDriver\.value/);
    expect(body![0]).toMatch(/fontFamily|fontSize|lineHeight/);
  });

  test("cursor and scrollback changes do not trigger a fit", () => {
    const body = source.match(/function\s+applyAppearance\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(body![0]).toMatch(/metricsChanged/);
  });

  test("App drills one appearance object through PaneGrid", () => {
    expect(appSource).toMatch(/const terminalAppearance = ref</);
    expect(appSource).toContain(':appearance="terminalAppearance"');
    expect(paneSource).toContain("appearance?: TerminalAppearance");
    expect(paneSource).toContain(':appearance="appearance"');
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `npx vitest run src/components/TerminalView.test.ts`
Expected: FAIL —— 第一条断在找不到 `appearance?: TerminalAppearance`。

- [ ] **Step 3: 定义类型**

在 `src/lib/types.ts` 加：

```ts
// Terminal appearance settings, resolved on the desktop side and drilled
// down as one object so PaneGrid doesn't grow a prop per setting and
// per-profile overrides (item 22) only have to swap the source.
export interface TerminalAppearance {
  fontHead: string;
  fontSize: number;
  lineHeight: number;
  cursorStyle: "block" | "underline" | "bar";
  cursorBlink: boolean;
  scrollback: number;
}
```

- [ ] **Step 4: App.vue 读取与持有**

加一个 `const terminalAppearance = ref<TerminalAppearance>({...})`，初值用与 Go 侧
一致的默认值（`fontHead: ""`、`fontSize: 13`、`lineHeight: 1.0`、`cursorStyle: "block"`、
`cursorBlink: true`、`scrollback: 5000`）。

在 `refreshTerminalTheme()` 旁边加 `refreshTerminalAppearance()`，并在启动链的
`caps.wailsBindings` 分支里、`bootStage = "refreshTerminalTheme"` 那一步之后调用它
（新增自己的 `bootStage` 字符串，红线 #19 要求每步单独标记）。

把 `SettingsGeneral` 的 `appearance-changed` 接到一个 handler，直接整体替换 ref 的值。

`<PaneGrid>` 上加 `:appearance="terminalAppearance"`。

- [ ] **Step 5: PaneGrid 透传**

`defineProps` 加 `appearance?: TerminalAppearance`，`<TerminalView>` 上加
`:appearance="appearance"`。

- [ ] **Step 6: TerminalView 应用**

prop 加 `appearance?: TerminalAppearance`，并给 `withDefaults` 一份与上面一致的默认值。

`ensureTerm()` 里把 `new Terminal({...})` 的四个字面量换成 prop 读取：
`fontFamily: composeFontFamily(props.appearance.fontHead)`、
`fontSize: props.appearance.fontSize`、`lineHeight: props.appearance.lineHeight`、
`cursorBlink: props.appearance.cursorBlink`、`cursorStyle: props.appearance.cursorStyle`、
`scrollback: props.appearance.scrollback`。

新增 `applyAppearance(next, prev)`：

```ts
// Appearance changes split in two: font family / size / line height change the
// character cell box and therefore need a re-fit (which sends a RESIZE via the
// existing sendResize path); cursor style and scrollback do not. Fitting is
// driver-only — a viewer's dimensions are locked to the PTY by onMeta
// (redline #17), so changing font size on a viewer pane is local-render-only.
function applyAppearance(next: TerminalAppearance, prev?: TerminalAppearance) {
  if (!term) return;
  term.options.fontFamily = composeFontFamily(next.fontHead);
  term.options.fontSize = next.fontSize;
  term.options.lineHeight = next.lineHeight;
  term.options.cursorStyle = next.cursorStyle;
  term.options.cursorBlink = next.cursorBlink;
  term.options.scrollback = next.scrollback;

  const metricsChanged =
    !prev ||
    prev.fontHead !== next.fontHead ||
    prev.fontSize !== next.fontSize ||
    prev.lineHeight !== next.lineHeight;
  if (metricsChanged && isDriver.value) fit?.fit();
}

watch(
  () => props.appearance,
  (next, prev) => applyAppearance(next, prev),
  { deep: true },
);
```

- [ ] **Step 7: 跑测试**

Run: `npx vitest run src/components/TerminalView.test.ts && npm test && npm run build`
Expected: 全过。

- [ ] **Step 8: 手动验证**

Run（仓库根）：`cd desktop && wails dev -tags webkit2_41`（macOS/Windows 省略 `-tags`）

1. 改字号 → 所有 pane 立即重排，`vim` / `htop` 不错位
2. 改字体族 → 生效；输入中文，字宽正常无方框（红线 #13）
3. 改行高 → 生效且重排
4. 改光标样式与闪烁 → 生效，**且终端不重排**
5. 改 scrollback → 生效，提示里的 MB 数跟着变；不重排
6. 分屏后在 **viewer pane**（远端会话）改字号 → 本地渲染变化，对端尺寸不变
7. 重启 app → 六项设置全部保持
8. 全部调回默认 → 与本次改动前的观感一致

- [ ] **Step 9: 重建 web 嵌入产物**

Run（仓库根，需 Node 20）：

```bash
nvm use 20
./scripts/build-web.sh
git status --short internal/relay/web-dist | head
```

- [ ] **Step 10: 勾掉 roadmap**

`docs/roadmap.md` 第 20 项「终端外观设置」下的六个复选框改为 `- [x]`。注意其中
「增加连接层测试，确认高 scrollback 下 #343 的洪水保护仍生效」一条**不可执行**——
不存在那样一层保护（见 design §3 发现 2）。把该行改写为「scrollback 上限 20000 +
每 pane 内存提示」，再勾选。同时修正 spec
`docs/superpowers/specs/2026-08-16-sync-layer-roadmap-design.md` §7 与 §6 风险 6 的
对应措辞。

- [ ] **Step 11: 提交**

```bash
cd "$(git rev-parse --show-toplevel)"
git add desktop/frontend/src/App.vue desktop/frontend/src/components/PaneGrid.vue \
        desktop/frontend/src/components/TerminalView.vue \
        desktop/frontend/src/components/TerminalView.test.ts \
        desktop/frontend/src/lib/types.ts \
        internal/relay/web-dist docs/roadmap.md \
        docs/superpowers/specs/2026-08-16-sync-layer-roadmap-design.md
git commit -m "feat(terminal): apply appearance settings to live panes"
```
