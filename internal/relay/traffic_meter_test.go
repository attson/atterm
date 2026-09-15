package relay

import (
	"sync"
	"testing"

	"github.com/attson/atterm/internal/proto"
)

func TestTrafficMeterAddAndSnapshot(t *testing.T) {
	m := newTrafficMeter()
	m.add("u1", proto.TypeOut, trafficOut, 100)
	m.add("u1", proto.TypeOut, trafficOut, 50)  // same key accumulates
	m.add("u1", proto.TypeIn, trafficIn, 10)    // different direction
	m.add("u2", proto.TypeMeta, trafficOut, 30) // different user
	m.add("", proto.TypeOut, trafficOut, 999)   // empty user is dropped

	got := m.snapshot()
	type k struct {
		u       string
		ft, dir int
	}
	sum := map[k]int64{}
	frames := map[k]int64{}
	for _, d := range got {
		sum[k{d.UserID, d.FrameType, d.Direction}] += d.Bytes
		frames[k{d.UserID, d.FrameType, d.Direction}] += d.Frames
	}
	if sum[k{"u1", int(proto.TypeOut), trafficOut}] != 150 {
		t.Errorf("u1 OUT bytes = %d, want 150", sum[k{"u1", int(proto.TypeOut), trafficOut}])
	}
	if sum[k{"u1", int(proto.TypeIn), trafficIn}] != 10 {
		t.Errorf("u1 IN bytes = %d, want 10", sum[k{"u1", int(proto.TypeIn), trafficIn}])
	}
	if sum[k{"u2", int(proto.TypeMeta), trafficOut}] != 30 {
		t.Errorf("u2 META bytes = %d, want 30", sum[k{"u2", int(proto.TypeMeta), trafficOut}])
	}
	if _, ok := sum[k{"", int(proto.TypeOut), trafficOut}]; ok {
		t.Error("empty userID should be dropped")
	}
	if frames[k{"u1", int(proto.TypeOut), trafficOut}] != 2 {
		t.Errorf("u1 OUT frames = %d, want 2", frames[k{"u1", int(proto.TypeOut), trafficOut}])
	}
}

func TestTrafficMeterSnapshotClearsIncrement(t *testing.T) {
	m := newTrafficMeter()
	m.add("u1", proto.TypeOut, trafficOut, 100)
	_ = m.snapshot()
	if got := m.snapshot(); len(got) != 0 {
		t.Errorf("second snapshot len = %d, want 0", len(got))
	}
}

func TestTrafficMeterConcurrentAdd(t *testing.T) {
	m := newTrafficMeter()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.add("u1", proto.TypeOut, trafficOut, 1)
		}()
	}
	wg.Wait()
	var total int64
	for _, d := range m.snapshot() {
		total += d.Bytes
	}
	if total != 100 {
		t.Errorf("concurrent add total = %d, want 100", total)
	}
}
