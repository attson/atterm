package relay

import "sync/atomic"

type directMetrics struct {
	attempts     atomic.Uint64
	successes    atomic.Uint64
	fallbacks    atomic.Uint64
	bytesAvoided atomic.Uint64
}

type directMetricsSnapshot struct {
	Attempts     uint64
	Successes    uint64
	Fallbacks    uint64
	BytesAvoided uint64
}

func (m *directMetrics) snapshot() directMetricsSnapshot {
	return directMetricsSnapshot{
		Attempts:     m.attempts.Load(),
		Successes:    m.successes.Load(),
		Fallbacks:    m.fallbacks.Load(),
		BytesAvoided: m.bytesAvoided.Load(),
	}
}
