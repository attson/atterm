import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

const RELAY_HTTP = 'http://127.0.0.1:8080'
const RELAY_WS = 'ws://127.0.0.1:8080'

// PR-B introduces the first real entries (login + signup). Index,
// settings, admin are still served from web/legacy/ via build-web.sh
// layer 1; they migrate in PR-C, PR-D, PR-E.
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
        login:    fileURLToPath(new URL('./login.html',        import.meta.url)),
        signup:   fileURLToPath(new URL('./signup.html',       import.meta.url)),
        settings: fileURLToPath(new URL('./settings.html',     import.meta.url)),
        admin:    fileURLToPath(new URL('./admin/index.html',  import.meta.url)),
      },
    },
  },
})
