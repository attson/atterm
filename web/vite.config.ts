import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// PR-A wires up Vite with a single placeholder entry; `npm run build`
// emits a harmless artifact that build-web.sh filters out before
// rsyncing into web-dist. Real entries arrive in PR-B (login/signup),
// PR-C (settings), PR-D (admin), PR-E (terminal home); the placeholder
// is deleted when PR-B lands.
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@shared': fileURLToPath(new URL('./src/shared', import.meta.url)),
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    assetsInlineLimit: 0,
    rollupOptions: {
      input: {
        _placeholder: fileURLToPath(new URL('./src/_placeholder.html', import.meta.url)),
      },
    },
  },
})
