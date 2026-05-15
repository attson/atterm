export interface ResizerOptions {
  onDrag: (deltaX: number, deltaY: number) => void;
  onEnd?: () => void;
}

// Drag handler that uses rAF batching so onDrag fires at most once per frame.
export function useResizer(opts: ResizerOptions) {
  let startX = 0;
  let startY = 0;
  let lastX = 0;
  let lastY = 0;
  let raf = 0;

  function onMouseMove(e: MouseEvent) {
    lastX = e.clientX;
    lastY = e.clientY;
    if (raf) return;
    raf = requestAnimationFrame(() => {
      raf = 0;
      opts.onDrag(startX - lastX, startY - lastY);
      startX = lastX;
      startY = lastY;
    });
  }

  function onMouseUp() {
    window.removeEventListener("mousemove", onMouseMove);
    window.removeEventListener("mouseup", onMouseUp);
    if (raf) cancelAnimationFrame(raf);
    raf = 0;
    opts.onEnd?.();
  }

  function onMouseDown(e: MouseEvent) {
    startX = e.clientX;
    startY = e.clientY;
    lastX = startX;
    lastY = startY;
    e.preventDefault();
    window.addEventListener("mousemove", onMouseMove);
    window.addEventListener("mouseup", onMouseUp);
  }

  return { onMouseDown };
}
