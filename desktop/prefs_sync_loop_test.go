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

	mu             sync.Mutex
	pullCalls      int
	pushCalls      int
	seedCalls      int
	markDirtyCalls int
	pullErr        error
	pushErr        error
	pullPanic      bool

	// callDelay holds every Pull/Push call up briefly so concurrent
	// enqueueSync callers have a real window to race into the engine if
	// nothing is stopping them.
	callDelay time.Duration

	// pushStarted, if non-nil, receives once per Push call right as it
	// begins (before callDelay), so a test can deterministically know a
	// push is in flight before firing more enqueueSync calls at it.
	pushStarted chan struct{}

	// markDirtyDelay, if non-zero, holds MarkDirty up before it records the
	// call -- used to widen the race window a caller that reordered
	// "MarkDirty before enqueue" into "enqueue before MarkDirty" would fall
	// into, so TestMarkPrefDirtyAndPush_MarkDirtyPrecedesThePush can catch it
	// deterministically instead of relying on scheduling luck.
	markDirtyDelay time.Duration
	markDirtyDone  atomic.Bool

	// pushObserved/pushSawMarkDirty record, for the first Push call only,
	// whether MarkDirty had already completed by the time that Push started.
	pushObserved     atomic.Bool
	pushSawMarkDirty atomic.Bool
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
	// Record, once, whether MarkDirty had already completed by the time this
	// (the first) Push began -- see TestMarkPrefDirtyAndPush_MarkDirtyPrecedesThePush.
	if f.pushObserved.CompareAndSwap(false, true) {
		f.pushSawMarkDirty.Store(f.markDirtyDone.Load())
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	return err
}

func (f *fakePrefsSyncEngine) MarkDirty(key string, updatedAtLocalMs int64) {
	f.mu.Lock()
	delay := f.markDirtyDelay
	f.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	f.mu.Lock()
	f.markDirtyCalls++
	f.mu.Unlock()
	f.markDirtyDone.Store(true)
}

func (f *fakePrefsSyncEngine) SeedFromLocal(isCustomized func(key string) bool, updatedAtLocalMs int64) {
	f.mu.Lock()
	f.seedCalls++
	f.mu.Unlock()
}

func (f *fakePrefsSyncEngine) counts() (pull, push int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pullCalls, f.pushCalls
}

func (f *fakePrefsSyncEngine) seedCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seedCalls
}

func (f *fakePrefsSyncEngine) markDirtyCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.markDirtyCalls
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
// free of <receiver>.prefsSync.<Method> calls. A caller that bypasses
// enqueueSync / enqueuePostLoginSeed reintroduces exactly the unserialised
// access this whole file exists to remove.
//
// The pattern is receiver-agnostic (".prefsSync." followed by an exported
// method name) rather than anchored to "a" or "app": a method on some other
// receiver name would slip past a regex hard-coded to those two. It also
// reports every match per file via FindAllIndex, not just the first, so a
// file with more than one offending call site doesn't hide the rest.
func TestNoDirectPrefsSyncCallsOutsideLoopFile(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	re := regexp.MustCompile(`\.prefsSync\.[A-Z]`)
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
		for _, loc := range re.FindAllIndex(data, -1) {
			line := 1 + strings.Count(string(data[:loc[0]]), "\n")
			offenders = append(offenders, base+":"+strconv.Itoa(line))
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("found .prefsSync.<Method> call(s) outside prefs_sync_loop.go: %v", offenders)
	}
}

// captureLogSink (lockedBuffer's Write/String) is defined in
// logging_level_test.go, same package -- reused here rather than duplicated.

// stopLoop cancels the app's context and blocks until runPrefsSyncLoop has
// actually exited, so a test that captures log output doesn't race a
// still-running loop goroutine logging into a buffer a later test owns.
func stopLoop(t *testing.T, a *App, cancel context.CancelFunc) {
	t.Helper()
	cancel()
	select {
	case <-a.prefsSyncLoopDone:
	case <-time.After(time.Second):
		t.Fatal("loop goroutine never exited")
	}
}

