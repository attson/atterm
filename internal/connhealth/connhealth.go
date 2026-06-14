// Package connhealth tracks the health of a single WebSocket connection to
// the relay. A Tracker is fed RTT samples, byte counters, state transitions,
// and seq-gap events from the conn's read/write loops, and exposes an
// immutable Snapshot consumable by UI code (via Wails RPC on desktop, via a
// Vue ref on web/mobile).
//
// The Tracker is intentionally pure: no I/O, no goroutines, no timers. All
// time inputs are passed in so tests can drive it deterministically.
package connhealth

import (
	"sort"
	"sync"
	"time"
)

const (
	// RTTRingSize is the number of RTT samples retained. At one sample
	// every 5s, this is a 5-minute sliding window.
	RTTRingSize = 60

	// ReconnectHistorySize is the number of recent reconnect events kept
	// for the drawer's "Recent reconnects" table.
	ReconnectHistorySize = 5

	// ReconnectWindowMS is the rolling window for CountLastHour.
	ReconnectWindowMS = 60 * 60 * 1000

	// bytesEMAAlpha is the smoothing factor for the 1-Hz byte EMAs.
	bytesEMAAlpha = 0.2
)

type State string

const (
	StateClosed       State = "closed"
	StateConnecting   State = "connecting"
	StateConnected    State = "connected"
	StateReconnecting State = "reconnecting"
)

type RTTSample struct {
	AtMS  int64 `json:"at_ms"`
	RTTMS int   `json:"rtt_ms"`
}

type ReconnectEvent struct {
	AtMS       int64  `json:"at_ms"`
	Reason     string `json:"reason"`
	DurationMS int64  `json:"duration_ms"`
}

type RTTStats struct {
	LastMS *int `json:"last_ms"`
	P50MS  *int `json:"p50_ms"`
	P95MS  *int `json:"p95_ms"`
}

type ReconnectStats struct {
	CountLastHour int              `json:"count_last_hour"`
	LastAtMS      *int64           `json:"last_at_ms"`
	LastReason    string           `json:"last_reason"`
	History       []ReconnectEvent `json:"history"`
}

type ByteStats struct {
	InPerSec  int64 `json:"in_per_sec"`
	OutPerSec int64 `json:"out_per_sec"`
}

type Snapshot struct {
	State      State          `json:"state"`
	RTT        RTTStats       `json:"rtt"`
	RTTSamples []RTTSample    `json:"rtt_samples"`
	Reconnect  ReconnectStats `json:"reconnect"`
	Bytes      ByteStats      `json:"bytes"`
	SeqGaps    int            `json:"seq_gaps"`
}

type Tracker struct {
	mu sync.Mutex

	state      State
	stateSince time.Time

	rttRing   [RTTRingSize]RTTSample
	rttCount  int  // total samples ever written (for ring index)
	rttFilled bool // becomes true once rttCount >= RTTRingSize

	reconnects       []ReconnectEvent
	pendingReason    string
	pendingReconnect bool
	reconnectStartAt time.Time

	bytesInBucket  int64
	bytesOutBucket int64
	bytesBucketSec int64
	bytesInPerSec  float64
	bytesOutPerSec float64

	seqGaps int
}

func New() *Tracker {
	return &Tracker{state: StateClosed}
}

// SetState transitions to s at now. The caller is responsible for calling
// SetReconnectReason BEFORE SetState(StateReconnecting) if a reason is known.
func (t *Tracker) SetState(s State, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state == s {
		return
	}
	switch {
	case s == StateReconnecting:
		t.pendingReconnect = true
		t.reconnectStartAt = now
	case t.pendingReconnect && s == StateConnected:
		ev := ReconnectEvent{
			AtMS:       t.reconnectStartAt.UnixMilli(),
			Reason:     t.pendingReason,
			DurationMS: now.Sub(t.reconnectStartAt).Milliseconds(),
		}
		t.reconnects = append(t.reconnects, ev)
		if len(t.reconnects) > ReconnectHistorySize {
			t.reconnects = t.reconnects[len(t.reconnects)-ReconnectHistorySize:]
		}
		t.pendingReconnect = false
		t.pendingReason = ""
	}
	t.state = s
	t.stateSince = now
}

// SetReconnectReason records why the next reconnect happened. Call this from
// the WS close handler (with the mapped reason string) before SetState.
func (t *Tracker) SetReconnectReason(reason string, _ time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pendingReason = reason
}

