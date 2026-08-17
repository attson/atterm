import type { TaskState } from "./taskState";
import { commandLabel, titleOrCommand } from "./sessionLabel";
import { shortenCwd } from "./shortenCwd";
import { compareSessionsBySidebarOrder } from "./sessionSort";

/**
 * WidgetSessionSource lists exactly the fields the projection reads, so both
 * SessionInfo (which keys on `id`) and the merged RemoteSession shape (which
 * keys on `session_id`) satisfy it structurally. The widget renders the *merged*
 * list, so it must accept the merged type without a lossy cast at the call
 * site.
 */
export interface WidgetSessionSource {
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
  command_ended_at?: number;
  attention_at?: number;
  last_output_at?: number;
  started_at?: number;
  host_id?: string;
  host?: string;
  type?: string;
  unread?: boolean;
}

function idOf(s: WidgetSessionSource): string {
  return s.id ?? s.session_id ?? "";
}

/**
 * widgetState projects the merged session list into the minimal snapshot the
 * companion window renders. It is a pure function so the whole
 * aggregation policy — priority ordering, headline wording, truncation —
 * is unit-testable without spawning a window or a webview.
 *
 * The widget process never connects to anything (see
 * docs/superpowers/specs/2026-08-10-desk-widget-design.md):
 * the main app owns the merged + unsealed list and pushes this projection
 * down a pipe, so no relay token and no account_key ever leaves the main
 * process (red line #21).
 *
 * Row labels come from sessionLabel.ts rather than a private heuristic, so
 * the widget and the sidebar always name a session the same way.
 */

export type WidgetMood = "idle" | "running" | "waiting" | "failed";

export interface WidgetSessionRow {
  sessionId: string;
  /** Display name — same helper the sidebar row uses. */
  title: string;
  /** Where the session lives — the shortened cwd, same helper and same
   *  wording as the sidebar row. Empty when it would only repeat the title. */
  subtitle: string;
  state: WidgetMood;
  /** Original task state for the per-row status icon. */
  taskState: TaskState;
  /** "claude" | "codex" | … — empty when the session is not classified AI. */
  kind: string;
  /** Non-empty when the session runs on another machine (via relay). */
  remoteHost: string;
  /** Wall-clock age of the current command in ms; 0 when not running. */
  ageMs: number;
  /** Raw unix-seconds output timestamp; the widget formats it against its local clock. */
  lastOutputAt: number;
  /** Same per-user unread flag the sidebar renders. */
  unread: boolean;
}

export interface WidgetState {
  /** Aggregate mood across every session: running > waiting > failed > idle. */
  mood: WidgetMood;
  /** Sessions with output the user has not read yet. */
  unreadCount: number;
  waitingCount: number;
  runningCount: number;
  failedCount: number;
  completedCount: number;
  /** Sessions doing nothing and not recently finished — typically a shell
   *  sitting at its prompt, which is the most common state of all. */
  idleCount: number;
  /** Primary line, e.g. "2 个会话有最新内容". */
  headline: string;
  /** Secondary line containing the existing task-state summary. */
  subline: string;
  rows: WidgetSessionRow[];
  /**
   * Ids of every session currently demanding the user (waiting / failed),
   * computed over the WHOLE filtered list rather than the truncated `rows`.
   *
   * The widget diffs this set between pushes to decide whether a collapsed
   * window should raise its hand. Diffing `rows` instead makes a session that
   * merely scrolled past maxRows look like it left, so the next reordering
   * that brings it back reads as a fresh escalation and pops the widget open
   * for something that never changed.
   */
  attentionIds: string[];
  /** Sessions beyond maxRows that were truncated out of `rows`. */
  overflowCount: number;
  /** Mirrors the aiOnly setting so the widget's own menu can show its state
   *  without a second channel — the snapshot is pushed on every change. */
  aiOnly: boolean;
}

/** Rows shown in the expanded panel. Beyond this the tail is summarised. */
export const WIDGET_MAX_ROWS = 6;

/**
 * Aggregate precedence — higher wins when folding many sessions into one mood.
 *
 * Mirrors the sidebar's SESSION_URGENCY, running first: the widget is a
 * one-glance summary of the same list, and the two disagreeing about which
 * state is the headline would be worse than either order on its own.
 *
 * Note this governs only which face the sprite wears. Which session gets
 * highlighted, and whether a collapsed widget unfolds itself, still key off
 * waiting/failed — those are events that call for the user, which is a
 * different question from what the list is mostly doing.
 */
const MOOD_RANK: Record<WidgetMood, number> = {
  idle: 0,
  failed: 1,
  waiting: 2,
  running: 3,
};

/**
 * moodOf maps a session's TaskState onto the four moods the widget can express.
 *
 * `disconnected` folds into `idle` rather than `failed` on purpose: a dropped
 * relay is not a failed command, and painting the widget red every time the
 * network hiccups trains the user to ignore red.
 */
