import { createApp, type App as VueApp } from "vue";
import { createPinia } from "pinia";

import App from "../App.vue";
import { initI18n, type LocalePreference } from "../i18n";
import { bridgeSharedI18n } from "../i18n/shared-bridge";
import { initPlatform, type Platform } from "../platform";
import { installLogFlushHandlers, logInfo } from "./log";

/**
 * bootstrapApp is the shared boot skeleton for the three entry points
 * (main.ts Wails desktop, main.web.ts browser build, main.capacitor.ts
 * iOS shell). Each one used to open-code the same six steps:
 *
 *   1. await initI18n({ loadPreference, savePreference })
 *   2. bridgeSharedI18n()
 *   3. Platform-specific pre-mount setup (prefsSync engine, iOS keyboard
 *      accessory bar, ...)
 *   4. initPlatform(<factory>)
 *   5. createApp(App).use(Pinia).provide('platform').mount('#app')
 *   6. Platform-specific post-mount side effects (Wails EventsOn wire)
 *
 * The three files drifted over time — capacitor added its own locale
 * storage key, web added prefsSync engine wiring, wails added an
 * EventsOn hook — even though the middle three steps are identical.
 * Fold the common path here and let each entry supply only the parts
 * that actually differ, as `i18n` / `createPlatform` / `beforeMount`
 * / `afterMount`.
 */
export interface BootstrapAppOptions {
  /** Locale load/save hooks handed to initI18n. Different per entry:
   *  Wails proxies through the Go bindings, web reads localStorage
   *  directly, capacitor reads localStorage with an iOS-safe try/catch. */
  i18n: {
    loadPreference: () => Promise<unknown>;
    savePreference: (p: LocalePreference) => Promise<void>;
  };
  /** Platform factory: createWailsPlatform / createWebPlatform /
   *  createCapacitorPlatform. */
  createPlatform: () => Platform;
  /** Runs after platform init, before the Vue app mounts. Good spot for
   *  wiring prefsSync engines or one-shot platform toggles that need
   *  the platform object but must run before component setup. */
  beforeMount?: (platform: Platform) => void | Promise<void>;
  /** Runs after mount. Hook for post-mount side effects like Wails
   *  EventsOn('prefs:changed') listeners. */
  afterMount?: (platform: Platform, app: VueApp<Element>) => void | Promise<void>;
}

export async function bootstrapApp(opts: BootstrapAppOptions): Promise<void> {
  // First thing, so a failure in any step below is still flushed to the log
  // file when the window goes away. Renderer logs are batched, and the boot
  // path is exactly where losing the last batch hurts most.
  installLogFlushHandlers();

  await initI18n(opts.i18n);
  // @shared/i18n powers components under `@shared/*` (admin panel, shared
  // Topbar). Mirror the desktop-local locale into it so those components
  // render in the user's chosen language, not the shared-i18n default 'en'.
  bridgeSharedI18n();

  const platform = initPlatform(opts.createPlatform);
  await opts.beforeMount?.(platform);

  const app = createApp(App);
  app.use(createPinia());
  app.provide("platform", platform);
  app.config.globalProperties.$platform = platform;
  app.mount("#app");

  await opts.afterMount?.(platform, app);

  // A heartbeat on the happy path. Every other renderer log record sits on a
  // failure branch, so an empty `ui-*` section in the log file is ambiguous:
  // it reads the same whether nothing went wrong or the bridge to the Go
  // logger is broken. One line per boot makes the difference observable —
  // and marks where each run of the window begins.
  logInfo("boot", "renderer ready", {
    href: typeof location === "undefined" ? "" : location.pathname,
  });
}
