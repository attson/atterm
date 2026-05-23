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

- `npm run build` — builds for the Wails desktop target (default, current behaviour).
- `npm run build:capacitor` (added in PR-B) — builds for Capacitor mobile.

PR-A only delivers the adapter layer + Wails implementation; Capacitor build lands in PR-B.
