# Settings Select Dropdown Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the two native `<select>` elements in the desktop Settings dialog with a custom `SelectDropdown.vue` component so both the trigger and popup menu match the dark theme.

**Architecture:** A new reusable Vue component (`SelectDropdown.vue`) renders a themed trigger button and an absolutely-positioned popover listbox. Both `SettingsGeneral.vue` (terminal theme picker) and `SettingsRelay.vue` (remote permissions picker) consume it with `v-model`. The component is keyboard-accessible (arrows, Home/End, Enter, Esc) and closes on outside click / Esc / Tab.

**Tech Stack:** Vue 3 `<script setup lang="ts">`, Vitest + `@vue/test-utils` (`mount` from `@vue/test-utils`, jsdom environment), CSS scoped to the component using the existing `var(--bg)`, `var(--fg)`, `var(--fg-dim)`, `var(--border)`, `var(--accent)` tokens.

**Spec:** `docs/superpowers/specs/2026-05-14-settings-select-redesign-design.md`

---

## File Structure

- **Create** `desktop/frontend/src/components/SelectDropdown.vue` — reusable themed select component
- **Create** `desktop/frontend/src/components/SelectDropdown.test.ts` — mount-based interaction tests
- **Modify** `desktop/frontend/src/components/SettingsGeneral.vue` — replace native select with `<SelectDropdown>`
- **Modify** `desktop/frontend/src/components/SettingsGeneral.test.ts` — replace `<select>`/`@change` source assertions
- **Modify** `desktop/frontend/src/components/SettingsRelay.vue` — replace native select with `<SelectDropdown>`
- **Modify** `desktop/frontend/src/components/SettingsRelay.test.ts` — keep label assertion, add `SelectDropdown` import assertion

All tests run from `desktop/frontend/`:

```bash
cd desktop/frontend
npm test -- src/components/SelectDropdown.test.ts
npm test -- src/components
npm test
```

---

## Task 1: SelectDropdown scaffold + trigger renders selected label

**Files:**
- Create: `desktop/frontend/src/components/SelectDropdown.vue`
- Create: `desktop/frontend/src/components/SelectDropdown.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/components/SelectDropdown.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import SelectDropdown from "./SelectDropdown.vue";

const options = [
  { value: "a", label: "Apple", description: "red fruit" },
  { value: "b", label: "Banana", description: "yellow fruit" },
  { value: "c", label: "Cherry" },
];

describe("SelectDropdown trigger", () => {
  test("renders the selected option's label in the trigger", () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "b", options },
    });
    const trigger = wrapper.get('[data-testid="select-trigger"]');
    expect(trigger.text()).toContain("Banana");
    expect(trigger.text()).not.toContain("yellow fruit");
  });

  test("trigger is a button with aria-haspopup=listbox", () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "a", options },
    });
    const trigger = wrapper.get('[data-testid="select-trigger"]');
    expect(trigger.element.tagName).toBe("BUTTON");
    expect(trigger.attributes("aria-haspopup")).toBe("listbox");
    expect(trigger.attributes("aria-expanded")).toBe("false");
  });

  test("falls back to empty trigger label when modelValue matches no option", () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "missing", options },
    });
    const trigger = wrapper.get('[data-testid="select-trigger"]');
    expect(trigger.text().trim()).toBe("");
  });
});
```

- [ ] **Step 2: Run the test and verify it fails**

```bash
cd desktop/frontend
npm test -- src/components/SelectDropdown.test.ts
```

Expected: FAIL — module `./SelectDropdown.vue` not found.

- [ ] **Step 3: Create the minimal component to make the test pass**

Create `desktop/frontend/src/components/SelectDropdown.vue`:

```vue
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
      <span class="chevron" aria-hidden="true">▾</span>
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
.chevron {
  color: var(--fg-dim);
  font-size: 11px;
  transition: transform 0.15s ease;
}
.trigger:hover:not(:disabled) .chevron {
  color: var(--fg);
}
</style>
```

