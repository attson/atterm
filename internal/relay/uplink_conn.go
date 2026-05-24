package relay

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/webpush"
	"github.com/attson/atterm/internal/webhook"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)


const (
	uplinkReadLimit       = 17 * 1024 * 1024
	uplinkWriteWait       = 10 * time.Second
	uplinkPingPeriod      = 25 * time.Second
	uplinkOutBuffer       = 256
	uplinkInboundFwdDepth = 64
)

// mirrorState is one mirror session this uplink owns. Mirror sessions live
// in the relay's main registry but are populated by the uplink's frames
// rather than by an /agent connection.
type mirrorState struct {
	sess      *session.Session
	fwdCancel context.CancelFunc // running inbound forwarder, nil when not streaming
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

	uplinkOut := make(chan proto.Frame, uplinkOutBuffer)
	var (
		mu      sync.Mutex
		mirrors = make(map[uuid.UUID]*mirrorState)
	)

	enqueue := func(f proto.Frame) {
		select {
		case uplinkOut <- f:
		case <-connCtx.Done():
		}
	}

	// startStream wires up an inbound forwarder for a mirror session. Called
	// from the lifecycle hook when the first subscriber arrives.
	startStream := func(id uuid.UUID) {
		mu.Lock()
		ms, ok := mirrors[id]
		if !ok || ms.fwdCancel != nil {
			mu.Unlock()
			return
		}
		fwdCtx, cancelFwd := context.WithCancel(connCtx)
		ms.fwdCancel = cancelFwd
		sess := ms.sess
		mu.Unlock()

		// notify uplink to start sending bytes
		payload, _ := json.Marshal(proto.StreamRequestPayload{SessionID: id.String()})
		frame := proto.Frame{Type: proto.TypeStreamRequest, SessionID: id, Payload: payload}
		s.debugFrame("uplink", "enqueue", frame)
		enqueue(frame)

		// drain the mirror session's inbound (IN/RESIZE coming from web clients)
		// and push them up the WS to the uplink, so the desktop can route them
		// to the local PTY.
		go func() {
			inbound := sess.Inbound()
			for {
				select {
				case <-fwdCtx.Done():
					return
				case f, ok := <-inbound:
					if !ok {
						return
					}
					s.debugFrame("uplink", "enqueue", f)
					select {
					case uplinkOut <- f:
					case <-fwdCtx.Done():
						return
					}
				}
			}
		}()
	}

	stopStream := func(id uuid.UUID) {
		mu.Lock()
		ms, ok := mirrors[id]
		if !ok || ms.fwdCancel == nil {
			mu.Unlock()
			return
		}
		ms.fwdCancel()
		ms.fwdCancel = nil
		mu.Unlock()
		payload, _ := json.Marshal(proto.StreamStopPayload{SessionID: id.String()})
		frame := proto.Frame{Type: proto.TypeStreamStop, SessionID: id, Payload: payload}
		s.debugFrame("uplink", "enqueue", frame)
		enqueue(frame)
	}

	// reconcile applies a fresh ANNOUNCE: add new sessions, remove vanished
	// ones. Updates metadata for existing sessions.
	reconcile := func(sessions []proto.SessionInfo) {
		seen := make(map[uuid.UUID]struct{}, len(sessions))
		for _, info := range sessions {
			id, err := uuid.Parse(info.ID)
			if err != nil {
				continue
			}
			seen[id] = struct{}{}

			mu.Lock()
			existing, ok := mirrors[id]
			mu.Unlock()
			if ok {
				// ANNOUNCE carries no driver_client_id; use UpdateCwdTitle so a
				// driverFromUpstream mirror doesn't adopt an empty driver and
				// clobber the active driver (every client would flip to viewer).
				existing.sess.UpdateCwdTitle(info.Cwd, info.Title)
				existing.sess.UpdateRemotePermission(info.RemotePermission)
				s.registry.NotifyChange()
				s.debugf("uplink mirror_update session=%s cwd=%q title=%q", id, info.Cwd, info.Title)
				continue
			}

			sess := newMirrorSession(id, info, ownerUserID)
			// snapshot capture: id is per-iteration, must capture by value
			sid := id
			sess.SetSubscriberLifecycle(
				func() { startStream(sid) },
				func() { stopStream(sid) },
			)
			// Report the mirror's remote subscriber count down the uplink so
			// the desktop owner can render a "N watching" badge. enqueue is
			// non-blocking (drops if the downlink is saturated).
			sess.SetSubscriberCountHook(func(n int) {
				payload, _ := json.Marshal(proto.ViewersPayload{SessionID: sid.String(), Count: n})
				enqueue(proto.Frame{Type: proto.TypeViewers, SessionID: sid, Payload: payload})
			})
			if _, err := s.registry.Add(sess); err != nil {
				// Owner mismatch: another user already holds this session ID.
				// Close the WS with a well-known code so the desktop can display
				// a localized error. Do not modify the existing session.
				s.debugf("uplink mirror_add_rejected session=%s reason=owner_mismatch", id)
				_ = c.Close(websocket.StatusCode(CloseCodeSessionIDOwnerMismatch), CloseReasonSessionIDOwnerMismatch)
				return
			}
			mu.Lock()
			mirrors[id] = &mirrorState{sess: sess}
			mu.Unlock()
			s.debugf("uplink mirror_add session=%s command=%q host_id=%q host=%q user=%q", id, info.Command, info.HostID, info.Host, info.User)
		}
		// remove sessions no longer in the manifest
		mu.Lock()
		var gone []uuid.UUID
		for id := range mirrors {
			if _, ok := seen[id]; !ok {
				gone = append(gone, id)
			}
		}
		mu.Unlock()
		for _, id := range gone {
			mu.Lock()
			ms := mirrors[id]
			delete(mirrors, id)
			mu.Unlock()
			if ms != nil && ms.fwdCancel != nil {
				ms.fwdCancel()
			}
			s.registry.Remove(id)
			s.debugf("uplink mirror_remove session=%s reason=missing_from_announce", id)
		}
	}

	cleanup := func() {
		mu.Lock()
		ids := make([]uuid.UUID, 0, len(mirrors))
		for id := range mirrors {
			ids = append(ids, id)
		}
		mirrors = make(map[uuid.UUID]*mirrorState)
		mu.Unlock()
		for _, id := range ids {
			s.registry.Remove(id)
			s.debugf("uplink mirror_remove session=%s reason=connection_cleanup", id)
		}
	}
	defer cleanup()

	log.Printf("uplink: host %s connected (%d session(s))", ann.HostID, len(ann.Sessions))
	reconcile(ann.Sessions)

	// writer
	go func() {
		ticker := time.NewTicker(uplinkPingPeriod)
		defer ticker.Stop()
		// When the writer exits — either ping timeout (peer is unreachable)
		// or write error (TCP gone) — tear down the conn so the reader
		// unblocks and the deferred cleanup() runs. Without this, kill -9 /
		// network drop / machine sleep on the desktop side leaves orphan
		// mirror sessions in the registry until OS-level TCP keepalive
		// finally errors the read (potentially many minutes).
		defer cancelConn()
		for {
			select {
			case <-connCtx.Done():
				return
			case <-ticker.C:
				wctx, wc := context.WithTimeout(connCtx, uplinkWriteWait)
				err := c.Ping(wctx)
				wc()
				if err != nil {
					log.Printf("uplink: ping failed (%v), closing", err)
					return
				}
			case f := <-uplinkOut:
				s.debugFrame("uplink", "send", f)
				wctx, wc := context.WithTimeout(connCtx, uplinkWriteWait)
				err := c.Write(wctx, websocket.MessageBinary, proto.Marshal(f))
				wc()
				if err != nil {
					s.debugf("uplink write_failed frame=%s session=%s error=%q", frameTypeName(f.Type), f.SessionID, err)
					return
				}
			}
		}
	}()

	// reader
	for {
		f, err := readFrame(connCtx, c)
		if err != nil {
			var ce websocket.CloseError
			if !errors.As(err, &ce) && !errors.Is(err, context.Canceled) {
				log.Printf("uplink: read: %v", err)
			}
			return
		}
		s.debugFrame("uplink", "recv", f)
		switch f.Type {
		case proto.TypeAnnounce:
			var p proto.AnnouncePayload
			if err := json.Unmarshal(f.Payload, &p); err == nil {
				reconcile(p.Sessions)
			}
		case proto.TypeOut:
			seq, data, err := proto.DecodeOut(f.Payload)
			if err != nil {
				continue
			}
			mu.Lock()
			ms := mirrors[f.SessionID]
			mu.Unlock()
			if ms != nil {
				ms.sess.PushOut(seq, data)
			}
		case proto.TypeMeta:
			var m proto.MetaPayload
			if err := json.Unmarshal(f.Payload, &m); err != nil {
				continue
			}
			mu.Lock()
			ms := mirrors[f.SessionID]
			mu.Unlock()
			if ms != nil {
				// Mirror sessions adopt the upstream's driver_client_id from
				// this META (SetDriverFromUpstream), so remote /client
				// subscribers see the real PTY driver (the host desktop) and
				// render the viewer overlay. UpdateMeta is the single broadcast
				// point; the raw upstream frame is not re-broadcast separately.
				ms.sess.UpdateMeta(m)
				s.registry.NotifyChange()
			}
		case proto.TypeClose:
			mu.Lock()
			ms := mirrors[f.SessionID]
			delete(mirrors, f.SessionID)
			mu.Unlock()
			if ms != nil {
				ms.sess.Broadcast(f)
				if ms.fwdCancel != nil {
					ms.fwdCancel()
				}
				s.registry.Remove(f.SessionID)
			}
		case proto.TypeCommandEvent:
			s.handleUplinkCommandEvent(f, mirrors, &mu)
		case proto.TypePong:
			// keepalive
		default:
			log.Printf("uplink: unexpected frame type 0x%02x", f.Type)
		}
	}
}

