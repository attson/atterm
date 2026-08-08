// Renderer-side logging.
//
// Everything the UI logs is batched and handed to the Go side, which writes it
// into the same desktop.log the backend uses. That matters most exactly when
// the app is broken: a user reporting "it won't start" hands over one file
// containing both the boot sequence and whatever the backend was doing at the
// time, instead of a log file that mysteriously stops before the interesting
// part and a devtools console nobody opened.
//
// This module is shared with the Capacitor/iOS build, which has no Wails
// bindings. There it silently stays console-only — never throws, never blocks.

import type { FrontendLogRecord } from "./api/_bindings";

export type LogLevel = "DEBUG" | "INFO" | "WARN" | "ERROR";

/**
 * Structured context appended to a message as `key=value` pairs.
 *
 * Primitives only, deliberately: accepting arbitrary objects invites
 * `logError("relay", "login failed", { req })` and with it a serialized
 * password. See AGENTS.md red line #21.
 */
export type LogFields = Record<string, string | number | boolean | null | undefined>;

/** Flush when this many records have queued up. */
const FLUSH_AT_RECORDS = 64;
/** ...or after this long, whichever comes first. */
const FLUSH_INTERVAL_MS = 1000;
/**
 * Hard ceiling on the queue. A render loop that logs on every frame must not
 * be able to grow this without bound while a flush is failing.
 */
const MAX_BUFFERED = 512;

/**
 * Field keys whose values are replaced with `***`. A blunt instrument on
 * purpose — the cost of over-redacting a log line is nothing, the cost of
 * writing an account_key to disk is the whole E2EE model.
 */
const SECRET_KEY_RE = /pass|token|key|secret|cred|auth/i;

let buffer: FrontendLogRecord[] = [];
let timer: ReturnType<typeof setTimeout> | null = null;
let droppedSinceLastFlush = 0;
let flushing = false;

/** Records held back after a failed flush, retried once on the next one. */
let retryQueue: FrontendLogRecord[] = [];

function bindingAvailable(): boolean {
  return typeof window !== "undefined" && typeof window.go?.main?.App?.AppendFrontendLogs === "function";
}

function formatFields(fields?: LogFields): string {
  if (!fields) return "";
  const parts: string[] = [];
  for (const [key, raw] of Object.entries(fields)) {
    if (raw === undefined) continue;
    const value = SECRET_KEY_RE.test(key) ? "***" : String(raw);
    parts.push(`${key}=${JSON.stringify(value)}`);
  }
  return parts.length ? " " + parts.join(" ") : "";
}

function consoleFor(level: LogLevel): (...args: unknown[]) => void {
  /* eslint-disable no-console */
  switch (level) {
    case "ERROR":
      return console.error;
    case "WARN":
      return console.warn;
    case "DEBUG":
      return console.debug;
    default:
      return console.info;
  }
  /* eslint-enable no-console */
}

function enqueue(level: LogLevel, tag: string, message: string) {
  buffer.push({
    // Stamped now, not at flush time, so batching cannot smear the timeline.
    timestamp_ms: Date.now(),
    level,
    tag,
    message,
  });

  if (buffer.length > MAX_BUFFERED) {
    droppedSinceLastFlush += buffer.length - MAX_BUFFERED;
    buffer = buffer.slice(-MAX_BUFFERED);
  }

  // Errors go out immediately: the next thing that happens after an error is
  // often the renderer dying, which would take the buffer with it.
  if (level === "ERROR" || buffer.length >= FLUSH_AT_RECORDS) {
    void flushLogs();
    return;
  }
  scheduleFlush();
}

function scheduleFlush() {
  if (timer !== null) return;
  timer = setTimeout(() => {
    timer = null;
    void flushLogs();
  }, FLUSH_INTERVAL_MS);
}

/**
 * Sends everything queued to the Go side. Safe to call at any time; resolves
 * even when there is nothing to do or no binding to send to.
 */
export async function flushLogs(): Promise<void> {
  if (timer !== null) {
    clearTimeout(timer);
    timer = null;
  }
  if (flushing) return;
  if (!bindingAvailable()) {
    // Console-only mode (Capacitor, browser, tests): drop the queue rather
    // than let it grow forever waiting for a binding that will never appear.
    buffer = [];
    retryQueue = [];
    droppedSinceLastFlush = 0;
    return;
  }

  const batch = retryQueue.concat(buffer);
  retryQueue = [];
  buffer = [];
  if (droppedSinceLastFlush > 0) {
    // Surface the loss instead of leaving a silent hole in the timeline.
    batch.push({
      timestamp_ms: Date.now(),
      level: "WARN",
      tag: "log",
      message: `dropped ${droppedSinceLastFlush} log record(s): renderer buffer overflow`,
    });
    droppedSinceLastFlush = 0;
  }
  if (batch.length === 0) return;

  flushing = true;
  try {
    await window.go!.main!.App!.AppendFrontendLogs!(batch);
  } catch {
    // One retry, then give up and count the loss. Retrying forever would turn
    // a broken bridge into an unbounded memory leak.
    if (retryQueue.length === 0) {
      retryQueue = batch.slice(-MAX_BUFFERED);
    } else {
      droppedSinceLastFlush += batch.length;
    }
  } finally {
    flushing = false;
  }
}

function log(level: LogLevel, tag: string, message: string, fields?: LogFields) {
  const full = message + formatFields(fields);
  // Dev keeps the console output so the devtools workflow is unchanged; a
  // production build writes to the file only.
  if (import.meta.env?.DEV || !bindingAvailable()) {
    consoleFor(level)(`[${tag}] ${full}`);
  }
  enqueue(level, tag, full);
}

/** Per-frame / per-keystroke detail. Not written unless the log level is DEBUG. */
export function logDebug(tag: string, message: string, fields?: LogFields): void {
  log("DEBUG", tag, message, fields);
}

/** Lifecycle and state transitions worth reading in a normal log. */
export function logInfo(tag: string, message: string, fields?: LogFields): void {
  log("INFO", tag, message, fields);
}

/** Failed but degraded or retried; the main flow continues. */
export function logWarn(tag: string, message: string, fields?: LogFields): void {
  log("WARN", tag, message, fields);
}

/** User-visible failure that nothing retries. */
export function logError(tag: string, message: string, fields?: LogFields): void {
  log("ERROR", tag, message, fields);
}

/** Renders an unknown caught value for a log message. */
export function errText(e: unknown): string {
  if (e instanceof Error) return `${e.name}: ${e.message}`;
  if (typeof e === "string") return e;
  try {
    return String(e);
  } catch {
    return "unknown error";
  }
}

/**
 * Installs the flush-on-teardown handlers. Called once from the app bootstrap;
 * without it the last second of logs is lost exactly when the window is going
 * away, which is when they matter.
 */
export function installLogFlushHandlers(): void {
  if (typeof window === "undefined") return;
  window.addEventListener("beforeunload", () => void flushLogs());
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") void flushLogs();
  });
}

/** Test-only: drops queued state so cases don't leak into each other. */
export function __resetLogBufferForTest(): void {
  if (timer !== null) {
    clearTimeout(timer);
    timer = null;
  }
  buffer = [];
  retryQueue = [];
  droppedSinceLastFlush = 0;
  flushing = false;
}
