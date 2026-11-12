<script lang="ts" setup>
import { ref, computed, onMounted, onBeforeUnmount, watch } from "vue";
import {
  Server, Plus, X, Pencil, Trash2, Search, KeyRound, Upload, FileUp, FileDown,
  Network, LayoutGrid, List, ArrowDownWideNarrow, Square, RefreshCw, Eye, EyeOff, Copy, Folder, ChevronRight, ChevronDown, ArrowLeft,
} from "lucide-vue-next";
import { browseTunnels, type TunnelSort } from "../lib/sshTunnelBrowser";
import TermiusImportDrawer from "./TermiusImportDrawer.vue";
import type { TermiusImportReport } from "../lib/api";
import SelectDropdown, { type SelectOption } from "./SelectDropdown.vue";
import SessionRowMenu, { type MenuItem } from "./SessionRowMenu.vue";
import { readPrivateKeyFile } from "../lib/sshKeyFile";
import { sshCommandFor } from "../lib/sshCommand";
import { allHostTags, hostHasAllTags, normalizeTags, parseTagInput } from "../lib/hostTags";
import { fallbackCopyText } from "../lib/terminalCopy";
import { useI18n } from "../i18n/useI18n";
import {
  listSSHHosts,
  addSSHHost,
  updateSSHHost,
  deleteSSHHost,
  newSshSessionByID,
  listSSHKeys,
  addSSHKey,
  updateSSHKey,
  deleteSSHKey,
  revealSSHKey,
  previewSSHConfigImport,
  importSSHHosts,
  startForward,
  stopForward,
  listActiveForwards,
  type AcceptedHostKey,
  type SSHHost,
  type SSHKey,
  type SSHConfigImportPreview,
  type ForwardRule,
  type ActiveForward,
} from "../lib/api";
import { parseHostKeyPrompt, type HostKeyPrompt } from "../lib/sshHostKey";

const emit = defineEmits<{
  (e: "connected", sessionId: string): void;
  (e: "close"): void;
}>();

const { t } = useI18n();

type Tab = "hosts" | "keys" | "tunnels";
const activeTab = ref<Tab>("hosts");

const hosts = ref<SSHHost[]>([]);
const keys = ref<SSHKey[]>([]);
const errorMsg = ref("");
const query = ref("");
const connectingId = ref("");

async function reload() {
  [hosts.value, keys.value] = await Promise.all([listSSHHosts(), listSSHKeys()]);
}

// The key drawer invites the user to drag a file in. A near-miss drop would
// otherwise reach the webview, which navigates to the dropped file and wipes
// the app view. Swallow stray drags for as long as this modal is up; the drop
// zone's own handler has already run by the time these fire.
function swallowStrayDrag(e: Event) {
  e.preventDefault();
}
// Active tunnels are polled while the panel is open rather than pushed: the
// interesting transitions (a dropped SSH connection stopping every tunnel on
// it) happen in Go with no event to subscribe to, and this panel is a modal
// the user has open for seconds at a time.
const FORWARD_POLL_MS = 3000;
let forwardsPoll: ReturnType<typeof setInterval> | undefined;

onMounted(() => {
  window.addEventListener("dragover", swallowStrayDrag);
  window.addEventListener("drop", swallowStrayDrag);
  void reload();
  void refreshActiveForwards();
  forwardsPoll = setInterval(() => void refreshActiveForwards(), FORWARD_POLL_MS);
});
onBeforeUnmount(() => {
  window.removeEventListener("dragover", swallowStrayDrag);
  window.removeEventListener("drop", swallowStrayDrag);
  if (forwardsPoll !== undefined) clearInterval(forwardsPoll);
});

// ---- Hosts ----
// A path is an array: labels containing slashes remain a single group name.
const selectedGroupPath = ref<string[]>([]);
const availableTags = computed(() => allHostTags(hosts.value));
const sameGroupPath = (a: string[], b: string[]) =>
  a.length === b.length && a.every((part, i) => part === b[i]);
const withinGroup = (path: string[], parent: string[]) =>
  parent.every((part, i) => path[i] === part);
const hostGroups = computed(() => {
  const groups = new Map<string, number>();
  for (const host of hosts.value) {
    if (host.group_path?.length) {
      if (!withinGroup(host.group_path, selectedGroupPath.value)) continue;
      const child = host.group_path[selectedGroupPath.value.length];
      if (child !== undefined) groups.set(child, (groups.get(child) ?? 0) + 1);
    } else if (!selectedGroupPath.value.length) {
      // Existing manually tagged hosts keep their previous flat navigation.
      for (const tag of normalizeTags(host.tags ?? [])) groups.set(tag, (groups.get(tag) ?? 0) + 1);
    }
  }
  return [...groups].map(([name, count]) => ({ name, count })).sort((a, b) => a.name.localeCompare(b.name));
});
const filteredHosts = computed(() => {
  const q = query.value.trim().toLowerCase();
  return hosts.value.filter((h) => {
    if (h.group_path?.length) {
      if (q ? !withinGroup(h.group_path, selectedGroupPath.value) : !sameGroupPath(h.group_path, selectedGroupPath.value)) return false;
    } else if (!hostHasAllTags(h.tags, selectedGroupPath.value)) return false;
    if (q === "") return true;
    return `${h.alias ?? ""} ${h.host} ${h.user} ${(h.tags ?? []).join(" ")} ${(h.group_path ?? []).join(" ")}`.toLowerCase().includes(q);
  });
});
function enterGroup(tag: string) {
  selectedGroupPath.value = [...selectedGroupPath.value, tag];
}
function goToRoot() {
  selectedGroupPath.value = [];
}

const keyOptions = computed<SelectOption[]>(() =>
  keys.value.map((k) => ({
    value: k.id,
    label: k.key_type ? `${k.name} (${k.key_type})` : k.name,
  })),
);

// Tags already in use elsewhere, minus the ones this host already carries —
// offered as suggestions in the host form. Typing a brand-new tag also works.
const tagMenuOpen = ref(false);
const tagSuggestions = computed<string[]>(() => {
  const q = fTagInput.value.trim().toLowerCase();
  const owned = new Set(fTags.value.map((t) => t.toLowerCase()));
  return availableTags.value.filter(
    (t) => !owned.has(t.toLowerCase()) && (q === "" || t.toLowerCase().includes(q)),
  );
});
function addFormTags(raw: string[]) {
  fTags.value = normalizeTags([...fTags.value, ...raw]);
}
function commitTagInput() {
  addFormTags(parseTagInput(fTagInput.value));
  fTagInput.value = "";
}
function pickTag(t: string) {
  addFormTags([t]);
  fTagInput.value = "";
  tagMenuOpen.value = false;
}
function removeFormTag(t: string) {
  fTags.value = fTags.value.filter((x) => x !== t);
}
// Backspace on an empty input peels off the last chip, the usual tag-field feel.
function onTagBackspace() {
  if (fTagInput.value === "") fTags.value = fTags.value.slice(0, -1);
}
function onTagBlur() {
  // Commit whatever is half-typed. Without this, tabbing out of the field (or
  // going straight for Save) silently threw the tag away — the text was
  // visibly *there*, so the only signal that it had not been added was the
  // missing chip, which is exactly the thing you are not looking at while
  // typing. Enter and comma still commit as before; this just stops leaving
  // by any other route from being a data-loss route.
  //
  // It cannot swallow a suggestion click: the options commit through
  // @mousedown.prevent, which suppresses the blur entirely.
  commitTagInput();
  // Delay so a mousedown on a suggestion registers before the menu closes.
  window.setTimeout(() => (tagMenuOpen.value = false), 120);
}

function hostLabel(h: SSHHost): string {
  return h.alias?.trim() || `${h.user}@${h.host}`;
}
function hostSubtitle(h: SSHHost): string {
  const port = h.port && h.port !== "22" ? `:${h.port}` : "";
  const auth = h.auth_kind === "key" ? t("ssh.subtitleAuthKey") : t("ssh.subtitleAuthPassword");
  return `${h.user}@${h.host}${port} · ${auth}`;
}

// ---- Proxied hosts ----
// ssh_config puts two very different things behind the same "proxy" word, and
// roadmap item 27 made them opposites.
//
// ProxyJump is connectable now. The backend dials the chain a hop at a time,
// and every hop has to be a saved host in atterm because a bastion needs its
// *own* credential — atterm will not send this host's password to a different
// machine. The route stays on the row, because which machines a connection
// passes through is worth seeing before it happens, but it is information now
// rather than a refusal.
//
// ProxyCommand is still refused, and always will be: running an arbitrary
// command to open a connection is an RCE surface atterm does not offer. This is
// the one the list still has to stop before the click reaches Go.
type ProxyFields = Pick<SSHHost, "proxy_jump" | "proxy_command" | "proxy">;

// runsProxyCommand is the *blocking* check, and the only one. It deliberately
// does not look at proxy_jump: this predicate used to be `jump || command`, and
// while it stayed that way the chain support was unreachable from the UI — the
// panel short-circuited before the RPC, so a working backend never got asked.
function runsProxyCommand(h: ProxyFields): boolean {
  // Older desktop clients refuse this marker; current clients use metadata.
  return !!h.proxy_command && !(h.proxy_command === "atterm:native-proxy:v1" && h.proxy);
}
// Whether the row carries a proxy marker at all — a chain to show, or a refusal.
function hasProxyMarker(h: ProxyFields): boolean {
  return !!(h.proxy_jump || h.proxy_command || h.proxy);
}
// Short pill text for the host row / preview row. Checks proxy_command first,
// via runsProxyCommand: a host can carry both fields (sshconfig parses them
// separately, and the importer keeps both), and ProxyCommand is the one that
// blocks the connection. Reading proxy_jump first on such a host would show
// the affirmative "via bastion" badge on a row Connect refuses to open.
function jumpRouteLabel(route: string): string {
  return route.split(',').map(part => {
    const token = part.trim();
    if (!token.startsWith('atterm-id:')) return token;
    const host = hosts.value.find(h => h.id === token.slice('atterm-id:'.length));
    return host?.alias || host?.host || t('ssh.termius.missingHop');
  }).join(' → ');
}
function proxyLabel(h: ProxyFields): string {
  if (!runsProxyCommand(h) && h.proxy) return `${h.proxy.kind.toUpperCase()} · ${h.proxy.host}:${h.proxy.port}`;
  return runsProxyCommand(h)
    ? t("ssh.proxy.commandBadge")
    : t("ssh.proxy.jumpBadge", { target: jumpRouteLabel(h.proxy_jump ?? "") });
}
// Full sentence for tooltips and the error line. Same ordering as
// proxyLabel, and for the same reason: a host with both fields set must read
// as the refusal it is, not as the jump sentence that happens to also be
// true.
function proxyReason(h: ProxyFields): string {
  if (runsProxyCommand(h)) {
    return t("ssh.proxy.commandReason", { command: h.proxy_command ?? "" });
  }
  if (h.proxy) return t("ssh.termius.proxyRoute", { route: proxyLabel(h) });
  if (h.proxy_jump) {
    return t("ssh.proxy.jumpReason", { target: jumpRouteLabel(h.proxy_jump) });
  }
  return "";
}

// ---- Unknown host keys (TOFU) ----
// Each hop asks separately; retain the intended action across confirmations.
type HostKeyConfirmation = { host: SSHHost; prompt: HostKeyPrompt; rule?: ForwardRule };
// Concurrent tunnel attempts can each ask about a different host. Keep every
// question until answered rather than letting the last response replace it.
const hostKeyQueue = ref<HostKeyConfirmation[]>([]);
const hostKey = computed(() => hostKeyQueue.value[0] ?? null);
function queueHostKey(state: HostKeyConfirmation) {
  const index = hostKeyQueue.value.findIndex((pending) => pending.host.id === state.host.id && pending.rule?.id === state.rule?.id);
  if (index >= 0) hostKeyQueue.value[index] = state;
  else hostKeyQueue.value.push(state);
}
function clearHostKey(hostID: string, ruleID?: string) {
  hostKeyQueue.value = hostKeyQueue.value.filter((pending) => pending.host.id !== hostID || pending.rule?.id !== ruleID);
}
const hostKeyBusy = computed(() => {
  const state = hostKey.value;
  return state?.rule ? isForwardBusy(state.host.id, state.rule.id) : !!connectingId.value;
});

// jumpLabel mirrors Go's sshHostLabel (desktop/ssh_jump.go): the alias when
// there is one, the hostname otherwise. HopName comes out of that function, so
// this is what decides whether a prompt is about the destination or about a
// bastion in front of it.
function jumpLabel(h: SSHHost): string {
  return (h.alias ?? "").trim() || (h.host ?? "").trim();
}

const hostKeyMessage = computed(() => {
  const state = hostKey.value;
  if (!state) return "";
  const { prompt } = state;
  // hopIndex 0 is a direct connection with no chain to disambiguate, so it
  // reads exactly as it did before jump hosts existed. (Only hopIndex decides
  // this — Go populates HopName for a direct saved host too.)
  if (prompt.hopIndex === 0) return t("ssh.hostKey.direct");
  const target = jumpLabel(state.host);
  if (prompt.hopName === target) {
    return t("ssh.hostKey.targetHop", { target, hop: prompt.hopIndex });
  }
  return t("ssh.hostKey.jumpHop", { name: prompt.hopName, hop: prompt.hopIndex, target });
});

// acceptHostKey retries with the pair the dialog showed, both halves verbatim.
// Rebuilding either one is how the scoping breaks: prompt.host is the address
// the backend actually dialled ("10.0.0.9:22"), which for a jump hop is not
// the address on the row at all. It is not a known_hosts name and must not be
// normalised into one — an acceptance that does not match is silently ignored,
// which looks exactly like the dialog refusing to go away.
function acceptHostKey() {
  const state = hostKey.value;
  if (!state || hostKeyBusy.value) return;
  hostKeyQueue.value.shift();
  const accepted = {
    host: state.prompt.host,
    fingerprint: state.prompt.fingerprint,
  };
  if (state.rule) void startRule(state.host, state.rule, accepted);
  else void connect(state.host.id, accepted);
}
function rejectHostKey() {
  hostKeyQueue.value.shift();
}

