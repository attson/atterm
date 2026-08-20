package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attson/atterm/internal/sshclient"
)

const snippetTestTplLabel = "Test"
const snippetTestTplText = "echo hi"

// fakeSnippetConn adapts per-call functions to snippetConn for tests.
type fakeSnippetConn struct {
	run     func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error)
	onClose func()
}

func (c fakeSnippetConn) Run(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
	return c.run(ctx, cmd, limit)
}

func (c fakeSnippetConn) Close() error {
	if c.onClose != nil {
		c.onClose()
	}
	return nil
}

// newSnippetTestApp builds an App with n saved hosts ("h0".."h{n-1}") and a
// channel fed with every "snippet:run:progress" payload — the only way
// these tests can observe a run's outcome, since
// RunSnippetOnHosts/CancelSnippetRun report nothing beyond a run id and an
// error.
func newSnippetTestApp(t *testing.T, n int) (*App, []string, chan SnippetRunProgress) {
	t.Helper()
	a := &App{cfgStore: newTestConfigStore(t), ctx: context.Background()}

	hostIDs := make([]string, n)
	hosts := make([]SSHHost, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("h%d", i)
		hostIDs[i] = id
		hosts[i] = SSHHost{ID: id, Alias: id, Host: id + ".example", User: "u"}
	}
	cfg := a.cfgStore.Get()
	cfg.SSHHosts = hosts
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	events := make(chan SnippetRunProgress, 4096)
	a.eventsEmitter = func(_ context.Context, name string, data ...interface{}) {
		if name != "snippet:run:progress" || len(data) == 0 {
			return
		}
		p, ok := data[0].(SnippetRunProgress)
		if !ok {
			return
		}
		events <- p
	}
	return a, hostIDs, events
}

func isTerminalState(s string) bool {
	return s == snippetHostOK || s == snippetHostFailed || s == snippetHostError
}

// drainTerminal reads events until every id in want has reached a terminal
// state, returning the last result seen per host id.
func drainTerminal(t *testing.T, events chan SnippetRunProgress, want []string) map[string]SnippetHostResult {
	t.Helper()
	need := make(map[string]bool, len(want))
	for _, id := range want {
		need[id] = true
	}
	got := make(map[string]SnippetHostResult, len(want))
	deadline := time.After(5 * time.Second)
	for len(need) > 0 {
		select {
		case ev := <-events:
			got[ev.Result.HostID] = ev.Result
			if isTerminalState(ev.Result.State) {
				delete(need, ev.Result.HostID)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for terminal results; still pending: %v", need)
		}
	}
	return got
}

func TestRunSnippetOnHostsCapsConcurrencyAtEight(t *testing.T) {
	const n = 20
	a, hostIDs, events := newSnippetTestApp(t, n)

	entered := make(chan string, n)
	release := make(chan struct{})
	var mu sync.Mutex
	current, max := 0, 0

	a.snippetDialer = func(ctx context.Context, h SSHHost) (snippetConn, error) {
		return fakeSnippetConn{run: func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
			mu.Lock()
			current++
			if current > max {
				max = current
			}
			mu.Unlock()
			entered <- h.ID
			<-release
			mu.Lock()
			current--
			mu.Unlock()
			return sshclient.ExecResult{}, nil
		}}, nil
	}

	if _, err := a.RunSnippetOnHosts(snippetTestTplLabel, snippetTestTplText, hostIDs); err != nil {
		t.Fatalf("RunSnippetOnHosts: %v", err)
	}

	// wantConcurrency is a literal, not snippetMaxConcurrentHosts: the whole
	// point of this test is to catch someone changing that constant, so the
	// expectation has to live independently of it. Using the constant here
	// would make the test self-adjust to any mutation instead of catching it
	// — worse, draining more than the 20 hosts here will ever send would
	// deadlock the test instead of failing it.
	const wantConcurrency = 8

	// Deterministically wait for exactly the cap to be in flight: each Run
	// call reports itself to `entered` before blocking, so receiving 8 of
	// them guarantees 8 concurrent calls existed at that moment — no sleep,
	// no timing. Bounded by a deadline so a broken cap (more or fewer than 8
	// ever entering) fails the test instead of hanging it.
	deadline := time.After(5 * time.Second)
	for i := 0; i < wantConcurrency; i++ {
		select {
		case <-entered:
		case <-deadline:
			t.Fatalf("timed out waiting for %d hosts to start; got %d", wantConcurrency, i)
		}
	}
	// A grace window, not a non-blocking poll: with the cap actually broken,
	// hosts 9-20 are all trying to send on `entered` right now, but "trying"
	// and "having sent" are not the same instant, and a bare `default:` fires
	// before a goroutine that hasn't been scheduled yet gets the chance. 200ms
	// is generous against that scheduling latency while staying far below
	// this test's own 5s deadline.
	select {
	case host := <-entered:
		t.Fatalf("a 9th host (%s) started running before any of the first %d finished", host, wantConcurrency)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	drainTerminal(t, events, hostIDs)

	mu.Lock()
	gotMax := max
	mu.Unlock()
	if gotMax != wantConcurrency {
		t.Fatalf("observed max concurrency = %d, want %d", gotMax, wantConcurrency)
	}
}

func TestRunSnippetOnHostsIsolatesOneHostFailure(t *testing.T) {
	a, hostIDs, events := newSnippetTestApp(t, 4)
	failing := hostIDs[2]

	a.snippetDialer = func(ctx context.Context, h SSHHost) (snippetConn, error) {
		if h.ID == failing {
			return nil, errors.New("boom: dial refused")
		}
		return fakeSnippetConn{run: func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
			return sshclient.ExecResult{ExitCode: 0, Output: []byte("ok")}, nil
		}}, nil
	}

	if _, err := a.RunSnippetOnHosts(snippetTestTplLabel, snippetTestTplText, hostIDs); err != nil {
		t.Fatalf("RunSnippetOnHosts: %v", err)
	}
	got := drainTerminal(t, events, hostIDs)

	if got[failing].State != snippetHostError {
		t.Fatalf("failing host state = %q, want %q", got[failing].State, snippetHostError)
	}
	if got[failing].Error == "" {
		t.Fatalf("failing host has no error message")
	}
	for _, id := range hostIDs {
		if id == failing {
			continue
		}
		if got[id].State != snippetHostOK {
			t.Fatalf("host %s state = %q, want %q (must be untouched by %s's failure)", id, got[id].State, snippetHostOK, failing)
		}
	}
}

