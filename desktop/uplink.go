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
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/attson/atterm/internal/connhealth"
	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
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

	// outMu protects out. out is the writer goroutine's input channel for
	// the currently-active connection. nil when not connected. Producer
	// callers (e.g. SendCommandEvent) read this under outMu.RLock() and
	// drop on the floor if nil or if the channel buffer is full.
	outMu sync.RWMutex
	out   chan<- proto.Frame

	// eventsEmit is the Wails EventsEmit function used to push events to the
	// frontend. Defaults to wailsruntime.EventsEmit; overridden in tests.
	eventsEmit func(ctx context.Context, name string, data ...interface{})

	// recordError is called once per relay error so the App can keep a
	// recent-errors ring buffer for diagnostics export. Nil-safe.
	recordError func(err error)

	// tracker accumulates link-health metrics (RTT, byte rate, reconnect
	// history, seq gaps) for the connection. Surfaced to the frontend via
	// (a *App).GetUplinkHealth and rendered as the ConnHealthPill / drawer.
	tracker *connhealth.Tracker
	// startMono is the wall time at uplink construction; it anchors the
	// monotonic ms timestamps embedded in PING payloads. Using a relative
	// origin keeps the 8-byte payload uint64-safe forever.
	startMono time.Time

	// accountKey, when non-nil, returns the unlocked E2EE account key for
	// the user authenticated against this remote relay. Nil-returning is
	// the "key not unlocked" state — TypeOut frames bypass encryption and
	// the relay sees plaintext (degrades cleanly: bootstrap-admin / local-
	// only setups, or first-boot before the user has logged in, work
	// unchanged). Set by NewApp wiring (see desktop/app.go).
	accountKey func() []byte
}

func newUplink(relayURL, token, remotePermission string, host *relayHost, recordError func(error), accountKey func() []byte) *uplink {
	return &uplink{
		relayURL:         strings.TrimRight(relayURL, "/"),
		token:            token,
		remotePermission: normalizeRemotePermission(remotePermission),
		host:             host,
		eventsEmit:       wailsruntime.EventsEmit,
		recordError:      recordError,
		tracker:          connhealth.New(),
		startMono:        time.Now(),
		accountKey:       accountKey,
	}
}

// Health returns a snapshot of the uplink's connection-health metrics.
// Safe to call concurrently with the read/write loops. Surfaced to the
// frontend via App.GetUplinkHealth.
func (u *uplink) Health() connhealth.Snapshot {
	return u.tracker.Snapshot(time.Now())
}

// monoMS returns the uplink-relative monotonic timestamp in milliseconds
// that we embed in PING payloads. Echoed back unchanged inside the matching
// PONG, then subtracted from a fresh monoMS() to compute RTT.
func (u *uplink) monoMS() uint64 {
	return uint64(time.Since(u.startMono).Milliseconds())
}

