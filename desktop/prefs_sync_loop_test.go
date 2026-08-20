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

	"github.com/attson/atterm/internal/prefssync"
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
	// pushPanic, when non-empty, makes Push panic with it — so a test can
	// prove a survived panic still reaches the status, not just the log.
	pushPanic string
	// pullResult is returned by Pull whenever pullErr is nil, so tests can
	// drive the "sync:pulled" event without a real relay.
	pullResult prefssync.PullResult

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

func (f *fakePrefsSyncEngine) Pull(ctx context.Context) (prefssync.PullResult, error) {
	done := f.track()
	defer done()

	f.mu.Lock()
	f.pullCalls++
	err := f.pullErr
	doPanic := f.pullPanic
	delay := f.callDelay
	result := f.pullResult
	f.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}
	if doPanic {
		panic("fakePrefsSyncEngine: pull panic")
	}
	if err != nil {
		return prefssync.PullResult{}, err
	}
	return result, nil
}

func (f *fakePrefsSyncEngine) Push(ctx context.Context) error {
	done := f.track()
	defer done()

	f.mu.Lock()
	f.pushCalls++
	err := f.pushErr
	delay := f.callDelay
	panicWith := f.pushPanic
	f.mu.Unlock()

	if panicWith != "" {
		panic(panicWith)
	}

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

// --- SyncStatus / SyncNow / sync:status / sync:pulled ---
//
// recordedEvent + newRecordingEmitter give the tests below a way to assert
// both that an event fired and what it carried, without a live Wails
// context -- the same injection point emitPrefsChanged already relies on.

type recordedEvent struct {
	name string
	data []interface{}
}

func newRecordingEmitter() (emit func(ctx context.Context, name string, data ...interface{}), get func() []recordedEvent) {
	var mu sync.Mutex
	var events []recordedEvent
	emit = func(ctx context.Context, name string, data ...interface{}) {
		mu.Lock()
		events = append(events, recordedEvent{name: name, data: data})
		mu.Unlock()
	}
	get = func() []recordedEvent {
		mu.Lock()
		defer mu.Unlock()
		out := make([]recordedEvent, len(events))
		copy(out, events)
		return out
	}
	return emit, get
}

// syncStatusStates filters events down to every "sync:status" payload's
// State, in emission order -- what the tests below actually care about.
func syncStatusStates(events []recordedEvent) []string {
	var out []string
	for _, e := range events {
		if e.name != "sync:status" || len(e.data) == 0 {
			continue
		}
		status, ok := e.data[0].(SyncStatus)
		if !ok {
			continue
		}
		out = append(out, status.State)
	}
	return out
}

func statesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// configuredCfgStore builds a cfgStore with everything syncOffline checks
// for present, so GetSyncStatus never falls into "offline" -- the baseline
// every "error"/"idle"/"syncing" test below starts from.
func configuredCfgStore(extra appConfig) *configStore {
	extra.RelayURL = "https://relay.example"
	extra.RelaySessionToken = "session-token"
	extra.RelayPaused = false
	return &configStore{cfg: extra}
}

// TestGetSyncStatus_OfflineWhenRelayNotConfigured pins the offline/error
// distinction from the task-3 brief: an unconfigured relay must report
// "offline", never "error" -- even when the engine call that ran while
// unconfigured (a real relay client would fail auth) came back with an
// error. syncOffline is checked before the stored error in GetSyncStatus
// specifically so this can't regress into a red indicator. It also pins
// MAJOR 3: LastError itself, not just State, must be suppressed while
// offline.
//
// It does NOT cover coming back online. An earlier version of this docstring
// claimed it did, and the whole-branch review disproved that: masking alone
// left a stale error to reappear red the moment the config became online
// again. That half is
// TestGetSyncStatus_ConfigChangeDropsAStaleErrorAndEmits below.
func TestGetSyncStatus_OfflineWhenRelayNotConfigured(t *testing.T) {
	a, fake, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)
	a.cfgStore = &configStore{cfg: appConfig{}} // RelayURL, RelaySessionToken both empty

	fake.mu.Lock()
	fake.pullErr = errors.New("not logged in")
	fake.mu.Unlock()

	a.enqueueSync(syncRequest{pull: true})
	waitFor(t, time.Second, "pull to run while unconfigured", func() bool {
		pull, _ := fake.counts()
		return pull == 1
	})
	time.Sleep(20 * time.Millisecond)

	status := a.GetSyncStatus()
	if status.State != "offline" {
		t.Fatalf("State = %q, want %q (relay is not configured; a failed engine call must not surface as \"error\")", status.State, "offline")
	}
	if status.LastError != "" {
		t.Fatalf("LastError = %q while offline, want \"\" (a stale/recorded error must not leak through once state is \"offline\")", status.LastError)
	}
}

