# 终端外观设置（P5 第 20 项）— design

Date: 2026-08-17
Status: Drafted — awaiting user review before plan.
Parent: [2026-08-16 sync-layer roadmap](./2026-08-16-sync-layer-roadmap-design.md) §5 P5 第 20 项 · roadmap 第 20 项

## 0. Summary

把终端的字体族、字号、行高、光标样式与闪烁、scrollback 行数从硬编码常量提成用户设置，
改完即时生效、不需要重启。

五项里四项今天是 `TerminalView.vue` 里 `new Terminal({...})` 的字面量
（`fontSize: 13`、`cursorBlink: true`、`scrollback: 5000`，行高未设即 xterm 默认 1.0），
字体族是 `lib/terminalFont.ts` 的常量 `TERMINAL_FONT_FAMILY`。

本设计**不做跨端同步**。同步是 P5 第 21 项的范围，那一项会把整个 L1 层
（主题、本项新增的五项、`default_shell`、快捷键绑定）一次性接进 `prefssync`。
分开做是为了不把同步管线铺两遍，也匹配 spec 对两项的切分。

## 1. Goals

- 五项设置可在 Settings 里改，改完当前所有 pane 立即生效。
- 默认值精确等于今天的行为，升级上来的用户看不出变化。
- 字体族可换，但不能破坏红线 #13 的 CJK-first 字体栈语义。
- scrollback 可调，但用户要能看见调高的代价。

## 2. Non-Goals

- 不接 `prefssync`（第 21 项）。
- 不做 per-session / per-profile 覆盖——profile 是第 22 项，本项只做全局。
- 不做主题编辑器或主题导入导出（后者是第 31 项）。
- 不动 web / 移动端。见下节。

## 3. 现状与两个约束性发现

**发现 1：外观偏好在 web 上本来就不生效，这是既有先例而非疏漏。**
`App.vue` 的启动链把 `refreshTerminalTheme()` 整个包在 `if (caps.wailsBindings)` 里；
web 分支（同一 `onMounted` 的 `else`）压根不调它，`currentTerminalThemeID` 保持
`DEFAULT_TERMINAL_THEME_ID`。也就是说**今天 web 端的终端主题就是默认主题，无法更改**。

本项沿用同一形状：五项设置走 Wails 绑定，桌面端可改；web 与 Capacitor 拿默认值。
理由是这条链路（`appConfig` 字段 → `App.Get*/Set*` → `lib/api.ts` 包装 → `App.vue`
响应式 ref → prop 下钻）已经为 `terminal_theme` 跑通并被测试覆盖，复制它的成本最低、
回归面最小。让 web 也能改需要另一套 relay 侧持久化，那属于第 21 项接同步之后的自然结果，
不该在本项里另起炉灶。

**发现 2：#343 没有独立的「输出洪水保护」——抗洪手段就是低 scrollback 本身。**
spec §7 写着「scrollback 行数改动需补一条连接层测试，确认高值下 #343 的洪水保护仍生效」，
这句基于一个错误假设。核过 #343：`connection.ts` 的改动是让**被丢弃的 session list 可诊断**，
与终端输出洪水无关；真正解决洪水下内存爆掉的，就是把 `scrollback` 从 20000 下调到 5000
这一个动作（`TerminalView.vue` 的注释记了实测数据：200 列下约 2.75 KB/行，
20000 行 ≈ 55 MB/终端，十几个 pane 累计到过 ~600 MB）。

所以把 scrollback 放开，**就是把 #343 修掉的内存问题交还给用户自己权衡**，不存在
「还有一层防护兜着」。本设计据此处理，而不是假装防护网仍在——见 §4.5。

## 4. 设计

五项统一走 §3 发现 1 的既有链路。逐项的设计要点：

### 4.1 字体族 —— 只让用户换「头部」，尾部锁死

`TERMINAL_FONT_FAMILY` 是一条精心排序的链：ASCII 等宽族在前（Menlo / Monaco /
Consolas / Liberation Mono / DejaVu Sans Mono），CJK 族在后（PingFang SC /
Microsoft YaHei / Noto Sans Mono CJK SC），最后 `monospace`。顺序是红线 #13 的
核心——iOS 26 的 WebKit 会让 `ui-monospace` / `-apple-system` 声明全 Unicode 覆盖
却不渲染 CJK，同时阻断后续回退，导致方框与错宽重叠。

因此**不提供自由文本输入**。设置项是一个预设下拉，用户选的是**链首的 ASCII 等宽族**，
应用时把它拼到既有链的前面：

```
<用户选择>, <TERMINAL_FONT_FAMILY 原链>
```

预设列表给跨平台常见等宽族（如 Menlo、SF Mono、JetBrains Mono、Fira Code、
Cascadia Code、Source Code Pro、IBM Plex Mono），外加一个「系统默认」＝不加前缀、
用原链。用户机器上没装选中的族时，per-glyph 回退自然落到原链，不会坏。

