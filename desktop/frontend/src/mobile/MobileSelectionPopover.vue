<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '../i18n/useI18n'

const props = defineProps<{
  visible: boolean
  x: number               // viewport px — popover horizontal CENTER (we translateX(-50%))
  y: number               // viewport px — interpreted as `bottom` when arrowDir='down', `top` when 'up'
  arrowDir: 'down' | 'up'
  copying: boolean
  sending: boolean
}>()

defineEmits<{
  (e: 'copy'): void
  (e: 'send'): void
  (e: 'cancel'): void
}>()

const { t } = useI18n()

const popStyle = computed(() => {
  const base = `left: ${props.x}px;`
  return props.arrowDir === 'down'
    ? `${base} bottom: ${props.y}px;`
    : `${base} top: ${props.y}px;`
})
</script>

<template>
  <div
    v-if="visible"
    class="popover"
    :class="[`arrow-${arrowDir}`]"
    :style="popStyle"
    data-testid="selection-popover"
    role="toolbar"
    :aria-label="t('mobile.selection.copy') + ' / ' + t('mobile.selection.send')"
    @pointerdown.stop
    @pointerup.stop
    @click.stop
  >
    <button
      type="button"
      class="btn"
      :disabled="copying"
      data-testid="selection-popover-copy"
      @click="$emit('copy')"
    >{{ t('mobile.selection.copy') }}</button>
    <button
      type="button"
      class="btn send"
      :disabled="sending"
      data-testid="selection-popover-send"
      @click="$emit('send')"
    >{{ t('mobile.selection.send') }}</button>
    <button
      type="button"
      class="btn cancel"
      :aria-label="t('mobile.selection.cancel')"
      data-testid="selection-popover-cancel"
      @click="$emit('cancel')"
    >×</button>
  </div>
</template>

<style scoped>
/* Container: dark bar, ~21 px tall visual; transform centers horizontally on x. */
.popover {
  position: absolute;
  transform: translateX(-50%);
  background: #2b2c30;
  color: #fff;
  border-radius: 8px;
  display: flex;
  box-shadow: 0 6px 20px rgba(0, 0, 0, .4);
  overflow: hidden;
  font-family: -apple-system, system-ui, sans-serif;
  z-index: 1000;            /* above terminal canvas + control panel */
  pointer-events: auto;
}
/* Arrow rendered as a pseudo-element pointing at the selection */
.popover::after {
  content: '';
  position: absolute;
  left: 50%;
  transform: translateX(-50%);
  width: 0;
  height: 0;
  border-left: 5px solid transparent;
  border-right: 5px solid transparent;
}
.popover.arrow-down::after {
  bottom: -5px;
  border-top: 5px solid #2b2c30;
}
.popover.arrow-up::after {
  top: -5px;
  border-bottom: 5px solid #2b2c30;
}
/* Buttons: 11 px visual + 4×11 padding ≈ 21 px tall; transparent hit-slop
   margin doubles the actual tap target without changing visual layout. */
.btn {
  position: relative;
  background: none;
  border: none;
  color: #fff;
  padding: 4px 11px;
  font-size: 11px;
  font-family: inherit;
  cursor: pointer;
  border-right: 1px solid #3f4046;
}
.btn:last-child { border-right: none; }
.btn.send { color: #60a5fa; font-weight: 600; }
.btn.cancel { font-size: 14px; line-height: 1; padding: 4px 8px; }
.btn:disabled { opacity: .5; cursor: not-allowed; }
/* Hit-slop: transparent box extends tap target above/below visual without
   shifting layout. ::before is the hit area; pointer-events:auto on parent
   .popover passes clicks through to the underlying <button>. */
.btn::before {
  content: '';
  position: absolute;
  top: -8px;
  bottom: -8px;
  left: 0;
  right: 0;
}
</style>