// TestGetSyncStatus_ErrorWhenConfiguredAndSyncFails pins the other half of
// the distinction: with a relay actually configured, a failed Push must
// report "error", carrying the failure's message -- the whole point being
// that a silent failure (the ssh_hosts_encrypted incident referenced in
// markPrefDirtyAndPush) is no longer silent.
func TestGetSyncStatus_ErrorWhenConfiguredAndSyncFails(t *testing.T) {
	a, fake, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)
	a.cfgStore = configuredCfgStore(appConfig{})

	fake.mu.Lock()
	fake.pushErr = errors.New("400 unknown_key: ssh_hosts_encrypted")
	fake.mu.Unlock()

	a.enqueueSync(syncRequest{push: true})
	waitFor(t, time.Second, "failing push to run", func() bool {
		_, push := fake.counts()
		return push == 1
	})
	time.Sleep(20 * time.Millisecond)

	status := a.GetSyncStatus()
	if status.State != "error" {
		t.Fatalf("State = %q, want %q", status.State, "error")
	}
	if !strings.Contains(status.LastError, "400 unknown_key: ssh_hosts_encrypted") {
		t.Fatalf("LastError = %q, want it to contain the push failure's message", status.LastError)
	}
}

// TestGetSyncStatus_ErrorClearsOnNextSuccess pins the "clears on the next
// success" requirement: a failed push puts the status into "error"; a
// subsequent successful push must clear both State back to "idle" and
// LastError back to "". Before recordSyncOutcome existed, nothing ever
// reset a stored failure -- this is the test that would catch that
// regressing back in.
func TestGetSyncStatus_ErrorClearsOnNextSuccess(t *testing.T) {
	a, fake, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)
	a.cfgStore = configuredCfgStore(appConfig{})

	fake.mu.Lock()
	fake.pushErr = errors.New("400 unknown_key")
	fake.mu.Unlock()

	a.enqueueSync(syncRequest{push: true})
	waitFor(t, time.Second, "failing push to run", func() bool {
		_, push := fake.counts()
		return push == 1
	})
	time.Sleep(20 * time.Millisecond)
	if got := a.GetSyncStatus().State; got != "error" {
		t.Fatalf("State after failing push = %q, want %q", got, "error")
	}

	fake.mu.Lock()
	fake.pushErr = nil
	fake.mu.Unlock()

	a.enqueueSync(syncRequest{push: true})
	waitFor(t, time.Second, "succeeding push to run", func() bool {
		_, push := fake.counts()
		return push == 2
	})
	time.Sleep(20 * time.Millisecond)

	status := a.GetSyncStatus()
	if status.State != "idle" {
		t.Fatalf("State after succeeding push = %q, want %q", status.State, "idle")
	}
	if status.LastError != "" {
		t.Fatalf("LastError = %q after a successful push, want empty", status.LastError)
	}
	if status.LastSyncedAt == 0 {
		t.Fatal("LastSyncedAt still 0 after a successful push")
	}
}

