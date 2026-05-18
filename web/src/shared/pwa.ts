// shared/pwa.ts — sole entry that imports the virtual:pwa-register module.
// Imported as a side-effect from each MPA entry's main.ts so the generated
// service worker registers exactly once per page load. Skips on engines
// without ServiceWorker support (older iOS, locked-down WebView, etc.).

import { registerSW } from 'virtual:pwa-register'

if (typeof navigator !== 'undefined' && 'serviceWorker' in navigator) {
  registerSW({ immediate: true })
}