export function moodOf(state: TaskState | string | undefined): WidgetMood {
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

function taskStateOf(state: TaskState | string | undefined): TaskState {
  switch (state) {
    case "running":
    case "waiting_input":
    case "completed":
    case "failed":
    case "disconnected":
    case "closed":
      return state;
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
 * shows cwd and host, but in the widget's one-line row that renders as a bare
 * hex blob. A plain idle shell is exactly that case, so fall back to the cwd
 * basename (then the launch command) before accepting the id.
 */
export function displayTitle(s: WidgetSessionSource): string {
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


/**
 * subtitleOf gives the row its second line: where the session lives.
 *
 * It used to show the current command, which reads as noise in a 252px row —
 * a truncated `claude --permission-mode by…` tells you nothing you did not
 * already get from the title, while the directory tells you *which* of your
 * five claude sessions this is. Uses the sidebar's shortenCwd so both
 * surfaces elide paths identically.
 *
 * Returns "" when the path would only repeat the title (a shell whose OSC
 * title is already its directory, e.g. "~"), so the row stays one line
 * instead of saying the same word twice.
 */
export function subtitleOf(s: WidgetSessionSource, home: string, title: string): string {
  const short = shortenCwd(s.cwd, home);
  if (!short || short === title) return "";
  return short;
}

function ageOf(s: WidgetSessionSource, mood: WidgetMood, nowMs: number): number {
  if (mood !== "running" && mood !== "waiting") return 0;
  const started = s.command_started_at;
  if (!started) return 0;
  // command_started_at is a unix seconds timestamp on the wire.
  const age = nowMs - started * 1000;
  return age > 0 ? age : 0;
}

export interface ProjectWidgetStateOptions {
  /** Host id of this machine; sessions with a different host_id are remote. */
  localHostId?: string;
  /** The user's home directory, so cwds render as `~/…` like the sidebar. */
  home?: string;
  /** Restrict the list to AI-classified sessions (claude / codex / aider). */
  aiOnly?: boolean;
  /** Injected for deterministic tests. Defaults to Date.now(). */
  nowMs?: number;
  maxRows?: number;
}

/**
 * projectWidgetState folds the merged session list into a WidgetState.
 *
 * Sessions in `closed` state are dropped entirely — a closed session is not
 * something the user can act on, and keeping them would let a long-lived
 * window accumulate dead rows.
 */
export function projectWidgetState(
  sessions: readonly WidgetSessionSource[],
  opts: ProjectWidgetStateOptions = {},
): WidgetState {
  const nowMs = opts.nowMs ?? Date.now();
  const maxRows = opts.maxRows ?? WIDGET_MAX_ROWS;
  const localHostId = (opts.localHostId ?? "").trim();
  const home = opts.home ?? "";

  const aiOnly = opts.aiOnly ?? false;
  // `closed` sessions are dropped entirely — a closed session is nothing the
  // user can act on, and keeping them would let a long-lived window fill with
  // dead rows. The aiOnly filter runs here too so every count, the headline
  // and the overflow all describe the same filtered set.
  const live = sessions.filter(
    (s) => s.task_state !== "closed" && (!aiOnly || s.type === "ai"),
  );

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
    const title = displayTitle(s);
    const row: WidgetSessionRow = {
      sessionId: idOf(s),
      title,
      subtitle: subtitleOf(s, home, title),
      state: mood,
      taskState: taskStateOf(s.task_state),
      kind: s.type === "ai" ? commandLabel(labelInput) : "",
      remoteHost: isRemote ? (s.host ?? "").trim() : "",
      ageMs: ageOf(s, mood, nowMs),
      lastOutputAt: s.last_output_at ?? 0,
      unread: s.unread === true,
    };
    return { row, source: s };
  });

  // Always rank by urgency (running first), never by host. The widget is a
  // one-glance "what is moving right now" view, so a remote running session
  // must be able to outrank a local idle shell. Grouping local-first — the old
  // behaviour — buried remote sessions below a pile of idle local shells and,
  // once the list passed maxRows, sliced them off entirely, so a remote agent
  // that was actually running never showed up. This is the same comparator the
  // sidebar's state groups use, so the two surfaces agree on the order.
  decorated.sort((a, b) =>
    compareSessionsBySidebarOrder(
      { ...a.source, session_id: idOf(a.source) },
      { ...b.source, session_id: idOf(b.source) },
    ),
  );

  const rows = decorated.map((d) => d.row);

  const waitingCount = rows.filter((r) => r.state === "waiting").length;
  const failedCount = rows.filter((r) => r.state === "failed").length;
  const runningCount = rows.filter((r) => r.state === "running").length;
  const unreadCount = rows.filter((r) => r.unread).length;
  // "completed" is not a mood — a finished session reads as idle. Count it off
  // the raw task_state so the subline can still say "1 个已完成".
  const completedCount = live.filter((s) => s.task_state === "completed").length;

  let mood: WidgetMood = "idle";
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
    unreadCount,
    ...counts,
    ...summarize(unreadCount, counts),
    rows: rows.slice(0, maxRows),
    attentionIds: rows
      .filter((r) => r.state === "waiting" || r.state === "failed")
      .map((r) => r.sessionId),
    overflowCount: Math.max(0, rows.length - maxRows),
    aiOnly,
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
 * The headline reports unread sessions. The subline reports actionable task
 * states; idle sessions remain in the rows and count model but do not consume
 * scarce header space.
 */
function summarize(
  unreadCount: number,
  c: Counts,
): { headline: string; subline: string } {
  const parts: string[] = [];
  if (c.failedCount > 0) parts.push(`${c.failedCount} 个失败`);
  if (c.runningCount > 0) parts.push(`${c.runningCount} 个在跑`);
  if (c.waitingCount > 0) parts.push(`${c.waitingCount} 个等待输入`);
  if (c.completedCount > 0) parts.push(`${c.completedCount} 个已完成`);

  return {
    headline: unreadCount > 0 ? `${unreadCount} 个会话有最新内容` : "暂无最新内容",
    subline: parts.join(" · "),
  };
}
