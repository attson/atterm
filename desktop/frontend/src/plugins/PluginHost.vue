<script lang="ts" setup>
import { errText, logError, logWarn } from "../lib/log";
import { computed, onMounted, ref, watch, shallowRef, type Component } from "vue";
import { usePluginConfigStore } from "./configStore";
import { descriptorsForSlot } from "./registry";
import type { PluginContext, PluginDescriptor, PluginSlot } from "./types";
import { useI18n } from "../i18n/useI18n";

const props = defineProps<{
  slotId: PluginSlot;
  context: PluginContext;
}>();

const store = usePluginConfigStore();
const { t } = useI18n();

interface LoadedPlugin {
  descriptor: PluginDescriptor;
  component: Component;
}

const loaded = shallowRef<LoadedPlugin[]>([]);
const loading = ref(false);

async function reconcile() {
  if (!store.cfg) return;
  if (loading.value) return;
  loading.value = true;
  try {
    const slotPlugins = descriptorsForSlot(props.slotId).filter((d) =>
      store.isPluginEnabled(d.id),
    );
    // Two slots are not mountable here: "context-menu" plugins are headless
    // (they expose getMenuItems), and "companion-window" plugins render into a
    // separate OS window owned by a child process, not into this tree.
    const componentPlugins = slotPlugins.filter(
      (d) => d.slot !== "context-menu" && d.slot !== "companion-window",
    );
    const next: LoadedPlugin[] = [];
    for (const d of componentPlugins) {
      try {
        const mod = await d.load();
        next.push({ descriptor: d, component: mod.default });
      } catch (err) {
        logError("plugin", "failed to load", { plugin: d.id, error: errText(err) });
        props.context.showToast(t("app.pluginFailedToLoad", { title: t(d.titleKey) }));
        // Disable so the user is not stuck retrying every reconcile.
        try {
          await store.setEnabled(d.id, false);
        } catch (e) {
          // Now the plugin is broken AND still enabled, so it will fail again
          // on every reconcile.
          logWarn("plugin", "disable-after-load-failure also failed", {
            plugin: d.id,
            error: errText(e),
          });
        }
      }
    }
    loaded.value = next;
  } finally {
    loading.value = false;
  }
}

onMounted(async () => {
  if (!store.cfg) await store.load();
  await reconcile();
});

watch(
  () => store.cfg,
  () => {
    void reconcile();
  },
  { deep: true },
);
</script>

<template>
  <div class="plugin-host" :class="`slot-${slotId}`">
    <template v-for="p in loaded" :key="p.descriptor.id">
      <component :is="p.component" :context="context" />
    </template>
  </div>
</template>

<style scoped>
.plugin-host {
  display: contents;
}
.plugin-host.slot-right-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
}
.plugin-host.slot-bottom-toolbar {
  display: flex;
  flex-direction: row;
  align-items: center;
  min-height: 0;
}
</style>
