package quicktunnel

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/attson/atterm/internal/peertransport"
)

const wssMaxQueuedRequests = 64

type wssPriority byte

const (
	wssPriorityControl wssPriority = iota
	wssPriorityTerminal
	wssPriorityConfig
)

// WSSFallbackConfig binds application callbacks before a client explicitly
// requests fallback. A non-nil config means the caller permits this route.
type WSSFallbackConfig struct {
	OnAuthenticated func(*WSSChannel)
	OnRecord        func(peertransport.RecordKind, []byte)
	OnConfigMessage func(peertransport.RecordKind, []byte) error
}

// WSSChannel carries the existing encrypted terminal/config record protocol on
// the authenticated Quick Tunnel WebSocket when WebRTC cannot be established.
type WSSChannel struct {
	signal           *SignalChannel
	role             signalChannelRole
	remoteMembership string
	onRecord         func(peertransport.RecordKind, []byte)
	onConfigMessage  func(peertransport.RecordKind, []byte) error
	writer           *wssWriter
	nextMessageID    atomic.Uint64
	receiveMu        sync.Mutex
	reassembler      peertransport.Reassembler
	configAssembler  peertransport.ConfigReassembler
}

// BindWSSFallback declares support or user consent without changing the active
// route. It must happen before the client sends the fallback request.
func (c *SignalChannel) BindWSSFallback(cfg WSSFallbackConfig) error {
	if c == nil || c.conn == nil || c.sealer == nil || c.opener == nil {
		return fmt.Errorf("%w: invalid WSS fallback channel", peertransport.ErrDirectTransport)
	}
	select {
	case <-c.done:
		return fmt.Errorf("%w: WSS fallback channel is closed", peertransport.ErrDirectTransport)
	default:
	}
	c.fallbackMu.Lock()
	defer c.fallbackMu.Unlock()
	if c.fallbackCfg != nil || c.fallback != nil {
		return fmt.Errorf("%w: WSS fallback already bound", peertransport.ErrDirectTransport)
	}
	copyCfg := cfg
	c.fallbackCfg = &copyCfg
	return nil
}

// StartWSSFallback explicitly switches a client channel from WebRTC signaling
// to terminal/config records. Hosts switch only after receiving this request.
func (c *SignalChannel) StartWSSFallback(ctx context.Context) (*WSSChannel, error) {
	if c == nil || c.role != signalChannelClient {
		return nil, fmt.Errorf("%w: only a client can request WSS fallback", peertransport.ErrDirectTransport)
	}
	if c.activeWSSFallback() != nil {
		return nil, fmt.Errorf("%w: WSS fallback already active", peertransport.ErrDirectTransport)
	}
	if !c.hasWSSFallbackBinding() {
		return nil, fmt.Errorf("%w: WSS fallback is not permitted", peertransport.ErrDirectTransport)
	}
	if err := c.SendSignal(ctx, Signal{Type: SignalWSSFallback}); err != nil {
		return nil, err
	}
	select {
	case <-c.fallbackDone:
		channel := c.activeWSSFallback()
		if channel == nil {
			return nil, fmt.Errorf("%w: WSS fallback activation lost", peertransport.ErrDirectTransport)
		}
		return channel, nil
	case <-ctx.Done():
		_ = c.Close()
		return nil, ctx.Err()
	case <-c.done:
		return nil, fmt.Errorf("%w: WSS fallback closed before ready", peertransport.ErrDirectTransport)
	}
}

func (c *SignalChannel) hasWSSFallbackBinding() bool {
	c.fallbackMu.Lock()
	defer c.fallbackMu.Unlock()
	return c.fallbackCfg != nil
}

func (c *SignalChannel) activeWSSFallback() *WSSChannel {
	if c == nil {
		return nil
	}
	c.fallbackMu.Lock()
	defer c.fallbackMu.Unlock()
	return c.fallback
}

func (c *SignalChannel) setFallbackCloser(closePion func()) {
	c.fallbackMu.Lock()
	c.fallbackClose = closePion
	active := c.fallback != nil
	c.fallbackMu.Unlock()
	if active && closePion != nil {
		go closePion()
	}
}

func (c *SignalChannel) acknowledgeWSSFallback() error {
	if c == nil || c.role != signalChannelHost || !c.hasWSSFallbackBinding() {
		return fmt.Errorf("%w: WSS fallback is not supported", peertransport.ErrDirectTransport)
	}
	ctx, cancel := context.WithTimeout(context.Background(), pionSignalWriteTimeout)
	defer cancel()
	if err := c.SendSignal(ctx, Signal{Type: SignalWSSReady}); err != nil {
		return err
	}
	_, err := c.activateWSSFallback()
	return err
}