// TestGetSyncStatus_PendingKeysCountsDirtyKeys pins that PendingKeys is the
// same number dirtyPrefKeys() would report -- the brief's instruction to
// reuse that helper rather than growing a second way to count the same
// thing. A PendingKeys computed some other way (e.g. from push call counts)
// would not track this.
func TestGetSyncStatus_PendingKeysCountsDirtyKeys(t *testing.T) {
	a, _, cancel := newLoopTestApp(t)
	defer cancel()
	a.cfgStore = configuredCfgStore(appConfig{PrefsMeta: map[string]prefsMetaEntry{
		"ssh_hosts_encrypted": {Dirty: true, UpdatedAtLocal: 1},
		"terminal_theme":      {Dirty: true, UpdatedAtLocal: 2},
		"default_shell":       {Dirty: false, UpdatedAtLocal: 3},
	}})

	want := len(a.dirtyPrefKeys())
	if want != 2 {
		t.Fatalf("test fixture: dirtyPrefKeys() = %d, want 2 (sanity check on the fixture itself)", want)
	}
	if got := a.GetSyncStatus().PendingKeys; got != want {
		t.Fatalf("PendingKeys = %d, want %d (dirtyPrefKeys() count)", got, want)
	}
}

// TestSyncNow_OfflineReturnsErrorWithoutEnqueuing pins "SyncNow errors only
// for cannot-start (offline)": with no relay configured it must return a
// non-nil error and must not enqueue any engine work.
func TestSyncNow_OfflineReturnsErrorWithoutEnqueuing(t *testing.T) {
	a, fake, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)
	a.cfgStore = &configStore{cfg: appConfig{}}

	if err := a.SyncNow(); err == nil {
		t.Fatal("SyncNow() returned nil error while offline, want an error")
	}
	time.Sleep(50 * time.Millisecond)
	pull, push := fake.counts()
	if pull != 0 || push != 0 {
		t.Fatalf("pull=%d push=%d after an offline SyncNow, want 0/0 (must not enqueue)", pull, push)
	}
}

// TestSyncNow_OnlineEnqueuesPullThenPush pins the success half: with a
// relay configured, SyncNow returns nil immediately and the loop ends up
// running both a pull and a push.
func TestSyncNow_OnlineEnqueuesPullThenPush(t *testing.T) {
	a, fake, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)
	a.cfgStore = configuredCfgStore(appConfig{})

	if err := a.SyncNow(); err != nil {
		t.Fatalf("SyncNow() = %v, want nil while configured", err)
	}
	waitFor(t, time.Second, "SyncNow's pull and push to run", func() bool {
		pull, push := fake.counts()
		return pull == 1 && push == 1
	})
}

// TestSyncStatusEvent_FiresOnTransitions_Success pins "a sync:status event
// fires on every observable change to SyncStatus": a single successful push,
// starting from idle, passes through "syncing" twice -- once when the loop
// marks itself busy (LastSyncedAt still 0, nothing has succeeded yet), and
// once more when recordSyncOutcome stamps LastSyncedAt after the push
// actually succeeds, still with busy == true -- before finally dropping to
// "idle" once the loop clears busy. That middle "syncing" carries genuinely
// new information (LastSyncedAt going from 0 to non-zero) even though State
// itself didn't change, which is exactly why emitSyncStatusIfChanged dedupes
// on the whole SyncStatus value rather than State alone (see MAJOR 2 in the
// fix-round-1 report): deduping on State only would silently swallow that
// middle event, and a frontend timing "last synced" off of it would miss the
// update until the next unrelated transition happened to fire.
func TestSyncStatusEvent_FiresOnTransitions_Success(t *testing.T) {
	a, fake, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)
	a.cfgStore = configuredCfgStore(appConfig{})
	emit, events := newRecordingEmitter()
	a.eventsEmitter = emit

	a.enqueueSync(syncRequest{push: true})
	waitFor(t, time.Second, "push to run", func() bool {
		_, push := fake.counts()
		return push == 1
	})
	time.Sleep(20 * time.Millisecond)

	got := syncStatusStates(events())
	want := []string{"syncing", "syncing", "idle"}
	if !statesEqual(got, want) {
		t.Fatalf("sync:status states = %v, want %v", got, want)
	}

	all := events()
	var sawZeroLastSyncedAt, sawNonZeroLastSyncedAt bool
	for _, e := range all {
		if e.name != "sync:status" || len(e.data) == 0 {
			continue
		}
		status, ok := e.data[0].(SyncStatus)
		if !ok {
			continue
		}
		if status.LastSyncedAt == 0 {
			sawZeroLastSyncedAt = true
		} else {
			sawNonZeroLastSyncedAt = true
		}
	}
	if !sawZeroLastSyncedAt || !sawNonZeroLastSyncedAt {
		t.Fatalf("expected both a pre-success (LastSyncedAt == 0) and a post-success (LastSyncedAt != 0) sync:status event among the two \"syncing\" emissions, got events: %+v", all)
	}
}

