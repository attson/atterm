import type { PetState } from "../lib/petState";

/**
 * Thin typed wrapper over the Wails runtime globals the companion window uses.
 *
 * It deliberately does NOT import from `wailsjs/` — those files are generated
 * by `wails build`, and the repo convention is that only `src/platform/*` may
 * import them. The pet is a standalone entry with four calls and one event, so
 * reaching for the globals keeps it self-contained and lets `npm run build`
 * succeed without a prior binding-generation pass.
 */

interface PetBridgeMethods {
  Activate(sessionId: string): Promise<void>;
  SetCollapsed(collapsed: boolean): Promise<void>;
  Peek(open: boolean): Promise<void>;
  ReportPosition(): Promise<void>;
  Mute(untilUnix: number): Promise<void>;
  Hide(): Promise<void>;
}

interface WailsRuntime {
  EventsOn(name: string, cb: (...data: unknown[]) => void): () => void;
}

/**
 * Read the Wails globals through a local cast rather than `declare global`:
 * lib/api/_bindings.ts already augments Window with `go.main.App`, and a
 * second augmentation of the same property is a type conflict even though the
 * pet entry never imports that module.
 */
interface PetWindow {
  go?: { main?: { PetBridge?: PetBridgeMethods } };
  runtime?: WailsRuntime;
}

function petWindow(): PetWindow {
  return window as unknown as PetWindow;
}

function methods(): PetBridgeMethods | null {
  return petWindow().go?.main?.PetBridge ?? null;
}

/**
 * Every call is fire-and-forget and swallows rejections on purpose: a failed
 * window resize must never break rendering, and there is no user-meaningful
 * recovery for "the IPC call didn't land".
 */
function call(fn: (m: PetBridgeMethods) => Promise<void>): void {
  const m = methods();
  if (!m) return;
  void fn(m).catch(() => {});
}

export const petBridge = {
  available(): boolean {
    return methods() !== null;
  },
  activate(sessionId: string): void {
    if (!sessionId) return;
    call((m) => m.Activate(sessionId));
  },
  setCollapsed(collapsed: boolean): void {
    call((m) => m.SetCollapsed(collapsed));
  },
  peek(open: boolean): void {
    call((m) => m.Peek(open));
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
};

export interface PetBootstrap {
  collapsed: boolean;
  x: number;
  y: number;
  locale: string;
}

/**
 * onPetState subscribes to parent-pushed snapshots. The Go side forwards the
 * raw JSON line so it never has to mirror PetState's shape; parsing happens
 * here, and a malformed line is dropped rather than blanking the window.
 */
export function onPetState(cb: (state: PetState) => void): void {
  petWindow().runtime?.EventsOn("pet:state", (...data: unknown[]) => {
    const raw = data[0];
    if (typeof raw !== "string") return;
    try {
      cb(JSON.parse(raw) as PetState);
    } catch {
      /* keep the last good render */
    }
  });
}

export function onPetBootstrap(cb: (boot: PetBootstrap) => void): void {
  petWindow().runtime?.EventsOn("pet:bootstrap", (...data: unknown[]) => {
    const raw = data[0];
    if (raw && typeof raw === "object") cb(raw as PetBootstrap);
  });
}