// accepted defaults to the pair that matches nothing, so every affordance that
// is not the TOFU dialog's own button fails closed.
async function connect(id: string, accepted: AcceptedHostKey = { host: "", fingerprint: "" }) {
  if (connectingId.value) return;
  errorMsg.value = "";
  // Never dial a ProxyCommand host, whichever affordance got us here (button,
  // double-click, context menu). The backend refuses too; stopping here just
  // makes the reason arrive without a round trip.
  const target = hosts.value.find((h) => h.id === id);
  if (target && runsProxyCommand(target)) {
    errorMsg.value = proxyReason(target);
    return;
  }
  clearHostKey(id);
  connectingId.value = id;
  try {
    const resp = await newSshSessionByID(id, accepted);
    emit("connected", resp.session_id);
  } catch (e) {
    // A prompt arriving *after* an acceptance is the next hop asking, not the
    // one just answered — so replace the question rather than closing it.
    const prompt = parseHostKeyPrompt(e);
    if (prompt && target) {
      queueHostKey({ host: target, prompt });
      return;
    }
    errorMsg.value = e instanceof Error ? e.message : String(e);
  } finally {
    connectingId.value = "";
  }
}

// ---- Host context menu ----
const hostMenu = ref<{ open: boolean; x: number; y: number; host: SSHHost | null }>({
  open: false,
  x: 0,
  y: 0,
  host: null,
});

// Computed, not a module-level const: the labels have to re-resolve when the
// user switches language while the panel is open.
const hostMenuItems = computed<MenuItem[]>(() => [
  { key: "connect", label: t("ssh.hosts.menuConnect") },
  { key: "edit", label: t("ssh.hosts.menuEdit") },
  { key: "duplicate", label: t("ssh.hosts.menuDuplicate") },
  { key: "copy-ssh", label: t("ssh.hosts.menuCopySsh") },
  { key: "remove", label: t("ssh.hosts.menuRemove"), separatorBefore: true },
]);

function openHostMenu(e: MouseEvent, h: SSHHost) {
  hostMenu.value = { open: true, x: e.clientX, y: e.clientY, host: h };
}
function closeHostMenu() {
  hostMenu.value = { ...hostMenu.value, open: false, host: null };
}

async function onHostMenuSelect(key: string) {
  const h = hostMenu.value.host;
  if (!h) return;
  switch (key) {
    case "connect":
      await connect(h.id);
      break;
    case "edit":
      openEditHost(h);
      break;
    case "duplicate":
      await duplicateHost(h);
      break;
    case "copy-ssh":
      await copySshCommand(h);
      break;
    case "remove":
      await removeHost(h.id);
      break;
  }
}

