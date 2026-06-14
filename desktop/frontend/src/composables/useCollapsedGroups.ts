import { ref } from "vue";

// Module-scope so the Set survives component unmount/remount. Mobile's
// MobileSessionList is mounted with v-if and torn down whenever the user
// navigates to a terminal or to settings; hoisting the state out of the
// component keeps collapse state intact across those round-trips while
// still letting onMounted re-fetch sessions on each return.
const collapsed = ref<Set<string>>(new Set());

export interface UseCollapsedGroups {
  collapsed: typeof collapsed;
  isCollapsed(key: string): boolean;
  toggle(key: string): void;
}

export function useCollapsedGroups(): UseCollapsedGroups {
  function isCollapsed(key: string): boolean {
    return collapsed.value.has(key);
  }
  function toggle(key: string): void {
    // Swap in a fresh Set so Vue reactivity picks up the mutation.
    const next = new Set(collapsed.value);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    collapsed.value = next;
  }
  return { collapsed, isCollapsed, toggle };
}

export function __resetForTests(): void {
  collapsed.value = new Set();
}
