import type { TaskState } from "./taskState";
import { commandLabel, titleOrCommand } from "./sessionLabel";

/**
 * PetSessionSource lists exactly the fields the projection reads, so both
 * SessionInfo (which keys on `id`) and the merged RemoteSession shape (which
 * keys on `session_id`) satisfy it structurally. The pet renders the *merged*
 * list, so it must accept the merged type without a lossy cast at the call
 * site.
 */
export interface PetSessionSource {
  id?: string;
  session_id?: string;
  command?: string;
  cwd?: string;
  title?: string;
  current_command?: string;
  task_state?: TaskState | string;
  command_exit_code?: number;
  command_duration_ms?: number;
  command_started_at?: number;
  last_output_at?: number;
  started_at?: number;
  host_id?: string;
  host?: string;
  type?: string;
}

function idOf(s: PetSessionSource): string {
  return s.id ?? s.session_id ?? "";
}

/**
 * petState projects the merged session list into the minimal snapshot the
 * companion (pet) window renders. It is a pure function so the whole
 * aggregation policy — priority ordering, headline wording, truncation —
 * is unit-testable without spawning a window or a webview.
 *
 * The pet process never connects to anything (see
 * docs/superpowers/specs/2026-08-10-ai-pet-companion-window-design.md):
 * the main app owns the merged + unsealed list and pushes this projection
 * down a pipe, so no relay token and no account_key ever leaves the main
 * process (red line #21).
 *
 * Row labels come from sessionLabel.ts rather than a private heuristic, so
 * the pet and the sidebar always name a session the same way.
 */

export type PetMood = "idle" | "running" | "waiting" | "failed";

export interface PetSessionRow {
  sessionId: string;
  /** Display name — same helper the sidebar row uses. */
  title: string;
  /** Current command, exit status, or duration. May be empty. */
  subtitle: string;
  state: PetMood;
  /** "claude" | "codex" | … — empty when the session is not classified AI. */
  kind: string;
  /** Non-empty when the session runs on another machine (via relay). */
  remoteHost: string;
  /** Wall-clock age of the current command in ms; 0 when not running. */
  ageMs: number;
}

export interface PetState {
  /** Aggregate mood across every session: waiting > failed > running > idle. */
  mood: PetMood;
  waitingCount: number;
  runningCount: number;
  failedCount: number;
  completedCount: number;
  /** Sessions doing nothing and not recently finished — typically a shell
   *  sitting at its prompt, which is the most common state of all. */
  idleCount: number;
  /** Primary line, e.g. "1 个等你输入". */
  headline: string;
  /** Secondary line, e.g. "2 个在跑 · 1 个已完成". */
  subline: string;
  rows: PetSessionRow[];
  /** Sessions beyond maxRows that were truncated out of `rows`. */
  overflowCount: number;
}

/** Rows shown in the expanded panel. Beyond this the tail is summarised. */
export const PET_MAX_ROWS = 6;

/** Ordering weight within the list — lower sorts first. */
const STATE_ORDER: Record<PetMood, number> = {
  waiting: 0,
  failed: 1,
  running: 2,
  idle: 3,
};

/** Aggregate precedence — higher wins when folding many sessions into one mood. */
const MOOD_RANK: Record<PetMood, number> = {
  idle: 0,
  running: 1,
  failed: 2,
  waiting: 3,
};

/**
 * moodOf maps a session's TaskState onto the four moods the pet can express.
 *
 * `disconnected` folds into `idle` rather than `failed` on purpose: a dropped
 * relay is not a failed command, and painting the pet red every time the
 * network hiccups trains the user to ignore red.
 */
export function moodOf(state: TaskState | string | undefined): PetMood {
  switch (state) {
    case "waiting_input":
      return "waiting";
    case "failed":
      return "failed";
    case "running":
      return "running";
    default:
      return "idle";
  }
}

function basename(p: string): string {
  const trimmed = p.trim().replace(/[/\\]+$/, "");
  if (!trimmed) return "";
  const idx = Math.max(trimmed.lastIndexOf("/"), trimmed.lastIndexOf("\\"));
  return idx >= 0 ? trimmed.slice(idx + 1) : trimmed;
}

/**
 * displayTitle names a row the way the sidebar would, with one extra fallback.
 *
 * titleOrCommand() bottoms out at `session_id.slice(0, 8)` when a session has
 * neither a title nor a current command — fine in the sidebar, which also
 * shows cwd and host, but in the pet's one-line row that renders as a bare
 * hex blob. A plain idle shell is exactly that case, so fall back to the cwd
 * basename (then the launch command) before accepting the id.
 */
export function displayTitle(s: PetSessionSource): string {
  const labelInput = {
    current_command: s.current_command,
    title: s.title,
    session_id: idOf(s),
    cwd: s.cwd,
    type: s.type,
  };
  if ((s.current_command ?? "").trim() || (s.title ?? "").trim()) {
    return titleOrCommand(labelInput);
  }
  const base = basename(s.cwd ?? "");
  if (base) return base;
  const cmd = (s.command ?? "").trim();
  if (cmd) return commandLabel({ current_command: cmd, title: "", session_id: idOf(s) });
  return titleOrCommand(labelInput);
}

