<script lang="ts" setup>
import { computed, onMounted, ref, watch, shallowRef, type Component } from "vue";
import { usePluginConfigStore } from "./configStore";
import { descriptorsForSlot } from "./registry";
import type { PluginContext, PluginDescriptor, PluginSlot } from "./types";

const props = defineProps<{
  slotId: PluginSlot;
  context: PluginContext;
}>();

const store = usePluginConfigStore();

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
    // Context-menu plugins are headless — they expose getMenuItems instead
    // of a Vue component and must not be mounted by this host.
    const componentPlugins = slotPlugins.filter((d) => d.slot !== "context-menu");
    const next: LoadedPlugin[] = [];
    for (const d of componentPlugins) {
      try {
        const mod = await d.load();
        next.push({ descriptor: d, component: mod.default });
      } catch (err) {
        console.error(`plugin ${d.id} failed to load`, err);
        props.context.showToast(`Plugin "${d.title}" failed to load`);
        // Disable so the user is not stuck retrying every reconcile.
        try {
          await store.setEnabled(d.id, false);
        } catch {
          /* ignore secondary failure */
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
