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
