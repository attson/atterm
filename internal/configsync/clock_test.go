package configsync

import (
	"errors"
	"testing"
	"time"
)

func TestClockTickObserveAndSkew(t *testing.T) {
	now := time.UnixMilli(1_800_000_000_000)
	clock := NewClockWithSource(func() time.Time { return now }, 5*time.Minute)

	first := clock.Tick()
	second := clock.Tick()
	if first != (Timestamp{PhysicalMS: now.UnixMilli()}) || second != (Timestamp{PhysicalMS: now.UnixMilli(), Logical: 1}) {
		t.Fatalf("unexpected local ticks: first=%+v second=%+v", first, second)
	}

	observed, err := clock.Observe(Timestamp{PhysicalMS: now.Add(time.Minute).UnixMilli(), Logical: 4})
	if err != nil {
		t.Fatal(err)
	}
	if observed != (Timestamp{PhysicalMS: now.Add(time.Minute).UnixMilli(), Logical: 5}) {
		t.Fatalf("observed = %+v", observed)
	}

	beforeRejected := observed
	if got, err := clock.Observe(Timestamp{PhysicalMS: now.Add(6 * time.Minute).UnixMilli()}); !errors.Is(err, ErrClockSkew) || got != beforeRejected {
		t.Fatalf("future timestamp: got=%+v err=%v", got, err)
	}

	now = now.Add(2 * time.Minute)
	if next := clock.Tick(); CompareTimestamp(next, observed) <= 0 {
		t.Fatalf("clock did not remain monotonic: next=%+v observed=%+v", next, observed)
	}
}

func TestCompareTimestamp(t *testing.T) {
	cases := []struct {
		a, b Timestamp
		want int
	}{
		{Timestamp{1, 0}, Timestamp{2, 0}, -1},
		{Timestamp{2, 0}, Timestamp{1, 99}, 1},
		{Timestamp{2, 1}, Timestamp{2, 2}, -1},
		{Timestamp{2, 2}, Timestamp{2, 2}, 0},
	}
	for _, tc := range cases {
		if got := CompareTimestamp(tc.a, tc.b); got != tc.want {
			t.Fatalf("CompareTimestamp(%+v, %+v) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