func TestRunSnippetOnHostsNonZeroExitIsFailedNotError(t *testing.T) {
	a, hostIDs, events := newSnippetTestApp(t, 1)
	a.snippetDialer = func(ctx context.Context, h SSHHost) (snippetConn, error) {
		return fakeSnippetConn{run: func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
			return sshclient.ExecResult{ExitCode: 7, Output: []byte("nope")}, nil
		}}, nil
	}
	if _, err := a.RunSnippetOnHosts(snippetTestTplLabel, snippetTestTplText, hostIDs); err != nil {
		t.Fatalf("RunSnippetOnHosts: %v", err)
	}
	got := drainTerminal(t, events, hostIDs)[hostIDs[0]]
	if got.State != snippetHostFailed {
		t.Fatalf("state = %q, want %q", got.State, snippetHostFailed)
	}
	if got.ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7", got.ExitCode)
	}
	if got.Error != "" {
		t.Fatalf("a non-zero exit must not set Error, got %q", got.Error)
	}
}

func TestRunSnippetOnHostsEmitsRunningThenTerminalPerHost(t *testing.T) {
	a, hostIDs, events := newSnippetTestApp(t, 3)
	a.snippetDialer = func(ctx context.Context, h SSHHost) (snippetConn, error) {
		return fakeSnippetConn{run: func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
			return sshclient.ExecResult{ExitCode: 0}, nil
		}}, nil
	}
	if _, err := a.RunSnippetOnHosts(snippetTestTplLabel, snippetTestTplText, hostIDs); err != nil {
		t.Fatalf("RunSnippetOnHosts: %v", err)
	}

	seq := map[string][]string{}
	need := len(hostIDs) * 2
	deadline := time.After(5 * time.Second)
	for i := 0; i < need; i++ {
		select {
		case ev := <-events:
			seq[ev.Result.HostID] = append(seq[ev.Result.HostID], ev.Result.State)
		case <-deadline:
			t.Fatalf("timed out; got so far: %v", seq)
		}
	}
	for _, id := range hostIDs {
		states := seq[id]
		if len(states) != 2 {
			t.Fatalf("host %s: got %d events %v, want 2", id, len(states), states)
		}
		if states[0] != snippetHostRunning {
			t.Fatalf("host %s: first event = %q, want %q", id, states[0], snippetHostRunning)
		}
		if !isTerminalState(states[1]) {
			t.Fatalf("host %s: second event = %q, want a terminal state", id, states[1])
		}
	}
}

