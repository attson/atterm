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
  ReportPosition(): Promise<void>;
  Mute(untilUnix: number): Promise<void>;
  Hide(): Promise<void>;
  Resize(height: number): Promise<void>;
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

/**
 * Every call is fire-and-forget and swallows rejections on purpose: a failed
 * window resize must never break rendering, and there is no user-meaningful
 * recovery for "the IPC call didn't land".
 */
function call(fn: (m: WidgetBridgeMethods) => Promise<void>): void {
  const m = methods();
  if (!m) return;
  void fn(m).catch(() => {});
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
  /**
   * Report the rendered card height so the OS window matches it exactly.
   * The window used to use hardcoded heights, which clipped the card's bottom
   * edge (rounded corner included) whenever the guess was short — and any
   * constant is wrong for most states, since height varies with row count,
   * font and locale.
   */
  resize(height: number): void {
    call((m) => m.Resize(height));
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
