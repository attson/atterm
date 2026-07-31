---
layout: home
hero:
  name: AT Term
  text: 带远程接管的跨平台终端
  tagline: 桌面端启动的 shell、Codex、Claude 等长任务,离开电脑后从手机、浏览器或另一台电脑继续查看和输入。启用 E2EE 后,输出对 relay 全程不可读。
  actions:
    - theme: brand
      text: 下载最新版
      link: https://github.com/attson/atterm/releases/latest
    - theme: alt
      text: 使用文档
      link: /guide/
    - theme: alt
      text: 部署 Relay
      link: /guide/deploy-relay
---

<script setup>
import HomeDemo from './.vitepress/theme/components/HomeDemo.vue'
</script>

<div class="tech-home">

<HomeDemo />

<section class="badge-strip">
  <div class="badge-wrap">
    <div class="badge"><span class="badge-k">E2EE</span><span class="badge-v">端到端加密</span></div>
    <div class="badge"><span class="badge-k">3 平台</span><span class="badge-v">macOS / Linux / Windows</span></div>
    <div class="badge"><span class="badge-k">OSC 133</span><span class="badge-v">任务状态推导</span></div>
    <div class="badge"><span class="badge-k">MCP</span><span class="badge-v">AI / CLI 控制</span></div>
    <div class="badge"><span class="badge-k">Web Push</span><span class="badge-v">Feishu / webhook 通知</span></div>
    <div class="badge"><span class="badge-k">Apache-2.0</span><span class="badge-v">开源</span></div>
  </div>
</section>

## 核心能力

<div class="feature-cards">
  <div class="feature-card"><h3>远程接管(lazy 同步)</h3><p>桌面连上 relay 后,手机 / 浏览器 / 另一台桌面可 attach 同一会话;默认 viewer,take over 才能写。无人看时不上传字节。</p></div>
  <div class="feature-card"><h3>会话状态与侧栏</h3><p>OSC 133 推导 running / waiting / done / failed;侧栏可搜索、置顶、按 host 分组,attention 高亮 AI 等输入的会话。</p></div>
  <div class="feature-card"><h3>多通道通知</h3><p>命令完成、AI 等输入触发系统通知 / Web Push / 飞书卡片 / 出站 webhook,payload 带 session id 与摘要。</p></div>
  <div class="feature-card"><h3>AI Agent 识别</h3><p>Claude Code / Codex / Aider / Gemini 自动识别:命令分类、resume 注入、Notification hook 自动安装。</p></div>
  <div class="feature-card"><h3>端到端加密</h3><p>account_key 只在客户端持有;输出 / 标题 / cwd / 摘要在 relay 侧全程密文,自托管不外泄。</p></div>
  <div class="feature-card"><h3>远程文件浏览</h3><p>浏览 owner 机器文件系统、编辑保存、全套 CRUD + 回收站,双源切换本地 / 远程。</p></div>
</div>

## 下载

<div class="downloads">
  <a class="download-card" href="https://github.com/attson/atterm/releases/latest" target="_blank" rel="noreferrer"><div class="os">macOS</div><div class="os-sub">.dmg / .zip · Intel / Apple Silicon</div></a>
  <a class="download-card" href="https://github.com/attson/atterm/releases/latest" target="_blank" rel="noreferrer"><div class="os">Linux</div><div class="os-sub">.deb / .tar.gz · amd64 / arm64</div></a>
  <a class="download-card" href="https://github.com/attson/atterm/releases/latest" target="_blank" rel="noreferrer"><div class="os">Windows</div><div class="os-sub">.exe / .zip · amd64</div></a>
</div>

<p style="margin-top:12px;opacity:.7;font-size:13px">三平台均前往 <a href="https://github.com/attson/atterm/releases/latest" target="_blank" rel="noreferrer">GitHub Releases</a> 下载最新版。</p>

</div>
