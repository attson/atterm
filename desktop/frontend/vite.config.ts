import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// VITE_TARGET picks the build:
//   - 'wails'     (default): index.html → dist/        (Wails embeds dist)
//   - 'capacitor':           index.capacitor.html → dist-capacitor/index.html
//                            (mobile/scripts/sync-web.mjs syncs into mobile/www/)
const target = process.env.VITE_TARGET ?? 'wails'

// Vite keeps the source HTML basename in the output. Capacitor expects the
// entry to be index.html, so for the capacitor target rename the emitted
// index.capacitor.html asset to index.html during bundle generation.
function renameCapacitorEntry(): Plugin {
  return {
    name: 'rename-capacitor-entry',
    enforce: 'post',
    generateBundle(_options, bundle) {
      for (const key of Object.keys(bundle)) {
        const asset = bundle[key]
        if (asset.fileName.endsWith('index.capacitor.html')) {
          asset.fileName = asset.fileName.replace(/index\.capacitor\.html$/, 'index.html')
        }
      }
    },
  }
}

export default defineConfig({
  plugins: [
    vue(),
    ...(target === 'capacitor' ? [renameCapacitorEntry()] : []),
  ],
  resolve: {
    alias: {
      '@shared': fileURLToPath(new URL('../../web/src/shared', import.meta.url)),
    },
  },
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
