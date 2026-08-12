import type { TaskState } from "./taskState";

export const LAST_OUTPUT_LIVE_WINDOW_MS = 5_000;

export interface LastOutputDisplay {
  text: string;
  live: boolean;
  title: string;
}

/**
 * Formats the wire's unix-seconds last_output_at for compact session rows.
 * Keep every threshold here so the sidebar and desk widget cannot drift.
 */
export function formatLastOutput(
  lastOutputAt: number | undefined,
  taskState: TaskState | string | undefined,
  nowMs: number,
): LastOutputDisplay | null {
  if (!lastOutputAt || !Number.isFinite(lastOutputAt) || lastOutputAt <= 0) return null;

  const rawElapsedMs = nowMs - lastOutputAt * 1000;
  const elapsedMs = Math.max(0, rawElapsedMs);
  // A future timestamp is clock skew, not evidence that bytes are currently
  // flowing. Render it as now but never promote it to live.
  if (rawElapsedMs >= 0 && taskState === "running" && elapsedMs <= LAST_OUTPUT_LIVE_WINDOW_MS) {
    return { text: "live", live: true, title: "Output active" };
  }

  const elapsedSeconds = Math.floor(elapsedMs / 1000);
  if (elapsedSeconds < 60) {
    return { text: "now", live: false, title: "Last output just now" };
  }
  if (elapsedSeconds < 60 * 60) {
    const text = `${Math.floor(elapsedSeconds / 60)}m`;
    return { text, live: false, title: `Last output ${text} ago` };
  }
  if (elapsedSeconds < 24 * 60 * 60) {
    const text = `${Math.floor(elapsedSeconds / (60 * 60))}h`;
    return { text, live: false, title: `Last output ${text} ago` };
  }
  const text = `${Math.floor(elapsedSeconds / (24 * 60 * 60))}d`;
  return { text, live: false, title: `Last output ${text} ago` };
}
