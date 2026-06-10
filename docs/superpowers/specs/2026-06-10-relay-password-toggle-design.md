# Password Show/Hide Toggle — Desktop Settings → Relay

> **Audience**: 实施者
> **Last updated**: 2026-06-10
> **Status**: design / approved
> **See also**: `desktop/frontend/src/components/SettingsRelay.vue`

## 背景

桌面 Settings → Relay 的"连接远程 relay"表单里，密码输入框是原生 `<input type="password">`。用户希望加一个常见的"眼睛"图标，点一下能切换显示/隐藏密码内容，方便核对邮箱-密码是否输对。

## 范围

仅本次截图所在的 `SettingsRelay.vue` 远程登录密码字段。不动：

- 移动端 `MobileSetup.vue` 等其他登录入口
- Web `/login.html` 的密码字段（那是 Naive UI 的 `NInput`，已自带 `show-password-on` 支持，若有相同诉求另开 PR）
- 桌面端其他可能存在密码输入的地方

## 设计

### UI

- 把 `<input id="relay-login-password" type="password" ...>` 包进 `<div class="password-field">`（`position: relative`）
- 输入框内右侧（垂直居中、绝对定位）放一个 `<button type="button" class="password-toggle">`
  - 按钮内嵌内联 SVG：`showPassword=false` 时画"眼睛"图标，`true` 时画"眼睛带斜杠"图标（不引入图标库，跟 SettingsRelay.vue 当前风格一致）
  - `:aria-label` 跟随状态在 `t('settings.relay.passwordShow')` / `t('settings.relay.passwordHide')` 之间切换
  - `:aria-pressed="showPassword"`
- 输入框 `:type="showPassword ? 'text' : 'password'"`
- 输入框的 `padding-right` 增加 ~36px 给按钮腾位置
- 颜色 / hover 与文件里已有的 `input[type=...]` 配色一致；不引入新主题变量

### 状态

```ts
const showPassword = ref(false)
```

无需持久化；切换页面 / 关闭对话框时重置即可（与 `password` ref 同生命周期）。

### i18n

`desktop/frontend/src/i18n/messages/en.ts` 和 `zh-CN.ts` 的 `settings.relay` namespace 各加 2 个 key：

```
en:
  passwordShow: "Show password"
  passwordHide: "Hide password"

zh-CN:
  passwordShow: "显示密码"
  passwordHide: "隐藏密码"
```

### 不做

- 不引入 `lucide-vue-next` 等图标库（依赖增加不值当）
- 不替换为 Naive UI 的 `NInput`（会被牵动一片 styling 重写）
- 不加"密码强度指示"（YAGNI）
- 不动移动 / web 的密码字段（不同入口、不同组件库、本次范围外）

## 测试

- Vitest：现有 `SettingsRelay.test.ts` 加一个测试用例：mount 组件，找到 toggle button，断言初始 `type="password"`；点击后 `type="text"`；再次点击恢复 `password`。
- 手工：wails dev 跑桌面，打开 Settings → Relay，输入字符，点眼睛能看到明文，再点恢复掩码。

## 行为变更摘要

| 项 | 状态 |
|---|---|
| 桌面 Settings → Relay 密码字段 | 加入眼睛 toggle |
| 其他密码字段 | 不变 |
| i18n 文件 | 加 2 个 key（en + zh-CN） |
| 依赖 / 包 | 无新增 |
| 行为兼容 | 完全兼容；toggle 默认 `password` 模式，与现有行为一致 |
