package relay

import (
	"sync"

	"github.com/attson/atterm/internal/proto"
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