// TestSyncStatusEvent_FiresOnTransitions_Error is the failing counterpart:
// a push that errors must transition to "error", not back to "idle". Like
// the success case above, "syncing" fires twice -- the loop marking itself
// busy, then recordSyncOutcome stamping LastError while busy is still true
// -- before the loop clears busy and the stored error surfaces as "error".
func TestSyncStatusEvent_FiresOnTransitions_Error(t *testing.T) {
	a, fake, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)
	a.cfgStore = configuredCfgStore(appConfig{})
	emit, events := newRecordingEmitter()
	a.eventsEmitter = emit

	fake.mu.Lock()
	fake.pushErr = errors.New("boom")
	fake.mu.Unlock()

	a.enqueueSync(syncRequest{push: true})
	waitFor(t, time.Second, "failing push to run", func() bool {
		_, push := fake.counts()
		return push == 1
	})
	time.Sleep(20 * time.Millisecond)

	got := syncStatusStates(events())
	want := []string{"syncing", "syncing", "error"}
	if !statesEqual(got, want) {
		t.Fatalf("sync:status states = %v, want %v", got, want)
	}
}

// TestSyncPulledEvent_FiresWhenPullChangedSomething pins "sync:pulled fires
// with the PullResult after a pull that changed something": a Pull result
// carrying an adopted key must produce exactly one "sync:pulled" event
// carrying that same PullResult.
func TestSyncPulledEvent_FiresWhenPullChangedSomething(t *testing.T) {
	a, fake, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)
	a.cfgStore = configuredCfgStore(appConfig{})
	emit, events := newRecordingEmitter()
	a.eventsEmitter = emit

	want := prefssync.PullResult{Adopted: []string{"terminal_theme"}, Conflict: []string{"default_shell"}}
	fake.mu.Lock()
	fake.pullResult = want
	fake.mu.Unlock()

	a.enqueueSync(syncRequest{pull: true})
	waitFor(t, time.Second, "pull to run", func() bool {
		pull, _ := fake.counts()
		return pull == 1
	})
	time.Sleep(20 * time.Millisecond)

	var pulled []recordedEvent
	for _, e := range events() {
		if e.name == "sync:pulled" {
			pulled = append(pulled, e)
		}
	}
	if len(pulled) != 1 {
		t.Fatalf("sync:pulled events = %d, want 1", len(pulled))
	}
	got, ok := pulled[0].data[0].(prefssync.PullResult)
	if !ok {
		t.Fatalf("sync:pulled payload type = %T, want prefssync.PullResult", pulled[0].data[0])
	}
	if !statesEqual(got.Adopted, want.Adopted) || !statesEqual(got.Conflict, want.Conflict) {
		t.Fatalf("sync:pulled payload = %+v, want %+v", got, want)
	}
}