func (c *SignalChannel) activateWSSFallback() (*WSSChannel, error) {
	c.fallbackMu.Lock()
	if c.fallback != nil {
		channel := c.fallback
		c.fallbackMu.Unlock()
		return channel, nil
	}
	if c.fallbackCfg == nil {
		c.fallbackMu.Unlock()
		return nil, fmt.Errorf("%w: WSS fallback is not permitted", peertransport.ErrDirectTransport)
	}
	cfg := *c.fallbackCfg
	channel := &WSSChannel{
		signal: c, role: c.role, remoteMembership: c.remoteMembership,
		onRecord: cfg.OnRecord, onConfigMessage: cfg.OnConfigMessage,
	}
	channel.writer = newWSSWriter(c.done, channel.writeRecord)
	c.fallback = channel
	close(c.fallbackDone)
	closePion := c.fallbackClose
	c.fallbackMu.Unlock()

	go channel.writer.run()
	if cfg.OnAuthenticated != nil {
		cfg.OnAuthenticated(channel)
	}
	if closePion != nil {
		go closePion()
	}
	return channel, nil
}

func (c *WSSChannel) writeRecord(ctx context.Context, kind peertransport.RecordKind, plaintext []byte) error {
	if c == nil || c.signal == nil {
		return fmt.Errorf("%w: WSS fallback closed", peertransport.ErrDirectTransport)
	}
	c.signal.writeMu.Lock()
	defer c.signal.writeMu.Unlock()
	select {
	case <-c.signal.done:
		return fmt.Errorf("%w: WSS fallback closed", peertransport.ErrDirectTransport)
	default:
	}
	if c.signal.activeWSSFallback() != c {
		return fmt.Errorf("%w: WSS fallback inactive", peertransport.ErrDirectTransport)
	}
	return c.signal.writeRecord(ctx, kind, plaintext)
}

func (c *WSSChannel) receive(kind peertransport.RecordKind, plaintext []byte) error {
	c.receiveMu.Lock()
	defer c.receiveMu.Unlock()
	if kind == peertransport.RecordFragment {
		frame, complete, err := c.reassembler.Add(plaintext, time.Now())
		if err != nil || !complete {
			return err
		}
		kind = peertransport.RecordFrame
		plaintext = frame
	}
	if kind == peertransport.RecordConfigFragment {
		configKind, message, complete, err := c.configAssembler.Add(plaintext, time.Now())
		if err != nil || !complete {
			return err
		}
		kind = configKind
		plaintext = message
	}
	if kind.IsConfigMessage() {
		if c.onConfigMessage != nil {
			if err := c.onConfigMessage(kind, append([]byte(nil), plaintext...)); err != nil {
				return fmt.Errorf("%w: WSS config message: %w", peertransport.ErrDirectTransport, err)
			}
		}
		return nil
	}
	if c.onRecord != nil {
		c.onRecord(kind, append([]byte(nil), plaintext...))
	}
	return nil
}

// SendRecord queues one control record at the highest WSS priority.
func (c *WSSChannel) SendRecord(ctx context.Context, kind peertransport.RecordKind, plaintext []byte) error {
	if c == nil || c.writer == nil || !kind.IsDataMessage() || kind == peertransport.RecordFragment ||
		kind == peertransport.RecordConfigFragment || kind.IsConfigMessage() || len(plaintext) > peertransport.MaxRecordPlaintext {
		return fmt.Errorf("%w: invalid WSS control record", peertransport.ErrDirectTransport)
	}
	return c.writer.send(ctx, wssPriorityControl, []wssPlainRecord{{kind: kind, payload: append([]byte(nil), plaintext...)}})
}

// SendFrame queues a marshaled terminal frame. Client input/control frames are
// high priority; host output is below control and above config replication.
func (c *WSSChannel) SendFrame(ctx context.Context, frame []byte) error {
	if c == nil || c.writer == nil || len(frame) > peertransport.MaxFrameSize {
		return fmt.Errorf("%w: invalid WSS terminal frame", peertransport.ErrDirectTransport)
	}
	records, err := c.frameRecords(frame)
	if err != nil {
		return err
	}
	priority := wssPriorityTerminal
	if c.role == signalChannelClient {
		priority = wssPriorityControl
	}
	return c.writer.send(ctx, priority, records)
}

// SendConfigMessage queues one anti-entropy message at the lowest WSS
// priority, fragmenting it independently from terminal frames.
func (c *WSSChannel) SendConfigMessage(ctx context.Context, kind peertransport.RecordKind, message []byte) error {
	if c == nil || c.writer == nil || !kind.IsConfigMessage() || len(message) > peertransport.MaxConfigMessageSize {
		return fmt.Errorf("%w: invalid WSS config message", peertransport.ErrDirectTransport)
	}
	records, err := c.configRecords(kind, message)
	if err != nil {
		return err
	}
	return c.writer.send(ctx, wssPriorityConfig, records)
}

func (c *WSSChannel) frameRecords(frame []byte) ([]wssPlainRecord, error) {
	if len(frame) <= peertransport.MaxRecordPlaintext {
		return []wssPlainRecord{{kind: peertransport.RecordFrame, payload: append([]byte(nil), frame...)}}, nil
	}
	fragments, err := peertransport.FragmentFrame(c.nextMessageID.Add(1), frame)
	if err != nil {
		return nil, err
	}
	return wssRecords(peertransport.RecordFragment, fragments), nil
}

