<script lang="ts" setup>
import { nextTick, onMounted, ref } from "vue";
import { File, Folder } from "lucide-vue-next";

const props = defineProps<{
  level: number;
  initialValue?: string;
  placeholder?: string;
  icon: "file" | "folder";
}>();

const emit = defineEmits<{
  (e: "submit", value: string): void;
  (e: "cancel"): void;
}>();

const value = ref(props.initialValue ?? "");
const inputRef = ref<HTMLInputElement | null>(null);

onMounted(async () => {
  await nextTick();
  inputRef.value?.focus();
  inputRef.value?.select();
});

function onKey(e: KeyboardEvent) {
  if (e.key === "Enter") {
    if (value.value.trim() === "") emit("cancel");
    else emit("submit", value.value.trim());
  } else if (e.key === "Escape") {
    emit("cancel");
  }
}
</script>

<template>
  <div class="row" :style="{ paddingLeft: `${level * 8}px` }" data-test="inline-edit-row">
    <span class="icon">
      <component :is="icon === 'folder' ? Folder : File" :size="16" :stroke-width="1.5" />
    </span>
    <input
      ref="inputRef"
      v-model="value"
      class="input"
      :placeholder="placeholder"
      data-test="inline-edit-input"
      @keydown="onKey"
      @blur="emit('cancel')"
    />
  </div>
</template>

<style scoped>
.row { display: flex; align-items: center; height: 22px; gap: 6px; }
.icon { display: inline-flex; align-items: center; width: 20px; margin-left: 14px; color: var(--ed-folder, #d4a14a); }
.input {
  flex: 1 1 auto;
  background: transparent;
  color: inherit;
  border: 1px solid var(--ed-tab-active-bar, #539bf5);
  border-radius: 2px;
  font: inherit;
  padding: 0 4px;
  outline: none;
}
</style>