// TestSyncPulledEvent_SilentWhenPullChangedNothing is the counterpart: a
// Pull whose result is empty (nothing adopted, nothing conflicting -- the
// common no-op case) must not fire "sync:pulled" at all.
func TestSyncPulledEvent_SilentWhenPullChangedNothing(t *testing.T) {
	a, fake, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)
	a.cfgStore = configuredCfgStore(appConfig{})
	emit, events := newRecordingEmitter()
	a.eventsEmitter = emit

	fake.mu.Lock()
	fake.pullResult = prefssync.PullResult{}
	fake.mu.Unlock()

	a.enqueueSync(syncRequest{pull: true})
	waitFor(t, time.Second, "pull to run", func() bool {
		pull, _ := fake.counts()
		return pull == 1
	})
	time.Sleep(20 * time.Millisecond)

	for _, e := range events() {
		if e.name == "sync:pulled" {
			t.Fatalf("unexpected sync:pulled event for an empty PullResult: %+v", e)
		}
	}
}

// TestSyncStatusEvent_OfflineEditsEachEmitDistinctPendingKeys pins MAJOR 2:
// emitSyncStatusIfChanged must dedupe on the whole SyncStatus value, not
// just State. While offline, State is pinned to "offline" for the entire
// scenario below (syncOffline short-circuits GetSyncStatus's switch before
// busy/error are even considered), so a State-only dedupe would emit once
// and then silently swallow every later change -- exactly the bug reported
// against commit 40ecc85: three edits while offline produced one
// "sync:status" event carrying the first edit's stale PendingKeys forever
// after. Each of the three calls below changes PendingKeys by swapping in a
// cfgStore with a different dirty-key count immediately before calling
// emitSyncStatusIfChanged directly -- the same call recordSyncOutcome and
// setSyncBusy make on every engine call, driven here without a real engine
// round trip so the three "edits" land deterministically instead of racing
// the loop goroutine.
func TestSyncStatusEvent_OfflineEditsEachEmitDistinctPendingKeys(t *testing.T) {
	a, _, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)

	emit, events := newRecordingEmitter()
	a.eventsEmitter = emit

	offlineCfgWithDirtyCount := func(n int) *configStore {
		meta := map[string]prefsMetaEntry{}
		for i := 0; i < n; i++ {
			meta[strconv.Itoa(i)] = prefsMetaEntry{Dirty: true, UpdatedAtLocal: int64(i)}
		}
		// RelayURL left empty -- offline throughout this whole test.
		return &configStore{cfg: appConfig{PrefsMeta: meta}}
	}

	for _, n := range []int{1, 2, 3} {
		a.cfgStore = offlineCfgWithDirtyCount(n)
		a.emitSyncStatusIfChanged()
	}

	var pending []int
	for _, e := range events() {
		if e.name != "sync:status" || len(e.data) == 0 {
			continue
		}
		status, ok := e.data[0].(SyncStatus)
		if !ok {
			continue
		}
		if status.State != "offline" {
			t.Fatalf("sync:status event had State = %q while cfg was never configured, want %q", status.State, "offline")
		}
		if status.LastError != "" {
			t.Fatalf("sync:status event had LastError = %q while offline, want suppressed to \"\"", status.LastError)
		}
		pending = append(pending, status.PendingKeys)
	}

	want := []int{1, 2, 3}
	if len(pending) != len(want) {
		t.Fatalf("sync:status PendingKeys sequence = %v, want %v (one distinct event per distinct PendingKeys value while offline, not deduped away because State never changes)", pending, want)
	}
	for i := range want {
		if pending[i] != want[i] {
			t.Fatalf("sync:status PendingKeys sequence = %v, want %v", pending, want)
		}
	}
}

