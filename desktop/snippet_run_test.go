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

const snippetTestTplID = "tpl1"
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

// newSnippetTestApp builds an App with n saved hosts ("h0".."h{n-1}"), one
// quick template, and a channel fed with every "snippet:run:progress"
// payload — the only way these tests can observe a run's outcome, since
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
	cfg.QuickTemplates = []QuickTemplate{{ID: snippetTestTplID, Label: "Test", Text: snippetTestTplText}}
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

	if _, err := a.RunSnippetOnHosts(snippetTestTplID, hostIDs); err != nil {
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
	select {
	case host := <-entered:
		t.Fatalf("a 9th host (%s) started running before any of the first %d finished", host, wantConcurrency)
	default:
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

	if _, err := a.RunSnippetOnHosts(snippetTestTplID, hostIDs); err != nil {
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
	if _, err := a.RunSnippetOnHosts(snippetTestTplID, hostIDs); err != nil {
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
	if _, err := a.RunSnippetOnHosts(snippetTestTplID, hostIDs); err != nil {
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

func TestRunSnippetOnHostsRejectsUnknownSnippet(t *testing.T) {
	a, hostIDs, _ := newSnippetTestApp(t, 1)
	dialCalls := int32(0)
	a.snippetDialer = func(ctx context.Context, h SSHHost) (snippetConn, error) {
		atomic.AddInt32(&dialCalls, 1)
		return fakeSnippetConn{run: func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
			return sshclient.ExecResult{}, nil
		}}, nil
	}
	runID, err := a.RunSnippetOnHosts("does-not-exist", hostIDs)
	if err == nil {
		t.Fatal("expected an error for an unknown snippet id")
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
	runID, err := a.RunSnippetOnHosts(snippetTestTplID, nil)
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

	if _, err := a.RunSnippetOnHosts(snippetTestTplID, all); err != nil {
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
	deadlineCh := make(chan bool, 1)
	a.snippetDialer = func(ctx context.Context, h SSHHost) (snippetConn, error) {
		return fakeSnippetConn{run: func(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
			if h.ID == slow {
				dl, ok := ctx.Deadline()
				deadlineCh <- ok && time.Until(dl) <= snippetHostTimeout && time.Until(dl) > 0
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
	if _, err := a.RunSnippetOnHosts(snippetTestTplID, hostIDs); err != nil {
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

func TestCancelSnippetRunLeavesFinishedResultsAlone(t *testing.T) {
	const n = snippetMaxConcurrentHosts + 2 // guarantees some hosts stay pending
	a, hostIDs, events := newSnippetTestApp(t, n)

	entered := make(chan string, n)
	release := make(chan struct{}) // never closed in this test; ctx cancellation is what unblocks these

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

	runID, err := a.RunSnippetOnHosts(snippetTestTplID, hostIDs)
	if err != nil {
		t.Fatalf("RunSnippetOnHosts: %v", err)
	}

	running := make(map[string]bool, snippetMaxConcurrentHosts)
	for i := 0; i < snippetMaxConcurrentHosts; i++ {
		running[<-entered] = true
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

	if err := a.CancelSnippetRun(runID); err != nil {
		t.Fatalf("CancelSnippetRun: %v", err)
	}

	// cancelPending runs synchronously inside CancelSnippetRun, under the
	// run's own lock, so the pending hosts' results are already final the
	// moment CancelSnippetRun returns — no event wait needed for these two.
	a.snippetRunsMu.Lock()
	run := a.snippetRuns[runID]
	a.snippetRunsMu.Unlock()
	for _, id := range pending {
		res := run.snapshot(id)
		if res.State != snippetHostError {
			t.Fatalf("pending host %s state = %q, want %q", id, res.State, snippetHostError)
		}
		if res.Error == "" {
			t.Fatalf("pending host %s has no cancellation message", id)
		}
	}

	// The still-running hosts unblock via ctx cancellation and finish as
	// "error" too; that is expected (their in-flight run really was cut
	// short) and is not what this test pins.
	drainTerminal(t, events, hostIDs)

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

	if _, err := a.RunSnippetOnHosts(snippetTestTplID, hostIDs); err != nil {
		t.Fatalf("RunSnippetOnHosts: %v", err)
	}
	drainTerminal(t, events, hostIDs)

	want := int32(len(hostIDs) - 1) // every host except the one whose dial failed
	if got := atomic.LoadInt32(&closed); got != want {
		t.Fatalf("closed %d connections, want %d", got, want)
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
