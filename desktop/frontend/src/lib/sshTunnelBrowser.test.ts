import { describe, expect, it } from 'vitest';
import { browseTunnels, type TunnelRow } from './sshTunnelBrowser';

const row = (name: string, id: string, alias = 'Bastion'): TunnelRow => ({
  host: { id: alias, alias, host: '192.0.2.1', user: 'ops', auth_kind: 'password', group_path: ['Production'] },
  rule: { id, note: name, kind: 'local', bind_port: '6379', target_host: 'redis.internal', target_port: '6379' },
});

describe('tunnel browsing', () => {
  it('searches names, host metadata and both endpoints with combined words', () => {
    const rows = [row('Redis cache', 'one'), row('Database', 'two', 'Other')];
    expect(browseTunnels(rows, 'REDIS cache ops Production 127.0.0.1 6379', 'oldest')).toEqual([rows[0]]);
    expect(browseTunnels(rows, '192.0.2.1 redis.internal', 'oldest')).toHaveLength(2);
    expect(browseTunnels(rows, 'missing', 'oldest')).toEqual([]);
  });
  it('sorts names naturally without changing stored ordering', () => {
    const rows = [row('cache 10', 'one'), row('Cache 2', 'two')];
    expect(browseTunnels(rows, '', 'name-asc')).toEqual([rows[1], rows[0]]);
    expect(browseTunnels(rows, '', 'name-desc')).toEqual(rows);
    expect(rows[0].rule.note).toBe('cache 10');
  });
  it('orders addition times across hosts rather than host grouping', () => {
    const older = row('Z', '01ARZ3NDEKTSV4RRFFQ69G5FAV', 'A');
    const newer = row('A', '01BRZ3NDEKTSV4RRFFQ69G5FAV', 'B');
    expect(browseTunnels([newer, older], '', 'newest')).toEqual([newer, older]);
    expect(browseTunnels([newer, older], '', 'oldest')).toEqual([older, newer]);
  });
  it('keeps deterministic ordering for older non-ULID rules and ties', () => {
    const rows = [row('same', 'legacy-one'), row('same', 'legacy-two')];
    expect(browseTunnels(rows, '', 'oldest')).toEqual(rows);
    expect(browseTunnels(rows, '', 'newest')).toEqual([...rows].reverse());
    expect(browseTunnels(rows, '', 'name-asc')).toEqual(rows);
  });
});