function formatDuration(ms: number): string {
  if (ms <= 0) return "";
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m${String(s % 60).padStart(2, "0")}s`;
  const h = Math.floor(m / 60);
  return `${h}h${String(m % 60).padStart(2, "0")}m`;
}

/**
 * subtitleOf describes what the session is doing right now. Running sessions
 * show the live command; finished ones show how they ended, because "exit 1"
 * is the single most useful thing to know at a glance.
 */
export function subtitleOf(s: PetSessionSource, mood: PetMood): string {
  const current = (s.current_command ?? "").trim();
  if (mood === "running" || mood === "waiting") return current;

  const code = s.command_exit_code;
  const dur = formatDuration(s.command_duration_ms ?? 0);
  if (typeof code === "number") {
    const head = `exit ${code}`;
    return dur ? `${head} · ${dur}` : head;
  }
  return current;
}

function ageOf(s: PetSessionSource, mood: PetMood, nowMs: number): number {
  if (mood !== "running" && mood !== "waiting") return 0;
  const started = s.command_started_at;
  if (!started) return 0;
  // command_started_at is a unix seconds timestamp on the wire.
  const age = nowMs - started * 1000;
  return age > 0 ? age : 0;
}

/**
 * lastActivityOf is the tiebreaker within a priority band: most recently
 * active first. Falls back through last output → command start → session
 * start so a session always has a stable ordering key.
 */
function lastActivityOf(s: PetSessionSource): number {
  return s.last_output_at || s.command_started_at || s.started_at || 0;
}

export interface ProjectPetStateOptions {
  /** Host id of this machine; sessions with a different host_id are remote. */
  localHostId?: string;
  /** Injected for deterministic tests. Defaults to Date.now(). */
  nowMs?: number;
  maxRows?: number;
}

/**
 * projectPetState folds the merged session list into a PetState.
 *
 * Sessions in `closed` state are dropped entirely — a closed session is not
 * something the user can act on, and keeping them would let a long-lived
 * window accumulate dead rows.
 */
export function projectPetState(
  sessions: readonly PetSessionSource[],
  opts: ProjectPetStateOptions = {},
): PetState {
  const nowMs = opts.nowMs ?? Date.now();
  const maxRows = opts.maxRows ?? PET_MAX_ROWS;
  const localHostId = (opts.localHostId ?? "").trim();

  const live = sessions.filter((s) => s.task_state !== "closed");

  // Sort carries the activity key alongside the row so the comparator stays
  // O(n log n) — looking the session back up inside compare() would be O(n²).
  const decorated = live.map((s) => {
    const mood = moodOf(s.task_state);
    const hostId = (s.host_id ?? "").trim();
    const isRemote = !!localHostId && !!hostId && hostId !== localHostId;
    const labelInput = {
      current_command: s.current_command,
      title: s.title,
      session_id: idOf(s),
      cwd: s.cwd,
      type: s.type,
    };
    const row: PetSessionRow = {
      sessionId: idOf(s),
      title: displayTitle(s),
      subtitle: subtitleOf(s, mood),
      state: mood,
      kind: s.type === "ai" ? commandLabel(labelInput) : "",
      remoteHost: isRemote ? (s.host ?? "").trim() : "",
      ageMs: ageOf(s, mood, nowMs),
    };
    return { row, activity: lastActivityOf(s) };
  });

  decorated.sort((a, b) => {
    const d = STATE_ORDER[a.row.state] - STATE_ORDER[b.row.state];
    if (d !== 0) return d;
    return b.activity - a.activity;
  });

  const rows = decorated.map((d) => d.row);

  const waitingCount = rows.filter((r) => r.state === "waiting").length;
  const failedCount = rows.filter((r) => r.state === "failed").length;
  const runningCount = rows.filter((r) => r.state === "running").length;
  // "completed" is not a mood — a finished session reads as idle. Count it off
  // the raw task_state so the subline can still say "1 个已完成".
  const completedCount = live.filter((s) => s.task_state === "completed").length;

  let mood: PetMood = "idle";
  for (const r of rows) {
    if (MOOD_RANK[r.state] > MOOD_RANK[mood]) mood = r.state;
  }

  // Everything else in the idle band: shells sitting at a prompt, and
  // disconnected sessions. Subtracting completed stops a finished session
  // being counted in two bands at once.
  const idleCount = Math.max(
    0,
    rows.filter((r) => r.state === "idle").length - completedCount,
  );

  const counts = { waitingCount, failedCount, runningCount, completedCount, idleCount };

  return {
    mood,
    ...counts,
    ...summarize(counts),
    rows: rows.slice(0, maxRows),
    overflowCount: Math.max(0, rows.length - maxRows),
  };
}

interface Counts {
  waitingCount: number;
  failedCount: number;
  runningCount: number;
  completedCount: number;
  idleCount: number;
}

/**
 * summarize turns the counts into the two header lines.
 *
 * Bands are listed in priority order and the first one becomes the headline,
 * the rest the subline — so the two lines never repeat the same number, and
 * every non-zero band is accounted for exactly once.
 *
 * The idle band matters more than it looks: a shell sitting at its prompt is
 * the single most common session state, and leaving it out of the count made
 * a window listing ten live sessions announce "没有会话".
 */
function summarize(c: Counts): { headline: string; subline: string } {
  const parts: string[] = [];
  if (c.waitingCount > 0) parts.push(`${c.waitingCount} 个等你输入`);
  if (c.failedCount > 0) parts.push(`${c.failedCount} 个失败`);
  if (c.runningCount > 0) parts.push(`${c.runningCount} 个在跑`);
  if (c.completedCount > 0) parts.push(`${c.completedCount} 个已完成`);
  if (c.idleCount > 0) parts.push(`${c.idleCount} 个空闲`);

  if (parts.length === 0) return { headline: "没有会话", subline: "" };
  // Work finished and nothing else going on — worth saying in words rather
  // than as a bare count.
  if (parts.length === 1 && c.completedCount > 0) {
    return { headline: "都跑完了", subline: parts[0] };
  }
  return { headline: parts[0], subline: parts.slice(1).join(" · ") };
}
