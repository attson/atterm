package relay

import (
	"sync"
	"sync/atomic"

	"github.com/attson/atterm/internal/userstore"
)

type directMetrics struct {
	attempts     atomic.Uint64
	successes    atomic.Uint64
	fallbacks    atomic.Uint64
	bytesAvoided atomic.Uint64

	mu      sync.Mutex
	pending map[string]*userstore.DirectTrafficDelta
}

type directMetricsSnapshot struct {
	Attempts     uint64
	Successes    uint64
	Fallbacks    uint64
	BytesAvoided uint64
}

func newDirectMetrics() *directMetrics {
	return &directMetrics{pending: make(map[string]*userstore.DirectTrafficDelta)}
}

func (m *directMetrics) cellLocked(userID string) *userstore.DirectTrafficDelta {
	cell := m.pending[userID]
	if cell == nil {
		cell = &userstore.DirectTrafficDelta{UserID: userID}
		m.pending[userID] = cell
	}
	return cell
}

func (m *directMetrics) recordAttempt(userID string) {
	m.attempts.Add(1)
	if userID == "" {
		return
	}
	m.mu.Lock()
	m.cellLocked(userID).Attempts++
	m.mu.Unlock()
}

func (m *directMetrics) recordSuccess(userID string) {
	m.successes.Add(1)
	if userID == "" {
		return
	}
	m.mu.Lock()
	m.cellLocked(userID).Successes++
	m.mu.Unlock()
}

func (m *directMetrics) recordFallback(userID string) {
	m.fallbacks.Add(1)
	if userID == "" {
		return
	}
	m.mu.Lock()
	m.cellLocked(userID).Fallbacks++
	m.mu.Unlock()
}

func (m *directMetrics) recordBytes(userID string, sent, received uint64) {
	m.bytesAvoided.Add(sent + received)
	if userID == "" {
		return
	}
	m.mu.Lock()
	cell := m.cellLocked(userID)
	cell.BytesSent += int64(sent)
	cell.BytesReceived += int64(received)
	m.mu.Unlock()
}

func (m *directMetrics) drain() []userstore.DirectTrafficDelta {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]userstore.DirectTrafficDelta, 0, len(m.pending))
	for _, cell := range m.pending {
		out = append(out, *cell)
	}
	m.pending = make(map[string]*userstore.DirectTrafficDelta)
	return out
}

func (m *directMetrics) snapshot() directMetricsSnapshot {
	return directMetricsSnapshot{
		Attempts:     m.attempts.Load(),
		Successes:    m.successes.Load(),
		Fallbacks:    m.fallbacks.Load(),
		BytesAvoided: m.bytesAvoided.Load(),
	}
}
