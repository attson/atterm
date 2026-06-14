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
// ownerUserID, when non-empty, restricts ATTACH to sessions owned by that user.
func (s *Server) handleClient(ctx context.Context, c *websocket.Conn, scope authScope, ownerUserID string) {
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
			var infos []proto.SessionInfo
			if ownerUserID != "" {
				infos = s.sessionInfoListForOwner(ownerUserID, s.seenForOwner(ctx, ownerUserID))
			} else {
				infos = s.sessionInfoList()
			}
			payload, _ := json.Marshal(infos)
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
			// Enforce owner isolation when resolver is wired in.
			if ownerUserID != "" && target.OwnerUserID != ownerUserID {
				s.debugf("client attach rejected session=%s reason=forbidden owner=%q principal=%q", id, target.OwnerUserID, ownerUserID)
				_ = c.Close(websocket.StatusCode(CloseCodeForbidden), CloseReasonForbidden)
				return
			}
			sess = target
			sub, _ = sess.Subscribe(ap.SinceSeq, ap.ClientID, ap.ClientName)
			s.debugf("client attached session=%s since_seq=%d client_id=%q client_name=%q", id, ap.SinceSeq, ap.ClientID, ap.ClientName)
			if ownerUserID != "" && s.cfg.Store != nil {
				// Attaching == viewing == read. Best-effort; a failed write
				// just leaves the item unread, which is safe.
				_ = s.cfg.Store.SetSeen(context.Background(), ownerUserID,
					[]string{sess.ID.String()}, time.Now().Unix())
			}
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
			if !sess.IsDriver(sub) {
				s.debugf("client drop frame=%s reason=not_driver session=%s", frameTypeName(f.Type), sess.ID)
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

		case proto.TypeClaimDriver:
			if sess == nil {
				s.debugf("client drop frame=CLAIM_DRIVER reason=not_attached")
				continue
			}
			if scope == authRead {
				s.debugf("client drop frame=CLAIM_DRIVER reason=read_only_scope session=%s", sess.ID)
				continue
			}
			if sessionRemotePermission(sess) == permView {
				s.debugf("client drop frame=CLAIM_DRIVER reason=view_only session=%s", sess.ID)
				continue
			}
			var cp proto.ClaimDriverPayload
			if err := json.Unmarshal(f.Payload, &cp); err != nil {
				s.debugf("client drop frame=CLAIM_DRIVER reason=bad_payload session=%s err=%q", sess.ID, err)
				continue
			}
			sess.ClaimDriver(sub, cp.ClientID, cp.ClientName)
			// On a mirror session the authoritative PTY driver lives upstream
			// (the host relay). Forward the claim up the uplink so the host's
			// ClaimLocalDriver runs and the new driver is broadcast back as
			// META — reconciling every layer and showing the previous driver
			// (e.g. the desktop owner) the viewer overlay. Local sessions own
			// the PTY directly, so the ClaimDriver above is sufficient there.
			if sess.DriverFromUpstream() {
				if !sess.SendInbound(f) {
					log.Printf("client: inbound full, dropping CLAIM_DRIVER session=%s", sess.ID)
				}
			}
			s.debugf("client claim_driver session=%s client_id=%q client_name=%q", sess.ID, cp.ClientID, cp.ClientName)

		case proto.TypePing:
			// Echo PING payload as PONG so the client can compute RTT
			// without trusting the server clock. nhooyr Conn.Write is
			// safe for concurrent use, so racing the writer goroutine
			// (which drains sub.Out()) is fine.
			pong := proto.Frame{Type: proto.TypePong, SessionID: f.SessionID, Payload: f.Payload}
			wctx, wcancel := context.WithTimeout(ctx, clientWriteWait)
			err := c.Write(wctx, websocket.MessageBinary, proto.Marshal(pong))
			wcancel()
			if err != nil {
				s.debugf("client pong_write_failed error=%q", err)
				return
			}

		case proto.TypePong:
			// keepalive response

		default:
			log.Printf("client: unexpected frame type 0x%02x", f.Type)
		}
	}
}
