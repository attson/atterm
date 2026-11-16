import { computed, onBeforeUnmount, onMounted, ref } from "vue";

/** Keep remote background connections unless the user opts into pausing them. */
export function useTerminalSubscriptionActive(
  tabActive: () => boolean,
  localSession: () => boolean,
  pauseInBackground: () => boolean = () => false,
) {
  const pageVisible = ref(!document.hidden);
  const updateVisibility = () => { pageVisible.value = !document.hidden; };
  onMounted(() => {
    updateVisibility();
    document.addEventListener("visibilitychange", updateVisibility);
  });
  onBeforeUnmount(() => document.removeEventListener("visibilitychange", updateVisibility));
  // A local session must keep its existing lifecycle when the desktop window
  // is hidden. Visible split panes all share the tab's active state.
  return computed(() => localSession() ? tabActive() : !pauseInBackground() || (tabActive() && pageVisible.value));
}
