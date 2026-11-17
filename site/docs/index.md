---
layout: home
title: AT Term - 本地优先的 SSH 与 AI Agent 跨平台终端
titleTemplate: false
description: AT Term 是本地优先的跨平台终端、SSH 工作台与 AI Agent 控制台，支持通过端到端加密的 Relay 或 Peer Space 从桌面、浏览器和 iPhone 远程接管会话。
hero:
  name: AT Term
  text: AI 时代的跨设备终端
  highlightsLabel: 产品卖点
  highlights:
    - AI 任务状态
    - 远程查看与接管
    - SSH 主机管理
    - 文件浏览与编辑
    - 端到端加密
    - 配置同步
    - 全平台支持
  actions:
    - theme: brand
      text: 选择下载
      link: '#下载'
    - theme: alt
      text: 产品手册
      link: /guide/
    - theme: alt
      text: 部署 Relay
      link: /guide/deploy-relay
---

<div class="tech-home">

<HomeDemo />

## 选择连接方式

<p class="section-lead">先把 AT Term 当成一个本地终端使用；需要跨设备时，再按账号制或点对点场景接入。</p>

<div class="connection-paths">
  <section><span class="path-index">01</span><h3>本地终端与 SSH</h3><p>本机 PTY、本地优先。管理 SSH 主机、标签页和分屏；Relay 不可用也不影响本地 shell。</p><a href="/atterm/guide/">从桌面端开始</a></section>
  <section><span class="path-index">02</span><h3>账户 Relay</h3><p>适合固定设备和持续远程接管。自托管账号、历史回放、Web/PWA、通知与端到端加密集中在一处。</p><a href="/atterm/guide/deploy-relay.html">部署自己的 Relay</a></section>
  <section><span class="path-index">03</span><h3>Peer Space</h3><p>适合无账户的可信设备组。Rendezvous 只负责发现和加密信令，建立 WebRTC 后终端流量不经过它。</p><a href="/atterm/guide/deploy-rendezvous.html">了解 Rendezvous</a></section>
</div>

## 为终端里的长任务而设计

<div class="capability-rows">
  <section><h3>会话状态与远程接管</h3><p>OSC 133 推导 running、waiting、done、failed；远程默认 viewer，明确 take over 后才写入。无人观看时不上传 PTY 字节。</p></section>
  <section><h3>AI Agent 有边界的集成</h3><p>Claude Code、Codex、Aider、Gemini CLI 都可识别分类；Claude/Codex 支持会话 ID 恢复，Aider 重放启动命令；Notification hook 与飞书 AskUserQuestion 目前只覆盖 Claude Code。</p></section>
  <section><h3>端到端加密与权限</h3><p>启用 E2EE 后，终端输出、标题、cwd 和任务摘要对 Relay 保持密文。owner 以 view/control/full 控制远程输入和文件访问。</p></section>
  <section><h3>文件、通知与恢复</h3><p>在 full 权限下浏览远端文件，任务完成或等待输入时接收系统、Web Push 或飞书通知；桌面重启后按 session ID 恢复正确的会话。</p></section>
</div>

## 下载

<p class="section-lead">桌面构件直接来自 GitHub 最新 Release。自动更新会强制验签并校验 SHA256；手动下载可使用下方同一份签名清单核验。</p>

<ReleaseDownloads locale="zh" />

</div>