// TestSyncPulledEvent_FiresFromPostLoginSeed pins MAJOR 4: enqueuePostLoginSeed's
// own Pull call, not just runSyncRequest's, must surface a non-empty
// PullResult over "sync:pulled". Before this test, deleting the
// emitSyncPulledIfNonEmpty(result) call from enqueuePostLoginSeed's task
// closure left the whole suite green: TestSyncPulledEvent_FiresWhenPullChangedSomething
// only exercises runSyncRequest's pull branch. RelaySessionUserID is left
// empty so the task returns right after the pull (skipping SeedFromLocal
// and the seed's own Push), keeping this test's assertions about the pull's
// event isolated from the seed/push path.
func TestSyncPulledEvent_FiresFromPostLoginSeed(t *testing.T) {
	a, fake, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)
	a.cfgStore = configuredCfgStore(appConfig{}) // RelaySessionUserID == "" -> pull only, no seed
	emit, events := newRecordingEmitter()
	a.eventsEmitter = emit

	want := prefssync.PullResult{Adopted: []string{"terminal_theme"}, Conflict: []string{"default_shell"}}
	fake.mu.Lock()
	fake.pullResult = want
	fake.mu.Unlock()

	a.enqueuePostLoginSeed()
	waitFor(t, time.Second, "post-login pull to run", func() bool {
		pull, _ := fake.counts()
		return pull == 1
	})
	time.Sleep(20 * time.Millisecond)

	if fake.seedCallCount() != 0 {
		t.Fatalf("SeedFromLocal calls = %d, want 0 (RelaySessionUserID is empty, seeding should be skipped)", fake.seedCallCount())
	}

	var pulled []recordedEvent
	for _, e := range events() {
		if e.name == "sync:pulled" {
			pulled = append(pulled, e)
		}
	}
	if len(pulled) != 1 {
		t.Fatalf("sync:pulled events from enqueuePostLoginSeed = %d, want 1", len(pulled))
	}
	got, ok := pulled[0].data[0].(prefssync.PullResult)
	if !ok {
		t.Fatalf("sync:pulled payload type = %T, want prefssync.PullResult", pulled[0].data[0])
	}
	if !statesEqual(got.Adopted, want.Adopted) || !statesEqual(got.Conflict, want.Conflict) {
		t.Fatalf("sync:pulled payload = %+v, want %+v", got, want)
	}
}

// TestGetSyncStatus_ErrorWhenConfiguredAndPullFails pins MAJOR 5: a failing
// Pull, not just a failing Push, must surface as State == "error" (when a
// relay is actually configured). Before this test, deleting
// recordSyncOutcome(err) from runSyncRequest's pull branch left the whole
// suite green -- TestSyncLoopLogsWarnOnPullFailure only pins the log line,
// and every other GetSyncStatus/error test in this file drives the failure
// through a push.
func TestGetSyncStatus_ErrorWhenConfiguredAndPullFails(t *testing.T) {
	a, fake, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)
	a.cfgStore = configuredCfgStore(appConfig{})

	fake.mu.Lock()
	fake.pullErr = errors.New("pull failed: 503 relay unavailable")
	fake.mu.Unlock()

	a.enqueueSync(syncRequest{pull: true})
	waitFor(t, time.Second, "failing pull to run", func() bool {
		pull, _ := fake.counts()
		return pull == 1
	})
	time.Sleep(20 * time.Millisecond)

	status := a.GetSyncStatus()
	if status.State != "error" {
		t.Fatalf("State = %q, want %q", status.State, "error")
	}
	if !strings.Contains(status.LastError, "pull failed: 503 relay unavailable") {
		t.Fatalf("LastError = %q, want it to contain the pull failure's message", status.LastError)
	}
}

