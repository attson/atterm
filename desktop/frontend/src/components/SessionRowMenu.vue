<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";

export type MenuItem = {
  key: string;
  label: string;
  disabled?: boolean;
  // Draws a divider above this item, for setting a destructive action apart.
  separatorBefore?: boolean;
};

const props = withDefaults(defineProps<{
  open: boolean;
  x: number;
  y: number;
  items: MenuItem[];
}>(), { open: false });

const emit = defineEmits<{
  (e: "close"): void;
  (e: "select", key: string): void;
}>();

const menuRef = ref<HTMLElement | null>(null);
const positionedX = ref(0);
const positionedY = ref(0);

function updatePosition() {
  if (!menuRef.value) {
    positionedX.value = props.x;
    positionedY.value = props.y;
    return;
  }
  const rect = menuRef.value.getBoundingClientRect();
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  positionedX.value = props.x + rect.width > vw ? Math.max(0, props.x - rect.width) : props.x;
  positionedY.value = props.y + rect.height > vh ? Math.max(0, props.y - rect.height) : props.y;
}

const style = computed(() => ({
  left: positionedX.value + "px",
  top: positionedY.value + "px",
}));

function onItemClick(item: MenuItem) {
  if (item.disabled) return;
  emit("select", item.key);
  emit("close");
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Escape") emit("close");
}

function onOutside(e: MouseEvent) {
  if (!menuRef.value) return;
  if (!menuRef.value.contains(e.target as Node)) emit("close");
}

function onFocusOut(e: FocusEvent) {
  if (!menuRef.value) return;
  const related = e.relatedTarget as Node | null;
  if (!related || !menuRef.value.contains(related)) emit("close");
}

watch(
  () => props.open,
  (v) => {
    if (v) {
      positionedX.value = props.x;
      positionedY.value = props.y;
      requestAnimationFrame(updatePosition);
      window.addEventListener("keydown", onKeydown);
      window.addEventListener("mousedown", onOutside);
    } else {
      window.removeEventListener("keydown", onKeydown);
      window.removeEventListener("mousedown", onOutside);
    }
  },
  { immediate: true },
);

onMounted(() => {
  if (props.open) requestAnimationFrame(updatePosition);
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKeydown);
  window.removeEventListener("mousedown", onOutside);
});
</script>

<template>
  <div
    v-if="open"
    ref="menuRef"
    class="session-row-menu"
    data-test="session-row-menu"
    role="menu"
    :style="style"
    @focusout.capture="onFocusOut"
    @contextmenu.prevent
  >
    <template v-for="item in items" :key="item.key">
    <div
      v-if="item.separatorBefore"
      class="menu-separator"
      :data-test="`session-row-menu-separator-${item.key}`"
      role="separator"
    />
    <button
      class="menu-item"
      :class="{ disabled: item.disabled }"
      :data-test="`session-row-menu-item-${item.key}`"
      role="menuitem"
      type="button"
      :disabled="item.disabled"
      @click.stop="onItemClick(item)"
    >
      {{ item.label }}
    </button>
    </template>
  </div>
</template>

<style scoped>
.session-row-menu {
  position: fixed;
  z-index: 1000;
  min-width: 140px;
  padding: 4px;
  background: var(--menu-bg, #1f1f22);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 6px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
}
.menu-item {
  display: block;
  width: 100%;
  padding: 6px 10px;
  background: transparent;
  border: none;
  color: inherit;
  text-align: left;
  cursor: pointer;
  border-radius: 4px;
  font-size: 13px;
}
.menu-item:hover:not(.disabled),
.menu-item:focus:not(.disabled) {
  background: rgba(255, 255, 255, 0.08);
  outline: none;
}
.menu-item.disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.menu-separator {
  height: 1px;
  margin: 4px 6px;
  background: rgba(255, 255, 255, 0.12);
}
</style>
