// Shared display metadata for session.type ("shell" | "ai" | "test" | "build"
// | "deploy"). Renderers call displayForType() and decide their own layout.
//
// SVG path strings are 16×16 viewBox, single-path, stroke="currentColor"
// stroke-width="1.6" linecap="round" linejoin="round" — match the lucide
// look used elsewhere in the app. iconPath is the `d` attribute only;
// renderers wrap it in <svg viewBox="0 0 16 16"><path d=... /></svg>.

export type DisplayKey = 'ai' | 'test' | 'build' | 'deploy'

export interface TypeDisplay {
  key: DisplayKey
  color: string
  iconPath: string
}

const TABLE: Record<DisplayKey, TypeDisplay> = {
  ai: {
    key: 'ai',
    color: '#a78bfa',
    // 4-pointed sparkle.
    iconPath: 'M8 2v3M8 11v3M2 8h3M11 8h3M3.5 3.5l2 2M10.5 10.5l2 2M3.5 12.5l2-2M10.5 5.5l2-2',
  },
  test: {
    key: 'test',
    color: '#34d399',
    // Conical flask outline.
    iconPath: 'M6 2h4M7 2v4l-4 8h10l-4-8V2',
  },
  build: {
    key: 'build',
    color: '#fbbf24',
    // Stacked box (package).
    iconPath: 'M2 5l6-3 6 3v6l-6 3-6-3V5zM2 5l6 3 6-3M8 8v6',
  },
  deploy: {
    key: 'deploy',
    color: '#f87171',
    // Up arrow into a cloud-like cap.
    iconPath: 'M8 13V4M3 8l5-5 5 5M3 13h10',
  },
}

export function displayForType(t: string | undefined | null): TypeDisplay | null {
  if (!t || t === 'shell') return null
  return (TABLE as Record<string, TypeDisplay>)[t] ?? null
}