// OnPongRTT records an RTT sample (ms). Caller should only call this when
// the matching ping's payload round-tripped successfully.
func (t *Tracker) OnPongRTT(rttMS int, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	idx := t.rttCount % RTTRingSize
	t.rttRing[idx] = RTTSample{AtMS: now.UnixMilli(), RTTMS: rttMS}
	t.rttCount++
	if t.rttCount >= RTTRingSize {
		t.rttFilled = true
	}
}

// OnBytesIn / OnBytesOut accumulate per-second buckets. The caller MUST call
// Tick(now) once per second (or more frequently — Tick is idempotent within
// the same second).
func (t *Tracker) OnBytesIn(n int, now time.Time)  { t.addBytes(int64(n), 0, now) }
func (t *Tracker) OnBytesOut(n int, now time.Time) { t.addBytes(0, int64(n), now) }

func (t *Tracker) addBytes(in, out int64, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	sec := now.Unix()
	if sec != t.bytesBucketSec {
		t.rolloverBuckets(sec)
	}
	t.bytesInBucket += in
	t.bytesOutBucket += out
}

// Tick rolls forward the per-second EMA. Safe to call from a 1 Hz timer.
func (t *Tracker) Tick(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	sec := now.Unix()
	if sec != t.bytesBucketSec {
		t.rolloverBuckets(sec)
	}
}

func (t *Tracker) rolloverBuckets(newSec int64) {
	// Apply EMA with the previous bucket's totals, then reset.
	t.bytesInPerSec = bytesEMAAlpha*float64(t.bytesInBucket) + (1-bytesEMAAlpha)*t.bytesInPerSec
	t.bytesOutPerSec = bytesEMAAlpha*float64(t.bytesOutBucket) + (1-bytesEMAAlpha)*t.bytesOutPerSec
	t.bytesInBucket = 0
	t.bytesOutBucket = 0
	t.bytesBucketSec = newSec
}

// OnSeqGap increments the seq-gap counter. The caller observes TypeOut frames
// and increments when the new seq is more than 1 past the last seen.
func (t *Tracker) OnSeqGap() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seqGaps++
}

// Snapshot returns an immutable view at the given wall-clock now.
func (t *Tracker) Snapshot(now time.Time) Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := Snapshot{
		State:      t.state,
		RTTSamples: []RTTSample{},
		Reconnect:  ReconnectStats{History: []ReconnectEvent{}},
	}
	s.SeqGaps = t.seqGaps
	s.Bytes.InPerSec = int64(t.bytesInPerSec)
	s.Bytes.OutPerSec = int64(t.bytesOutPerSec)

	samples := t.orderedRTTLocked()
	if len(samples) > 0 {
		s.RTTSamples = samples
		last := samples[len(samples)-1].RTTMS
		s.RTT.LastMS = &last
		sorted := make([]int, len(samples))
		for i, sm := range samples {
			sorted[i] = sm.RTTMS
		}
		sort.Ints(sorted)
		p50 := percentile(sorted, 50)
		p95 := percentile(sorted, 95)
		s.RTT.P50MS = &p50
		s.RTT.P95MS = &p95
	}

	nowMS := now.UnixMilli()
	cutoff := nowMS - ReconnectWindowMS
	count := 0
	for _, ev := range t.reconnects {
		if ev.AtMS >= cutoff {
			count++
		}
	}
	s.Reconnect.CountLastHour = count
	if len(t.reconnects) > 0 {
		s.Reconnect.History = append([]ReconnectEvent(nil), t.reconnects...)
		last := t.reconnects[len(t.reconnects)-1]
		s.Reconnect.LastAtMS = &last.AtMS
		s.Reconnect.LastReason = last.Reason
	}
	return s
}

func (t *Tracker) orderedRTTLocked() []RTTSample {
	if !t.rttFilled {
		return append([]RTTSample(nil), t.rttRing[:t.rttCount]...)
	}
	start := t.rttCount % RTTRingSize
	out := make([]RTTSample, 0, RTTRingSize)
	out = append(out, t.rttRing[start:]...)
	out = append(out, t.rttRing[:start]...)
	return out
}

func percentile(sorted []int, p int) int {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	// Nearest-rank.
	idx := (p * len(sorted)) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
