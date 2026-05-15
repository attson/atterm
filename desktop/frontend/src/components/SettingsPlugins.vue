<script lang="ts" setup>
import { onMounted } from "vue";
import { usePluginConfigStore } from "../plugins/configStore";
import { PLUGINS } from "../plugins/registry";
import QuickInputSettings from "../plugins/quickInput/QuickInputSettings.vue";

const store = usePluginConfigStore();

onMounted(async () => {
  if (!store.cfg) await store.load();
});

async function toggle(id: "quick-input" | "file-explorer", enabled: boolean) {
  try {
    await store.setEnabled(id, enabled);
  } catch (err) {
    console.error("setEnabled failed", err);
  }
}
</script>

<template>
  <section class="settings-plugins">
    <p class="hint">
      Plugins are loaded on demand. Disabled plugins do not affect startup
      time or memory.
    </p>
    <div v-if="!store.cfg" class="loading">Loading…</div>
    <ul v-else class="plugin-list">
      <li v-for="p in PLUGINS" :key="p.id" class="plugin-row">
        <label class="row-head">
          <input
            type="checkbox"
            :checked="store.isPluginEnabled(p.id)"
            @change="toggle(p.id, ($event.target as HTMLInputElement).checked)"
          />
          <span class="title">{{ p.title }}</span>
        </label>
        <p class="desc">{{ p.description }}</p>
        <QuickInputSettings v-if="p.id === 'quick-input' && store.isPluginEnabled('quick-input')" />
      </li>
      <li v-if="PLUGINS.length === 0" class="empty">No plugins registered.</li>
    </ul>
  </section>
</template>

<style scoped>
.settings-plugins {
  padding: 12px 16px;
  color: var(--settings-fg, #c9d1d9);
}
.hint {
  margin: 0 0 12px;
  font-size: 12px;
  opacity: 0.7;
}
.plugin-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.plugin-row {
  border: 1px solid #2d333b;
  border-radius: 6px;
  padding: 10px 12px;
  margin-bottom: 8px;
}
.row-head {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
}
.desc {
  margin: 6px 0 0 24px;
  font-size: 12px;
  opacity: 0.7;
}
.empty {
  font-size: 12px;
  opacity: 0.5;
}
</style>
