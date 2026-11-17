import { ref, type Ref } from "vue";
import type { SessionLabelTone } from "../lib/sessionLabel";

export type SessionLabelMode = "neutral" | "category";
export type SessionLabelColorKey = Exclude<SessionLabelTone, "neutral">;
export type SessionLabelColors = Record<SessionLabelColorKey, string>;

export const SESSION_LABEL_APPEARANCE_STORAGE_KEY = "atterm.session-label-appearance.v1";
export const DEFAULT_SESSION_LABEL_COLORS: Readonly<SessionLabelColors> = Object.freeze({
  ssh: "#58A6FF",
  codex: "#3FB950",
  claude: "#D29922",
});

interface StoredAppearance {
  mode: SessionLabelMode;
  colors: SessionLabelColors;
}

const mode = ref<SessionLabelMode>("category");
const colors = ref<SessionLabelColors>({ ...DEFAULT_SESSION_LABEL_COLORS });
const saveError = ref(false);
let loaded = false;

function normalizedColor(value: unknown, fallback: string): string {
  return typeof value === "string" && /^#[0-9a-f]{6}$/i.test(value)
    ? value.toUpperCase()
    : fallback;
}

function normalize(value: unknown): StoredAppearance {
  const candidate = value && typeof value === "object" ? value as Record<string, unknown> : {};
  const candidateColors = candidate.colors && typeof candidate.colors === "object"
    ? candidate.colors as Record<string, unknown>
    : {};
  return {
    mode: candidate.mode === "neutral" || candidate.mode === "category" ? candidate.mode : "category",
    colors: {
      ssh: normalizedColor(candidateColors.ssh, DEFAULT_SESSION_LABEL_COLORS.ssh),
      codex: normalizedColor(candidateColors.codex, DEFAULT_SESSION_LABEL_COLORS.codex),
      claude: normalizedColor(candidateColors.claude, DEFAULT_SESSION_LABEL_COLORS.claude),
    },
  };
}

function load(): void {
  if (loaded) return;
  loaded = true;
  try {
    const raw = globalThis.localStorage?.getItem(SESSION_LABEL_APPEARANCE_STORAGE_KEY);
    if (!raw) return;
    const stored = normalize(JSON.parse(raw));
    mode.value = stored.mode;
    colors.value = stored.colors;
  } catch {
    // A blocked storage API or malformed value leaves the documented defaults.
  }
}

function persist(next: StoredAppearance): boolean {
  try {
    globalThis.localStorage?.setItem(SESSION_LABEL_APPEARANCE_STORAGE_KEY, JSON.stringify(next));
  } catch {
    saveError.value = true;
    return false;
  }
  mode.value = next.mode;
  colors.value = next.colors;
  saveError.value = false;
  return true;
}

function setMode(nextMode: SessionLabelMode): boolean {
  return persist({ mode: nextMode, colors: { ...colors.value } });
}

function setColor(key: SessionLabelColorKey, value: string): boolean {
  if (!/^#[0-9a-f]{6}$/i.test(value)) return false;
  return persist({
    mode: mode.value,
    colors: { ...colors.value, [key]: value.toUpperCase() },
  });
}

function resetColor(key: SessionLabelColorKey): boolean {
  return setColor(key, DEFAULT_SESSION_LABEL_COLORS[key]);
}

function resetColors(): boolean {
  return persist({ mode: mode.value, colors: { ...DEFAULT_SESSION_LABEL_COLORS } });
}

function colorFor(tone: SessionLabelTone): string {
  if (mode.value !== "category" || tone === "neutral") return "";
  return colors.value[tone];
}

export interface SessionLabelAppearance {
  mode: Ref<SessionLabelMode>;
  colors: Ref<SessionLabelColors>;
  saveError: Ref<boolean>;
  setMode: typeof setMode;
  setColor: typeof setColor;
  resetColor: typeof resetColor;
  resetColors: typeof resetColors;
  colorFor: typeof colorFor;
}

export function useSessionLabelAppearance(): SessionLabelAppearance {
  load();
  return { mode, colors, saveError, setMode, setColor, resetColor, resetColors, colorFor };
}

export function __resetForTests(): void {
  loaded = false;
  mode.value = "category";
  colors.value = { ...DEFAULT_SESSION_LABEL_COLORS };
  saveError.value = false;
}