// TestSyncLoopLogsWarnOnPullFailure pins MAJOR 1's first half: a failed Pull
// must produce a visible warn-level log line. Deleting the logWarn call in
// runSyncRequest's pull branch (and emitting prefs:changed unconditionally
// instead) is exactly the kind of change this test exists to catch -- it
// compiles cleanly and every other test in this package stays green.
func TestSyncLoopLogsWarnOnPullFailure(t *testing.T) {
	buf := captureLogSink(t)
	a, fake, cancel := newLoopTestApp(t)

	fake.mu.Lock()
	fake.pullErr = errors.New("boom: pull failed distinctively")
	fake.mu.Unlock()

	a.enqueueSync(syncRequest{pull: true})
	waitFor(t, time.Second, "failing pull to run", func() bool {
		pull, _ := fake.counts()
		return pull == 1
	})
	time.Sleep(20 * time.Millisecond)
	stopLoop(t, a, cancel)

	got := buf.String()
	if !strings.Contains(got, "WARN") || !strings.Contains(got, "boom: pull failed distinctively") {
		t.Fatalf("expected a WARN log line naming the pull error, got: %q", got)
	}
}

// TestSyncLoopLogsWarnWithDirtyKeysOnPushFailure pins MAJOR 1's second half
// and MAJOR 2: a failed Push must produce a visible warn-level log line, and
// that line must name the set of keys that were dirty going into the push --
// not the single key whose setter happened to trigger the enqueue. The old
// per-trigger attribution could point at the wrong suspect (during the
// ssh_hosts_encrypted incident, the trigger was plausibly some unrelated key
// like terminal_font_size); the dirty set is exactly the batch Push collects.
func TestSyncLoopLogsWarnWithDirtyKeysOnPushFailure(t *testing.T) {
	buf := captureLogSink(t)
	a, fake, cancel := newLoopTestApp(t)

	fake.mu.Lock()
	fake.pushErr = errors.New("400 unknown_key")
	fake.mu.Unlock()

	a.cfgStore = &configStore{cfg: appConfig{PrefsMeta: map[string]prefsMetaEntry{
		"ssh_hosts_encrypted": {Dirty: true, UpdatedAtLocal: 1},
		"terminal_theme":      {Dirty: true, UpdatedAtLocal: 2},
		"default_shell":       {Dirty: false, UpdatedAtLocal: 3},
	}}}

	a.enqueueSync(syncRequest{push: true})
	waitFor(t, time.Second, "failing push to run", func() bool {
		_, push := fake.counts()
		return push == 1
	})
	time.Sleep(20 * time.Millisecond)
	stopLoop(t, a, cancel)

	got := buf.String()
	if !strings.Contains(got, "WARN") || !strings.Contains(got, "400 unknown_key") {
		t.Fatalf("expected a WARN log line naming the push error, got: %q", got)
	}
	if !strings.Contains(got, "ssh_hosts_encrypted") || !strings.Contains(got, "terminal_theme") {
		t.Fatalf("expected the WARN line to name the dirty keys in the failed batch, got: %q", got)
	}
	if strings.Contains(got, "default_shell") {
		t.Fatalf("WARN line named a key that was not dirty, got: %q", got)
	}
}

// TestEnqueuePostLoginSeed_FailedPushLeavesSeedMarkerUnwritten pins MAJOR 3:
// the seed marker must only be written once Push has actually succeeded. As
// the comment above the Push call in enqueuePostLoginSeed explains, marking
// the seed done anyway would make it permanently un-retryable -- the next
// launch would see PrefsSeedMarkerFor(userID)==true, skip SeedFromLocal
// entirely, and Pull would adopt whatever's on the relay over local values
// that never got a second chance to upload.
func TestEnqueuePostLoginSeed_FailedPushLeavesSeedMarkerUnwritten(t *testing.T) {
	a, fake, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)

	fake.mu.Lock()
	fake.pushErr = errors.New("seed push failed")
	fake.mu.Unlock()

	a.cfgStore = &configStore{cfg: appConfig{RelaySessionUserID: "user-1"}}
	a.eventsEmitter = func(context.Context, string, ...interface{}) {}

	a.enqueuePostLoginSeed()

	waitFor(t, time.Second, "seed push to run", func() bool {
		_, push := fake.counts()
		return push == 1
	})
	time.Sleep(20 * time.Millisecond)

	if fake.seedCallCount() != 1 {
		t.Fatalf("SeedFromLocal calls = %d, want 1 (seed must still run before the failing push)", fake.seedCallCount())
	}
	if a.cfgStore.Get().PrefsSeedMarkerFor("user-1") {
		t.Fatal("seed marker written despite the seed push failing -- next launch would skip SeedFromLocal and never retry")
	}
}

