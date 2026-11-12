import type { App, Directive, DirectiveBinding } from "vue";
import "./tooltip.css";

type TooltipValue = string | number | null | undefined | false;

interface TooltipState {
  el: HTMLElement;
  value: string;
  hovered: boolean;
  focused: boolean;
  timer: number | null;
  tooltip: HTMLElement | null;
  previousDescribedBy: string | null;
  onPointerEnter: (event: PointerEvent) => void;
  onPointerLeave: () => void;
  onFocus: () => void;
  onBlur: () => void;
}

const SHOW_DELAY_MS = 320;
const VIEWPORT_GAP = 8;
const TARGET_GAP = 7;
const states = new WeakMap<HTMLElement, TooltipState>();
let active: TooltipState | null = null;
let nextID = 0;

function normalize(value: TooltipValue): string {
  if (value === null || value === undefined || value === false) return "";
  return String(value).trim();
}

function removeGlobalListeners(): void {
  document.removeEventListener("keydown", onDocumentKeydown, true);
  document.removeEventListener("pointermove", onDocumentPointerMove, true);
  document.removeEventListener("scroll", onViewportChange, true);
  window.removeEventListener("resize", onViewportChange);
}

function hide(state: TooltipState): void {
  if (state.timer !== null) {
    window.clearTimeout(state.timer);
    state.timer = null;
  }
  if (state.tooltip) {
    state.tooltip.remove();
    state.tooltip = null;
    if (state.previousDescribedBy) {
      state.el.setAttribute("aria-describedby", state.previousDescribedBy);
    } else {
      state.el.removeAttribute("aria-describedby");
    }
    state.previousDescribedBy = null;
  }
  if (active === state) {
    active = null;
    removeGlobalListeners();
  }
}

function position(state: TooltipState): void {
  const tooltip = state.tooltip;
  if (!tooltip) return;

  const targetRect = state.el.getBoundingClientRect();
  const tooltipRect = tooltip.getBoundingClientRect();
  const viewportWidth = document.documentElement.clientWidth || window.innerWidth;
  const viewportHeight = document.documentElement.clientHeight || window.innerHeight;
  const centeredLeft = targetRect.left + targetRect.width / 2 - tooltipRect.width / 2;
  const maxLeft = Math.max(VIEWPORT_GAP, viewportWidth - tooltipRect.width - VIEWPORT_GAP);
  const left = Math.min(Math.max(centeredLeft, VIEWPORT_GAP), maxLeft);
  const topSpace = targetRect.top - TARGET_GAP - tooltipRect.height;
  const placeBelow = topSpace < VIEWPORT_GAP;
  const top = placeBelow ? targetRect.bottom + TARGET_GAP : topSpace;
  const maxTop = Math.max(VIEWPORT_GAP, viewportHeight - tooltipRect.height - VIEWPORT_GAP);

  tooltip.dataset.placement = placeBelow ? "bottom" : "top";
  tooltip.style.left = `${Math.round(left)}px`;
  tooltip.style.top = `${Math.round(Math.min(Math.max(VIEWPORT_GAP, top), maxTop))}px`;
  tooltip.style.visibility = "visible";
}

function show(state: TooltipState): void {
  if (state.timer !== null) {
    window.clearTimeout(state.timer);
    state.timer = null;
  }
  if (!state.value || (!state.hovered && !state.focused)) return;
  if (active && active !== state) hide(active);
  if (state.tooltip) return;

  const tooltip = document.createElement("div");
  tooltip.id = `atterm-tooltip-${++nextID}`;
  tooltip.className = "atterm-tooltip";
  tooltip.setAttribute("role", "tooltip");
  tooltip.textContent = state.value;
  tooltip.style.visibility = "hidden";
  document.body.appendChild(tooltip);

  state.previousDescribedBy = state.el.getAttribute("aria-describedby");
  const describedBy = [state.previousDescribedBy, tooltip.id].filter(Boolean).join(" ");
  state.el.setAttribute("aria-describedby", describedBy);
  state.tooltip = tooltip;
  active = state;
  position(state);

  document.addEventListener("keydown", onDocumentKeydown, true);
  document.addEventListener("pointermove", onDocumentPointerMove, true);
  document.addEventListener("scroll", onViewportChange, true);
  window.addEventListener("resize", onViewportChange);
}

function schedule(state: TooltipState): void {
  if (!state.value || state.tooltip || state.timer !== null) return;
  state.timer = window.setTimeout(() => show(state), SHOW_DELAY_MS);
}

function hideUnlessActive(state: TooltipState): void {
  if (!state.hovered && !state.focused) hide(state);
}

function onDocumentKeydown(event: KeyboardEvent): void {
  if (event.key === "Escape" && active) hide(active);
}

function onDocumentPointerMove(event: Event): void {
  const state = active;
  if (!state) return;
  const target = event.target;
  if (target instanceof Node && state.el.contains(target)) {
    state.hovered = true;
    return;
  }
  state.hovered = false;
  hideUnlessActive(state);
}

function onViewportChange(): void {
  if (active) hide(active);
}

function mountTooltip(el: HTMLElement, binding: DirectiveBinding<TooltipValue>): void {
  const state: TooltipState = {
    el,
    value: normalize(binding.value),
    hovered: false,
    focused: false,
    timer: null,
    tooltip: null,
    previousDescribedBy: null,
    onPointerEnter: () => {},
    onPointerLeave: () => {},
    onFocus: () => {},
    onBlur: () => {},
  };

  // A stale title would make WebKit paint its native popup alongside ours.
  el.removeAttribute("title");
  state.onPointerEnter = (event) => {
    if (event.pointerType === "touch") return;
    state.hovered = true;
    schedule(state);
  };
  state.onPointerLeave = () => {
    state.hovered = false;
    hideUnlessActive(state);
  };
  state.onFocus = () => {
    state.focused = true;
    // Keyboard users already made an explicit navigation action; showing
    // immediately avoids adding hover latency to every focus step.
    show(state);
  };
  state.onBlur = () => {
    state.focused = false;
    hideUnlessActive(state);
  };

  el.addEventListener("pointerenter", state.onPointerEnter);
  el.addEventListener("pointerleave", state.onPointerLeave);
  el.addEventListener("focus", state.onFocus);
  el.addEventListener("blur", state.onBlur);
  states.set(el, state);
}

function updateTooltip(el: HTMLElement, binding: DirectiveBinding<TooltipValue>): void {
  const state = states.get(el);
  if (!state) return;
  el.removeAttribute("title");
  state.value = normalize(binding.value);
  if (!state.value) {
    hide(state);
  } else if (state.tooltip) {
    state.tooltip.textContent = state.value;
    position(state);
  }
}

function unmountTooltip(el: HTMLElement): void {
  const state = states.get(el);
  if (!state) return;
  hide(state);
  el.removeEventListener("pointerenter", state.onPointerEnter);
  el.removeEventListener("pointerleave", state.onPointerLeave);
  el.removeEventListener("focus", state.onFocus);
  el.removeEventListener("blur", state.onBlur);
  states.delete(el);
}

export const tooltipDirective: Directive<HTMLElement, TooltipValue> = {
  mounted: mountTooltip,
  updated: updateTooltip,
  beforeUnmount: unmountTooltip,
};

export function installTooltip(app: App): void {
  app.directive("tooltip", tooltipDirective);
}
