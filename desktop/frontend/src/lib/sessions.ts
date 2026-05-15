import type { SessionInfo } from "./connection";

export type SessionGroup = {
  key: string;       // host_id, or "__unknown__"
  hostname: string;  // display name; "unknown host" if no entry has a non-empty host
  hostId: string;    // raw host_id; "" for the unknown group
  sessions: SessionInfo[];
};

const UNKNOWN_KEY = "__unknown__";
const UNKNOWN_HOSTNAME = "unknown host";

export function groupSessionsByHost(sessions: SessionInfo[]): SessionGroup[] {
  const buckets = new Map<string, SessionInfo[]>();
  for (const s of sessions) {
    const key = s.host_id ? s.host_id : UNKNOWN_KEY;
    let bucket = buckets.get(key);
    if (!bucket) {
      bucket = [];
      buckets.set(key, bucket);
    }
    bucket.push(s);
  }

  const groups: SessionGroup[] = [];
  for (const [key, bucket] of buckets) {
    let displayHost = "";
    let bestStartedAt = -Infinity;
    for (const s of bucket) {
      const h = s.host || "";
      // >= so that when started_at values tie, the later-arriving entry wins.
      if (h && s.started_at >= bestStartedAt) {
        displayHost = h;
        bestStartedAt = s.started_at;
      }
    }
    groups.push({
      key,
      hostname: displayHost || UNKNOWN_HOSTNAME,
      hostId: key === UNKNOWN_KEY ? "" : key,
      sessions: bucket,
    });
  }

  groups.sort((a, b) => {
    if (a.key === UNKNOWN_KEY) return 1;
    if (b.key === UNKNOWN_KEY) return -1;
    return a.hostname.localeCompare(b.hostname);
  });

  return groups;
}
