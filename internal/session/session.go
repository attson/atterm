// Package session is the relay-side authoritative model for a live PTY session.
//
// One Session per live agent connection: it holds the scrollback ring, the
// metadata snapshot, the fan-out subscriber set, and the inbound channel that
// carries client-side IN/RESIZE frames back to the agent.
package session

import (
	"bytes"
	"encoding/json"
	"fmt"
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

	mu        sync.RWMutex
	meta      proto.SessionInfo
	closed    bool
	subs      map[*Subscriber]struct{}
	scroll    *ringbuf.Buffer
	inbound   chan proto.Frame // bounded; client -> agent
	altScreen bool
	termTail  []byte
	osc133Buf []byte
	// contentOpaque is the E2EE bypass flag. When set, PushOut treats the
	// session's OUT bytes as opaque ciphertext and skips OSC 133 parsing /
	// Summary accumulation. Set by uplink_conn the first time it sees the
	// e2eecrypto cipher_id prefix on an inbound chunk. See MarkContentOpaque.
	contentOpaque bool
	// oscTitleBuf retains the tail of any unfinished OSC 0/1/2 (icon/window
	// title) sequence so a terminator that lands on an Append boundary still
	// parses. Independent from osc133Buf to keep the two parsers from
	// tripping each other.
	oscTitleBuf []byte
	cmdStarted  time.Time

	// Silence-detection heuristic state. silenceTimer is armed by
	// rescheduleSilenceTimerLocked whenever the session is in running + alt
	// screen; on fire onSilenceFired re-checks guards and flips to
	// waiting_input. waitingFromSilence remembers that the current
	// waiting_input came from this heuristic (so output arriving while in
	// waiting_input restores running only for us, never undoing a
	// looksLikeWaitingInput match). silenceThresholdMS and silenceDetectEnabled
	// are populated from env at New() and cached per session so tests can
	// inject short thresholds via t.Setenv.
	silenceTimer         *time.Timer
	waitingFromSilence   bool
	silenceThresholdMS   int64
	silenceDetectEnabled bool

	// silenceRestoreBytes accumulates len(data) seen on OUT while we are
	// in heuristic waiting_input. Restoring to running only triggers once
	// the accumulator passes silenceRestoreByteThreshold — without this,
	// tiny cursor-blink / spinner redraws emitted by TUIs (claude, codex,
	// etc.) keep flipping waiting_input back to running every few seconds
	// and the sidebar visibly oscillates. Reset on any non-restore state
	// transition (Close, OSC 133, external UpdateMeta).
	silenceRestoreBytes         int64
	silenceRestoreByteThreshold int64
	silenceActivityBytes        int64
	silenceActivityStartedMono  time.Time

	// lastResizeMono stamps the monotonic time of the last UpdateSize call
	// that actually changed cols/rows. PTY resize → SIGWINCH → TUIs
	// (claude, codex, vim, htop, …) repaint the entire screen, which can
	// be several KB of OUT in the next instant. Without a grace window
	// those repaint bytes blow past silenceRestoreByteThreshold and
	// (incorrectly) restore running — so collapsing/expanding the desktop
	// sidebar visibly knocks idle AI sessions back to the running spinner.
	// Output arriving within silenceResizeGraceMS of a size change does
	// not count toward the restore accumulator.
	lastResizeMono       time.Time
	silenceResizeGraceMS int64

	// lastOutputMono is a monotonic wall-clock stamp of the last meaningful
	// output for silence detection. LastOutputAt still records every OUT
	// chunk for display; this field deliberately ignores isolated tiny TUI
	// redraws so idle Codex/Claude prompts can settle to waiting_input.
	// It uses monotonic time because LastOutputAt is unix-seconds, which
	// silently rounds sub-second silenceThresholdMS values up to the next
	// integer-second boundary.
	lastOutputMono time.Time

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

	// onMetaChanged, if set, fires after any spontaneous state change that
	// originates inside Session (not in response to an inbound frame the
	// caller already follows up with NotifyChange). The silence heuristic
	// uses this so its async timer-driven waiting_input transitions reach
	// the session-list subscribers. The hook runs OUTSIDE the lock.
	onMetaChanged func()

	// onAIClassified fires once per type→ai transition in applyOSC133Locked.
	// Receives the OSC 133;C command line payload (stripped of "C;") and the
	// session's current cwd at fire time. Desktop uses it to spawn the AI
	// sid sniffer for fresh sessions (restored sessions get sniff via
	// NewSession's req.AIKind path).
	onAIClassified func(commandLine, cwd string)

	// aiCommandFinished records the exact boundary where an AI CLI command has
	// emitted OSC 133;D. If the next OSC 133;C is a plain shell command, the
	// session can safely leave the sticky ai type without treating AI-internal
	// input as a shell command.
	aiCommandFinished bool

	// onFirstPrompt fires once on the first OSC 133;A/B prompt marker — i.e.
	// the shell has drawn its first prompt and is ready to accept input.
	// Desktop uses it to inject a resume command into a restored AI pane.
	// Runs while s.mu is held — keep it non-blocking.
	onFirstPrompt    func()
	firstPromptFired bool

	// onTaskStateChange fires once per task-state transition observed within
	// the session. The callback runs while the session mutex is held —
	// implementers must NOT call back into Session methods that take s.mu.
	onTaskStateChange func(sid uuid.UUID, prev, next string, meta TaskMeta)
}

