import { bindings } from "./_bindings";

export type { SnippetHostResult, SnippetHostState, SnippetRunProgress } from "./_bindings";

// runSnippetOnHosts starts a batch run of the quick template `snippetId` on
// every host in `hostIds` and returns the run id (results only ever arrive
// through the "snippet:run:progress" event, keyed on that id + each
// result's host_id — there is no separate "list results" call).
export function runSnippetOnHosts(snippetId: string, hostIds: string[]): Promise<string> {
  return bindings().RunSnippetOnHosts(snippetId, hostIds);
}

// cancelSnippetRun stops a run in progress: every host still queued or
// dialling/running moves to "error"; hosts that already reached a terminal
// state are left untouched.
export function cancelSnippetRun(runId: string): Promise<void> {
  return bindings().CancelSnippetRun(runId);
}
