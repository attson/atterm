<script lang="ts" setup>
import { computed, onMounted, onBeforeUnmount } from "vue";
import type { SessionInfo } from "../lib/connection";

const props = defineProps<{
  excludeSessionIds: string[];
  localSessions: SessionInfo[];
  remoteSessions: SessionInfo[];
}>();

const emit = defineEmits<{
  (e: "pick", payload: { sessionId: string; remote: boolean }): void;
  (e: "close"): void;
}>();

const exclude = computed(() => new Set(props.excludeSessionIds));
const localOptions = computed(() =>
  props.localSessions.filter((s) => !exclude.value.has(s.id)),
);
const remoteOptions = computed(() =>
  props.remoteSessions.filter((s) => !exclude.value.has(s.id)),
);

function onEsc(e: KeyboardEvent) {
  if (e.key === "Escape") emit("close");
}
onMounted(() => document.addEventListener("keydown", onEsc));
onBeforeUnmount(() => document.removeEventListener("keydown", onEsc));

function shortTitle(s: SessionInfo): string {
  if (s.cwd) {
    const stripped = s.cwd.replace(/\/+$/, "");
    if (stripped !== "") return stripped.split("/").pop() || stripped;
  }
  const first = (s.command || "").split(/\s+/)[0] || "shell";
  return first.split("/").pop() || first;
}
</script>

<template>
  <div class="backdrop" @click.self="emit('close')">
    <div class="dialog">
      <h2>pick a session</h2>

      <div v-if="localOptions.length + remoteOptions.length === 0" class="empty">
        no sessions available — none running locally and no eligible remote.
      </div>

      <template v-else>
        <section v-if="localOptions.length > 0">
          <h3>local</h3>
          <div class="grid">
            <button
              v-for="s in localOptions"
              :key="s.id"
              class="card"
              @click="emit('pick', { sessionId: s.id, remote: false })"
            >
              <div class="title">{{ shortTitle(s) }}</div>
              <div class="meta">
                <span class="cmd">{{ s.command || "(unknown)" }}</span>
                <span class="cwd">{{ s.cwd }}</span>
              </div>
            </button>
          </div>
        </section>

        <section v-if="remoteOptions.length > 0">
          <h3>remote</h3>
          <div class="grid">
            <button
              v-for="s in remoteOptions"
              :key="s.id"
              class="card remote"
              @click="emit('pick', { sessionId: s.id, remote: true })"
            >
              <div class="title">{{ shortTitle(s) }}</div>
              <div class="meta">
                <span class="cmd">{{ s.command || "(unknown)" }}</span>
                <span class="cwd">{{ s.cwd }}</span>
                <span class="who">{{ (s.user || "") + "@" + (s.host || "") }}</span>
              </div>
            </button>
          </div>
        </section>
      </template>

      <div class="row">
        <button class="cancel" @click="emit('close')">cancel (esc)</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.6);
  display: flex; align-items: center; justify-content: center; z-index: 100;
}
.dialog {
  background: var(--panel); border: 1px solid var(--border);
  border-radius: 8px; padding: 20px 24px; width: 720px;
  max-width: calc(100vw - 32px); max-height: calc(100vh - 64px);
  display: flex; flex-direction: column; gap: 12px;
}
h2 {
  margin: 0; font-size: 14px; font-weight: 600; letter-spacing: 0.05em;
  text-transform: uppercase; color: var(--fg-dim);
}
h3 {
  margin: 12px 0 6px; font-size: 11px; letter-spacing: 0.08em;
  text-transform: uppercase; color: var(--fg-dim);
}
.empty {
  color: var(--fg-dim); font-size: 13px; text-align: center; padding: 40px 0;
}
.grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 10px; max-height: 30vh; overflow-y: auto;
}
.card {
  text-align: left; background: #0d1117; border: 1px solid var(--border);
  border-radius: 6px; padding: 10px 12px; cursor: pointer;
  transition: border-color 120ms; color: var(--fg);
  font-family: inherit;
}
.card:hover { border-color: var(--accent); }
.card.remote .title { color: #d29922; }
.card .title {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px; margin-bottom: 4px;
}
.card .meta {
  font-size: 11px; color: var(--fg-dim);
  display: flex; gap: 10px; flex-wrap: wrap;
}
.card .meta .cwd { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 100%; }
.row {
  display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px;
}
.cancel {
  padding: 6px 12px; background: transparent; border: 1px solid var(--border);
  color: var(--fg-dim); border-radius: 4px; cursor: pointer;
}
.cancel:hover { color: var(--fg); border-color: var(--accent); }
</style>
