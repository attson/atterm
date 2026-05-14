<script lang="ts" setup>
import { computed, onBeforeUnmount, ref, watch } from "vue";

export interface SelectOption {
  value: string;
  label: string;
  description?: string;
}

let idSeq = 0;
function makeId(): string {
  idSeq += 1;
  return `select-dropdown-${idSeq}`;
}
const instanceId = makeId();

function optionId(index: number): string {
  return `${instanceId}-opt-${index}`;
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
const highlightIndex = ref(-1);

const selectedLabel = computed(() => {
  const match = props.options.find((o) => o.value === props.modelValue);
  return match ? match.label : "";
});

function findSelectedIndex(): number {
  return props.options.findIndex((o) => o.value === props.modelValue);
}

function openMenu() {
  if (props.disabled) return;
  open.value = true;
  const idx = findSelectedIndex();
  highlightIndex.value = idx >= 0 ? idx : 0;
}

function closeMenu() {
  open.value = false;
  highlightIndex.value = -1;
}

function selectOption(option: SelectOption) {
  closeMenu();
  if (option.value !== props.modelValue) {
    emit("update:modelValue", option.value);
  }
}

function moveHighlight(delta: number) {
  if (props.options.length === 0) return;
  const n = props.options.length;
  const current = highlightIndex.value < 0 ? findSelectedIndex() : highlightIndex.value;
  const base = current < 0 ? 0 : current;
  highlightIndex.value = (base + delta + n) % n;
}

function commitHighlight() {
  if (highlightIndex.value < 0) return;
  const option = props.options[highlightIndex.value];
  if (!option) return;
  selectOption(option);
}

function onTriggerClick() {
  if (props.disabled) return;
  open.value ? closeMenu() : openMenu();
}

function onTriggerKeydown(e: KeyboardEvent) {
  if (props.disabled) return;
  if (e.key === "Escape") {
    if (open.value) {
      e.preventDefault();
      closeMenu();
    }
    return;
  }
  if (e.key === "ArrowDown") {
    e.preventDefault();
    if (!open.value) openMenu();
    else moveHighlight(1);
    return;
  }
  if (e.key === "ArrowUp") {
    e.preventDefault();
    if (!open.value) openMenu();
    else moveHighlight(-1);
    return;
  }
  if (e.key === "Home" && open.value) {
    e.preventDefault();
    highlightIndex.value = 0;
    return;
  }
  if (e.key === "End" && open.value) {
    e.preventDefault();
    highlightIndex.value = props.options.length - 1;
    return;
  }
  if (e.key === "Enter" && open.value) {
    e.preventDefault();
    commitHighlight();
    return;
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
      :aria-activedescendant="open && highlightIndex >= 0 ? optionId(highlightIndex) : undefined"
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
        v-for="(option, index) in options"
        :id="optionId(index)"
        :key="option.value"
        class="option"
        :class="{
          'option-highlight': index === highlightIndex,
          'option-selected': option.value === modelValue,
        }"
        data-testid="select-option"
        role="option"
        :aria-selected="option.value === modelValue ? 'true' : 'false'"
        @click="selectOption(option)"
        @mouseenter="highlightIndex = index"
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
  position: relative;
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
.option-highlight {
  background: rgba(255, 255, 255, 0.06);
}
.option-selected::before {
  content: "";
  position: absolute;
  top: 4px;
  bottom: 4px;
  left: 0;
  width: 2px;
  background: var(--accent);
  border-radius: 2px;
}
.option-selected .option-label {
  color: var(--fg);
  font-weight: 500;
}
</style>
