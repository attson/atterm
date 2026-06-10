<script setup lang="ts">
import { computed } from 'vue'
import type { RemoteSession } from '../platform/types'
import TaskStateIcon from '../components/TaskStateIcon.vue'
import { aiTitleOrCommand, taskStateLabel } from '../lib/sessionLabel'
import { shortenCwd } from '../lib/shortenCwd'
import { useI18n } from '../i18n/useI18n'
import { useTaskPreset } from '../composables/useTaskPreset'

const props = defineProps<{ session: RemoteSession; home: string }>()
const emit = defineEmits<{
  (e: 'open', s: RemoteSession): void
  (e: 'markSeen', payload: { ids: string[] }): void
}>()

const { t } = useI18n()
const preset = useTaskPreset()
const showStateLabel = computed(() => preset.active.value.showLabel)

const cmd = computed(() => aiTitleOrCommand(props.session))
const cwd = computed(() => shortenCwd(props.session.cwd, props.home))

function onMark() {
  emit('markSeen', { ids: [props.session.session_id] })
}
</script>

<template>
  <div class="card" :class="`state-${session.task_state || 'idle'}`">
    <button
      class="body"
      data-testid="card-body"
      @click="emit('open', session)"
    >
      <TaskStateIcon :state="session.task_state ?? 'idle'" :size="14" />
      <span v-if="showStateLabel" class="state-label">{{ taskStateLabel(session.task_state, t) }}</span>
      <span class="cmd-and-cwd">
        <span class="cmd">{{ cmd }}</span>
        <span v-if="cwd" class="cwd">·&nbsp;{{ cwd }}</span>
      </span>
      <span v-if="session.unread" class="unread-dot" data-testid="unread-dot">●</span>
      <span
        v-if="session.unread"
        class="row-mark-read"
        data-testid="row-mark-read"
        role="button"
        tabindex="0"
        :aria-label="t('tasks.markRead')"
        @click.stop="onMark"
        @keydown.enter.stop.prevent="onMark"
        @keydown.space.stop.prevent="onMark"
      >✓</span>
    </button>
    <span class="helper">{{ session.host }}·{{ session.user }}</span>
  </div>
</template>

<style scoped>
.card { display: flex; flex-direction: column; gap: 2px; padding: 8px 12px; min-height: 56px; border-radius: 11px; background: #11182b; border: 1px solid #1e2638; margin-bottom: 8px; }
.body { display: flex; align-items: center; gap: 8px; padding: 0; background: none; border: none; color: inherit; text-align: left; cursor: pointer; }
.state-label { font-size: 0.72rem; opacity: 0.85; white-space: nowrap; }
.cmd-and-cwd { flex: 1 1 auto; min-width: 0; display: flex; gap: 6px; overflow: hidden; align-items: baseline; }
.cmd { font-family: var(--font-mono); white-space: nowrap; }
.cwd { color: var(--fg-dim, #9aa3b2); font-family: var(--font-mono); font-size: 0.78rem; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.unread-dot { font-size: 9px; color: currentColor; }
.row-mark-read { display: inline-flex; align-items: center; justify-content: center; min-width: 44px; min-height: 44px; padding: 0 4px; font-size: 16px; cursor: pointer; }
.helper { font-size: 0.72rem; color: #8d93a3; font-family: var(--font-mono); padding-left: 22px; }
</style>
