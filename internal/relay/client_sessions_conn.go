package relay

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/attson/atterm/internal/proto"
	"nhooyr.io/websocket"
)

const (
	clientSessionsWriteWait  = 10 * time.Second
	clientSessionsPingPeriod = 25 * time.Second
)

// handleClientSessions streams full session-list snapshots. The first snapshot
// is sent immediately; later snapshots are pushed only after registry/meta
// changes, replacing the desktop frontend's old /api/sessions polling loop.
// ownerUserID, when non-empty, filters sessions to only those owned by that user.
func (s *Server) handleClientSessions(ctx context.Context, c *websocket.Conn, ownerUserID string) {
	sub := s.registry.SubscribeChanges()
	defer sub.Close()

	if !s.writeSessionList(ctx, c, ownerUserID) {
		return
	}

	ticker := time.NewTicker(clientSessionsPingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pctx, cancel := context.WithTimeout(ctx, clientSessionsWriteWait)
			err := c.Ping(pctx)
			cancel()
			if err != nil {
				s.debugf("client-sessions ping_failed error=%q", err)
				return
			}
		case <-sub.C():
			if !s.writeSessionList(ctx, c, ownerUserID) {
				return
			}
		}
	}
}

func (s *Server) writeSessionList(ctx context.Context, c *websocket.Conn, ownerUserID string) bool {
	var infos []proto.SessionInfo
	if ownerUserID != "" {
		infos = s.sessionInfoListForOwner(ownerUserID)
	} else {
		infos = s.sessionInfoList()
	}
	payload, err := json.Marshal(infos)
	if err != nil {
		s.debugf("client-sessions marshal_failed error=%q", err)
		return false
	}
	frame := proto.Frame{Type: proto.TypeListResp, Payload: payload}
	s.debugFrame("client-sessions", "send", frame)
	wctx, cancel := context.WithTimeout(ctx, clientSessionsWriteWait)
	err = c.Write(wctx, websocket.MessageBinary, proto.Marshal(frame))
	cancel()
	if err != nil {
		s.debugf("client-sessions write_failed error=%q", err)
		return false
	}
	return true
}

func (s *Server) sessionInfoList() []proto.SessionInfo {
	sessions := s.registry.List()
	infos := make([]proto.SessionInfo, 0, len(sessions))
	for _, ss := range sessions {
		infos = append(infos, ss.Info())
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	return infos
}

// sessionInfoListForOwner returns session infos filtered to those owned by ownerUserID.
func (s *Server) sessionInfoListForOwner(ownerUserID string) []proto.SessionInfo {
	sessions := s.registry.List()
	infos := make([]proto.SessionInfo, 0)
	for _, ss := range sessions {
		if ss.OwnerUserID == ownerUserID {
			infos = append(infos, ss.Info())
		}
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	return infos
}
