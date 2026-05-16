import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

// Independent vite config for the FileTree style preview. Run with:
//   npx vite --config vite.preview.config.ts
// Output is the dev server at http://localhost:5173/ — no backend needed.
export default defineConfig({
  root: "preview",
  plugins: [vue()],
  server: {
    port: 5173,
    strictPort: true,
    host: "127.0.0.1",
  },
});