func TestRunSnippetOnHostsRejectsEmptySnippetText(t *testing.T) {
	a, hostIDs, _ := newSnippetTestApp(t, 1)
	dialCalls := int32(0)
	a.snippetDialer = func(ctx context.Context, h SSHHost) (snippetConn, error) {
		atomic.AddInt32(&dialCalls, 1)
		return fakeSnippetConn{run: func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
			return sshclient.ExecResult{}, nil
		}}, nil
	}
	runID, err := a.RunSnippetOnHosts(snippetTestTplLabel, "", hostIDs)
	if err == nil {
		t.Fatal("expected an error for empty snippet text")
	}
	if runID != "" {
		t.Fatalf("expected empty run id on rejection, got %q", runID)
	}
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&dialCalls) != 0 {
		t.Fatalf("dialer was called %d times; a rejected run must start nothing", dialCalls)
	}
}

func TestRunSnippetOnHostsRejectsEmptyHostList(t *testing.T) {
	a, _, _ := newSnippetTestApp(t, 0)
	runID, err := a.RunSnippetOnHosts(snippetTestTplLabel, snippetTestTplText, nil)
	if err == nil {
		t.Fatal("expected an error for an empty host list")
	}
	if runID != "" {
		t.Fatalf("expected empty run id on rejection, got %q", runID)
	}
}

func TestRunSnippetOnHostsUnknownHostBecomesErrorResult(t *testing.T) {
	a, hostIDs, events := newSnippetTestApp(t, 2)
	all := append(append([]string{}, hostIDs...), "ghost-host")

	dialCalls := int32(0)
	a.snippetDialer = func(ctx context.Context, h SSHHost) (snippetConn, error) {
		atomic.AddInt32(&dialCalls, 1)
		return fakeSnippetConn{run: func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
			return sshclient.ExecResult{ExitCode: 0}, nil
		}}, nil
	}

	if _, err := a.RunSnippetOnHosts(snippetTestTplLabel, snippetTestTplText, all); err != nil {
		t.Fatalf("RunSnippetOnHosts: %v", err)
	}
	got := drainTerminal(t, events, all)

	if got["ghost-host"].State != snippetHostError {
		t.Fatalf("unknown host state = %q, want %q", got["ghost-host"].State, snippetHostError)
	}
	if atomic.LoadInt32(&dialCalls) != int32(len(hostIDs)) {
		t.Fatalf("dialer called %d times, want %d (the unknown host must never be dialled)", dialCalls, len(hostIDs))
	}
}

