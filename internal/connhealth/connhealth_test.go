package connhealth

import (
	"testing"
	"time"
)

func TestTracker_InitialSnapshot(t *testing.T) {
	tr := New()
	s := tr.Snapshot(time.Unix(0, 0))
	if s.State != StateClosed {
		t.Fatalf("initial state = %q, want %q", s.State, StateClosed)
	}
	if s.RTT.LastMS != nil {
		t.Fatalf("initial RTT non-nil")
	}
	if s.Reconnect.CountLastHour != 0 {
		t.Fatalf("initial reconnect count = %d", s.Reconnect.CountLastHour)
	}
}

func TestTracker_RecordsRTTAndComputesPercentiles(t *testing.T) {
	tr := New()
	tr.SetState(StateConnected, time.Unix(0, 0))
	for i, ms := range []int{50, 60, 70, 80, 90, 100, 110, 120, 130, 140} {
		tr.OnPongRTT(ms, time.Unix(int64(i), 0))
	}
	s := tr.Snapshot(time.Unix(10, 0))
	if got := *s.RTT.LastMS; got != 140 {
		t.Fatalf("last RTT = %d, want 140", got)
	}
	if got := *s.RTT.P50MS; got < 90 || got > 100 {
		t.Fatalf("p50 = %d, want 90..100", got)
	}
	if got := *s.RTT.P95MS; got < 130 || got > 140 {
		t.Fatalf("p95 = %d, want 130..140", got)
	}
	if len(s.RTTSamples) != 10 {
		t.Fatalf("samples len = %d, want 10", len(s.RTTSamples))
	}
}

func TestTracker_RTTRingEvictsOldest(t *testing.T) {
	tr := New()
	tr.SetState(StateConnected, time.Unix(0, 0))
	for i := 0; i < RTTRingSize+5; i++ {
		tr.OnPongRTT(i, time.Unix(int64(i), 0))
	}
	s := tr.Snapshot(time.Unix(0, 0))
	if len(s.RTTSamples) != RTTRingSize {
		t.Fatalf("ring size = %d, want %d", len(s.RTTSamples), RTTRingSize)
	}
	// Oldest sample should be i=5 (we wrote 65 items into a 60-slot ring).
	first := s.RTTSamples[0]
	if first.RTTMS != 5 {
		t.Fatalf("ring[0].rtt = %d, want 5", first.RTTMS)
	}
}

func TestTracker_StateTransitionsLogReconnects(t *testing.T) {
	tr := New()
	tr.SetState(StateConnecting, time.Unix(0, 0))
	tr.SetState(StateConnected, time.Unix(1, 0))
	tr.SetState(StateReconnecting, time.Unix(60, 0))
	tr.SetReconnectReason("ws_close_1006", time.Unix(60, 0))
	tr.SetState(StateConnected, time.Unix(62, 0))
	s := tr.Snapshot(time.Unix(120, 0))
	if s.Reconnect.CountLastHour != 1 {
		t.Fatalf("reconnect count = %d, want 1", s.Reconnect.CountLastHour)
	}
	if len(s.Reconnect.History) != 1 {
		t.Fatalf("history len = %d", len(s.Reconnect.History))
	}
	h := s.Reconnect.History[0]
	if h.Reason != "ws_close_1006" {
		t.Fatalf("reason = %q", h.Reason)
	}
	if h.DurationMS != 2000 {
		t.Fatalf("downtime = %d ms, want 2000", h.DurationMS)
	}
}

func TestTracker_ReconnectCountWindowsAtOneHour(t *testing.T) {
	tr := New()
	tr.SetState(StateConnected, time.Unix(0, 0))
	// Two reconnects ~ at t=100/200s (outside the 1h window from t=5500).
	tr.SetState(StateReconnecting, time.Unix(100, 0))
	tr.SetState(StateConnected, time.Unix(101, 0))
	tr.SetState(StateReconnecting, time.Unix(200, 0))
	tr.SetState(StateConnected, time.Unix(201, 0))
	// One inside the window.
	tr.SetState(StateReconnecting, time.Unix(5000, 0))
	tr.SetState(StateConnected, time.Unix(5001, 0))
	s := tr.Snapshot(time.Unix(5500, 0))
	if s.Reconnect.CountLastHour != 1 {
		t.Fatalf("reconnect count = %d, want 1 (5000s is inside, 100/200s are outside)", s.Reconnect.CountLastHour)
	}
}

func TestTracker_BytesEMA(t *testing.T) {
	tr := New()
	tr.SetState(StateConnected, time.Unix(0, 0))
	// 1000 bytes/s for 30 seconds (so the EMA has time to converge).
	for i := 0; i < 30; i++ {
		tr.OnBytesIn(1000, time.Unix(int64(i), 0))
		tr.OnBytesOut(500, time.Unix(int64(i), 0))
		tr.Tick(time.Unix(int64(i+1), 0)) // tick at end of bucket
	}
	s := tr.Snapshot(time.Unix(30, 0))
	if s.Bytes.InPerSec < 800 || s.Bytes.InPerSec > 1100 {
		t.Fatalf("in_per_sec = %d, want ~1000", s.Bytes.InPerSec)
	}
	if s.Bytes.OutPerSec < 400 || s.Bytes.OutPerSec > 550 {
		t.Fatalf("out_per_sec = %d, want ~500", s.Bytes.OutPerSec)
	}
}

func TestTracker_SeqGapsCounter(t *testing.T) {
	tr := New()
	tr.OnSeqGap()
	tr.OnSeqGap()
	tr.OnSeqGap()
	if got := tr.Snapshot(time.Unix(0, 0)).SeqGaps; got != 3 {
		t.Fatalf("seq_gaps = %d, want 3", got)
	}
}