// TestSyncLoopTaskPanicDoesNotKillLoop pins the other half of MAJOR 3:
// runSyncTask's recover() must actually contain a panic from a task run via
// prefsSyncTaskCh, exactly as runSyncRequest's recover() does for
// prefsSyncCh -- the loop must still serve the next request afterwards.
// Before this test, that recover() had never executed in any test.
func TestSyncLoopTaskPanicDoesNotKillLoop(t *testing.T) {
	a, fake, _ := newLoopTestApp(t)

	a.prefsSyncTaskCh <- func(engine prefsSyncEngine) { panic("task boom") }

	// Give the loop a moment to run the panicking task and recover from it.
	time.Sleep(20 * time.Millisecond)

	a.enqueueSync(syncRequest{pull: true})
	waitFor(t, time.Second, "pull to run on the loop after a task panicked", func() bool {
		pull, _ := fake.counts()
		return pull == 1
	})
}

// TestSyncLoopCoalesceMergesDifferentFlags pins MAJOR 4: enqueueSync's merge
// must OR flags together, not drop whichever request loses a race for the
// pending slot. TestSyncLoopCoalescesWhileInFlight only asserts a call
// count, which a naive drop-on-full policy (`select { case ch <- req:
// default: }`) also satisfies -- it just silently discards the pull. That is
// a real behaviour difference: a watcher's pull would vanish whenever a
// push happened to already be queued.
func TestSyncLoopCoalesceMergesDifferentFlags(t *testing.T) {
	a, fake, _ := newLoopTestApp(t)
	fake.callDelay = 80 * time.Millisecond
	fake.pushStarted = make(chan struct{}, 1)

	a.enqueueSync(syncRequest{push: true})
	select {
	case <-fake.pushStarted:
	case <-time.After(time.Second):
		t.Fatal("first push never started")
	}

	// The first push is now in flight and the pending slot is empty. Fill it
	// with a second push, then merge a pull into it -- a drop-on-full policy
	// would silently discard whichever of these loses the race for the one
	// slot (the pull, arriving second) instead of merging both flags into
	// the request that eventually runs.
	a.enqueueSync(syncRequest{push: true})
	a.enqueueSync(syncRequest{pull: true})

	waitFor(t, 2*time.Second, "merged pull+push request to complete", func() bool {
		pull, push := fake.counts()
		return pull == 1 && push == 2 && atomic.LoadInt32(&fake.inFlight) == 0
	})
	time.Sleep(20 * time.Millisecond)

	pull, push := fake.counts()
	if pull != 1 || push != 2 {
		t.Fatalf("pull=%d push=%d; want pull=1 push=2 (the in-flight push, plus one merged request carrying both flags)", pull, push)
	}
}

// TestMarkPrefDirtyAndPush_MarkDirtyPrecedesThePush pins MINOR 5: MarkDirty
// must be called, and complete, before markPrefDirtyAndPush enqueues the
// push -- swapping the two lines in markPrefDirtyAndPush would let the loop
// goroutine start (and even finish) the push before the dirty stamp for this
// edit exists, losing it from that push's batch. fake.markDirtyDelay widens
// that race window artificially so the assertion is deterministic instead of
// depending on scheduler luck: under the correct order, MarkDirty (delayed)
// fully completes on the caller's goroutine strictly before enqueueSync is
// even called, so the loop can never observe it as not-yet-done; under the
// swapped order, the fast, undelayed Push routinely wins the race.
func TestMarkPrefDirtyAndPush_MarkDirtyPrecedesThePush(t *testing.T) {
	a, fake, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)
	fake.markDirtyDelay = 20 * time.Millisecond

	a.markPrefDirtyAndPush("terminal_theme")

	waitFor(t, time.Second, "push triggered by markPrefDirtyAndPush to run", func() bool {
		return fake.pushObserved.Load()
	})

	if !fake.pushSawMarkDirty.Load() {
		t.Fatal("push ran before MarkDirty finished -- markPrefDirtyAndPush must call MarkDirty synchronously before enqueuing the push")
	}
	if fake.markDirtyCallCount() != 1 {
		t.Fatalf("MarkDirty calls = %d, want 1", fake.markDirtyCallCount())
	}
}
