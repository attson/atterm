// Package session is the relay-side authoritative model for a live PTY session.
//
// One Session per live agent connection: it holds the scrollback ring, the
// metadata snapshot, the fan-out subscriber set, and the inbound channel that
// carries client-side IN/RESIZE frames back to the agent.
package session

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
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
	// Replay coalesces consecutive small chunks into one OUT frame up to this
	// many bytes so the subscriber outbox can't be exhausted by chunk count.
	// At 16 KiB, a full 4 MiB scrollback replays in at most 256 OUT frames —
	// well below subscriberQueueDepth — regardless of original chunk granularity.
	replayCoalesceTargetBytes = 16 * 1024
)

// Session is the relay-side state for one PTY.
type Session struct {
	ID          uuid.UUID
	StartedAt   time.Time
	OwnerUserID string // ULID; empty for legacy/non-account paths

	mu         sync.RWMutex
	meta       proto.SessionInfo
	closed     bool
	subs       map[*Subscriber]struct{}
	scroll     *ringbuf.Buffer
	inbound    chan proto.Frame // bounded; client -> agent
	altScreen  bool
	termTail   []byte
	osc133Buf  []byte
	cmdStarted time.Time

	// driverSubscriber is the only subscriber whose IN/RESIZE/PASTE_IMAGE
	// frames are forwarded to the PTY. Nil means no driver is currently
	// assigned. driverClientID is the end-to-end client_id broadcast in META
	// so clients can recognize themselves; driverClientName is the
	// human-readable hostname shown in the viewer overlay.
	driverSubscriber *Subscriber
	driverClientID   string
	driverClientName string

	// driverFromUpstream marks a MIRROR session: its driver is dictated by the
	// upstream META forwarded over the uplink (the real PTY driver on the host
	// relay), not by its own first subscriber. Local sessions leave this false
	// and keep first-subscriber-wins + commit #4's solo behavior.
	driverFromUpstream bool

	// Optional lifecycle hooks. Used by mirror sessions (Phase 1.5 lazy
	// uplink) so the relay can ask the upstream host to start/stop a stream
	// only when there's at least one local subscriber.
	onFirstSubscribe  func()
	onLastUnsubscribe func()

	// onSubscriberCount, if set, fires with the new subscriber count whenever
	// a subscriber is added or removed. Used by mirror sessions to report the
	// remote viewer count downstream. Fired synchronously after the lock is
	// released (to preserve count ordering); the hook must not block.
	onSubscriberCount func(int)
}

// Subscriber is a client connection's outbox.
type Subscriber struct {
	out        chan proto.Frame
	closed     chan struct{}
	once       sync.Once
	clientID   string // end-to-end ID echoed in META.driver_client_id when this sub is driver
	clientName string // human-readable name (hostname) echoed in META.driver_client_name
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
	if meta.TaskState == "" {
		meta.TaskState = proto.TaskStateIdle
	}
	if meta.Type == "" {
		meta.Type = SessionTypeShell
	}
	return &Session{
		ID:        id,
		StartedAt: time.Now(),
		meta:      meta,
		subs:      make(map[*Subscriber]struct{}),
		scroll:    ringbuf.New(scrollbackBytes),
		inbound:   make(chan proto.Frame, inboundQueueDepth),
	}
}

