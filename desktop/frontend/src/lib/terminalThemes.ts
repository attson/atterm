import type { ITheme } from "xterm";
import type { MessageKey } from "../i18n";

export type TerminalThemeID = "classic" | "nord" | "solarized-dark" | "daylight";

export interface TerminalThemeDefinition {
  id: TerminalThemeID;
  labelKey: MessageKey;
  descriptionKey: MessageKey;
  xtermTheme: ITheme;
  appVars: Record<string, string>;
}

export const DEFAULT_TERMINAL_THEME_ID: TerminalThemeID = "classic";

export const TERMINAL_THEMES: TerminalThemeDefinition[] = [
  {
    id: "classic",
    labelKey: "settings.general.themes.classicLabel",
    descriptionKey: "settings.general.themes.classicDescription",
    xtermTheme: {
      background: "#000000",
      foreground: "#c9d1d9",
      cursor: "#c9d1d9",
      cursorAccent: "#000000",
      selectionBackground: "#264f78",
      black: "#000000",
      red: "#f85149",
      green: "#3fb950",
      yellow: "#d29922",
      blue: "#58a6ff",
      magenta: "#bc8cff",
      cyan: "#39c5cf",
      white: "#b1bac4",
      brightBlack: "#6e7681",
      brightRed: "#ff7b72",
      brightGreen: "#56d364",
      brightYellow: "#e3b341",
      brightBlue: "#79c0ff",
      brightMagenta: "#d2a8ff",
      brightCyan: "#56d4dd",
      brightWhite: "#f0f6fc",
    },
    appVars: {
      "--bg": "#0d1117",
      "--panel": "#161b22",
      "--border": "#30363d",
      "--fg": "#c9d1d9",
      "--fg-dim": "#8b949e",
      "--accent": "#58a6ff",
      "--good": "#3fb950",
      "--bad": "#f85149",
      "--warn": "#d29922",
      "--neutral": "#6e7681",
      "--terminal-bg": "#000000",
      "--terminal-grid": "#11161d",
      "--terminal-overlay": "rgba(13, 17, 23, 0.85)",
    },
  },
  {
    id: "nord",
    labelKey: "settings.general.themes.nordLabel",
    descriptionKey: "settings.general.themes.nordDescription",
    xtermTheme: {
      background: "#2e3440",
      foreground: "#d8dee9",
      cursor: "#d8dee9",
      cursorAccent: "#2e3440",
      selectionBackground: "#434c5e",
      black: "#3b4252",
      red: "#bf616a",
      green: "#a3be8c",
      yellow: "#ebcb8b",
      blue: "#81a1c1",
      magenta: "#b48ead",
      cyan: "#88c0d0",
      white: "#e5e9f0",
      brightBlack: "#4c566a",
      brightRed: "#bf616a",
      brightGreen: "#a3be8c",
      brightYellow: "#ebcb8b",
      brightBlue: "#81a1c1",
      brightMagenta: "#b48ead",
      brightCyan: "#8fbcbb",
      brightWhite: "#eceff4",
    },
    appVars: {
      "--bg": "#242933",
      "--panel": "#2e3440",
      "--border": "#4c566a",
      "--fg": "#d8dee9",
      "--fg-dim": "#9aa7bd",
      "--accent": "#88c0d0",
      "--good": "#a3be8c",
      "--bad": "#bf616a",
      "--warn": "#ebcb8b",
      "--neutral": "#4c566a",
      "--terminal-bg": "#2e3440",
      "--terminal-grid": "#242933",
      "--terminal-overlay": "rgba(36, 41, 51, 0.88)",
    },
  },
  {
    id: "solarized-dark",
    labelKey: "settings.general.themes.solarizedDarkLabel",
    descriptionKey: "settings.general.themes.solarizedDarkDescription",
    xtermTheme: {
      background: "#002b36",
      foreground: "#839496",
      cursor: "#93a1a1",
      cursorAccent: "#002b36",
      selectionBackground: "#073642",
      black: "#073642",
      red: "#dc322f",
      green: "#859900",
      yellow: "#b58900",
      blue: "#268bd2",
      magenta: "#d33682",
      cyan: "#2aa198",
      white: "#eee8d5",
      brightBlack: "#002b36",
      brightRed: "#cb4b16",
      brightGreen: "#586e75",
      brightYellow: "#657b83",
      brightBlue: "#839496",
      brightMagenta: "#6c71c4",
      brightCyan: "#93a1a1",
      brightWhite: "#fdf6e3",
    },
    appVars: {
      "--bg": "#00212b",
      "--panel": "#073642",
      "--border": "#28535f",
      "--fg": "#93a1a1",
      "--fg-dim": "#657b83",
      "--accent": "#268bd2",
      "--good": "#859900",
      "--bad": "#dc322f",
      "--warn": "#b58900",
      "--neutral": "#586e75",
      "--terminal-bg": "#002b36",
      "--terminal-grid": "#001e26",
      "--terminal-overlay": "rgba(0, 33, 43, 0.9)",
    },
  },
  {
    id: "daylight",
    labelKey: "settings.general.themes.daylightLabel",
    descriptionKey: "settings.general.themes.daylightDescription",
    xtermTheme: {
      background: "#faf4e8",
      foreground: "#403832",
      cursor: "#403832",
      cursorAccent: "#faf4e8",
      selectionBackground: "#d9c7a8",
      black: "#403832",
      red: "#c94f4f",
      green: "#4d8a3f",
      yellow: "#b9831d",
      blue: "#2f6db3",
      magenta: "#8b5fa8",
      cyan: "#2f8f8a",
      white: "#ede0cc",
      brightBlack: "#7a6d61",
      brightRed: "#d56565",
      brightGreen: "#5d9f4d",
      brightYellow: "#c9922c",
      brightBlue: "#3d7fcf",
      brightMagenta: "#9b6db8",
      brightCyan: "#3aa39d",
      brightWhite: "#fffaf0",
    },
    appVars: {
      "--bg": "#f4ead8",
      "--panel": "#fff8ed",
      "--border": "#d8c7ad",
      "--fg": "#403832",
      "--fg-dim": "#776b5f",
      "--accent": "#2f6db3",
      "--good": "#4d8a3f",
      "--bad": "#c94f4f",
      "--warn": "#b9831d",
      "--neutral": "#7a6d61",
      "--terminal-bg": "#faf4e8",
      "--terminal-grid": "#e5d6bd",
      "--terminal-overlay": "rgba(255, 248, 237, 0.92)",
    },
  },
];

export function getTerminalTheme(id: string): TerminalThemeDefinition {
  return TERMINAL_THEMES.find((theme) => theme.id === id) ?? TERMINAL_THEMES[0];
}

// isLightTerminalTheme returns true when the active terminal palette has a
// light background. Plugins (File Explorer) use this to pick a matching
// light/dimmed shell.
export function isLightTerminalTheme(id: string): boolean {
  return id === "daylight";
}
