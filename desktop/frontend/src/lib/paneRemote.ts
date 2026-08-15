/**
 * Decides the `remote` flag for a pane that is about to display an existing
 * session, and reports when the request had to be overridden.
 *
 * `remote` picks which endpoint the pane attaches through, so getting it wrong
 * points the pane at a relay that does not have the session and it renders
 * empty. Callers that open a session which is not currently on screen ask for
 * `remote: true`, which holds today because of an invariant nobody enforces:
 * every local session is placed into a pane in the same step that spawns it,
 * and every detach re-places it immediately, so a session that is nowhere on
 * screen is necessarily remote.
 *
 * This is the guard for the day that stops being true. The local session list
 * is authoritative about locality, so trust it over the request and say so.
 */
export function resolvePaneRemote(
  sessionId: string,
  localSessionIds: readonly string[],
  requested: boolean,
): { remote: boolean; corrected: boolean } {
  if (!requested) return { remote: false, corrected: false };
  const isLocal = localSessionIds.includes(sessionId);
  return { remote: !isLocal, corrected: isLocal };
}
