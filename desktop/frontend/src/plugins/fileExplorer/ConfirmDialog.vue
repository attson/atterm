<script lang="ts" setup>
import { onMounted, onBeforeUnmount, ref } from "vue";

const props = defineProps<{
  title: string;
  message?: string;
  buttons: Array<{ id: string; label: string; kind?: "primary" | "danger" | "secondary" }>;
}>();

const emit = defineEmits<{
  (e: "resolve", id: string): void;
}>();

const rootRef = ref<HTMLDivElement | null>(null);

function handleKey(e: KeyboardEvent) {
  if (e.key === "Escape") {
    const cancel = props.buttons.find((b) => b.id === "cancel");
    if (cancel) emit("resolve", cancel.id);
  }
}

onMounted(() => {
  window.addEventListener("keydown", handleKey);
  rootRef.value?.focus();
});
onBeforeUnmount(() => window.removeEventListener("keydown", handleKey));
</script>

<template>
  <div class="dlg-scrim" data-test="confirm-dialog" @click.self="() => {
    const cancel = props.buttons.find((b) => b.id === 'cancel');
    if (cancel) emit('resolve', cancel.id);
  }">
    <div ref="rootRef" class="dlg" tabindex="-1" role="dialog" :aria-label="title">
      <div class="dlg-title">{{ title }}</div>
      <div v-if="message" class="dlg-msg">{{ message }}</div>
      <div class="dlg-buttons">
        <button
          v-for="b in buttons"
          :key="b.id"
          :class="['dlg-btn', b.kind ?? 'secondary']"
          :data-test="`btn-${b.id}`"
          @click="emit('resolve', b.id)"
        >
          {{ b.label }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dlg-scrim {
  position: fixed; inset: 0; background: rgba(0,0,0,0.35);
  display: flex; align-items: center; justify-content: center; z-index: 50;
}
.dlg {
  background: var(--ed-shell-bg, #22272e);
  color: var(--ed-row-fg, #adbac7);
  border: 1px solid var(--ed-border, #444c56);
  border-radius: 6px; padding: 16px; min-width: 320px; max-width: 480px;
  outline: none;
}
.dlg-title { font-weight: 600; margin-bottom: 8px; }
.dlg-msg { font-size: 12px; opacity: 0.85; margin-bottom: 12px; }
.dlg-buttons { display: flex; gap: 8px; justify-content: flex-end; }
.dlg-btn {
  padding: 4px 10px; border-radius: 3px; border: 1px solid var(--ed-border, #444c56);
  background: var(--ed-editor-bg, #22272e); color: inherit; cursor: pointer; font-size: 12px;
}
.dlg-btn.primary { background: var(--ed-tab-active-bar, #539bf5); color: white; border-color: transparent; }
.dlg-btn.danger { background: var(--ed-error, #f47067); color: white; border-color: transparent; }
</style>
