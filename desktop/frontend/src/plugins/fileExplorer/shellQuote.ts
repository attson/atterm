// Characters that are safe unquoted in a POSIX shell command line. Anything
// outside this set (spaces, $, quotes, glob chars, ~, etc.) forces quoting so
// the path reaches the shell as a single literal argument.
const SAFE = /^[A-Za-z0-9_./@:+,%=-]+$/;

/**
 * Quote a filesystem path for safe insertion into a POSIX shell command line.
 * Safe paths pass through unchanged; anything else is wrapped in single quotes
 * with embedded single quotes escaped as '\'' (close, escaped quote, reopen).
 */
export function quoteForShell(path: string): string {
  if (path === "") return "''";
  if (SAFE.test(path)) return path;
  return "'" + path.replace(/'/g, "'\\''") + "'";
}
