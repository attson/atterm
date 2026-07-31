import { defineConfig } from 'vitepress'

// 项目页部署在 https://attson.github.io/atterm/,base 必须与仓库名一致,
// 否则静态资源 404。
export default defineConfig({
  base: '/atterm/',
  lang: 'zh-CN',
  title: 'AT Term',
  description: '带远程接管能力的跨平台终端(桌面 + 浏览器 + 手机)',
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
})
