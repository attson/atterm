import type { MarkSessionsSeenOpts } from "./api";
import type { SessionInfo } from "./connection";

export class SeenReceiptOverlay {
  private readonly attentionBySession = new Map<string, number>();

  confirm(sessions: SessionInfo[], target: MarkSessionsSeenOpts): void {
    const ids = "all" in target ? null : new Set(target.ids);
    for (const session of sessions) {
      if (ids !== null && !ids.has(session.id)) continue;
      const attentionAt = session.attention_at ?? 0;
      const current = this.attentionBySession.get(session.id);
      if (current === undefined || attentionAt > current) {
        this.attentionBySession.set(session.id, attentionAt);
      }
    }
  }

  apply(sessions: SessionInfo[]): SessionInfo[] {
    let changed = false;
    const next = sessions.map((session) => {
      const seenAttentionAt = this.attentionBySession.get(session.id);
      if (seenAttentionAt === undefined) {
        return session;
      }
      if ((session.attention_at ?? 0) > seenAttentionAt) {
        this.attentionBySession.delete(session.id);
        return session;
      }
      if (session.unread !== true) return session;
      changed = true;
      return { ...session, unread: false };
    });
    return changed ? next : sessions;
  }
}