// TaskMeta carries the fields the OnTaskStateChange caller needs that
// are NOT already in proto.SessionInfo. Right now this is just enough
// for the desktop dispatcher to render Feishu cards. Add fields as
// new use cases need them.
type TaskMeta struct {
	ExitCode   int
	ElapsedMS  int
	Label      string
	SealedBody []byte
	// RecentOutput is the command's tail output, already ANSI-stripped and
	// line-limited by computeSummary (the same summary clients render).
	// Empty for non-D transitions and for content-opaque (E2EE) sessions.
	RecentOutput string
}

// Subscriber is a client connection's outbox.
type Subscriber struct {
	out        chan proto.Frame
	closed     chan struct{}
	once       sync.Once
	clientID   string // end-to-end ID echoed in META.driver_client_id when this sub is driver
	clientName string // human-readable name (hostname) echoed in META.driver_client_name
	// noAutoDrive, when set, keeps Subscribe from auto-promoting this subscriber
	// to driver when the session has no driver. Set for the desktop uplink's
	// proxy subscriber, which fans out to remote viewers: a remote becomes
	// driver only via an explicit CLAIM_DRIVER, never because the uplink
	// (re)subscribed. ClaimDriver still promotes it.
	noAutoDrive bool
}

// SubscribeOption configures a subscription created by Subscribe.
type SubscribeOption func(*Subscriber)

