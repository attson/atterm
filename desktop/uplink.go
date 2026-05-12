// Phase 1.5 — lazy uplink. The uplink is the desktop app's optional client
// connection to a remote atterm-relay. It advertises the local mini-relay's
// sessions via ANNOUNCE so other devices can see them, but it only streams a
// session's bytes when the remote relay asks (STREAM_REQUEST), and stops
// when the last remote attacher leaves (STREAM_STOP). The connection is best-
// effort: when it drops, the local app keeps working unaffected.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

const (
	uplinkOutQueueDepth = 256
	announceInterval    = 30 * time.Second
	uplinkDialTimeout   = 10 * time.Second
	uplinkWriteTimeout  = 10 * time.Second
)

// uplink runs the desktop side of the lazy mirror. One instance per desktop app.
type uplink struct {
	relayURL         string
	token            string
	remotePermission string
	host             *relayHost

	announced announceCache
}

func newUplink(relayURL, token, remotePermission string, host *relayHost) *uplink {
	return &uplink{
		relayURL:         strings.TrimRight(relayURL, "/"),
		token:            token,
		remotePermission: normalizeRemotePermission(remotePermission),
		host:             host,
	}
}

// Run keeps a control connection open to the remote relay until ctx is cancelled.
func (u *uplink) Run(ctx context.Context) {
	backoff := 500 * time.Millisecond
	for ctx.Err() == nil {
		err := u.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("uplink: %v (retry in %s)", err, backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 8*time.Second {
			backoff = 8 * time.Second
		}
	}
}

// streamingLocal tracks one active STREAM_REQUEST: a subscriber on a local
// session and the goroutine that forwards its frames to the relay.
type streamingLocal struct {
	id        uuid.UUID
	sub       *session.Subscriber
	cancelFwd context.CancelFunc
}

func (u *uplink) runOnce(ctx context.Context) error {
	dialCtx, cancelDial := context.WithTimeout(ctx, uplinkDialTimeout)
	hdr := map[string][]string{}
	if u.token != "" {
		hdr["Authorization"] = []string{"Bearer " + u.token}
	}
	conn, _, err := websocket.Dial(dialCtx, u.relayURL+"/uplink", &websocket.DialOptions{HTTPHeader: hdr})
	cancelDial()
	if err != nil {
		return err
	}
	conn.SetReadLimit(17 * 1024 * 1024)
	defer conn.Close(websocket.StatusNormalClosure, "")

	connCtx, cancelConn := context.WithCancel(ctx)
	defer cancelConn()
	u.announced.reset()

	out := make(chan proto.Frame, uplinkOutQueueDepth)

	// Send first ANNOUNCE so the relay registers the host immediately.
	if err := u.writeAnnounce(connCtx, conn); err != nil {
		return err
	}
	log.Printf("uplink: connected, sent ANNOUNCE (%d session(s))", len(u.host.Snapshot()))

	// streaming map and its lock.
	var (
		mu        sync.Mutex
		streaming = make(map[uuid.UUID]*streamingLocal)
	)
	stopAll := func() {
		mu.Lock()
		defer mu.Unlock()
		for id, s := range streaming {
			s.cancelFwd()
			u.host.UnsubscribeLocal(id, s.sub)
		}
		streaming = make(map[uuid.UUID]*streamingLocal)
	}
	defer stopAll()

	// Periodic announce + watch local changes.
	go func() {
		ticker := time.NewTicker(announceInterval)
		defer ticker.Stop()
		changes := u.host.server.Registry().SubscribeChanges()
		defer changes.Close()
		for {
			select {
			case <-connCtx.Done():
				return
			case <-ticker.C:
			case <-changes.C():
			}
			if err := u.writeAnnounce(connCtx, conn); err != nil {
				cancelConn()
				return
			}
		}
	}()

	// Writer: drain `out`. ANNOUNCEs are sent inline (not via `out`) since
	// they carry an authoritative snapshot timing decision.
	go func() {
		for {
			select {
			case <-connCtx.Done():
				return
			case f := <-out:
				wctx, wc := context.WithTimeout(connCtx, uplinkWriteTimeout)
				err := conn.Write(wctx, websocket.MessageBinary, proto.Marshal(f))
				wc()
				if err != nil {
					cancelConn()
					return
				}
			}
		}
	}()

	startStream := func(id uuid.UUID, sinceSeq uint64) {
		mu.Lock()
		if _, ok := streaming[id]; ok {
			mu.Unlock()
			return
		}
		sub, err := u.host.SubscribeLocal(id, sinceSeq)
		if err != nil {
			mu.Unlock()
			log.Printf("uplink: STREAM_REQUEST for unknown session %s", id)
			return
		}
		fwdCtx, cancelFwd := context.WithCancel(connCtx)
		streaming[id] = &streamingLocal{id: id, sub: sub, cancelFwd: cancelFwd}
		mu.Unlock()

		// Forwarder: copies frames from local subscriber to remote out queue.
		// Frames already carry SessionID == id, which the remote uplink_conn
		// uses to route into the mirror session's PushOut/Broadcast.
		go func() {
			for {
				select {
				case <-fwdCtx.Done():
					return
				case f, ok := <-sub.Out():
					if !ok {
						return
					}
					if !localSubscriberFrameForwardedToUplink(f.Type) {
						continue
					}
					select {
					case out <- f:
					case <-fwdCtx.Done():
						return
					}
				case <-sub.Done():
					return
				}
			}
		}()
	}

	stopStream := func(id uuid.UUID) {
		mu.Lock()
		s, ok := streaming[id]
		if ok {
			delete(streaming, id)
		}
		mu.Unlock()
		if !ok {
			return
		}
		s.cancelFwd()
		u.host.UnsubscribeLocal(id, s.sub)
	}

	// Reader.
	for {
		mt, data, err := conn.Read(connCtx)
		if err != nil {
			var ce websocket.CloseError
			if !errors.As(err, &ce) && !errors.Is(err, context.Canceled) {
				return err
			}
			return nil
		}
		if mt != websocket.MessageBinary {
			continue
		}
		f, err := proto.Unmarshal(data)
		if err != nil {
			continue
		}
		switch f.Type {
		case proto.TypeStreamRequest:
			var p proto.StreamRequestPayload
			if err := json.Unmarshal(f.Payload, &p); err != nil {
				continue
			}
			id, err := uuid.Parse(p.SessionID)
			if err != nil {
				continue
			}
			startStream(id, p.SinceSeq)
		case proto.TypeStreamStop:
			var p proto.StreamStopPayload
			if err := json.Unmarshal(f.Payload, &p); err != nil {
				continue
			}
			id, err := uuid.Parse(p.SessionID)
			if err != nil {
				continue
			}
			stopStream(id)
		case proto.TypeIn, proto.TypeResize, proto.TypePasteImage:
			if !localFrameAllowedByPermission(u.remotePermission, f.Type) {
				log.Printf("uplink: drop inbound frame 0x%02x for permission %s", f.Type, u.remotePermission)
				continue
			}
			if err := u.host.SendLocalInbound(f.SessionID, f); err != nil {
				log.Printf("uplink: forward inbound: %v", err)
			}
		case proto.TypePong:
			// keepalive ack from relay
		default:
			log.Printf("uplink: unexpected frame type 0x%02x", f.Type)
		}
	}
}

func (u *uplink) writeAnnounce(ctx context.Context, conn *websocket.Conn) error {
	hostID, host, user := u.host.HostMeta()
	payload, err := buildAnnouncePayload(hostID, host, user, u.host.Snapshot(), u.remotePermission)
	if err != nil {
		return err
	}
	if !u.announced.shouldSend(payload) {
		return nil
	}
	wctx, cancel := context.WithTimeout(ctx, uplinkWriteTimeout)
	defer cancel()
	if err := conn.Write(wctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type:    proto.TypeAnnounce,
		Payload: payload,
	})); err != nil {
		return err
	}
	u.announced.markSent(payload)
	return nil
}

