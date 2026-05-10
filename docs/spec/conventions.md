# 代码约定

## 包结构

### Go

```
cmd/<binary>/main.go        二进制入口，仅做配置 + wire-up，业务逻辑放 internal/
internal/<package>/         可复用业务包，不依赖 cmd/ 或 desktop/
desktop/                    Wails 桌面 app（package main，但比 cmd/ 大）
```

依赖方向规则：

```
cmd/* → internal/*           ✓
desktop/ → internal/*        ✓
internal/* → 其他 internal/* ✓ (按层次：proto < ringbuf < session < relay; ptyhost 独立; agent → ptyhost+proto+relay)
internal/* → cmd/* 或 desktop/   ✗ 禁止反向依赖
```

如果 desktop 端需要 internal 包暴露新方法，**改 internal 包加 method**（如 `relay.Server.AdoptSession`、`Registry()`），不要在 desktop/ 写 wrapper 后再桥到 internal/。

### TypeScript

```
desktop/frontend/src/
├── main.ts             Vue 入口
├── App.vue             根组件
├── components/         全部 *.vue，每个组件单文件
├── composables/        Vue 3 setup helpers (e.g. useTerminalShortcuts.ts)
├── lib/                pure TS 模块（无 Vue 依赖）
│   ├── proto.ts        协议层
│   ├── connection.ts   WS 连接 + 重连
│   ├── types.ts        共享类型定义（LayoutKind / Pane / Tab 等）
│   ├── layout.ts       纯函数（pane 布局 transition / close / focus 导航）
│   └── api.ts          Wails bindings 包装
└── store/              （未来）pinia / composables
```

`lib/` 不依赖 Vue（便于单元测试、未来共享给 web/）。组件不直接 fetch / WebSocket，全走 `lib/`。`lib/layout.ts` 这种纯函数模块用 vitest 跑单测，TDD 增量覆盖。

## 命名

| 实体 | 风格 | 例 |
|------|------|----|
| Go 包名 | 小写单词，无下划线 | `relay`, `ptyhost`, `hostid` |
| Go 公共 type | PascalCase，描述性 | `SessionInfo`, `PtyHost`, `OpenPayload` |
| Go 公共 func | PascalCase，动词起头 | `AdoptSession`, `SubscribeLocal` |
| Go 私有 | camelCase | `mirrorState`, `streamingLocal` |
| Vue 组件 | PascalCase 文件 + kebab-case 标签 | `TabBar.vue` → `<TabBar>` |
| TS 类型 | PascalCase，interface 优先 | `Endpoint`, `RelayConfig` |
| TS 常量 | UPPER_SNAKE 仅给协议常量 | `TYPE.OUT`, `VERSION` |

## 注释

- Go：每个公共 type/func 写包注释 + Godoc 风格说明，**侧重 why**
- 不要写 commit-specific 注释（`// added for issue #123`、`// see ticket FOO-456`）
- 不要写描述代码做什么的注释（自解释）；写**约束**（"caller must hold mu"、"runs forever until ctx canceled"）
- 不要写"添加于 X 时间"类时间戳，git blame 已经有了

例：

```go
// AdoptSession registers an in-process PTY as a relay session, bypassing the
// /agent WebSocket entirely. It is the desktop app's hook for surfacing
// locally-spawned PTYs to the same xterm.js code path that handles remote
// sessions.
//
// The returned cleanup must be called exactly once. The PtyHost is NOT closed
// by cleanup — its lifecycle stays with the caller.
```

不写：

```go
// AdoptSession adopts a session.  // ← 重复函数名
// added in Phase 1.5              // ← commit info
// 2026-05-09: refactor             // ← 时间戳
```

## 错误处理

### Go

- 使用 `fmt.Errorf("...%w", err)` wrap，调用方用 `errors.Is/As`
- 不要 `panic` 在请求处理路径；只能在 `init` / `main` 启动失败用
- 用户可见错误：通过 binding 返回，前端展示
- 系统错误：`log.Printf("...")` 用标准库 log，**不引入** logrus/zap

```go
if err != nil {
    return uuid.Nil, fmt.Errorf("open pty: %w", err)
}
```

### TypeScript

- 异步路径用 `try/catch`，把上下文 wrap 进 Error
- 网络层错误带 URL：`throw new Error(\`fetch ${url}: ${e?.message ?? e}\`)`
- UI 层错误：写到 `errorMsg` ref 给用户看，不要 console.error 然后 swallow

## 测试

### Go

- 测试文件 `*_test.go` 同包
- e2e 测试放被测组件目录下（`desktop/uplink_e2e_test.go`，**不**单独 test/ 目录）
- 测试名 `TestXxx` PascalCase
- 用 `testing.Short()` 跳过慢测试
- e2e 必须自包含：起 server、起 client、断言完整流程

