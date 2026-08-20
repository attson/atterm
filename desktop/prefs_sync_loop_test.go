package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakePrefsSyncEngine is the prefsSyncEngine substitute every test in this
// file drives instead of a real *prefssync.Engine. It records call counts
// and the maximum number of overlapping calls it ever observed, so tests
// can assert serialisation directly rather than inferring it from timing.
type fakePrefsSyncEngine struct {
	inFlight    int32
	maxInFlight int32

	mu        sync.Mutex
	pullCalls int
	pushCalls int
	pullErr   error
	pushErr   error
	pullPanic bool

	// callDelay holds every Pull/Push call up briefly so concurrent
	// enqueueSync callers have a real window to race into the engine if
	// nothing is stopping them.
	callDelay time.Duration

	// pushStarted, if non-nil, receives once per Push call right as it
	// begins (before callDelay), so a test can deterministically know a
	// push is in flight before firing more enqueueSync calls at it.
	pushStarted chan struct{}
}

func (f *fakePrefsSyncEngine) track() func() {
	n := atomic.AddInt32(&f.inFlight, 1)
	for {
		old := atomic.LoadInt32(&f.maxInFlight)
		if n <= old {
			break
		}
		if atomic.CompareAndSwapInt32(&f.maxInFlight, old, n) {
			break
		}
	}
	return func() { atomic.AddInt32(&f.inFlight, -1) }
}

func (f *fakePrefsSyncEngine) Pull(ctx context.Context) error {
	done := f.track()
	defer done()

	f.mu.Lock()
	f.pullCalls++
	err := f.pullErr
	doPanic := f.pullPanic
	delay := f.callDelay
	f.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}
	if doPanic {
		panic("fakePrefsSyncEngine: pull panic")
	}
	return err
}

func (f *fakePrefsSyncEngine) Push(ctx context.Context) error {
	done := f.track()
	defer done()

	f.mu.Lock()
	f.pushCalls++
	err := f.pushErr
	delay := f.callDelay
	f.mu.Unlock()

	if f.pushStarted != nil {
		select {
		case f.pushStarted <- struct{}{}:
		default:
		}
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	return err
}

func (f *fakePrefsSyncEngine) MarkDirty(key string, updatedAtLocalMs int64) {}

func (f *fakePrefsSyncEngine) SeedFromLocal(isCustomized func(key string) bool, updatedAtLocalMs int64) {
}

func (f *fakePrefsSyncEngine) counts() (pull, push int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pullCalls, f.pushCalls
}

// newLoopTestApp builds a minimal App wired to a fake engine and starts the
// serial sync loop against it. Returns the app, the fake, and a cancel func
// for a.ctx (callers needing the un-cancelled context just don't call it).
func newLoopTestApp(t *testing.T) (*App, *fakePrefsSyncEngine, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	fake := &fakePrefsSyncEngine{}
	a := &App{ctx: ctx, prefsSync: fake}
	a.startPrefsSyncLoop()
	return a, fake, cancel
}

// Polling for an asynchronous engine call to land uses waitFor, defined in
// ssh_tunnels_test.go (same package) — never used here to "prove"
// serialisation itself, that's what maxInFlight is for.

func TestSyncLoopSerialisesConcurrentRequests(t *testing.T) {
	a, fake, _ := newLoopTestApp(t)
	fake.callDelay = 5 * time.Millisecond

	var wg sync.WaitGroup
	const goroutines = 20
	const perGoroutine = 5
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				if (i+j)%2 == 0 {
					a.enqueueSync(syncRequest{pull: true})
				} else {
					a.enqueueSync(syncRequest{push: true})
				}
				time.Sleep(time.Millisecond)
			}
		}(i)
	}
	wg.Wait()

	// Give the loop time to drain whatever is still queued.
	waitFor(t, 2*time.Second, "loop to drain queued requests", func() bool {
		pull, push := fake.counts()
		return pull+push > 0 && atomic.LoadInt32(&fake.inFlight) == 0
	})
	time.Sleep(20 * time.Millisecond) // settle any last in-flight call

	if max := atomic.LoadInt32(&fake.maxInFlight); max > 1 {
		t.Fatalf("observed %d concurrent engine calls; the loop must serialise to 1", max)
	}
}

