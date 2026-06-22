export type LogLevel = "DEBUG" | "INFO" | "WARN" | "ERROR";

export type ParsedLogLine =
  | { kind: "structured"; ts: string; level: LogLevel; tag: string; msg: string }
  | { kind: "raw"; text: string };

export const LEVEL_ORDER: Record<LogLevel, number> = {
  DEBUG: 0,
  INFO: 1,
  WARN: 2,
  ERROR: 3,
};

const LINE_RE =
  /^(\d{4}\/\d{2}\/\d{2} \d{2}:\d{2}:\d{2}\.\d{3}) (DEBUG|INFO|WARN|ERROR)\s+\[([^\]]+)\] (.*)$/;

export function parseLogLine(line: string): ParsedLogLine {
  const m = LINE_RE.exec(line);
  if (!m) return { kind: "raw", text: line };
  return {
    kind: "structured",
    ts: m[1],
    level: m[2] as LogLevel,
    tag: m[3],
    msg: m[4],
  };
}

export function levelAtLeast(level: LogLevel, min: LogLevel): boolean {
  return LEVEL_ORDER[level] >= LEVEL_ORDER[min];
}

// Options for the log-level threshold filter, shared by the log viewers.
// Shaped for SelectDropdown.vue ({ value, label }).
export const LEVEL_FILTER_OPTIONS: { value: LogLevel; label: string }[] = [
  { value: "DEBUG", label: "DEBUG+" },
  { value: "INFO", label: "INFO+" },
  { value: "WARN", label: "WARN+" },
  { value: "ERROR", label: "ERROR" },
];
