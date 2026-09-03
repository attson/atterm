export type TaskState =
  | "idle"
  | "running"
  | "waiting_input"
  | "completed"
  | "failed"
  | "disconnected"
  | "closed";

export type PresetId = "iconOnly" | "iconLabel";

export interface TaskStatePreset {
  id: PresetId;
  i18nKey: string; // tasks.preset.<id>
  colorOf(state: TaskState): string;
  textOpacity: number;
  // When true, the sidebar row renders a short status-text label next to
  // the icon (Running / 等待输入 …). When false, only the colored glyph
  // shows. The two predefined presets differ only on this knob — they
  // share the same vivid palette.
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

/**
 * Palette lookup for surfaces that tint a whole row rather than draw a glyph
 * (sidebar rows, tabs, widget rows). They need the raw colour to hand to CSS
 * as a custom property; going through a preset's colorOf would tie a row's
 * tint to whichever preset happens to be active, and the two presets differ
 * only by whether a text label shows.
 */
export function stateColor(state: TaskState): string {
  return COLORS[state];
}

// Glyph shape is not on the preset — TaskStateIcon.vue dispatches on
// state directly and draws inline SVG. Text-glyph fallback (unicode
// symbols like ◐ / ✓ / ✗) failed to render on iOS 26.3 as .notdef "?"
// boxes when the CJK-first font stack couldn't resolve them.

function makePreset(id: PresetId, showLabel: boolean): TaskStatePreset {
  return {
    id,
    i18nKey: `tasks.preset.${id}`,
    colorOf: (s) => COLORS[s],
    textOpacity: 1.0,
    showLabel,
  };
}

export const presets: Record<PresetId, TaskStatePreset> = {
  iconOnly: makePreset("iconOnly", false),
  iconLabel: makePreset("iconLabel", true),
};
