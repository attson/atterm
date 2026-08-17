# 已有配置接入同步（P5 第 21 项）— design

Date: 2026-08-17
Status: Drafted — awaiting user review before plan.
Parent: [2026-08-16 sync-layer roadmap](./2026-08-16-sync-layer-roadmap-design.md) §5 P5 第 21 项 · roadmap 第 21 项
See also: [2026-08-17 terminal appearance](./2026-08-17-terminal-appearance-design.md)（本项要接的六个键来自那一项）

## 0. Summary

把 L1 偏好层接进已有的 `internal/prefssync` 引擎：终端主题、第 20 项新增的六个外观设置、
以及快捷键绑定。接完之后换一台桌面机登录同一账号，这些配置自动一致。

这是母 spec 的主线动作——引擎、LWW 语义、播种路径、失效通知全都已经存在并被验证过，
本项只是把内容物喂进去。因此本设计的篇幅不在「怎么同步」，而在**哪些键不该同步**，
以及**接入时哪三件事会静默出错**。

## 1. Goals

- `terminal_theme` 与第 20 项的六个外观键跨桌面设备一致。
- 快捷键绑定从 `Plugins.shortcuts.bindings` 拆成独立键并跨设备一致。
- 首次接入不覆盖任何一台设备上已有的本地配置。
- 远端 pull 下来的新值立即反映到界面上，不需要重开设置面板或重启。

## 2. Non-Goals

- 不做 sealed（L2/L3）内容物。profile 是第 22 项，SSH 那套已经在跑。
- 不做同步状态可见化（指示器、手动 push/pull）——那是第 30 项。
- 不动 web / 移动端的读取路径。这些偏好目前是桌面专属（见第 20 项设计 §3 发现 1），
  接同步不改变这一点：relay 上有值，但 web 端仍不读。让 web 读是独立的一步。

## 3. 现状：要接的东西已经全部就位

核过代码确认，本项不需要新建任何机制：

- `internal/prefssync/sync.go`：per-key LWW 引擎，`syncedKeys` 白名单 + `Adapter` 接口。
- `desktop/prefssync_adapter.go`：`ReadValue`/`WriteValue` 的 key switch，新键在这里加 case。
- `desktop/app.go` 的 `updatePref(key, mutate)`：**第 20 项的六个 setter 已经在调它，只是传了空 key**
  （空 key 跳过 `markPrefDirtyAndPush`）。本项把 key 填上即可，setter 本体不用动。
  这是第 20 项刻意留的接口。
- `isPrefCustomized(cfg)`（`app.go:1635`）+ `Engine.SeedFromLocal`：首登播种路径。
- 失效通知：`markPrefDirtyAndPush` 在 Push 成功后发 `prefs:changed`；
  `prefs_watch.go` 的 relay watch 在 **Pull 成功后也发**同一事件；`main.ts` 把它桥到平台总线；
  `useSessionPins` 已是一个在用的监听方范例。

## 4. 哪些键不接——两个反例

母 spec §5 第 21 项列的是「`terminal_theme` + `default_shell` + 快捷键绑定」。核实现后，
其中一项要改口径，另有一项曾被 spec §4 归进 L1 但同样不该同步。

### 4.1 `default_shell`：同步，但**应用前必须验在**

shell 是**机器局部**的：`/opt/homebrew/bin/fish` 在 Mac 上成立，同步到 Windows 或另一台
没装 fish 的机器上就是一条打不开的路径，后果是新会话直接起不来——比「不同步」严重得多。

但也不能一刀切不同步：两台同构机器（很常见——两台 Mac）之间同步 shell 正是用户列出来的
痛点之一。

所以：**同步这个键，但在应用侧加存在性校验**。pull 到一个本机不存在的 shell 路径时，
保留本机当前值，并记一条 warn。写回 relay 的仍是本机的真实值，不做「修正后回写」——
那会让两台机器互相覆盖。

