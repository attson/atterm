import { Terminal } from "xterm";
import { FitAddon } from "xterm-addon-fit";

import type { LayoutKind } from "./types";

// Offscreen xterm.js instance used to predict how many rows/cols a would-be
// pane will end up with, before spawning the shell that expects those
// dimensions. The alternative — spawn at 80×24 then let FitAddon resize —
// causes the shell to observe an early SIGWINCH and some interactive apps
// (fzf, less, tmux) redraw wrong on that first fit. Predicting up front
// lets NewSession send the correct cols/rows in its very first PTY frame.
//
// The probe DOM sits inside a 0×0 fixed host with `overflow:hidden` so it
// never leaks into `document.scrollWidth` — earlier `position:absolute;
// left:-99999px` placement caused WKWebView to paint root-level scrollbars
// underneath the real terminal.

let measureTerm: Terminal | null = null;
let measureFit: FitAddon | null = null;
let measureDiv: HTMLDivElement | null = null;
let measureHost: HTMLDivElement | null = null;

/** setupMeasureProbe mounts the offscreen xterm. Idempotent-ish: call
 *  teardown before re-mounting; a second call without teardown will leak
 *  the previous host. Resolves after one animation frame so the renderer
 *  has laid out the css cell size. */
export function setupMeasureProbe(): Promise<void> {
  return new Promise((resolve) => {
    measureHost = document.createElement("div");
    measureHost.style.cssText =
      "position:fixed;top:0;left:0;width:0;height:0;overflow:hidden;pointer-events:none;visibility:hidden;";
    measureDiv = document.createElement("div");
    measureDiv.style.cssText = "position:absolute;top:0;left:0;width:400px;height:300px;";
    measureHost.appendChild(measureDiv);
    document.body.appendChild(measureHost);
    measureTerm = new Terminal({
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
      fontSize: 13,
      allowProposedApi: true,
    });
    measureFit = new FitAddon();
    measureTerm.loadAddon(measureFit);
    measureTerm.open(measureDiv);
    // Renderer needs a frame to compute the css cell size.
    requestAnimationFrame(() => resolve());
  });
}

export function teardownMeasureProbe(): void {
  measureTerm?.dispose();
  if (measureHost && measureHost.parentElement) {
    measureHost.parentElement.removeChild(measureHost);
  }
  measureTerm = null;
  measureFit = null;
  measureDiv = null;
  measureHost = null;
}

// Predict what xterm.js's FitAddon will pick for a cell of the given px
// dimensions. Routes through the probe so the math is the same as the real
// fit() call — within a column.
function predictCellDimsForSize(width: number, height: number): { cols: number; rows: number } {
  if (!measureFit || !measureDiv) return { cols: 80, rows: 24 };
  measureDiv.style.width = `${Math.max(40, Math.floor(width))}px`;
  measureDiv.style.height = `${Math.max(40, Math.floor(height))}px`;
  // Force layout so proposeDimensions reads the new size.
  void measureDiv.offsetWidth;
  const dims = measureFit.proposeDimensions();
  if (!dims || !dims.cols || !dims.rows) return { cols: 80, rows: 24 };
  return { cols: dims.cols, rows: dims.rows };
}

/** predictCellDims returns the (cols, rows) a new pane in the given layout
 *  would receive after FitAddon runs. Falls back to 80×24 when the probe
 *  hasn't been set up or the main container isn't laid out yet.
 *
 *  PaneGrid uses a 2px gap between cells; this function accounts for it
 *  when splitting so a vertical / horizontal / 2×2 layout predicts the
 *  same size the FitAddon will actually pick on mount. */
export function predictCellDims(layout: LayoutKind): { cols: number; rows: number } {
  const main = document.querySelector(".main") as HTMLElement | null;
  if (!main || main.clientWidth < 100 || main.clientHeight < 100) {
    return { cols: 80, rows: 24 };
  }
  const colsDiv = layout === "vertical" || layout === "grid2x2" ? 2 : 1;
  const rowsDiv = layout === "horizontal" || layout === "grid2x2" ? 2 : 1;
  // PaneGrid has gap:2px between cells.
  const cellW = (main.clientWidth - (colsDiv - 1) * 2) / colsDiv;
  const cellH = (main.clientHeight - (rowsDiv - 1) * 2) / rowsDiv;
  return predictCellDimsForSize(cellW, cellH);
}
