import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

const RELAY_HTTP = 'http://127.0.0.1:8080'
const RELAY_WS = 'ws://127.0.0.1:8080'

// PR-E introduces the terminal home (index) entry — the last entry to
// migrate. All five MPA entries are now Vue 3 + Naive UI. Legacy admin
// + settings + login/signup/index all served from web/legacy/ are
// fully replaced; PR-F handles cutover (icons → public/, sw via
// vite-plugin-pwa, legacy/ removal).
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@shared': fileURLToPath(new URL('./src/shared', import.meta.url)),
      '@':       fileURLToPath(new URL('./src',        import.meta.url)),
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api':       { target: RELAY_HTTP, changeOrigin: false },
      '/sub':       { target: RELAY_HTTP, changeOrigin: false },
      '/admin/api': { target: RELAY_HTTP, changeOrigin: false },
      '/agent':     { target: RELAY_WS,   ws: true, changeOrigin: false },
      '/uplink':    { target: RELAY_WS,   ws: true, changeOrigin: false },
      '/client':    { target: RELAY_WS,   ws: true, changeOrigin: false },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    assetsInlineLimit: 0,
    rollupOptions: {
      input: {
        index:    fileURLToPath(new URL('./index.html',           import.meta.url)),
        login:    fileURLToPath(new URL('./login.html',           import.meta.url)),
        signup:   fileURLToPath(new URL('./signup.html',          import.meta.url)),
        settings: fileURLToPath(new URL('./settings.html',        import.meta.url)),
        admin:    fileURLToPath(new URL('./admin/index.html',     import.meta.url)),
      },
    },
  },
})
