import { computed, ref, watch, type ComputedRef, type Ref, type WritableComputedRef } from "vue";

import { isLightTerminalTheme, type TerminalThemeID } from "../lib/terminalThemes";
import { usePluginConfigStore } from "../plugins/configStore";
import { useFileRevealStore } from "../plugins/fileExplorer/fileReveal";
import { useResizer } from "../plugins/useResizer";

/**
 * usePluginPanel bundles the right-hand plugin panel state that App.vue
 * used to inline: persisted / drag width, collapsed toggle, the drag
 * resizer, the "any plugin enabled?" gate, and the light/dimmed theme
 * derivation for the plugin surface.
 *
 * Also owns the file-reveal watcher that force-opens the panel + enables
 * the file explorer when the terminal asks to reveal a path — this keeps
 * every "the right panel needs to be visible" trigger in one place.
 *
 * The terminal theme id is passed in as a ref because it is owned by
 * App.vue (both the local reactive source and the persisted setter live
 * there). Everything else the composable can source from the plugin +
 * file-reveal Pinia stores directly.
 */
export interface UsePluginPanel {
  /** Effective panel width (drag override if dragging, else persisted). */
  panelWidth: ComputedRef<number>;
  /** Writable — set to true/false to collapse/expand and persist. */
  panelCollapsed: WritableComputedRef<boolean>;
  /** Flip the collapsed flag. */
  togglePanel: () => void;
  /** Bound to the vertical resizer's mousedown. */
  onPanelResizeDown: (e: MouseEvent) => void;
  /** True when at least one plugin is enabled (the collapse handle
   *  hides entirely otherwise). */
  rightPanelHasPlugin: ComputedRef<boolean>;
  /** Skin name for the panel surface; tracks the terminal theme. */
  fileExplorerTheme: ComputedRef<"dimmed" | "light">;
}

export function usePluginPanel(opts: { terminalThemeId: Ref<TerminalThemeID> }): UsePluginPanel {
  const pluginStore = usePluginConfigStore();
  const fileRevealStore = useFileRevealStore();

  const persistedPanelWidth = computed(() => pluginStore.cfg?.fileExplorer.panelWidthPx ?? 380);
  const dragPanelWidth = ref<number | null>(null);
  const panelWidth = computed(() => dragPanelWidth.value ?? persistedPanelWidth.value);

  const panelCollapsed = computed<boolean>({
    get: () => pluginStore.cfg?.fileExplorer.panelCollapsed ?? true,
    set: (v: boolean) => {
      if (!pluginStore.cfg) return;
      const next = JSON.parse(JSON.stringify(pluginStore.cfg));
      next.fileExplorer.panelCollapsed = v;
      void pluginStore.save(next);
    },
  });

  function togglePanel() {
    panelCollapsed.value = !panelCollapsed.value;
  }

  // When the terminal asks to reveal a path, make sure the file explorer is
  // enabled and the panel is open so it can mount and consume the request.
  watch(
    () => fileRevealStore.pending,
    async (p) => {
      if (!p) return;
      if (!pluginStore.isPluginEnabled("file-explorer")) {
        await pluginStore.setEnabled("file-explorer", true);
      }
      if (panelCollapsed.value) panelCollapsed.value = false;
    },
  );

  // True when at least one right-panel plugin is enabled. Suppresses the
  // collapse handle entirely when the slot has nothing to host.
  const rightPanelHasPlugin = computed(() => pluginStore.isPluginEnabled("file-explorer"));

  // Derive a plugin-side theme name from the active terminal theme so the
  // global --ed-* CSS vars on .app can paint the panel toggle, Quick Input
  // bar, and the file explorer in matching dimmed/light skins.
  const fileExplorerTheme = computed<"dimmed" | "light">(() =>
    isLightTerminalTheme(opts.terminalThemeId.value) ? "light" : "dimmed",
  );

  const { onMouseDown: onPanelResizeDown } = useResizer({
    onDrag: (deltaX) => {
      // useResizer reports deltaX = -mouseMovementX (right drag → negative).
      // The resizer sits on the panel's left edge, so dragging right shrinks
      // the panel: panelWidth + deltaX = panelWidth - movement.
      const current = dragPanelWidth.value ?? persistedPanelWidth.value;
      const next = Math.max(240, Math.min(current + deltaX, window.innerWidth * 0.7));
      dragPanelWidth.value = next;
    },
    onEnd: () => {
      if (dragPanelWidth.value === null || !pluginStore.cfg) {
        dragPanelWidth.value = null;
        return;
      }
      const next = JSON.parse(JSON.stringify(pluginStore.cfg));
      next.fileExplorer.panelWidthPx = dragPanelWidth.value;
      void pluginStore.save(next);
      dragPanelWidth.value = null;
    },
  });

  return {
    panelWidth,
    panelCollapsed,
    togglePanel,
    onPanelResizeDown,
    rightPanelHasPlugin,
    fileExplorerTheme,
  };
}
