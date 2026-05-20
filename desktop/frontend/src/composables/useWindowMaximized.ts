import { ref, type Ref } from "vue";
import { WindowIsMaximised } from "../../wailsjs/runtime/runtime";

const isMaximized = ref(false);

let initStarted = false;
function initOnce() {
  if (initStarted) return;
  initStarted = true;
  // Wails runtime may be unavailable in tests or on first paint; default
  // to false if the call rejects.
  Promise.resolve()
    .then(() => WindowIsMaximised())
    .then((v) => {
      isMaximized.value = !!v;
    })
    .catch(() => {
      isMaximized.value = false;
    });
}

export function useWindowMaximized(): Ref<boolean> {
  initOnce();
  return isMaximized;
}

export function setMaximized(v: boolean): void {
  isMaximized.value = v;
}
