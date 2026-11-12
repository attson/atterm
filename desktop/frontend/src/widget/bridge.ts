import type { WidgetState } from "../lib/widgetState";

/**
 * Thin typed wrapper over the Wails runtime globals the companion window uses.
 *
 * It deliberately does NOT import from `wailsjs/` — those files are generated
 * by `wails build`, and the repo convention is that only `src/platform/*` may
 * import them. The widget is a standalone entry with four calls and one event, so
 * reaching for the globals keeps it self-contained and lets `npm run build`
 * succeed without a prior binding-generation pass.
 */

interface WidgetBridgeMethods {
  Activate(sessionId: string): Promise<void>;
  SetCollapsed(collapsed: boolean): Promise<void>;
  SetMinimized(minimized: boolean): Promise<void>;
  ReportPosition(): Promise<void>;
  Mute(untilUnix: number): Promise<void>;
  Hide(): Promise<void>;
  Resize(width: number, height: number): Promise<void>;
  Ready(): Promise<void>;
  SetAIOnly(aiOnly: boolean): Promise<void>;
}

interface WailsRuntime {
  EventsOn(name: string, cb: (...data: unknown[]) => void): () => void;
}

/**
 * Read the Wails globals through a local cast rather than `declare global`:
 * lib/api/_bindings.ts already augments Window with `go.main.App`, and a
 * second augmentation of the same property is a type conflict even though the
 * widget entry never imports that module.
 */
interface WidgetWindow {
  go?: { main?: { WidgetBridge?: WidgetBridgeMethods } };
  runtime?: WailsRuntime;
}

function widgetWindow(): WidgetWindow {
  return window as unknown as WidgetWindow;
}

function methods(): WidgetBridgeMethods | null {
  return widgetWindow().go?.main?.WidgetBridge ?? null;
}

/** Most calls are fire-and-forget and swallow rejections on purpose. */
function call(fn: (m: WidgetBridgeMethods) => Promise<void>): void {
  const m = methods();
  if (!m) return;
  void fn(m).catch(() => {});
}

function callAndWait(fn: (m: WidgetBridgeMethods) => Promise<void>): Promise<void> {
  const m = methods();
  if (!m) return Promise.resolve();
  return fn(m).catch(() => {});
}

export const widgetBridge = {
  available(): boolean {
    return methods() !== null;
  },
  /**
   * Signal that the window has mounted and subscribed. Wails drops events
   * emitted before this point, and the parent writes bootstrap + the first
   * state snapshot immediately after spawn — so Go parks them until now.
   */
  ready(): void {
    call((m) => m.Ready());
  },
  activate(sessionId: string): void {
    if (!sessionId) return;
    call((m) => m.Activate(sessionId));
  },
  setCollapsed(collapsed: boolean): void {
    call((m) => m.SetCollapsed(collapsed));
  },
  setMinimized(minimized: boolean): void {
    call((m) => m.SetMinimized(minimized));
  },
  /**
   * Report the rendered widget dimensions so the OS window matches it exactly.
   * Height varies with content, while icon-only mode also changes the width.
   * Measuring both prevents transparent native-window space from intercepting
   * clicks outside the visible card.
   */
  resize(width: number, height: number): Promise<void> {
    return callAndWait((m) => m.Resize(width, height));
  },
  reportPosition(): void {
    call((m) => m.ReportPosition());
  },
  mute(untilUnix: number): void {
    call((m) => m.Mute(untilUnix));
  },
  hide(): void {
    call((m) => m.Hide());
  },
  setAiOnly(aiOnly: boolean): void {
    call((m) => m.SetAIOnly(aiOnly));
  },
};

export interface WidgetBootstrap {
  collapsed: boolean;
  minimized: boolean;
  x: number;
  y: number;
  locale: string;
}

/**
 * onWidgetState subscribes to parent-pushed snapshots. The Go side forwards the
 * raw JSON line so it never has to mirror WidgetState's shape; parsing happens
 * here, and a malformed line is dropped rather than blanking the window.
 */
export function onWidgetState(cb: (state: WidgetState) => void): void {
  widgetWindow().runtime?.EventsOn("widget:state", (...data: unknown[]) => {
    const raw = data[0];
    if (typeof raw !== "string") return;
    try {
      cb(JSON.parse(raw) as WidgetState);
    } catch {
      /* keep the last good render */
    }
  });
}

export function onWidgetBootstrap(cb: (boot: WidgetBootstrap) => void): void {
  widgetWindow().runtime?.EventsOn("widget:bootstrap", (...data: unknown[]) => {
    const raw = data[0];
    if (raw && typeof raw === "object") cb(raw as WidgetBootstrap);
  });
}
