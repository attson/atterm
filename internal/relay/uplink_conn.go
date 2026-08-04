package relay

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/webpush"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

const (
	uplinkReadLimit       = 17 * 1024 * 1024
	uplinkWriteWait       = 10 * time.Second
	uplinkPingPeriod      = 25 * time.Second
	uplinkOutBuffer       = 256
	uplinkInboundFwdDepth = 64
	defaultWebPushIdle    = 10 * time.Minute
)

// mirrorState is one mirror session this uplink owns. Mirror sessions live
// in the relay's main registry but are populated by the uplink's frames
// rather than by an /agent connection.
type mirrorState struct {
	sess      *session.Session
	streaming bool // true once StreamRequest has been sent (first subscriber)

	idleCancel                 context.CancelFunc
	idleNotifiedCommandStarted int64
	waitingNotifiedKey         int64
}

// newMirrorSession builds a mirror Session: driver state is adopted from the
// upstream host relay's META (driverFromUpstream), never self-assigned to a
// local subscriber. OwnerUserID is stamped for the registry's owner check.
func newMirrorSession(id uuid.UUID, info proto.SessionInfo, ownerUserID string) *session.Session {
	sess := session.New(id, info)
	sess.OwnerUserID = ownerUserID
	sess.SetDriverFromUpstream(true)
	return sess
}

// uplinkSession is the per-connection state for one handleUplink invocation.
// Previously ten separate closures inside handleUplink all captured (mu,
// mirrors, out, ctx, cancel, conn, server, ownerUserID); collapsing them
// onto a struct + methods lets the compiler enforce field access, keeps
// each helper's signature explicit, and puts the shared-state ownership
// documentation on one type instead of scattered in closure comments.
//
// Lifetime: one uplinkSession per WebSocket connection. cleanup() drains
// mirrors on defer at the end of handleUplink; the writer goroutine holds
// no session state directly.
type uplinkSession struct {
	server      *Server
	conn        *websocket.Conn
	ownerUserID string
	// ctx cancels on writer teardown (kill -9 / TCP drop / ping timeout) so
	// the reader unblocks and cleanup() runs. Every mirror-lifecycle
	// goroutine also selects on ctx.Done() to bail cleanly.
	ctx    context.Context
	cancel context.CancelFunc
	// out is the writer's ordered queue of frames to send back to the
	// desktop uplink. Buffered so short bursts don't block callers; the
	// writer goroutine drains it in FIFO order.
	out chan proto.Frame

	// mu guards mirrors + every field on mirrorState (streaming,
	// idleCancel, idleNotifiedCommandStarted, waitingNotifiedKey).
	mu      sync.Mutex
	mirrors map[uuid.UUID]*mirrorState
}