判据：`os.Stat` + 可执行位。校验放在读取侧（`DefaultShellOrDefault` 一类的访问器），
不是写入侧——写入侧拒绝会让同步过来的值永远卡在 config 里却不生效，反而更难查。

### 4.2 `webgl_renderer_enabled`：不接

母 spec §4 的 L1 表把「渲染器」列进了同步内容物。不该接：这个开关的正确值**取决于本机
GPU 驱动**。`TerminalView.vue` 的注释记着它在 Linux 上默认关闭的原因——NVIDIA 专有驱动
+ X11 + WebKitGTK 会把光标/末格重绘推迟一两帧，表现为可感知的输入延迟（#48）。

把一台 AMD 机器上的 `true` 同步到那台 NVIDIA 机器上，等于把一个已知的卡顿 bug 传染过去。
这类「值的正确性由硬件决定」的偏好不属于 L1，它压根不是偏好，是本机适配。
建议同时修正母 spec §4 的 L1 表述。

## 5. 键清单

| key | 来源字段 | 备注 |
|---|---|---|
| `terminal_theme` | `TerminalTheme` | 已有字段，未接同步 |
| `terminal_font_head` | `TerminalFontHead` | 第 20 项 |
| `terminal_font_size` | `TerminalFontSize` | 第 20 项 |
| `terminal_line_height` | `TerminalLineHeight` | 第 20 项 |
| `terminal_cursor_style` | `TerminalCursorStyle` | 第 20 项 |
| `terminal_cursor_blink` | `TerminalCursorBlink`（`*bool`）| 第 20 项，走 `marshalPtr` |
| `terminal_scrollback` | `TerminalScrollback` | 第 20 项 |
| `default_shell` | `DefaultShell` | 需 §4.1 的存在性校验 |
| `shortcut_bindings` | **新字段**，见 §6 | 需迁移 |

**一键一设置，不打包成一个 `appearance` blob。** prefssync 是 per-key LWW：打包意味着
在 A 机改字号、B 机改光标样式时，后写的整体覆盖先写的，用户会看到自己没动过的设置被改回去。
LWW 的粒度应该对齐用户心里「一次独立选择」的粒度。代价是 adapter 多几个 case，很便宜。

## 6. 快捷键绑定拆键与迁移

绑定当前存在 `Plugins.shortcuts.bindings`（`PluginConfig` 内），整个 `plugins` 字段没接同步，
也不该接——插件启用状态里混着本机相关的东西。

拆一个独立的顶层字段 `ShortcutBindings map[string]string`，key 为 action id。迁移规则：

1. 读 config 时，若 `ShortcutBindings` 为空**且** `Plugins.shortcuts.bindings` 非空 → 搬过去。
2. 搬完清空旧位置，避免两处并存后各自被写。
3. 迁移必须**幂等**：已迁移过的 config（新字段有值、旧位置已空）再跑一次不能把值清掉。
4. 旧位置有值、新字段**也**有值时（理论上只会出现在手工编辑过的 config 里）→
   以新字段为准，清空旧位置，记一条 warn。

前端 `SettingsShortcuts.vue` 当前经 `usePluginConfigStore` 读写绑定，要改成读写新字段。

## 7. 三个会静默出错的地方

这是本设计的重点。三条都不会报错，只会让用户某天发现配置被改掉了。

### 7.1 首登播种：不能让 pull 覆盖本地

`Engine.SeedFromLocal(isCustomized, now)` 的语义是：对每个「本地已自定义且未 dirty」的键，
打上 dirty 标记，让接下来的 Push 把本地值送上去。**`isPrefCustomized`（`app.go:1635`）
是一个 key 的白名单 switch，没列的键一律返回 false**——也就是说，本项新增的九个键
如果不加进那个 switch，首登时不会被播种，会被 relay 上的空/旧值 pull 覆盖。