func TestRunSnippetOnHostsTimesOutOneHostWithoutStallingTheRun(t *testing.T) {
	a, hostIDs, events := newSnippetTestApp(t, 3)
	slow := hostIDs[0]
	unblock := make(chan struct{})
	t.Cleanup(func() { close(unblock) })

	// Reported over a channel, not a flag checked after the fact: the slow
	// host's goroutine can legitimately be scheduled after the other two have
	// already finished and been drained, and a flag read at that point would
	// race with the write. This waits for the actual event instead.
	// wantHostTimeout is a literal, not snippetHostTimeout, for the same
	// reason wantConcurrency is one in the concurrency test above: this test
	// exists to catch someone changing that constant, so referencing it here
	// would make the check self-adjust to the very mutation it must catch.
	const wantHostTimeout = 60 * time.Second

	deadlineCh := make(chan bool, 1)
	a.snippetDialer = func(ctx context.Context, h SSHHost) (snippetConn, error) {
		return fakeSnippetConn{run: func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
			if h.ID == slow {
				dl, ok := ctx.Deadline()
				// Bounded on BOTH sides. An upper bound alone passes just as
				// happily against a 1-second timeout as against the real one,
				// so it catches only half of "someone changed the constant".
				// The lower bound is slack enough (50s) to absorb scheduling
				// between WithTimeout and this read.
				remaining := time.Until(dl)
				deadlineCh <- ok && remaining <= wantHostTimeout && remaining >= wantHostTimeout-10*time.Second
				select {
				case <-unblock:
				case <-ctx.Done():
				}
				return sshclient.ExecResult{}, ctx.Err()
			}
			return sshclient.ExecResult{ExitCode: 0}, nil
		}}, nil
	}

	start := time.Now()
	if _, err := a.RunSnippetOnHosts(snippetTestTplLabel, snippetTestTplText, hostIDs); err != nil {
		t.Fatalf("RunSnippetOnHosts: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("RunSnippetOnHosts blocked for %v; it must return immediately", elapsed)
	}

	select {
	case ok := <-deadlineCh:
		if !ok {
			t.Fatal("the stuck host's context did not carry a deadline bounded by snippetHostTimeout")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the stuck host's Run was never called")
	}

	others := []string{hostIDs[1], hostIDs[2]}
	got := drainTerminal(t, events, others)
	for _, id := range others {
		if got[id].State != snippetHostOK {
			t.Fatalf("host %s state = %q, want %q; a stuck host must not stall the others", id, got[id].State, snippetHostOK)
		}
	}
}

// TestCancelSnippetRunLeavesFinishedResultsAlone is the test this name
// promises: some hosts must genuinely reach a terminal state — via events
// actually observed, not merely "cancel was called after some arbitrary
// delay" — before CancelSnippetRun runs, or the "leaves finished results
// alone" guard is never exercised at all. It also still covers the original
// "hosts still queued behind the cap resolve to error" half.
func TestCancelSnippetRunLeavesFinishedResultsAlone(t *testing.T) {
	const n = snippetMaxConcurrentHosts + 2 // guarantees some hosts stay queued behind the cap
	const wantFast = 3                      // how many of the running hosts are told to finish for real before cancel

	a, hostIDs, events := newSnippetTestApp(t, n)

	entered := make(chan string, n)
	// One release gate per host, closed individually so the test controls
	// exactly which hosts finish before cancel and which stay blocked. A
	// single shared gate cannot do this: closing it unblocks everyone at
	// once, including whichever of the still-queued hosts races into a
	// slot the moment one frees up (semaphore slots are up for grabs the
	// instant any running host returns — see below), and that would let a
	// host meant to stay queued finish "ok" too, defeating the very split
	// this test needs.
	release := make(map[string]chan struct{}, n)
	for _, id := range hostIDs {
		release[id] = make(chan struct{})
	}

	a.snippetDialer = func(ctx context.Context, h SSHHost) (snippetConn, error) {
		return fakeSnippetConn{run: func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
			entered <- h.ID
			select {
			case <-release[h.ID]:
				return sshclient.ExecResult{ExitCode: 0}, nil
			case <-ctx.Done():
				return sshclient.ExecResult{}, ctx.Err()
			}
		}}, nil
	}

	runID, err := a.RunSnippetOnHosts(snippetTestTplLabel, snippetTestTplText, hostIDs)
	if err != nil {
		t.Fatalf("RunSnippetOnHosts: %v", err)
	}
	// Captured now, while the run is definitely still registered, so reading
	// it after cancel does not race the completion-pruning goroutine (which
	// can delete this run's map entry the instant its last host resolves).
	// The struct itself stays valid through this pointer regardless of map
	// membership.
	a.snippetRunsMu.Lock()
	run := a.snippetRuns[runID]
	a.snippetRunsMu.Unlock()

	// Nothing has returned yet at this point — no semaphore slot has freed —
	// so this is the one moment "first 8 to enter == the running set, the
	// other 2 are genuinely still queued" is guaranteed rather than merely
	// likely.
	runningOrder := make([]string, 0, snippetMaxConcurrentHosts)
	running := make(map[string]bool, snippetMaxConcurrentHosts)
	for i := 0; i < snippetMaxConcurrentHosts; i++ {
		id := <-entered
		running[id] = true
		runningOrder = append(runningOrder, id)
	}
	var pending []string
	for _, id := range hostIDs {
		if !running[id] {
			pending = append(pending, id)
		}
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending hosts, got %d: %v", len(pending), pending)
	}

	// Let exactly wantFast of the running hosts finish for real, and wait for
	// their terminal events — the same channel a real client would use, not
	// inferred from timing. Everyone else's gate stays shut; only cancel (or
	// this test never calling it) will move them.
	fastHosts := append([]string(nil), runningOrder[:wantFast]...)
	for _, id := range fastHosts {
		close(release[id])
	}
	drainTerminal(t, events, fastHosts)

	if err := a.CancelSnippetRun(runID); err != nil {
		t.Fatalf("CancelSnippetRun: %v", err)
	}

	// The hosts that already finished must be untouched by cancel — the one
	// guarantee this test exists to pin down. Read live state via the run
	// struct, not the event captured before cancel: a cancelPending that
	// forgot to check state would silently rewrite this entry after the
	// event already fired, and a check against the stale event would never
	// see that.
	for _, id := range fastHosts {
		res := run.snapshot(id)
		if res.State != snippetHostOK {
			t.Fatalf("finished host %s state = %q, want %q (cancel must not touch it)", id, res.State, snippetHostOK)
		}
	}

	// Everything else — the 2 hosts that were still queued, and the 5
	// running hosts whose gate was never opened — must resolve to "error".
	// A queued host may or may not win a semaphore slot freed by a fast
	// finisher before cancel reaches it (the semaphore does not know or
	// care which hosts this test meant to keep queued); either way its gate
	// is never closed, so it only ever unblocks via ctx cancellation and
	// still ends up "error" — which is the outcome this assertion checks,
	// not the path taken to get there.
	rest := make([]string, 0, n-wantFast)
	isFast := make(map[string]bool, wantFast)
	for _, id := range fastHosts {
		isFast[id] = true
	}
	for _, id := range hostIDs {
		if !isFast[id] {
			rest = append(rest, id)
		}
	}
	restResults := drainTerminal(t, events, rest)
	for _, id := range rest {
		res := restResults[id]
		if res.State != snippetHostError {
			t.Fatalf("host %s state = %q, want %q", id, res.State, snippetHostError)
		}
		if res.Error == "" {
			t.Fatalf("host %s has no cancellation/timeout message", id)
		}
	}

	if err := a.CancelSnippetRun("no-such-run"); err == nil {
		t.Fatal("expected an error cancelling an unknown run id")
	}
}

