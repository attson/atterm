import { t } from "../i18n";

/**
 * Formats a unix-seconds timestamp as a short relative "ago" string
 * (just now / N min ago / N h ago / N d ago). Shared by SettingsUpdates.vue
 * (last update check) and SyncStatusIndicator.vue (last sync) so the same
 * Settings dialog doesn't speak two different time dialects two tabs apart.
 *
 * `nowMs` is injectable (default `Date.now()`) so callers/tests can pin a
 * deterministic "now" instead of racing the real clock.
 */
export function formatAgo(unixSec: number, nowMs: number = Date.now()): string {
  const diffSec = Math.floor(nowMs / 1000) - unixSec;
  if (diffSec < 60) return t("settings.updates.justNow");
  if (diffSec < 3600) return t("settings.updates.minutesAgo", { count: Math.floor(diffSec / 60) });
  if (diffSec < 86400) return t("settings.updates.hoursAgo", { count: Math.floor(diffSec / 3600) });
  return t("settings.updates.daysAgo", { count: Math.floor(diffSec / 86400) });
}
