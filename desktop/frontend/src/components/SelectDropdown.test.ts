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
