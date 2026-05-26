import { t } from "../i18n";

export interface ReplayProgress {
  phase: "start" | "chunk" | "end";
  bytes: number;
  total_bytes: number;
  seq?: number;
}
function formatBytes(value: number): string {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}

export function progressPercent(progress: ReplayProgress): number {
  if (progress.total_bytes <= 0) return 0;
  return Math.max(0, Math.min(100, Math.round((progress.bytes / progress.total_bytes) * 100)));
}

export function formatReplayProgress(progress: ReplayProgress): string {
  if (progress.total_bytes <= 0) {
    return t("terminal.loadingHistoryBytes", { bytes: formatBytes(progress.bytes) });
  }
  return t("terminal.loadingHistoryProgress", {
    pct: progressPercent(progress),
    bytes: formatBytes(progress.bytes),
    total: formatBytes(progress.total_bytes),
  });
}