// Run keeps a control connection open to the remote relay until ctx is cancelled.
func (u *uplink) Run(ctx context.Context) {
	backoff := 500 * time.Millisecond
	for ctx.Err() == nil {
		u.tracker.SetState(connhealth.StateConnecting, time.Now())
		err := u.runOnce(ctx)
		if ctx.Err() != nil {
			u.tracker.SetState(connhealth.StateClosed, time.Now())
			return
		}
		if err != nil {
			reason := "network_error"
			if err.Error() != "" {
				reason = err.Error()
			}
			u.tracker.SetReconnectReason(reason, time.Now())
			if u.recordError != nil {
				u.recordError(err)
			}
			log.Printf("uplink: %v (retry in %s)", err, backoff)
		}
		u.tracker.SetState(connhealth.StateReconnecting, time.Now())
		select {
		case <-ctx.Done():
			u.tracker.SetState(connhealth.StateClosed, time.Now())
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 8*time.Second {
			backoff = 8 * time.Second
		}
	}
	u.tracker.SetState(connhealth.StateClosed, time.Now())
}

// streamingLocal tracks one active STREAM_REQUEST: a subscriber on a local
// session and the goroutine that forwards its frames to the relay.
type streamingLocal struct {
	id        uuid.UUID
	sub       *session.Subscriber
	cancelFwd context.CancelFunc
}

type streamForwardStats struct {
	sessionID        uuid.UUID
	started          time.Time
	outFrames        int
	outBytes         int
	metaFrames       int
	closeFrames      int
	firstOutLogged   bool
	replayDropLogged bool
	nextReportBytes  int
	nextReportFrames int
}

func newStreamForwardStats(id uuid.UUID) *streamForwardStats {
	return &streamForwardStats{
		sessionID:        id,
		started:          time.Now(),
		nextReportBytes:  1024 * 1024,
		nextReportFrames: 256,
	}
}

func (s *streamForwardStats) observe(f proto.Frame) {
	switch f.Type {
	case proto.TypeOut:
		seq, data, err := proto.DecodeOut(f.Payload)
		if err != nil {
			log.Printf("desktop-uplink: stream_out_decode_failed session=%s error=%v", s.sessionID, err)
			return
		}
		s.outFrames++
		s.outBytes += len(data)
		if !s.firstOutLogged {
			s.firstOutLogged = true
			log.Printf("desktop-uplink: stream_out_first session=%s seq=%d bytes=%d", s.sessionID, seq, len(data))
		}
		if s.outBytes >= s.nextReportBytes || s.outFrames >= s.nextReportFrames {
			log.Printf("desktop-uplink: stream_out_progress session=%s out_frames=%d out_bytes=%d last_seq=%d", s.sessionID, s.outFrames, s.outBytes, seq)
			for s.outBytes >= s.nextReportBytes {
				s.nextReportBytes += 1024 * 1024
			}
			for s.outFrames >= s.nextReportFrames {
				s.nextReportFrames += 256
			}
		}
	case proto.TypeMeta:
		s.metaFrames++
		log.Printf("desktop-uplink: stream_meta_forward session=%s payload_bytes=%d", s.sessionID, len(f.Payload))
	case proto.TypeClose:
		s.closeFrames++
		log.Printf("desktop-uplink: stream_close_forward session=%s payload_bytes=%d", s.sessionID, len(f.Payload))
	case proto.TypeReplayProgress:
		if !s.replayDropLogged {
			s.replayDropLogged = true
			log.Printf("desktop-uplink: stream_replay_progress_drop session=%s reason=local_subscriber_only", s.sessionID)
		}
	}
}

func (s *streamForwardStats) finish(reason string) {
	log.Printf(
		"desktop-uplink: stream_end session=%s reason=%s out_frames=%d out_bytes=%d meta_frames=%d close_frames=%d duration=%s",
		s.sessionID,
		reason,
		s.outFrames,
		s.outBytes,
		s.metaFrames,
		s.closeFrames,
		time.Since(s.started).Round(time.Millisecond),
	)
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
	u.outMu.Lock()
	u.out = out
	u.outMu.Unlock()
	defer func() {
		u.outMu.Lock()
		u.out = nil
		u.outMu.Unlock()
	}()

	// Send first ANNOUNCE so the relay registers the host immediately.
	if err := u.writeAnnounce(connCtx, conn); err != nil {
		return err
	}
	log.Printf("uplink: connected, sent ANNOUNCE (%d session(s))", len(u.host.Snapshot()))
	u.tracker.SetState(connhealth.StateConnected, time.Now())

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
				data := proto.Marshal(f)
				err := conn.Write(wctx, websocket.MessageBinary, data)
				wc()
				if err != nil {
					cancelConn()
					return
				}
				u.tracker.OnBytesOut(len(data), time.Now())
			}
		}
	}()

	// Application-level PING for RTT + 1Hz Tick for byte EMAs.
	go func() {
		pingTicker := time.NewTicker(5 * time.Second)
		tickTicker := time.NewTicker(time.Second)
		defer pingTicker.Stop()
		defer tickTicker.Stop()
		for {
			select {
			case <-connCtx.Done():
				return
			case <-tickTicker.C:
				u.tracker.Tick(time.Now())
			case <-pingTicker.C:
				frame := proto.Frame{Type: proto.TypePing, Payload: proto.EncodePingTimestamp(u.monoMS())}
				select {
				case out <- frame:
				case <-connCtx.Done():
					return
				default:
					// Channel full — skip; next tick will retry.
				}
			}
		}
	}()

	startStream := func(id uuid.UUID, sinceSeq uint64) {
		log.Printf("desktop-uplink: stream_request session=%s since_seq=%d", id, sinceSeq)
		mu.Lock()
		if _, ok := streaming[id]; ok {
			mu.Unlock()
			log.Printf("desktop-uplink: stream_request_duplicate session=%s since_seq=%d", id, sinceSeq)
			return
		}
		sub, replayToSeq, err := u.host.SubscribeLocal(id, sinceSeq)
		if err != nil {
			mu.Unlock()
			log.Printf("desktop-uplink: stream_request_failed session=%s since_seq=%d error=%v", id, sinceSeq, err)
			return
		}
		fwdCtx, cancelFwd := context.WithCancel(connCtx)
		streaming[id] = &streamingLocal{id: id, sub: sub, cancelFwd: cancelFwd}
		mu.Unlock()
		log.Printf("desktop-uplink: stream_subscribed session=%s since_seq=%d replay_to_seq=%d", id, sinceSeq, replayToSeq)

		// Forwarder: copies frames from local subscriber to remote out queue.
		// Frames already carry SessionID == id, which the remote uplink_conn
		// uses to route into the mirror session's PushOut/Broadcast.
		go func() {
			stats := newStreamForwardStats(id)
			reason := "unknown"
			defer func() { stats.finish(reason) }()
			for {
				select {
				case <-fwdCtx.Done():
					reason = "context_done"
					return
				case f, ok := <-sub.Out():
					if !ok {
						reason = "subscriber_closed"
						return
					}
					if !forwardLocalSubscriberFrame(fwdCtx, out, f, stats, func() {
						u.host.RequestLocalRepaint(id)
					}, u.accountKey) {
						reason = "uplink_out_closed"
						return
					}
				case <-sub.Done():
					reason = "subscriber_done"
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
			log.Printf("desktop-uplink: stream_stop_unknown session=%s", id)
			return
		}
		log.Printf("desktop-uplink: stream_stop session=%s", id)
		s.cancelFwd()
		u.host.UnsubscribeLocal(id, s.sub)
	}

	// Reader.
	for {
		mt, data, err := conn.Read(connCtx)
		if err != nil {
			var ce websocket.CloseError
			if errors.As(err, &ce) {
				u.handleCloseError(ctx, ce)
				return nil
			}
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		if mt != websocket.MessageBinary {
			continue
		}
		u.tracker.OnBytesIn(len(data), time.Now())
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
			log.Printf("desktop-uplink: inbound_recv type=%s %s", desktopUplinkFrameTypeName(f.Type), desktopUplinkFrameLogDetails(f))
			if !localFrameAllowedByPermission(u.remotePermission, f.Type) {
				log.Printf("desktop-uplink: inbound_drop_permission type=%s permission=%s %s", desktopUplinkFrameTypeName(f.Type), u.remotePermission, desktopUplinkFrameLogDetails(f))
				continue
			}
			if err := u.host.SendLocalInbound(f.SessionID, f); err != nil {
				log.Printf("desktop-uplink: inbound_forward_failed type=%s %s error=%v", desktopUplinkFrameTypeName(f.Type), desktopUplinkFrameLogDetails(f), err)
			} else {
				log.Printf("desktop-uplink: inbound_forward_ok type=%s %s", desktopUplinkFrameTypeName(f.Type), desktopUplinkFrameLogDetails(f))
			}
		case proto.TypeClaimDriver:
			log.Printf("desktop-uplink: inbound_recv type=CLAIM_DRIVER %s", desktopUplinkFrameLogDetails(f))
			var cp proto.ClaimDriverPayload
			if err := json.Unmarshal(f.Payload, &cp); err != nil {
				log.Printf("desktop-uplink: inbound_drop type=CLAIM_DRIVER reason=bad_payload session=%s err=%q", f.SessionID, err)
				continue
			}
			if err := u.host.ClaimLocalDriver(f.SessionID, cp.ClientID, cp.ClientName); err != nil {
				log.Printf("desktop-uplink: inbound_drop type=CLAIM_DRIVER reason=%q session=%s", err, f.SessionID)
			} else {
				log.Printf("desktop-uplink: inbound_forward_ok type=CLAIM_DRIVER session=%s client_id=%q client_name=%q", f.SessionID, cp.ClientID, cp.ClientName)
			}
		case proto.TypeViewers:
			var p proto.ViewersPayload
			if err := json.Unmarshal(f.Payload, &p); err != nil {
				continue
			}
			if u.eventsEmit != nil {
				u.eventsEmit(ctx, "relay:viewers", map[string]any{"session_id": p.SessionID, "count": p.Count})
			}
		case proto.TypeAuthInfo:
			var info struct {
				UserID string `json:"user_id"`
			}
			if err := json.Unmarshal(f.Payload, &info); err == nil {
				u.eventsEmit(ctx, "relay:auth-info", info)
			}
		case proto.TypePong:
			// Relay echoes our PING payload back unchanged. An 8-byte
			// payload carries our monoMS at send time; subtract from a
			// fresh reading to get RTT.
			if ts, ok := proto.DecodePingTimestamp(f.Payload); ok {
				rtt := int(u.monoMS() - ts)
				if rtt >= 0 && rtt < 60_000 {
					u.tracker.OnPongRTT(rtt, time.Now())
				}
			}
		default:
			log.Printf("uplink: unexpected frame type 0x%02x", f.Type)
		}
	}
}

// handleCloseError inspects a WebSocket close frame and, for auth-related
// codes (4001-4003), emits a "relay:auth-error" event to the frontend so it
// can surface a banner prompting the user to update their settings.
func (u *uplink) handleCloseError(ctx context.Context, ce websocket.CloseError) {
	// Map the close reason string first; fall back to code-based mapping.
	reason := ce.Reason
	if reason == "" {
		switch int(ce.Code) {
		case 4001:
			reason = "auth_invalid_token"
		case 4002:
			reason = "session_id_owner_mismatch"
		case 4003:
			reason = "forbidden"
		}
	}
	// Only emit for auth-related codes.
	switch int(ce.Code) {
	case 4001, 4002, 4003:
		if u.eventsEmit != nil {
			u.eventsEmit(ctx, "relay:auth-error", map[string]string{"reason": reason})
		}
		if u.recordError != nil {
			u.recordError(fmt.Errorf("%s", reason))
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

func forwardLocalSubscriberFrame(ctx context.Context, out chan<- proto.Frame, f proto.Frame, stats *streamForwardStats, requestRepaint func(), accountKey func() []byte) bool {
	if stats != nil {
		stats.observe(f)
	}
	if localSubscriberFrameRequestsRepaint(f) && requestRepaint != nil {
		log.Printf("desktop-uplink: stream_request_repaint %s", desktopUplinkFrameLogDetails(f))
		requestRepaint()
	}
	if !localSubscriberFrameForwardedToUplink(f.Type) {
		return true
	}
	if f.Type == proto.TypeOut && accountKey != nil {
		if sealed, ok := sealOutFrame(f, accountKey()); ok {
			f = sealed
		}
	}
	select {
	case out <- f:
		return true
	case <-ctx.Done():
		return false
	}
}

// sealOutFrame wraps a TypeOut payload's bytes with an AEAD envelope so
// the remote relay carries only opaque ciphertext. The wire layout of
// TypeOut stays seq(8B BE) || data — only the data portion is replaced
// with the envelope. accountKey == nil (user not logged in / no remote
// E2EE relay) is a clean bypass: the original plaintext frame is sent
// through.
//
// ok == false on any sealing error; caller falls back to plaintext. This
// is the right call for the M2b agent path because losing one chunk is
// preferable to dropping the session, and the only failure modes here are
// program bugs (account_key shorter than 32B) — they will be caught in
// unit tests, not at runtime.
func sealOutFrame(f proto.Frame, accountKey []byte) (proto.Frame, bool) {
	if len(accountKey) < e2eecrypto.SessionKeySize {
		return f, false
	}
	seq, data, err := proto.DecodeOut(f.Payload)
	if err != nil {
		return f, false
	}
	sk, err := e2eecrypto.DeriveSessionKey(accountKey, f.SessionID)
	if err != nil {
		return f, false
	}
	envelope, err := e2eecrypto.SealOut(sk, f.SessionID, byte(proto.TypeOut), seq, data)
	if err != nil {
		return f, false
	}
	return proto.EncodeOut(f.SessionID, seq, envelope), true
}

func localSubscriberFrameRequestsRepaint(f proto.Frame) bool {
	if f.Type != proto.TypeOut {
		return false
	}
	seq, data, err := proto.DecodeOut(f.Payload)
	return err == nil && seq == 0 && bytes.Equal(data, []byte("\x1b[?1049h\x1b[2J\x1b[H"))
}

func desktopUplinkFrameTypeName(typ proto.Type) string {
	switch typ {
	case proto.TypeIn:
		return "IN"
	case proto.TypeResize:
		return "RESIZE"
	case proto.TypeOut:
		return "OUT"
	case proto.TypeMeta:
		return "META"
	case proto.TypeClose:
		return "CLOSE"
	case proto.TypePasteImage:
		return "PASTE_IMAGE"
	case proto.TypeClaimDriver:
		return "CLAIM_DRIVER"
	case proto.TypeReplayProgress:
		return "REPLAY_PROGRESS"
	default:
		return fmt.Sprintf("0x%02x", typ)
	}
}

func desktopUplinkFrameLogDetails(f proto.Frame) string {
	prefix := fmt.Sprintf("session=%s payload_bytes=%d", f.SessionID, len(f.Payload))
	switch f.Type {
	case proto.TypePasteImage:
		var p proto.PasteImagePayload
		if err := json.Unmarshal(f.Payload, &p); err != nil {
			return prefix + fmt.Sprintf(" bad_payload=%q", err)
		}
		return fmt.Sprintf(
			"session=%s filename=%q content_type=%q image_bytes=%d payload_bytes=%d",
			f.SessionID,
			p.Filename,
			p.ContentType,
			len(p.Data),
			len(f.Payload),
		)
	case proto.TypeResize:
		cols, rows, err := proto.DecodeResize(f.Payload)
		if err != nil {
			return prefix + fmt.Sprintf(" bad_payload=%q", err)
		}
		return fmt.Sprintf("session=%s cols=%d rows=%d", f.SessionID, cols, rows)
	case proto.TypeOut:
		seq, data, err := proto.DecodeOut(f.Payload)
		if err != nil {
			return prefix + fmt.Sprintf(" bad_payload=%q", err)
		}
		return fmt.Sprintf("session=%s seq=%d out_bytes=%d", f.SessionID, seq, len(data))
	default:
		return prefix
	}
}

// SendCommandEvent queues a TypeCommandEvent frame for the writer goroutine.
// Drops on the floor when the uplink is nil, not connected, or its out
// channel buffer is full. Failure is silent because the local OS
// notification has already fired and Web Push misses are acceptable.
func (u *uplink) SendCommandEvent(sessionID uuid.UUID, exit, elapsedMS int, label string) {
	if u == nil {
		return
	}
	u.outMu.RLock()
	out := u.out
	u.outMu.RUnlock()
	if out == nil {
		return
	}
	frame, err := proto.EncodeCommandEvent(sessionID, proto.CommandEventPayload{
		ExitCode:  exit,
		ElapsedMS: elapsedMS,
		Label:     label,
	})
	if err != nil {
		log.Printf("uplink: SendCommandEvent encode: %v", err)
		return
	}
	select {
	case out <- frame:
	default:
		log.Printf("uplink: out chan full; dropping command event session=%s", sessionID)
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
