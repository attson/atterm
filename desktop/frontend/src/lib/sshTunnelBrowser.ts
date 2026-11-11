import type { ForwardRule, SSHHost } from './api';

export type TunnelRow = { host: SSHHost; rule: ForwardRule };
export type TunnelSort = 'newest' | 'oldest' | 'name-asc' | 'name-desc';

// Saved rules use ULIDs. Their timestamp preserves addition order across hosts
// without making a schema change or confusing it with the source app's dates.
function addedAt(id: string): number {
  if (!/^[0-7][0-9A-HJKMNP-TV-Z]{25}$/i.test(id)) return 0;
  const alphabet = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';
  return [...id.slice(0, 10).toUpperCase()].reduce((n, c) => n * 32 + alphabet.indexOf(c), 0);
}

export function browseTunnels(rows: TunnelRow[], query: string, sort: TunnelSort): TunnelRow[] {
  const words = query.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
  return rows.map((row, index) => ({ row, index, time: addedAt(row.rule.id) }))
    .filter(({ row: { host, rule } }) => {
      const text = [rule.note, rule.kind, rule.bind_addr || '127.0.0.1', rule.bind_port,
        rule.target_host, rule.target_port, host.alias, host.user, host.host,
        ...(host.tags ?? []), ...(host.group_path ?? [])].join(' ').toLocaleLowerCase();
      return words.every((word) => text.includes(word));
    })
    .sort((a, b) => {
      if (sort === 'name-asc' || sort === 'name-desc') {
        const order = (a.row.rule.note ?? '').localeCompare(b.row.rule.note ?? '', undefined, { numeric: true, sensitivity: 'base' });
        return (sort === 'name-desc' ? -order : order) || a.index - b.index;
      }
      const order = a.time - b.time || a.index - b.index;
      return sort === 'newest' ? -order : order;
    })
    .map(({ row }) => row);
}