// handleUplink services a desktop app's "control" connection. The first frame
// must be ANNOUNCE; subsequent ANNOUNCEs are full-snapshot reconciliations.
// OUT/META/CLOSE frames flow uplink→relay; STREAM_REQUEST/STOP and
// IN/RESIZE/PASTE_IMAGE flow relay→uplink.
// ownerUserID is the authenticated user's ID (empty for legacy/dev-mode paths).
func (s *Server) handleUplink(ctx context.Context, c *websocket.Conn, ownerUserID string) {
	c.SetReadLimit(uplinkReadLimit)

	// Send AUTH_INFO so the desktop client knows which user this connection
	// is authenticated as. Only sent on the resolver (user-account) path;
	// the legacy shared-token path leaves ownerUserID empty and gets no frame.
	if ownerUserID != "" {
		type authInfoPayload struct {
			UserID string `json:"user_id"`
		}
		authPayload, _ := json.Marshal(authInfoPayload{UserID: ownerUserID})
		wctx, wc := context.WithTimeout(ctx, uplinkWriteWait)
		err := c.Write(wctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
			Type:    proto.TypeAuthInfo,
			Payload: authPayload,
		}))
		wc()
		if err != nil {
			log.Printf("relay: uplink send AUTH_INFO failed: %v", err)
			return
		}
		s.debugf("uplink auth_info_sent user_id=%s", ownerUserID)
	}

	first, err := readFrame(ctx, c)
	if err != nil {
		log.Printf("uplink: read ANNOUNCE: %v", err)
		_ = c.Close(websocket.StatusPolicyViolation, "expected ANNOUNCE")
		return
	}
	if first.Type != proto.TypeAnnounce {
		_ = c.Close(websocket.StatusPolicyViolation, "first frame must be ANNOUNCE")
		return
	}
	s.debugFrame("uplink", "recv", first)
	var ann proto.AnnouncePayload
	if err := json.Unmarshal(first.Payload, &ann); err != nil {
		_ = c.Close(websocket.StatusPolicyViolation, "bad ANNOUNCE payload")
		return
	}

	connCtx, cancelConn := context.WithCancel(ctx)
	defer cancelConn()

	u := &uplinkSession{
		server:      s,
		conn:        c,
		ownerUserID: ownerUserID,
		ctx:         connCtx,
		cancel:      cancelConn,
		out:         make(chan proto.Frame, uplinkOutBuffer),
		mirrors:     make(map[uuid.UUID]*mirrorState),
	}
	defer u.cleanup()

	log.Printf("uplink: host %s connected (%d session(s))", ann.HostID, len(ann.Sessions))
	u.reconcile(ann.Sessions)

	go u.writeLoop()
	u.readLoop()
}

// enqueue submits f for the writer goroutine to send. Non-blocking on
// connection teardown so callers don't hang waiting to write into a dead
// channel.
func (u *uplinkSession) enqueue(f proto.Frame) {
	select {
	case u.out <- f:
	case <-u.ctx.Done():
	}
}

// startStream asks the desktop to begin sending OUT bytes for a mirror
// session. Called from the lifecycle hook when the first subscriber
// arrives. This is intentionally gated on subscribers to save bandwidth
// (no remote viewer => no need to stream OUT). The inbound forwarder is
// NOT wired here — it runs unconditionally (see startInboundForwarder)
// so relay-injected IN frames (e.g. Feishu card buttons) reach the PTY
// even when no viewer is subscribed.
func (u *uplinkSession) startStream(id uuid.UUID) {
	u.mu.Lock()
	ms, ok := u.mirrors[id]
	if !ok || ms.streaming {
		u.mu.Unlock()
		return
	}
	ms.streaming = true
	u.mu.Unlock()

	payload, _ := json.Marshal(proto.StreamRequestPayload{SessionID: id.String()})
	frame := proto.Frame{Type: proto.TypeStreamRequest, SessionID: id, Payload: payload}
	u.server.debugFrame("uplink", "enqueue", frame)
	u.enqueue(frame)
}

func (u *uplinkSession) stopStream(id uuid.UUID) {
	u.mu.Lock()
	ms, ok := u.mirrors[id]
	if !ok || !ms.streaming {
		u.mu.Unlock()
		return
	}
	ms.streaming = false
	u.mu.Unlock()
	payload, _ := json.Marshal(proto.StreamStopPayload{SessionID: id.String()})
	frame := proto.Frame{Type: proto.TypeStreamStop, SessionID: id, Payload: payload}
	u.server.debugFrame("uplink", "enqueue", frame)
	u.enqueue(frame)
}

// startInboundForwarder runs a resident goroutine that drains a mirror
// session's inbound channel (IN/RESIZE frames pushed by web clients OR
// injected by the relay itself, e.g. Feishu card callbacks via
// SendInbound) and pushes them up the WS so the desktop routes them to
// the local PTY. Unlike OUT streaming, this is NOT gated on remote
// subscribers: a frame injected with no viewer present must still reach
// the PTY. The goroutine exits when the connection is torn down
// (ctx done) or the session's inbound channel closes.
func (u *uplinkSession) startInboundForwarder(sess *session.Session) {
	go func() {
		inbound := sess.Inbound()
		for {
			select {
			case <-u.ctx.Done():
				return
			case f, ok := <-inbound:
				if !ok {
					return
				}
				u.server.debugFrame("uplink", "enqueue", f)
				select {
				case u.out <- f:
				case <-u.ctx.Done():
					return
				}
			}
		}
	}()
}

