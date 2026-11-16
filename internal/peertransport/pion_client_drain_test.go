package peertransport

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

type drainTestDataChannel struct {
	buffered atomic.Uint64
	closed   atomic.Bool
}

func (d *drainTestDataChannel) BufferedAmount() uint64 { return d.buffered.Load() }
func (d *drainTestDataChannel) ReadyState() webrtc.DataChannelState {
	if d.closed.Load() {
		return webrtc.DataChannelStateClosed
	}
	return webrtc.DataChannelStateOpen
}

func TestPionClientDrainWaitsForEmptyQueueAndAllowsControlSends(t *testing.T) {
	dc := &drainTestDataChannel{}
	dc.buffered.Store(100)
	var sendMu sync.Mutex
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- drainClientDataChannel(ctx, ctx, dc, &sendMu, time.Second) }()
	select {
	case err := <-done:
		t.Fatalf("drain completed with queued bytes: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if !sendMu.TryLock() {
		t.Fatal("drain blocked control sends while waiting for the queue")
	}
	sendMu.Unlock()
	dc.buffered.Store(0)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPionClientDrainWaitsForPriorSendAndCancelsWhileItIsLocked(t *testing.T) {
	var sendMu sync.Mutex
	sendMu.Lock()
	defer sendMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := drainClientDataChannel(ctx, context.Background(), &drainTestDataChannel{}, &sendMu, time.Second); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("drain while prior SendFrame holds lock: %v", err)
	}
}

func TestPionClientDrainRejectsCancellationAndClosedChannelEvenWhenEmpty(t *testing.T) {
	for _, test := range []string{"caller", "attempt", "closed"} {
		t.Run(test, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			attemptCtx, cancelAttempt := context.WithCancel(context.Background())
			defer cancelAttempt()
			dc := &drainTestDataChannel{}
			switch test {
			case "caller":
				cancel()
			case "attempt":
				cancelAttempt()
			case "closed":
				dc.closed.Store(true)
			}
			err := drainClientDataChannel(ctx, attemptCtx, dc, &sync.Mutex{}, time.Second)
			if err == nil || test == "caller" && !errors.Is(err, context.Canceled) || test != "caller" && !errors.Is(err, ErrDirectTransport) {
				t.Fatalf("drain error=%v", err)
			}
		})
	}
}

func TestPionClientDrainRejectsStalledQueue(t *testing.T) {
	dc := &drainTestDataChannel{}
	dc.buffered.Store(1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := drainClientDataChannel(ctx, ctx, dc, &sync.Mutex{}, 20*time.Millisecond); !errors.Is(err, ErrDirectTransport) {
		t.Fatalf("stalled drain error=%v", err)
	}
}

func TestPionClientDrainResetsStallTimeoutOnProgress(t *testing.T) {
	dc := &drainTestDataChannel{}
	dc.buffered.Store(4)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		for remaining := uint64(3); ; remaining-- {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			dc.buffered.Store(remaining)
			if remaining == 0 {
				return
			}
		}
	}()
	started := time.Now()
	if err := drainClientDataChannel(ctx, ctx, dc, &sync.Mutex{}, 150*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 150*time.Millisecond {
		t.Fatal("drain did not wait beyond a single stall timeout")
	}
}

func TestPionClientDrainRejectsUnauthenticatedChannel(t *testing.T) {
	for _, channel := range []*PionClientChannel{nil, {}} {
		if err := channel.Drain(context.Background()); !errors.Is(err, ErrDirectTransport) {
			t.Fatalf("unauthenticated drain error=%v", err)
		}
	}
}
