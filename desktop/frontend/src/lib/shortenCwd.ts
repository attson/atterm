/**
 * Shorten a cwd for inline display in a tight row.
 *
 * Strategy:
 *  - empty / undefined → "" (callers can v-if it away)
 *  - if cwd begins with the user's $HOME, replace that prefix with `~`
 *  - if the result has more than 2 path segments (under either `/` or
 *    `~/`), collapse to `…/last/two`
 *  - otherwise return the substituted path verbatim
 *
 * The full path is always available via the row's `title` attribute, so
 * the truncation is non-destructive.
 */
export function shortenCwd(cwd: string | undefined, home: string): string {
  if (!cwd) return "";
  if (!home) return cwd;
  let s = cwd;
  if (s === home) return "~";
  if (s.startsWith(home + "/")) {
    s = "~" + s.slice(home.length);
  }
  const tildePrefixed = s.startsWith("~/");
  const body = tildePrefixed ? s.slice(2) : s.replace(/^\//, "");
  const parts = body.split("/").filter(Boolean);
  if (parts.length <= 2) {
    return s;
  }
  return "…/" + parts.slice(-2).join("/");
}
