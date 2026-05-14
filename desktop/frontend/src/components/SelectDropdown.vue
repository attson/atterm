<script lang="ts" setup>
import { computed, onBeforeUnmount, ref, watch } from "vue";

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

const emit = defineEmits<{
  (e: "update:modelValue", value: string): void;
}>();

const open = ref(false);
const rootRef = ref<HTMLElement | null>(null);

const selectedLabel = computed(() => {
  const match = props.options.find((o) => o.value === props.modelValue);
  return match ? match.label : "";
});

function openMenu() {
  if (props.disabled) return;
  open.value = true;
}

function closeMenu() {
  open.value = false;
}

function selectOption(option: SelectOption) {
  closeMenu();
  if (option.value !== props.modelValue) {
    emit("update:modelValue", option.value);
  }
}

function onTriggerClick() {
  if (props.disabled) return;
  open.value ? closeMenu() : openMenu();
}

function onTriggerKeydown(e: KeyboardEvent) {
  if (props.disabled) return;
  if (e.key === "Escape" && open.value) {
    e.preventDefault();
    closeMenu();
  }
}

function onDocumentMousedown(e: MouseEvent) {
  if (!open.value) return;
  const target = e.target as Node | null;
  if (rootRef.value && target && rootRef.value.contains(target)) return;
  closeMenu();
}

watch(open, (isOpen) => {
  if (isOpen) {
    document.addEventListener("mousedown", onDocumentMousedown);
  } else {
    document.removeEventListener("mousedown", onDocumentMousedown);
  }
});

onBeforeUnmount(() => {
  document.removeEventListener("mousedown", onDocumentMousedown);
});
</script>

<template>
  <div ref="rootRef" class="select-dropdown">
    <button
      type="button"
      class="trigger"
      :class="{ 'trigger-open': open }"
      data-testid="select-trigger"
      :aria-label="ariaLabel"
      aria-haspopup="listbox"
      :aria-expanded="open ? 'true' : 'false'"
      :disabled="disabled"
      @click="onTriggerClick"
      @keydown="onTriggerKeydown"
    >
      <span class="trigger-label">{{ selectedLabel }}</span>
    </button>
    <ul
      v-if="open"
      class="menu"
      data-testid="select-menu"
      role="listbox"
    >
      <li
        v-for="option in options"
        :key="option.value"
        class="option"
        data-testid="select-option"
        role="option"
        :aria-selected="option.value === modelValue ? 'true' : 'false'"
        @click="selectOption(option)"
      >
        <span class="option-label">{{ option.label }}</span>
        <span v-if="option.description" class="option-description">
          {{ option.description }}
        </span>
      </li>
    </ul>
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
.trigger-open::after {
  transform: rotate(180deg);
}
.menu {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  right: 0;
  max-height: 240px;
  overflow-y: auto;
  margin: 0;
  padding: 4px 0;
  list-style: none;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 6px;
  box-shadow: 0 6px 16px rgba(0, 0, 0, 0.35);
  z-index: 1000;
}
.option {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 8px 10px;
  cursor: pointer;
}
.option-label {
  color: var(--fg);
  font-size: 13px;
  line-height: 1.3;
}
.option-description {
  color: var(--fg-dim);
  font-size: 12px;
  line-height: 1.3;
}
</style>
