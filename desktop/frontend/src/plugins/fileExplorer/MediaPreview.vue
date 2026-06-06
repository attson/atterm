<script lang="ts" setup>
import { ref, computed } from "vue";
import { usePlatform } from "../../platform";
import BinaryBanner from "./BinaryBanner.vue";

const props = defineProps<{ path: string; kind: "audio" | "video" }>();
const platform = usePlatform();
const src = computed(() => platform.pluginHost!.fs.assetUrlFor(props.path));

const failed = ref(false);
function onError() { failed.value = true; }
</script>

<template>
  <BinaryBanner v-if="failed" :path="path" />
  <div v-else class="media-host">
    <video
      v-if="kind === 'video'"
      :src="src"
      controls
      preload="metadata"
      @error="onError"
    />
    <audio
      v-else
      :src="src"
      controls
      preload="metadata"
      @error="onError"
    />
  </div>
</template>

<style scoped>
.media-host {
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--ed-editor-bg, #22272e);
  padding: 16px;
}
video {
  max-width: 100%;
  max-height: 100%;
  background: #000;
  outline: none;
}
audio {
  width: min(480px, 100%);
}
</style>
