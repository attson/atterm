# 粘贴图片本地预览 toast — design

Date: 2026-06-26
Status: Drafted — awaiting user review before plan.

## 0. Summary

用户粘贴图片到终端后，**在右上角弹出缩略图 toast**，让用户立刻确认"贴对了没"。原有 `sendPasteImage` 上行链路不变，预览是并行的纯本地 UI 反馈：5s 自动消失、hover 暂停、X 手动关、点缩略图开 lightbox 看原图、多张连贴堆叠显示。无设置开关，默认开启。

## 1. Goals

- 4 个 `sendPasteImage` 调用点都触发预览：
  - 桌面 Ctrl+V（`desktop/frontend/src/components/TerminalView.vue::handleImagePaste`）
  - 桌面右键菜单（`desktop/frontend/src/lib/terminalPaste.ts::pasteFromClipboard` 内 image 分支）
  - 移动端相册选图（`desktop/frontend/src/mobile/MobileTerminal.vue` 选图回调）
  - Web 端文件选择（`web/src/main/App.vue::onPasteImage`）
- Toast 固定在视口右上 `top: 88px; right: 0.75rem`，避开输入区
- 5s 自动消失；hover 暂停计时；X 立即关闭
- 点缩略图打开内置 lightbox（全屏遮罩 + 原尺寸图）；点空白处或 Esc 关闭
- 多张连贴：toast 上下堆叠，各自独立计时（不去重不合并）
- 预览仅本地反馈——远程粘贴者只能看到自己端的 toast，本地桌面 / 其它 viewer **不**接收预览广播（已澄清不做这件事）

## 2. Non-goals

- 不加设置开关（YAGNI）
- 不做跨端广播预览（不发新的 wire 消息把缩略图同步给其它 viewer）
- 不改 `sendPasteImage` 上行链路、不改 `desktop/paste_image.go`、不改 wire 协议
- 不改 `[Image #N]` 在终端里的渲染（那是 Claude Code / Codex 自己的占位符）
- 不做飞书卡片里渲染真图（属另一独立需求）
- 桌面 + Web 两份前端实现独立（YAGNI，不抽公共包）

## 3. 现状（改动基线）

- **桌面前端**：`desktop/frontend/src/`，未用 naive-ui，组件自带 CSS
  - `components/TerminalView.vue::handleImagePaste`（line ~177-189）：监听 xterm 容器 `paste` 事件，找到 `image/*` item → `conn.sendPasteImage(file, name)`
  - `lib/terminalPaste.ts::pasteFromClipboard`（line ~37-64）：右键菜单触发，image 分支调 `opts.conn.sendPasteImage(blob, name)`
  - `mobile/MobileTerminal.vue`（line ~220）：相册选图后 `conn.sendPasteImage(file, name)`
- **Web 前端**：`web/src/main/`，用 naive-ui（已挂 `n-message-provider`）
  - `App.vue::onPasteImage(file)`（line 98-100）：`PasteFallback` 文件选择回调 → `termRef.value?.sendPasteImage(file, file.name)`
  - `TerminalView.vue::sendPasteImage`（line 179-181）：转发给 `conn.sendPasteImage`
  - 注：web TerminalView **没有**原生 Ctrl+V 图片处理，所有图片走 PasteFallback 文件选择
- **共享情况**：两套前端没有共享代码（`web/src/shared/*` 仅 web 内部 alias，桌面前端访问不到），UI 组件需各做一份

## 4. 设计

### 4.1 事件总线（每个 frontend 一份）

`desktop/frontend/src/lib/pasteImageBus.ts` 和 `web/src/main/lib/pasteImageBus.ts`（同样实现）：

```ts
export type PasteImageEvent = { id: string; file: Blob; name: string }
type Handler = (e: PasteImageEvent) => void
const handlers = new Set<Handler>()

export const pasteImageBus = {
  emit(file: Blob, name: string): void {
    const event: PasteImageEvent = {
      id: crypto.randomUUID(),
      file,
      name: name || 'clipboard-image',
    }
    handlers.forEach((h) => h(event))
  },
  on(h: Handler): () => void {
    handlers.add(h)
    return () => handlers.delete(h)
  },
}
```