func TestSyncLoopCoalescesWhileInFlight(t *testing.T) {
	a, fake, _ := newLoopTestApp(t)
	fake.callDelay = 80 * time.Millisecond
	fake.pushStarted = make(chan struct{}, 1)

	a.enqueueSync(syncRequest{push: true})

	select {
	case <-fake.pushStarted:
	case <-time.After(time.Second):
		t.Fatal("first push never started")
	}

	// The first push is now in flight (asleep inside callDelay). Fire a
	// burst of further push requests at it; a channel that queued a second
	// request per call would turn this into 10 more round trips instead of
	// coalescing them into the one pending slot.
	for i := 0; i < 10; i++ {
		a.enqueueSync(syncRequest{push: true})
	}

	waitFor(t, 2*time.Second, "coalesced push to complete", func() bool {
		_, push := fake.counts()
		return push >= 2 && atomic.LoadInt32(&fake.inFlight) == 0
	})
	time.Sleep(20 * time.Millisecond)

	if _, push := fake.counts(); push != 2 {
		t.Fatalf("push calls = %d, want exactly 2 (the in-flight call plus one coalesced call)", push)
	}
}

func TestSyncLoopSurvivesAnEngineError(t *testing.T) {
	a, fake, _ := newLoopTestApp(t)

	fake.mu.Lock()
	fake.pullErr = errors.New("boom: pull failed")
	fake.mu.Unlock()

	a.enqueueSync(syncRequest{pull: true})
	waitFor(t, time.Second, "first (erroring) pull to run", func() bool {
		pull, _ := fake.counts()
		return pull == 1
	})

	fake.mu.Lock()
	fake.pullErr = nil
	fake.pullPanic = true
	fake.mu.Unlock()

	a.enqueueSync(syncRequest{pull: true})
	waitFor(t, time.Second, "second (panicking) pull to run", func() bool {
		pull, _ := fake.counts()
		return pull == 2
	})

	fake.mu.Lock()
	fake.pullPanic = false
	fake.mu.Unlock()

	// The loop must still be alive and serving requests after both a
	// returned error and a panic.
	a.enqueueSync(syncRequest{pull: true})
	waitFor(t, time.Second, "third pull to run after the loop survived an error and a panic", func() bool {
		pull, _ := fake.counts()
		return pull == 3
	})
}

func TestSyncLoopStopsOnContextCancel(t *testing.T) {
	a, fake, cancel := newLoopTestApp(t)

	a.enqueueSync(syncRequest{pull: true})
	waitFor(t, time.Second, "loop to process one request before cancel", func() bool {
		pull, _ := fake.counts()
		return pull == 1
	})

	cancel()
	select {
	case <-a.prefsSyncLoopDone:
	case <-time.After(time.Second):
		t.Fatal("loop goroutine never exited after ctx cancel")
	}

	// enqueueSync after the loop has exited must neither block nor panic.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 5; i++ {
			a.enqueueSync(syncRequest{push: true})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("enqueueSync blocked after the loop exited")
	}

	// And nothing new was actually processed — the loop is gone.
	time.Sleep(20 * time.Millisecond)
	if _, push := fake.counts(); push != 0 {
		t.Fatalf("push calls = %d after context cancel, want 0 (loop should not still be running)", push)
	}
}

func TestEnqueueAfterShutdownDoesNotBlock(t *testing.T) {
	t.Run("before startPrefsSyncLoop has ever run", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		a := &App{ctx: ctx, prefsSync: &fakePrefsSyncEngine{}}

		done := make(chan struct{})
		go func() {
			a.enqueueSync(syncRequest{pull: true})
			a.enqueueSync(syncRequest{push: true})
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("enqueueSync blocked with no loop ever started")
		}
	})

	t.Run("after the loop has stopped", func(t *testing.T) {
		a, _, cancel := newLoopTestApp(t)
		cancel()
		select {
		case <-a.prefsSyncLoopDone:
		case <-time.After(time.Second):
			t.Fatal("loop goroutine never exited")
		}

		done := make(chan struct{})
		go func() {
			for i := 0; i < 20; i++ {
				a.enqueueSync(syncRequest{pull: i%2 == 0, push: i%2 != 0})
			}
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("enqueueSync blocked after loop shutdown")
		}
	})
}

// TestNoDirectPrefsSyncCallsOutsideLoopFile pins the rewiring requirement
// itself: every desktop/*.go file other than prefs_sync_loop.go must be
// free of a.prefsSync.<Method> calls. A caller that bypasses enqueueSync /
// enqueuePostLoginSeed reintroduces exactly the unserialised access this
// whole file exists to remove.
func TestNoDirectPrefsSyncCallsOutsideLoopFile(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	re := regexp.MustCompile(`a(?:pp)?\.prefsSync\.`)
	var offenders []string
	for _, f := range files {
		base := filepath.Base(f)
		if base == "prefs_sync_loop.go" || strings.HasSuffix(base, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if loc := re.FindIndex(data); loc != nil {
			line := 1 + strings.Count(string(data[:loc[0]]), "\n")
			offenders = append(offenders, filepath.Base(f)+":"+strconv.Itoa(line))
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("found a.prefsSync. call(s) outside prefs_sync_loop.go: %v", offenders)
	}
}
