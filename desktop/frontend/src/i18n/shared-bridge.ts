import { watch } from "vue";
import {
  initI18n as initSharedI18n,
  setLocalePreference as setSharedLocalePreference,
} from "@shared/i18n";
import { localePreference as localLocalePreference } from "./index";

// Bridges the desktop-local i18n instance (desktop/frontend/src/i18n) with the
// shared web instance (web/src/shared/i18n). The two live in separate module
// graphs — components under `@shared/*` (admin panel, shared Topbar, etc.)
// read `@shared/i18n`'s reactive locale refs, while the rest of the desktop
// UI reads the desktop-local ones.
//
// Without this bridge, `@shared/i18n`'s `resolvedLocale` stays at its default
// `'en'` forever and the admin panel renders in English regardless of the
// user's saved locale. The bridge:
//   1. Calls shared `initI18n()` so shared attaches its `languagechange`
//      listener and picks up any localStorage-backed preference on load.
//   2. Immediately overrides shared with the authoritative desktop-local
//      value (Wails stores locale via a Go binding, not localStorage — so the
//      shared init's localStorage read would be stale/missing on wails).
//   3. Watches the desktop-local `localePreference` ref and mirrors every
//      subsequent change into shared so both trees update reactively.
//
// Must be called AFTER the desktop-local `initI18n({ loadPreference, ... })`
// completes so `localLocalePreference.value` reflects the user's choice.
export function bridgeSharedI18n(): void {
  initSharedI18n();
  setSharedLocalePreference(localLocalePreference.value);
  watch(localLocalePreference, (next) => {
    setSharedLocalePreference(next);
  });
}