- 模块级单例，进程内全局
- `emit` 同步派发；handler 内出异常不影响其它 handler / 主路径

### 4.2 Host 组件（每个 frontend 一份）

`desktop/frontend/src/components/PasteImagePreviewHost.vue` 和 `web/src/main/components/PasteImagePreviewHost.vue`：

- **State**：
  - `toasts: { id: string; file: Blob; url: string; name: string }[]`
  - `timers: Map<string, number>`（toast id → setTimeout handle）
  - `lightbox: { url: string; name: string } | null`（自有 url，独立于 toast）
- **生命周期**：
  - `onMounted`：`unsubscribe = pasteImageBus.on(handlePaste)`；挂 `document.addEventListener('keydown', onKeydown)`
  - `onBeforeUnmount`：`unsubscribe()`；移除 keydown 监听；清所有 timer；revoke 所有 toast.url；revoke lightbox.url
- **`handlePaste(e)`**：
  - `url = URL.createObjectURL(e.file)`（try/catch；失败则 `console.warn` 返回，主路径不受影响）
  - push `{ id: e.id, file: e.file, url, name: e.name }` 到 `toasts`
  - `timers.set(e.id, window.setTimeout(() => dismiss(e.id), 5000))`
- **`dismiss(id)`**：
  - 清 timer
  - 从 `toasts` 找到并移除
  - `URL.revokeObjectURL(toast.url)`
- **`onMouseEnter(id)` / `onMouseLeave(id)`**：清 timer / 重设 5s timer
- **`onThumbClick(toast)`**：
  - 若 `lightbox` 已存在 → 先 `URL.revokeObjectURL(lightbox.url)`
  - `lightbox = { url: URL.createObjectURL(toast.file), name: toast.name }`（**新建独立 url**，和 toast 完全解耦）
- **`closeLightbox()`**：`URL.revokeObjectURL(lightbox.url)`；`lightbox = null`
- **`onKeydown(e)`**：lightbox 打开时 Esc 关之
- **模板结构**：
  ```html
  <div class="paste-preview-host">
    <div v-for="t in toasts" :key="t.id" class="paste-toast" @mouseenter ...>
      <img :src="t.url" :alt="t.name" @click="onThumbClick(t)" />
      <span class="name">{{ t.name }}</span>
      <button class="close" @click="dismiss(t.id)">×</button>
    </div>
    <div v-if="lightbox" class="paste-lightbox" @click="closeLightbox">
      <img :src="lightbox.url" @click.stop />
    </div>
  </div>
  ```
- **CSS**：
  ```
  .paste-preview-host { position: fixed; top: 88px; right: 0.75rem; z-index: 50;
                       display: flex; flex-direction: column; gap: 8px;
                       pointer-events: none; }
  .paste-toast { pointer-events: auto; width: 180px; ... }
  .paste-toast img { width: 100%; max-height: 120px; object-fit: contain; cursor: zoom-in; }
  .paste-lightbox { position: fixed; inset: 0; background: rgba(0,0,0,0.85); z-index: 100;
                    display: flex; align-items: center; justify-content: center;
                    cursor: zoom-out; pointer-events: auto; }
  .paste-lightbox img { max-width: 90vw; max-height: 90vh; cursor: default; }
  ```

### 4.3 调用点改动（4 处各 +1 行）

```ts
// 桌面 components/TerminalView.vue handleImagePaste（在 sendPasteImage 之前）
pasteImageBus.emit(file, file.name || 'clipboard-image')

// 桌面 lib/terminalPaste.ts pasteFromClipboard image 分支（在 sendPasteImage 之前）
pasteImageBus.emit(blob, payload.filename || 'clipboard-image')

// 桌面 mobile/MobileTerminal.vue 选图回调（在 sendPasteImage 之前）
pasteImageBus.emit(file, file.name)

// Web App.vue onPasteImage（在 sendPasteImage 之前）
pasteImageBus.emit(file, file.name)
```

