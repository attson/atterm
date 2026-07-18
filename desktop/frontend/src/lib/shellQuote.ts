// SAFE covers characters that never need escaping for a POSIX shell to read
// them as a single literal argument. Anything outside this set — space, tab,
// `'`, `"`, backslash, and every metacharacter (`$&|;<>()[]{}*?~!#`) — forces
// single-quote wrapping.
const SAFE = /^[A-Za-z0-9_@%+=:,./-]+$/;

// posixShellQuote returns a shell-safe token: the input verbatim when it only
// uses characters shells never expand or split on, and a single-quoted form
// otherwise. Embedded single quotes are escaped as '\''. Empty input becomes
// '' so it still occupies one argv slot.
export function posixShellQuote(s: string): string {
  if (s === "") return "''";
  if (SAFE.test(s)) return s;
  return "'" + s.replace(/'/g, "'\\''") + "'";
}
