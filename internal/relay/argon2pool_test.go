package relay

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestArgon2Pool_DummyHashUsesCurrentParams verifies that the pool generates
// a dummy hash at startup using the current SEC-2 argon2id parameters.
func TestArgon2Pool_DummyHashUsesCurrentParams(t *testing.T) {
	pool := NewArgon2Pool(runtime.NumCPU())
	h := pool.DummyHash()

	// Must be an argon2id hash with our fixed parameters: m=65536, t=3, p=2.
	const prefix = "$argon2id$v=19$m=65536,t=3,p=2$"
	if !strings.HasPrefix(h, prefix) {
		t.Fatalf("DummyHash() = %q; want prefix %q", h, prefix)
	}
	// Must have all 6 $-separated parts.
	parts := strings.Split(h, "$")
	if len(parts) != 6 {
		t.Fatalf("DummyHash() has %d '$'-separated parts; want 6; hash=%q", len(parts), h)
	}
}

// TestArgon2Pool_RunCapsConcurrency verifies that Verify never exceeds the
// concurrency cap. We inject a fake verifyFn via the package-level var so we
// can observe the in-flight count without spending real argon2 time.
func TestArgon2Pool_RunCapsConcurrency(t *testing.T) {
	const cap = 3
	pool := NewArgon2Pool(cap)

	var (
		inFlight    int64
		maxObserved int64
		mu          sync.Mutex
	)

	// Inject a stub that records the peak in-flight count.
	old := argon2Verify
	argon2Verify = func(pw, encoded string) bool {
		cur := atomic.AddInt64(&inFlight, 1)
		mu.Lock()
		if cur > maxObserved {
			maxObserved = cur
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond) // hold the slot briefly
		atomic.AddInt64(&inFlight, -1)
		return true
	}
	defer func() { argon2Verify = old }()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = pool.Verify(context.Background(), "pw", pool.DummyHash())
		}()
	}
	wg.Wait()

	if maxObserved > int64(cap) {
		t.Errorf("peak concurrent invocations = %d; want <= %d", maxObserved, cap)
	}
	if maxObserved == 0 {
		t.Error("stub was never called; test is broken")
	}
}

// TestArgon2Pool_QueueDepthOverflow_Returns503Sentinel fires 50 concurrent
// callers at a pool with concurrency=1, holding each slot for 200ms so the
// queue fills up. At least one call must return ErrArgon2Overloaded.
func TestArgon2Pool_QueueDepthOverflow_Returns503Sentinel(t *testing.T) {
	const concurrency = 1
	pool := NewArgon2Pool(concurrency)

	// Inject a slow stub so slots stay occupied long enough for the queue to fill.
	old := argon2Verify
	argon2Verify = func(pw, encoded string) bool {
		time.Sleep(200 * time.Millisecond)
		return true
	}
	defer func() { argon2Verify = old }()

	const goroutines = 50
	results := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			_, err := pool.Verify(context.Background(), "pw", pool.DummyHash())
			results <- err
		}()
	}

	var overloaded int
	for i := 0; i < goroutines; i++ {
		err := <-results
		if errors.Is(err, ErrArgon2Overloaded) {
			overloaded++
		}
	}

	if overloaded == 0 {
		t.Errorf("expected at least one ErrArgon2Overloaded with %d goroutines and cap=%d+queueDepth=%d; got none",
			goroutines, concurrency, queueDepth)
	}
	t.Logf("overloaded=%d / %d goroutines (cap=%d, queueDepth=%d, total slots=%d)",
		overloaded, goroutines, concurrency, queueDepth, concurrency+queueDepth)
}
