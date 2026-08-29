package relay

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"nhooyr.io/websocket"
	"time"

	"github.com/attson/atterm/internal/logging"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
)

const clientReadLimit = 17 * 1024 * 1024

var (
	// clientWriteWait bounds a single write (and the keepalive ping) before the
	// connection is torn down, so a peer that has stopped reading cannot pin the
	// writer goroutine forever.
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
	targetedOut := make(chan proto.Frame, 16)

	var (
		sess     *session.Session
		sub      *session.Subscriber
		clientID string
	)
	defer func() {
		if sess != nil && sub != nil {
			sess.Unsubscribe(sub)
		}
	}()
	// A client that disconnects mid-request must not leave a
	// TypeSessionCreate route registered forever — the TTL sweep would
	// eventually clear it, but there's no reason to wait: nothing will ever
	// read targetedOut again once this connection is gone.
	defer s.sessionCreateRoutes().unregisterClient(targetedOut)
	defer s.services.closeRequester(targetedOut)
	defer func() {
		routes := s.fsRoutes()
		if sess != nil && sess.DriverFromUpstream() {
			for _, watch := range routes.clientWatches(targetedOut) {
				if watch.sessionID != sess.ID {
					continue
				}
				request := proto.FSRequestPayload{
					RequestID: "cleanup-unwatch-" + uuid.NewString(),
					Op:        "unwatch_dir",
					WatchID:   watch.watchID,
				}
				payload, err := json.Marshal(request)
				if err != nil {
					continue
				}
				if !sess.SendInbound(proto.Frame{Type: proto.TypeFSRequest, SessionID: sess.ID, Payload: payload}) {
					s.debugf("client fs_cleanup_drop reason=inbound_full session=%s watch_id=%s", sess.ID, watch.watchID)
				}
			}
		}
		routes.unregisterClient(targetedOut)
	}()

	// Outgoing pump. Runs for the whole connection lifetime, not just after
	// ATTACH: a TypeSessionCreate reply must reach targetedOut before (and
	// possibly without ever) attaching to a session, so unlike the old
	// ATTACH-gated startWriter this cannot wait for ATTACH to start draining
	// targetedOut. subOut/subDone start as nil channels — a nil channel in a
	// select case simply never fires, which is what keeps the loop safe
	// before ATTACH — and become live the moment ATTACH sends the new
	// subscriber over attachSignal.
	writerCtx, cancelWriter := context.WithCancel(ctx)
	defer cancelWriter()
	targetedOverflow := func() {
		s.debugf("client targeted_fs_overflow")
		cancelWriter()
		_ = c.CloseNow()
	}
	attachSignal := make(chan *session.Subscriber, 1)

	go func() {
		var subOut <-chan proto.Frame
		var subDone <-chan struct{}
		ticker := time.NewTicker(clientPingPeriod)
		pacer := newReplayPacer(replayPaceBytes)
		defer ticker.Stop()
		for {
			select {
			case <-writerCtx.Done():
				return
			case newSub := <-attachSignal:
				// session.Subscribe never returns a nil *Subscriber today,
				// but this runs in a goroutine with no caller to recover a
				// panic: a nil here would take down the whole relay process
				// instead of just this one connection, so guard it anyway.
				if newSub == nil {
					continue
				}
				subOut = newSub.Out()
				subDone = newSub.Done()
			case <-subDone:
				// The subscription ended, which is not the same as the session
				// ending: it also covers the relay letting this client go.
				// Naming it "session ended" sent at least one debugging
				// session chasing a session that was alive the whole time.
				_ = c.Close(websocket.StatusGoingAway, "subscription ended")
				return
			case f := <-targetedOut:
				s.debugFrame("client", "send", f)
				ctx, cancel := context.WithTimeout(writerCtx, clientWriteWait)
				err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(f))
				cancel()
				if err != nil {
					// A write that cannot complete inside clientWriteWait
					// means the client stopped draining its socket — a
					// renderer whose main thread is stuck parsing a flood
					// looks exactly like this. Closing here is what the
					// user sees as "reconnecting", so say it at a level
					// they can actually find.
					logging.Info("relay-client", "closing session=%s reason=write-timeout frame=%s error=%v",
						f.SessionID, frameTypeName(f.Type), err)
					_ = c.CloseNow()
					return
				}
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
			case f := <-subOut:
				s.debugFrame("client", "send", f)
				ctx, cancel := context.WithTimeout(writerCtx, clientWriteWait)
				err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(f))
				cancel()
				if err != nil {
					// Same close as the targeted path above: the live
					// stream is where a client drowning in output actually
					// stalls, so this is the line that explains a
					// "reconnecting" badge on a purely local session.
					logging.Info("relay-client", "closing session=%s reason=write-timeout frame=%s error=%v",
						f.SessionID, frameTypeName(f.Type), err)
					_ = c.CloseNow()
					return
				}
				if pacer.observe(f) {
					timer := time.NewTimer(2 * time.Millisecond)
					select {
					case <-writerCtx.Done():
						timer.Stop()
						return
					case <-subDone:
						timer.Stop()
						_ = c.Close(websocket.StatusGoingAway, "subscription ended")
						return
					case <-timer.C:
					}
				}
			}
		}
	}()

	for {
		f, err := readFrame(ctx, c)
		if err != nil {
			var ce websocket.CloseError
			// Record how the connection ended. The client only reports
			// "reconnecting", and the close code is the one fact that says
			// whether the browser hung up, the peer went away, or a frame broke
			// a limit — without it every disconnect looks the same from both
			// ends.
			switch {
			case errors.As(err, &ce):
				logging.Info("relay-client", "closed session=%s code=%d reason=%q",
					attachedSessionID(sess), int(ce.Code), ce.Reason)
			case errors.Is(err, context.Canceled), errors.Is(err, net.ErrClosed):
				logging.Info("relay-client", "closed session=%s reason=local-teardown",
					attachedSessionID(sess))
			default:
				logging.Info("relay-client", "closed session=%s reason=read-error error=%v",
					attachedSessionID(sess), err)
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
				logging.Warn("relay-client", "ATTACH after attach ignored")
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
			clientID = ap.ClientID
			sub, _ = sess.Subscribe(ap.SinceSeq, ap.ClientID, ap.ClientName)
			s.debugf("client attached session=%s since_seq=%d client_id=%q client_name=%q", id, ap.SinceSeq, ap.ClientID, ap.ClientName)
			if ownerUserID != "" && s.cfg.Store != nil {
				// Attaching == viewing == read. Best-effort; a failed write
				// just leaves the item unread, which is safe.
				_ = s.cfg.Store.SetSeen(context.Background(), ownerUserID,
					[]string{sess.ID.String()}, time.Now().Unix())
			}
			attachSignal <- sub

		case proto.TypeIn, proto.TypeResize, proto.TypePasteImage, proto.TypePasteFile:
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
				logging.Warn("relay-client", "inbound full, dropping frame type 0x%02x", f.Type)
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
					logging.Warn("relay-client", "inbound full, dropping CLAIM_DRIVER session=%s", sess.ID)
				}
			}
			s.debugf("client claim_driver session=%s client_id=%q client_name=%q", sess.ID, cp.ClientID, cp.ClientName)

		case proto.TypeSessionCreate:
			// No ATTACH required: this asks a desktop to fork a brand new
			// session, so there is nothing to attach to yet. sess may be nil.
			var req proto.SessionCreatePayload
			if err := json.Unmarshal(f.Payload, &req); err != nil || req.RequestID == "" || req.HostID == "" || req.ProfileID == "" {
				s.sendSessionCreateError(targetedOut, targetedOverflow, req.RequestID, "invalid_request")
				continue
			}
			// authRead is dormant today (every authenticated principal is
			// authWrite — see auth.go), but forking a PTY and running a
			// profile's startup_cmd is the most privileged thing a read-only
			// token could do if that scope is ever revived. Gate it now,
			// same as CLAIM_DRIVER above and open_external below, rather
			// than leaving it to be noticed later.
			if scope == authRead {
				s.debugf("client session_create_denied reason=read_only_scope")
				s.sendSessionCreateError(targetedOut, targetedOverflow, req.RequestID, "permission_denied")
				continue
			}
			routes := s.sessionCreateRoutes()
			// One outstanding request per client at a time (design §4: "a
			// phone that taps twice does not fork two shells"). Client
			// identity — targetedOut — only exists here at the relay; see
			// hasOutstandingRequest's doc comment for why this cannot live
			// on the desktop uplink instead. Checked before the host lookup
			// so a client already waiting on one host can't queue a second
			// request against a different host either.
			if routes.hasOutstandingRequest(targetedOut) {
				s.debugf("client session_create_denied reason=request_in_flight request_id=%s", req.RequestID)
				s.sendSessionCreateError(targetedOut, targetedOverflow, req.RequestID, "request_in_flight")
				continue
			}
			// lookupHost is scoped to ownerUserID itself, so a miss already
			// means "no such host_id for this owner" — whether nobody ever
			// announced it or it belongs to someone else is not a
			// distinction this lookup can make (or needs to): both answer
			// unknown_host_id, which is what keeps host_id enumeration
			// across owners closed.
			host, ok := routes.lookupHost(req.HostID, ownerUserID)
			if !ok {
				s.debugf("client session_create_denied host_id=%q reason=unknown_host", req.HostID)
				s.sendSessionCreateError(targetedOut, targetedOverflow, req.RequestID, "unknown_host_id")
				continue
			}
			if !routes.registerRequestRoute(req.RequestID, targetedOut, host.out, targetedOverflow) {
				s.sendSessionCreateError(targetedOut, targetedOverflow, req.RequestID, "duplicate_request_id")
				continue
			}
			if !sendFSFrame(host.out, f) {
				routes.unregisterRequest(req.RequestID, targetedOut)
				s.debugf("client session_create_drop reason=uplink_unavailable host_id=%q request_id=%s", req.HostID, req.RequestID)
				s.sendSessionCreateError(targetedOut, targetedOverflow, req.RequestID, "upstream_unavailable")
			}

		case proto.TypeFSRequest:
			if sess == nil {
				s.debugf("client drop frame=FS_REQUEST reason=not_attached")
				continue
			}
			var request proto.FSRequestPayload
			// Segment 0 carries op / request_id in the clear precisely so
			// this gate keeps working without the relay holding a key.
			if err := proto.DecodeFSHead(f.Payload, &request); err != nil || request.RequestID == "" || request.Op == "" || f.SessionID != sess.ID {
				s.sendFSClientError(targetedOut, targetedOverflow, sess.ID, request.RequestID, "invalid_request")
				continue
			}
			if request.Op != "open_external" && !isReadOnlyFSOperation(request.Op) {
				s.sendFSClientError(targetedOut, targetedOverflow, sess.ID, request.RequestID, "invalid_request")
				continue
			}
			if sess.Info().RemotePermission != proto.RemotePermissionFull {
				s.sendFSClientError(targetedOut, targetedOverflow, sess.ID, request.RequestID, "permission_denied")
				continue
			}
			if !sess.DriverFromUpstream() {
				s.sendFSClientError(targetedOut, targetedOverflow, sess.ID, request.RequestID, "upstream_unavailable")
				continue
			}
			if request.Op == "open_external" {
				if scope != authWrite {
					s.sendFSClientError(targetedOut, targetedOverflow, sess.ID, request.RequestID, "permission_denied")
					continue
				}
				if !sess.IsDriver(sub) {
					s.sendFSClientError(targetedOut, targetedOverflow, sess.ID, request.RequestID, "driver_required")
					continue
				}
			}

			routes := s.fsRoutes()
			if request.Op == "unwatch_dir" {
				if request.WatchID == "" {
					s.sendFSClientError(targetedOut, targetedOverflow, sess.ID, request.RequestID, "invalid_request")
					continue
				}
				if !routes.clientOwnsWatch(sess.ID, request.WatchID, targetedOut) {
					s.sendFSClientError(targetedOut, targetedOverflow, sess.ID, request.RequestID, "permission_denied")
					continue
				}
			}
			request.ClientID = clientID
			payload, err := json.Marshal(request)
			if err != nil {
				s.sendFSClientError(targetedOut, targetedOverflow, sess.ID, request.RequestID, "invalid_request")
				continue
			}
			f.Payload = payload
			if !routes.registerRequestRoute(sess.ID, request, targetedOut, targetedOverflow) {
				s.sendFSClientError(targetedOut, targetedOverflow, sess.ID, request.RequestID, "duplicate_request_id")
				continue
			}
			if !sess.SendInbound(f) {
				routes.unregisterRequest(sess.ID, request.RequestID, targetedOut)
				s.debugf("client fs_request_drop reason=inbound_full session=%s request_id=%s", sess.ID, request.RequestID)
				s.sendFSClientError(targetedOut, targetedOverflow, sess.ID, request.RequestID, "upstream_unavailable")
			}

		case proto.TypeServiceOpen:
			if sess == nil || f.SessionID != sess.ID {
				s.sendServiceClientError(targetedOut, targetedOverflow, f.SessionID, "", "", "invalid_request")
				continue
			}
			var req proto.ServiceOpenPayload
			if err := json.Unmarshal(f.Payload, &req); err != nil || req.RequestID == "" || req.ServiceID == "" || len(req.Sealed) == 0 {
				s.sendServiceClientError(targetedOut, targetedOverflow, sess.ID, req.RequestID, req.ServiceID, "invalid_request")
				continue
			}
			if scope != authWrite || sess.Info().RemotePermission != proto.RemotePermissionFull {
				s.sendServiceClientError(targetedOut, targetedOverflow, sess.ID, req.RequestID, req.ServiceID, "permission_denied")
				continue
			}
			if !sess.IsDriver(sub) {
				s.sendServiceClientError(targetedOut, targetedOverflow, sess.ID, req.RequestID, req.ServiceID, "driver_required")
				continue
			}
			if !sess.DriverFromUpstream() {
				s.sendServiceClientError(targetedOut, targetedOverflow, sess.ID, req.RequestID, req.ServiceID, "upstream_unavailable")
				continue
			}
			forwarded, message, ok := s.services.begin(ownerUserID, sess.ID, req, targetedOut, targetedOverflow)
			if !ok {
				s.sendServiceClientError(targetedOut, targetedOverflow, sess.ID, req.RequestID, req.ServiceID, message)
				continue
			}
			payload, err := json.Marshal(forwarded)
			if err != nil {
				if id, parseErr := uuid.Parse(req.ServiceID); parseErr == nil {
					s.services.closeByRequester(id, targetedOut)
				}
				s.sendServiceClientError(targetedOut, targetedOverflow, sess.ID, req.RequestID, req.ServiceID, "invalid_request")
				continue
			}
			f.Payload = payload
			if !sess.SendInbound(f) {
				if id, parseErr := uuid.Parse(req.ServiceID); parseErr == nil {
					s.services.closeByRequester(id, targetedOut)
				}
				s.sendServiceClientError(targetedOut, targetedOverflow, sess.ID, req.RequestID, req.ServiceID, "upstream_unavailable")
			}

		case proto.TypeServiceClose:
			if sess == nil || f.SessionID != sess.ID {
				continue
			}
			var req proto.ServiceClosePayload
			if err := json.Unmarshal(f.Payload, &req); err != nil {
				continue
			}
			serviceID, err := uuid.Parse(req.ServiceID)
			if err != nil {
				continue
			}
			s.services.closeByRequester(serviceID, targetedOut)

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
			logging.Warn("relay-client", "unexpected frame type 0x%02x", f.Type)
		}
	}
}