例：`desktop/uplink_e2e_test.go::TestUplinkE2E` / `TestTwoHostsCrossAttach` 是模板。

### TS / Vue

- 暂未引入测试框架。Phase 2 加 vitest 时优先测 `lib/proto.ts` / `lib/connection.ts`（pure），组件测放后

## 风格细则

### Go

- `gofmt` / `goimports`，提交前 `go vet -tags webkit2_41 ./...` 必须 0 warning
- 不引入新依赖除非有必要；现有依赖：`creack/pty`, `google/uuid`, `nhooyr.io/websocket`, `golang.org/x/term`, `wails/v2`
- 不写日志框架；用 `log.Printf` + 短前缀 `"uplink:"`、`"agent:"`、`"desktop relay:"`
- channel 缓冲深度写常量：`subscriberQueueDepth = 256`
- 锁顺序：`registry.mu` > `Session.mu`，避免反向 lock；持锁尽量短

### TypeScript

- strict mode 必开（`tsconfig.json` 已设）
- 不写 `any`；除非 wails generated 文件无可避免
- 类型在 `interface` 写公共 schema；`type` 用于 union / 别名
- 模板里数据结构的对齐：`<TerminalView :endpoint="…" :session-id="…" :active="…" />` kebab-case，`<script>` 用 camelCase
- 不在组件内做副作用（fetch/WS）跨 mount 边界；`onMounted/onUnmounted` 配对清理

### CSS

- 用 CSS 变量（`--bg`、`--accent` 等，定义在 `style.css :root`）
- scoped style 在每个 `.vue` 单文件里
- 不引入 CSS 框架（Tailwind / Bootstrap）；现有几百行 CSS 手写够

## Commit 风格

参考现有 git log：

```
ci: github actions to build linux/amd64 + darwin/arm64 desktop

  - 缩进列表说明改动
  - 多行 body
  - subject 用小写动词起头，≤ 72 字符
```

类型前缀：`fix:` / `feat:` / `refactor:` / `ci:` / `docs:` / `chore:`（非强制但鼓励）。

**不要**：
- 在 commit msg 写 "Co-Authored-By: Claude" / AI 生成痕迹
- subject 末尾加句号
- 写"WIP"/"checkpoint"风格 commit（应当在合并前 squash 干净）

## 不要做的事（红线）

- ❌ 在 internal/relay 引用 desktop/ 包（依赖方向倒置）
- ❌ 按 host_id 去重 session（详见 `architecture.md` §会话标识）
- ❌ 直接 `conn.Write` 在多 goroutine（nhooyr/websocket 不允许）
- ❌ 给 OUT 帧 seq 重置（重连续传依赖单调性）
- ❌ 在 PTY 字节流路径加 JSON 解析或 regex 匹配
- ❌ 用 `--no-verify` skip git hook（修复 hook 失败的根因，不是绕过）
- ❌ 给 frontend 加新依赖未经讨论（已有 vue + xterm 已够）

## 如何修改协议

1. 决定新帧类型字节（看 `internal/proto/frame.go` 已用列表，挑未占用的）
2. 加 `Type` 常量 + payload struct
3. 加发送方实现（agent / uplink / relay 的某条路径）
4. 加接收方分支（在 reader switch 里）
5. 同步 TS 端 `desktop/frontend/src/lib/proto.ts` 的 `TYPE` 枚举（如果是 client 路径用到）
6. 同步 `web/app.js` 的 `TYPE` 字典（如果浏览器需要）
7. 更新 `docs/spec/protocol.md` 帧类型表
8. 加 e2e 测试（`desktop/uplink_e2e_test.go` 或新文件）
9. 提交：subject 用 `proto:` 前缀

## 如何加 Wails binding

1. 在 `desktop/app.go` 加 method on `*App`，名字 PascalCase
2. 参数和返回类型用 `desktop/app.go` 内 named type（不要 anonymous struct——wails generator 不喜欢）
3. wails dev / wails build 会自动 generate `desktop/frontend/wailsjs/go/main/App.{ts,js}` + `models.ts`
4. 前端在 `desktop/frontend/src/lib/api.ts` 手写一份对应 wrapper（**不**依赖 generated 文件，走 `window.go.main.App.<Method>` 全局；保持 type interface 与 generated 对齐即可）
5. 错误回传用 `(T, error)` 模式，前端 try/catch

## 如何加新前端组件

1. 文件 `desktop/frontend/src/components/<PascalCase>.vue`
2. 单文件三段：`<script setup lang="ts">`、`<template>`、`<style scoped>`
3. props/emits 用 TypeScript defineProps/defineEmits 显式定义
4. 不在组件里写 fetch/WS——通过 `lib/api` 或 `lib/connection`
5. 样式用 `var(--xxx)` 主题变量，不写硬编码颜色（除特殊视觉如 `#d29922` 远程 amber）
