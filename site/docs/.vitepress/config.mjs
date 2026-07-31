import { defineConfig } from 'vitepress'
import { fileURLToPath } from 'node:url'

// 首页 demo 复用真实前端源码(desktop/frontend/src 与 web/src/shared),通过
// vite alias 接入,零侵入。wailsjs/* 指向 mock stub(demo 走 web/mock 平台,
// 不触达 Wails 绑定)。dedupe 保证 vue/pinia/naive-ui/xterm 落到 site 的单例。
const R = (p) => fileURLToPath(new URL(p, import.meta.url))

// 项目页部署在 https://attson.github.io/atterm/,base 必须与仓库名一致,
// 否则静态资源 404。
export default defineConfig({
  base: '/atterm/',
  lang: 'zh-CN',
  title: 'AT Term',
  description: '带远程接管能力的跨平台终端(桌面 + 浏览器 + 手机)',
  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/atterm/favicon.svg' }],
  ],
  themeConfig: {
    nav: [
      { text: '首页', link: '/' },
      { text: '文档', link: '/guide/' },
      { text: '部署 Relay', link: '/guide/deploy-relay' },
      { text: '下载', link: 'https://github.com/attson/atterm/releases/latest' },
    ],
    sidebar: {
      '/guide/': [
        {
          text: '使用文档',
          items: [
            { text: '介绍与快速上手', link: '/guide/' },
            { text: '远程接管与会话侧栏', link: '/guide/remote-takeover' },
            { text: '端到端加密与安全', link: '/guide/e2ee' },
            { text: '部署 Relay', link: '/guide/deploy-relay' },
            { text: 'AI Agent 与 Feishu', link: '/guide/ai-agents' },
            { text: 'FAQ / 故障排查', link: '/guide/faq' },
          ],
        },
      ],
    },
    socialLinks: [{ icon: 'github', link: 'https://github.com/attson/atterm' }],
    search: { provider: 'local' },
  },
  vite: {
    resolve: {
      alias: [
        // wailsjs/* 必须排在 @ 之前,否则 @/… 里对 ../../wailsjs 的相对 import
        // 解析出的绝对路径不受影响(相对 import 不走 alias),但保险起见先匹配。
        { find: /^.*wailsjs\/go\/main\/App$/, replacement: R('./theme/components/mock/wailsStub.ts') },
        { find: /^.*wailsjs\/runtime\/runtime$/, replacement: R('./theme/components/mock/wailsStub.ts') },
        // opaqueWasm.ts 会 import gitignore 的 wasm_exec.js(CI 无此文件),而
        // demo 不走 OPAQUE 登录 —— 用桩替换整个模块。必须排在 @shared/@ 之前。
        { find: /^.*lib\/opaqueWasm$/, replacement: R('./theme/components/mock/opaqueWasmStub.ts') },
        { find: /^@webshared\//, replacement: R('../../../web/src/shared/') },
        { find: /^@shared\//, replacement: R('../../../web/src/shared/') },
        { find: /^@\//, replacement: R('../../../desktop/frontend/src/') },
      ],
      dedupe: ['vue', 'pinia', 'naive-ui', 'xterm', 'xterm-addon-fit', 'vfonts'],
    },
    ssr: {
      // App.vue 及其依赖是浏览器端组件,交给 client 渲染(HomeDemo 包了
      // ClientOnly 且动态 import);这些包在 SSR 阶段不外部化以免 Node 解析报错。
      noExternal: ['naive-ui', 'xterm', 'xterm-addon-fit', 'vfonts'],
    },
  },
})
