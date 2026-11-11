export type AnimationFrames = {
  request(callback: FrameRequestCallback): number;
  cancel(id: number): void;
};

const browserFrames: AnimationFrames = {
  request: (callback) => requestAnimationFrame(callback),
  cancel: (id) => cancelAnimationFrame(id),
};

export function scheduleTerminalActivation(
  reveal: () => void,
  resume: () => void,
  frames: AnimationFrames = browserFrames,
): () => void {
  let revealFrame: number | null = null;
  let resumeFrame: number | null = null;
  let cancelled = false;

  revealFrame = frames.request(() => {
    revealFrame = null;
    if (cancelled) return;
    reveal();
    resumeFrame = frames.request(() => {
      resumeFrame = null;
      if (!cancelled) resume();
    });
  });

  return () => {
    if (cancelled) return;
    cancelled = true;
    if (revealFrame !== null) frames.cancel(revealFrame);
    if (resumeFrame !== null) frames.cancel(resumeFrame);
    revealFrame = null;
    resumeFrame = null;
  };
}
