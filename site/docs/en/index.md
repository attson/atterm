---
layout: home
title: AT Term - Local-first terminal for SSH and AI agents
titleTemplate: false
description: AT Term is a local-first cross-platform terminal, SSH workspace, and AI agent console with end-to-end encrypted remote takeover through Relay or Peer Space.
hero:
  name: AT Term
  text: A cross-device terminal for the AI era
  highlightsLabel: Product highlights
  highlights:
    - AI task status
    - Remote view and control
    - SSH host management
    - File browsing and editing
    - End-to-end encryption
    - Configuration sync
    - Cross-platform
  actions:
    - theme: brand
      text: Choose a download
      link: '#downloads'
    - theme: alt
      text: Product manual
      link: /en/guide/
    - theme: alt
      text: Deploy Relay
      link: /en/guide/deploy-relay
---

<div class="tech-home">

<HomeDemo locale="en" />

## Choose how you connect

<p class="section-lead">Start with AT Term as a local terminal. Add an account-based Relay or a peer-to-peer space only when you need access across devices.</p>

<div class="connection-paths">
  <section><span class="path-index">01</span><h3>Local terminal and SSH</h3><p>Local PTYs remain local-first. Manage SSH hosts, tabs, and panes; a Relay outage never blocks your local shell.</p><a href="/atterm/en/guide/">Start on desktop</a></section>
  <section><span class="path-index">02</span><h3>Account Relay</h3><p>For persistent access across your own devices. Self-host accounts, history replay, Web/PWA access, notifications, and end-to-end encryption in one place.</p><a href="/atterm/en/guide/deploy-relay.html">Deploy your Relay</a></section>
  <section><span class="path-index">03</span><h3>Peer Space</h3><p>For trusted device groups without accounts. Rendezvous handles discovery and encrypted signaling; terminal traffic bypasses it after WebRTC connects.</p><a href="/atterm/en/guide/deploy-rendezvous.html">Learn about Rendezvous</a></section>
</div>

## Built for long-running terminal work

<div class="capability-rows">
  <section><h3>Session state and takeover</h3><p>OSC 133 derives running, waiting, done, and failed states. Remote clients start as viewers and write only after an explicit takeover. No PTY bytes are uploaded while nobody watches.</p></section>
  <section><h3>AI integration with clear boundaries</h3><p>Claude Code, Codex, Aider, and Gemini CLI are recognized. Claude and Codex support session-ID resume, Aider replays its launch command, and Notification hooks plus Feishu AskUserQuestion currently apply only to Claude Code.</p></section>
  <section><h3>End-to-end encryption and permissions</h3><p>With E2EE enabled, terminal output, titles, cwd, and task summaries stay opaque to Relay. Owners choose view, control, or full access for input and files.</p></section>
  <section><h3>Files, notifications, and recovery</h3><p>Browse remote files with full permission, receive system, Web Push, or Feishu alerts, and recover the correct conversation by session ID after a desktop restart.</p></section>
</div>

## Downloads

<p class="section-lead">Desktop packages come from the latest GitHub Release. Automatic updates require a valid signature and SHA256 hash; manual downloads can be checked against the same manifest below.</p>

<ReleaseDownloads locale="en" />

</div>
