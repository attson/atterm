# Vue 3 + TypeScript + Vite

This template should help get you started developing with Vue 3 and TypeScript in Vite. The template uses Vue
3 `<script setup>` SFCs, check out
the [script setup docs](https://v3.vuejs.org/api/sfc-script-setup.html#sfc-script-setup) to learn more.

## Recommended IDE Setup

- [VS Code](https://code.visualstudio.com/) + [Volar](https://marketplace.visualstudio.com/items?itemName=Vue.volar)

## Type Support For `.vue` Imports in TS

Since TypeScript cannot handle type information for `.vue` imports, they are shimmed to be a generic Vue component type
by default. In most cases this is fine if you don't really care about component prop types outside of templates.
However, if you wish to get actual prop types in `.vue` imports (for example to get props validation when using
manual `h(...)` calls), you can enable Volar's Take Over mode by following these steps:

1. Run `Extensions: Show Built-in Extensions` from VS Code's command palette, look
   for `TypeScript and JavaScript Language Features`, then right click and select `Disable (Workspace)`. By default,
   Take Over mode will enable itself if the default TypeScript extension is disabled.
2. Reload the VS Code window by running `Developer: Reload Window` from the command palette.

You can learn more about Take Over mode [here](https://github.com/johnsoncodehk/volar/discussions/471).

# desktop/frontend

Vue 3 + TypeScript + Naive UI + xterm frontend for the Wails desktop app, and (via the `platform/` adapter) the mobile Capacitor shell.

## Platform adapter

All Go-bound calls and Wails runtime calls route through `src/platform/`. Components do:

```ts
import { usePlatform } from '@/platform'
const platform = usePlatform()
await platform.relay.fetchMe()
platform.events.on('before-close', handler)
```

`platform/wails.ts` is the only file allowed to import from `../wailsjs/*` or `../lib/api`. To call a new Go method:

1. Add the `App.go` method on the Go side; let Wails regenerate `wailsjs/go/main/App.{js,d.ts}`.
2. Wrap it in `src/platform/wails.ts` on the appropriate `Bridge` (e.g. `RelayBridge`, `SessionBridge`).
3. If it represents new functionality, add the method to `src/platform/types.ts` first.
4. The Capacitor implementation (`platform/capacitor.ts`, PR-B onwards) decides whether to implement, no-op, or omit (optional method).

## Build targets

The same source builds for two targets, selected by `VITE_TARGET`:

- `npm run build:wails` (or the default `npm run build`) — `index.html` → `dist/`; consumed by `wails build`.
- `npm run build:capacitor` — `VITE_TARGET=capacitor`, builds `index.capacitor.html` → `dist-capacitor/index.html` (a Vite `generateBundle` hook renames the entry to `index.html` for Capacitor). `mobile/scripts/sync-web.mjs` syncs this into `mobile/www/`.

`src/main.ts` (Wails) mounts `App.vue` with `createWailsPlatform`. `src/main.capacitor.ts` (Capacitor) mounts `MobilePlaceholder.vue` with `createCapacitorPlatform`. The capacitor entry deliberately does NOT mount `App.vue` yet — `App.vue`'s `onMounted` calls Wails-only bindings; mounting the real mobile UI is PR-C. Vite tree-shakes the unused platform impl per target, so the capacitor bundle never pulls in `wailsjs/*`/`lib/api.ts`.

## Mobile boot state (PR-B)

The iOS Capacitor app boots into `MobilePlaceholder.vue`, confirming the bundle + `platform/` adapter load inside iOS WebView. Relay config + remote session UI land in PR-C.
