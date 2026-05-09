package relay

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

const (
	clientReadLimit  = 1 * 1024 * 1024
	clientWriteWait  = 10 * time.Second
	clientPingPeriod = 25 * time.Second
)

// handleClient services a browser/SDK client. Until ATTACH arrives the only
// frames accepted are LIST. Once ATTACH locks onto a session, frames flow:
// agent -> session -> sub.Out() -> client (writer goroutine), and client ->
// IN/RESIZE -> session.SendInbound (reader loop).
func (s *Server) handleClient(ctx context.Context, c *websocket.Conn) {
	c.SetReadLimit(clientReadLimit)

	var (
		sess *session.Session
		sub  *session.Subscriber
	)
	defer func() {
		if sess != nil && sub != nil {
			sess.Unsubscribe(sub)
		}
	}()

	// Outgoing pump (started after ATTACH).
	writerCtx, cancelWriter := context.WithCancel(ctx)
	defer cancelWriter()

	startWriter := func() {
		go func() {
			ticker := time.NewTicker(clientPingPeriod)
			defer ticker.Stop()
			for {
				select {
				case <-writerCtx.Done():
					return
				case <-sub.Done():
					_ = c.Close(websocket.StatusGoingAway, "session ended")
					return
				case <-ticker.C:
					ctx, cancel := context.WithTimeout(writerCtx, clientWriteWait)
					err := c.Ping(ctx)
					cancel()
					if err != nil {
						return
					}
				case f := <-sub.Out():
					ctx, cancel := context.WithTimeout(writerCtx, clientWriteWait)
					err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(f))
					cancel()
					if err != nil {
						return
					}
				}
			}
		}()
	}

	for {
		f, err := readFrame(ctx, c)
		if err != nil {
			var ce websocket.CloseError
			if !errors.As(err, &ce) && !errors.Is(err, context.Canceled) {
				log.Printf("client: read: %v", err)
			}
			return
		}
		switch f.Type {
		case proto.TypeList:
			sessions := s.registry.List()
			infos := make([]proto.SessionInfo, 0, len(sessions))
			for _, ss := range sessions {
				infos = append(infos, ss.Info())
			}
			payload, _ := json.Marshal(infos)
			ctx, cancel := context.WithTimeout(ctx, clientWriteWait)
			err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
				Type:    proto.TypeListResp,
				Payload: payload,
			}))
			cancel()
			if err != nil {
				return
			}

		case proto.TypeAttach:
			if sess != nil {
				log.Printf("client: ATTACH after attach ignored")
				continue
			}
			var ap proto.AttachPayload
			if err := json.Unmarshal(f.Payload, &ap); err != nil {
				_ = c.Close(websocket.StatusPolicyViolation, "bad ATTACH")
				return
			}
			id, err := uuid.Parse(ap.SessionID)
			if err != nil {
				_ = c.Close(websocket.StatusPolicyViolation, "bad session id")
				return
			}
			target, ok := s.registry.Get(id)
			if !ok {
				// session not (yet) live — close politely
				_ = c.Close(websocket.StatusPolicyViolation, "no such session")
				return
			}
			sess = target
			sub, _ = sess.Subscribe(ap.SinceSeq)
			startWriter()

		case proto.TypeIn, proto.TypeResize:
			if sess == nil {
				continue
			}
			if !sess.SendInbound(f) {
				log.Printf("client: inbound full, dropping frame type 0x%02x", f.Type)
			}

		case proto.TypePong:
			// keepalive response

		default:
			log.Printf("client: unexpected frame type 0x%02x", f.Type)
		}
	}
}
