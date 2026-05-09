<script lang="ts" setup>
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import TabBar from "./components/TabBar.vue";
import TerminalView from "./components/TerminalView.vue";
import SettingsDialog from "./components/SettingsDialog.vue";
import RemoteSessionsDialog from "./components/RemoteSessionsDialog.vue";
import {
  closeSession,
  getEndpoint,
  getHostInfo,
  getRelayConfig,
  listShells,
  newSession,
} from "./lib/api";
import type { Endpoint } from "./lib/api";
import { fetchSessions, type SessionInfo } from "./lib/connection";

interface AggSession extends SessionInfo {
  remote: boolean;
}

const localEndpoint = ref<Endpoint | null>(null);
const remoteEndpoint = ref<Endpoint | null>(null);
const localHostID = ref<string>("");

// Two source-of-truth lists from polling. Combined into the displayed tab list
// based on which remotes the user has actively opened.
const localList = ref<SessionInfo[]>([]);
const remoteList = ref<SessionInfo[]>([]);
const openedRemotes = ref<Set<string>>(new Set());

const currentSessionId = ref<string | null>(null);
const status = ref<"loading" | "ready" | "error">("loading");
const errorMsg = ref<string>("");
const starting = ref(false);
const showSettings = ref(false);
const showRemote = ref(false);
let autoStarted = false;

let pollHandle: number | null = null;

// Sessions actually shown as tabs: all local + remote ones the user opened.
const sessions = computed<AggSession[]>(() => {
  const out: AggSession[] = localList.value.map((s) => ({ ...s, remote: false }));
  for (const r of remoteList.value) {
    if (openedRemotes.value.has(r.id)) {
      out.push({ ...r, remote: true });
    }
  }
  return out;
});

// Remote sessions still available to open — i.e. not already in tabs.
const availableRemote = computed<SessionInfo[]>(() =>
  remoteList.value.filter((r) => !openedRemotes.value.has(r.id))
);

async function pollSessions() {
  if (!localEndpoint.value) return;

  let cfg = { url: "", token: "", connected: false };
  try {
    cfg = await getRelayConfig();
  } catch {
    /* keep last known */
  }
  remoteEndpoint.value = cfg.connected && cfg.url
    ? { url: cfg.url, token: cfg.token }
    : null;

  const local = await fetchSessions(localEndpoint.value).catch(() => [] as SessionInfo[]);
  const remote: SessionInfo[] = remoteEndpoint.value
    ? await fetchSessions(remoteEndpoint.value).catch(() => [] as SessionInfo[])
    : [];

  // Dedupe by session_id: a remote entry with the same id as a local one is
  // our own session mirrored back; prefer the local (zero-latency) path.
  const localIds = new Set(local.map((s) => s.id));
  const filteredRemote = remote.filter((s) => !localIds.has(s.id));

  localList.value = local;
  remoteList.value = filteredRemote;

  // Drop any opened-remote ids that have since vanished (remote host closed
  // them or went offline), so they don't linger as broken tabs forever.
  const remoteIds = new Set(filteredRemote.map((s) => s.id));
  for (const id of Array.from(openedRemotes.value)) {
    if (!remoteIds.has(id)) {
      openedRemotes.value.delete(id);
    }
  }

  if (status.value !== "ready") status.value = "ready";
}

function endpointFor(s: AggSession): Endpoint | null {
  return s.remote ? remoteEndpoint.value : localEndpoint.value;
}

