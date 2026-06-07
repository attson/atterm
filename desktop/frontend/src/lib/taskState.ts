export type TaskState =
  | "idle"
  | "running"
  | "waiting_input"
  | "completed"
  | "failed"
  | "disconnected"
  | "closed";

export type PresetId = "vivid" | "quiet";

export const ALL_TASK_STATES: readonly TaskState[] = [
  "idle",
  "running",
  "waiting_input",
  "completed",
  "failed",
  "disconnected",
  "closed",
] as const;

export interface TaskStatePreset {
  id: PresetId;
  i18nKey: string; // tasks.preset.<id>
  colorOf(state: TaskState): string;
  glyphOf(state: TaskState): "spinner" | string;
  spinnerDurationMs(state: TaskState): number;
  animatePulse(state: TaskState): boolean;
  textOpacity: number;
}

const VIVID_COLORS: Record<TaskState, string> = {
  idle: "#6b7280",
  running: "#06b6d4",
  waiting_input: "#f59e0b",
  completed: "#22c55e",
  failed: "#ef4444",
  disconnected: "#6b7280",
  closed: "#6b7280",
};

const QUIET_COLORS: Record<TaskState, string> = {
  idle: "#6b7280",
  running: "#4b8a93",
  waiting_input: "#b88239",
  completed: "#4a8b6a",
  failed: "#a04b4b",
  disconnected: "#6b7280",
  closed: "#6b7280",
};

const GLYPHS: Record<TaskState, "spinner" | string> = {
  idle: "·",
  running: "spinner",
  waiting_input: "◐",
  completed: "✓",
  failed: "✗",
  disconnected: "·",
  closed: "·",
};

function makePreset(
  id: PresetId,
  colors: Record<TaskState, string>,
  spinDuration: number,
  pulseWaiting: boolean,
  textOpacity: number,
): TaskStatePreset {
  return {
    id,
    i18nKey: `tasks.preset.${id}`,
    colorOf: (s) => colors[s],
    glyphOf: (s) => GLYPHS[s],
    spinnerDurationMs: (s) => (s === "running" ? spinDuration : 0),
    animatePulse: (s) => pulseWaiting && s === "waiting_input",
    textOpacity,
  };
}

export const presets: Record<PresetId, TaskStatePreset> = {
  vivid: makePreset("vivid", VIVID_COLORS, 1500, true, 1.0),
  quiet: makePreset("quiet", QUIET_COLORS, 2500, false, 0.75),
};