// TestSyncOffline_EachDisjunctAloneIsOffline pins MAJOR 6: syncOffline is an
// OR of three independent conditions, and each one alone -- with the other
// two satisfied -- must be enough to report offline. The task-3 brief's own
// fixture (appConfig{}) leaves all three simultaneously true, which makes
// each disjunct individually deletable from `cfg.RelayURL == "" ||
// cfg.RelayPaused || cfg.RelaySessionToken == ""` without any test noticing
// (the other two still evaluate true). These cases hold two of the three
// terms at their "configured" value and only the third at its "missing"
// value, so deleting any one disjunct flips exactly one of these subtests
// from offline to (incorrectly) online.
func TestSyncOffline_EachDisjunctAloneIsOffline(t *testing.T) {
	a := &App{}
	cases := []struct {
		name string
		cfg  appConfig
	}{
		{
			name: "URL missing only",
			cfg:  appConfig{RelayURL: "", RelaySessionToken: "session-token", RelayPaused: false},
		},
		{
			name: "token missing only",
			cfg:  appConfig{RelayURL: "https://relay.example", RelaySessionToken: "", RelayPaused: false},
		},
		{
			name: "paused only",
			cfg:  appConfig{RelayURL: "https://relay.example", RelaySessionToken: "session-token", RelayPaused: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !a.syncOffline(tc.cfg) {
				t.Fatalf("syncOffline(%+v) = false, want true -- this disjunct alone must be enough to report offline", tc.cfg)
			}
		})
	}

	t.Run("all three satisfied is online", func(t *testing.T) {
		cfg := appConfig{RelayURL: "https://relay.example", RelaySessionToken: "session-token", RelayPaused: false}
		if a.syncOffline(cfg) {
			t.Fatalf("syncOffline(%+v) = true, want false (URL set, token set, not paused)", cfg)
		}
	})
}

// TestSyncNow_ReturnsBeforeUnderlyingSyncCompletes pins MINOR 9:
// TestSyncNow_OnlineEnqueuesPullThenPush alone would still pass if SyncNow
// blocked synchronously on the pull+push it enqueues, since waitFor just
// polls until the counts land regardless of when SyncNow itself returned.
// fake.callDelay holds every Pull/Push call up for long enough that a
// synchronous SyncNow would take at least that long to return; this test
// asserts SyncNow returns in a small fraction of that time instead.
func TestSyncNow_ReturnsBeforeUnderlyingSyncCompletes(t *testing.T) {
	a, fake, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)
	a.cfgStore = configuredCfgStore(appConfig{})
	fake.callDelay = 300 * time.Millisecond

	start := time.Now()
	if err := a.SyncNow(); err != nil {
		t.Fatalf("SyncNow() = %v, want nil while configured", err)
	}
	if elapsed := time.Since(start); elapsed >= fake.callDelay/3 {
		t.Fatalf("SyncNow() took %v to return, want well under the %v engine callDelay -- it must enqueue and return immediately, not block on the sync itself", elapsed, fake.callDelay)
	}

	waitFor(t, 2*time.Second, "SyncNow's pull and push to eventually run", func() bool {
		pull, push := fake.counts()
		return pull == 1 && push == 1
	})
}

// The whole-branch review found the gap these two pin, and it is one gap with
// two halves: a relay config change neither cleared the recorded error nor
// emitted anything. Pausing sync, failing a push, and unpausing left
// GetSyncStatus returning {error, "<the old error>"} with no sync attempted
// since -- because unpausing goes through applyRelayConfig, which enqueues no
// work, so the loop's own emitters never ran. A user watching the Settings
// header while pausing sync in the relay tab went on seeing "Up to date".
func TestGetSyncStatus_ConfigChangeDropsAStaleErrorAndEmits(t *testing.T) {
	a, fake, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)
	a.cfgStore = &configStore{cfg: appConfig{RelayURL: "wss://r", RelaySessionToken: "tok"}}

	fake.mu.Lock()
	fake.pushErr = errors.New("400 unknown_key: stale-and-irrelevant")
	fake.mu.Unlock()

	a.enqueueSync(syncRequest{push: true})
	waitFor(t, time.Second, "the push to fail", func() bool {
		return a.GetSyncStatus().State == "error"
	})

	// Now the user pauses the relay. The config is offline, so the error is
	// masked -- but masking is not clearing, which is the whole point.
	cfg := a.cfgStore.Get()
	cfg.RelayPaused = true
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("set config: %v", err)
	}
	a.syncOfflineChanged()
	if got := a.GetSyncStatus(); got.State != "offline" || got.LastError != "" {
		t.Fatalf("while paused: %+v, want state=offline and no LastError", got)
	}

	// And unpauses, without anything having enqueued a sync in between.
	cfg = a.cfgStore.Get()
	cfg.RelayPaused = false
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("set config: %v", err)
	}
	a.syncOfflineChanged()
	got := a.GetSyncStatus()
	if got.State == "error" || got.LastError != "" {
		t.Fatalf("after unpausing: %+v -- a stale error from the previous config must not resurface; no sync has been attempted since", got)
	}
}

