// Tag helpers for the SSH Hosts panel. Tags replaced the old single-group
// field: a host carries any number of them, and the panel's filter bar narrows
// the list to hosts carrying *all* of the selected tags.
//
// Comparison is case-insensitive throughout so "Prod" and "prod" are one tag,
// but the user's own capitalisation is what gets stored and displayed.

type TaggedHost = { tags?: string[] };

const fold = (t: string) => t.trim().toLowerCase();

// normalizeTags trims, drops blanks, and removes case-insensitive duplicates
// while keeping the first spelling and the entry order.
export function normalizeTags(tags: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const raw of tags) {
    const tag = raw.trim();
    if (tag === "") continue;
    const key = fold(tag);
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(tag);
  }
  return out;
}

// parseTagInput turns what the user typed into the tag field into tags.
// Comma is the only separator, so a tag may contain spaces ("hong kong").
export function parseTagInput(text: string): string[] {
  return normalizeTags(text.split(","));
}

// allHostTags is every tag in use, sorted, for the filter bar and the host
// form's suggestions.
export function allHostTags(hosts: TaggedHost[]): string[] {
  const merged = normalizeTags(hosts.flatMap((h) => h.tags ?? []));
  return merged.sort((a, b) => a.localeCompare(b));
}

// hostHasAllTags implements the filter bar's AND semantics. No selection
// matches everything.
export function hostHasAllTags(hostTags: string[] | undefined, selected: string[]): boolean {
  if (selected.length === 0) return true;
  const owned = new Set((hostTags ?? []).map(fold));
  return selected.every((t) => owned.has(fold(t)));
}
