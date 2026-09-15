import { ref, type Ref } from "vue";

// One shared animation clock for every "running" indicator in the app — the
// per-session task-state icons and the titlebar sweep. PR #373 removed the
// per-icon CSS animations because each mounted copy scheduled its own repaint
// loop; on machines without GPU compositing (e.g. Linux with no accelerated
// canvas) software-rendered CSS animations run on the main thread once per
// copy per frame, and a list of running rows pinned the renderer CPU.
//
// This clock keeps that cost flat and bounded instead:
//   - a single requestAnimationFrame loop, shared by N subscribers;
//   - throttled to ~8fps (one phase step every STEP_MS) rather than repainting
//     on every frame;
//   - reference-counted, so the loop is fully cancelled the moment the last
//     running indicator unmounts — zero cost when nothing is running.
//
// Subscribers read `phase` (a monotonically increasing step counter) and
// derive whatever they need from it: the icon maps it to a rotation angle,
// the titlebar maps it to a background-position offset.

const STEP_MS = 125; // ~8fps

const phase = ref(0);

let refCount = 0;
let rafId: number | null = null;
let lastStepAt = 0;

function tick(now: number): void {
  if (now - lastStepAt >= STEP_MS) {
    lastStepAt = now;
    phase.value++;
  }
  // Keep scheduling only while someone still needs the clock.
  if (refCount > 0) {
    rafId = requestAnimationFrame(tick);
  }
}

function acquire(): void {
  refCount++;
  if (refCount === 1 && rafId === null) {
    lastStepAt = performance.now();
    rafId = requestAnimationFrame(tick);
  }
}

function release(): void {
  if (refCount === 0) return;
  refCount--;
  if (refCount === 0 && rafId !== null) {
    cancelAnimationFrame(rafId);
    rafId = null;
  }
}

export interface UseRunningSpin {
  // Monotonically increasing step counter, advanced ~8fps while acquired.
  phase: Ref<number>;
  // Reference-count the shared clock. Pair every acquire() with a release()
  // (typically onMounted / onUnmounted while a "running" state is shown).
  acquire(): void;
  release(): void;
}

export function useRunningSpin(): UseRunningSpin {
  return { phase, acquire, release };
}

// Test-only reset for the module-level singleton.
export function __resetForTests(): void {
  if (rafId !== null) {
    cancelAnimationFrame(rafId);
    rafId = null;
  }
  refCount = 0;
  lastStepAt = 0;
  phase.value = 0;
}
