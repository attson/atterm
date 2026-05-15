import { onScopeDispose, watch, type Ref } from "vue";
import type { QuickInputButton } from "../configStore";
import { normalizeHotkey } from "./hotkeyConflict";

function keyboardEventToHotkey(e: KeyboardEvent): string | null {
  if (!e.altKey) return null;
  const parts: string[] = ["Alt"];
  if (e.shiftKey) parts.push("Shift");
  if (/^Key[A-Z]$/.test(e.code)) {
    parts.push(e.code.slice(3));
  } else if (/^Digit\d$/.test(e.code)) {
    parts.push(e.code.slice(5));
  } else if (e.key === "ArrowLeft" || e.key === "ArrowRight" || e.key === "ArrowUp" || e.key === "ArrowDown") {
    parts.push(e.key);
  } else {
    return null;
  }
  return parts.join("+");
}

export function useQuickInputHotkeys(
  buttons: Ref<QuickInputButton[]>,
  onFire: (button: QuickInputButton) => void,
): void {
  let map = new Map<string, QuickInputButton>();
  function rebuild() {
    map = new Map();
    for (const b of buttons.value) {
      if (!b.hotkey) continue;
      const n = normalizeHotkey(b.hotkey);
      if (!n) continue;
      map.set(n, b);
    }
  }
  rebuild();
  const stop = watch(buttons, rebuild, { deep: true });

  function handler(e: KeyboardEvent) {
    const sig = keyboardEventToHotkey(e);
    if (!sig) return;
    const b = map.get(sig);
    if (!b) return;
    e.preventDefault();
    e.stopPropagation();
    onFire(b);
  }
  document.addEventListener("keydown", handler, { capture: true });
  onScopeDispose(() => {
    document.removeEventListener("keydown", handler, { capture: true } as EventListenerOptions);
    stop();
  });
}