// SetDriverFromUpstream marks this session as a mirror whose driver is adopted
// from upstream META rather than self-assigned. Call once right after New.
func (s *Session) SetDriverFromUpstream(v bool) {
	s.mu.Lock()
	s.driverFromUpstream = v
	s.mu.Unlock()
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

// SetSubscriberCountHook registers a callback fired with the new subscriber
// count on every add/remove (synchronously, lock released — must not block).
// Replaces any previous hook.
func (s *Session) SetSubscriberCountHook(fn func(int)) {
	s.mu.Lock()
	s.onSubscriberCount = fn
	s.mu.Unlock()
}

// UpdateMeta merges new cwd/title from a META frame and, on real change,
// broadcasts a META frame that reflects the full current session state
// (driver_client_id/name + cols/rows + cwd/title). The driver fields are
// critical: a "lite" META containing only cwd would make clients read
// driver_client_id="" and re-render as viewers, even when a driver is
// active. Always go through this helper instead of constructing a META
// frame manually at the caller.
func (s *Session) UpdateMeta(m proto.MetaPayload) {
	s.mu.Lock()
	changed := false
	if m.Cwd != "" && s.meta.Cwd != m.Cwd {
		s.meta.Cwd = m.Cwd
		changed = true
	}
	if m.Title != "" && s.meta.Title != m.Title {
		s.meta.Title = m.Title
		changed = true
	}
	if mergeTaskMeta(&s.meta, m) {
		changed = true
	}
	// Mirror sessions adopt the upstream's authoritative driver. Local
	// sessions never take driver fields from a META (preserves commit #4:
	// a cwd-only update must not clobber driver state).
	if s.driverFromUpstream &&
		(s.driverClientID != m.DriverClientID || s.driverClientName != m.DriverClientName) {
		s.driverClientID = m.DriverClientID
		s.driverClientName = m.DriverClientName
		changed = true
	}
	metaCopy := s.meta
	driverID := s.driverClientID
	driverName := s.driverClientName
	s.mu.Unlock()
	if changed {
		s.broadcastDriverMeta(metaCopy, driverID, driverName)
	}
}

// UpdateCwdTitle updates cwd/title and rebroadcasts with the session's CURRENT
// driver — it never adopts a driver from the caller. Used for ANNOUNCE-driven
// cwd/title reconciliation on mirror sessions: the announce's SessionInfo
// carries no driver_client_id, so routing it through UpdateMeta (which a
// driverFromUpstream mirror treats as "adopt") would clobber the adopted
// driver to empty and flip every client back to viewer. The authoritative
// driver always arrives via the streamed META (UpdateMeta), not the announce.
func (s *Session) UpdateCwdTitle(cwd, title string) {
	s.mu.Lock()
	changed := false
	if cwd != "" && s.meta.Cwd != cwd {
		s.meta.Cwd = cwd
		changed = true
	}
	if title != "" && s.meta.Title != title {
		s.meta.Title = title
		changed = true
	}
	metaCopy := s.meta
	driverID := s.driverClientID
	driverName := s.driverClientName
	s.mu.Unlock()
	if changed {
		s.broadcastDriverMeta(metaCopy, driverID, driverName)
	}
}

// UpdateAdvertisedInfo reconciles metadata from an ANNOUNCE snapshot without
// adopting driver state. ANNOUNCEs are owner-published session facts, not
// subscriber-control facts.
func (s *Session) UpdateAdvertisedInfo(info proto.SessionInfo) {
	s.mu.Lock()
	changed := false
	if info.Cwd != "" && s.meta.Cwd != info.Cwd {
		s.meta.Cwd = info.Cwd
		changed = true
	}
	if info.Title != "" && s.meta.Title != info.Title {
		s.meta.Title = info.Title
		changed = true
	}
	if info.RemotePermission != s.meta.RemotePermission {
		s.meta.RemotePermission = info.RemotePermission
		changed = true
	}
	if mergeTaskInfo(&s.meta, info) {
		changed = true
	}
	metaCopy := s.meta
	driverID := s.driverClientID
	driverName := s.driverClientName
	s.mu.Unlock()
	if changed {
		s.broadcastDriverMeta(metaCopy, driverID, driverName)
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
// On real change, broadcasts a META frame so viewers can update their xterm
// dims to match the new PTY size.
func (s *Session) UpdateSize(cols, rows uint16) {
	s.mu.Lock()
	changed := false
	if cols > 0 && s.meta.Cols != cols {
		s.meta.Cols = cols
		changed = true
	}
	if rows > 0 && s.meta.Rows != rows {
		s.meta.Rows = rows
		changed = true
	}
	metaCopy := s.meta
	driverID := s.driverClientID
	driverName := s.driverClientName
	s.mu.Unlock()
	if changed {
		s.broadcastDriverMeta(metaCopy, driverID, driverName)
	}
}

// PushOut is called by the agent reader loop for each TypeOut frame.
// It records the chunk to scrollback and fans out to current subscribers.
// Slow subscribers are dropped (their channels are closed) so they reconnect.
// It returns true when advertised task metadata changed.
func (s *Session) PushOut(seq uint64, data []byte) bool {
	metaChanged := s.updateTerminalState(data)
	if metaChanged {
		s.broadcastCurrentMeta()
	}
	s.scroll.Push(ringbuf.Chunk{Seq: seq, Data: append([]byte(nil), data...)})
	frame := proto.EncodeOut(s.ID, seq, data)
	s.fanout(frame)
	return metaChanged
}

// Broadcast sends a frame (META/CLOSE) to all subscribers.
func (s *Session) Broadcast(f proto.Frame) {
	s.fanout(f)
}

// subsSnapshotPool reuses the temporary slice of subscribers fanout copies out
// of s.subs each call. PushOut is a hot path (every PTY OUT frame); without
// pooling, every chunk would allocate a fresh []*Subscriber proportional to
// the subscriber count.
var subsSnapshotPool = sync.Pool{
	New: func() any {
		buf := make([]*Subscriber, 0, 8)
		return &buf
	},
}

func (s *Session) fanout(f proto.Frame) {
	s.mu.RLock()
	if len(s.subs) == 0 {
		s.mu.RUnlock()
		return
	}
	bufPtr := subsSnapshotPool.Get().(*[]*Subscriber)
	subs := (*bufPtr)[:0]
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

	// Clear references before returning to the pool so the GC can reclaim
	// any *Subscriber whose only remaining root is this buffer.
	for i := range subs {
		subs[i] = nil
	}
	*bufPtr = subs[:0]
	subsSnapshotPool.Put(bufPtr)
}

// Subscribe registers a new client outbox and replays scrollback strictly
// greater than sinceSeq. When sinceSeq is 0 the full scrollback is replayed.
// clientID is the end-to-end identifier echoed in META.driver_client_id when
// this subscriber is the active driver. clientName is the human-readable
// hostname echoed in META.driver_client_name. Both may be empty.
// Returns the subscriber and the largest seq replayed.
func (s *Session) Subscribe(sinceSeq uint64, clientID, clientName string) (*Subscriber, uint64) {
	sub := &Subscriber{
		out:        make(chan proto.Frame, subscriberQueueDepth),
		closed:     make(chan struct{}),
		clientID:   clientID,
		clientName: clientName,
	}

	chunks := s.scroll.Since(sinceSeq)
	s.mu.RLock()
	altScreen := s.altScreen
	s.mu.RUnlock()
	totalBytes := replayBytes(chunks)
	_ = enqueueReplayProgress(sub, s.ID, proto.ReplayProgressStart, 0, totalBytes, 0)
	// If the replay starts after evicted bytes, the gap is unrecoverable:
	// send a soft truncation marker so a fresh xterm doesn't render a torn
	// tail of cursor-addressed output on top of an unrelated blank state.
	if replayIsTruncated(s.scroll.OldestSeq(), sinceSeq, len(chunks)) {
		marker := proto.EncodeOut(s.ID, 0, replayResetMarker(altScreen))
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
	var (
		promotedToDriver   bool
		snapshotMeta       proto.SessionInfo
		snapshotDriverID   string
		snapshotDriverName string
	)
	if !closed && enqueueReplayProgress(sub, s.ID, proto.ReplayProgressEnd, replayedBytes, totalBytes, lastSeq) {
		s.subs[sub] = struct{}{}
		added = true
		if !s.driverFromUpstream && s.driverSubscriber == nil {
			s.driverSubscriber = sub
			s.driverClientID = sub.clientID
			s.driverClientName = sub.clientName
			promotedToDriver = true
		}
		snapshotMeta = s.meta
		snapshotDriverID = s.driverClientID
		snapshotDriverName = s.driverClientName
	}
	firstHook := s.onFirstSubscribe
	countHook := s.onSubscriberCount
	subCount := len(s.subs)
	s.mu.Unlock()

	if closed || !added {
		sub.close()
		return sub, lastSeq
	}
	if promotedToDriver {
		s.broadcastDriverMeta(snapshotMeta, snapshotDriverID, snapshotDriverName)
	} else {
		s.sendSnapshotMeta(sub, snapshotMeta, snapshotDriverID, snapshotDriverName)
	}
	if wasEmpty && firstHook != nil {
		go firstHook()
	}
	// Synchronous (not goroutine) so successive counts stay ordered — a stale
	// count arriving after a newer one would mis-render the viewer badge. The
	// hook must be non-blocking (the relay's is a non-blocking enqueue).
	if countHook != nil {
		countHook(subCount)
	}
	return sub, lastSeq
}

// ClaimDriver makes sub the active driver, recording clientID and clientName
// as the end-to-end identifier and human-readable hostname respectively.
// Broadcasts a META frame with the new driver to all subscribers. No-op if
// sub is no longer registered with the session.
func (s *Session) ClaimDriver(sub *Subscriber, clientID, clientName string) {
	s.mu.Lock()
	if _, ok := s.subs[sub]; !ok {
		s.mu.Unlock()
		return
	}
	s.driverSubscriber = sub
	s.driverClientID = clientID
	s.driverClientName = clientName
	metaCopy := s.meta
	s.mu.Unlock()

	s.broadcastDriverMeta(metaCopy, clientID, clientName)
}

func (s *Session) broadcastDriverMeta(meta proto.SessionInfo, driverClientID, driverClientName string) {
	payload, err := encodeMetaPayload(meta, driverClientID, driverClientName)
	if err != nil {
		return
	}
	s.Broadcast(proto.Frame{Type: proto.TypeMeta, SessionID: s.ID, Payload: payload})
}

func (s *Session) sendSnapshotMeta(sub *Subscriber, meta proto.SessionInfo, driverClientID, driverClientName string) {
	payload, err := encodeMetaPayload(meta, driverClientID, driverClientName)
	if err != nil {
		return
	}
	select {
	case sub.out <- proto.Frame{Type: proto.TypeMeta, SessionID: s.ID, Payload: payload}:
	default:
		// channel full — next fanout will drop this slow consumer normally
	}
}

func (s *Session) broadcastCurrentMeta() {
	s.mu.RLock()
	metaCopy := s.meta
	driverID := s.driverClientID
	driverName := s.driverClientName
	s.mu.RUnlock()
	s.broadcastDriverMeta(metaCopy, driverID, driverName)
}

func encodeMetaPayload(meta proto.SessionInfo, driverClientID, driverClientName string) ([]byte, error) {
	return json.Marshal(proto.MetaPayload{
		Cwd:               meta.Cwd,
		Title:             meta.Title,
		DriverClientID:    driverClientID,
		DriverClientName:  driverClientName,
		Cols:              meta.Cols,
		Rows:              meta.Rows,
		TaskState:         meta.TaskState,
		CurrentCommand:    meta.CurrentCommand,
		CommandStartedAt:  meta.CommandStartedAt,
		CommandEndedAt:    meta.CommandEndedAt,
		CommandDurationMS: meta.CommandDurationMS,
		CommandExitCode:   meta.CommandExitCode,
		LastOutputAt:      meta.LastOutputAt,
	})
}

// DriverClientID returns the end-to-end client_id of the current driver, or
// "" if no driver is assigned.
func (s *Session) DriverClientID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.driverClientID
}

// DriverClientName returns the human-readable hostname of the current driver,
// or "" if no driver / driver didn't supply a name.
func (s *Session) DriverClientName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.driverClientName
}

// DriverFromUpstream reports whether this is a mirror session whose driver is
// dictated by upstream META (see SetDriverFromUpstream).
func (s *Session) DriverFromUpstream() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.driverFromUpstream
}

func replayIsTruncated(oldestSeq, sinceSeq uint64, chunkCount int) bool {
	if chunkCount == 0 {
		return false
	}
	if sinceSeq == 0 {
		return oldestSeq > 1
	}
	return oldestSeq > sinceSeq+1
}

func replayResetMarker(altScreen bool) []byte {
	if altScreen {
		return []byte("\x1b[?1049h\x1b[2J\x1b[H")
	}
	return []byte("\x1b[2J\x1b[H")
}

func (s *Session) updateTerminalState(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	now := time.Now()
	if s.meta.TaskState == "" {
		s.meta.TaskState = proto.TaskStateIdle
		changed = true
	}
	s.meta.LastOutputAt = now.Unix()
	if s.applyOSC133Locked(data, now) {
		changed = true
	} else if s.meta.TaskState != proto.TaskStateRunning && looksLikeWaitingInput(data) && s.meta.TaskState != proto.TaskStateWaitingInput {
		s.meta.TaskState = proto.TaskStateWaitingInput
		changed = true
	}
	const tailLen = 32
	prevTail := s.termTail
	if len(prevTail) > 0 {
		prefixLen := tailLen - len(prevTail)
		if prefixLen > len(data) {
			prefixLen = len(data)
		}
		prefix := append(append([]byte(nil), prevTail...), data[:prefixLen]...)
		s.altScreen = scanAltScreenMode(s.altScreen, prefix)
	}
	s.altScreen = scanAltScreenMode(s.altScreen, data)
	s.termTail = appendTrailingBytes(s.termTail[:0], prevTail, data, tailLen)
	return changed
}

func (s *Session) applyOSC133Locked(data []byte, now time.Time) bool {
	events := s.consumeOSC133Locked(data)
	changed := false
	for _, payload := range events {
		if payload == "" {
			continue
		}
		switch payload[0] {
		case 'C':
			command := strings.TrimSpace(strings.TrimPrefix(payload, "C;"))
			exitNil := (*int)(nil)
			if s.meta.TaskState != proto.TaskStateRunning {
				s.meta.TaskState = proto.TaskStateRunning
				changed = true
			}
			if s.meta.CurrentCommand != command {
				s.meta.CurrentCommand = command
				changed = true
			}
			// Sticky non-shell classification: shell never overwrites an
			// already-set non-shell tag (so opening `claude`, exiting back
			// to the shell prompt, and running `ls` keeps the session
			// flagged as ai). See spec §4.
			if newType := ClassifyCommand(command); newType != SessionTypeShell && s.meta.Type != newType {
				s.meta.Type = newType
				changed = true
			}
			started := now.Unix()
			s.cmdStarted = now
			if s.meta.CommandStartedAt != started {
				s.meta.CommandStartedAt = started
				changed = true
			}
			if s.meta.CommandEndedAt != 0 {
				s.meta.CommandEndedAt = 0
				changed = true
			}
			if s.meta.CommandDurationMS != 0 {
				s.meta.CommandDurationMS = 0
				changed = true
			}
			if s.meta.CommandExitCode != nil {
				s.meta.CommandExitCode = exitNil
				changed = true
			}
		case 'D':
			if s.meta.TaskState != proto.TaskStateRunning && s.meta.CommandStartedAt == 0 {
				continue
			}
			exitCode := parseOSC133Exit(payload)
			state := proto.TaskStateCompleted
			if exitCode != 0 {
				state = proto.TaskStateFailed
			}
			if s.meta.TaskState != state {
				s.meta.TaskState = state
				changed = true
			}
			ended := now.Unix()
			if s.meta.CommandEndedAt != ended {
				s.meta.CommandEndedAt = ended
				changed = true
			}
			startedAt := s.cmdStarted
			if startedAt.IsZero() {
				startedAt = time.Unix(s.meta.CommandStartedAt, 0)
			}
			duration := int(now.Sub(startedAt).Milliseconds())
			if duration < 0 {
				duration = 0
			}
			if s.meta.CommandDurationMS != duration {
				s.meta.CommandDurationMS = duration
				changed = true
			}
			if s.meta.CommandExitCode == nil || *s.meta.CommandExitCode != exitCode {
				v := exitCode
				s.meta.CommandExitCode = &v
				changed = true
			}
		}
	}
	return changed
}

func (s *Session) consumeOSC133Locked(data []byte) []string {
	buf := append(append([]byte(nil), s.osc133Buf...), data...)
	s.osc133Buf = s.osc133Buf[:0]
	var out []string
	prefix := []byte("\x1b]133;")
	for {
		idx := bytes.Index(buf, prefix)
		if idx < 0 {
			s.osc133Buf = keepOSC133Tail(s.osc133Buf, buf)
			return out
		}
		buf = buf[idx+len(prefix):]
		termStart, termLen := oscTerminator(buf)
		if termStart < 0 {
			// Keep the incomplete OSC so a split BEL/ST terminator can finish it
			// on the next chunk. Cap the buffer to avoid unbounded growth on
			// malformed terminal output.
			incomplete := append(prefix, buf...)
			const maxOSC = 4096
			if len(incomplete) > maxOSC {
				incomplete = incomplete[len(incomplete)-maxOSC:]
			}
			s.osc133Buf = append(s.osc133Buf, incomplete...)
			return out
		}
		out = append(out, string(buf[:termStart]))
		buf = buf[termStart+termLen:]
	}
}

func keepOSC133Tail(dst, buf []byte) []byte {
	const keep = len("\x1b]133;") - 1
	if len(buf) > keep {
		buf = buf[len(buf)-keep:]
	}
	return append(dst, buf...)
}

func oscTerminator(buf []byte) (int, int) {
	for i, b := range buf {
		if b == 0x07 {
			return i, 1
		}
		if b == 0x1b && i+1 < len(buf) && buf[i+1] == '\\' {
			return i, 2
		}
	}
	return -1, 0
}

func parseOSC133Exit(payload string) int {
	idx := strings.IndexByte(payload, ';')
	if idx < 0 {
		return 0
	}
	raw := strings.TrimSpace(payload[idx+1:])
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return -1
	}
	return n
}