// handleUplinkCommandEvent processes a TypeCommandEvent frame received from
// an uplink. The frame must reference a session id currently in the
// uplink's manifest (mirrors map) — this prevents one uplink from forging
// "command finished" events for another uplink's sessions. host_id is
// pulled from the session's info, not the payload, for the same reason.
func (s *Server) handleUplinkCommandEvent(f proto.Frame, mirrors map[uuid.UUID]*mirrorState, mu *sync.Mutex) {
	if s.cfg.WebPush == nil && s.cfg.Webhook == nil {
		return
	}
	payload, err := proto.DecodeCommandEvent(f)
	if err != nil {
		s.debugf("uplink command_event decode_failed session=%s error=%q", f.SessionID, err)
		return
	}
	mu.Lock()
	ms, ok := mirrors[f.SessionID]
	mu.Unlock()
	if !ok || ms == nil {
		s.debugf("uplink command_event unknown_session session=%s", f.SessionID)
		return
	}
	hostIDStr := ms.sess.Info().HostID
	hostID, _ := uuid.Parse(hostIDStr) // ignore parse error — hostID is informational
	if s.cfg.WebPush != nil {
		s.cfg.WebPush.DispatchCommandFinished(ms.sess.OwnerUserID, webpush.CommandFinished{
			SessionID: f.SessionID,
			HostID:    hostID,
			ExitCode:  payload.ExitCode,
			ElapsedMS: payload.ElapsedMS,
			Label:     payload.Label,
		})
	}
	if s.cfg.Webhook != nil {
		s.cfg.Webhook.DispatchCommandFinished(ms.sess.OwnerUserID, webhook.CommandFinished{
			SessionID: f.SessionID,
			HostID:    hostID,
			ExitCode:  payload.ExitCode,
			ElapsedMS: payload.ElapsedMS,
			Label:     payload.Label,
		})
	}
}