// duplicateHost copies every non-secret field. The password lives in the
// keyring and cannot be read back, so a password host's copy starts without
// one — open the drawer in that case rather than leaving a host that silently
// fails to connect.
//
// The " copy" suffix is deliberately not translated: it becomes the stored
// alias, which syncs to every other device. Localizing it would mean the same
// host reads differently depending on which machine duplicated it, and the
// user can rename it in the drawer anyway.
async function duplicateHost(h: SSHHost) {
  errorMsg.value = "";
  const copy: SSHHost = {
    ...h,
    id: "",
    alias: h.alias?.trim() ? `${h.alias.trim()} copy` : "",
  };
  try {
    const created = await addSSHHost(copy, {});
    await reload();
    if (h.auth_kind === "password") openEditHost(created ?? copy);
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}

async function copySshCommand(h: SSHHost) {
  const text = sshCommandFor({ user: h.user, host: h.host, port: h.port });
  const clipboard = typeof navigator === "undefined" ? undefined : navigator.clipboard;
  if (clipboard?.writeText) {
    await clipboard.writeText(text);
    return;
  }
  fallbackCopyText(text);
}

// ---- Host drawer ----
const hostDrawer = ref(false);
const hostEditId = ref<string | null>(null);
// The record the drawer was opened on, kept whole. The form refs below only
// cover the fields the drawer edits; identity_file / proxy_jump /
// proxy_command have no control and would be dropped from the save payload
// if it were built from the refs alone — silently stripping the
// not-directly-connectable gate off an imported host at the exact moment the
// user has to open this drawer to give it a credential. Same shape as
// duplicateHost's {...h}.
const editingHost = ref<SSHHost | null>(null);
const fAlias = ref("");
const fHost = ref("");
const fPort = ref("22");
const fUser = ref("");
const fTags = ref<string[]>([]);
const fTagInput = ref("");
const fAuthKind = ref<"password" | "key">("password");
const fPassword = ref("");
const fKeyID = ref("");
// Startup command: the lines typed into the shell after login, one per
// prompt, and the pause before each. Empty delay means "use the backend
// default" — the field is left blank rather than pre-filled with 1500 so that
// changing the default later reaches hosts the user never touched.
const fStartupCommand = ref("");
const fStartupDelayMs = ref("");

// startupDelayValue parses the delay field for the save payload. Anything
// unparseable becomes 0, which the Go side reads as "default" — the same
// answer as leaving it blank, so a typo cannot produce a host that types its
// startup command 3ms after login.
function startupDelayValue(): number {
  const n = Number.parseInt(fStartupDelayMs.value.trim(), 10);
  return Number.isFinite(n) && n > 0 ? n : 0;
}
// Mirrors startupCommandLines in desktop/ssh_startup_command.go: blank lines
// are dropped, so the preview counts the steps that will actually be sent.
const startupStepCount = computed(
  () => fStartupCommand.value.split("\n").filter((l) => l.trim() !== "").length,
);

function openNewHost() {
  hostEditId.value = "";
  editingHost.value = null;
  fAlias.value = "";
  fHost.value = "";
  fPort.value = "22";
  fUser.value = "";
  fTags.value = [];
  fTagInput.value = "";
  fAuthKind.value = "password";
  fPassword.value = "";
  fKeyID.value = keys.value[0]?.id ?? "";
  fStartupCommand.value = "";
  fStartupDelayMs.value = "";
  fForwards.value = [];
  hostDrawer.value = true;
}
function openEditHost(h: SSHHost) {
  hostEditId.value = h.id;
  editingHost.value = { ...h };
  fAlias.value = h.alias ?? "";
  fHost.value = h.host;
  fPort.value = h.port || "22";
  fUser.value = h.user;
  fTags.value = normalizeTags(h.tags ?? []);
  fTagInput.value = "";
  fAuthKind.value = h.auth_kind;
  fPassword.value = "";
  fKeyID.value = h.key_id ?? keys.value[0]?.id ?? "";
  fStartupCommand.value = h.startup_command ?? "";
  fStartupDelayMs.value = h.startup_delay_ms ? String(h.startup_delay_ms) : "";
  fForwards.value = cloneForwards(h.forwards);
  hostDrawer.value = true;
}
function closeHostDrawer() {
  hostDrawer.value = false;
  hostEditId.value = null;
  editingHost.value = null;
}
const canSaveHost = computed(() => {
  if (fHost.value.trim() === "" || fUser.value.trim() === "") return false;
  if (fAuthKind.value === "key") return fKeyID.value !== "";
  return true;
});
async function saveHost() {
  if (!canSaveHost.value) return;
  errorMsg.value = "";
  const base = {
    alias: fAlias.value.trim(),
    host: fHost.value.trim(),
    port: fPort.value.trim() || "22",
    user: fUser.value.trim(),
    tags: fTags.value,
    group_path: editingHost.value?.group_path ?? [...selectedGroupPath.value],
    auth_kind: fAuthKind.value,
    key_id: fAuthKind.value === "key" ? fKeyID.value : undefined,
    // Same ownership rule as forwards below: the drawer has a control for
    // both, so the payload always carries them and clearing one sticks.
    startup_command: fStartupCommand.value,
    startup_delay_ms: startupDelayValue(),
    // The drawer owns Forwards, and UpdateSSHHost lets the caller's value
    // win — including an empty list, so deleting the last rule is a saveable
    // edit. The flip side is that a payload built without this line deletes
    // every rule the host had, on every device. That is the shape of the
    // item-25 bug that silently stripped proxy_jump; it is pinned by a test.
    forwards: cloneForwards(fForwards.value),
  };
  const cred = { password: fAuthKind.value === "password" ? fPassword.value : undefined };
  const hasNewCred = fAuthKind.value === "password" && fPassword.value !== "";
  try {
    if (hostEditId.value) {
      // Spread the record we opened on first so fields the form does not own
      // survive the round trip; base then overrides everything it does own.
      await updateSSHHost(
        { ...(editingHost.value ?? {}), id: hostEditId.value, ...base },
        hasNewCred ? cred : null,
      );
    } else {
      await addSSHHost({ id: "", ...base }, cred);
    }
    closeHostDrawer();
    await reload();
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}
async function removeHost(id: string) {
  errorMsg.value = "";
  try {
    await deleteSSHHost(id);
    await reload();
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}

// ---- Port forwarding: rules editor (host drawer) ----
// Rules are configuration, tunnels are an action, and this is the
// configuration half (design doc §5.3). The drawer owns the whole list: it is
// copied out of the record on open and copied back on save, so adding,
// editing and deleting a rule are all just edits to fForwards.
const fForwards = ref<ForwardRule[]>([]);

// cloneForwards deep-copies and normalises. The optional string fields become
// "" so a v-model can never write undefined into the save payload, and so
// editing a rule never mutates the loaded host record in place.
function cloneForwards(list: ForwardRule[] | undefined): ForwardRule[] {
  return (list ?? []).map((r) => ({
    id: r.id,
    kind: r.kind,
    bind_addr: r.bind_addr ?? "",
    bind_port: r.bind_port ?? "",
    target_host: r.target_host ?? "",
    target_port: r.target_port ?? "",
    note: r.note ?? "",
  }));
}

// Rule IDs are minted here because the Go side does not assign them:
// StartForward looks a rule up by (hostID, ruleID), so a rule saved without
// one could never be started.
function newForwardRuleID(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `fwd-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

function addForwardRule() {
  fForwards.value = [
    ...fForwards.value,
    {
      id: newForwardRuleID(),
      kind: "local",
      // Spelled out rather than left empty: the safe value should be visible
      // in the field the user is about to change.
      bind_addr: "127.0.0.1",
      bind_port: "",
      target_host: "",
      target_port: "",
      note: "",
    },
  ];
}
function removeForwardRule(id: string) {
  fForwards.value = fForwards.value.filter((r) => r.id !== id);
}
function setForwardKind(rule: ForwardRule, kind: ForwardRule["kind"]) {
  rule.kind = kind;
}
function forwardKindHint(kind: string): string {
  if (kind === "remote") return t("ssh.forwards.kindRemoteHint");
  if (kind === "dynamic") return t("ssh.forwards.kindDynamicHint");
  return t("ssh.forwards.kindLocalHint");
}

// isLoopbackBind mirrors defaultForwardBindAddr's contract: empty means the
// backend fills in 127.0.0.1, and only loopback keeps the listener private to
// this machine.
// The 127/8 test is anchored at both ends and spelled out octet by octet on
// purpose: /^127\./ also matches the hostname 127.0.0.1.example.com, which
// resolves anywhere its owner likes and would have suppressed the warning.
// net.Listen would refuse that value with a clear error rather than bind
// anything wide, so this is not a hole — but a check that only claims what it
// can prove costs nothing.
function isLoopbackBind(addr: string | undefined): boolean {
  const a = (addr ?? "").trim().toLowerCase();
  if (a === "") return true;
  if (a === "localhost" || a === "::1" || a === "[::1]") return true;
  const m = /^127\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(a);
  return m !== null && m.slice(1).every((o) => Number(o) <= 255);
}

// forwardBindWarning is the §5.1 warning, and it is deliberately three
// different sentences. A non-loopback *local* rule hands one service to
// everyone on the network with no SSH credential; a non-loopback *dynamic*
// rule is an unauthenticated SOCKS5 open proxy into everything the SSH host
// can reach, using our credential. Warning about the second with the first's
// wording would understate it by a lot. There is deliberately no "allow LAN
// access" toggle: this is a value the user types, not a convenience switch.
function forwardBindWarning(r: ForwardRule): string {
  if (isLoopbackBind(r.bind_addr)) return "";
  const addr = (r.bind_addr ?? "").trim();
  if (r.kind === "dynamic") return t("ssh.forwards.warnDynamic", { addr });
  if (r.kind === "remote") return t("ssh.forwards.warnRemote", { addr });
  return t("ssh.forwards.warnLocal", { addr });
}

// ---- Port forwarding: active tunnels (Tunnels tab) ----
const activeForwards = ref<ActiveForward[]>([]);
const forwardOperations = ref(new Map<string, "starting" | "stopping">());
function isForwardBusy(hostID: string, ruleID: string): boolean {
  return forwardOperations.value.has(forwardKey(hostID, ruleID));
}

function forwardKey(hostID: string, ruleID: string): string {
  return `${hostID}/${ruleID}`;
}

async function refreshActiveForwards() {
  try {
    activeForwards.value = (await listActiveForwards()) ?? [];
  } catch {
    // This runs on a timer. Writing a transport hiccup into errorMsg would
    // wipe whatever message the user is currently reading, so a failed poll
    // just leaves the previous snapshot on screen.
  }
}

// activeByKey indexes *every* entry, running or not: ListActiveForwards also
// returns tunnels that stopped by themselves (a dropped SSH connection), and
// losing those would erase the only place their reason is shown.
const activeByKey = computed(() => {
  const m = new Map<string, ActiveForward>();
  for (const a of activeForwards.value) m.set(forwardKey(a.host_id, a.rule_id), a);
  return m;
});
function forwardState(hostID: string, ruleID: string): ActiveForward | undefined {
  return activeByKey.value.get(forwardKey(hostID, ruleID));
}
function isForwardRunning(hostID: string, ruleID: string): boolean {
  return forwardState(hostID, ruleID)?.running === true;
}

const forwardRows = computed(() => hosts.value.flatMap((host) =>
  (host.forwards ?? []).map((rule) => ({ host, rule })),
));
const savedForwardCount = computed(() => forwardRows.value.length);

const tunnelQuery = ref("");
function readTunnelPreference(key: string, allowed: string[], fallback: string): string {
  try { const saved = localStorage.getItem(key); return saved && allowed.includes(saved) ? saved : fallback; }
  catch { return fallback; }
}
const tunnelLayout = ref(readTunnelPreference("atterm.ssh.tunnels.layout", ["grid", "list"], "grid"));
const tunnelSort = ref(readTunnelPreference("atterm.ssh.tunnels.sort", ["newest", "oldest", "name-asc", "name-desc"], "newest"));
watch([tunnelLayout, tunnelSort], ([layout, sort]) => {
  try {
    localStorage.setItem("atterm.ssh.tunnels.layout", layout);
    localStorage.setItem("atterm.ssh.tunnels.sort", sort);
  } catch { /* Browsing remains available when browser storage is disabled. */ }
});
const tunnelLayoutOptions = computed<SelectOption[]>(() => [
  { value: "grid", label: t("ssh.forwards.layoutGrid") },
  { value: "list", label: t("ssh.forwards.layoutList") },
]);
const tunnelSortOptions = computed<SelectOption[]>(() => [
  { value: "name-asc", label: t("ssh.forwards.sortNameAsc") },
  { value: "name-desc", label: t("ssh.forwards.sortNameDesc") },
  { value: "newest", label: t("ssh.forwards.sortNewest") },
  { value: "oldest", label: t("ssh.forwards.sortOldest") },
]);
const visibleForwardRows = computed(() => browseTunnels(forwardRows.value, tunnelQuery.value, tunnelSort.value as TunnelSort));

function forwardKindLabel(kind: string): string {
  if (kind === "remote") return t("ssh.forwards.kindRemote");
  if (kind === "dynamic") return t("ssh.forwards.kindDynamic");
  return t("ssh.forwards.kindLocal");
}

// The listen address of a *running* tunnel comes from ActiveForward, not from
// the rule: that is the listener's real resolved address, so a defaulted or
// ephemeral bind reports what it actually became.
function forwardListenText(h: SSHHost, r: ForwardRule): string {
  const a = forwardState(h.id, r.id);
  if (a?.running && a.listen_addr) return a.listen_addr;
  const addr = (r.bind_addr ?? "").trim() || "127.0.0.1";
  return `${addr}:${(r.bind_port ?? "").trim()}`;
}
function forwardTargetText(h: SSHHost, r: ForwardRule): string {
  const a = forwardState(h.id, r.id);
  if (a?.running) {
    return a.target?.trim() ? a.target : t("ssh.forwards.dynamicTarget");
  }
  // A dynamic rule has no configured destination at all — the SOCKS client
  // names one per connection — so printing target_host:target_port here would
  // be inventing a route that does not exist.
  if (r.kind === "dynamic") return t("ssh.forwards.dynamicTarget");
  return `${(r.target_host ?? "").trim()}:${(r.target_port ?? "").trim()}`;
}
function forwardStateLabel(h: SSHHost, r: ForwardRule): string {
  const operation = forwardOperations.value.get(forwardKey(h.id, r.id));
  if (operation) return t(operation === "starting" ? "ssh.forwards.stateConnecting" : "ssh.forwards.stateStopping");
  if (hostKeyQueue.value.some((pending) => pending.host.id === h.id && pending.rule?.id === r.id)) return t("ssh.forwards.stateConfirm");
  const a = forwardState(h.id, r.id);
  if (!a) return t("ssh.forwards.stateIdle");
  return a.running ? t("ssh.forwards.stateRunning") : t("ssh.forwards.stateStopped");
}
function forwardStateClass(h: SSHHost, r: ForwardRule): string {
  if (isForwardBusy(h.id, r.id)) return "connecting";
  const a = forwardState(h.id, r.id);
  if (!a) return "idle";
  return a.running ? "running" : "stopped";
}

// startRule is the only thing that brings a tunnel up — nothing starts on
// connect (§5.3), because a tunnel occupies a local port and opening a
// terminal must not silently grab 5432.
async function startRule(h: SSHHost, r: ForwardRule, accepted?: AcceptedHostKey) {
  // Only ProxyCommand is refused here. A ProxyJump host's tunnel rides the same
  // chain the terminal does (item 27), and this guard blocking it would make
  // the UI refuse something the backend does — the greyed button would be the
  // whole reason the feature looked unimplemented.
  if (runsProxyCommand(h) || isForwardBusy(h.id, r.id) || isForwardRunning(h.id, r.id)) return;
  errorMsg.value = "";
  clearHostKey(h.id, r.id);
  forwardOperations.value.set(forwardKey(h.id, r.id), "starting");
  try {
    await startForward(h.id, r.id, accepted);
  } catch (e) {
    const prompt = parseHostKeyPrompt(e);
    if (prompt) {
      queueHostKey({ host: h, rule: r, prompt });
      return;
    }
    errorMsg.value = e instanceof Error ? e.message : String(e);
  } finally {
    await refreshActiveForwards();
    forwardOperations.value.delete(forwardKey(h.id, r.id));
  }
}

// stopRule closes a running tunnel and also clears an entry that already
// stopped by itself, which is why the dead-tunnel row offers it as "dismiss".
async function stopRule(h: SSHHost, r: ForwardRule) {
  if (isForwardBusy(h.id, r.id)) return;
  errorMsg.value = "";
  forwardOperations.value.set(forwardKey(h.id, r.id), "stopping");
  try {
    await stopForward(h.id, r.id);
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  } finally {
    await refreshActiveForwards();
    forwardOperations.value.delete(forwardKey(h.id, r.id));
  }
}

function openTunnelsTab() {
  activeTab.value = "tunnels";
  void refreshActiveForwards();
}

// ---- Port forwarding: create / edit a rule from the Tunnels tab ----
// A rule lives on a host, so until now the only way to add one was to go find
// that host and open its drawer — which is backwards when what you are doing
// is "I need a tunnel", not "I am editing this host". This drawer inverts it:
// pick the host (the bastion the tunnel rides), then describe the rule.
//
// It writes through UpdateSSHHost like the host drawer does, and for the same
// reason has to send the host record *whole*: a payload built from the rule
// alone would clear every other field the store lets the caller own.
const tunnelDrawer = ref(false);
const tunnelHostID = ref("");
const tunnelEditRuleID = ref("");
const tunnelSaving = ref(false);
const tunnelDraft = ref<ForwardRule | null>(null);

// Hosts a tunnel can actually be built on. A ProxyCommand host is absent
// rather than present-and-broken: StartForward refuses it outright, so
// offering it here would only produce a saved rule that can never run.
const tunnelHostOptions = computed<SelectOption[]>(() =>
  hosts.value
    .filter((h) => !runsProxyCommand(h))
    .map((h) => ({ value: h.id, label: hostLabel(h) })),
);

function openNewTunnel() {
  errorMsg.value = "";
  tunnelEditRuleID.value = "";
  tunnelHostID.value = tunnelHostOptions.value[0]?.value ?? "";
  tunnelDraft.value = {
    id: newForwardRuleID(),
    kind: "local",
    bind_addr: "127.0.0.1",
    bind_port: "",
    target_host: "",
    target_port: "",
    note: "",
  };
  tunnelDrawer.value = true;
}

// The host picker is locked while editing. Moving a rule between hosts would
// be two writes to two different records, and a half-applied move leaves the
// rule on both — not worth the affordance when "delete and re-add" says the
// same thing in two clicks.
function openEditTunnel(h: SSHHost, r: ForwardRule) {
  errorMsg.value = "";
  tunnelEditRuleID.value = r.id;
  tunnelHostID.value = h.id;
  tunnelDraft.value = cloneForwards([r])[0];
  tunnelDrawer.value = true;
}

function closeTunnelDrawer() {
  tunnelDrawer.value = false;
  tunnelDraft.value = null;
  tunnelEditRuleID.value = "";
}

const canSaveTunnel = computed(() => {
  const r = tunnelDraft.value;
  if (!r || tunnelHostID.value === "") return false;
  if ((r.bind_port ?? "").trim() === "") return false;
  // Dynamic names its destination per connection, so it has none to require.
  if (r.kind === "dynamic") return true;
  return (r.target_host ?? "").trim() !== "" && (r.target_port ?? "").trim() !== "";
});

// saveTunnel returns the rule it wrote so "save and start" can start exactly
// that one, and undefined when the save did not happen.
async function saveTunnel(): Promise<{ host: SSHHost; rule: ForwardRule } | undefined> {
  const draft = tunnelDraft.value;
  if (!canSaveTunnel.value || !draft || tunnelSaving.value) return undefined;
  const host = hosts.value.find((h) => h.id === tunnelHostID.value);
  if (!host) return undefined;
  errorMsg.value = "";
  tunnelSaving.value = true;
  const rule = cloneForwards([draft])[0];
  const existing = cloneForwards(host.forwards);
  const at = existing.findIndex((r) => r.id === rule.id);
  const forwards = at >= 0
    ? existing.map((r, i) => (i === at ? rule : r))
    : [...existing, rule];
  try {
    await updateSSHHost({ ...host, forwards }, null);
    await reload();
    closeTunnelDrawer();
    return { host, rule };
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
    return undefined;
  } finally {
    tunnelSaving.value = false;
  }
}

// Saving does not start the tunnel — a tunnel occupies a local port, and that
// stays an explicit act (§5.3). This is the opt-in shortcut for the case where
// you came here to bring one up right now.
async function saveAndStartTunnel() {
  const saved = await saveTunnel();
  if (saved) await startRule(saved.host, saved.rule);
}

async function removeTunnelRule(h: SSHHost, r: ForwardRule) {
  errorMsg.value = "";
  try {
    // Stop first: the rule is about to stop existing, and reconcile on the Go
    // side would kill the listener anyway — doing it here means the error, if
    // any, arrives attached to the click that caused it.
    if (forwardState(h.id, r.id)) await stopForward(h.id, r.id);
    await updateSSHHost(
      { ...h, forwards: cloneForwards(h.forwards).filter((x) => x.id !== r.id) },
      null,
    );
    await reload();
    await refreshActiveForwards();
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}

// ---- Key drawer ----
const keyDrawer = ref(false);
const keyEditId = ref<string | null>(null);
const kName = ref("");
const kPem = ref("");
const kPassphrase = ref("");
// Set when an imported key's PEM is passphrase-protected, so the drawer can say
// so instead of letting the user hit a backend parse error on save.
const kNeedsPassphrase = ref(false);

// kRevealed tracks whether the drawer is currently showing the *stored* key.
// While false, a blank PEM means "leave the stored key alone" (UpdateSSHKey's
// contract); once revealed the textarea holds the real thing, so saving
// rewrites it with what is on screen — which is what makes the revealed form
// editable rather than just readable.
const kRevealed = ref(false);
const kRevealing = ref(false);

function resetKeyForm() {
  kName.value = "";
  kPem.value = "";
  kPassphrase.value = "";
  kNeedsPassphrase.value = false;
  kRevealed.value = false;
  kRevealing.value = false;
}

// toggleRevealKey fetches the private material on demand, and clears it again
// on the way back. Nothing is fetched when the drawer opens: renaming a key
// must not put a private key on screen, and the value is dropped from
// component state the moment the user hides it.
async function toggleRevealKey() {
  if (!keyEditId.value || kRevealing.value) return;
  if (kRevealed.value) {
    kRevealed.value = false;
    kPem.value = "";
    kPassphrase.value = "";
    kNeedsPassphrase.value = false;
    return;
  }
  errorMsg.value = "";
  kRevealing.value = true;
  try {
    const sec = await revealSSHKey(keyEditId.value);
    kPem.value = sec.private_key;
    kPassphrase.value = sec.passphrase ?? "";
    kRevealed.value = true;
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  } finally {
    kRevealing.value = false;
  }
}

// copyKeyPem puts the stored private key on the clipboard without needing it
// on screen first — the common reason to want it back is to move it onto
// another machine, not to read it.
async function copyKeyPem() {
  if (!keyEditId.value) return;
  errorMsg.value = "";
  try {
    const pem = kRevealed.value ? kPem.value : (await revealSSHKey(keyEditId.value)).private_key;
    const clipboard = typeof navigator === "undefined" ? undefined : navigator.clipboard;
    if (clipboard?.writeText) await clipboard.writeText(pem);
    else fallbackCopyText(pem);
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}

function openNewKey() {
  keyEditId.value = "";
  resetKeyForm();
  keyDrawer.value = true;
}
// Jump from the host form's empty-vault hint straight into adding a key:
// close the host drawer, switch to the Keys tab, open the New Key drawer.
function jumpToNewKey() {
  closeHostDrawer();
  activeTab.value = "keys";
  openNewKey();
}
function openEditKey(k: SSHKey) {
  keyEditId.value = k.id;
  resetKeyForm();
  kName.value = k.name;
  keyDrawer.value = true;
}
function closeKeyDrawer() {
  keyDrawer.value = false;
  keyEditId.value = null;
}
// ---- Key file import ----
// Dragging is pointer-only; on touch builds (iOS) the drop zone would be dead
// UI, so only the picker button is offered there. Evaluated per mount so tests
// (and a device switching input modes) get an honest answer.
const supportsFileDrag = ref(
  window.matchMedia?.("(hover: hover) and (pointer: fine)")?.matches ?? true,
);
const keyFileInput = ref<HTMLInputElement | null>(null);
const keyDragOver = ref(false);

function pickKeyFile() {
  keyFileInput.value?.click();
}

async function onKeyFilePicked(e: Event) {
  const input = e.target as HTMLInputElement;
  const file = input.files?.[0];
  await importKeyFile(file);
  // Clear so re-picking the same path fires change again.
  input.value = "";
}

async function onKeyFileDropped(e: DragEvent) {
  keyDragOver.value = false;
  await importKeyFile(e.dataTransfer?.files?.[0]);
}

// importKeyFile fills the drawer from a local file. The name is only filled in
// when the user has not typed one, so an explicit label always wins.
async function importKeyFile(file: File | undefined | null) {
  if (!file) return;
  errorMsg.value = "";
  try {
    const imported = await readPrivateKeyFile(file);
    kPem.value = imported.pem;
    kNeedsPassphrase.value = imported.encrypted;
    if (kName.value.trim() === "") kName.value = imported.suggestedName;
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}

const canSaveKey = computed(() => kName.value.trim() !== "" && (keyEditId.value ? true : kPem.value.trim() !== ""));
async function saveKey() {
  if (!canSaveKey.value) return;
  errorMsg.value = "";
  try {
    if (keyEditId.value) {
      await updateSSHKey(keyEditId.value, kName.value.trim(), kPem.value, kPassphrase.value);
    } else {
      await addSSHKey(kName.value.trim(), kPem.value, kPassphrase.value);
    }
    closeKeyDrawer();
    await reload();
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}
async function removeKey(id: string) {
  errorMsg.value = "";
  try {
    await deleteSSHKey(id);
    await reload();
  } catch (e) {
    // Reference guard errors ("key in use by: ...") surface here.
    errorMsg.value = e instanceof Error ? e.message : String(e);
  }
}

const importMenu = ref({ open: false, x: 0, y: 0 });
const importMenuItems = computed<MenuItem[]>(() => [
  { key: "ssh-config", label: t("ssh.configImport.open") },
  { key: "termius", label: t("ssh.termius.open") },
]);
function toggleImportMenu(event: MouseEvent) {
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
  importMenu.value = { open: !importMenu.value.open, x: rect.left, y: rect.bottom + 6 };
}
function selectImportSource(source: string) {
  importMenu.value.open = false;
  if (source === "ssh-config") void openConfigImport();
  else if (source === "termius") termiusDrawer.value = true;
}
watch(activeTab, () => { importMenu.value.open = false; });

const termiusDrawer = ref(false);
const termiusResult = ref<TermiusImportReport | null>(null);
async function onTermiusImported(report: TermiusImportReport) {
  termiusDrawer.value = false;
  termiusResult.value = report;
  await reload();
}

// ---- ssh_config import ----
// Two-step preview/import (design doc §5.1): PreviewSSHConfigImport only
// reads ~/.ssh/config, ImportSSHHosts only writes what the user explicitly
// checked. Rows default to unchecked — the host list syncs to other devices,
// so undoing a bad bulk import means unchecking rows one at a time, not one
// big "select all" the user has to partially undo afterwards.
const configImportDrawer = ref(false);
const configImportLoading = ref(false);
// Two separate errors on purpose. A failed *preview* means there is nothing
// to show, so the body shows the message instead of a list. A failed
// *confirm* leaves the preview intact and belongs in the footer next to the
// button that failed — reusing one ref made a confirm error blank out the
// entry list the user was still working with.
const configPreviewError = ref("");
const configConfirmError = ref("");
const configPreview = ref<SSHConfigImportPreview | null>(null);
const configSelected = ref<Set<number>>(new Set());
const configImporting = ref(false);
const configImportResult = ref<number | null>(null);

// The Go side make()s both slices so they marshal to [] rather than null, but
// the drawer reads .length on every render — normalise here so one nil slice
// crossing the boundary can never take the whole panel down with a TypeError.
const previewEntries = computed<SSHHost[]>(() => configPreview.value?.entries ?? []);
const previewSkipped = computed(() => configPreview.value?.skipped ?? []);

// Aliases already saved locally. ImportSSHHosts matches incoming entries
// against stored hosts by exact Alias, so this mirrors it exactly (empty
// aliases never match — they always append). Computed in the frontend rather
// than returned by the preview because the backend knows nothing the panel
// doesn't: the saved host list is already loaded here, and "will this
// overwrite?" is a fact about the *local* store at the moment the user is
// looking at the list, not about ~/.ssh/config.
const savedAliases = computed(() => new Set(hosts.value.map((h) => h.alias).filter(Boolean)));
function willOverwrite(e: SSHHost): boolean {
  return !!e.alias && savedAliases.value.has(e.alias);
}

function hostFromConfigEntry(e: SSHHost): SSHHost {
  return { ...e };
}

async function openConfigImport() {
  configImportDrawer.value = true;
  configPreviewError.value = "";
  configConfirmError.value = "";
  configImportResult.value = null;
  configPreview.value = null;
  configSelected.value = new Set();
  configImportLoading.value = true;
  try {
    configPreview.value = await previewSSHConfigImport();
  } catch (e) {
    configPreviewError.value = e instanceof Error ? e.message : String(e);
  } finally {
    configImportLoading.value = false;
  }
}
function closeConfigImportDrawer() {
  configImportDrawer.value = false;
}
function toggleConfigEntry(index: number) {
  const next = new Set(configSelected.value);
  if (next.has(index)) next.delete(index);
  else next.add(index);
  configSelected.value = next;
}
const canImportConfigSelection = computed(() => configSelected.value.size > 0);

async function confirmConfigImport() {
  if (!configPreview.value || configSelected.value.size === 0) return;
  configImporting.value = true;
  configConfirmError.value = "";
  try {
    const chosen = previewEntries.value
      .filter((_, i) => configSelected.value.has(i))
      .map(hostFromConfigEntry);
    const count = await importSSHHosts(chosen);
    configImportResult.value = count;
    configImportDrawer.value = false;
    await reload();
  } catch (e) {
    configConfirmError.value = e instanceof Error ? e.message : String(e);
  } finally {
    configImporting.value = false;
  }
}
</script>

<template>
  <div class="ssh-overlay" @click.self="$emit('close')">
    <div class="ssh-shell">
      <!-- Header -->
      <header class="ssh-header">
        <div class="tabs">
          <button
            class="tab" :class="{ on: activeTab === 'hosts' }"
            data-test="ssh-tab-hosts" @click="activeTab = 'hosts'"
          ><Server :size="14" /> {{ t("ssh.tabHosts") }} <span class="tab-count">{{ hosts.length }}</span></button>
          <button
            class="tab" :class="{ on: activeTab === 'keys' }"
            data-test="ssh-tab-keys" @click="activeTab = 'keys'"
          ><KeyRound :size="14" /> {{ t("ssh.tabKeys") }} <span class="tab-count">{{ keys.length }}</span></button>
          <button
            class="tab" :class="{ on: activeTab === 'tunnels' }"
            data-test="ssh-tab-tunnels" @click="openTunnelsTab"
          ><Network :size="14" /> {{ t("ssh.forwards.tab") }} <span class="tab-count">{{ savedForwardCount }}</span></button>
        </div>
        <div v-if="activeTab === 'hosts'" class="search">
          <Search :size="13" class="search-icon" />
          <input v-model="query" data-test="ssh-search" :placeholder="t('ssh.filterPlaceholder')" spellcheck="false" autocomplete="off" />
        </div>
        <div v-else class="search-spacer" />
        <button
          v-if="activeTab === 'hosts'" class="new-btn ghost"
          data-test="ssh-import-open" aria-haspopup="menu" :aria-expanded="importMenu.open"
          @click="toggleImportMenu"
        >
          <FileDown :size="14" /> {{ t("ssh.import") }} <ChevronDown :size="12" />
        </button>
        <button v-if="activeTab === 'hosts'" class="new-btn" data-test="ssh-new-host" @click="openNewHost">
          <Plus :size="14" /> {{ t("ssh.newHost") }}
        </button>
        <button v-else-if="activeTab === 'keys'" class="new-btn" data-test="ssh-key-new" @click="openNewKey">
          <Plus :size="14" /> {{ t("ssh.newKey") }}
        </button>
        <template v-else>
          <button class="new-btn ghost" data-test="ssh-tunnels-refresh" @click="refreshActiveForwards">
            <RefreshCw :size="14" /> {{ t("ssh.forwards.refresh") }}
          </button>
          <button
            class="new-btn" data-test="ssh-tunnel-new"
            :disabled="tunnelHostOptions.length === 0"
            v-tooltip="tunnelHostOptions.length === 0 ? t('ssh.forwards.newNeedsHost') : ''"
            @click="openNewTunnel"
          ><Plus :size="14" /> {{ t("ssh.forwards.new") }}</button>
        </template>
        <button class="close-x" v-tooltip="t('common.close')" :aria-label="t('common.close')" @click="$emit('close')"><X :size="16" /></button>
      </header>

      <p v-if="errorMsg" class="ssh-error" data-test="ssh-hosts-error">{{ errorMsg }}</p>
      <!-- The fingerprint is a <code> on its own line: it is the one thing the
           user is supposed to compare character by character against something
           they got from elsewhere. -->
      <div v-if="hostKey" class="ssh-tofu" data-test="ssh-host-tofu">
        <p class="tofu-title">{{ t("ssh.hostKey.title") }}</p>
        <p class="tofu-msg">{{ hostKeyMessage }}</p>
        <code class="tofu-host">{{ hostKey.prompt.host }}</code>
        <code class="tofu-fp">{{ hostKey.prompt.fingerprint }}</code>
        <p v-if="hostKey.prompt.hopIndex > 0" class="tofu-note">{{ t("ssh.hostKey.chainNote") }}</p>
        <div class="tofu-actions">
          <button data-test="ssh-host-reject-hostkey" :disabled="hostKeyBusy" @click="rejectHostKey">
            {{ t("ssh.hostKey.reject") }}
          </button>
          <button class="new-btn" data-test="ssh-host-accept-hostkey" :disabled="hostKeyBusy" @click="acceptHostKey">
            {{ t("ssh.hostKey.accept") }}
          </button>
        </div>
      </div>
      <p v-if="termiusResult" class="ssh-success" data-test="ssh-termius-import-result">
        {{ t("ssh.termius.result", { hosts: termiusResult.hosts, keys: termiusResult.keys, skipped: termiusResult.skipped }) }}
      </p>
      <p v-if="configImportResult !== null" class="ssh-success" data-test="ssh-config-import-result">
        {{ t("ssh.importedHosts", { count: configImportResult }) }}
      </p>

      <!-- HOSTS TAB -->
      <div v-show="activeTab === 'hosts'" class="ssh-body">
        <div v-if="hosts.length === 0" class="empty">
          <Server :size="40" class="empty-icon" />
          <p class="empty-title">{{ t("ssh.hosts.emptyTitle") }}</p>
          <p class="empty-sub">{{ t("ssh.hosts.emptySub") }}</p>
          <button class="new-btn ghost" @click="openNewHost"><Plus :size="14" /> {{ t("ssh.newHost") }}</button>
        </div>
        <template v-else>
          <nav v-if="selectedGroupPath.length" class="host-breadcrumb" :aria-label="t('ssh.hosts.groups')">
            <button data-test="ssh-tag-filter-clear" @click="goToRoot">
              <ArrowLeft :size="14" /> {{ t("ssh.hosts.allHosts") }}
            </button>
            <template v-for="(part, index) in selectedGroupPath" :key="index">
              <ChevronRight :size="14" />
              <span v-if="index === selectedGroupPath.length - 1" aria-current="page">{{ part }}</span>
              <button v-else :data-test="`ssh-group-ancestor-${index}`" @click="selectedGroupPath = selectedGroupPath.slice(0, index + 1)">{{ part }}</button>
            </template>
          </nav>
          <section v-if="hostGroups.length" class="host-groups" data-test="ssh-tag-filter-bar">
            <h3 class="host-section-title">{{ t("ssh.hosts.groups") }} <span>{{ hostGroups.length }}</span></h3>
            <div class="group-grid">
              <button
                v-for="group in hostGroups" :key="group.name" class="group-card"
                :data-test="`ssh-tag-filter-${group.name}`" @click="enterGroup(group.name)"
              >
                <span class="group-glyph"><Folder :size="20" /></span>
                <span class="group-main">
                  <span class="group-name" v-tooltip="group.name">{{ group.name }}</span>
                  <span class="group-count">{{ t("ssh.hosts.hostCount", { count: group.count }) }}</span>
                </span>
                <ChevronRight :size="14" class="group-chevron" />
              </button>
            </div>
          </section>
          <h3 class="host-section-title">{{ t("ssh.hosts.hostList") }} <span>{{ filteredHosts.length }}</span></h3>
          <div v-if="filteredHosts.length === 0 && (!hostGroups.length || query.trim())" class="empty" data-test="ssh-hosts-no-match">
            <Server :size="40" class="empty-icon" />
            <p class="empty-title">{{ t("ssh.hosts.noMatchTitle") }}</p>
            <p class="empty-sub">{{ t("ssh.hosts.noMatchSub") }}</p>
          </div>
            <div v-if="filteredHosts.length" class="card-grid host-grid">
              <article
                v-for="h in filteredHosts" :key="h.id" class="card host-card"
                :data-test="`ssh-host-card-${h.id}`"
                :class="{ busy: connectingId === h.id }" tabindex="0"
                @keydown.enter.self.prevent="connect(h.id)" @dblclick="connect(h.id)"
                @contextmenu.prevent="openHostMenu($event, h)"
              >
                <div class="card-glyph">
                  <KeyRound v-if="h.auth_kind === 'key'" :size="16" />
                  <Server v-else :size="16" />
                </div>
                <div class="card-main">
                  <div class="card-label" v-tooltip="hostLabel(h)">{{ hostLabel(h) }}</div>
                  <div class="card-sub">{{ hostSubtitle(h) }}</div>
                  <div v-if="hasProxyMarker(h)" class="card-proxy">
                    <span
                      class="proxy-badge" :class="{ blocked: runsProxyCommand(h) }"
                      :data-test="`ssh-host-proxy-${h.id}`"
                      v-tooltip="proxyReason(h)"
                    >{{ proxyLabel(h) }}</span>
                  </div>
                  <div v-if="h.tags?.length" class="card-tags">
                    <span
                      v-for="tag in h.tags" :key="tag" class="card-tag"
                      :data-test="`ssh-host-tag-${h.id}-${tag}`"
                    >{{ tag }}</span>
                  </div>
                </div>
                <div class="card-actions" @dblclick.stop>
                  <button class="act" v-tooltip="t('ssh.edit')" :aria-label="t('ssh.edit')" @click.stop="openEditHost(h)"><Pencil :size="13" /></button>
                  <button class="act danger" :data-test="`ssh-delete-${h.id}`" v-tooltip="t('common.delete')" :aria-label="t('common.delete')" @click.stop="removeHost(h.id)"><Trash2 :size="13" /></button>
                </div>
              </article>
            </div>
        </template>
      </div>

      <!-- KEYS TAB -->
      <div v-show="activeTab === 'keys'" class="ssh-body">
        <div v-if="keys.length === 0" class="empty">
          <KeyRound :size="40" class="empty-icon" />
          <p class="empty-title">{{ t("ssh.keys.emptyTitle") }}</p>
          <p class="empty-sub">{{ t("ssh.keys.emptySub") }}</p>
          <button class="new-btn ghost" @click="openNewKey"><Plus :size="14" /> {{ t("ssh.newKey") }}</button>
        </div>
        <div v-else class="card-grid">
          <article v-for="k in keys" :key="k.id" class="card key-card" tabindex="0">
            <div class="card-glyph"><KeyRound :size="16" /></div>
            <div class="card-main">
              <div class="card-label">{{ k.name }}</div>
              <div class="card-sub">{{ k.key_type ? t("ssh.keys.typeLabel", { type: k.key_type }) : t("ssh.keys.genericSubtitle") }}</div>
            </div>
            <div class="card-actions">
              <button class="act hover-action" :data-test="`ssh-key-edit-${k.id}`" v-tooltip="t('ssh.edit')" :aria-label="t('ssh.edit')" @click.stop="openEditKey(k)"><Pencil :size="13" /></button>
              <button class="act danger hover-action" :data-test="`ssh-key-delete-${k.id}`" v-tooltip="t('common.delete')" :aria-label="t('common.delete')" @click.stop="removeKey(k.id)"><Trash2 :size="13" /></button>
            </div>
          </article>
        </div>
      </div>

      <!-- TUNNELS TAB -->
      <div v-show="activeTab === 'tunnels'" class="ssh-body">
        <div v-if="forwardRows.length" class="tunnel-toolbar">
          <div class="search tunnel-search">
            <Search :size="13" class="search-icon" />
            <input v-model="tunnelQuery" data-test="ssh-tunnel-search" :placeholder="t('ssh.forwards.searchPlaceholder')" :aria-label="t('ssh.forwards.searchPlaceholder')" autocomplete="off" spellcheck="false" />
            <button v-if="tunnelQuery" class="close-x" :aria-label="t('ssh.forwards.clearSearch')" @click="tunnelQuery = ''"><X :size="13" /></button>
          </div>
          <div class="tunnel-view-controls">
            <div class="tunnel-view-control">
              <LayoutGrid v-if="tunnelLayout === 'grid'" :size="14" /><List v-else :size="14" />
              <SelectDropdown v-model="tunnelLayout" :options="tunnelLayoutOptions" :aria-label="t('ssh.forwards.layout')" data-test="ssh-tunnel-layout" />
            </div>
            <div class="tunnel-view-control tunnel-sort-control">
              <ArrowDownWideNarrow :size="14" />
              <SelectDropdown v-model="tunnelSort" :options="tunnelSortOptions" :aria-label="t('ssh.forwards.sort')" data-test="ssh-tunnel-sort" />
            </div>
          </div>
        </div>
        <div v-if="forwardRows.length === 0" class="empty" data-test="ssh-tunnels-empty">
          <Network :size="40" class="empty-icon" />
          <p class="empty-title">{{ t("ssh.forwards.emptyTitle") }}</p>
          <p class="empty-sub">{{ t("ssh.forwards.emptySub") }}</p>
          <button
            v-if="tunnelHostOptions.length" class="new-btn ghost"
            data-test="ssh-tunnel-new-empty" @click="openNewTunnel"
          ><Plus :size="14" /> {{ t("ssh.forwards.new") }}</button>
        </div>
        <div v-else-if="!visibleForwardRows.length" class="empty tunnel-no-match" data-test="ssh-tunnels-no-match">
          <Search :size="32" class="empty-icon" />
          <p class="empty-title">{{ t("ssh.forwards.noMatch") }}</p>
          <button class="new-btn ghost" @click="tunnelQuery = ''">{{ t("ssh.forwards.clearSearch") }}</button>
        </div>
        <div v-else class="card-grid" :class="{ 'tunnel-list': tunnelLayout === 'list' }" data-test="ssh-tunnel-grid">
            <article
              v-for="{ host: h, rule: r } in visibleForwardRows" :key="`${h.id}:${r.id}`" class="card fwd-card"
              :data-test="`ssh-tunnel-row-${h.id}-${r.id}`" :aria-busy="isForwardBusy(h.id, r.id)" tabindex="0"
              @dblclick="startRule(h, r)" @keydown.enter.self.prevent="startRule(h, r)"
            >
              <div class="card-glyph"><Network :size="16" /></div>
              <div class="card-main">
                <div class="card-label">
                  <span class="fwd-note" v-tooltip="r.note">{{ r.note || t("ssh.forwards.tab") }}</span>
                  <span v-if="r.kind !== 'local'" class="fwd-kind">{{ forwardKindLabel(r.kind) }}</span>
                </div>
                <div class="card-sub fwd-route" v-tooltip="`${forwardListenText(h, r)} → ${hostLabel(h)} (${h.user}@${h.host}) → ${forwardTargetText(h, r)}`">
                  {{ forwardListenText(h, r) }} → {{ hostLabel(h) }} → {{ forwardTargetText(h, r) }}
                </div>
                <div v-if="hasProxyMarker(h)" class="card-proxy">
                  <span class="proxy-badge" :class="{ blocked: runsProxyCommand(h) }"
                    :data-test="`ssh-tunnel-proxy-${h.id}`" v-tooltip="proxyReason(h)"
                  >{{ proxyLabel(h) }}</span>
                </div>
                <p v-if="runsProxyCommand(h)" class="fwd-proxy-reason"
                  :data-test="`ssh-tunnel-proxy-reason-${h.id}`"
                >{{ t("ssh.forwards.proxyBlocked") }} {{ proxyReason(h) }}</p>
                <div v-if="forwardState(h.id, r.id)" class="fwd-row-conns">
                  {{ t("ssh.forwards.conns", { count: forwardState(h.id, r.id)?.conns ?? 0 }) }}
                </div>
                <p
                  v-if="forwardState(h.id, r.id)?.error" class="fwd-row-error"
                  :data-test="`ssh-tunnel-error-${h.id}-${r.id}`"
                >{{ forwardState(h.id, r.id)?.error }}</p>
              </div>
              <div class="fwd-card-side">
                <div class="fwd-status" @dblclick.stop>
                  <span
                    class="fwd-state" :class="forwardStateClass(h, r)"
                    :data-test="`ssh-tunnel-state-${h.id}-${r.id}`"
                  >{{ forwardStateLabel(h, r) }}</span>
                  <!-- A stopped-with-an-error entry gets a restart and a
                       dismiss, never a stop: its listener is already gone. -->
                  <button
                    v-if="isForwardRunning(h.id, r.id)" class="act danger"
                    :data-test="`ssh-tunnel-stop-${h.id}-${r.id}`"
                    v-tooltip="t('ssh.forwards.stop')"
                    :aria-label="t('ssh.forwards.stop')"
                    :disabled="isForwardBusy(h.id, r.id)"
                    @click="stopRule(h, r)"
                  ><Square :size="13" /></button>
                  <template v-else>
                    <button
                      v-if="forwardState(h.id, r.id)" class="act"
                      :data-test="`ssh-tunnel-dismiss-${h.id}-${r.id}`"
                      v-tooltip="t('ssh.forwards.dismiss')"
                      :aria-label="t('ssh.forwards.dismiss')"
                      :disabled="isForwardBusy(h.id, r.id)"
                      @click="stopRule(h, r)"
                    ><X :size="13" /></button>
                  </template>
                </div>
                <div class="card-actions" @dblclick.stop>
                  <button
                    class="act hover-action" :data-test="`ssh-tunnel-edit-${h.id}-${r.id}`"
                    v-tooltip="t('ssh.edit')" :aria-label="t('ssh.edit')" @click="openEditTunnel(h, r)"
                  ><Pencil :size="13" /></button>
                  <button
                    class="act danger hover-action" :data-test="`ssh-tunnel-delete-${h.id}-${r.id}`"
                    v-tooltip="t('common.delete')" :aria-label="t('common.delete')" @click="removeTunnelRule(h, r)"
                  ><Trash2 :size="13" /></button>
                </div>
              </div>
            </article>
        </div>
      </div>

      <SessionRowMenu
        :open="importMenu.open" :x="importMenu.x" :y="importMenu.y" :items="importMenuItems"
        @close="importMenu.open = false" @select="selectImportSource"
      />

      <SessionRowMenu
        :open="hostMenu.open" :x="hostMenu.x" :y="hostMenu.y" :items="hostMenuItems"
        @close="closeHostMenu" @select="onHostMenuSelect"
      />

      <!-- HOST DRAWER -->
      <transition name="drawer">
        <aside v-if="hostDrawer" class="drawer">
          <div class="drawer-head"><span>{{ hostEditId ? t("ssh.hostDrawer.titleEdit") : t("ssh.hostDrawer.titleNew") }}</span><button class="close-x" @click="closeHostDrawer"><X :size="15" /></button></div>
          <div class="drawer-body">
            <label class="field"><span class="fl">{{ t("ssh.hostDrawer.address") }}</span><input data-test="ssh-add-host" v-model="fHost" :placeholder="t('ssh.hostDrawer.addressPlaceholder')" spellcheck="false" autocomplete="off" /></label>
            <div class="field-row">
              <label class="field grow"><span class="fl">{{ t("ssh.hostDrawer.label") }}</span><input data-test="ssh-add-alias" v-model="fAlias" :placeholder="t('ssh.hostDrawer.labelPlaceholder')" autocomplete="off" /></label>
              <label class="field port"><span class="fl">{{ t("ssh.hostDrawer.port") }}</span><input data-test="ssh-add-port" v-model="fPort" autocomplete="off" /></label>
            </div>
            <label class="field tag-field">
              <span class="fl">{{ t("ssh.hostDrawer.tags") }}</span>
              <div class="tag-editor">
                <span
                  v-for="tag in fTags" :key="tag" class="chip"
                  :data-test="`ssh-add-tag-chip-${tag}`"
                >
                  {{ tag }}
                  <button
                    type="button" class="chip-x" :data-test="`ssh-add-tag-remove-${tag}`"
                    v-tooltip="t('ssh.hostDrawer.removeTag', { tag })" :aria-label="t('ssh.hostDrawer.removeTag', { tag })" @click.prevent="removeFormTag(tag)"
                  ><X :size="10" /></button>
                </span>
                <input
                  class="tag-input" data-test="ssh-add-tag-input" v-model="fTagInput"
                  :placeholder="fTags.length ? '' : t('ssh.hostDrawer.tagsPlaceholder')"
                  autocomplete="off" spellcheck="false"
                  @focus="tagMenuOpen = true" @input="tagMenuOpen = true" @blur="onTagBlur"
                  @keydown.enter.prevent="commitTagInput"
                  @keydown.,.prevent="commitTagInput"
                  @keydown.backspace="onTagBackspace"
                />
                <ul v-if="tagMenuOpen && tagSuggestions.length" class="combo-menu" data-test="ssh-tag-menu">
                  <li
                    v-for="tag in tagSuggestions" :key="tag"
                    class="combo-opt" :data-test="`ssh-tag-opt-${tag}`"
                    @mousedown.prevent="pickTag(tag)"
                  >{{ tag }}</li>
                </ul>
              </div>
            </label>
            <label class="field"><span class="fl">{{ t("ssh.hostDrawer.username") }}</span><input data-test="ssh-add-user" v-model="fUser" :placeholder="t('ssh.hostDrawer.usernamePlaceholder')" autocomplete="off" /></label>
            <div class="seg">
              <button :class="{ on: fAuthKind === 'password' }" data-test="ssh-auth-password" @click="fAuthKind = 'password'">{{ t("ssh.hostDrawer.authPassword") }}</button>
              <button :class="{ on: fAuthKind === 'key' }" data-test="ssh-auth-key" @click="fAuthKind = 'key'">{{ t("ssh.hostDrawer.authKey") }}</button>
            </div>
            <!-- Read-only: ssh_config's IdentityFile is recorded as a path,
                 never read. Showing it is the whole point of recording it —
                 it tells the user which key to go import. -->
            <p v-if="editingHost?.identity_file" class="identity-hint" data-test="ssh-host-identity-file">
              <span class="fl">{{ t("ssh.hostDrawer.identityFileLabel") }}</span>
              <code>{{ editingHost.identity_file }}</code>
              <span class="hint">{{ t("ssh.hostDrawer.identityFileHint") }}</span>
            </p>
            <p v-if="editingHost && hasProxyMarker(editingHost)" class="identity-hint proxy" data-test="ssh-host-drawer-proxy">
              {{ proxyReason(editingHost) }}
            </p>
            <label v-if="fAuthKind === 'password'" class="field">
              <span class="fl">{{ t("ssh.hostDrawer.password") }}<template v-if="hostEditId"> <em>{{ t("ssh.hostDrawer.keepBlank") }}</em></template></span>
              <input data-test="ssh-add-password" type="password" v-model="fPassword" autocomplete="off" />
            </label>
            <template v-else>
              <label v-if="keys.length" class="field">
                <span class="fl">{{ t("ssh.hostDrawer.keyPicker") }}</span>
                <div data-test="ssh-add-keyid">
                  <SelectDropdown v-model="fKeyID" :options="keyOptions" :aria-label="t('ssh.hostDrawer.keyPickerAria')" />
                </div>
              </label>
              <div v-else class="empty-keys-hint">
                <p class="hint">{{ t("ssh.hostDrawer.noKeys") }}</p>
                <button class="btn primary sm" data-test="ssh-host-add-key" @click="jumpToNewKey">
                  <Plus :size="13" /> {{ t("ssh.hostDrawer.addKey") }}
                </button>
              </div>
            </template>

            <!-- STARTUP COMMAND -->
            <!-- For a bastion that puts a menu in front of the shell.
                 JumpServer asks for the asset at `Opt>` and then for the
                 system user at `ID>`, so this is a *list* of answers, not one
                 command — hence a textarea and a per-step interval rather
                 than a single line. -->
            <div class="fwd-section" data-test="ssh-startup-section">
              <div class="fwd-section-head">
                <span class="fl">{{ t("ssh.startup.sectionTitle") }}</span>
              </div>
              <p class="hint">{{ t("ssh.startup.sectionHint") }}</p>
              <label class="field">
                <textarea
                  data-test="ssh-startup-command" v-model="fStartupCommand" rows="3"
                  spellcheck="false" autocomplete="off"
                  :placeholder="t('ssh.startup.placeholder')"
                ></textarea>
              </label>
              <div class="field-row">
                <label class="field port">
                  <span class="fl">{{ t("ssh.startup.delay") }}</span>
                  <input
                    data-test="ssh-startup-delay" v-model="fStartupDelayMs"
                    :placeholder="String(1500)" autocomplete="off"
                  />
                </label>
                <p v-if="startupStepCount" class="hint grow" data-test="ssh-startup-steps">
                  {{ t("ssh.startup.steps", { count: startupStepCount }) }}
                </p>
              </div>
            </div>

            <!-- PORT FORWARDING RULES -->
            <div class="fwd-section" data-test="ssh-forwards-section">
              <div class="fwd-section-head">
                <span class="fl">{{ t("ssh.forwards.sectionTitle") }}</span>
                <button class="btn sm" type="button" data-test="ssh-forward-add" @click.prevent="addForwardRule">
                  <Plus :size="13" /> {{ t("ssh.forwards.add") }}
                </button>
              </div>
              <p class="hint">{{ t("ssh.forwards.sectionHint") }}</p>
              <p v-if="fForwards.length === 0" class="hint" data-test="ssh-forwards-none">
                {{ t("ssh.forwards.noRules") }}
              </p>
              <div
                v-for="r in fForwards" :key="r.id" class="fwd-rule"
                :data-test="`ssh-forward-rule-${r.id}`"
              >
                <div class="fwd-rule-head">
                  <div class="seg sm">
                    <button
                      type="button" :class="{ on: r.kind === 'local' }"
                      :data-test="`ssh-forward-kind-local-${r.id}`"
                      @click.prevent="setForwardKind(r, 'local')"
                    >{{ t("ssh.forwards.kindLocal") }}</button>
                    <button
                      type="button" :class="{ on: r.kind === 'remote' }"
                      :data-test="`ssh-forward-kind-remote-${r.id}`"
                      @click.prevent="setForwardKind(r, 'remote')"
                    >{{ t("ssh.forwards.kindRemote") }}</button>
                    <button
                      type="button" :class="{ on: r.kind === 'dynamic' }"
                      :data-test="`ssh-forward-kind-dynamic-${r.id}`"
                      @click.prevent="setForwardKind(r, 'dynamic')"
                    >{{ t("ssh.forwards.kindDynamic") }}</button>
                  </div>
                  <button
                    type="button" class="act danger"
                    :data-test="`ssh-forward-remove-${r.id}`"
                    v-tooltip="t('ssh.forwards.remove')"
                    :aria-label="t('ssh.forwards.remove')"
                    @click.prevent="removeForwardRule(r.id)"
                  ><Trash2 :size="13" /></button>
                </div>
                <p class="hint">{{ forwardKindHint(r.kind) }}</p>
                <div class="field-row">
                  <label class="field grow">
                    <span class="fl">{{ t("ssh.forwards.bindAddr") }}</span>
                    <input
                      :data-test="`ssh-forward-bind-addr-${r.id}`" v-model="r.bind_addr"
                      placeholder="127.0.0.1" spellcheck="false" autocomplete="off"
                    />
                  </label>
                  <label class="field port">
                    <span class="fl">{{ t("ssh.forwards.bindPort") }}</span>
                    <input :data-test="`ssh-forward-bind-port-${r.id}`" v-model="r.bind_port" autocomplete="off" />
                  </label>
                </div>
                <!-- Dynamic has no configured destination: the SOCKS5 client
                     names one per connection, so these fields would be a lie. -->
                <div v-if="r.kind !== 'dynamic'" class="field-row">
                  <label class="field grow">
                    <span class="fl">{{ t("ssh.forwards.targetHost") }}</span>
                    <input
                      :data-test="`ssh-forward-target-host-${r.id}`" v-model="r.target_host"
                      spellcheck="false" autocomplete="off"
                    />
                  </label>
                  <label class="field port">
                    <span class="fl">{{ t("ssh.forwards.targetPort") }}</span>
                    <input :data-test="`ssh-forward-target-port-${r.id}`" v-model="r.target_port" autocomplete="off" />
                  </label>
                </div>
                <label class="field">
                  <span class="fl">{{ t("ssh.forwards.label") }}</span>
                  <input
                    :data-test="`ssh-forward-note-${r.id}`" v-model="r.note"
                    :placeholder="t('ssh.forwards.labelPlaceholder')" autocomplete="off"
                  />
                </label>
                <p
                  v-if="forwardBindWarning(r)" class="fwd-warn"
                  :data-test="`ssh-forward-bind-warning-${r.id}`"
                >{{ forwardBindWarning(r) }}</p>
              </div>
            </div>
          </div>
          <div class="drawer-foot">
            <button class="btn ghost" @click="closeHostDrawer">{{ t("common.cancel") }}</button>
            <button class="btn primary" data-test="ssh-add-submit" :disabled="!canSaveHost" @click="saveHost">{{ hostEditId ? t("common.save") : t("ssh.hostDrawer.submitNew") }}</button>
          </div>
        </aside>
      </transition>

      <!-- KEY DRAWER -->
      <transition name="drawer">
        <aside v-if="keyDrawer" class="drawer">
          <div class="drawer-head"><span>{{ keyEditId ? t("ssh.keyDrawer.titleEdit") : t("ssh.keyDrawer.titleNew") }}</span><button class="close-x" @click="closeKeyDrawer"><X :size="15" /></button></div>
          <div class="drawer-body">
            <label class="field"><span class="fl">{{ t("ssh.keyDrawer.name") }}</span><input data-test="ssh-key-name" v-model="kName" :placeholder="t('ssh.keyDrawer.namePlaceholder')" autocomplete="off" /></label>
            <label class="field">
              <span class="fl pem-label">
                <span>{{ t("ssh.keyDrawer.pem") }}<template v-if="keyEditId && !kRevealed"> <em>{{ t("ssh.keyDrawer.keepBlank") }}</em></template></span>
                <!-- Reveal is opt-in and edit-only: there is nothing stored
                     to show for a key being created, and opening a saved key
                     to rename it must not put a private key on screen. -->
                <span v-if="keyEditId" class="pem-tools">
                  <button
                    type="button" class="act" data-test="ssh-key-reveal"
                    :disabled="kRevealing"
                    v-tooltip="kRevealed ? t('ssh.keyDrawer.hide') : t('ssh.keyDrawer.reveal')"
                    :aria-label="kRevealed ? t('ssh.keyDrawer.hide') : t('ssh.keyDrawer.reveal')"
                    @click.prevent="toggleRevealKey"
                  ><EyeOff v-if="kRevealed" :size="13" /><Eye v-else :size="13" /></button>
                  <button
                    type="button" class="act" data-test="ssh-key-copy"
                    v-tooltip="t('ssh.keyDrawer.copy')" :aria-label="t('ssh.keyDrawer.copy')" @click.prevent="copyKeyPem"
                  ><Copy :size="13" /></button>
                </span>
              </span>
              <!-- The placeholder is the literal PEM header the user is
                   expected to paste, not prose — it stays untranslated. -->
              <textarea data-test="ssh-key-pem" v-model="kPem" rows="6" spellcheck="false" placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"></textarea>
            </label>
            <label class="field">
              <span class="fl">{{ t("ssh.keyDrawer.passphrase") }}</span>
              <input data-test="ssh-key-passphrase" type="password" v-model="kPassphrase" autocomplete="off" />
            </label>
            <p v-if="kNeedsPassphrase" class="enc-hint" data-test="ssh-key-encrypted-hint">
              {{ t("ssh.keyDrawer.encryptedHint") }}
            </p>

            <div class="import-block">
              <input
                ref="keyFileInput" class="file-input" data-test="ssh-key-file-input"
                type="file" @change="onKeyFilePicked"
              />
              <div
                v-if="supportsFileDrag" class="dropzone" :class="{ over: keyDragOver }"
                data-test="ssh-key-dropzone"
                @dragover.prevent="keyDragOver = true"
                @dragenter.prevent="keyDragOver = true"
                @dragleave="keyDragOver = false"
                @drop.prevent="onKeyFileDropped"
              >
                <FileUp :size="20" class="dropzone-icon" />
                <span>{{ t("ssh.keyDrawer.dropzone") }}</span>
              </div>
              <button class="btn import" type="button" data-test="ssh-key-import-btn" @click="pickKeyFile">
                <Upload :size="13" /> {{ t("ssh.keyDrawer.importFromFile") }}
              </button>
            </div>
          </div>
          <div class="drawer-foot">
            <button class="btn ghost" @click="closeKeyDrawer">{{ t("common.cancel") }}</button>
            <button class="btn primary" data-test="ssh-key-submit" :disabled="!canSaveKey" @click="saveKey">{{ keyEditId ? t("common.save") : t("ssh.keyDrawer.submitNew") }}</button>
          </div>
        </aside>
      </transition>

      <!-- TUNNEL DRAWER -->
      <transition name="drawer">
        <aside v-if="tunnelDrawer && tunnelDraft" class="drawer" data-test="ssh-tunnel-drawer">
          <div class="drawer-head">
            <span>{{ tunnelEditRuleID ? t("ssh.forwards.drawerTitleEdit") : t("ssh.forwards.drawerTitleNew") }}</span>
            <button class="close-x" @click="closeTunnelDrawer"><X :size="15" /></button>
          </div>
          <div class="drawer-body">
            <label class="field">
              <span class="fl">{{ t("ssh.forwards.hostPicker") }}</span>
              <!-- Locked while editing: moving a rule between hosts is two
                   writes to two records, and a half-applied move leaves it on
                   both. Delete and re-add says the same thing safely. -->
              <div data-test="ssh-tunnel-host">
                <SelectDropdown
                  v-model="tunnelHostID" :options="tunnelHostOptions"
                  :disabled="tunnelEditRuleID !== ''"
                  :aria-label="t('ssh.forwards.hostPickerAria')"
                />
              </div>
            </label>
            <p class="hint">{{ t("ssh.forwards.hostPickerHint") }}</p>

            <div class="seg">
              <button
                type="button" :class="{ on: tunnelDraft.kind === 'local' }"
                data-test="ssh-tunnel-kind-local" @click.prevent="tunnelDraft.kind = 'local'"
              >{{ t("ssh.forwards.kindLocal") }}</button>
              <button
                type="button" :class="{ on: tunnelDraft.kind === 'remote' }"
                data-test="ssh-tunnel-kind-remote" @click.prevent="tunnelDraft.kind = 'remote'"
              >{{ t("ssh.forwards.kindRemote") }}</button>
              <button
                type="button" :class="{ on: tunnelDraft.kind === 'dynamic' }"
                data-test="ssh-tunnel-kind-dynamic" @click.prevent="tunnelDraft.kind = 'dynamic'"
              >{{ t("ssh.forwards.kindDynamic") }}</button>
            </div>
            <p class="hint">{{ forwardKindHint(tunnelDraft.kind) }}</p>

            <div class="field-row">
              <label class="field grow">
                <span class="fl">{{ t("ssh.forwards.bindAddr") }}</span>
                <input
                  data-test="ssh-tunnel-bind-addr" v-model="tunnelDraft.bind_addr"
                  placeholder="127.0.0.1" spellcheck="false" autocomplete="off"
                />
              </label>
              <label class="field port">
                <span class="fl">{{ t("ssh.forwards.bindPort") }}</span>
                <input data-test="ssh-tunnel-bind-port" v-model="tunnelDraft.bind_port" autocomplete="off" />
              </label>
            </div>
            <div v-if="tunnelDraft.kind !== 'dynamic'" class="field-row">
              <label class="field grow">
                <span class="fl">{{ t("ssh.forwards.targetHost") }}</span>
                <input
                  data-test="ssh-tunnel-target-host" v-model="tunnelDraft.target_host"
                  spellcheck="false" autocomplete="off"
                />
              </label>
              <label class="field port">
                <span class="fl">{{ t("ssh.forwards.targetPort") }}</span>
                <input data-test="ssh-tunnel-target-port" v-model="tunnelDraft.target_port" autocomplete="off" />
              </label>
            </div>
            <label class="field">
              <span class="fl">{{ t("ssh.forwards.label") }}</span>
              <input
                data-test="ssh-tunnel-note" v-model="tunnelDraft.note"
                :placeholder="t('ssh.forwards.labelPlaceholder')" autocomplete="off"
              />
            </label>
            <p
              v-if="forwardBindWarning(tunnelDraft)" class="fwd-warn"
              data-test="ssh-tunnel-bind-warning"
            >{{ forwardBindWarning(tunnelDraft) }}</p>
          </div>
          <div class="drawer-foot">
            <button class="btn ghost" @click="closeTunnelDrawer">{{ t("common.cancel") }}</button>
            <!-- Saving alone does not open a listener (§5.3). This is the
                 opt-in for the case where bringing it up now is the point. -->
            <button
              class="btn" data-test="ssh-tunnel-save-start"
              :disabled="!canSaveTunnel || tunnelSaving" @click="saveAndStartTunnel"
            >{{ t("ssh.forwards.saveAndStart") }}</button>
            <button
              class="btn primary" data-test="ssh-tunnel-save"
              :disabled="!canSaveTunnel || tunnelSaving" @click="saveTunnel"
            >{{ t("common.save") }}</button>
          </div>
        </aside>
      </transition>

      <!-- SSH CONFIG IMPORT DRAWER -->
      <transition name="drawer">
        <aside v-if="configImportDrawer" class="drawer wide" data-test="ssh-config-import-drawer">
          <div class="drawer-head">
            <span>{{ t("ssh.configImport.title") }}</span>
            <button class="close-x" @click="closeConfigImportDrawer"><X :size="15" /></button>
          </div>
          <div class="drawer-body">
            <div v-if="configImportLoading" class="empty" data-test="ssh-config-import-loading">
              <p class="empty-sub">{{ t("ssh.configImport.loading") }}</p>
            </div>
            <p v-else-if="configPreviewError" class="ssh-error inline" data-test="ssh-config-import-error">
              {{ configPreviewError }}
            </p>
            <template v-else-if="configPreview">
              <div
                v-if="previewEntries.length === 0 && previewSkipped.length === 0"
                class="empty" data-test="ssh-config-import-empty"
              >
                <FileDown :size="36" class="empty-icon" />
                <p class="empty-title">{{ t("ssh.configImport.emptyTitle") }}</p>
                <p class="empty-sub">{{ t("ssh.configImport.emptySub") }}</p>
              </div>
              <template v-else>
                <p v-if="previewEntries.length" class="config-section-title">
                  {{ t("ssh.configImport.importableTitle", { count: previewEntries.length }) }}
                </p>
                <ul v-if="previewEntries.length" class="config-entry-list">
                  <li
                    v-for="(e, i) in previewEntries" :key="`${e.alias}-${i}`"
                    class="config-entry"
                  >
                    <label class="config-entry-label">
                      <input
                        type="checkbox" :data-test="`ssh-config-entry-check-${i}`"
                        :checked="configSelected.has(i)" @change="toggleConfigEntry(i)"
                      />
                      <span class="config-entry-main">
                        <span class="config-entry-alias">{{ e.alias || `${e.user}@${e.host}` }}</span>
                        <span class="config-entry-sub">{{ e.user }}@{{ e.host }}{{ e.port && e.port !== '22' ? `:${e.port}` : '' }}</span>
                      </span>
                      <span class="config-entry-badges">
                        <span
                          v-if="willOverwrite(e)" class="overwrite-badge"
                          :data-test="`ssh-config-entry-overwrite-${i}`"
                          v-tooltip="t('ssh.configImport.overwriteTitle')"
                        >{{ t("ssh.configImport.overwriteBadge") }}</span>
                        <!-- Only a ProxyCommand entry carries the "not
                             directly connectable" disclaimer now; a ProxyJump
                             entry shows its route, which it can be connected
                             through once each hop is saved as a host. -->
                        <span
                          v-if="hasProxyMarker(e)" class="proxy-badge"
                          :class="{ blocked: runsProxyCommand(e) }"
                          :data-test="`ssh-config-entry-proxy-${i}`"
                          v-tooltip="proxyReason(e)"
                        >{{
                          runsProxyCommand(e)
                            ? t("ssh.configImport.proxyBadge", { label: proxyLabel(e) })
                            : proxyLabel(e)
                        }}</span>
                      </span>
                    </label>
                  </li>
                </ul>
                <template v-if="previewSkipped.length">
                  <p class="config-section-title">{{ t("ssh.configImport.skippedTitle", { count: previewSkipped.length }) }}</p>
                  <ul class="config-skipped" data-test="ssh-config-import-skipped">
                    <li v-for="(s, i) in previewSkipped" :key="`${s.alias}-${i}`" class="skip-row">
                      <span class="skip-alias">{{ s.alias }}</span>
                      <span class="skip-reason">{{ s.reason }}</span>
                    </li>
                  </ul>
                </template>
              </template>
              <!-- Backend-authored: PreviewSSHConfigImport returns this note
                   already worded, so it is rendered verbatim, not keyed. -->
              <p class="config-note">{{ configPreview.note }}</p>
            </template>
          </div>
          <div class="drawer-foot config-foot">
            <p v-if="configConfirmError" class="ssh-error inline" data-test="ssh-config-import-confirm-error">
              {{ configConfirmError }}
            </p>
            <div class="foot-actions">
              <button class="btn ghost" @click="closeConfigImportDrawer">{{ t("common.cancel") }}</button>
              <button
                class="btn primary" data-test="ssh-config-import-confirm"
                :disabled="!canImportConfigSelection || configImporting"
                @click="confirmConfigImport"
              >{{ configImporting ? t("ssh.configImport.importing") : t("ssh.configImport.confirm", { count: configSelected.size }) }}</button>
            </div>
          </div>
        </aside>
      </transition>
      <TermiusImportDrawer v-if="termiusDrawer" @close="termiusDrawer = false" @imported="onTermiusImported" />
    </div>
  </div>
</template>

<style scoped>
.ssh-overlay {
  position: fixed; inset: 0; z-index: 120;
  background: rgba(1, 4, 9, 0.72); backdrop-filter: blur(3px);
  display: flex; align-items: center; justify-content: center;
}
.ssh-shell {
  position: relative;
  width: min(1040px, calc(100vw - 48px)); height: min(720px, calc(100vh - 48px));
  background: radial-gradient(120% 80% at 100% 0%, rgba(88, 166, 255, 0.06), transparent 55%), var(--bg);
  border: 1px solid var(--border); border-radius: 12px;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.55); overflow: hidden;
  display: flex; flex-direction: column;
}
.ssh-header {
  display: flex; align-items: center; gap: 12px; flex-wrap: wrap;
  padding: 10px 16px; border-bottom: 1px solid var(--border); background: var(--panel);
}
.tabs { display: flex; gap: 4px; }
.tab {
  display: inline-flex; align-items: center; gap: 6px;
  background: transparent; border: 1px solid transparent; color: var(--fg-dim);
  font-size: 12px; font-weight: 600; letter-spacing: 0.04em;
  padding: 6px 12px; border-radius: 7px; cursor: pointer; transition: all 120ms;
}
.tab:hover { color: var(--fg); }
.tab.on { color: var(--fg); background: var(--bg); border-color: var(--border); }
.tab-count { font-size: 10px; color: var(--fg-dim); background: rgba(139,148,158,0.16); padding: 1px 6px; border-radius: 999px; }
.search { flex: 1 1 180px; min-width: 120px; max-width: 360px; display: flex; align-items: center; gap: 7px; background: var(--bg); border: 1px solid var(--border); border-radius: 7px; padding: 0 10px; }
.search-spacer { flex: 1; }
.search-icon { color: var(--fg-dim); flex: none; }
.search input { flex: 1; min-width: 0; background: transparent; border: none; outline: none; color: var(--fg); font-size: 13px; padding: 7px 0; }
.new-btn { display: inline-flex; align-items: center; gap: 5px; background: var(--accent); color: #04101f; border: none; font-size: 12px; font-weight: 600; padding: 7px 12px; border-radius: 7px; cursor: pointer; transition: filter 120ms; }
.new-btn:hover { filter: brightness(1.1); }
.new-btn.ghost { background: transparent; color: var(--accent); border: 1px solid var(--accent); }
.close-x { display: inline-flex; align-items: center; justify-content: center; background: transparent; border: none; color: var(--fg-dim); cursor: pointer; padding: 4px; border-radius: 6px; transition: color 120ms, background 120ms; }
.close-x:hover { color: var(--fg); background: rgba(139, 148, 158, 0.12); }
.ssh-error { margin: 0; padding: 8px 16px; font-size: 12px; color: var(--bad); background: rgba(248, 81, 73, 0.08); border-bottom: 1px solid rgba(248, 81, 73, 0.2); }
.ssh-error.inline { border-bottom: none; border-radius: 6px; }
.ssh-tofu {
  margin: 0; padding: 10px 16px; display: flex; flex-direction: column; gap: 6px;
  background: rgba(210, 153, 34, 0.08);
  border-bottom: 1px solid rgba(210, 153, 34, 0.3);
}
.ssh-tofu p { margin: 0; font-size: 12px; line-height: 1.5; }
.ssh-tofu .tofu-title { font-weight: 600; color: var(--warn, #d29922); }
.ssh-tofu .tofu-msg { color: var(--fg); }
.ssh-tofu .tofu-note { color: var(--fg-dim); font-size: 11px; }
.ssh-tofu .tofu-host { font-size: 12px; word-break: break-all; color: var(--fg-dim); }
.ssh-tofu .tofu-fp { font-size: 12px; word-break: break-all; color: var(--fg); }
.ssh-tofu .tofu-actions { display: flex; gap: 8px; justify-content: flex-end; }
.ssh-success { margin: 0; padding: 8px 16px; font-size: 12px; color: var(--good, #3fb950); background: rgba(63, 185, 80, 0.08); border-bottom: 1px solid rgba(63, 185, 80, 0.2); }
.ssh-body { flex: 1; overflow-y: auto; padding: 18px 16px 24px; }
.host-section-title { display: flex; align-items: center; gap: 8px; margin: 0 0 12px; font-size: 13px; font-weight: 600; color: var(--fg); }
.host-section-title > span { color: var(--fg-dim); font-size: 12px; font-weight: 400; }
.host-groups { margin-bottom: 26px; }
.group-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(240px, 100%), 1fr)); gap: 10px; }
.group-card { display: flex; align-items: center; gap: 12px; min-width: 0; padding: 12px; background: var(--panel); border: 1px solid var(--border); border-radius: 8px; color: var(--fg); text-align: left; cursor: pointer; }
.group-card:hover { border-color: var(--accent); }
.group-glyph { display: flex; align-items: center; justify-content: center; flex: none; width: 38px; height: 38px; border-radius: 8px; color: var(--accent); background: color-mix(in srgb, var(--accent) 12%, transparent); }
.group-main { display: flex; flex-direction: column; gap: 4px; flex: 1; min-width: 0; }
.group-name { font-size: 13px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.group-count { font-size: 12px; color: var(--fg-dim); }
.group-chevron { flex: none; color: var(--fg-dim); }
.host-breadcrumb { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-bottom: 22px; color: var(--fg-dim); font-size: 13px; }
.host-breadcrumb button { display: inline-flex; align-items: center; gap: 6px; min-height: 32px; background: transparent; border: none; padding: 0 4px; color: var(--fg-dim); font: inherit; cursor: pointer; }
.host-breadcrumb button:hover { color: var(--fg); }
.host-breadcrumb > span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--fg); }
.group-card:focus-visible, .host-card:focus-visible, .host-breadcrumb button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.host-grid { grid-template-columns: repeat(auto-fill, minmax(min(280px, 100%), 1fr)); }
.host-card { min-height: 74px; align-items: flex-start; border-radius: 8px; }
.host-card:hover { transform: none; box-shadow: none; }
.host-card .card-glyph { width: 38px; height: 38px; border: none; background: color-mix(in srgb, var(--accent) 10%, transparent); }
.host-card .card-sub { font-size: 12px; }
.host-card .card-tags { max-height: 20px; overflow: hidden; }
.host-card .card-actions { position: absolute; right: 10px; top: 12px; flex-direction: column; }
.host-card .card-actions .act { display: none; }
.host-card:hover .card-actions, .host-card:focus-within .card-actions { flex-direction: row; padding-left: 6px; background: var(--panel); }
.host-card:hover .card-actions .act, .host-card:focus-within .card-actions .act { display: inline-flex; }
.host-grid + .empty, .host-groups ~ .empty, .host-breadcrumb ~ .empty { min-height: 140px; height: auto; }
@media (hover: none) {
  .host-card { padding-bottom: 46px; }
  .host-card .card-actions { top: auto; bottom: 10px; flex-direction: row; }
  .host-card .card-actions .act { display: inline-flex; }
}
.card-proxy { display: flex; margin-top: 5px; }
.card-proxy .proxy-badge { max-width: 100%; text-align: left; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.card-tags { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 5px; }
.card-tag {
  font-size: 10px; color: var(--fg-dim); background: rgba(139, 148, 158, 0.16);
  padding: 1px 7px; border-radius: 999px; white-space: nowrap;
}
.card-grid { display: grid; gap: 10px; grid-template-columns: repeat(auto-fill, minmax(288px, 1fr)); }
.card { position: relative; display: flex; align-items: center; gap: 11px; padding: 12px; background: var(--panel); border: 1px solid var(--border); border-radius: 10px; transition: border-color 140ms, transform 140ms, box-shadow 140ms; }
.card:hover { border-color: var(--accent); transform: translateY(-1px); box-shadow: 0 6px 18px rgba(0, 0, 0, 0.35); }
.card.busy { opacity: 0.6; }
.card-glyph { flex: none; width: 34px; height: 34px; border-radius: 8px; display: flex; align-items: center; justify-content: center; color: var(--accent); background: rgba(88, 166, 255, 0.1); border: 1px solid rgba(88, 166, 255, 0.18); }
.card-main { flex: 1; min-width: 0; }
.card-label { font-size: 13px; font-weight: 600; color: var(--fg); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.card-sub { font-size: 11px; color: var(--fg-dim); font-family: var(--font-mono-strict); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; margin-top: 2px; }
.card-actions { flex: none; display: flex; gap: 4px; align-items: center; }
.act { display: inline-flex; align-items: center; justify-content: center; width: 28px; height: 28px; border-radius: 6px; background: var(--bg); border: 1px solid var(--border); color: var(--fg); cursor: pointer; transition: color 120ms, background 120ms, border-color 120ms; }
.act svg { display: block; flex: none; width: 15px; height: 15px; }
.act:hover { color: #fff; border-color: var(--neutral); background: rgba(139, 148, 158, 0.18); }
.act.danger { color: var(--fg-dim); }
.act.danger:hover { color: #fff; border-color: var(--bad); background: rgba(248, 81, 73, 0.2); }
.act:disabled { opacity: 0.5; cursor: default; }
.empty { height: 100%; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 6px; color: var(--fg-dim); }
.empty-icon { color: var(--neutral); opacity: 0.5; margin-bottom: 6px; }
.empty-title { margin: 0; font-size: 14px; color: var(--fg); font-weight: 600; }
.empty-sub { margin: 0 0 12px; font-size: 12px; }
.drawer { position: absolute; top: 0; right: 0; bottom: 0; width: 360px; max-width: 84%; background: var(--panel); border-left: 1px solid var(--border); box-shadow: -20px 0 48px rgba(0, 0, 0, 0.45); display: flex; flex-direction: column; }
.drawer.wide { width: 480px; }
.drawer-head { display: flex; align-items: center; justify-content: space-between; padding: 14px 16px; border-bottom: 1px solid var(--border); font-size: 12px; font-weight: 700; letter-spacing: 0.1em; text-transform: uppercase; color: var(--fg-dim); }
.drawer-body { flex: 1; overflow-y: auto; padding: 16px; display: flex; flex-direction: column; gap: 12px; }
.field { display: flex; flex-direction: column; gap: 5px; }
.field-row { display: flex; gap: 10px; }
.field.grow { flex: 1; }
.field.port { width: 78px; flex: none; }
.fl { font-size: 11px; color: var(--fg-dim); }
.fl em { color: var(--neutral); font-style: normal; }
.field input, .field textarea { background: var(--bg); border: 1px solid var(--border); border-radius: 6px; padding: 7px 9px; color: var(--fg); font-size: 13px; outline: none; transition: border-color 120ms; }
.field input:focus, .field textarea:focus { border-color: var(--accent); }
.field textarea { resize: vertical; font-family: var(--font-mono-strict); font-size: 12px; }
.hint { font-size: 12px; color: var(--fg-dim); margin: 0; }
.tag-editor {
  position: relative; display: flex; flex-wrap: wrap; align-items: center; gap: 4px;
  background: var(--bg); border: 1px solid var(--border); border-radius: 6px;
  padding: 5px 6px; transition: border-color 120ms;
}
.tag-editor:focus-within { border-color: var(--accent); }
.tag-editor .chip {
  display: inline-flex; align-items: center; gap: 3px;
  font-size: 11px; color: var(--fg); background: rgba(88, 166, 255, 0.14);
  border: 1px solid rgba(88, 166, 255, 0.28); padding: 1px 4px 1px 8px; border-radius: 999px;
}
.chip-x {
  display: inline-flex; align-items: center; justify-content: center;
  background: transparent; border: none; color: var(--fg-dim); cursor: pointer;
  padding: 1px; border-radius: 999px; line-height: 1;
}
.chip-x:hover { color: #fff; background: rgba(248, 81, 73, 0.35); }
.tag-input {
  flex: 1; min-width: 90px;
  background: transparent; border: none; outline: none;
  color: var(--fg); font-size: 13px; padding: 2px 3px;
}
.combo { position: relative; display: flex; }
.combo input { flex: 1; padding-right: 26px; width: 100%; }
.combo-caret {
  position: absolute; right: 6px; top: 50%; transform: translateY(-50%);
  background: transparent; border: none; color: var(--fg-dim); cursor: pointer;
  font-size: 11px; padding: 2px 4px; line-height: 1;
}
.combo-caret:hover { color: var(--fg); }
.combo-menu {
  position: absolute; top: calc(100% + 4px); left: 0; right: 0; z-index: 10;
  margin: 0; padding: 4px 0; list-style: none; max-height: 180px; overflow-y: auto;
  background: var(--bg); border: 1px solid var(--border); border-radius: 6px;
  box-shadow: 0 6px 16px rgba(0, 0, 0, 0.35);
}
.combo-opt { padding: 7px 10px; font-size: 13px; color: var(--fg); cursor: pointer; }
.combo-opt:hover { background: rgba(255, 255, 255, 0.06); }
.identity-hint {
  margin: -4px 0 0; display: flex; flex-direction: column; gap: 3px;
  font-size: 11px; color: var(--fg-dim); line-height: 1.4;
}
.identity-hint code {
  font-family: var(--font-mono-strict); font-size: 11px; color: var(--fg);
  background: var(--bg); border: 1px solid var(--border); border-radius: 5px;
  padding: 4px 7px; word-break: break-all;
}
.identity-hint.proxy { color: var(--warn, #d29922); }
.empty-keys-hint { display: flex; flex-direction: column; align-items: flex-start; gap: 8px; }
.enc-hint { margin: -4px 0 0; font-size: 11px; color: var(--warn, #d29922); }
.import-block { margin-top: 4px; display: flex; flex-direction: column; gap: 8px; }
.file-input { position: absolute; width: 1px; height: 1px; opacity: 0; pointer-events: none; }
.dropzone {
  display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 6px;
  padding: 18px 12px; text-align: center;
  border: 1px dashed var(--border); border-radius: 8px;
  color: var(--fg-dim); font-size: 11px; line-height: 1.4;
  transition: border-color 120ms, background 120ms, color 120ms;
}
.dropzone-icon { color: var(--neutral); }
.dropzone.over { border-color: var(--accent); color: var(--fg); background: rgba(88, 166, 255, 0.08); }
.dropzone.over .dropzone-icon { color: var(--accent); }
.btn.import { display: inline-flex; align-items: center; justify-content: center; gap: 6px; width: 100%; }
.btn.import svg { display: block; }
.btn.import:hover { background: rgba(139, 148, 158, 0.1); }
.btn.sm { display: inline-flex; align-items: center; gap: 5px; padding: 6px 11px; font-size: 12px; }
.btn.sm svg { display: block; }
.seg { display: flex; background: var(--bg); border: 1px solid var(--border); border-radius: 7px; padding: 3px; gap: 3px; }
.seg button { flex: 1; background: transparent; border: none; color: var(--fg-dim); font-size: 12px; padding: 6px 0; border-radius: 5px; cursor: pointer; transition: all 120ms; }
.seg button.on { background: var(--accent); color: #04101f; font-weight: 600; }
.drawer-foot { display: flex; justify-content: flex-end; gap: 8px; padding: 12px 16px; border-top: 1px solid var(--border); }
.btn { font-size: 12px; padding: 8px 14px; border-radius: 7px; cursor: pointer; border: 1px solid var(--border); background: transparent; color: var(--fg); transition: all 120ms; }
.btn.ghost:hover { background: rgba(139, 148, 158, 0.1); }
.btn.primary { background: var(--accent); color: #04101f; border-color: var(--accent); font-weight: 600; }
.btn.primary:hover:not(:disabled) { filter: brightness(1.1); }
.btn.primary:disabled { opacity: 0.4; cursor: default; }
.drawer-enter-active, .drawer-leave-active { transition: transform 180ms ease, opacity 180ms ease; }
.drawer-enter-from, .drawer-leave-to { transform: translateX(24px); opacity: 0; }
.config-section-title { margin: 4px 0 2px; font-size: 11px; font-weight: 700; letter-spacing: 0.06em; text-transform: uppercase; color: var(--fg-dim); }
.config-entry-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.config-entry { background: var(--bg); border: 1px solid var(--border); border-radius: 8px; }
.config-entry-label { display: flex; align-items: center; gap: 10px; padding: 9px 10px; cursor: pointer; }
.config-entry-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.config-entry-alias { font-size: 13px; font-weight: 600; color: var(--fg); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.config-entry-sub { font-size: 11px; color: var(--fg-dim); font-family: var(--font-mono-strict); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.config-entry-badges { flex: none; display: flex; flex-direction: column; align-items: flex-end; gap: 4px; }
/* Neutral by default: a ProxyJump badge describes the route a connection takes,
   which is information, not a warning. The amber is reserved for the one that
   still refuses to connect. */
.proxy-badge { flex: none; font-size: 10px; color: var(--fg-dim); background: rgba(139, 148, 158, 0.12); border: 1px solid rgba(139, 148, 158, 0.3); padding: 3px 8px; border-radius: 999px; max-width: 150px; line-height: 1.3; text-align: right; }
.proxy-badge.blocked { color: var(--warn, #d29922); background: rgba(210, 153, 34, 0.12); border-color: rgba(210, 153, 34, 0.3); }
.overwrite-badge { flex: none; font-size: 10px; color: var(--fg-dim); background: rgba(139, 148, 158, 0.16); border: 1px solid var(--border); padding: 3px 8px; border-radius: 999px; line-height: 1.3; white-space: nowrap; }
.config-skipped { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
.skip-row { display: flex; align-items: baseline; gap: 8px; padding: 6px 10px; background: rgba(139, 148, 158, 0.08); border-radius: 6px; }
.skip-alias { font-size: 12px; font-weight: 600; color: var(--fg); flex: none; }
.skip-reason { font-size: 11px; color: var(--fg-dim); }
.config-note { margin: 6px 0 0; font-size: 11px; color: var(--fg-dim); line-height: 1.5; }
.config-foot { flex-direction: column; align-items: stretch; gap: 8px; }
.foot-actions { display: flex; justify-content: flex-end; gap: 8px; }

/* --- port forwarding: rules editor (host drawer) --- */
.fwd-section { display: flex; flex-direction: column; gap: 8px; padding-top: 12px; border-top: 1px solid var(--border); }
.fwd-section-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.fwd-section-head .fl { font-size: 11px; font-weight: 700; letter-spacing: 0.06em; text-transform: uppercase; }
.fwd-rule { display: flex; flex-direction: column; gap: 8px; padding: 10px; background: var(--bg); border: 1px solid var(--border); border-radius: 8px; }
.fwd-rule-head { display: flex; align-items: center; gap: 8px; }
.fwd-rule-head .seg { flex: 1; }
.seg.sm button { font-size: 11px; padding: 4px 0; }
.fwd-warn {
  margin: 0; padding: 8px 9px; font-size: 11px; line-height: 1.5;
  color: var(--warn, #d29922); background: rgba(210, 153, 34, 0.1);
  border: 1px solid rgba(210, 153, 34, 0.32); border-radius: 6px;
}

/* --- port forwarding: active tunnels --- */
.fwd-proxy-reason { margin: 0 0 8px; font-size: 11px; line-height: 1.5; color: var(--warn, #d29922); }

.tunnel-toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; }
.tunnel-search { max-width: none; }
.tunnel-view-controls { display: flex; gap: 10px; margin-left: auto; }
.tunnel-view-control { display: flex; align-items: center; gap: 6px; color: var(--fg-dim); }
.tunnel-view-control > svg { flex: none; }
.tunnel-view-control .select-dropdown { width: 94px; }
.tunnel-sort-control .select-dropdown { width: 154px; }
.tunnel-no-match { height: auto; min-height: 180px; }
.tunnel-list { grid-template-columns: minmax(0, 1fr); }
.tunnel-list .fwd-card { align-items: center; padding-top: 10px; padding-bottom: 10px; }
.tunnel-list .fwd-card-side { flex-direction: row; align-items: center; }
.tunnel-list .card-main { display: grid; grid-template-columns: minmax(120px, 1fr) minmax(0, 2fr); gap: 4px 16px; align-items: center; }
.tunnel-list .fwd-route { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.tunnel-list .card-proxy, .tunnel-list .fwd-proxy-reason, .tunnel-list .fwd-row-conns, .tunnel-list .fwd-row-error { grid-column: 1 / -1; }
@media (max-width: 640px) {
  .tunnel-search { flex-basis: 100%; }
  .tunnel-view-controls { margin-left: 0; }
  .tunnel-list .card-main { grid-template-columns: minmax(0, 1fr); }
}
.fwd-card { align-items: flex-start; }
.key-card:focus-visible, .fwd-card:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.card .hover-action { visibility: hidden; }
.card:hover .hover-action, .card:focus-within .hover-action { visibility: visible; }
@media (hover: none) { .card .hover-action { visibility: visible; } }
.fwd-status { display: flex; align-items: center; justify-content: flex-end; gap: 6px; }
.fwd-card-side { flex: none; display: flex; flex-direction: column; align-items: flex-end; gap: 6px; }
.fwd-card .card-label { display: flex; align-items: baseline; gap: 6px; }
/* The route is the one thing on the card you actually read, so it wraps
   instead of inheriting .card-sub's ellipsis — a truncated address is not
   worth having on screen. */
.fwd-route { color: color-mix(in srgb, var(--fg-dim) 70%, var(--panel)); white-space: normal; overflow: visible; word-break: break-all; }
.fwd-kind { flex: none; font-size: 10px; font-weight: 700; letter-spacing: 0.06em; text-transform: uppercase; color: var(--accent); }
.fwd-note { font-size: 12px; color: var(--fg); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.fwd-row-conns { margin-top: 2px; font-size: 10px; color: var(--fg-dim); }
.fwd-row-error { margin: 5px 0 0; font-size: 11px; line-height: 1.5; color: var(--bad); }
.fwd-state { flex: none; font-size: 10px; padding: 3px 9px; border-radius: 999px; border: 1px solid var(--border); color: var(--fg-dim); background: rgba(139, 148, 158, 0.14); }
.fwd-state.connecting { color: var(--accent); border-color: var(--accent); }
.fwd-state.running { color: var(--good, #3fb950); border-color: rgba(63, 185, 80, 0.35); background: rgba(63, 185, 80, 0.12); }
.fwd-state.stopped { color: var(--bad); border-color: rgba(248, 81, 73, 0.35); background: rgba(248, 81, 73, 0.12); }

/* --- key drawer: reveal / copy --- */
.pem-label { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.pem-tools { display: flex; gap: 4px; }
</style>