func (u *uplinkSession) notifySession(ms *mirrorState, info proto.SessionInfo, notificationType string, idleForSeconds int) {
	if ms == nil {
		return
	}
	if ms.sess.SubscriberCount() > 0 { // watching == read == no push
		return
	}
	hostID, _ := uuid.Parse(info.HostID) // host id is informational in push payloads
	if u.server.cfg.WebPush != nil {
		u.server.cfg.WebPush.DispatchSessionNotification(ms.sess.OwnerUserID, webpush.SessionNotification{
			SessionID:        ms.sess.ID,
			HostID:           hostID,
			NotificationType: notificationType,
			Label:            webpush.SessionLabel(info),
			RemotePermission: info.RemotePermission,
			IdleForSeconds:   idleForSeconds,
		})
	}
}

func (u *uplinkSession) cancelIdleTimer(ms *mirrorState) {
	if ms != nil && ms.idleCancel != nil {
		ms.idleCancel()
		ms.idleCancel = nil
	}
}

func (u *uplinkSession) scheduleIdleTimer(id uuid.UUID, ms *mirrorState, info proto.SessionInfo) {
	idleTimeout := u.server.webPushIdleTimeout()
	if u.server.cfg.WebPush == nil || idleTimeout <= 0 || ms == nil || info.TaskState != proto.TaskStateRunning {
		u.cancelIdleTimer(ms)
		return
	}
	idleKey := webpush.TaskNotificationKey(info)
	if ms.idleNotifiedCommandStarted == idleKey {
		u.cancelIdleTimer(ms)
		return
	}
	u.cancelIdleTimer(ms)
	timerCtx, cancel := context.WithCancel(u.ctx)
	ms.idleCancel = cancel

	wait := idleTimeout
	if info.LastOutputAt > 0 {
		if remaining := idleTimeout - time.Since(time.Unix(info.LastOutputAt, 0)); remaining > 0 {
			wait = remaining
		} else {
			wait = 0
		}
	}
	lastOutputAt := info.LastOutputAt
	go func() {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timerCtx.Done():
			return
		case <-timer.C:
		}

		u.mu.Lock()
		current := u.mirrors[id]
		if current != ms {
			u.mu.Unlock()
			return
		}
		currentInfo := ms.sess.Info()
		currentIdleKey := webpush.TaskNotificationKey(currentInfo)
		if currentInfo.TaskState != proto.TaskStateRunning ||
			currentInfo.LastOutputAt > lastOutputAt ||
			ms.idleNotifiedCommandStarted == currentIdleKey {
			u.mu.Unlock()
			return
		}
		ms.idleCancel = nil
		ms.idleNotifiedCommandStarted = currentIdleKey
		u.mu.Unlock()

		u.notifySession(ms, currentInfo, webpush.NotificationIdleTimeout, int(idleTimeout.Seconds()))
	}()
}

func (u *uplinkSession) handleTaskNotifications(id uuid.UUID, ms *mirrorState) {
	if ms == nil {
		return
	}
	info := ms.sess.Info()
	if info.TaskState == proto.TaskStateWaitingInput {
		key := webpush.TaskNotificationKey(info)
		u.mu.Lock()
		shouldNotify := ms.waitingNotifiedKey != key
		if shouldNotify {
			ms.waitingNotifiedKey = key
		}
		u.mu.Unlock()
		if shouldNotify {
			u.notifySession(ms, info, webpush.NotificationWaitingInput, 0)
		}
	}
	u.mu.Lock()
	u.scheduleIdleTimer(id, ms, info)
	u.mu.Unlock()
}

