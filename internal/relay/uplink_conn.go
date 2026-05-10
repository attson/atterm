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

// handleUplink services a desktop app's "control" connection. The first frame
// must be ANNOUNCE; subsequent ANNOUNCEs are full-snapshot reconciliations.
// OUT/META/CLOSE frames flow uplink→relay; STREAM_REQUEST/STOP and IN/RESIZE
// flow relay→uplink.
func (s *Server) handleUplink(ctx context.Context, c *websocket.Conn) {
	c.SetReadLimit(uplinkReadLimit)

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
		enqueue(proto.Frame{Type: proto.TypeStreamRequest, SessionID: id, Payload: payload})

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
		enqueue(proto.Frame{Type: proto.TypeStreamStop, SessionID: id, Payload: payload})
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
				existing.sess.UpdateMeta(proto.MetaPayload{Cwd: info.Cwd, Title: info.Title})
				continue
			}

			sess := session.New(id, info)
			// snapshot capture: id is per-iteration, must capture by value
			sid := id
			sess.SetSubscriberLifecycle(
				func() { startStream(sid) },
				func() { stopStream(sid) },
			)
			s.registry.Add(sess)
			mu.Lock()
			mirrors[id] = &mirrorState{sess: sess}
			mu.Unlock()
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
				wctx, wc := context.WithTimeout(connCtx, uplinkWriteWait)
				err := c.Write(wctx, websocket.MessageBinary, proto.Marshal(f))
				wc()
				if err != nil {
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
				ms.sess.UpdateMeta(m)
				ms.sess.Broadcast(f)
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
		case proto.TypePong:
			// keepalive
		default:
			log.Printf("uplink: unexpected frame type 0x%02x", f.Type)
		}
	}
}
