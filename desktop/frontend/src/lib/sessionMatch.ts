import type { RemoteSession } from "../platform/types";

// matchesSession returns true when q is empty (no filter) or when q is a
// case-insensitive substring of any of the session's user-visible identity
// fields (title / cwd / current_command). Fields are matched independently;
// a match in any one wins. q must already be trimmed and lowercased by the
// caller — this keeps the hot path (called per session per keystroke)
// branch-free of allocation. Empty / null / undefined fields never
// contribute.
export function matchesSession(s: RemoteSession, q: string): boolean {
  if (!q) return true;
  const t = s.title;
  if (t && t.toLocaleLowerCase().includes(q)) return true;
  const c = s.cwd;
  if (c && c.toLocaleLowerCase().includes(q)) return true;
  const cc = s.current_command;
  if (cc && cc.toLocaleLowerCase().includes(q)) return true;
  return false;
}
