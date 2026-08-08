export type LogLevel = "DEBUG" | "INFO" | "WARN" | "ERROR";

export type ParsedLogLine =
  | { kind: "structured"; ts: string; level: LogLevel; tag: string; msg: string }
  | { kind: "raw"; text: string };

const LEVEL_ORDER: Record<LogLevel, number> = {
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

// Options for the *write* threshold — which records reach the file at all.
// Deliberately labelled without the "+" suffix used above: the filter picks
// what to show from an existing file, this picks what gets written in the
// first place, and conflating the two makes for a confusing bug report.
export const LEVEL_WRITE_OPTIONS: { value: LogLevel; label: string }[] = [
  { value: "DEBUG", label: "DEBUG" },
  { value: "INFO", label: "INFO" },
  { value: "WARN", label: "WARN" },
  { value: "ERROR", label: "ERROR" },
];

export function isLogLevel(value: string): value is LogLevel {
  return value in LEVEL_ORDER;
}

// ---- tag filtering ------------------------------------------------------
//
// Tags come in families — feishu / feishu-anchor / feishu-form / feishu-hook,
// relay-client / relay-agent / relay-uplink, every renderer tag under ui-.
// Filtering by one exact tag is too narrow when you're chasing "why didn't the
// Feishu card update", so the option list offers a `<prefix>*` entry alongside
// the exact tags whenever a family has more than one member.

/** Matches every tag. */
export const TAG_FILTER_ALL = "";

const FAMILY_SUFFIX = "*";

/**
 * tagMatches reports whether a line's tag passes the filter.
 * `""` matches everything, `"feishu*"` matches feishu and feishu-anchor,
 * anything else is an exact match.
 */
export function tagMatches(tag: string, filter: string): boolean {
  if (!filter) return true;
  if (filter.endsWith(FAMILY_SUFFIX)) {
    const prefix = filter.slice(0, -FAMILY_SUFFIX.length);
    return tag === prefix || tag.startsWith(prefix + "-");
  }
  return tag === filter;
}

/**
 * logTagOptions derives the filter choices from the log text itself, so the
 * list can never drift from the tags the code actually emits — no hardcoded
 * vocabulary to keep in sync.
 *
 * Counts are included because "which subsystem is flooding this file" is a
 * question you usually have before you know which tag to pick.
 */
export function logTagOptions(
  content: string,
  allLabel: string,
): { value: string; label: string }[] {
  const counts = new Map<string, number>();
  for (const line of content.split("\n")) {
    const parsed = parseLogLine(line);
    if (parsed.kind !== "structured") continue;
    counts.set(parsed.tag, (counts.get(parsed.tag) ?? 0) + 1);
  }

  const total = Array.from(counts.values()).reduce((a, b) => a + b, 0);

  // A family is a prefix shared by 2+ distinct tags. `feishu` counts as a
  // member of the `feishu` family even though it has no dash.
  const familyMembers = new Map<string, string[]>();
  for (const tag of counts.keys()) {
    const prefix = tag.split("-")[0];
    familyMembers.set(prefix, [...(familyMembers.get(prefix) ?? []), tag]);
  }

  const families = Array.from(familyMembers.entries())
    .filter(([, members]) => members.length > 1)
    .map(([prefix, members]) => ({
      value: prefix + FAMILY_SUFFIX,
      label: `${prefix}${FAMILY_SUFFIX} (${members.reduce(
        (n, t) => n + (counts.get(t) ?? 0),
        0,
      )})`,
    }))
    .sort((a, b) => a.value.localeCompare(b.value));

  const exact = Array.from(counts.entries())
    .map(([tag, n]) => ({ value: tag, label: `${tag} (${n})` }))
    .sort((a, b) => a.value.localeCompare(b.value));

  return [
    { value: TAG_FILTER_ALL, label: `${allLabel} (${total})` },
    ...families,
    ...exact,
  ];
}
