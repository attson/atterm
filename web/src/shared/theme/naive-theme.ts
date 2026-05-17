import type { GlobalThemeOverrides } from 'naive-ui'

// readVar returns the trimmed value of a CSS custom property, or the
// supplied fallback if the variable is not defined on the document root.
function readVar(name: string, fallback: string): string {
  if (typeof document === 'undefined') return fallback
  const raw = getComputedStyle(document.documentElement).getPropertyValue(name)
  return raw.trim() || fallback
}

// getNaiveOverrides resolves the design tokens from tokens.css and
// maps them onto Naive UI's GlobalThemeOverrides shape. Called at
// app mount time by each entry's App.vue (added in PR-B onward).
export function getNaiveOverrides(): GlobalThemeOverrides {
  const bg = readVar('--bg', '#0b1020')
  const fg = readVar('--fg', '#e2e8f0')
  const fgDim = readVar('--fg-dim', '#94a3b8')
  const panel = readVar('--panel', '#0f172a')
  const border = readVar('--border', '#1e293b')
  const accent = readVar('--accent', '#60a5fa')
  const bad = readVar('--bad', '#f87171')

  return {
    common: {
      bodyColor: bg,
      textColorBase: fg,
      textColor1: fg,
      textColor2: fg,
      textColor3: fgDim,
      primaryColor: accent,
      primaryColorHover: accent,
      primaryColorPressed: accent,
      borderColor: border,
      cardColor: panel,
      errorColor: bad,
    },
  }
}