// reconcile applies a fresh ANNOUNCE: add new sessions, remove vanished
// ones. Updates metadata for existing sessions.
func (u *uplinkSession) reconcile(sessions []proto.SessionInfo) {
	seen := make(map[uuid.UUID]struct{}, len(sessions))
	for _, info := range sessions {
		id, err := uuid.Parse(info.ID)
		if err != nil {
			continue
		}
		seen[id] = struct{}{}

		u.mu.Lock()
		existing, ok := u.mirrors[id]
		u.mu.Unlock()
		if ok {
			// ANNOUNCE carries no driver_client_id; reconcile advertised
			// facts without adopting an empty driver and clobbering the
			// active driver (every client would flip to viewer).
			existing.sess.UpdateAdvertisedInfo(info)
			u.server.registry.NotifyChange()
			u.handleTaskNotifications(id, existing)
			u.server.debugf("uplink mirror_update session=%s cwd=%q title=%q", id, info.Cwd, info.Title)
			continue
		}

		sess := newMirrorSession(id, info, u.ownerUserID)
		// snapshot capture: id is per-iteration, must capture by value
		sid := id
		sess.SetSubscriberLifecycle(
			func() { u.startStream(sid) },
			func() { u.stopStream(sid) },
		)
		// Report the mirror's remote subscriber count down the uplink so
		// the desktop owner can render a "N watching" badge. enqueue is
		// non-blocking (drops if the downlink is saturated).
		sess.SetSubscriberCountHook(func(n int) {
			payload, _ := json.Marshal(proto.ViewersPayload{SessionID: sid.String(), Count: n})
			u.enqueue(proto.Frame{Type: proto.TypeViewers, SessionID: sid, Payload: payload})
		})
		sess.SetMetaChangedHook(u.server.registry.NotifyChange)
		if _, err := u.server.registry.Add(sess); err != nil {
			// Owner mismatch: another user already holds this session ID.
			// Close the WS with a well-known code so the desktop can display
			// a localized error. Do not modify the existing session.
			u.server.debugf("uplink mirror_add_rejected session=%s reason=owner_mismatch", id)
			_ = u.conn.Close(websocket.StatusCode(CloseCodeSessionIDOwnerMismatch), CloseReasonSessionIDOwnerMismatch)
			return
		}
		u.mu.Lock()
		ms := &mirrorState{sess: sess}
		u.mirrors[id] = ms
		u.mu.Unlock()
		// Resident inbound forwarder: must run regardless of remote
		// subscribers so relay-injected IN frames reach the PTY.
		u.startInboundForwarder(sess)
		u.handleTaskNotifications(id, ms)
		u.server.debugf("uplink mirror_add session=%s command=%q host_id=%q host=%q user=%q", id, info.Command, info.HostID, info.Host, info.User)
	}
	// remove sessions no longer in the manifest
	u.mu.Lock()
	var gone []uuid.UUID
	for id := range u.mirrors {
		if _, ok := seen[id]; !ok {
			gone = append(gone, id)
		}
	}
	u.mu.Unlock()
	for _, id := range gone {
		u.mu.Lock()
		ms := u.mirrors[id]
		delete(u.mirrors, id)
		u.mu.Unlock()
		u.cancelIdleTimer(ms)
		u.server.removeSession(id)
		u.server.debugf("uplink mirror_remove session=%s reason=missing_from_announce", id)
	}
}

func (u *uplinkSession) cleanup() {
	u.mu.Lock()
	gone := make(map[uuid.UUID]*mirrorState, len(u.mirrors))
	for id, ms := range u.mirrors {
		gone[id] = ms
	}
	u.mirrors = make(map[uuid.UUID]*mirrorState)
	u.mu.Unlock()
	for id, ms := range gone {
		u.server.removeSession(id)
		if ms != nil {
			u.cancelIdleTimer(ms)
			u.notifySession(ms, ms.sess.Info(), webpush.NotificationUplinkDisconnected, 0)
		}
		u.server.debugf("uplink mirror_remove session=%s reason=connection_cleanup", id)
	}
}

