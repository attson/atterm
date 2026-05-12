// Package session is the relay-side authoritative model for a live PTY session.
//
// One Session per live agent connection: it holds the scrollback ring, the
// metadata snapshot, the fan-out subscriber set, and the inbound channel that
// carries client-side IN/RESIZE frames back to the agent.
package session

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/ringbuf"
	"github.com/google/uuid"
)

const (
	subscriberQueueDepth        = 4096
	scrollbackBytes             = 4 * 1024 * 1024
	inboundQueueDepth           = 64
	replayProgressIntervalBytes = 64 * 1024
)

// Session is the relay-side state for one PTY.
type Session struct {
	ID        uuid.UUID
	StartedAt time.Time

	mu      sync.RWMutex
	meta    proto.SessionInfo
	closed  bool
	subs    map[*Subscriber]struct{}
	scroll  *ringbuf.Buffer
	inbound chan proto.Frame // bounded; client -> agent

	// Optional lifecycle hooks. Used by mirror sessions (Phase 1.5 lazy
	// uplink) so the relay can ask the upstream host to start/stop a stream
	// only when there's at least one local subscriber.
	onFirstSubscribe  func()
	onLastUnsubscribe func()
}

// Subscriber is a client connection's outbox.
type Subscriber struct {
	out    chan proto.Frame
	closed chan struct{}
	once   sync.Once
}

// Out returns the channel this subscriber should be drained from.
func (s *Subscriber) Out() <-chan proto.Frame { return s.out }

// Done is closed when the subscriber has been removed (session closed or lag).
func (s *Subscriber) Done() <-chan struct{} { return s.closed }

func (s *Subscriber) close() {
	s.once.Do(func() { close(s.closed) })
}

// New creates a Session with the given id and initial OPEN metadata.
func New(id uuid.UUID, meta proto.SessionInfo) *Session {
	return &Session{
		ID:        id,
		StartedAt: time.Now(),
		meta:      meta,
		subs:      make(map[*Subscriber]struct{}),
		scroll:    ringbuf.New(scrollbackBytes),
		inbound:   make(chan proto.Frame, inboundQueueDepth),
	}
}

// Info returns a snapshot of current metadata.
func (s *Session) Info() proto.SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	info := s.meta
	info.ID = s.ID.String()
	info.StartedAt = s.StartedAt.Unix()
	return info
}

// SetSubscriberLifecycle registers callbacks fired when the subscriber count
// transitions 0→1 (first) and N→0 (last). Both fire asynchronously to avoid
// holding session locks. Pass nil for either to disable that hook. Replaces
// any previously-registered hooks.
func (s *Session) SetSubscriberLifecycle(first, last func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onFirstSubscribe = first
	s.onLastUnsubscribe = last
}

// UpdateMeta merges new cwd/title from a META frame.
func (s *Session) UpdateMeta(m proto.MetaPayload) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.Cwd != "" {
		s.meta.Cwd = m.Cwd
	}
	if m.Title != "" {
		s.meta.Title = m.Title
	}
}

// UpdateRemotePermission records the owner-published remote permission for
// mirror sessions. Empty keeps the backwards-compatible full-control default.
func (s *Session) UpdateRemotePermission(value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta.RemotePermission = value
}

// UpdateSize records the latest PTY window size advertised by a client resize.
func (s *Session) UpdateSize(cols, rows uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cols > 0 {
		s.meta.Cols = cols
	}
	if rows > 0 {
		s.meta.Rows = rows
	}
}

// PushOut is called by the agent reader loop for each TypeOut frame.
// It records the chunk to scrollback and fans out to current subscribers.
// Slow subscribers are dropped (their channels are closed) so they reconnect.
func (s *Session) PushOut(seq uint64, data []byte) {
	s.scroll.Push(ringbuf.Chunk{Seq: seq, Data: append([]byte(nil), data...)})
	frame := proto.EncodeOut(s.ID, seq, data)
	s.fanout(frame)
}

// Broadcast sends a frame (META/CLOSE) to all subscribers.
func (s *Session) Broadcast(f proto.Frame) {
	s.fanout(f)
}

func (s *Session) fanout(f proto.Frame) {
	s.mu.RLock()
	subs := make([]*Subscriber, 0, len(s.subs))
	for sub := range s.subs {
		subs = append(subs, sub)
	}
	s.mu.RUnlock()
	for _, sub := range subs {
		select {
		case sub.out <- f:
		default:
			// slow consumer: drop and let it reconnect with ATTACH(since_seq)
			s.removeSubscriber(sub)
		}
	}
}