func TestRunSnippetOnHostsClosesEveryConn(t *testing.T) {
	a, hostIDs, events := newSnippetTestApp(t, 5)
	failDial := hostIDs[4] // dial itself fails: nothing to close for this one

	var closed int32
	a.snippetDialer = func(ctx context.Context, h SSHHost) (snippetConn, error) {
		if h.ID == failDial {
			return nil, errors.New("dial refused")
		}
		exitCode := 0
		if h.ID == hostIDs[1] {
			exitCode = 3 // a "failed" (non-zero exit) host must still be closed
		}
		return fakeSnippetConn{
			run: func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
				if h.ID == hostIDs[2] {
					return sshclient.ExecResult{}, errors.New("run: dropped mid-command") // "error" host must still be closed
				}
				return sshclient.ExecResult{ExitCode: exitCode}, nil
			},
			onClose: func() { atomic.AddInt32(&closed, 1) },
		}, nil
	}

	if _, err := a.RunSnippetOnHosts(snippetTestTplLabel, snippetTestTplText, hostIDs); err != nil {
		t.Fatalf("RunSnippetOnHosts: %v", err)
	}
	drainTerminal(t, events, hostIDs)

	want := int32(len(hostIDs) - 1) // every host except the one whose dial failed
	// Poll rather than read once. conn.Close runs in a defer registered after
	// the terminal event is emitted, so a host can be drained here while its
	// Close is still a scheduling instant away — a single read fails ~25% of
	// the time under -count=20. The bound still fails the test if a Close
	// genuinely never happens; it only refuses to race the last one.
	deadline := time.Now().Add(2 * time.Second)
	var got int32
	for time.Now().Before(deadline) {
		if got = atomic.LoadInt32(&closed); got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("closed %d connections, want %d", got, want)
}