func buildAnnouncePayload(hostID, host, user string, sessions []proto.SessionInfo, remotePermission string) ([]byte, error) {
	snapshot := append([]proto.SessionInfo(nil), sessions...)
	perm := normalizeRemotePermission(remotePermission)
	for i := range snapshot {
		snapshot[i].RemotePermission = perm
	}
	sort.Slice(snapshot, func(i, j int) bool { return snapshot[i].ID < snapshot[j].ID })
	return json.Marshal(proto.AnnouncePayload{
		HostID:   hostID,
		Host:     host,
		User:     user,
		Sessions: snapshot,
	})
}

func normalizeRemotePermission(value string) string {
	switch value {
	case proto.RemotePermissionView, proto.RemotePermissionControl, proto.RemotePermissionFull:
		return value
	default:
		return proto.RemotePermissionFull
	}
}

func localFrameAllowedByPermission(remotePermission string, typ proto.Type) bool {
	switch typ {
	case proto.TypeIn, proto.TypeResize:
		return remotePermission == proto.RemotePermissionControl || remotePermission == proto.RemotePermissionFull
	case proto.TypePasteImage:
		return remotePermission == proto.RemotePermissionFull
	default:
		return true
	}
}

func localSubscriberFrameForwardedToUplink(typ proto.Type) bool {
	switch typ {
	case proto.TypeOut, proto.TypeMeta, proto.TypeClose:
		return true
	default:
		return false
	}
}

type announceCache struct {
	last []byte
}

func (c *announceCache) shouldSend(payload []byte) bool {
	return !bytes.Equal(c.last, payload)
}

func (c *announceCache) markSent(payload []byte) {
	c.last = append(c.last[:0], payload...)
}

func (c *announceCache) reset() {
	c.last = nil
}
