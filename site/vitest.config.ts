import { defineConfig } from 'vitest/config'
import { fileURLToPath, URL } from 'node:url'

// mock 层单测复用真实前端源码里的类型/工具(proto、platform types),
// 因此需要与 vitepress config.mjs 一致的 alias。路径相对 site/ 根。
export default defineConfig({
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('../desktop/frontend/src', import.meta.url)),
      '@webshared': fileURLToPath(new URL('../web/src/shared', import.meta.url)),
      '@shared': fileURLToPath(new URL('../web/src/shared', import.meta.url)),
    },
  },
})