`terminalPaste.ts` 由于是纯函数 lib（被 test 覆盖），用直接 import；如果测试需要 mock，用 `vi.mock('./pasteImageBus')`。

### 4.4 挂载

- `desktop/frontend/src/App.vue` 模板顶层加 `<PasteImagePreviewHost />`
- `web/src/main/App.vue` 在 `<n-message-provider>` 内加 `<PasteImagePreviewHost />`

## 5. 错误与边界

- **`URL.createObjectURL` 失败**：捕获后 `console.warn`，跳过 toast；不阻断 `sendPasteImage`
- **会话切换 / 组件卸载**：unmount 钩子统一清 timer + revoke 所有 url
- **toast 在 lightbox 打开期间自动消失**：toast url 立即 revoke 即可；lightbox 有自己独立的 url（持有同一 Blob，Blob 不会被 GC）
- **hover 后立即按 X**：先清 timer 再走 dismiss；timer 不会再 fire
- **连贴 N 张图**：N 个 toast 堆叠，各自独立 5s 计时；不做上限——后端单图本身有 10MB 上限（`desktop/paste_image.go` `maxPasteImageBytes`），5s 后自然清掉
- **Esc 关 lightbox**：组件 mount 起一直挂 keydown 监听，handler 内只在 `lightbox != null` 时响应 Esc（避免反复挂/移）
- **远程粘贴 → 本地桌面**：本地桌面不弹 toast（设计上不广播；澄清结果）

## 6. 测试

### 桌面前端

- `desktop/frontend/src/lib/__tests__/pasteImageBus.test.ts`：
  - `emit` 后所有订阅者都收到事件
  - `on` 返回的 unsubscribe 调用后不再收到事件
  - handler 抛异常不影响其它 handler

- `desktop/frontend/src/components/__tests__/PasteImagePreviewHost.test.ts`：
  - 收到 bus 事件 → 渲染对应 toast（断言 `<img src>`、`name`）
  - 5s 后 toast 自动消失（`vi.useFakeTimers()` + `vi.advanceTimersByTime(5000)`）
  - hover 暂停计时，离开后续计 5s
  - 点 X 立即移除 toast 并 revoke url（mock `URL.revokeObjectURL` 断言调用）
  - 点缩略图 → lightbox 出现且 src 一致
  - lightbox 打开时点空白处或 Esc → 关闭
  - lightbox 打开期间 toast 5s 自动消失 → toast 消失但 lightbox 图仍可见（独立 url）
  - 卸载组件 → 所有 timer 清掉 + 所有 url revoke

- 更新 `desktop/frontend/src/lib/terminalPaste.test.ts`：
  - image 分支 assert `pasteImageBus.emit` 被调用一次（mock bus）
  - text 分支 assert 不调用

- 更新 `desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts::image button sends...`：assert bus 也被 emit

- 更新（如有）桌面 `TerminalView` paste 测试：assert `handleImagePaste` 触发 bus

### Web 前端

- `web/src/main/lib/__tests__/pasteImageBus.test.ts`：同上
- `web/src/main/components/__tests__/PasteImagePreviewHost.test.ts`：同上
- 更新 `web/tests/unit/main/App.test.ts`：assert `onPasteImage` 触发 bus

### 不做

- 不写 e2e（纯 UI 反馈，无后端依赖）
- 不写 Go 端测试（后端无改动）

## 7. 实现顺序

1. `pasteImageBus.ts` + 单测（桌面、Web 各一份）
2. `PasteImagePreviewHost.vue` + 组件测试（桌面、Web 各一份）
3. 4 个调用点接 bus + 更新相关测试
4. 两个 App.vue 挂 host
5. 手动验证全 4 路径（桌面 Ctrl+V、桌面右键、移动端相册、Web 文件选择）
6. 全量回归 `npm test`（两个 frontend）