- [ ] **Step 4: Run the test and verify it passes**

```bash
cd desktop/frontend
npm test -- src/components/SelectDropdown.test.ts
```

Expected: PASS — 3 tests pass.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/SelectDropdown.vue desktop/frontend/src/components/SelectDropdown.test.ts
git commit -m "add SelectDropdown trigger with selected-label rendering"
```

---

## Task 2: Open and close the listbox

**Files:**
- Modify: `desktop/frontend/src/components/SelectDropdown.vue`
- Modify: `desktop/frontend/src/components/SelectDropdown.test.ts`

- [ ] **Step 1: Write the failing tests (append to existing describe blocks)**

Append to `SelectDropdown.test.ts`:

```ts
describe("SelectDropdown open/close", () => {
  const options = [
    { value: "a", label: "Apple" },
    { value: "b", label: "Banana" },
  ];

  test("clicking the trigger opens the listbox", async () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "a", options },
      attachTo: document.body,
    });
    expect(wrapper.find('[data-testid="select-menu"]').exists()).toBe(false);
    await wrapper.get('[data-testid="select-trigger"]').trigger("click");
    expect(wrapper.find('[data-testid="select-menu"]').exists()).toBe(true);
    expect(wrapper.get('[data-testid="select-trigger"]').attributes("aria-expanded")).toBe("true");
    wrapper.unmount();
  });

  test("pressing Esc on the trigger closes an open listbox", async () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "a", options },
      attachTo: document.body,
    });
    const trigger = wrapper.get('[data-testid="select-trigger"]');
    await trigger.trigger("click");
    expect(wrapper.find('[data-testid="select-menu"]').exists()).toBe(true);
    await trigger.trigger("keydown", { key: "Escape" });
    expect(wrapper.find('[data-testid="select-menu"]').exists()).toBe(false);
    wrapper.unmount();
  });

  test("clicking outside the component closes the listbox", async () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "a", options },
      attachTo: document.body,
    });
    await wrapper.get('[data-testid="select-trigger"]').trigger("click");
    expect(wrapper.find('[data-testid="select-menu"]').exists()).toBe(true);
    document.body.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    await wrapper.vm.$nextTick();
    expect(wrapper.find('[data-testid="select-menu"]').exists()).toBe(false);
    wrapper.unmount();
  });

  test("does not open when disabled", async () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "a", options, disabled: true },
      attachTo: document.body,
    });
    await wrapper.get('[data-testid="select-trigger"]').trigger("click");
    expect(wrapper.find('[data-testid="select-menu"]').exists()).toBe(false);
    wrapper.unmount();
  });
});
```

- [ ] **Step 2: Run the tests and verify the new ones fail**

```bash
cd desktop/frontend
npm test -- src/components/SelectDropdown.test.ts
```

Expected: 4 new tests fail. Previous 3 still pass.

- [ ] **Step 3: Update the component to handle open/close**

Replace the contents of `SelectDropdown.vue` with:

```vue
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