每个新键都要加对应的「是否已自定义」判据，且判据必须是**「用户是否显式设过」而不是
「是否等于默认值」**：一个显式选择了 13 号字的用户和一个从没动过的用户，config 里的值
相同但语义不同。第 20 项的字段设计正好支持这点——数值字段零值表示未设，
`TerminalCursorBlink` 是 `*bool`。所以判据统一为「非零值 / 非 nil」。

### 7.2 pull 之后界面不刷新

Pull 改的是 Go 侧 config，前端的 ref 不会自己变。`prefs:changed` 事件已经在 Pull 后发出
并桥到平台总线，所以基础设施是现成的，缺的是监听方：

- `App.vue`：重新读取主题与六项外观（它已有 `refreshTerminalTheme` / `refreshTerminalAppearance`，
  直接复用）。
- `SettingsTerminalAppearance.vue` 与 `SettingsShortcuts.vue`：面板打开时重读。

**监听方重载后绝对不能再持久化。** 否则 A pull → 本地写回 → push → B pull → 写回 → push，
两台机器无限 ping-pong。重载路径必须只写 ref，不调 setter、不调 `markPrefDirty`。

（本地同名事件的**递归**已经被 `platform/wails.ts` 的 `dispatching` Set 全局挡掉了——
历史上一次 `prefs:changed` 自递归到约 1286 层、抛栈溢出、冻住 UI，#334 修的就是这个。
那条防线保护的是「处理事件时又发同名事件」，保护不了「处理事件时写回并 push」。）

### 7.3 `updatePref` 的 key 一旦填上，写入就会触发网络

第 20 项的六个 setter 现在传空 key。填上 key 之后，每次用户拖动字号都会 `MarkDirty` +
后台 Push。第 20 项已经把提交时机设成 `change` 而非 `input`，所以不会每敲一下打一次网络；
本项**不要**放松那个约束。

## 8. 风险

1. **迁移在多设备上重复跑**：A 机迁移完 push `shortcut_bindings`，B 机还没升级，仍在读旧位置。
   B 机升级后本地迁移 + pull，两边值可能不同 → LWW 按时间戳裁决，可能丢掉 B 机的自定义。
   缓解：迁移写入时**不打 dirty**，让首登播种逻辑（§7.1）按「是否已自定义」正常决定谁该上传。
2. **`default_shell` 校验误判**：某些 shell 通过 `$PATH` 而非绝对路径配置。缓解：只对绝对路径做
   `os.Stat`，非绝对路径不校验直接放行。
3. **同步键数量从 8 涨到 17**：`GET/PUT /api/me/preferences` 的载荷变大，仍在可忽略量级
   （全部是标量与一个小 map）。不需要分页或增量。

## 9. 验证

- 每个新键一条 prefssync 往返测试：本地改 → dirty → push；远端改 → pull → 本地生效；
  两端同时改 → 时间戳裁决。
- `isPrefCustomized` 对九个新键各一条：未设 → false，显式设过 → true。
- 快捷键迁移：旧位置有值 → 搬迁且旧位置清空；重复跑幂等；新旧都有值 → 新的赢且记 warn。
- `default_shell` 校验：绝对路径不存在 → 保留本机值；绝对路径存在 → 采用；非绝对路径 → 直接采用。
- **ping-pong 回归测试**：模拟一次 pull 触发的 `prefs:changed`，断言重载路径没有调用任何 setter
  或 `MarkDirty`。这条直接锁 §7.2。
- 手动：两台桌面机登同一账号，A 改字号/主题/快捷键，B 在数秒内自动跟上，且 B 上没有被
  改动过的设置保持不变。

## 10. 与母 spec 的差异

- 母 spec §4 的 L1 表把「渲染器」列为同步内容物。本设计**排除** `webgl_renderer_enabled`，
  理由见 §4.2：它的正确值由本机 GPU 决定，同步它等于传染 #48 的卡顿。建议修正 §4 表述。
- 母 spec §5 第 21 项只提了三项（`terminal_theme` / `default_shell` / 快捷键绑定），
  未包含第 20 项新增的六个外观键——那一项当时尚未实现。本设计把九个键一次接完。
