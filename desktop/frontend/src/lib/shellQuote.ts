// posixShellQuote wraps a string in single quotes so the shell reads it as a
// single literal argument. Embedded single quotes are closed, escaped, and
// re-opened using the standard '\'' idiom. Empty input becomes '' so it still
// occupies one argv slot.
export function posixShellQuote(s: string): string {
  return "'" + s.replace(/'/g, "'\\''") + "'";
}