func (c *WSSChannel) configRecords(kind peertransport.RecordKind, message []byte) ([]wssPlainRecord, error) {
	if len(message) <= peertransport.MaxRecordPlaintext {
		return []wssPlainRecord{{kind: kind, payload: append([]byte(nil), message...)}}, nil
	}
	fragments, err := peertransport.FragmentConfigMessage(kind, c.nextMessageID.Add(1), message)
	if err != nil {
		return nil, err
	}
	return wssRecords(peertransport.RecordConfigFragment, fragments), nil
}

func wssRecords(kind peertransport.RecordKind, payloads [][]byte) []wssPlainRecord {
	records := make([]wssPlainRecord, len(payloads))
	for index := range payloads {
		records[index] = wssPlainRecord{kind: kind, payload: payloads[index]}
	}
	return records
}

// RemoteMembershipToken returns the exact membership from the signaling
// handshake; callers cannot substitute another config-sync principal.
func (c *WSSChannel) RemoteMembershipToken() (string, bool) {
	if c == nil || c.remoteMembership == "" {
		return "", false
	}
	return c.remoteMembership, true
}

func (c *WSSChannel) Close() error {
	if c == nil || c.signal == nil {
		return nil
	}
	return c.signal.Close()
}

type wssPlainRecord struct {
	kind    peertransport.RecordKind
	payload []byte
}

type wssSendRequest struct {
	ctx      context.Context
	records  []wssPlainRecord
	next     int
	priority wssPriority
	done     chan error
}

type wssWriter struct {
	done   <-chan struct{}
	write  func(context.Context, peertransport.RecordKind, []byte) error
	slots  chan struct{}
	notify chan struct{}

	mu      sync.Mutex
	stopped bool
	queues  [3][]*wssSendRequest
}

func newWSSWriter(done <-chan struct{}, write func(context.Context, peertransport.RecordKind, []byte) error) *wssWriter {
	return &wssWriter{
		done: done, write: write, slots: make(chan struct{}, wssMaxQueuedRequests), notify: make(chan struct{}, 1),
	}
}

func (w *wssWriter) send(ctx context.Context, priority wssPriority, records []wssPlainRecord) error {
	result, err := w.enqueue(ctx, priority, records)
	if err != nil {
		return err
	}
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-w.done:
		return fmt.Errorf("%w: WSS fallback closed", peertransport.ErrDirectTransport)
	}
}

func (w *wssWriter) enqueue(ctx context.Context, priority wssPriority, records []wssPlainRecord) (<-chan error, error) {
	if w == nil || w.write == nil || priority > wssPriorityConfig || len(records) == 0 {
		return nil, fmt.Errorf("%w: invalid WSS write", peertransport.ErrDirectTransport)
	}
	select {
	case w.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-w.done:
		return nil, fmt.Errorf("%w: WSS fallback closed", peertransport.ErrDirectTransport)
	}
	request := &wssSendRequest{ctx: ctx, records: records, priority: priority, done: make(chan error, 1)}
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		<-w.slots
		return nil, fmt.Errorf("%w: WSS writer stopped", peertransport.ErrDirectTransport)
	}
	w.queues[priority] = append(w.queues[priority], request)
	w.mu.Unlock()
	w.wake()
	return request.done, nil
}

func (w *wssWriter) run() {
	for {
		request := w.pop()
		if request == nil {
			select {
			case <-w.done:
				w.stop(fmt.Errorf("%w: WSS fallback closed", peertransport.ErrDirectTransport))
				return
			case <-w.notify:
				continue
			}
		}
		if err := request.ctx.Err(); err != nil {
			w.complete(request, err)
			continue
		}
		record := request.records[request.next]
		if err := w.write(request.ctx, record.kind, record.payload); err != nil {
			w.complete(request, err)
			continue
		}
		request.next++
		if request.next == len(request.records) {
			w.complete(request, nil)
			continue
		}
		w.requeue(request)
	}
}

func (w *wssWriter) pop() *wssSendRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	for priority := wssPriorityControl; priority <= wssPriorityConfig; priority++ {
		queue := w.queues[priority]
		if len(queue) == 0 {
			continue
		}
		request := queue[0]
		copy(queue, queue[1:])
		queue[len(queue)-1] = nil
		w.queues[priority] = queue[:len(queue)-1]
		return request
	}
	return nil
}

func (w *wssWriter) requeue(request *wssSendRequest) {
	w.mu.Lock()
	queue := w.queues[request.priority]
	queue = append(queue, nil)
	copy(queue[1:], queue[:len(queue)-1])
	queue[0] = request
	w.queues[request.priority] = queue
	w.mu.Unlock()
	w.wake()
}

func (w *wssWriter) complete(request *wssSendRequest, err error) {
	request.done <- err
	<-w.slots
}

func (w *wssWriter) wake() {
	select {
	case w.notify <- struct{}{}:
	default:
	}
}

func (w *wssWriter) stop(err error) {
	w.mu.Lock()
	w.stopped = true
	var pending []*wssSendRequest
	for priority := range w.queues {
		pending = append(pending, w.queues[priority]...)
		w.queues[priority] = nil
	}
	w.mu.Unlock()
	for _, request := range pending {
		w.complete(request, err)
	}
}
