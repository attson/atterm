package relay

import (
	"context"
	"sync"
	"time"

	"nhooyr.io/websocket"

	"github.com/attson/atterm/internal/logging"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/userstore"
)

const (
	trafficIn  = 0
	trafficOut = 1
)

// trafficDelta is one accumulated (user, frame type, direction) bucket
// drained from a trafficMeter snapshot.
type trafficDelta struct {
	UserID    string
	FrameType int
	Direction int
	Bytes     int64
	Frames    int64
}

type trafficKey struct {
	userID string
	ft     int
	dir    int
}

type trafficCell struct {
	bytes  int64
	frames int64
}

// trafficMeter accumulates per-(user, frame type, direction) byte and frame
// counts in memory. It is written on the hot send/receive paths (add) and
// drained on a fixed interval (snapshot). A single mutex is sufficient:
// snapshot runs at flush cadence (~60s), so lock contention on add is
// negligible relative to the WebSocket I/O it accompanies.
type trafficMeter struct {
	mu    sync.Mutex
	cells map[trafficKey]*trafficCell
}

func newTrafficMeter() *trafficMeter {
	return &trafficMeter{cells: make(map[trafficKey]*trafficCell)}
}

// add records wireBytes for one frame. A zero-length userID (pre-auth /
// anonymous frames) is dropped: traffic is only meaningful per account.
func (m *trafficMeter) add(userID string, t proto.Type, dir int, wireBytes int) {
	if userID == "" {
		return
	}
	k := trafficKey{userID: userID, ft: int(t), dir: dir}
	m.mu.Lock()
	c := m.cells[k]
	if c == nil {
		c = &trafficCell{}
		m.cells[k] = c
	}
	c.bytes += int64(wireBytes)
	c.frames++
	m.mu.Unlock()
}

// snapshot returns all accumulated deltas and resets the meter. The caller
// (flush loop) is responsible for persisting them; a failed persist loses
// at most one interval's data (accepted trade-off, see spec §Error handling).
func (m *trafficMeter) snapshot() []trafficDelta {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]trafficDelta, 0, len(m.cells))
	for k, c := range m.cells {
		out = append(out, trafficDelta{
			UserID:    k.userID,
			FrameType: k.ft,
			Direction: k.dir,
			Bytes:     c.bytes,
			Frames:    c.frames,
		})
	}
	m.cells = make(map[trafficKey]*trafficCell)
	return out
}

// trafficFlushInterval is how often accumulated traffic is folded into the
// daily rollup. A crash loses at most this window (see spec §Error handling).
const trafficFlushInterval = 60 * time.Second

// trafficFlushLoop periodically drains the meter into the daily rollup, and
// does a final drain on stop. Started by NewServer only when a store exists.
func (s *Server) trafficFlushLoop(store userstore.Store) {
	defer close(s.flushDone)
	t := time.NewTicker(trafficFlushInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.flushTraffic(store)
		case <-s.flushStop:
			s.flushTraffic(store) // final drain
			return
		}
	}
}

// flushTraffic snapshots the meter and folds the deltas into the current UTC
// day's rollup. A failed persist logs a warning and drops the window's data
// (accepted trade-off; the snapshot has already reset the meter).
func (s *Server) flushTraffic(store userstore.Store) {
	deltas := s.traffic.snapshot()
	directDeltas := s.directStats.drain()
	if len(deltas) == 0 && len(directDeltas) == 0 {
		return
	}
	day := time.Now().UTC().Format("2006-01-02")
	conv := make([]userstore.TrafficDelta, len(deltas))
	for i, d := range deltas {
		conv[i] = userstore.TrafficDelta{
			UserID:    d.UserID,
			FrameType: d.FrameType,
			Direction: d.Direction,
			Bytes:     d.Bytes,
			Frames:    d.Frames,
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if len(conv) > 0 {
		if err := store.AddTrafficDeltas(ctx, day, conv); err != nil {
			logging.Warn("relay-traffic", "flush failed, dropping %d deltas: %v", len(conv), err)
		}
	}
	if len(directDeltas) > 0 {
		if err := store.AddDirectTrafficDeltas(ctx, day, directDeltas); err != nil {
			logging.Warn("relay-traffic", "direct flush failed, dropping %d deltas: %v", len(directDeltas), err)
		}
	}
}

// Close stops the traffic flush loop and drains a final snapshot. Called from
// the binary's shutdown path. Safe to call when the loop never started (no
// store) and idempotent-safe for a single call.
func (s *Server) Close() {
	if s.flushStop != nil {
		close(s.flushStop)
		<-s.flushDone
	}
}

// recordTraffic folds one frame's wire size into the meter. Never blocks the
// caller beyond a mutex-guarded map write; a zero userID is dropped.
func (s *Server) recordTraffic(userID string, t proto.Type, dir int, wireBytes int) {
	s.traffic.add(userID, t, dir, wireBytes)
}

// writeFrame marshals f, writes it as one binary WS message, and — on a
// successful write — records its wire size against userID for traffic
// accounting. It centralizes the previously inlined
// c.Write(ctx, MessageBinary, proto.Marshal(f)) calls for the simple client
// write paths so every outbound frame is metered at one place.
func (s *Server) writeFrame(ctx context.Context, userID string, c *websocket.Conn, f proto.Frame) error {
	b := proto.Marshal(f)
	if err := c.Write(ctx, websocket.MessageBinary, b); err != nil {
		return err
	}
	s.recordTraffic(userID, f.Type, trafficOut, len(b))
	return nil
}
