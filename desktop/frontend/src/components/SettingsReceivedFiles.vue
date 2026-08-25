<script setup lang="ts">
import { onMounted, ref } from "vue";
import {
  ReceivedFilesList,
  ReceivedFilesClearAll,
  ReceivedFilesClearSession,
  ReceivedFilesDelete,
  ReceivedFilesOpenDir,
} from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";

const summary = ref<main.ReceivedFilesSummary>({ total_bytes: 0, sessions: [] } as unknown as main.ReceivedFilesSummary);
const loading = ref(false);
const expanded = ref<Set<string>>(new Set());
const errorMsg = ref("");

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}

async function reload() {
  loading.value = true;
  errorMsg.value = "";
  try {
    summary.value = await ReceivedFilesList();
  } catch (err) {
    errorMsg.value = String(err);
  } finally {
    loading.value = false;
  }
}

async function clearAll() {
  if (!confirm("Clear ALL received files? This cannot be undone.")) return;
  try {
    await ReceivedFilesClearAll();
  } catch (err) {
    errorMsg.value = String(err);
  }
  await reload();
}

async function clearSession(sid: string) {
  try {
    await ReceivedFilesClearSession(sid);
  } catch (err) {
    errorMsg.value = String(err);
  }
  await reload();
}

async function deleteFile(sid: string, name: string) {
  try {
    await ReceivedFilesDelete(sid, name);
  } catch (err) {
    errorMsg.value = String(err);
  }
  await reload();
}

async function openDir() {
  try {
    await ReceivedFilesOpenDir();
  } catch (err) {
    errorMsg.value = String(err);
  }
}

function toggle(sid: string) {
  const next = new Set(expanded.value);
  if (next.has(sid)) next.delete(sid);
  else next.add(sid);
  expanded.value = next;
}

onMounted(reload);
</script>

<template>
  <div class="received-files">
    <div class="header">
      <div>
        <strong data-testid="total-bytes">Total: {{ formatSize(summary.total_bytes) }}</strong>
        <span class="muted"> · {{ summary.sessions.length }} sessions</span>
      </div>
      <div class="actions">
        <button @click="openDir">Open folder</button>
        <button
          class="danger"
          :disabled="summary.sessions.length === 0"
          @click="clearAll"
        >Clear all</button>
      </div>
    </div>
    <div v-if="errorMsg" class="error">{{ errorMsg }}</div>
    <div v-if="loading">Loading…</div>
    <div v-else-if="summary.sessions.length === 0" class="empty">No files received.</div>
    <ul v-else class="sessions">
      <li v-for="s in summary.sessions" :key="s.session_id" class="session">
        <div class="session-row" data-testid="session-row" @click="toggle(s.session_id)">
          <span class="session-name">{{ s.session_name || s.session_id.slice(0, 8) }}</span>
          <span class="muted"> · {{ s.files.length }} files · {{ formatSize(s.bytes) }}</span>
          <button
            class="session-clear danger"
            @click.stop="clearSession(s.session_id)"
          >Clear</button>
        </div>
        <ul v-if="expanded.has(s.session_id)" class="files">
          <li v-for="f in s.files" :key="f.name" data-testid="file-row">
            <span class="file-name">{{ f.name }}</span>
            <span class="muted"> · {{ formatSize(f.bytes) }}</span>
            <button class="file-delete danger" @click="deleteFile(s.session_id, f.name)">Delete</button>
          </li>
        </ul>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.received-files { display: flex; flex-direction: column; gap: 12px; padding: 8px 4px; }
.header { display: flex; justify-content: space-between; align-items: center; }
.actions { display: flex; gap: 8px; }
.actions button { padding: 4px 10px; }
.actions button.danger { color: #d33; }
.muted { color: var(--fg-dim, #999); font-size: 0.875rem; }
.error { color: #d33; padding: 4px 0; font-size: 0.875rem; }
.sessions { list-style: none; padding: 0; margin: 0; }
.session { border-bottom: 1px solid var(--border, #333); padding: 8px 0; }
.session-row { display: flex; align-items: center; gap: 8px; cursor: pointer; }
.session-name { font-weight: 500; }
.session-clear { margin-left: auto; padding: 2px 8px; font-size: 0.85rem; }
.session-clear.danger { color: #d33; }
.files { list-style: none; padding-left: 16px; margin: 4px 0 0 0; }
.files li { display: flex; align-items: center; gap: 8px; padding: 2px 0; }
.file-name { font-family: monospace; font-size: 0.875rem; }
.file-delete { margin-left: auto; font-size: 0.8rem; padding: 1px 6px; }
.file-delete.danger { color: #d33; }
.empty { color: var(--fg-dim, #999); padding: 8px 0; }

@media (max-width: 640px) {
  .header {
    flex-wrap: wrap;
    align-items: flex-start;
    gap: 8px;
  }
  .actions {
    flex-wrap: wrap;
  }
  .session-row,
  .files li {
    flex-wrap: wrap;
  }
  .file-name {
    min-width: 0;
    overflow-wrap: anywhere;
  }
}
</style>
