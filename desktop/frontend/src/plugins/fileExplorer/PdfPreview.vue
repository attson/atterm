<script lang="ts" setup>
import { computed } from "vue";
import { usePlatform } from "../../platform";
import BinaryBanner from "./BinaryBanner.vue";

const props = defineProps<{ path: string }>();
const platform = usePlatform();
const src = computed(() => platform.pluginHost!.fs.assetUrlFor(props.path));
</script>

<template>
  <div class="pdf-host">
    <object :data="src" type="application/pdf">
      <!-- Shown if the browser can't render PDFs natively. -->
      <BinaryBanner :path="path" />
    </object>
  </div>
</template>

<style scoped>
.pdf-host {
  flex: 1 1 auto;
  display: flex;
  background: var(--ed-editor-bg, #22272e);
}
object {
  flex: 1 1 auto;
  width: 100%;
  height: 100%;
  border: none;
}
</style>
