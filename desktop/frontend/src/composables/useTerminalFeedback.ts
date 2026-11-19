import { computed, onScopeDispose, ref, watch, type Ref } from "vue";
import type { Status } from "../lib/connection";
import type { ReplayProgress } from "../lib/replayProgress";

/** Socket readiness, replay receipt, and xterm rendering are separate milestones. */
export function useTerminalFeedback(status: Ref<Status>, onTakeoverTimeout: () => void, waitForOutput = false) {
  const replayProgress = ref<ReplayProgress | null>(null);
  const renderingReplay = ref(false);
  const awaitingReplay = ref(true);
  const hasRendered = ref(false);
  const hasOutput = ref(false);
  const takeoverPending = ref(false);
  const takeoverQueued = ref(false);
  let replayGeneration = 0;
  let disposed = false;
  let outputDrainPending = false;
  let takeoverTimer: ReturnType<typeof setTimeout> | undefined;

  const waitingForOutput = computed(() => waitForOutput && !hasOutput.value && !awaitingReplay.value);
  const loading = computed(() => status.value !== "ended" && status.value !== "error"
    && (status.value !== "attached" || awaitingReplay.value || waitingForOutput.value));
  const initialLoading = computed(() => !hasRendered.value && loading.value);

  function clearTakeover() {
    clearTimeout(takeoverTimer);
    takeoverTimer = undefined;
    takeoverPending.value = false;
    takeoverQueued.value = false;
  }

  watch(status, (value) => {
    if (value === "attached") return;
    replayGeneration++;
    replayProgress.value = null;
    renderingReplay.value = false;
    awaitingReplay.value = !hasRendered.value;
    if (value === "ended" || value === "error") clearTakeover();
  }, { flush: "sync" });

  // The returned completion belongs to this replay only: a late xterm drain
  // from a disconnected route must not dismiss the next connection's loading.
  function onReplayProgress(progress: ReplayProgress): (() => void) | undefined {
    const generation = ++replayGeneration;
    replayProgress.value = progress;
    awaitingReplay.value = true;
    renderingReplay.value = progress.phase === "end";
    if (progress.phase !== "end") return;
    return () => {
      if (generation !== replayGeneration) return;
      replayProgress.value = null;
      renderingReplay.value = false;
      awaitingReplay.value = false;
      if (!waitForOutput || hasOutput.value) hasRendered.value = true;
    };
  }

  function onOutput(): (() => void) | undefined {
    if (hasOutput.value || outputDrainPending) return;
    outputDrainPending = true;
    return () => {
      // xterm survives transport reconnects; bytes queued before disconnect
      // still count when they render, even if the resumed replay is empty.
      if (disposed) return;
      outputDrainPending = false;
      hasOutput.value = true;
      if (!awaitingReplay.value) hasRendered.value = true;
    };
  }

  function requestTakeControl(claim: () => boolean) {
    if (takeoverPending.value) return;
    takeoverPending.value = true;
    takeoverTimer = setTimeout(() => {
      clearTakeover();
      onTakeoverTimeout();
    }, 15_000);
    try {
      const sent = claim();
      if (takeoverPending.value) takeoverQueued.value = !sent;
    } catch (error) {
      clearTakeover();
      throw error;
    }
  }

  function onDriverChange(isMe: boolean) {
    if (isMe) clearTakeover();
  }

  onScopeDispose(() => {
    replayGeneration++;
    disposed = true;
    clearTakeover();
  });

  return {
    replayProgress, renderingReplay, loading, initialLoading, waitingForOutput,
    takeoverPending, takeoverQueued, onReplayProgress, onOutput, requestTakeControl, onDriverChange,
  };
}