// TestRunSnippetOnHostsPrunesCompletedRunFromMap pins MAJOR 1: snippetRuns
// exists to route a cancellation to a *live* run, and a finished run has
// nothing left to cancel — its entry must not sit in the map pinning every
// host's Output (up to 256KB apiece) forever in a long-lived desktop process.
func TestRunSnippetOnHostsPrunesCompletedRunFromMap(t *testing.T) {
	a, hostIDs, events := newSnippetTestApp(t, 6)
	a.snippetDialer = func(ctx context.Context, h SSHHost) (snippetConn, error) {
		return fakeSnippetConn{run: func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
			return sshclient.ExecResult{ExitCode: 0}, nil
		}}, nil
	}

	runID, err := a.RunSnippetOnHosts(snippetTestTplLabel, snippetTestTplText, hostIDs)
	if err != nil {
		t.Fatalf("RunSnippetOnHosts: %v", err)
	}
	drainTerminal(t, events, hostIDs)

	// The map entry is deleted by a background goroutine after wg.Wait(),
	// which runs shortly after the last terminal event but with no direct
	// happens-before relationship to this goroutine observing it — so this
	// polls with a bound, rather than asserting on a single read right after
	// drainTerminal returns.
	deadline := time.Now().Add(2 * time.Second)
	for {
		a.snippetRunsMu.Lock()
		_, exists := a.snippetRuns[runID]
		remaining := len(a.snippetRuns)
		a.snippetRunsMu.Unlock()
		if !exists {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s still present in snippetRuns %v after completing", runID, remaining)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := a.CancelSnippetRun(runID); err == nil {
		t.Fatal("expected an error cancelling a run that already completed and was pruned")
	}
}

// TestRunSnippetOnHostsPassesTemplateTextAndOutputCap pins MAJOR 4: nothing
// else in this suite checks what command actually gets run or what output cap
// it is run with, so a call like conn.Run(hostCtx, "rm -rf /", 0) would leave
// every other test green.
func TestRunSnippetOnHostsPassesTemplateTextAndOutputCap(t *testing.T) {
	a, hostIDs, events := newSnippetTestApp(t, 1)
	var mu sync.Mutex
	var gotCmd string
	var gotLimit int64
	a.snippetDialer = func(ctx context.Context, h SSHHost) (snippetConn, error) {
		return fakeSnippetConn{run: func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
			mu.Lock()
			gotCmd, gotLimit = cmd, limit
			mu.Unlock()
			return sshclient.ExecResult{ExitCode: 0}, nil
		}}, nil
	}

	if _, err := a.RunSnippetOnHosts(snippetTestTplLabel, snippetTestTplText, hostIDs); err != nil {
		t.Fatalf("RunSnippetOnHosts: %v", err)
	}
	drainTerminal(t, events, hostIDs)

	mu.Lock()
	defer mu.Unlock()
	if gotCmd != snippetTestTplText {
		t.Fatalf("cmd = %q, want the snippet's own text %q", gotCmd, snippetTestTplText)
	}
	// 262144 as a hardcoded literal, not snippetMaxOutputBytes: this test
	// exists to catch someone changing that constant, or the call site's use
	// of it, out from under the batch run — referencing the same constant
	// here would let both drift together and still pass.
	const wantOutputCap = 262144
	if gotLimit != wantOutputCap {
		t.Fatalf("limit = %d, want %d", gotLimit, wantOutputCap)
	}
}

// TestRunSnippetOnHostsKeepsPartialOutputOnRunError pins MINOR 7: Run's own
// contract is that a non-nil error can still carry whatever the host printed
// before it dropped (see internal/sshclient/exec.go), and finishSnippetHost
// must keep that instead of discarding it just because err != nil.
func TestRunSnippetOnHostsKeepsPartialOutputOnRunError(t *testing.T) {
	a, hostIDs, events := newSnippetTestApp(t, 1)
	a.snippetDialer = func(ctx context.Context, h SSHHost) (snippetConn, error) {
		return fakeSnippetConn{run: func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
			return sshclient.ExecResult{Output: []byte("partial-output-before-drop")}, errors.New("connection dropped mid-command")
		}}, nil
	}

	if _, err := a.RunSnippetOnHosts(snippetTestTplLabel, snippetTestTplText, hostIDs); err != nil {
		t.Fatalf("RunSnippetOnHosts: %v", err)
	}
	got := drainTerminal(t, events, hostIDs)[hostIDs[0]]
	if got.State != snippetHostError {
		t.Fatalf("state = %q, want %q", got.State, snippetHostError)
	}
	if got.Output != "partial-output-before-drop" {
		t.Fatalf("Output = %q, want the partial bytes Run returned alongside its error", got.Output)
	}
}

