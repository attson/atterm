import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { VitePWA } from 'vite-plugin-pwa'
import { fileURLToPath, URL } from 'node:url'

const RELAY_HTTP = 'http://127.0.0.1:8080'
const RELAY_WS = 'ws://127.0.0.1:8080'

// All five MPA entries are Vue 3 + Naive UI. vite-plugin-pwa generates
// the service worker and manifest; runtime caching is disabled per Sec-5
// (static-only precache) and navigateFallback is null because this is an
// MPA, not an SPA.
export default defineConfig({
  plugins: [
    vue(),
    VitePWA({
      strategies: 'generateSW',
      registerType: 'autoUpdate',
      injectRegister: null,
      includeAssets: ['icon.svg', 'icon.png'],
      manifest: {
        name: 'AT Term',
        short_name: 'AT Term',
        description: 'Attach to AT Term sessions from your phone.',
        start_url: '.',
        scope: '.',
        display: 'standalone',
        background_color: '#05070d',
        theme_color: '#0b1020',
        icons: [
          { src: 'icon.png', sizes: '1024x1024', type: 'image/png', purpose: 'any maskable' },
          { src: 'icon.svg', sizes: 'any',       type: 'image/svg+xml', purpose: 'any' },
        ],
      },
      workbox: {
        globPatterns: ['**/*.{js,css,html,svg,png,ico,woff2,webmanifest}'],
        importScripts: ['notification-click.js'],
        runtimeCaching: [],
        navigateFallback: null,
        cleanupOutdatedCaches: true,
      },
      devOptions: {
        enabled: false,
      },
    }),
  ],
  resolve: {
    alias: {
      '@shared':    fileURLToPath(new URL('./src/shared', import.meta.url)),
      '@webshared': fileURLToPath(new URL('./src/shared', import.meta.url)),
      '@':          fileURLToPath(new URL('../desktop/frontend/src', import.meta.url)),
    },
    // main.web.ts lives under ../desktop/frontend/src/, so Rollup's default
    // resolution walks up from that path and never reaches web/node_modules
    // where pinia/vue/xterm/naive-ui/etc. are installed. `dedupe` forces
    // these packages to resolve from web/ project root regardless of the
    // importer's file location.
    dedupe: ['vue', 'pinia', 'naive-ui', 'xterm', 'xterm-addon-fit', 'vfonts'],
  },
  server: {
    port: 5173,
    strictPort: true,
    // The `main` entry (index.html) mounts desktop/frontend/src/main.web.ts,
    // which lives outside this project's root — allow vite's dev server to
    // serve files from there.
    fs: {
      allow: [
        fileURLToPath(new URL('.', import.meta.url)),
        fileURLToPath(new URL('../desktop/frontend/src', import.meta.url)),
      ],
    },
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
        admin:    fileURLToPath(new URL('./admin/index.html',     import.meta.url)),
        setup:    fileURLToPath(new URL('./setup.html',           import.meta.url)),
        firstrun: fileURLToPath(new URL('./firstrun.html',        import.meta.url)),
      },
    },
  },
})
