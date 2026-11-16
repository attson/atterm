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

  function cancel(): void {
    if (cancelled) return;
    cancelled = true;
    document.removeEventListener("visibilitychange", onVisibilityChange);
    if (revealFrame !== null) frames.cancel(revealFrame);
    if (resumeFrame !== null) frames.cancel(resumeFrame);
    revealFrame = null;
    resumeFrame = null;
  }

  function finish(): void {
    if (cancelled) return;
    cancel();
    resume();
  }

  // Browsers freeze RAF on hidden pages. The caller's subscription guard
  // decides whether to connect or pause; either decision must run promptly.
  function onVisibilityChange(): void {
    if (document.hidden) finish();
  }
  document.addEventListener("visibilitychange", onVisibilityChange);

  revealFrame = frames.request(() => {
    revealFrame = null;
    if (cancelled) return;
    reveal();
    if (cancelled) return;
    resumeFrame = frames.request(() => {
      resumeFrame = null;
      finish();
    });
  });

  return cancel;
}