// TestRunSnippetOnHostsResolvesPendingHostsWhenParentContextEnds pins MINOR 8:
// cancelPending only runs from CancelSnippetRun, but runCtx can also end
// because a.ctx itself was cancelled — app teardown, not a user-initiated
// cancel — and a host still queued behind the cap at that moment must still
// resolve instead of being stuck at "pending" forever.
func TestRunSnippetOnHostsResolvesPendingHostsWhenParentContextEnds(t *testing.T) {
	const n = snippetMaxConcurrentHosts + 2
	appCtx, appCancel := context.WithCancel(context.Background())
	t.Cleanup(appCancel)

	a, hostIDs, events := newSnippetTestApp(t, n)
	a.ctx = appCtx

	entered := make(chan string, n)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	a.snippetDialer = func(ctx context.Context, h SSHHost) (snippetConn, error) {
		return fakeSnippetConn{run: func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
			entered <- h.ID
			select {
			case <-release:
				return sshclient.ExecResult{ExitCode: 0}, nil
			case <-ctx.Done():
				return sshclient.ExecResult{}, ctx.Err()
			}
		}}, nil
	}

	if _, err := a.RunSnippetOnHosts(snippetTestTplLabel, snippetTestTplText, hostIDs); err != nil {
		t.Fatalf("RunSnippetOnHosts: %v", err)
	}
	for i := 0; i < snippetMaxConcurrentHosts; i++ {
		<-entered
	}

	// Nothing calls CancelSnippetRun here — the parent context itself ends,
	// as it would at app teardown mid-run.
	appCancel()

	got := drainTerminal(t, events, hostIDs)
	for _, id := range hostIDs {
		if got[id].State != snippetHostError {
			t.Fatalf("host %s state = %q, want %q after the parent context ended", id, got[id].State, snippetHostError)
		}
	}
}

// TestRunSnippetOnHostsResolvesLabelForHostsStillQueued pins MINOR 10: a host
// still "pending" (or later cancelled while still queued) must show its real
// alias, not the raw host id — resolveHostLabel is what fills that in at
// RunSnippetOnHosts time, before any goroutine has had the chance to resolve
// the host for real.
func TestRunSnippetOnHostsResolvesLabelForHostsStillQueued(t *testing.T) {
	const n = snippetMaxConcurrentHosts + 1
	a, hostIDs, events := newSnippetTestApp(t, n)

	cfg := a.cfgStore.Get()
	aliasOf := make(map[string]string, n)
	for i := range cfg.SSHHosts {
		alias := fmt.Sprintf("alias-%d", i) // distinct from the "h%d" id newSnippetTestApp uses as the default alias too
		cfg.SSHHosts[i].Alias = alias
		aliasOf[cfg.SSHHosts[i].ID] = alias
	}
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	entered := make(chan string, n)
	// Closed exactly once, explicitly, once this test is done reading the
	// pending host's state — not also registered via t.Cleanup, which would
	// close it a second time and panic.
	release := make(chan struct{})
	a.snippetDialer = func(ctx context.Context, h SSHHost) (snippetConn, error) {
		return fakeSnippetConn{run: func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
			entered <- h.ID
			<-release
			return sshclient.ExecResult{ExitCode: 0}, nil
		}}, nil
	}

	runID, err := a.RunSnippetOnHosts(snippetTestTplLabel, snippetTestTplText, hostIDs)
	if err != nil {
		t.Fatalf("RunSnippetOnHosts: %v", err)
	}
	a.snippetRunsMu.Lock()
	run := a.snippetRuns[runID]
	a.snippetRunsMu.Unlock()

	running := make(map[string]bool, snippetMaxConcurrentHosts)
	for i := 0; i < snippetMaxConcurrentHosts; i++ {
		running[<-entered] = true
	}
	var pending string
	for _, id := range hostIDs {
		if !running[id] {
			pending = id
			break
		}
	}
	if pending == "" {
		t.Fatal("expected exactly one host to still be queued behind the cap")
	}

	res := run.snapshot(pending)
	if res.State != snippetHostPending {
		t.Fatalf("host %s state = %q, want %q", pending, res.State, snippetHostPending)
	}
	if want := aliasOf[pending]; res.HostLabel != want {
		t.Fatalf("pending HostLabel = %q, want its alias %q, not the raw host id", res.HostLabel, want)
	}

	close(release)
	drainTerminal(t, events, hostIDs)
}

// TestRunSnippetOnHostsDedupesDuplicateHostIDs pins MINOR 11: a repeated id in
// the selection must not spawn a second goroutine that silently loses the
// race for the one results-map entry and reports nothing.
func TestDedupeHostIDsKeepsFirstOccurrenceOrder(t *testing.T) {
	got := dedupeHostIDs([]string{"b", "a", "b", "c", "a"})
	want := []string{"b", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf("dedupeHostIDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dedupeHostIDs = %v, want %v (order must be first-occurrence: the UI lists hosts in the order the user picked them)", got, want)
		}
	}
	if got := dedupeHostIDs(nil); len(got) != 0 {
		t.Fatalf("dedupeHostIDs(nil) = %v, want empty", got)
	}
}

