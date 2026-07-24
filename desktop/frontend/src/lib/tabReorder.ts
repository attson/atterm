export function applyTabReorder<T extends { id: string }>(
  list: T[],
  fromId: string,
  targetId: string,
  position: "before" | "after",
): T[] {
  if (fromId === targetId) return list;
  const arr = list.slice();
  const from = arr.findIndex((t) => t.id === fromId);
  if (from < 0) return list;
  const [moved] = arr.splice(from, 1);
  const to = arr.findIndex((t) => t.id === targetId);
  if (to < 0) {
    arr.push(moved);
    return arr;
  }
  const insertAt = position === "after" ? to + 1 : to;
  arr.splice(insertAt, 0, moved);
  return arr;
}
