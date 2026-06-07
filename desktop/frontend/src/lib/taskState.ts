export type TaskState =
  | "idle"
  | "running"
  | "waiting_input"
  | "completed"
  | "failed"
  | "disconnected"
  | "closed";

export type PresetId = "iconOnly" | "iconLabel";

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
  // When true, the sidebar row renders a short status-text label next to
  // the icon (Running / 等待输入 …). When false, only the colored glyph
  // shows. The two predefined presets differ only on this knob — they
  // share the same vivid palette and animation budget.
  showLabel: boolean;
}

const COLORS: Record<TaskState, string> = {
  idle: "#6b7280",
  running: "#06b6d4",
  waiting_input: "#f59e0b",
  completed: "#22c55e",
  failed: "#ef4444",
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

function makePreset(id: PresetId, showLabel: boolean): TaskStatePreset {
  return {
    id,
    i18nKey: `tasks.preset.${id}`,
    colorOf: (s) => COLORS[s],
    glyphOf: (s) => GLYPHS[s],
    spinnerDurationMs: (s) => (s === "running" ? 1500 : 0),
    animatePulse: (s) => s === "waiting_input",
    textOpacity: 1.0,
    showLabel,
  };
}

export const presets: Record<PresetId, TaskStatePreset> = {
  iconOnly: makePreset("iconOnly", false),
  iconLabel: makePreset("iconLabel", true),
};