// Pins that a repeated host id does not double-dial. Note what this does NOT
// pin: removing the dedupe call from RunSnippetOnHosts leaves it green,
// because markRunning already lets only one goroutine per id past, and the
// loser returns before dialling. The only thing dedupe changes is transient —
// N duplicates burning N semaphore slots on goroutines that do nothing — and
// that is not deterministically observable from out here. The dedupe helper
// itself is pinned by TestDedupeHostIDsKeepsFirstOccurrenceOrder above.
func TestRunSnippetOnHostsDedupesDuplicateHostIDs(t *testing.T) {
	a, hostIDs, events := newSnippetTestApp(t, 2)
	var dialCalls int32
	a.snippetDialer = func(ctx context.Context, h SSHHost) (snippetConn, error) {
		atomic.AddInt32(&dialCalls, 1)
		return fakeSnippetConn{run: func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
			return sshclient.ExecResult{ExitCode: 0}, nil
		}}, nil
	}

	dup := append(append([]string{}, hostIDs...), hostIDs[0]) // hostIDs[0] listed twice
	if _, err := a.RunSnippetOnHosts(snippetTestTplLabel, snippetTestTplText, dup); err != nil {
		t.Fatalf("RunSnippetOnHosts: %v", err)
	}
	drainTerminal(t, events, hostIDs)

	if got := atomic.LoadInt32(&dialCalls); got != int32(len(hostIDs)) {
		t.Fatalf("dialer called %d times, want %d — a duplicate host id must not double-dial", got, len(hostIDs))
	}
}

// TestRunSnippetOnHostsSuppressesTOFUAndSurfacesReadableMessage pins MAJOR 5:
// it deliberately leaves snippetDialer unset so RunSnippetOnHosts falls back
// to the real dialSnippetConn — a fake standing in for it, the way every
// other test in this file uses one, can never regress the errors.As
// conversion this test exists to protect, because the fake never goes near
// it. Deleting that conversion would let a raw *HostKeyUnknownError (whose
// Error() is the errCodeHostKeyUnknown sentinel the frontend's TOFU dialog
// keys on) leak out of a batch run, which has no dialog to show it in.
func TestRunSnippetOnHostsSuppressesTOFUAndSurfacesReadableMessage(t *testing.T) {
	srv := startForwardingSSHTestServerAs(t, "u", "pw")
	a := newJumpTestApp(t) // trusts nothing — the host's key is "unknown"
	host := addServerHost(t, a, "unseen", srv, "u", "pw", "")

	events := make(chan SnippetRunProgress, 16)
	a.eventsEmitter = func(_ context.Context, name string, data ...interface{}) {
		if name != "snippet:run:progress" || len(data) == 0 {
			return
		}
		if p, ok := data[0].(SnippetRunProgress); ok {
			events <- p
		}
	}

	if _, err := a.RunSnippetOnHosts(snippetTestTplLabel, snippetTestTplText, []string{host.ID}); err != nil {
		t.Fatalf("RunSnippetOnHosts: %v", err)
	}
	got := drainTerminal(t, events, []string{host.ID})[host.ID]

	if got.State != snippetHostError {
		t.Fatalf("state = %q, want %q", got.State, snippetHostError)
	}
	if strings.Contains(got.Error, errCodeHostKeyUnknown) {
		t.Fatalf("error leaked the raw TOFU sentinel %q instead of a readable message: %q", errCodeHostKeyUnknown, got.Error)
	}
	if !strings.Contains(got.Error, "not trusted yet") {
		t.Fatalf("error = %q, want it to explain the host key is not trusted yet", got.Error)
	}
}

func TestSnippetHostKeyErrorMessage(t *testing.T) {
	e := &HostKeyUnknownError{Host: "10.0.0.9:22", Fingerprint: "SHA256:abc"}
	err := snippetHostKeyError(e)
	if err == nil {
		t.Fatal("expected a non-nil error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "not trusted yet") || !strings.Contains(msg, e.Fingerprint) || !strings.Contains(msg, e.Host) {
		t.Fatalf("message %q missing expected content (host/fingerprint/not-trusted-yet)", msg)
	}
}