func isReadOnlyFSOperation(op string) bool {
	switch op {
	case "list_dir", "file_meta", "read_file", "read_chunk", "watch_dir", "unwatch_dir":
		return true
	default:
		return false
	}
}

func (s *Server) sendFSClientError(out chan<- proto.Frame, onOverflow func(), sessionID uuid.UUID, requestID, message string) {
	// Relay-generated errors are single-segment plaintext: the relay has
	// no key, and these messages carry no agent-side path.
	payload, err := proto.EncodeFSHead(proto.FSResponsePayload{
		RequestID: requestID,
		Error:     message,
	})
	if err != nil {
		return
	}
	if !sendFSFrameToRoute(fsClientRoute{out: out, onOverflow: onOverflow}, proto.Frame{Type: proto.TypeFSResponse, SessionID: sessionID, Payload: payload}) {
		s.debugf("client fs_error_drop session=%s request_id=%s", sessionID, requestID)
	}
}

func (s *Server) sendServiceClientError(out chan<- proto.Frame, onOverflow func(), sessionID uuid.UUID, requestID, serviceID, message string) {
	payload, err := json.Marshal(proto.ServiceOpenedPayload{
		RequestID: requestID,
		ServiceID: serviceID,
		Error:     message,
	})
	if err != nil {
		return
	}
	if !sendFSFrame(out, proto.Frame{Type: proto.TypeServiceOpened, SessionID: sessionID, Payload: payload}) && onOverflow != nil {
		onOverflow()
	}
}

// sendSessionCreateError answers a rejected/failed TypeSessionCreate
// directly, without going through the requests table: these are relay-side
// refusals the request never got registered for (or, for duplicate_request_id,
// registration deliberately failed), so there is nothing to route later.
func (s *Server) sendSessionCreateError(out chan<- proto.Frame, onOverflow func(), requestID, message string) {
	payload, err := json.Marshal(proto.SessionCreatedPayload{
		RequestID: requestID,
		Error:     message,
	})
	if err != nil {
		return
	}
	if !sendFSFrame(out, proto.Frame{Type: proto.TypeSessionCreated, Payload: payload}) {
		if onOverflow != nil {
			onOverflow()
		}
		s.debugf("client session_create_error_drop request_id=%s message=%s", requestID, message)
	}
}

// attachedSessionID renders the session a connection was attached to, or a
// placeholder while it is still pre-ATTACH.
func attachedSessionID(sess *session.Session) string {
	if sess == nil {
		return "none"
	}
	return sess.ID.String()
}
