import { ref } from "vue";

// Module-scope so the Set survives component unmount/remount. The shared
// TaskSidebar can be torn down by settings/admin view transitions; hoisting
// state keeps collapse state intact across those round-trips.
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
