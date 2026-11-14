package peertransport

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPionClientBackpressureRejectsStalledHighWaterQueue(t *testing.T) {
	dc := &drainTestDataChannel{}
	dc.buffered.Store(directBufferedHighWater + 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitClientDataChannel(ctx, ctx, dc, nil, directBufferedHighWater, 20*time.Millisecond); !errors.Is(err, ErrDirectTransport) {
		t.Fatalf("stalled enqueue error=%v", err)
	}
}

func TestPionClientBackpressureAllowsSlowProgressUntilHighWater(t *testing.T) {
	dc := &drainTestDataChannel{}
	dc.buffered.Store(directBufferedHighWater + 4)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		for remaining := uint64(3); ; remaining-- {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			dc.buffered.Store(directBufferedHighWater + remaining)
			if remaining == 0 {
				return
			}
		}
	}()
	started := time.Now()
	if err := waitClientDataChannel(ctx, ctx, dc, nil, directBufferedHighWater, 150*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 150*time.Millisecond {
		t.Fatal("enqueue did not wait beyond a single stall timeout")
	}
	if dc.BufferedAmount() != directBufferedHighWater {
		t.Fatal("enqueue waited for the entire queue instead of the high-water threshold")
	}
}

func TestPionClientBackpressureRejectsCancellationAndClosure(t *testing.T) {
	for _, event := range []string{"caller", "attempt", "closed"} {
		t.Run(event, func(t *testing.T) {
			dc := &drainTestDataChannel{}
			dc.buffered.Store(directBufferedHighWater + 1)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			attemptCtx, cancelAttempt := context.WithCancel(context.Background())
			defer cancelAttempt()
			done := make(chan error, 1)
			go func() {
				done <- waitClientDataChannel(ctx, attemptCtx, dc, nil, directBufferedHighWater, time.Second)
			}()
			switch event {
			case "caller":
				cancel()
			case "attempt":
				cancelAttempt()
			case "closed":
				dc.closed.Store(true)
			}
			err := <-done
			if err == nil || event == "caller" && !errors.Is(err, context.Canceled) || event != "caller" && !errors.Is(err, ErrDirectTransport) {
				t.Fatalf("enqueue error=%v", err)
			}
		})
	}
}

func TestPionClientBackpressureDoesNotDelaySmallInput(t *testing.T) {
	dc := &drainTestDataChannel{}
	dc.buffered.Store(1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitClientDataChannel(ctx, ctx, dc, nil, directBufferedHighWater, time.Second); err != nil {
		t.Fatalf("small input enqueue error=%v", err)
	}
}