> 2026-08-17 补记：实现（`terminalFont.ts` 的 `TERMINAL_FONT_PRESETS`）没有把
> Menlo 单列为一个预设项，保留原样而非补齐——Menlo 本来就是 `TERMINAL_FONT_FAMILY`
> 原链的第一个字体，所以「系统默认」在 macOS 上已经等价于 Menlo 优先；把它再列成
> 一个可选头部只会得到同一结果的两条路径。

这样红线 #13 的 CJK 尾部**在构造上**无法被绕过，而不是靠 code review 守着。
`lib/terminalFont.ts` 因此从导出一个常量，改为导出原链 + 预设列表 + 一个
`composeFontFamily(head)` 纯函数，纯函数单测。

### 4.2 字号

`fontSize`，整数，范围 8–32，默认 **13**（今天的字面量）。

### 4.3 行高

`lineHeight`，一位小数，范围 1.0–2.0，默认 **1.0**（xterm 不设时的默认）。

### 4.4 光标

`cursorStyle`：`block` / `underline` / `bar`，默认 **block**（xterm 默认）。
`cursorBlink`：布尔，默认 **true**（今天的字面量）。

### 4.5 scrollback 行数

`scrollback`，整数，默认 **5000**（#343 定的值，不动）。

上限 **20000**——即 #343 之前的值。选这个数不是拍脑袋：它是已知「单 pane 能活」
但「十几个 pane 会痛」的那条线，把它作为上限意味着任何单一设置值都不会比
#343 之前更糟。

设置项旁必须显示按当前值估算的每 pane 内存（沿用注释里的 2.75 KB/行），
并说明这是**每个 pane** 的开销。这是 §3 发现 2 的直接后果：既然没有防护网，
用户至少要看得见代价。

运行时调低会截断已有 scrollback，调高不影响已有内容——xterm 的行为，如实说明即可。

## 5. 生效路径

`App.vue` 持有五个响应式 ref（与 `currentTerminalThemeID` 同级），启动时在
`caps.wailsBindings` 分支里一次性读取，作为一个 `appearance` 对象 prop 下钻
`PaneGrid` → `TerminalView`。**用单个对象而不是五个 prop**，避免 `PaneGrid`
再长五行透传，也让后续第 22 项接 per-profile 覆盖时只换一个来源。

`TerminalView` 侧 `watch` 该对象，写进 `term.options.*`。三条约束：

1. **字体族、字号、行高变化会改变字符盒尺寸，必须重新 fit**；光标与 scrollback
   不会，不要触发 fit。
2. **重新 fit 只允许在 driver 模式下发生**（红线 #17）。viewer 的尺寸由 PTY 锁定，
   `onMeta` 的 `cols/rows` 说了算；在 viewer pane 上改字号只改本地渲染，不得
   跑 FitAddon、不得发 RESIZE。
3. fit 之后走既有的 `sendResize` 路径，它已经在 WS CONNECTING 时排队、并比对
   `expectedCols/Rows` 跳过无意义 RESIZE（红线 #6）。不要绕开它另发 RESIZE。

## 6. 风险

1. **字体族预设里某个族在某平台不存在**——per-glyph 回退到原链，无害。不做可用字体探测（`document.fonts.check` 在 WebKit 上对等宽族有假阳性）。
2. **改字号触发的 fit 会给子进程发 SIGWINCH**——与拖分隔条同类，既有路径已处理。但连续拖动字号滑块会连发；设置项用 change 而非 input 事件提交，或做防抖。
3. **scrollback 调高换回 #343 的内存问题**——已在 §4.5 用默认值 + 上限 + 显式代价提示处理。这是知情的权衡，不是回归。
4. **`TerminalView.vue` 已经约 2700 行**，本项还要再加 watch 与 fit 逻辑。若新增超过约 60 行，抽一个 `composables/useTerminalAppearance.ts`，与既有的 `useQuickTemplates` / `useTerminalLinkProvider` 抽法一致。

## 7. 验证

- `composeFontFamily(head)` 纯函数单测：拼接顺序正确；「系统默认」返回原链；
  CJK 尾部在所有分支中都存在（这条直接锁红线 #13）。
- Go 侧 `appConfig` 五个字段的 get/set + 范围校验单测（越界值被拒或钳制）。
- `TerminalView` 的 watch 与 fit 门控走本文件既有的源码正则断言约定
  （该组件无法 mount）：字体/字号/行高变化才 fit，viewer 模式不 fit。
- Settings UI 组件走 Vitest mount。
- 手动：改字号后终端立即重排且 `vim` 不错位；viewer pane 改字号不影响对端；
  scrollback 调到上限后跑一次大量输出，观察内存与可用性。

## 8. 与 spec 的差异

spec §7 的「确认高 scrollback 下 #343 的洪水保护仍生效」一条**不可执行**，
因为不存在那样一层保护（§3 发现 2）。本设计以「默认值不变 + 有界上限 + 显式代价提示」
替代该条，并建议在合并时同步修正 spec §7 与 §6 风险 6 的措辞。