defineEmits<{
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
      data-testid="select-trigger"
      :aria-label="ariaLabel"
      aria-haspopup="listbox"
      :aria-expanded="open ? 'true' : 'false'"
      :disabled="disabled"
      @click="onTriggerClick"
      @keydown="onTriggerKeydown"
    >
      <span class="trigger-label">{{ selectedLabel }}</span>
      <span class="chevron" :class="{ 'chevron-open': open }" aria-hidden="true">▾</span>
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
        role="option"
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
.chevron {
  color: var(--fg-dim);
  font-size: 11px;
  transition: transform 0.15s ease;
}
.trigger:hover:not(:disabled) .chevron {
  color: var(--fg);
}
.chevron-open {
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
```

- [ ] **Step 4: Run the tests and verify they pass**

```bash
cd desktop/frontend
npm test -- src/components/SelectDropdown.test.ts
```

Expected: PASS — all 7 tests pass.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/SelectDropdown.vue desktop/frontend/src/components/SelectDropdown.test.ts
git commit -m "open and close SelectDropdown listbox"
```

---

## Task 3: Select an option (click) and emit update:modelValue

**Files:**
- Modify: `desktop/frontend/src/components/SelectDropdown.vue`
- Modify: `desktop/frontend/src/components/SelectDropdown.test.ts`

- [ ] **Step 1: Write the failing tests**

Append to `SelectDropdown.test.ts`:

```ts
describe("SelectDropdown selection", () => {
  const options = [
    { value: "a", label: "Apple", description: "red fruit" },
    { value: "b", label: "Banana", description: "yellow fruit" },
    { value: "c", label: "Cherry" },
  ];

  test("clicking an option emits update:modelValue and closes the menu", async () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "a", options },
      attachTo: document.body,
    });
    await wrapper.get('[data-testid="select-trigger"]').trigger("click");
    const items = wrapper.findAll('[data-testid="select-option"]');
    expect(items).toHaveLength(3);
    await items[1].trigger("click");
    expect(wrapper.emitted("update:modelValue")).toEqual([["b"]]);
    expect(wrapper.find('[data-testid="select-menu"]').exists()).toBe(false);
    wrapper.unmount();
  });

  test("clicking the currently selected option closes without emitting", async () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "a", options },
      attachTo: document.body,
    });
    await wrapper.get('[data-testid="select-trigger"]').trigger("click");
    const items = wrapper.findAll('[data-testid="select-option"]');
    await items[0].trigger("click");
    expect(wrapper.emitted("update:modelValue")).toBeUndefined();
    expect(wrapper.find('[data-testid="select-menu"]').exists()).toBe(false);
    wrapper.unmount();
  });

  test("options render their description text under the label", async () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "a", options },
      attachTo: document.body,
    });
    await wrapper.get('[data-testid="select-trigger"]').trigger("click");
    const items = wrapper.findAll('[data-testid="select-option"]');
    expect(items[0].text()).toContain("Apple");
    expect(items[0].text()).toContain("red fruit");
    expect(items[2].text()).toContain("Cherry");
    expect(items[2].text()).not.toContain("undefined");
    wrapper.unmount();
  });
});
```

- [ ] **Step 2: Run the tests and verify the new ones fail**

```bash
cd desktop/frontend
npm test -- src/components/SelectDropdown.test.ts
```

Expected: 3 new tests fail (no `data-testid="select-option"` yet, no click handler emitting).

- [ ] **Step 3: Update the component to emit on selection**

In `SelectDropdown.vue`, update the script block to add the emit handler. Replace the `defineEmits` line and add a `selectOption` function:

```ts
const emit = defineEmits<{
  (e: "update:modelValue", value: string): void;
}>();

// ...existing code...

function selectOption(option: SelectOption) {
  closeMenu();
  if (option.value !== props.modelValue) {
    emit("update:modelValue", option.value);
  }
}
```

In the template, replace the `<li>` block with:

```html
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
```

- [ ] **Step 4: Run the tests and verify they pass**

```bash
cd desktop/frontend
npm test -- src/components/SelectDropdown.test.ts
```

Expected: PASS — all 10 tests pass.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/SelectDropdown.vue desktop/frontend/src/components/SelectDropdown.test.ts
git commit -m "emit update:modelValue when SelectDropdown option clicked"
```

---

## Task 4: Keyboard navigation (Arrow / Home / End / Enter)

**Files:**
- Modify: `desktop/frontend/src/components/SelectDropdown.vue`
- Modify: `desktop/frontend/src/components/SelectDropdown.test.ts`

- [ ] **Step 1: Write the failing tests**

Append to `SelectDropdown.test.ts`:

```ts
describe("SelectDropdown keyboard navigation", () => {
  const options = [
    { value: "a", label: "Apple" },
    { value: "b", label: "Banana" },
    { value: "c", label: "Cherry" },
  ];

  test("ArrowDown then Enter selects the next option", async () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "a", options },
      attachTo: document.body,
    });
    const trigger = wrapper.get('[data-testid="select-trigger"]');
    await trigger.trigger("click");
    await trigger.trigger("keydown", { key: "ArrowDown" });
    await trigger.trigger("keydown", { key: "Enter" });
    expect(wrapper.emitted("update:modelValue")).toEqual([["b"]]);
    wrapper.unmount();
  });

  test("ArrowDown wraps from last option back to first", async () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "c", options },
      attachTo: document.body,
    });
    const trigger = wrapper.get('[data-testid="select-trigger"]');
    await trigger.trigger("click");
    await trigger.trigger("keydown", { key: "ArrowDown" });
    await trigger.trigger("keydown", { key: "Enter" });
    expect(wrapper.emitted("update:modelValue")).toEqual([["a"]]);
    wrapper.unmount();
  });

  test("ArrowUp from first option wraps to last", async () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "a", options },
      attachTo: document.body,
    });
    const trigger = wrapper.get('[data-testid="select-trigger"]');
    await trigger.trigger("click");
    await trigger.trigger("keydown", { key: "ArrowUp" });
    await trigger.trigger("keydown", { key: "Enter" });
    expect(wrapper.emitted("update:modelValue")).toEqual([["c"]]);
    wrapper.unmount();
  });

  test("Home highlights first, End highlights last", async () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "b", options },
      attachTo: document.body,
    });
    const trigger = wrapper.get('[data-testid="select-trigger"]');
    await trigger.trigger("click");
    await trigger.trigger("keydown", { key: "End" });
    await trigger.trigger("keydown", { key: "Enter" });
    expect(wrapper.emitted("update:modelValue")).toEqual([["c"]]);
    wrapper.unmount();
  });

  test("ArrowDown opens a closed menu without emitting", async () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "a", options },
      attachTo: document.body,
    });
    const trigger = wrapper.get('[data-testid="select-trigger"]');
    expect(wrapper.find('[data-testid="select-menu"]').exists()).toBe(false);
    await trigger.trigger("keydown", { key: "ArrowDown" });
    expect(wrapper.find('[data-testid="select-menu"]').exists()).toBe(true);
    expect(wrapper.emitted("update:modelValue")).toBeUndefined();
    wrapper.unmount();
  });
});
```

- [ ] **Step 2: Run the tests and verify the new ones fail**

```bash
cd desktop/frontend
npm test -- src/components/SelectDropdown.test.ts
```

Expected: 5 new tests fail (no keyboard handlers yet).

- [ ] **Step 3: Add highlight state + keyboard handling**

In `SelectDropdown.vue`, expand the script block. Replace the existing `open` declaration block (everything between `const open = ref(false);` and the `function onTriggerKeydown` block) with:

```ts
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

