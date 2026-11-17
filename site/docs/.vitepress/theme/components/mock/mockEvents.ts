const listeners = new Map<string, Set<(data: unknown) => void>>()

export const mockEvents = {
  on(name: string, handler: (data: unknown) => void) {
    let set = listeners.get(name)
    if (!set) {
      set = new Set()
      listeners.set(name, set)
    }
    set.add(handler)
    return () => set!.delete(handler)
  },
  emit(name: string, data: unknown) {
    for (const handler of Array.from(listeners.get(name) ?? [])) {
      try {
        handler(data)
      } catch {
        // Demo event consumers are isolated like the real Wails event bus.
      }
    }
  },
}