// WithoutAutoDrive marks the subscriber as a passive proxy/fan-out that must
// not be auto-promoted to driver when the session currently has no driver. The
// driver role can still be assigned to it explicitly via ClaimDriver.
func WithoutAutoDrive() SubscribeOption {
	return func(sub *Subscriber) { sub.noAutoDrive = true }
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
		ID:                          id,
		StartedAt:                   time.Now(),
		meta:                        meta,
		subs:                        make(map[*Subscriber]struct{}),
		scroll:                      ringbuf.New(scrollbackBytes),
		inbound:                     make(chan proto.Frame, inboundQueueDepth),
		silenceThresholdMS:          envSilenceThresholdMS(),
		silenceDetectEnabled:        envSilenceDetectEnabled(),
		silenceRestoreByteThreshold: envSilenceRestoreByteThreshold(),
		silenceResizeGraceMS:        envSilenceResizeGraceMS(),
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

// TailOutput returns up to the last n bytes of scrollback. Used to attach a
// short output summary to Feishu cards. Returns nil when n <= 0 or empty.
func (s *Session) TailOutput(n int) []byte {
	return s.scroll.TailBytes(n)
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

// SetMetaChangedHook registers a callback fired after Session-driven async
// state transitions (currently: silence-heuristic flips). The hook runs
// outside the lock. Typical use: have the registry owner pass
// `registry.NotifyChange` so the session list view picks up the new state.
func (s *Session) SetMetaChangedHook(fn func()) {
	s.mu.Lock()
	s.onMetaChanged = fn
	s.mu.Unlock()
}

// SetOnAIClassified registers a callback invoked once when the session's
// type transitions to ai via OSC 133;C ClassifyCommand. The callback runs
// while s.mu is held — keep it non-blocking (typically `go startSniff(...)`).
func (s *Session) SetOnAIClassified(fn func(commandLine, cwd string)) {
	s.mu.Lock()
	s.onAIClassified = fn
	s.mu.Unlock()
}

// SetOnFirstPrompt registers a callback fired once when the shell emits its
// first OSC 133 prompt marker (ready for input). The callback runs while s.mu
// is held — keep it non-blocking (typically `go ...`).
func (s *Session) SetOnFirstPrompt(fn func()) {
	s.mu.Lock()
	s.onFirstPrompt = fn
	s.mu.Unlock()
}

// SetOnTaskStateChange registers a callback that fires on every
// task-state transition observed inside the session.
func (s *Session) SetOnTaskStateChange(fn func(sid uuid.UUID, prev, next string, meta TaskMeta)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onTaskStateChange = fn
}

// SubscriberCount returns the number of currently-attached subscribers.
func (s *Session) SubscriberCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.subs)
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
	prevTaskState := s.meta.TaskState
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
	if prevTaskState != proto.TaskStateRunning && s.meta.TaskState == proto.TaskStateRunning {
		s.markSilenceActivityLocked(time.Now())
	}
	if s.waitingFromSilence && s.meta.TaskState != proto.TaskStateWaitingInput {
		s.waitingFromSilence = false
		s.silenceRestoreBytes = 0
		s.resetSilenceActivityBurstLocked()
	}
	s.rescheduleSilenceTimerLocked()
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
	prevTaskState := s.meta.TaskState
	if info.Cwd != "" && s.meta.Cwd != info.Cwd {
		s.meta.Cwd = info.Cwd
		changed = true
	}
	if info.Title != "" && s.meta.Title != info.Title {
		s.meta.Title = info.Title
		changed = true
	}
	// Preserve the sealed envelope so /api/sessions serving a mirror
	// session returns the same title/cwd/command payload the agent
	// uploaded. Without this the mirror's meta.Sealed stays nil and
	// remote clients (mobile HTTP fetch, web) see empty content
	// fields, since the agent strips the plaintext before ANNOUNCE.
	// Empty inbound Sealed is ignored — matches the title/cwd
	// non-empty guard: an ANNOUNCE without Sealed shouldn't clobber a
	// previously-set envelope.
	if len(info.Sealed) > 0 && !bytes.Equal(s.meta.Sealed, info.Sealed) {
		s.meta.Sealed = append([]byte(nil), info.Sealed...)
		changed = true
	}
	if info.RemotePermission != s.meta.RemotePermission {
		s.meta.RemotePermission = info.RemotePermission
		changed = true
	}
	if mergeTaskInfo(&s.meta, info) {
		changed = true
	}
	if prevTaskState != proto.TaskStateRunning && s.meta.TaskState == proto.TaskStateRunning {
		s.markSilenceActivityLocked(time.Now())
	}
	if s.waitingFromSilence && s.meta.TaskState != proto.TaskStateWaitingInput {
		s.waitingFromSilence = false
		s.silenceRestoreBytes = 0
		s.resetSilenceActivityBurstLocked()
	}
	s.rescheduleSilenceTimerLocked()
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
	if changed {
		// SIGWINCH will follow; record the moment so updateTerminalState
		// can discount the repaint bytes in its silence-restore accumulator.
		s.lastResizeMono = time.Now()
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
//
// When the session is marked content-opaque (see MarkContentOpaque), the
// OSC 133 parser is bypassed — the data bytes are treated as ciphertext
// the relay is structurally unable to read. Task state, exit codes, and
// Summary continue to come from agent-supplied META events instead.
func (s *Session) PushOut(seq uint64, data []byte) bool {
	var metaChanged bool
	if !s.isContentOpaque() {
		metaChanged = s.updateTerminalState(data)
		if metaChanged {
			s.broadcastCurrentMeta()
		}
	}
	s.scroll.Push(ringbuf.Chunk{Seq: seq, Data: append([]byte(nil), data...)})
	frame := proto.EncodeOut(s.ID, seq, data)
	s.fanout(frame)
	return metaChanged
}

// MarkContentOpaque permanently flags this Session as carrying encrypted
// OUT bytes. PushOut stops invoking the OSC 133 parser; ringbuf storage
// and subscriber fan-out continue unchanged (the relay still routes the
// bytes, it just stops trying to read them). Idempotent; never unwinds.
//
// Called by uplink_conn the first time an inbound OUT chunk carries the
// e2eecrypto cipher_id prefix. Once set, the agent could send plaintext
// chunks mid-session (theoretically — would never happen in v1 since the
// agent locks the decision per-uplink-connection) and the relay would
// still treat the session as opaque. That bias toward "stay opaque" is
// safer than oscillating.
func (s *Session) MarkContentOpaque() {
	s.mu.Lock()
	s.contentOpaque = true
	s.mu.Unlock()
}

func (s *Session) isContentOpaque() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.contentOpaque
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
func (s *Session) Subscribe(sinceSeq uint64, clientID, clientName string, opts ...SubscribeOption) (*Subscriber, uint64) {
	sub := &Subscriber{
		out:        make(chan proto.Frame, subscriberQueueDepth),
		closed:     make(chan struct{}),
		clientID:   clientID,
		clientName: clientName,
	}
	for _, opt := range opts {
		opt(sub)
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
		if !s.driverFromUpstream && s.driverSubscriber == nil && !sub.noAutoDrive {
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
		prevStateW := s.meta.TaskState
		s.meta.TaskState = proto.TaskStateWaitingInput
		s.meta.AttentionAt = now.Unix()
		changed = true
		s.fireTaskStateLocked(prevStateW, proto.TaskStateWaitingInput, TaskMeta{})
	}
	// OSC 0/1/2 title scan is independent of OSC 133 and the waiting-input
	// heuristic. Same Append can carry both; the downstream
	// broadcastCurrentMeta covers both flags via the single `changed` path.
	if s.applyOSCTitleLocked(data) {
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

	// Silence heuristic: output arriving while we were heuristic-waiting
	// restores running, BUT only once we've accumulated enough output to
	// be confident this isn't just a TUI cursor blink / spinner redraw.
	// AttentionAt is intentionally NOT rolled back (2026-06-07 spec §6).
	// Existing keyword-based waiting_input is left alone because
	// waitingFromSilence == false.
	restoredFromSilence := false
	if s.waitingFromSilence && s.meta.TaskState == proto.TaskStateWaitingInput {
		inResizeGrace := !s.lastResizeMono.IsZero() &&
			now.Sub(s.lastResizeMono) < time.Duration(s.silenceResizeGraceMS)*time.Millisecond
		switch {
		case inResizeGrace:
			// SIGWINCH-driven repaint after a sidebar collapse / window
			// resize / font change. Don't count these bytes; reset the
			// accumulator so we start fresh once the grace window closes.
			s.silenceRestoreBytes = 0
			silenceDebugLocked(s, fmt.Sprintf("restore-skip-resize-grace: chunk=%d age=%v",
				len(data), now.Sub(s.lastResizeMono)))
		default:
			s.silenceRestoreBytes += int64(len(data))
			if s.silenceRestoreBytes >= s.silenceRestoreByteThreshold {
				silenceDebugLocked(s, fmt.Sprintf("restore: state waiting_input -> running (bytes=%d >= threshold=%d)",
					s.silenceRestoreBytes, s.silenceRestoreByteThreshold))
				s.meta.TaskState = proto.TaskStateRunning
				s.waitingFromSilence = false
				s.silenceRestoreBytes = 0
				s.markSilenceActivityLocked(now)
				changed = true
				restoredFromSilence = true
			} else {
				silenceDebugLocked(s, fmt.Sprintf("restore-defer: bytes=%d < threshold=%d (chunk=%d)",
					s.silenceRestoreBytes, s.silenceRestoreByteThreshold, len(data)))
			}
		}
	}
	if !restoredFromSilence {
		s.noteRunningSilenceActivityLocked(data, now)
	}
	// Always reschedule at the tail. The helper itself decides whether to
	// arm based on the post-update state + altScreen + detect-enabled flag.
	s.rescheduleSilenceTimerLocked()
	var metaHook func()
	if restoredFromSilence {
		metaHook = s.onMetaChanged
	}
	s.mu.Unlock()
	if metaHook != nil {
		// Fire OUT-OF-BAND so the session list view picks up running again
		// after silence restored. The caller (PushOut) will also broadcast
		// the OUT bytes, but the registry NotifyChange path is what kicks
		// list-only subscribers (e.g. the desktop sidebar).
		metaHook()
	}
	return changed
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
	if s.silenceTimer != nil {
		s.silenceTimer.Stop()
		s.silenceTimer = nil
	}
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

// fireTaskStateLocked invokes onTaskStateChange while s.mu is already
// held. Skips when state didn't change or no callback is registered.
func (s *Session) fireTaskStateLocked(prev, next string, meta TaskMeta) {
	if prev == next || s.onTaskStateChange == nil {
		return
	}
	s.onTaskStateChange(s.ID, prev, next, meta)
}