// writeLoop drains u.out onto the WebSocket, pinging on idle. Exits on
// write error, ping failure, or ctx cancellation. Cancels ctx on exit so
// the reader unblocks and the deferred cleanup() runs.
func (u *uplinkSession) writeLoop() {
	ticker := time.NewTicker(uplinkPingPeriod)
	defer ticker.Stop()
	// When the writer exits — either ping timeout (peer is unreachable)
	// or write error (TCP gone) — tear down the conn so the reader
	// unblocks and the deferred cleanup() runs. Without this, kill -9 /
	// network drop / machine sleep on the desktop side leaves orphan
	// mirror sessions in the registry until OS-level TCP keepalive
	// finally errors the read (potentially many minutes).
	defer u.cancel()
	for {
		select {
		case <-u.ctx.Done():
			return
		case <-ticker.C:
			wctx, wc := context.WithTimeout(u.ctx, uplinkWriteWait)
			err := u.conn.Ping(wctx)
			wc()
			if err != nil {
				log.Printf("uplink: ping failed (%v), closing", err)
				return
			}
		case f := <-u.out:
			u.server.debugFrame("uplink", "send", f)
			wctx, wc := context.WithTimeout(u.ctx, uplinkWriteWait)
			err := u.conn.Write(wctx, websocket.MessageBinary, proto.Marshal(f))
			wc()
			if err != nil {
				u.server.debugf("uplink write_failed frame=%s session=%s error=%q", frameTypeName(f.Type), f.SessionID, err)
				return
			}
		}
	}
}

// readLoop is the receive side: drain frames off the WebSocket and route
// them to the mirror sessions. Returns when the WS closes or the reader
// errors; the deferred cleanup() then drops all mirror sessions.
func (u *uplinkSession) readLoop() {
	for {
		f, err := readFrame(u.ctx, u.conn)
		if err != nil {
			var ce websocket.CloseError
			if !errors.As(err, &ce) && !errors.Is(err, context.Canceled) {
				log.Printf("uplink: read: %v", err)
			}
			return
		}
		u.server.debugFrame("uplink", "recv", f)
		switch f.Type {
		case proto.TypeAnnounce:
			var p proto.AnnouncePayload
			if err := json.Unmarshal(f.Payload, &p); err == nil {
				u.reconcile(p.Sessions)
			}
		case proto.TypeOut:
			seq, data, err := proto.DecodeOut(f.Payload)
			if err != nil {
				continue
			}
			u.mu.Lock()
			ms := u.mirrors[f.SessionID]
			u.mu.Unlock()
			if ms != nil {
				if looksLikeEncryptedOut(data) {
					ms.sess.MarkContentOpaque()
				}
				if ms.sess.PushOut(seq, data) {
					u.server.registry.NotifyChange()
					u.handleTaskNotifications(f.SessionID, ms)
				}
			}
		case proto.TypeMeta:
			var m proto.MetaPayload
			if err := json.Unmarshal(f.Payload, &m); err != nil {
				continue
			}
			u.mu.Lock()
			ms := u.mirrors[f.SessionID]
			u.mu.Unlock()
			if ms != nil {
				// Mirror sessions adopt the upstream's driver_client_id from
				// this META (SetDriverFromUpstream), so remote /client
				// subscribers see the real PTY driver (the host desktop) and
				// render the viewer overlay. UpdateMeta is the single broadcast
				// point; the raw upstream frame is not re-broadcast separately.
				ms.sess.UpdateMeta(m)
				u.server.registry.NotifyChange()
				u.handleTaskNotifications(f.SessionID, ms)
			}
		case proto.TypeClose:
			u.mu.Lock()
			ms := u.mirrors[f.SessionID]
			delete(u.mirrors, f.SessionID)
			u.mu.Unlock()
			if ms != nil {
				ms.sess.Broadcast(f)
				u.cancelIdleTimer(ms)
				u.server.removeSession(f.SessionID)
			}
		case proto.TypeCommandEvent:
			u.handleCommandEvent(f)
		case proto.TypeFSResponse:
			u.mu.Lock()
			ms := u.mirrors[f.SessionID]
			u.mu.Unlock()
			if ms == nil {
				u.server.debugf("uplink fs_response_drop reason=unknown_session session=%s", f.SessionID)
				continue
			}
			u.server.fsRoutes().routeResponse(f)
		case proto.TypeFSEvent:
			u.mu.Lock()
			ms := u.mirrors[f.SessionID]
			u.mu.Unlock()
			if ms == nil {
				u.server.debugf("uplink fs_event_drop reason=unknown_session session=%s", f.SessionID)
				continue
			}
			u.server.fsRoutes().routeEvent(f)
		case proto.TypePing:
			// Echo the payload back as PONG. Clients use this to measure
			// application-level RTT (their PING carries an 8B timestamp;
			// the same payload comes back unchanged). Empty payloads are
			// also echoed unchanged so old clients still get a liveness
			// signal.
			pong := proto.Frame{Type: proto.TypePong, Payload: f.Payload}
			select {
			case u.out <- pong:
			case <-u.ctx.Done():
				return
			default:
				u.server.debugf("uplink pong_drop reason=out_full")
			}
		case proto.TypePong:
			// keepalive
		default:
			log.Printf("uplink: unexpected frame type 0x%02x", f.Type)
		}
	}
}

