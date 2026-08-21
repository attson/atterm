// useGitInfo — polls git branch + uncommitted line counts for the sidebar's
// local-session cwds. The fetcher is injected (App.vue passes lib/api
// getGitInfo) so web/mobile builds — which have no Wails binding — pass
// enabled: false and never poll, and tests pass a fake.
//
// One batched call per tick, deduped by cwd on both ends. A failed poll
// keeps the previous map: a transient error should not strip git lines from
// every row for one cycle.

import { onScopeDispose, ref, watch, type Ref } from "vue";
import type { GitInfo } from "../lib/api/git";

export interface UseGitInfo {
  byCwd: Ref<ReadonlyMap<string, GitInfo>>;
  refresh: () => Promise<void>;
}

export function useGitInfo(
  cwds: Ref<readonly string[]>,
  fetcher: (cwds: string[]) => Promise<GitInfo[]>,
  opts: { enabled?: boolean; intervalMs?: number } = {},
): UseGitInfo {
  const byCwd = ref<ReadonlyMap<string, GitInfo>>(new Map());
  const refresh = async () => {
    const unique = [...new Set(cwds.value.filter((c) => c !== ""))];
    if (unique.length === 0) {
      if (byCwd.value.size > 0) byCwd.value = new Map();
      return;
    }
    try {
      const list = await fetcher(unique);
      byCwd.value = new Map(list.map((g) => [g.cwd, g]));
    } catch {
      // keep the previous map
    }
  };

  if (opts.enabled === false) return { byCwd, refresh: async () => {} };

  // Immediate fetch on mount and whenever the cwd SET changes (a new session
  // or a cd should not wait out the poll interval). Key on the sorted set so
  // list reordering alone does not refetch.
  watch(
    () => [...new Set(cwds.value)].sort().join("\n"),
    () => void refresh(),
    { immediate: true },
  );
  const timer = window.setInterval(() => void refresh(), opts.intervalMs ?? 10_000);
  onScopeDispose(() => window.clearInterval(timer));
  return { byCwd, refresh };
}
