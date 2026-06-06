<script lang="ts" setup>
import { ref, computed } from "vue";
import { usePlatform } from "../../platform";
import BinaryBanner from "./BinaryBanner.vue";

const props = defineProps<{ path: string; theme: "dimmed" | "light" }>();
const platform = usePlatform();
const src = computed(() => platform.pluginHost!.fs.assetUrlFor(props.path));

const mode = ref<"fit" | "native">("fit");
const failed = ref(false);

function toggle() { mode.value = mode.value === "fit" ? "native" : "fit"; }
function onError() { failed.value = true; }
</script>

<template>
  <BinaryBanner v-if="failed" :path="path" />
  <div v-else class="img-host" :class="mode">
    <img :src="src" alt="" @click="toggle" @error="onError" />
  </div>
</template>

<style scoped>
.img-host {
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: auto;
  background: var(--ed-editor-bg, #22272e);
  padding: 12px;
}
.img-host.fit img {
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
  cursor: zoom-in;
}
.img-host.native img {
  width: auto;
  height: auto;
  image-rendering: pixelated;
  cursor: zoom-out;
}
</style>
