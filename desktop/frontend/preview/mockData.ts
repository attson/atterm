// Mock dataset for the FileTree / FileExplorer demo. Each leaf entry may
// carry inline content so the preview can render a real editor view without
// any backend wails binding.

export interface MockEntry {
  name: string;
  isDir: boolean;
  children?: MockEntry[];
  // Leaf-only fields:
  content?: string;
  isBinary?: boolean;
  tooLarge?: boolean;
}

const readmeContent = `# AT Term

A cross-platform terminal emulator with remote takeover.

## Features

- **Multi-pane**: split horizontally / vertically up to a 2x2 grid.
- **Remote attach**: hand the session off to another machine over a relay.
- **Plugins**: lightweight Vue 3 plugin host with two render surfaces.

## Quick start

\`\`\`bash
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --web web
\`\`\`

See \`docs/spec/\` for the full architecture documents.
`;

const pkgJsonContent = `{
  "name": "atterm-frontend",
  "version": "0.2.0",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vue-tsc --noEmit && vite build",
    "test": "vitest"
  },
  "dependencies": {
    "vue": "^3.4.0",
    "pinia": "^3.0.4",
    "xterm": "^5.3.0",
    "@codemirror/state": "^6.0.0"
  }
}
`;

const appVueContent = `<script lang="ts" setup>
import { ref, onMounted } from "vue";
import TabBar from "./components/TabBar.vue";
import PaneGrid from "./components/PaneGrid.vue";

const tabs = ref<Tab[]>([]);
const status = ref<"loading" | "ready" | "error">("loading");

onMounted(async () => {
  status.value = "ready";
});
</script>

<template>
  <div class="app-root">
    <TabBar :tabs="tabs" />
    <main class="main">
      <PaneGrid v-if="status === 'ready'" />
    </main>
  </div>
</template>
`;

const mainTsContent = `import { createApp } from "vue";
import { createPinia } from "pinia";
import App from "./App.vue";
import "./style.css";

const app = createApp(App);
app.use(createPinia());
app.mount("#app");
`;

const viteCfgContent = `import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

export default defineConfig({
  plugins: [vue()],
  server: { port: 5173 },
});
`;

const tsConfigContent = `{
  "compilerOptions": {
    "target": "ES2020",
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "strict": true,
    "jsx": "preserve",
    "skipLibCheck": true
  },
  "include": ["src/**/*"]
}
`;

const serverGoContent = `package main

import (
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	log.Println("listening on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
`;

const stylesScssContent = `$primary: #1177bb;
$radius: 4px;

.app-root {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: #1e1e1e;
  color: #cccccc;
  & .panel { border-radius: $radius; }
}
`;

const buildShContent = `#!/usr/bin/env bash
set -euo pipefail

VERSION=$(git describe --tags --always)
echo "building atterm $VERSION"
go build -ldflags "-X main.Version=$VERSION" -o build/atterm ./cmd/atterm
`;

