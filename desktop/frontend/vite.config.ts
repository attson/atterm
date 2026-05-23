import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// VITE_TARGET picks the build:
//   - 'wails'     (default): index.html → dist/        (Wails embeds dist)
//   - 'capacitor':           index.capacitor.html → dist-capacitor/index.html
//                            (mobile/scripts/sync-web.mjs syncs into mobile/www/)
// The capacitor input is keyed `index` so Vite emits dist-capacitor/index.html,
// which is the entry filename Capacitor expects.
const target = process.env.VITE_TARGET ?? 'wails'

export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: target === 'capacitor' ? 'dist-capacitor' : 'dist',
    emptyOutDir: true,
    rollupOptions: {
      input: target === 'capacitor'
        ? { index: fileURLToPath(new URL('./index.capacitor.html', import.meta.url)) }
        : fileURLToPath(new URL('./index.html', import.meta.url)),
    },
  },
})