// Subscribe registers a new client outbox and replays scrollback strictly
// greater than sinceSeq. When sinceSeq is 0 the full scrollback is replayed.
// Returns the subscriber and the largest seq replayed (so the client can
// resume from that point on the live stream).
func (s *Session) Subscribe(sinceSeq uint64) (*Subscriber, uint64) {
	sub := &Subscriber{
		out:    make(chan proto.Frame, subscriberQueueDepth),
		closed: make(chan struct{}),
	}

	chunks := s.scroll.Since(sinceSeq)
	totalBytes := replayBytes(chunks)
	_ = enqueueReplayProgress(sub, s.ID, proto.ReplayProgressStart, 0, totalBytes, 0)
	// If the client asked for a seq older than what we still have, the gap
	// is unrecoverable: send a soft truncation marker (clear screen) so the
	// terminal does not render torn state, then replay everything we have.
	if sinceSeq > 0 && s.scroll.OldestSeq() > sinceSeq+1 && len(chunks) > 0 {
		marker := proto.EncodeOut(s.ID, 0, []byte("\x1b[2J\x1b[H"))
		select {
		case sub.out <- marker:
		default:
		}
	}
	var (
		lastSeq       uint64
		replayedBytes uint64
		nextProgress  uint64 = replayProgressIntervalBytes
		ok            bool
	)
	lastSeq, replayedBytes, nextProgress, ok = enqueueReplayChunks(sub, s.ID, chunks, lastSeq, replayedBytes, totalBytes, nextProgress)
	if !ok {
		sub.close()
		return sub, lastSeq
	}

	s.mu.Lock()
	for !s.closed {
		catchup := s.scroll.Since(lastSeq)
		if len(catchup) == 0 {
			break
		}
		totalBytes += replayBytes(catchup)
		s.mu.Unlock()
		lastSeq, replayedBytes, nextProgress, ok = enqueueReplayChunks(sub, s.ID, catchup, lastSeq, replayedBytes, totalBytes, nextProgress)
		if !ok {
			sub.close()
			return sub, lastSeq
		}
		s.mu.Lock()
	}
	closed := s.closed
	wasEmpty := len(s.subs) == 0
	added := false
	if !closed && enqueueReplayProgress(sub, s.ID, proto.ReplayProgressEnd, replayedBytes, totalBytes, lastSeq) {
		s.subs[sub] = struct{}{}
		added = true
	}
	firstHook := s.onFirstSubscribe
	s.mu.Unlock()

	if closed || !added {
		sub.close()
		return sub, lastSeq
	}
	if wasEmpty && firstHook != nil {
		go firstHook()
	}
	return sub, lastSeq
}

func replayBytes(chunks []ringbuf.Chunk) uint64 {
	var total uint64
	for _, c := range chunks {
		total += uint64(len(c.Data))
	}
	return total
}

func enqueueReplayChunks(sub *Subscriber, id uuid.UUID, chunks []ringbuf.Chunk, lastSeq, replayedBytes, totalBytes, nextProgress uint64) (uint64, uint64, uint64, bool) {
	for _, c := range chunks {
		select {
		case sub.out <- proto.EncodeOut(id, c.Seq, c.Data):
			lastSeq = c.Seq
			replayedBytes += uint64(len(c.Data))
		default:
			return lastSeq, replayedBytes, nextProgress, false
		}
		for replayedBytes >= nextProgress && nextProgress > 0 {
			if !enqueueReplayProgress(sub, id, proto.ReplayProgressChunk, replayedBytes, totalBytes, lastSeq) {
				return lastSeq, replayedBytes, nextProgress, false
			}
			nextProgress += replayProgressIntervalBytes
		}
	}
	return lastSeq, replayedBytes, nextProgress, true
}

func enqueueReplayProgress(sub *Subscriber, id uuid.UUID, phase string, bytes, totalBytes, seq uint64) bool {
	payload, err := json.Marshal(proto.ReplayProgressPayload{
		Phase:      phase,
		Bytes:      bytes,
		TotalBytes: totalBytes,
		Seq:        seq,
	})
	if err != nil {
		return false
	}
	select {
	case sub.out <- proto.Frame{Type: proto.TypeReplayProgress, SessionID: id, Payload: payload}:
		return true
	default:
		return false
	}
}

// Unsubscribe removes a client outbox.
func (s *Session) Unsubscribe(sub *Subscriber) {
	s.removeSubscriber(sub)
}

func (s *Session) removeSubscriber(sub *Subscriber) {
	s.mu.Lock()
	_, was := s.subs[sub]
	if was {
		delete(s.subs, sub)
	}
	nowEmpty := len(s.subs) == 0
	lastHook := s.onLastUnsubscribe
	s.mu.Unlock()
	sub.close()
	if was && nowEmpty && lastHook != nil {
		go lastHook()
	}
}

// Inbound returns the channel the relay writes IN/RESIZE frames into; the
// agent reader pulls from it.
func (s *Session) Inbound() <-chan proto.Frame { return s.inbound }

// SendInbound enqueues a client-originated frame for the agent.
// Returns false if the queue is full (caller may drop or disconnect the client).
func (s *Session) SendInbound(f proto.Frame) bool {
	select {
	case s.inbound <- f:
		return true
	default:
		return false
	}
}

// Close marks the session ended, closes all subscribers, and stops accepting input.
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	subs := s.subs
	s.subs = nil
	close(s.inbound)
	s.mu.Unlock()
	for sub := range subs {
		sub.close()
	}
}

// IsClosed reports whether the session has been closed.
func (s *Session) IsClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}
