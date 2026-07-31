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
  function request(path: string) {
    pending.value = path;
  }
  function consume(): string | null {
    const p = pending.value;
    pending.value = null;
    return p;
  }
  return { pending, request, consume };
});
