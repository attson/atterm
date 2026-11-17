import { defineConfig } from 'vitepress'
import { fileURLToPath } from 'node:url'

// 首页 demo 复用真实前端源码(desktop/frontend/src 与 web/src/shared),通过
// vite alias 接入,零侵入。wailsjs/* 指向 mock stub(demo 走 web/mock 平台,
// 不触达 Wails 绑定)。dedupe 保证 vue/pinia/naive-ui/xterm 落到 site 的单例。
const R = (p) => fileURLToPath(new URL(p, import.meta.url))
const SITE_ORIGIN = 'https://attson.github.io'
const SITE_BASE = '/atterm/'
const SOCIAL_IMAGE = `${SITE_ORIGIN}${SITE_BASE}atterm-demo-poster.png`

const zhNav = [
  { text: '首页', link: '/' },
  { text: '文档', link: '/guide/' },
  { text: '部署 Relay', link: '/guide/deploy-relay' },
  { text: '下载', link: '/#下载' },
]
const enNav = [
  { text: 'Home', link: '/en/' },
  { text: 'Documentation', link: '/en/guide/' },
  { text: 'Deploy Relay', link: '/en/guide/deploy-relay' },
  { text: 'Download', link: '/en/#downloads' },
]
const zhSidebar = [{
  text: '使用文档',
  items: [
    { text: '介绍与快速上手', link: '/guide/' },
    { text: '远程接管与会话侧栏', link: '/guide/remote-takeover' },
    { text: '端到端加密与安全', link: '/guide/e2ee' },
    { text: '部署 Relay', link: '/guide/deploy-relay' },
    { text: '部署 Rendezvous', link: '/guide/deploy-rendezvous' },
    { text: 'AI Agent 与 Feishu', link: '/guide/ai-agents' },
    { text: 'FAQ / 故障排查', link: '/guide/faq' },
  ],
}]
const enSidebar = [{
  text: 'Documentation',
  items: [
    { text: 'Introduction and quick start', link: '/en/guide/' },
    { text: 'Remote takeover and sessions', link: '/en/guide/remote-takeover' },
    { text: 'End-to-end encryption', link: '/en/guide/e2ee' },
    { text: 'Deploy Relay', link: '/en/guide/deploy-relay' },
    { text: 'Deploy Rendezvous', link: '/en/guide/deploy-rendezvous' },
    { text: 'AI agents and Feishu', link: '/en/guide/ai-agents' },
    { text: 'FAQ and troubleshooting', link: '/en/guide/faq' },
  ],
}]

function canonicalURL(page) {
  let route = page.replace(/\\/g, '/').replace(/\.md$/, '')
  if (route === 'index') route = ''
  if (route.endsWith('/index')) route = route.slice(0, -'index'.length)
  else if (route) route += '.html'
  return new URL(`${SITE_BASE}${route}`, SITE_ORIGIN).href
}

// 项目页部署在 https://attson.github.io/atterm/,base 必须与仓库名一致,
// 否则静态资源 404。
export default defineConfig({
  base: '/atterm/',
  lang: 'zh-CN',
  locales: {
    root: { label: '简体中文', lang: 'zh-CN', link: '/' },
    en: { label: 'English', lang: 'en-US', link: '/en/' },
  },
  title: 'AT Term',
  titleTemplate: ':title | AT Term',
  description: '本地优先的跨平台终端、SSH 工作台与 AI Agent 控制台，支持端到端加密的桌面、Web 和 iOS 远程会话接管。',
  sitemap: {
    hostname: 'https://attson.github.io/atterm/',
  },
  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/atterm/favicon.svg' }],
  ],
  transformHead({ page, pageData, title, description }) {
    if (pageData.isNotFound) {
      return [['meta', { name: 'robots', content: 'noindex, nofollow' }]]
    }

    const url = canonicalURL(page)
    const isEnglish = page.startsWith('en/')
    const locale = isEnglish ? 'en_US' : 'zh_CN'
    const head = [
      ['link', { rel: 'canonical', href: url }],
      ['meta', { property: 'og:type', content: 'website' }],
      ['meta', { property: 'og:site_name', content: 'AT Term' }],
      ['meta', { property: 'og:locale', content: locale }],
      ['meta', { property: 'og:locale:alternate', content: isEnglish ? 'zh_CN' : 'en_US' }],
      ['meta', { property: 'og:title', content: title }],
      ['meta', { property: 'og:description', content: description }],
      ['meta', { property: 'og:url', content: url }],
      ['meta', { property: 'og:image', content: SOCIAL_IMAGE }],
      ['meta', { property: 'og:image:width', content: '1200' }],
      ['meta', { property: 'og:image:height', content: '630' }],
      ['meta', { property: 'og:image:alt', content: isEnglish ? 'AT Term terminal and session interface' : 'AT Term 终端与会话界面' }],
      ['meta', { name: 'twitter:card', content: 'summary_large_image' }],
      ['meta', { name: 'twitter:title', content: title }],
      ['meta', { name: 'twitter:description', content: description }],
      ['meta', { name: 'twitter:image', content: SOCIAL_IMAGE }],
    ]

    if (page === 'index.md' || page === 'en/index.md') {
      head.push([
        'script',
        { type: 'application/ld+json' },
        JSON.stringify({
          '@context': 'https://schema.org',
          '@type': 'SoftwareApplication',
          name: 'AT Term',
          applicationCategory: 'DeveloperApplication',
          operatingSystem: 'macOS, Linux, Windows, iOS, Web',
          description,
          url,
          downloadUrl: 'https://github.com/attson/atterm/releases/latest',
          codeRepository: 'https://github.com/attson/atterm',
          license: 'https://www.apache.org/licenses/LICENSE-2.0',
          offers: {
            '@type': 'Offer',
            price: '0',
            priceCurrency: 'USD',
          },
        }),
      ])
    }

    return head
  },
  themeConfig: {
    locales: {
      root: { label: '简体中文', nav: zhNav, sidebar: { '/guide/': zhSidebar } },
      en: { label: 'English', nav: enNav, sidebar: { '/en/guide/': enSidebar } },
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
        // webTabsSnapshot:让 loadSnapshot 恒 null,从源头关闭 restoreFromWebSnapshot
        // (它会把残留快照恢复成 remote:true,导致本机会话被误标「远端」+「(空)」)。
        { find: /^.*lib\/webTabsSnapshot$/, replacement: R('./theme/components/mock/webTabsSnapshotStub.ts') },
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