function parseHash(): string | null {
  const m = location.hash.match(/^#\/s\/([0-9a-f-]{36})$/i);
  return m ? m[1] : null;
}

function syncRoute() {
  currentSessionId.value = parseHash();
}

function gotoSession(id: string) {
  if (location.hash !== "#/s/" + id) {
    location.hash = "#/s/" + id;
  } else {
    currentSessionId.value = id;
  }
}

async function startDefaultSession() {
  if (starting.value) return;
  starting.value = true;
  errorMsg.value = "";
  try {
    const shells = await listShells();
    if (shells.length === 0) throw new Error("no shells found on this machine");
    const resp = await newSession({ command: shells[0] });
    localList.value = [
      ...localList.value,
      {
        id: resp.session_id,
        command: shells[0],
        cwd: "",
        title: shells[0],
        cols: 80,
        rows: 24,
        started_at: Math.floor(Date.now() / 1000),
        host_id: localHostID.value,
      },
    ];
    gotoSession(resp.session_id);
    pollSessions();
  } catch (e: any) {
    status.value = "error";
    errorMsg.value = e?.message ?? String(e);
  } finally {
    starting.value = false;
  }
}

function openRemote(sessionId: string) {
  // make sure the new Set reference triggers reactivity
  const next = new Set(openedRemotes.value);
  next.add(sessionId);
  openedRemotes.value = next;
  showRemote.value = false;
  gotoSession(sessionId);
}

async function onTabClose(id: string) {
  const target = sessions.value.find((s) => s.id === id);
  if (target && target.remote) {
    // Closing a remote tab is a local detach — the PTY keeps running on the
    // owning host. Just remove from openedRemotes; poll will keep the entry
    // available in the discover panel.
    const next = new Set(openedRemotes.value);
    next.delete(id);
    openedRemotes.value = next;
  } else {
    try {
      await closeSession(id);
    } catch {
      /* will reconcile via poll */
    }
    localList.value = localList.value.filter((s) => s.id !== id);
  }
  if (currentSessionId.value === id) {
    if (sessions.value.length > 0) {
      gotoSession(sessions.value[0].id);
    } else {
      location.hash = "";
    }
  }
}

watch([sessions, currentSessionId], () => {
  if (sessions.value.length === 0) return;
  const id = currentSessionId.value;
  if (id && sessions.value.find((s) => s.id === id)) return;
  gotoSession(sessions.value[0].id);
});

const sessionCount = computed(() => sessions.value.length);

onMounted(async () => {
  syncRoute();
  window.addEventListener("hashchange", syncRoute);
  try {
    localEndpoint.value = await getEndpoint();
    const info = await getHostInfo();
    localHostID.value = info.host_id;
  } catch (e: any) {
    status.value = "error";
    errorMsg.value = e?.message ?? "Wails bindings unavailable";
    return;
  }
  await pollSessions();
  pollHandle = window.setInterval(pollSessions, 2000);

  if (!autoStarted && localList.value.length === 0) {
    autoStarted = true;
    startDefaultSession();
  }
});

onUnmounted(() => {
  window.removeEventListener("hashchange", syncRoute);
  if (pollHandle !== null) window.clearInterval(pollHandle);
});
</script>

<template>
  <div class="app">
    <header class="topbar">
      <div class="brand">atterm</div>
      <div class="status">
        <template v-if="status === 'loading'">starting…</template>
        <template v-else-if="status === 'error'">
          <span class="bad">{{ errorMsg }}</span>
        </template>
        <template v-else>
          {{ sessionCount }} session{{ sessionCount === 1 ? "" : "s" }}
          <span v-if="remoteEndpoint" class="dim"> · uplink on</span>
        </template>
      </div>
      <button
        class="icon-btn"
        :title="remoteEndpoint
          ? `${availableRemote.length} remote session(s) available`
          : 'connect to a relay to see remote sessions'"
        :disabled="!remoteEndpoint"
        @click="showRemote = true"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="16" height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d="M2 16.1A5 5 0 0 1 5.9 20" />
          <path d="M2 12.05A9 9 0 0 1 9.95 20" />
          <path d="M2 8V6a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-6" />
          <line x1="2" y1="20" x2="2.01" y2="20" />
        </svg>
        <span v-if="availableRemote.length > 0" class="badge">{{ availableRemote.length }}</span>
      </button>
      <button
        class="icon-btn"
        title="relay settings"
        @click="showSettings = true"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="16" height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
          <circle cx="12" cy="12" r="3" />
        </svg>
      </button>
    </header>

    <TabBar
      :sessions="sessions"
      :current-id="currentSessionId"
      :starting="starting"
      @activate="gotoSession"
      @close="onTabClose"
      @new="startDefaultSession"
    />

    <main class="main">
      <template v-if="localEndpoint">
        <div v-if="sessions.length === 0" class="empty">
          starting first session…
        </div>
        <TerminalView
          v-for="s in sessions"
          v-show="s.id === currentSessionId"
          :key="s.id + '|' + (endpointFor(s)?.url ?? '')"
          :endpoint="endpointFor(s)!"
          :session-id="s.id"
          :active="s.id === currentSessionId"
        />
      </template>
    </main>

    <SettingsDialog
      v-if="showSettings"
      @close="showSettings = false"
    />
    <RemoteSessionsDialog
      v-if="showRemote"
      :sessions="availableRemote"
      @open="openRemote"
      @close="showRemote = false"
    />
  </div>
</template>

<style scoped>
.app {
  display: flex;
  flex-direction: column;
  height: 100vh;
}
.topbar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 16px;
  background: var(--panel);
  border-bottom: 1px solid var(--border);
  flex: 0 0 auto;
}
.brand {
  font-weight: 600;
  letter-spacing: 0.06em;
}
.status {
  margin-left: auto;
  font-size: 12px;
  color: var(--fg-dim);
}
.status .bad { color: var(--bad); }
.status .dim { color: var(--good); }

.icon-btn {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: transparent;
  color: var(--fg-dim);
  line-height: 1;
  padding: 6px 8px;
  border-radius: 6px;
  cursor: pointer;
  transition: color 120ms, background 120ms;
}
.icon-btn svg { display: block; }
.icon-btn:hover:not(:disabled) {
  color: var(--accent);
  background: rgba(88, 166, 255, 0.08);
}
.icon-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.icon-btn .badge {
  position: absolute;
  top: -2px;
  right: -2px;
  background: #d29922;
  color: #0d1117;
  font-size: 9px;
  font-weight: 700;
  border-radius: 10px;
  padding: 1px 5px;
  line-height: 1.3;
  min-width: 16px;
  text-align: center;
}

.main {
  flex: 1 1 auto;
  position: relative;
  background: #000;
  overflow: hidden;
}
.empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-dim);
  font-size: 13px;
}
</style>
