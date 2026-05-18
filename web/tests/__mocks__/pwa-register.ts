// Stub for vite-plugin-pwa's virtual:pwa-register module in vitest.
// The plugin only exists at build time; vitest needs a real file to
// resolve. Tests that need to observe the call use vi.mock() on top.
export function registerSW(_opts?: { immediate?: boolean }): (reloadPage?: boolean) => Promise<void> {
  return async () => {}
}
