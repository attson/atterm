export type ReplayProgressPhase = "start" | "chunk" | "end";

export type ReplayInputGuard = {
  onProgress(phase: ReplayProgressPhase, scheduleDrain?: (release: () => void) => void): void;
  /**
   * Whether data may reach the PTY.
   *
   * `userOriginated` data always may. The guard exists to swallow the replies
   * xterm generates on its own while parsing escape sequences inside replayed
   * scrollback — cursor position reports, device attributes — which would
   * otherwise land in the shell as typed garbage. A keystroke is not one of
   * those, and suppressing it is actively harmful: a command flooding the
   * scrollback is both the reason the replay is huge and the reason the user is
   * reaching for Ctrl-C.
   */
  shouldForward(userOriginated?: boolean): boolean;
};

/**
 * Suppresses xterm-generated responses while replayed output is being parsed.
 * The end frame only releases the guard after the terminal write queue drains.
 * User keystrokes bypass it entirely — see shouldForward.
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
    shouldForward: (userOriginated = false) => userOriginated || state === "idle",
  };
}
