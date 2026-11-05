export function systemTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
}

export function isValidTimeZone(timeZone: string): boolean {
  if (!timeZone || timeZone === "Local") return false;
  try {
    new Intl.DateTimeFormat("en", { timeZone }).format(0);
    return true;
  } catch {
    return false;
  }
}

export function dateInTimeZone(value: string | number | Date, timeZone: string): string {
  const date = value instanceof Date ? value : new Date(value);
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(date);
  const part = (type: Intl.DateTimeFormatPartTypes) => parts.find((item) => item.type === type)?.value ?? "";
  return `${part("year")}-${part("month")}-${part("day")}`;
}

export function addCalendarDays(day: string, amount: number): string {
  const date = new Date(`${day}T12:00:00Z`);
  date.setUTCDate(date.getUTCDate() + amount);
  return date.toISOString().slice(0, 10);
}

/** Converts an IANA-zone wall clock to an absolute instant without relying on
 * the host zone. Two passes handle offset changes around DST boundaries. */
export function zonedDateTimeToUtc(day: string, hour: number, minute: number, timeZone: string): number {
  const [year, month, date] = day.split("-").map(Number);
  const wallClock = Date.UTC(year, month - 1, date, hour, minute);
  let candidate = wallClock;
  const formatter = new Intl.DateTimeFormat("en-US", {
    timeZone,
    year: "numeric", month: "2-digit", day: "2-digit",
    hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23",
  });
  for (let pass = 0; pass < 3; pass++) {
    const parts = formatter.formatToParts(candidate);
    const value = (type: Intl.DateTimeFormatPartTypes) => Number(parts.find((item) => item.type === type)?.value ?? 0);
    const represented = Date.UTC(value("year"), value("month") - 1, value("day"), value("hour"), value("minute"), value("second"));
    const next = candidate + (wallClock - represented);
    if (next === candidate) break;
    candidate = next;
  }
  return candidate;
}

function parsedTimestamp(value: string | number): number | null {
  const timestamp = typeof value === "number" ? value : Date.parse(value);
  return Number.isFinite(timestamp) ? timestamp : null;
}

export function formatLocalTime(
  value: string | number,
  locale: string,
  timeZone = systemTimeZone(),
): string {
  const timestamp = parsedTimestamp(value);
  if (timestamp === null) return "";
  return new Intl.DateTimeFormat(locale, {
    timeZone,
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  }).format(timestamp);
}

export function formatLocalDateTime(
  value: string | number,
  locale: string,
  timeZone = systemTimeZone(),
): string {
  const timestamp = parsedTimestamp(value);
  if (timestamp === null) return "";
  return new Intl.DateTimeFormat(locale, {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  }).format(timestamp);
}

export function formatLocalTimeRange(
  start: string | number,
  end: string | number,
  locale: string,
  timeZone = systemTimeZone(),
): string {
  const startTimestamp = parsedTimestamp(start);
  const endTimestamp = parsedTimestamp(end);
  if (startTimestamp === null || endTimestamp === null) return "";

  const dateFormatter = new Intl.DateTimeFormat("en-CA", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  });
  const startLabel = formatLocalDateTime(startTimestamp, locale, timeZone);
  const endLabel = dateFormatter.format(startTimestamp) === dateFormatter.format(endTimestamp)
    ? formatLocalTime(endTimestamp, locale, timeZone)
    : formatLocalDateTime(endTimestamp, locale, timeZone);
  return `${startLabel}-${endLabel}`;
}