// The emit half. None of the config transitions enqueue sync work, so without
// an explicit emit the frontend never learns the state changed at all.
func TestSyncStatusEvent_FiresOnAConfigChangeWithNoSyncWork(t *testing.T) {
	a, _, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)
	a.cfgStore = &configStore{cfg: appConfig{RelayURL: "wss://r", RelaySessionToken: "tok"}}
	emit, events := newRecordingEmitter()
	a.eventsEmitter = emit

	// Establish a baseline so the dedup has something to compare against.
	a.syncOfflineChanged()
	before := len(syncStatusStates(events()))

	cfg := a.cfgStore.Get()
	cfg.RelayPaused = true
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("set config: %v", err)
	}
	a.syncOfflineChanged()

	states := syncStatusStates(events())
	if len(states) <= before {
		t.Fatalf("no sync:status event after the config went offline (%d states, was %d); the Settings indicator would keep showing the previous state", len(states), before)
	}
	if last := states[len(states)-1]; last != "offline" {
		t.Fatalf("last emitted state = %q, want offline", last)
	}
}

// Pins the WIRING, not just the helper. An earlier version of the test above
// called syncOfflineChanged directly, so deleting its call site in
// applyRelayConfig left the suite green -- the same "the guard exists but
// nothing reaches it" shape this branch has now hit repeatedly.
func TestApplyRelayConfig_NotifiesSyncStatus(t *testing.T) {
	a, _, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)
	a.cfgStore = &configStore{cfg: appConfig{}}
	emit, events := newRecordingEmitter()
	a.eventsEmitter = emit

	// An unconfigured relay: applyRelayUplink and applyRelayPrefsWatch both
	// take their "nothing to do" branches, so what is left to observe is
	// precisely whether the sync status was notified.
	a.applyRelayConfig(a.cfgStore.Get())

	states := syncStatusStates(events())
	if len(states) == 0 {
		t.Fatal("applyRelayConfig emitted no sync:status; pausing, unpausing, logging in and logging out all flow through here and none of them enqueue sync work, so without this the indicator never learns the state changed")
	}
	if last := states[len(states)-1]; last != "offline" {
		t.Fatalf("last emitted state = %q, want offline", last)
	}
}

// A panic inside the loop must not merely be survived, it must be visible.
// Surviving keeps the loop alive; leaving the status untouched leaves the
// indicator saying "Up to date" about a sync that blew up, which is the
// invisible-failure mode this whole feature exists to end.
func TestSyncLoopPanicIsRecordedAsAnError(t *testing.T) {
	a, fake, cancel := newLoopTestApp(t)
	defer stopLoop(t, a, cancel)
	a.cfgStore = &configStore{cfg: appConfig{RelayURL: "wss://r", RelaySessionToken: "tok"}}

	fake.mu.Lock()
	fake.pushPanic = "boom"
	fake.mu.Unlock()

	a.enqueueSync(syncRequest{push: true})
	waitFor(t, 2*time.Second, "the panic to surface as an error status", func() bool {
		return a.GetSyncStatus().State == "error"
	})
	if got := a.GetSyncStatus(); !strings.Contains(got.LastError, "boom") {
		t.Fatalf("LastError = %q, want it to name the panic", got.LastError)
	}
}
