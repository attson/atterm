import { defineStore } from "pinia";
import { ref } from "vue";

/**
 * Bridges a "reveal this path in the file explorer" request from the terminal
 * to the (possibly not-yet-mounted) file-explorer plugin. The terminal calls
 * request(); App.vue reacts by enabling the plugin and opening the panel; the
 * FileExplorer consumes the pending path once mounted.
 */
export const useFileRevealStore = defineStore("fileReveal", () => {
  const pending = ref<string | null>(null);
  const requestSeq = ref(0);
  function request(path: string) {
    requestSeq.value++;
    pending.value = path;
    return requestSeq.value;
  }
  function consume(expectedSeq = requestSeq.value): string | null {
    if (expectedSeq !== requestSeq.value) return null;
    const p = pending.value;
    pending.value = null;
    return p;
  }
  return { pending, requestSeq, request, consume };
});
