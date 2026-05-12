package relay

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

const clientReadLimit = 17 * 1024 * 1024

var (
	clientWriteWait  = 10 * time.Second
	clientPingPeriod = 25 * time.Second
)

// handleClient services a browser/SDK client. Until ATTACH arrives the only
// frames accepted are LIST. Once ATTACH locks onto a session, frames flow:
// agent -> session -> sub.Out() -> client (writer goroutine), and client ->
// IN/RESIZE -> session.SendInbound (reader loop).
func (s *Server) handleClient(ctx context.Context, c *websocket.Conn, scope authScope) {
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
			pacer := newReplayPacer(replayPaceBytes)
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
						s.debugf("client ping_failed error=%q", err)
						// If only the writer side notices a dead browser, tear
						// down the websocket so the reader loop unblocks and the
						// subscriber is removed. Otherwise mirror sessions can stay
						// stuck above zero subscribers and never issue another
						// STREAM_REQUEST.
						_ = c.CloseNow()
						return
					}
				case f := <-sub.Out():
					s.debugFrame("client", "send", f)
					ctx, cancel := context.WithTimeout(writerCtx, clientWriteWait)
					err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(f))
					cancel()
					if err != nil {
						s.debugf("client write_failed frame=%s session=%s error=%q", frameTypeName(f.Type), f.SessionID, err)
						_ = c.CloseNow()
						return
					}
					if pacer.observe(f) {
						timer := time.NewTimer(2 * time.Millisecond)
						select {
						case <-writerCtx.Done():
							timer.Stop()
							return
						case <-sub.Done():
							timer.Stop()
							_ = c.Close(websocket.StatusGoingAway, "session ended")
							return
						case <-timer.C:
						}
					}
				}
			}
		}()
	}

	for {
		f, err := readFrame(ctx, c)
		if err != nil {
			var ce websocket.CloseError
			if !errors.As(err, &ce) && !errors.Is(err, context.Canceled) && !errors.Is(err, net.ErrClosed) {
				log.Printf("client: read: %v", err)
			}
			return
		}
		s.debugFrame("client", "recv", f)
		switch f.Type {
		case proto.TypeList:
			payload, _ := json.Marshal(s.sessionInfoList())
			resp := proto.Frame{Type: proto.TypeListResp, Payload: payload}
			s.debugFrame("client", "send", resp)
			ctx, cancel := context.WithTimeout(ctx, clientWriteWait)
			err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(resp))
			cancel()
			if err != nil {
				s.debugf("client write_failed frame=LIST_RESP error=%q", err)
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
			s.debugf("client attached session=%s since_seq=%d", id, ap.SinceSeq)
			startWriter()

		case proto.TypeIn, proto.TypeResize, proto.TypePasteImage:
			if sess == nil {
				s.debugf("client drop frame=%s reason=not_attached", frameTypeName(f.Type))
				continue
			}
			if !frameAllowedByPermission(scope, sessionRemotePermission(sess), f.Type) {
				s.debugf("client drop frame=%s reason=permission", frameTypeName(f.Type))
				continue
			}
			if f.Type == proto.TypeResize {
				if cols, rows, err := proto.DecodeResize(f.Payload); err == nil {
					sess.UpdateSize(cols, rows)
					s.registry.NotifyChange()
				}
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
