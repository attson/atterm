import type { ContextMenuPlugin, PluginDescriptor } from "../types";

/**
 * The companion window ("桌面挂件" / Desk Widget) plugin.
 *
 * Unlike every other plugin, this one mounts nothing into the main window: the
 * UI lives in a second process (`AT Term --widget`, see desktop/widget_window.go)
 * that owns a frameless always-on-top window. The descriptor exists so the widget
 * shows up in Settings → Plugins with the same enable/disable affordance and
 * the same persisted-config path as the others; the process lifecycle is
 * driven by composables/useDeskWidget.ts watching that enabled flag.
 *
 * load() therefore returns a headless no-op module. PluginHost never calls it
 * (it skips this slot), but PluginDescriptor requires the field and a stub is
 * honest about there being nothing to mount.
 */
const headless: ContextMenuPlugin = {
  getMenuItems: () => [],
};

export const deskWidgetDescriptor: PluginDescriptor = {
  id: "desk-widget",
  slot: "companion-window",
  titleKey: "plugins.deskWidget.title",
  descriptionKey: "plugins.deskWidget.description",
  load: async () => ({ default: headless }),
  defaultEnabled: false,
  desktopOnly: true,
};
