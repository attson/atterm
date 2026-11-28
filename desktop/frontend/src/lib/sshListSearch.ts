import type { SSHHost, SSHKey } from "./api/ssh";

function normalizeQuery(raw: string): string {
  return raw.trim().toLocaleLowerCase();
}

export function matchesSSHHost(host: SSHHost, raw: string): boolean {
  const query = normalizeQuery(raw);
  if (!query) return true;
  return [host.alias, host.host, host.user, ...(host.tags ?? []), ...(host.group_path ?? [])]
    .filter(Boolean)
    .join("\n")
    .toLocaleLowerCase()
    .includes(query);
}

export function matchesSSHKey(key: SSHKey, raw: string): boolean {
  const query = normalizeQuery(raw);
  if (!query) return true;
  return `${key.name}\n${key.key_type ?? ""}`.toLocaleLowerCase().includes(query);
}
