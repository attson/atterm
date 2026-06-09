import type { Terminal } from 'xterm'

export interface CellHit {
  col: number
  row: number
}

export interface CellSize {
  width: number
  height: number
}

// CellSizeReader lets callers (and tests) inject the cell-size source. Production
// callers pass `readXtermCellSize` (below); tests stub it.
export type CellSizeReader = (term: Terminal) => CellSize

// readXtermCellSize reads xterm's CSS-pixel cell size from its renderer's
// dimensions. xterm 5.x exposes this via _core._renderService.dimensions —
// an internal API. Falls back to a degraded `fontSize × lineHeight` estimate
// if the internal path is missing (e.g. before the renderer has measured).
export function readXtermCellSize(term: Terminal): CellSize {
  // Best path: live renderer dimensions.
  const dim = (term as unknown as {
    _core?: { _renderService?: { dimensions?: { css?: { cell?: { width?: number; height?: number } } } } }
  })._core?._renderService?.dimensions?.css?.cell
  if (dim && typeof dim.width === 'number' && typeof dim.height === 'number' && dim.width > 0 && dim.height > 0) {
    return { width: dim.width, height: dim.height }
  }
  // Fallback: estimate from font options. Mono fonts average ~0.6 width/height
  // ratio; xterm defaults lineHeight to 1.0.
  const fontSize = (term.options.fontSize ?? 12) as number
  const lineHeight = (term.options.lineHeight ?? 1.0) as number
  return { width: fontSize * 0.6, height: fontSize * lineHeight }
}

// cellCoordsAt converts viewport-relative client coords into the cell
// at that pixel inside the terminal's scrollback grid. Returns null when
// the coords fall outside the viewport.
export function cellCoordsAt(
  clientX: number,
  clientY: number,
  term: Terminal,
  viewport: HTMLElement,
  readSize: CellSizeReader = readXtermCellSize,
): CellHit | null {
  const rect = viewport.getBoundingClientRect()
  if (clientX < rect.left || clientX >= rect.right) return null
  if (clientY < rect.top || clientY >= rect.bottom) return null

  const { width: cw, height: ch } = readSize(term)
  if (cw <= 0 || ch <= 0) return null

  const localX = clientX - rect.left
  const localY = clientY - rect.top + (viewport.scrollTop ?? 0)
  const col = Math.floor(localX / cw)
  const row = Math.floor(localY / ch)
  return { col, row }
}
