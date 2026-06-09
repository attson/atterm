import { describe, it, expect, vi } from 'vitest'
import { cellCoordsAt, readXtermCellSize, type CellSizeReader } from './terminalCellCoords'

// Build a fake viewport element with a known bounding rect and scrollTop.
function viewport({ x, y, w, h, scrollTop = 0 }: { x: number; y: number; w: number; h: number; scrollTop?: number }) {
  const el = document.createElement('div')
  el.getBoundingClientRect = () => ({ x, y, left: x, top: y, width: w, height: h, right: x + w, bottom: y + h, toJSON() { return this } } as DOMRect)
  Object.defineProperty(el, 'scrollTop', { value: scrollTop, configurable: true })
  return el
}

const cellReader: CellSizeReader = () => ({ width: 8, height: 16 })
const term = { cols: 80, rows: 24 } as any

describe('cellCoordsAt', () => {
  it('maps top-left pixel to (0,0)', () => {
    const vp = viewport({ x: 0, y: 0, w: 640, h: 384 })
    expect(cellCoordsAt(4, 8, term, vp, cellReader)).toEqual({ col: 0, row: 0 })
  })
  it('maps pixel inside cell (3,2)', () => {
    const vp = viewport({ x: 0, y: 0, w: 640, h: 384 })
    expect(cellCoordsAt(28, 40, term, vp, cellReader)).toEqual({ col: 3, row: 2 })
  })
  it('accounts for viewport scrollTop in the row calculation', () => {
    const vp = viewport({ x: 0, y: 0, w: 640, h: 384, scrollTop: 160 })  // 10 rows scrolled
    expect(cellCoordsAt(4, 8, term, vp, cellReader)).toEqual({ col: 0, row: 10 })
  })
  it('returns null when clientX is past the right edge', () => {
    const vp = viewport({ x: 0, y: 0, w: 640, h: 384 })
    expect(cellCoordsAt(900, 40, term, vp, cellReader)).toBeNull()
  })
  it('returns null when clientY is below the bottom edge', () => {
    const vp = viewport({ x: 0, y: 0, w: 640, h: 384 })
    expect(cellCoordsAt(28, 700, term, vp, cellReader)).toBeNull()
  })
  it('returns null when coords are above/left of the viewport', () => {
    const vp = viewport({ x: 100, y: 100, w: 640, h: 384 })
    expect(cellCoordsAt(50, 50, term, vp, cellReader)).toBeNull()
  })
  it('respects a viewport offset within the page', () => {
    const vp = viewport({ x: 100, y: 200, w: 640, h: 384 })
    // pixel (108, 216) is (cellX 1, cellY 1) → col 1, row 1
    expect(cellCoordsAt(108, 216, term, vp, cellReader)).toEqual({ col: 1, row: 1 })
  })
  it('returns null at the exact right edge (half-open interval)', () => {
    const vp = viewport({ x: 0, y: 0, w: 640, h: 384 })
    expect(cellCoordsAt(640, 40, term, vp, cellReader)).toBeNull()
  })
  it('returns null when the cell-size reader gives a zero width', () => {
    const vp = viewport({ x: 0, y: 0, w: 640, h: 384 })
    const zeroReader: CellSizeReader = () => ({ width: 0, height: 16 })
    expect(cellCoordsAt(28, 40, term, vp, zeroReader)).toBeNull()
  })
  it('readXtermCellSize warns once per term when the internal API is unavailable', () => {
    const fakeTerm = { options: { fontSize: 15, lineHeight: 1.0 } } as any
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    readXtermCellSize(fakeTerm)
    readXtermCellSize(fakeTerm)
    readXtermCellSize(fakeTerm)
    expect(warn).toHaveBeenCalledTimes(1)
    expect(warn.mock.calls[0][0]).toMatch(/xterm renderer dimensions unavailable/)
    warn.mockRestore()
  })
})
