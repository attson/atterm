<script lang="ts" setup>
import { computed, ref } from "vue";
import { usePluginConfigStore } from "../plugins/configStore";
import {
  ACTIONS,
  formatChord,
  resolvedBindings,
  type Mod,
  type ShortcutAction,
} from "../lib/shortcutBindings";
import { useLongPressModifier } from "../composables/useLongPressModifier";

function detectMod(): Mod {
  if (typeof navigator === "undefined") return "Control";
  return navigator.platform?.toLowerCase().includes("mac") ? "Meta" : "Control";
}

const props = defineProps<{
  mod?: Mod;
  thresholdMs?: number;
}>();

const mod: Mod = props.mod ?? detectMod();
const store = usePluginConfigStore();
const visible = ref(false);

useLongPressModifier({
  mod,
  thresholdMs: props.thresholdMs ?? 3000,
  onShow: () => { visible.value = true; },
  onHide: () => { visible.value = false; },
});

const resolved = computed(() => resolvedBindings(store.cfg?.shortcuts?.bindings ?? {}));

const paneActions = ACTIONS.filter((a) => a.group === "pane");
const tabActions = ACTIONS.filter((a) => a.group === "tab");

function chordFor(action: ShortcutAction): string {
  return formatChord(resolved.value[action.id] ?? "", mod);
}
function isDisabled(action: ShortcutAction): boolean {
  return (resolved.value[action.id] ?? "") === "";
}
</script>

<template>
  <Transition name="fade">
    <div v-if="visible" class="hints-backdrop">
      <div class="hints-panel" role="dialog" aria-label="Keyboard Shortcuts">
        <div class="hints-header">Keyboard Shortcuts</div>

        <section class="hints-group">
          <h3>Pane</h3>
          <div
            v-for="action in paneActions"
            :key="action.id"
            class="hint-row"
            :class="{ disabled: isDisabled(action) }"
          >
            <div class="chord">{{ isDisabled(action) ? "—" : chordFor(action) }}</div>
            <div class="label">{{ action.label }}</div>
          </div>
        </section>

        <section class="hints-group">
          <h3>Tab</h3>
          <div
            v-for="action in tabActions"
            :key="action.id"
            class="hint-row"
            :class="{ disabled: isDisabled(action) }"
          >
            <div class="chord">{{ isDisabled(action) ? "—" : chordFor(action) }}</div>
            <div class="label">{{ action.label }}</div>
          </div>
        </section>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.hints-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 200;
  pointer-events: none;
}
.hints-panel {
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 20px 24px;
  width: 480px;
  max-width: calc(100vw - 32px);
  max-height: calc(100vh - 32px);
  overflow-y: auto;
  color: var(--fg);
  pointer-events: auto;
}
.hints-header {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--fg-dim);
  margin-bottom: 12px;
}
.hints-group { margin-bottom: 14px; }
.hints-group:last-child { margin-bottom: 0; }
.hints-group h3 {
  margin: 0 0 6px;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--fg-dim);
}
.hint-row {
  display: grid;
  grid-template-columns: 120px 1fr;
  gap: 12px;
  padding: 3px 0;
  font-size: 12px;
}
.hint-row.disabled { color: var(--fg-dim); }
.chord {
  font-family: "SF Mono", Menlo, monospace;
  text-align: right;
}
.label { color: inherit; }

.fade-enter-active, .fade-leave-active { transition: opacity 100ms ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