export const datasets: { id: string; label: string; root: string; entries: MockEntry[] }[] = [
  {
    id: "typical",
    label: "Typical project",
    root: "~/code/my-app",
    entries: [
      {
        name: "src",
        isDir: true,
        children: [
          {
            name: "components",
            isDir: true,
            children: [
              { name: "Button.vue", isDir: false, content: appVueContent },
              { name: "Card.vue", isDir: false, content: appVueContent },
              { name: "Modal.vue", isDir: false, content: appVueContent },
            ],
          },
          {
            name: "lib",
            isDir: true,
            children: [
              { name: "api.ts", isDir: false, content: mainTsContent },
              { name: "connection.ts", isDir: false, content: mainTsContent },
              { name: "types.ts", isDir: false, content: "export interface User { id: string; name: string }\n" },
            ],
          },
          { name: "App.vue", isDir: false, content: appVueContent },
          { name: "main.ts", isDir: false, content: mainTsContent },
          { name: "style.scss", isDir: false, content: stylesScssContent },
        ],
      },
      {
        name: "tests",
        isDir: true,
        children: [
          { name: "setup.ts", isDir: false, content: "// vitest setup\n" },
          { name: "App.spec.ts", isDir: false, content: "import { test, expect } from 'vitest';\ntest('placeholder', () => expect(true).toBe(true));\n" },
        ],
      },
      {
        name: "public",
        isDir: true,
        children: [
          { name: "favicon.ico", isDir: false, isBinary: true },
          { name: "robots.txt", isDir: false, content: "User-agent: *\nAllow: /\n" },
        ],
      },
      { name: ".gitignore", isDir: false, content: "node_modules\ndist\n.env*\n" },
      { name: "README.md", isDir: false, content: readmeContent },
      { name: "package.json", isDir: false, content: pkgJsonContent },
      { name: "package-lock.json", isDir: false, tooLarge: true },
      { name: "tsconfig.json", isDir: false, content: tsConfigContent },
      { name: "vite.config.ts", isDir: false, content: viteCfgContent },
    ],
  },
  {
    id: "long-names",
    label: "Long file names (tests ellipsis)",
    root: "~/code/super-long-project-name-for-truncation-tests",
    entries: [
      { name: "this-is-a-really-really-really-long-filename.ts", isDir: false, content: mainTsContent },
      { name: "another-one-with-a-lot-of-words.spec.ts", isDir: false, content: "// long-name spec\n" },
      { name: "short.md", isDir: false, content: "# short\n" },
      {
        name: "deeply-nested-directory-name-that-overflows",
        isDir: true,
        children: [
          { name: "inside.json", isDir: false, content: "{\n  \"key\": \"value\"\n}\n" },
          {
            name: "even-more-nested-directory-name-here",
            isDir: true,
            children: [{ name: "leaf.txt", isDir: false, content: "leaf\n" }],
          },
        ],
      },
    ],
  },
  {
    id: "mixed",
    label: "Mixed extensions",
    root: "~/code/full-stack",
    entries: [
      {
        name: "backend",
        isDir: true,
        children: [
          { name: "server.go", isDir: false, content: serverGoContent },
          { name: "handler.go", isDir: false, content: "package main\n\n// handlers\n" },
          { name: "go.mod", isDir: false, content: "module github.com/example/app\n\ngo 1.23\n" },
          { name: "go.sum", isDir: false, tooLarge: true },
        ],
      },
      {
        name: "frontend",
        isDir: true,
        children: [
          { name: "index.html", isDir: false, content: "<!doctype html>\n<html><body><div id=app></div></body></html>\n" },
          { name: "App.vue", isDir: false, content: appVueContent },
          { name: "style.scss", isDir: false, content: stylesScssContent },
        ],
      },
      {
        name: "docs",
        isDir: true,
        children: [
          { name: "design.md", isDir: false, content: readmeContent },
          { name: "diagram.svg", isDir: false, isBinary: true },
          { name: "screenshot.png", isDir: false, isBinary: true },
        ],
      },
      {
        name: "scripts",
        isDir: true,
        children: [
          { name: "build.sh", isDir: false, content: buildShContent },
          { name: "deploy.py", isDir: false, content: "import os\n\nprint('deploying...')\n" },
        ],
      },
      { name: "Dockerfile", isDir: false, content: "FROM golang:1.23-alpine\nWORKDIR /app\nCOPY . .\nRUN go build -o app .\nCMD [\"./app\"]\n" },
      { name: "Makefile", isDir: false, content: "build:\n\tgo build ./...\n\ntest:\n\tgo test ./...\n" },
      { name: ".env", isDir: false, content: "API_URL=https://api.example.com\n" },
      { name: "LICENSE", isDir: false, content: "MIT License\n\nCopyright (c) 2026\n" },
    ],
  },
];

// flatten produces a quick path -> entry lookup for the editor pane.
export function flatten(root: string, entries: MockEntry[]): Map<string, MockEntry> {
  const map = new Map<string, MockEntry>();
  function walk(parent: string, items: MockEntry[]) {
    for (const e of items) {
      const path = `${parent}/${e.name}`;
      map.set(path, e);
      if (e.isDir && e.children) walk(path, e.children);
    }
  }
  walk(root, entries);
  return map;
}
