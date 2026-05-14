<script lang="ts" setup>
import { computed } from "vue";

export interface SelectOption {
  value: string;
  label: string;
  description?: string;
}

const props = defineProps<{
  modelValue: string;
  options: SelectOption[];
  disabled?: boolean;
  ariaLabel?: string;
}>();

defineEmits<{
  (e: "update:modelValue", value: string): void;
}>();

const selectedLabel = computed(() => {
  const match = props.options.find((o) => o.value === props.modelValue);
  return match ? match.label : "";
});
</script>

<template>
  <div class="select-dropdown">
    <button
      type="button"
      class="trigger"
      data-testid="select-trigger"
      :aria-label="ariaLabel"
      aria-haspopup="listbox"
      aria-expanded="false"
      :disabled="disabled"
    >
      <span class="trigger-label">{{ selectedLabel }}</span>
    </button>
  </div>
</template>

<style scoped>
.select-dropdown {
  position: relative;
  width: 100%;
}
.trigger {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  height: 32px;
  padding: 6px 10px;
  background: var(--bg);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 6px;
  font-size: 13px;
  font-family: inherit;
  cursor: pointer;
  text-align: left;
}
.trigger::after {
  content: "▾";
  color: var(--fg-dim);
  font-size: 11px;
  transition: transform 0.15s ease;
  flex-shrink: 0;
}
.trigger:hover:not(:disabled)::after {
  color: var(--fg);
}
.trigger:hover:not(:disabled) {
  border-color: var(--fg-dim);
}
.trigger:focus-visible {
  outline: none;
  box-shadow: 0 0 0 2px var(--accent);
}
.trigger:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}
.trigger-label {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
