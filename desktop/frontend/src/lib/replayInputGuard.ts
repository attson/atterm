export type ReplayProgressPhase = "start" | "chunk" | "end";

export type ReplayInputGuard = {
  onProgress(phase: ReplayProgressPhase, scheduleDrain?: (release: () => void) => void): void;
  shouldForward(): boolean;
};

/**
 * Suppresses xterm-generated responses while replayed output is being parsed.
 * The end frame only releases the guard after the terminal write queue drains.
 */
export function createReplayInputGuard(): ReplayInputGuard {
  let state: "idle" | "replaying" | "draining" = "idle";
  let generation = 0;

  return {
    onProgress(phase, scheduleDrain) {
      if (phase === "start") {
        generation += 1;
        state = "replaying";
        return;
      }
      if (phase === "chunk") {
        state = "replaying";
        return;
      }

      const releaseGeneration = generation;
      state = "draining";
      const release = () => {
        if (generation === releaseGeneration) state = "idle";
      };
      if (scheduleDrain) scheduleDrain(release);
      else release();
    },
    shouldForward: () => state === "idle",
  };
}
