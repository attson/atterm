import { computed, ref, type ComputedRef, type Ref } from "vue";

// Module-scoped so state is shared across all consumers within the app
// lifetime (matches useSessionPins / useCollapsedGroups / useTaskGroupBy
// pattern). No persistence — selection is session-only by spec §1.
const selectedIds = ref<Set<string>>(new Set());
const anchorId = ref<string | null>(null);

export function useSessionSelection() {
  const size = computed(() => selectedIds.value.size);

  function isSelected(id: string): boolean {
    return selectedIds.value.has(id);
  }

  function toggle(id: string): void {
    if (selectedIds.value.has(id)) selectedIds.value.delete(id);
    else selectedIds.value.add(id);
    anchorId.value = id;
  }

  function selectOnly(id: string): void {
    for (const existing of Array.from(selectedIds.value)) {
      if (existing !== id) selectedIds.value.delete(existing);
    }
    selectedIds.value.add(id);
    anchorId.value = id;
  }

  function selectRange(id: string, orderedIds: string[]): void {
    if (!anchorId.value) return toggle(id);
    const a = orderedIds.indexOf(anchorId.value);
    const b = orderedIds.indexOf(id);
    if (a < 0 || b < 0) return toggle(id);
    const [lo, hi] = a < b ? [a, b] : [b, a];
    for (let i = lo; i <= hi; i++) selectedIds.value.add(orderedIds[i]);
    // anchor stays put — mirrors macOS Finder / VSCode Shift+click
    // behavior, so subsequent Shift+click extends from the same origin.
  }

  function clear(): void {
    if (selectedIds.value.size === 0 && anchorId.value === null) return;
    selectedIds.value.clear();
    anchorId.value = null;
  }

  return {
    selectedIds: selectedIds as Ref<Set<string>>,
    anchorId: anchorId as Ref<string | null>,
    size: size as ComputedRef<number>,
    isSelected,
    toggle,
    selectOnly,
    selectRange,
    clear,
  };
}

// Test-only: reset module state between tests. Do NOT call from app code.
export function __resetForTests(): void {
  selectedIds.value.clear();
  anchorId.value = null;
}