function onTriggerClick() {
  if (props.disabled) return;
  open.value ? closeMenu() : openMenu();
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
```

`selectOption` was added in Task 3. Function declarations are hoisted in JS, so `commitHighlight` can reference it regardless of source order — leave it where it is.

- [ ] **Step 4: Run all SelectDropdown tests**

```bash
cd desktop/frontend
npm test -- src/components/SelectDropdown.test.ts
```

Expected: PASS — all 15 tests pass.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/SelectDropdown.vue desktop/frontend/src/components/SelectDropdown.test.ts
git commit -m "add keyboard nav to SelectDropdown"
```

---

## Task 5: Visual highlight + selected indicator

**Files:**
- Modify: `desktop/frontend/src/components/SelectDropdown.vue`
- Modify: `desktop/frontend/src/components/SelectDropdown.test.ts`

- [ ] **Step 1: Write the failing tests**

Append to `SelectDropdown.test.ts`:

```ts
describe("SelectDropdown visual state", () => {
  const options = [
    { value: "a", label: "Apple" },
    { value: "b", label: "Banana" },
    { value: "c", label: "Cherry" },
  ];

  test("currently selected option has aria-selected=true and a selected class", async () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "b", options },
      attachTo: document.body,
    });
    await wrapper.get('[data-testid="select-trigger"]').trigger("click");
    const items = wrapper.findAll('[data-testid="select-option"]');
    expect(items[1].attributes("aria-selected")).toBe("true");
    expect(items[1].classes()).toContain("option-selected");
    expect(items[0].attributes("aria-selected")).toBe("false");
    expect(items[0].classes()).not.toContain("option-selected");
    wrapper.unmount();
  });

  test("trigger sets aria-activedescendant to the highlighted option while open", async () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "a", options },
      attachTo: document.body,
    });
    const trigger = wrapper.get('[data-testid="select-trigger"]');
    expect(trigger.attributes("aria-activedescendant")).toBeUndefined();
    await trigger.trigger("click");
    const items = wrapper.findAll('[data-testid="select-option"]');
    const firstId = items[0].attributes("id");
    expect(firstId).toBeTruthy();
    expect(trigger.attributes("aria-activedescendant")).toBe(firstId);
    await trigger.trigger("keydown", { key: "ArrowDown" });
    expect(trigger.attributes("aria-activedescendant")).toBe(items[1].attributes("id"));
    wrapper.unmount();
  });

  test("highlight class follows the keyboard highlight", async () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "a", options },
      attachTo: document.body,
    });
    const trigger = wrapper.get('[data-testid="select-trigger"]');
    await trigger.trigger("click");
    let items = wrapper.findAll('[data-testid="select-option"]');
    expect(items[0].classes()).toContain("option-highlight");
    await trigger.trigger("keydown", { key: "ArrowDown" });
    items = wrapper.findAll('[data-testid="select-option"]');
    expect(items[0].classes()).not.toContain("option-highlight");
    expect(items[1].classes()).toContain("option-highlight");
    wrapper.unmount();
  });

  test("hovering an option moves the highlight to it", async () => {
    const wrapper = mount(SelectDropdown, {
      props: { modelValue: "a", options },
      attachTo: document.body,
    });
    await wrapper.get('[data-testid="select-trigger"]').trigger("click");
    const items = wrapper.findAll('[data-testid="select-option"]');
    await items[2].trigger("mouseenter");
    expect(items[2].classes()).toContain("option-highlight");
    expect(items[0].classes()).not.toContain("option-highlight");
    wrapper.unmount();
  });
});
```

- [ ] **Step 2: Run the tests and verify the new ones fail**

```bash
cd desktop/frontend
npm test -- src/components/SelectDropdown.test.ts
```

Expected: 4 new tests fail.

- [ ] **Step 3: Wire highlight + selected classes, mouseenter handler, and aria-activedescendant**

In `SelectDropdown.vue`'s `<script setup>`, add a stable id base so options can be referenced by id. Add this near the top of the script (after imports):

```ts
let idSeq = 0;
function makeId(): string {
  idSeq += 1;
  return `select-dropdown-${idSeq}`;
}
const instanceId = makeId();

function optionId(index: number): string {
  return `${instanceId}-opt-${index}`;
}
```

Update the trigger element in the template to bind `aria-activedescendant` only when the menu is open:

```html
<button
  type="button"
  class="trigger"
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
  <span class="chevron" :class="{ 'chevron-open': open }" aria-hidden="true">▾</span>
</button>
```

Update the option `<li>` to render the id and class state:

```html
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
```

In the `<style scoped>` block, append:

```css
.option {
  position: relative;
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
```

- [ ] **Step 4: Run all SelectDropdown tests**

```bash
cd desktop/frontend
npm test -- src/components/SelectDropdown.test.ts
```

Expected: PASS — all 19 tests pass.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/SelectDropdown.vue desktop/frontend/src/components/SelectDropdown.test.ts
git commit -m "highlight and selected indicator on SelectDropdown options"
```

---

## Task 6: Wire SelectDropdown into SettingsGeneral.vue

**Files:**
- Modify: `desktop/frontend/src/components/SettingsGeneral.vue`
- Modify: `desktop/frontend/src/components/SettingsGeneral.test.ts`

- [ ] **Step 1: Update the source-level test assertions to match the new component usage**

Replace the body of the `"renders a theme select bound to the local selected ref"` test in `SettingsGeneral.test.ts` with assertions that reflect the new component. Also import SelectDropdown:

```ts
test("renders a SelectDropdown bound to the local selected ref", () => {
  expect(source).toContain("import SelectDropdown");
  expect(source).toContain('<SelectDropdown');
  expect(source).toContain('v-model="selected"');
  expect(source).toContain('@update:modelValue="onChange"');
  expect(source).toContain("terminal theme");
});
```

Leave the other tests in this file unchanged.

- [ ] **Step 2: Run the tests and verify they fail**

```bash
cd desktop/frontend
npm test -- src/components/SettingsGeneral.test.ts
```

Expected: FAIL — the updated test fails (`<SelectDropdown` not yet in source). Other tests still pass.

- [ ] **Step 3: Refactor SettingsGeneral.vue to use SelectDropdown**

In `desktop/frontend/src/components/SettingsGeneral.vue`:

1. Add the import at the top of `<script lang="ts" setup>`:

   ```ts
   import SelectDropdown from "./SelectDropdown.vue";
   ```

2. Add `computed` to the existing `vue` import:

   ```ts
   import { computed, onMounted, ref } from "vue";
   ```

3. After `const error = ref("");` add:

   ```ts
   const themeOptions = computed(() =>
     TERMINAL_THEMES.map((theme) => ({
       value: theme.id,
       label: theme.label,
       description: theme.description,
     })),
   );
   ```

4. In the template, replace the `<select>` block:

   ```html
   <select v-model="selected" :disabled="saving" @change="onChange">
     <option
       v-for="theme in TERMINAL_THEMES"
       :key="theme.id"
       :value="theme.id"
     >
       {{ theme.label }} — {{ theme.description }}
     </option>
   </select>
   ```

   with:

   ```html
   <SelectDropdown
     v-model="selected"
     :options="themeOptions"
     :disabled="saving"
     aria-label="terminal theme"
     @update:modelValue="onChange"
   />
   ```

5. Remove the `select { ... }` and `select:focus { ... }` blocks from the `<style scoped>` section. They are no longer used.

- [ ] **Step 4: Run all Settings tests**

```bash
cd desktop/frontend
npm test -- src/components/SettingsGeneral.test.ts src/components/SettingsDialog.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/SettingsGeneral.vue desktop/frontend/src/components/SettingsGeneral.test.ts
git commit -m "use SelectDropdown for terminal theme picker"
```

---

## Task 7: Wire SelectDropdown into SettingsRelay.vue

**Files:**
- Modify: `desktop/frontend/src/components/SettingsRelay.vue`
- Modify: `desktop/frontend/src/components/SettingsRelay.test.ts`

- [ ] **Step 1: Update the source-level test assertions**

In `SettingsRelay.test.ts`, replace the existing `"renders url, token, permissions, insecure toggle, and status pill"` test with:

```ts
test("renders url, token, permissions SelectDropdown, insecure toggle, and status pill", () => {
  expect(source).toContain('placeholder="wss://relay.example.com"');
  expect(source).toContain('type="password"');
  expect(source).toContain("remote session permissions");
  expect(source).toContain("import SelectDropdown");
  expect(source).toContain('<SelectDropdown');
  expect(source).toContain('v-model="remotePermission"');
  expect(source).toContain("enable insecure mode");
  expect(source).toContain("uplink running");
  expect(source).toContain("uplink stopped");
});
```

Leave the other tests in this file unchanged.

- [ ] **Step 2: Run the tests and verify they fail**

```bash
cd desktop/frontend
npm test -- src/components/SettingsRelay.test.ts
```

Expected: FAIL — `<SelectDropdown` / `import SelectDropdown` not in source yet.

- [ ] **Step 3: Refactor SettingsRelay.vue to use SelectDropdown**

In `desktop/frontend/src/components/SettingsRelay.vue`:

1. Add at the top of `<script lang="ts" setup>` (with the other imports):

   ```ts
   import SelectDropdown from "./SelectDropdown.vue";
   ```

2. Add somewhere in the script after the existing `ref` declarations:

   ```ts
   const permissionOptions = [
     { value: "view", label: "view only", description: "remote clients can watch output" },
     { value: "control", label: "control", description: "allow input and resize" },
     { value: "full", label: "full", description: "allow input, resize, and image paste" },
   ];
   ```

3. In the template, replace the `<select>` block (around line 144):

   ```html
   <select v-model="remotePermission" :disabled="saving">
     <option value="view">view only — remote clients can watch output</option>
     <option value="control">control — allow input and resize</option>
     <option value="full">full — allow input, resize, and image paste</option>
   </select>
   ```

   with:

   ```html
   <SelectDropdown
     v-model="remotePermission"
     :options="permissionOptions"
     :disabled="saving"
     aria-label="remote session permissions"
   />
   ```

4. If `SettingsRelay.vue`'s `<style scoped>` block contains a `select { ... }` or `select:focus { ... }` rule that is now unused, remove it. Leave any rules that style other elements alone.

- [ ] **Step 4: Run all relevant tests**

```bash
cd desktop/frontend
npm test -- src/components/SettingsRelay.test.ts src/components/SettingsDialog.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/SettingsRelay.vue desktop/frontend/src/components/SettingsRelay.test.ts
git commit -m "use SelectDropdown for relay permissions picker"
```

---

## Task 8: Full test suite + manual smoke test

**Files:** none (verification only)

- [ ] **Step 1: Run the full frontend test suite**

```bash
cd desktop/frontend
npm test
```

Expected: PASS — all tests across all files pass.

- [ ] **Step 2: Run the type check**

```bash
cd desktop/frontend
npm run build
```

Expected: vue-tsc emits no errors and vite build succeeds.

- [ ] **Step 3: Manual smoke test**

Start the desktop app dev server per the project's usual run command, open Settings, and verify:

1. **General tab → terminal theme dropdown:** trigger renders the current theme label only (no description). Click opens the menu; each option shows a two-line label + description. Hover and arrow keys move the highlight. The currently active theme shows the accent-bar selected indicator. Clicking another theme closes the menu, applies the theme everywhere, and persists across an app restart.
2. **Relay tab → remote session permissions dropdown:** same closed/open/hover/keyboard behavior. The label updates after selection. Saving + reloading restores the saved value.
3. **Disabled state:** while a save is in flight (`saving === true`), the trigger looks dimmed and does not open.
4. **Esc and outside-click** close the menu without changing the value.
5. **Both dropdowns** look visually consistent with each other and with the dark Settings dialog.

If anything is off, file a follow-up task — do not patch silently.

- [ ] **Step 4: Commit any small fixes if needed; otherwise this task is just verification**

No commit if nothing changed. If you fixed something, include the fix in a follow-up commit referencing the manual smoke step that caught it.

---

## Self-Review Notes

- **Spec coverage:** Tasks 1–5 cover the new `SelectDropdown` component (props, trigger, popover, keyboard, a11y, disabled, hover/selected). Task 6 covers the SettingsGeneral integration; Task 7 covers SettingsRelay. Task 8 covers the manual smoke test in the spec's expectations.
- **No placeholders:** All test bodies and component code are spelled out. No "similar to above" — Tasks 6/7 each include the full replacement block.
- **Type consistency:** `SelectOption.value`, `label`, `description?` are used identically across the component, its tests, and both Settings consumers. `update:modelValue` is the only emit and is used the same way in tests and consumers. `aria-label` prop maps to the `aria-label` HTML attribute via the `ariaLabel` prop name (Vue auto-converts the kebab-case `aria-label` attribute to the camelCase prop).
