<script lang="ts" setup>
import { computed } from "vue";
import { parseLogLine, levelAtLeast, type LogLevel } from "../lib/parseLogLine";

const props = withDefaults(
  defineProps<{ content: string; minLevel?: LogLevel }>(),
  { minLevel: "DEBUG" },
);

const lines = computed(() => {
  const out = props.content.split("\n").map(parseLogLine);
  return out.filter(
    (l) => l.kind === "raw" || levelAtLeast(l.level, props.minLevel),
  );
});
</script>

<template>
  <div class="log-lines">
    <div
      v-for="(l, i) in lines"
      :key="i"
      class="log-line"
      :class="l.kind === 'structured' ? 'lvl-' + l.level : 'lvl-raw'"
    >
      <template v-if="l.kind === 'structured'">
        <span class="ts">{{ l.ts }}</span>
        <span class="lvl">{{ l.level }}</span>
        <span class="tag">[{{ l.tag }}]</span>
        <span class="msg">{{ l.msg }}</span>
      </template>
      <template v-else>
        <span class="raw">{{ l.text }}</span>
      </template>
    </div>
  </div>
</template>

<style scoped>
.log-lines {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11px;
  line-height: 1.45;
  white-space: pre-wrap;
  word-break: break-word;
}
.log-line { display: block; }
.ts { color: var(--fg-dim); margin-right: 6px; }
.lvl { margin-right: 6px; font-weight: 700; }
.tag { color: var(--accent); margin-right: 6px; }
.msg { color: var(--fg); }
.raw { color: var(--fg-dim); }
.lvl-DEBUG .lvl { color: var(--fg-dim); }
.lvl-DEBUG .msg { color: var(--fg-dim); }
.lvl-INFO .lvl { color: var(--fg); }
.lvl-WARN .lvl,
.lvl-WARN .msg { color: var(--warn, #d2a86a); }
.lvl-ERROR .lvl,
.lvl-ERROR .msg { color: var(--bad); }
</style>
