```
   █████╗ ████████╗    ████████╗███████╗██████╗ ███╗   ███╗
  ██╔══██╗╚══██╔══╝    ╚══██╔══╝██╔════╝██╔══██╗████╗ ████║
  ███████║   ██║          ██║   █████╗  ██████╔╝██╔████╔██║
  ██╔══██║   ██║          ██║   ██╔══╝  ██╔══██╗██║╚██╔╝██║
  ██║  ██║   ██║          ██║   ███████╗██║  ██║██║ ╚═╝ ██║   █
  ╚═╝  ╚═╝   ╚═╝          ╚═╝   ╚══════╝╚═╝  ╚═╝╚═╝     ╚═╝
```

**带远程接管的跨平台终端**  ·  OPEN SOURCE [GO · VUE]

桌面端启动的 shell、Codex、Claude 等长任务,离开电脑后从手机、浏览器或另一台电脑继续查看和输入。启用 E2EE 后,输出对 relay 全程不可读。

---

- **体验** — <https://attson.github.io/atterm/>
- **下载** — [GitHub Releases](https://github.com/attson/atterm/releases/latest)
- **文档** — <https://attson.github.io/atterm/guide/>
- **协议** — Apache-2.0

---

## 跑起来

```bash
# 只用桌面端:到 Releases 下载对应平台的包,启动即用。

# 源码调试:
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --dev-insecure   # 终端 1
cd desktop && wails dev -tags webkit2_41                          # 终端 2(macOS/Windows 省略 -tags)
```

依赖 Go 1.23+ / Node 20+ / Wails v2.12.0。Linux 需
`libgtk-3-dev libwebkit2gtk-4.1-dev libxext-dev`。

---

## 文档

[快速上手](https://attson.github.io/atterm/guide/) ·
[远程接管](https://attson.github.io/atterm/guide/remote-takeover) ·
[端到端加密](https://attson.github.io/atterm/guide/e2ee) ·
[部署 Relay](https://attson.github.io/atterm/guide/deploy-relay) ·
[AI Agent 与飞书](https://attson.github.io/atterm/guide/ai-agents) ·
[FAQ](https://attson.github.io/atterm/guide/faq)

贡献者从 [AGENTS.md](./AGENTS.md) 开始;架构 / 协议 / 鉴权规范在 [docs/spec/](./docs/spec/)。

## 发布

发布统一走预检脚本，不要手工创建并推送版本 tag：

```bash
git config core.hooksPath .githooks   # 每个 clone 启用一次
./scripts/ship-release.sh              # 当前 HEAD 可达的最高版本线递增 patch
./scripts/ship-release.sh --line v0.2 # 明确维护旧版本线
```

脚本会确认 `main` 与远端同步、工作区干净、嵌入的
`internal/relay/web-dist/` 与 Node 20 / Go 固定版本构建完全一致，然后才创建并推送 tag。
`web/` 或 `desktop/frontend/src/` 有改动时，先运行 `./scripts/build-web.sh` 并把生成结果提交。
