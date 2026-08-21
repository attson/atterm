import { bindings } from "./_bindings";
import type { GitInfo } from "./_bindings";

export type { GitInfo } from "./_bindings";

// getGitInfo resolves branch + uncommitted line counts for each cwd. Non-repo
// cwds are omitted from the result; a nil Go slice arrives as null.
export async function getGitInfo(cwds: string[]): Promise<GitInfo[]> {
  return (await bindings().GetGitInfo(cwds)) ?? [];
}
