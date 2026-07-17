<script lang="ts" setup>
import { onBeforeUnmount, ref, watch } from "vue";
import BinaryBanner from "./BinaryBanner.vue";
import type { FileSystemBridge } from "./fsBridge";

const props = defineProps<{ fs: FileSystemBridge; path: string; theme: "dimmed" | "light" }>();
const src = ref("");
const assetKey = ref(0);

const mode = ref<"fit" | "native">("fit");
const failed = ref(false);
let active = true;
let request = 0;
let requested: { fs: FileSystemBridge; path: string } | null = null;

function releaseAsset() {
  if (!requested) return;
  requested.fs.revokeAssetUrl?.(requested.path);
  requested = null;
}

async function loadAsset() {
  const fs = props.fs;
  const path = props.path;
  const currentRequest = ++request;
  assetKey.value = currentRequest;
  releaseAsset();
  requested = { fs, path };
  src.value = "";
  failed.value = false;
  try {
    const url = await fs.assetUrlFor(path);
    if (!active || currentRequest !== request || props.fs !== fs || props.path !== path) {
      fs.revokeAssetUrl?.(path);
      return;
    }
    src.value = url;
  } catch {
    if (active && currentRequest === request && props.fs === fs && props.path === path) failed.value = true;
  }
}

watch(() => [props.path, props.fs.identity], () => { void loadAsset(); }, { immediate: true });
onBeforeUnmount(() => {
  active = false;
  request++;
  releaseAsset();
});

function toggle() { mode.value = mode.value === "fit" ? "native" : "fit"; }
function onError(event: Event) {
  const target = event.currentTarget as HTMLElement | null;
  if (!src.value || !target || target.dataset.assetRequest !== String(assetKey.value)) return;
  releaseAsset();
  failed.value = true;
}
</script>

<template>
  <BinaryBanner v-if="failed" :fs="fs" :path="path" />
  <div v-else class="img-host" :class="mode">
    <img :key="assetKey" :data-asset-request="assetKey" :src="src" alt="" @click="toggle" @error="onError" />
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