func looksLikeWaitingInput(data []byte) bool {
	text := strings.ToLower(string(data))
	for _, pattern := range []string{
		"[y/n]",
		"continue?",
		"proceed?",
		"confirm",
		"press enter",
		"password:",
	} {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

func mergeTaskMeta(meta *proto.SessionInfo, m proto.MetaPayload) bool {
	info := proto.SessionInfo{
		TaskState:         m.TaskState,
		CurrentCommand:    m.CurrentCommand,
		CommandStartedAt:  m.CommandStartedAt,
		CommandEndedAt:    m.CommandEndedAt,
		CommandDurationMS: m.CommandDurationMS,
		CommandExitCode:   m.CommandExitCode,
		LastOutputAt:      m.LastOutputAt,
	}
	return mergeTaskInfo(meta, info)
}

func mergeTaskInfo(meta *proto.SessionInfo, info proto.SessionInfo) bool {
	if !hasTaskInfo(info) {
		return false
	}
	changed := false
	if info.TaskState != "" && meta.TaskState != info.TaskState {
		meta.TaskState = info.TaskState
		changed = true
	}
	if info.CurrentCommand != meta.CurrentCommand {
		meta.CurrentCommand = info.CurrentCommand
		changed = true
	}
	if info.CommandStartedAt != 0 && meta.CommandStartedAt != info.CommandStartedAt {
		meta.CommandStartedAt = info.CommandStartedAt
		changed = true
	}
	if info.CommandEndedAt != meta.CommandEndedAt {
		meta.CommandEndedAt = info.CommandEndedAt
		changed = true
	}
	if info.CommandDurationMS != meta.CommandDurationMS {
		meta.CommandDurationMS = info.CommandDurationMS
		changed = true
	}
	if !sameOptionalInt(meta.CommandExitCode, info.CommandExitCode) {
		meta.CommandExitCode = cloneOptionalInt(info.CommandExitCode)
		changed = true
	}
	if info.LastOutputAt != 0 && meta.LastOutputAt != info.LastOutputAt {
		meta.LastOutputAt = info.LastOutputAt
		changed = true
	}
	return changed
}

func hasTaskInfo(info proto.SessionInfo) bool {
	return info.TaskState != "" ||
		info.CurrentCommand != "" ||
		info.CommandStartedAt != 0 ||
		info.CommandEndedAt != 0 ||
		info.CommandDurationMS != 0 ||
		info.CommandExitCode != nil ||
		info.LastOutputAt != 0
}

func sameOptionalInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func cloneOptionalInt(v *int) *int {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}

func scanAltScreenMode(alt bool, data []byte) bool {
	for i := 0; i+2 < len(data); i++ {
		if data[i] != 0x1b || data[i+1] != '[' {
			continue
		}
		j := i + 2
		if j >= len(data) || data[j] != '?' {
			continue
		}
		j++
		paramsStart := j
		for j < len(data) && (data[j] < 0x40 || data[j] > 0x7e) {
			j++
		}
		if j >= len(data) {
			break
		}
		final := data[j]
		if final == 'h' || final == 'l' {
			if csiParamsContainAltScreen(data[paramsStart:j]) {
				alt = final == 'h'
			}
		}
		i = j
	}
	return alt
}

func csiParamsContainAltScreen(params []byte) bool {
	value := 0
	haveDigits := false
	for _, b := range params {
		if b >= '0' && b <= '9' {
			value = value*10 + int(b-'0')
			haveDigits = true
			continue
		}
		if haveDigits && isAltScreenMode(value) {
			return true
		}
		value = 0
		haveDigits = false
	}
	return haveDigits && isAltScreenMode(value)
}

func isAltScreenMode(value int) bool {
	return value == 47 || value == 1047 || value == 1049
}

func appendTrailingBytes(dst, prev, data []byte, max int) []byte {
	if len(data) >= max {
		return append(dst, data[len(data)-max:]...)
	}
	combined := append(append([]byte(nil), prev...), data...)
	if len(combined) > max {
		combined = combined[len(combined)-max:]
	}
	return append(dst, combined...)
}

func replayBytes(chunks []ringbuf.Chunk) uint64 {
	var total uint64
	for _, c := range chunks {
		total += uint64(len(c.Data))
	}
	return total
}

func enqueueReplayChunks(sub *Subscriber, id uuid.UUID, chunks []ringbuf.Chunk, lastSeq, replayedBytes, totalBytes, nextProgress uint64) (uint64, uint64, uint64, bool) {
	var (
		batchData []byte
		batchSeq  uint64
	)
	flush := func() bool {
		if len(batchData) == 0 {
			return true
		}
		select {
		case sub.out <- proto.EncodeOut(id, batchSeq, batchData):
			lastSeq = batchSeq
			replayedBytes += uint64(len(batchData))
			batchData = nil
			batchSeq = 0
		default:
			return false
		}
		for replayedBytes >= nextProgress && nextProgress > 0 {
			if !enqueueReplayProgress(sub, id, proto.ReplayProgressChunk, replayedBytes, totalBytes, lastSeq) {
				return false
			}
			nextProgress += replayProgressIntervalBytes
		}
		return true
	}
	for _, c := range chunks {
		// Flush the current batch before appending if combining would push it
		// past the target — keeps large chunks as their own frame so existing
		// progress-bytes math stays per-chunk-boundary aligned.
		if len(batchData) > 0 && len(batchData)+len(c.Data) > replayCoalesceTargetBytes {
			if !flush() {
				return lastSeq, replayedBytes, nextProgress, false
			}
		}
		batchData = append(batchData, c.Data...)
		batchSeq = c.Seq
		if len(batchData) >= replayCoalesceTargetBytes {
			if !flush() {
				return lastSeq, replayedBytes, nextProgress, false
			}
		}
	}
	if !flush() {
		return lastSeq, replayedBytes, nextProgress, false
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
	wasDriver := s.driverSubscriber == sub
	if wasDriver {
		s.driverSubscriber = nil
		s.driverClientID = ""
		s.driverClientName = ""
	}
	nowEmpty := len(s.subs) == 0
	lastHook := s.onLastUnsubscribe
	countHook := s.onSubscriberCount
	subCount := len(s.subs)
	metaCopy := s.meta
	s.mu.Unlock()
	sub.close()
	if wasDriver {
		s.broadcastDriverMeta(metaCopy, "", "")
	}
	if was && nowEmpty && lastHook != nil {
		go lastHook()
	}
	if was && countHook != nil {
		countHook(subCount)
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

// IsDriver reports whether sub is currently the session driver.
func (s *Session) IsDriver(sub *Subscriber) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.driverSubscriber == sub
}