// handleCommandEvent processes a TypeCommandEvent frame received from
// the uplink. The frame must reference a session id currently in the
// uplink's manifest (mirrors map) — this prevents one uplink from
// forging "command finished" events for another uplink's sessions.
// host_id is pulled from the session's info, not the payload, for the
// same reason.
func (u *uplinkSession) handleCommandEvent(f proto.Frame) {
	if u.server.cfg.WebPush == nil {
		return
	}
	payload, err := proto.DecodeCommandEvent(f)
	if err != nil {
		u.server.debugf("uplink command_event decode_failed session=%s error=%q", f.SessionID, err)
		return
	}
	u.mu.Lock()
	ms, ok := u.mirrors[f.SessionID]
	u.mu.Unlock()
	if !ok || ms == nil {
		u.server.debugf("uplink command_event unknown_session session=%s", f.SessionID)
		return
	}
	info := ms.sess.Info()
	hostIDStr := info.HostID
	hostID, _ := uuid.Parse(hostIDStr) // ignore parse error — hostID is informational
	if ms.sess.SubscriberCount() == 0 {
		u.server.cfg.WebPush.DispatchCommandFinished(ms.sess.OwnerUserID, webpush.CommandFinished{
			SessionID:        f.SessionID,
			HostID:           hostID,
			ExitCode:         payload.ExitCode,
			ElapsedMS:        payload.ElapsedMS,
			Label:            payload.Label,
			SealedBody:       payload.SealedBody,
			RemotePermission: info.RemotePermission,
		})
	}
}

func (s *Server) webPushIdleTimeout() time.Duration {
	if s.cfg.WebPushIdleTimeout < 0 {
		return 0
	}
	if s.cfg.WebPushIdleTimeout == 0 {
		return defaultWebPushIdle
	}
	return s.cfg.WebPushIdleTimeout
}

// looksLikeEncryptedOut is a thin alias for e2eecrypto.LooksLikeSealed
// used on the relay's inbound OUT path. Kept as a package-local name so
// the call sites in the read loop read the same way they always did.
var looksLikeEncryptedOut = e2eecrypto.LooksLikeSealed
