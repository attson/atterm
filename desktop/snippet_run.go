package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/attson/atterm/internal/sshclient"
	"github.com/google/uuid"
)

// Batch snippet runs (roadmap item: run one quick-action snippet on many
// hosts at once).
//
// Every host goes through the same three gates a lone connection does —
// findable in the saved store, not a ProxyCommand host, host key already
// trusted — because dialSnippetConn is built on dialThroughJumps, the same
// chain builder the terminal and tunnel paths use. That is deliberate: a
// second implementation of ProxyJump resolution or the ProxyCommand refusal
// is how the two paths drift and a host becomes connectable one way but not
// the other (see hostRunsProxyCommand's doc comment on why there are exactly
// two callers of the gate — connectableSSHHost is one of them, and this file
// reuses it rather than adding a third).
//
// One host is never allowed to affect another's outcome or the run's
// progress: each host dials, runs and closes on its own goroutine, gated only
// by a semaphore that bounds how many run at once. A slow or wedged host
// times out on its own context and never blocks the others past the
// concurrency cap.

const (
	snippetHostPending = "pending"
	snippetHostRunning = "running"
	snippetHostOK      = "ok"
	snippetHostFailed  = "failed" // ran, non-zero exit
	snippetHostError   = "error"  // never ran (or never finished) at all
)

const (
	// snippetMaxConcurrentHosts bounds how many hosts a run dials and runs a
	// command on at the same time. Unbounded fan-out against a host list of
	// any real size turns "run this on my fleet" into a login-rate spike big
	// enough to look like a credential-stuffing attempt against every host at
	// once; 8 is generous for a quick-action snippet without being that.
	snippetMaxConcurrentHosts = 8

	// snippetHostTimeout bounds one host's dial+run, independent of every
	// other host's. Without a per-host bound, one unresponsive host (network
	// black hole, a command that never exits) would hold its semaphore slot
	// forever, and on a large enough host list that is indistinguishable from
	// the whole run being stuck.
	snippetHostTimeout = 60 * time.Second

	// snippetMaxOutputBytes caps what one host's result carries back. A batch
	// run's output rides one progress event per host, not a stream the user
	// scrolls interactively the way a terminal's does — there is no reason
	// for a runaway command on one host to hold multiple megabytes in memory
	// per result, times up to snippetMaxConcurrentHosts of them in flight.
	snippetMaxOutputBytes = 256 * 1024
)

// SnippetHostResult is one host's outcome, as of the last state change.
// Truncated and ExitCode are only meaningful once State has left "pending"/
// "running": a dial error or a cancellation before the command ever started
// leaves both zero.
type SnippetHostResult struct {
	HostID    string `json:"host_id"`
	HostLabel string `json:"host_label"`
	State     string `json:"state"`
	ExitCode  int    `json:"exit_code"`
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
	Error     string `json:"error,omitempty"`
}

// SnippetRunProgress is the payload of the "snippet:run:progress" event,
// pushed once a host enters "running" and again when it reaches a terminal
// state ("ok"/"failed"/"error"). The frontend keys a row on RunID +
// Result.HostID; there is no separate "list results" call, so this event
// stream is the only way a client learns anything about a run in progress.
type SnippetRunProgress struct {
	RunID  string            `json:"run_id"`
	Result SnippetHostResult `json:"result"`
}

// snippetConn is the surface RunSnippetOnHosts needs from a dialed host: run
// one command, then release whatever the dial opened. It exists so tests can
// substitute a fake instead of a live SSH server — the same reason App.host
// is not touched by this file at all (see the injectable dialer below).
type snippetConn interface {
	Run(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error)
	Close() error
}

// snippetRun is one batch run: the id handed back to the caller, the
// cancellation that reaches every host still in flight, and the
// mutex-guarded per-host results a live run's goroutines and CancelSnippetRun
// both write into.
type snippetRun struct {
	id     string
	cancel context.CancelFunc

	mu      sync.Mutex
	results map[string]SnippetHostResult
}

// snapshot returns a copy of one host's current result.
func (r *snippetRun) snapshot(hostID string) SnippetHostResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.results[hostID]
}

// markRunning transitions a host from "pending" to "running", reporting
// false when it was not pending — meaning CancelSnippetRun already moved it
// to "error" while it was still queued behind the semaphore. That false is
// the goroutine's signal to do nothing further: proceeding would overwrite a
// cancellation result with real work the cancel already promised would not
// happen.
func (r *snippetRun) markRunning(hostID string) (SnippetHostResult, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	res, ok := r.results[hostID]
	if !ok || res.State != snippetHostPending {
		return SnippetHostResult{}, false
	}
	res.State = snippetHostRunning
	r.results[hostID] = res
	return res, true
}

// setTerminal records a host's final result, whatever state it ends in.
func (r *snippetRun) setTerminal(result SnippetHostResult) {
	r.mu.Lock()
	r.results[result.HostID] = result
	r.mu.Unlock()
}

// cancelPending moves every still-pending host straight to "error" and
// returns the ones it changed, for the caller to emit progress on. A
// "pending" host has not been dialled at all — there is no in-flight dial or
// command for ctx cancellation to unwind, so without this sweep a cancelled
// run would leave queued hosts silently stuck at "pending" forever instead of
// resolving.
func (r *snippetRun) cancelPending(reason string) []SnippetHostResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	var changed []SnippetHostResult
	for hostID, res := range r.results {
		if res.State != snippetHostPending {
			continue
		}
		res.State = snippetHostError
		res.Error = reason
		r.results[hostID] = res
		changed = append(changed, res)
	}
	return changed
}

// RunSnippetOnHosts runs the quick template identified by snippetID on every
// host in hostIDs, fanning out with a bound on how many run at once, and
// returns a run id progress events are tagged with (see SnippetRunProgress).
// It returns as soon as the run is registered — callers get results only
// through the event stream, never by blocking here.
//
// Rejecting an unknown snippet or an empty host list here, before any
// goroutine starts, is what "starts nothing" means: a run id is only ever
// handed out for a run that is actually going to try every host in the list.
func (a *App) RunSnippetOnHosts(snippetID string, hostIDs []string) (string, error) {
	if len(hostIDs) == 0 {
		return "", errors.New("snippet run: no hosts selected")
	}
	tpl, ok := findQuickTemplate(a.GetQuickTemplates(), snippetID)
	if !ok {
		return "", fmt.Errorf("snippet run: unknown snippet %q", snippetID)
	}

	dial := a.snippetDialer
	if dial == nil {
		dial = a.dialSnippetConn
	}

	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)

	run := &snippetRun{
		id:      uuid.NewString(),
		cancel:  cancel,
		results: make(map[string]SnippetHostResult, len(hostIDs)),
	}
	for _, hostID := range hostIDs {
		run.results[hostID] = SnippetHostResult{HostID: hostID, HostLabel: hostID, State: snippetHostPending}
	}

	a.snippetRunsMu.Lock()
	if a.snippetRuns == nil {
		a.snippetRuns = map[string]*snippetRun{}
	}
	a.snippetRuns[run.id] = run
	a.snippetRunsMu.Unlock()

	sem := make(chan struct{}, snippetMaxConcurrentHosts)
	var wg sync.WaitGroup
	wg.Add(len(hostIDs))
	for _, hostID := range hostIDs {
		go a.runSnippetOnOneHost(runCtx, run, dial, tpl, hostID, sem, &wg)
	}
	// The run's own context is only ever read by its hosts' goroutines; once
	// every one of them has returned there is nothing left to cancel, and
	// releasing it here (rather than leaking it until the process exits) is
	// what makes CancelSnippetRun's cancel() safe to call on a run that has
	// already finished — it always has something valid to call.
	go func() {
		wg.Wait()
		cancel()
	}()

	return run.id, nil
}

// runSnippetOnOneHost is the whole life of one host within a run: wait for a
// semaphore slot, resolve + dial + run + close, and report the result. It
// never returns an error to its caller — every failure mode becomes a
// terminal SnippetHostResult instead, because one host's problem must never
// stop the others from being attempted.
func (a *App) runSnippetOnOneHost(
	runCtx context.Context,
	run *snippetRun,
	dial func(context.Context, SSHHost) (snippetConn, error),
	tpl QuickTemplate,
	hostID string,
	sem chan struct{},
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	select {
	case sem <- struct{}{}:
	case <-runCtx.Done():
		// Still queued when the run was cancelled: cancelPending (called by
		// CancelSnippetRun, under run.mu) is the one that resolves this host,
		// not this goroutine — returning without touching results is what
		// keeps that resolution single-writer.
		return
	}
	defer func() { <-sem }()

	if _, ok := run.markRunning(hostID); !ok {
		// Lost the race with cancelPending between winning the semaphore and
		// taking run.mu: this host is already "error" with a cancellation
		// reason, and starting real work now would stomp that.
		return
	}
	a.emitSnippetProgress(run.id, run.snapshot(hostID))

	host, err := a.connectableSSHHost(hostID)
	if err != nil {
		// Covers both "no such host" (a stale id in the selection) and "this
		// host runs a ProxyCommand" (the same refusal every other entry point
		// gives) — connectableSSHHost is the one place that judgement is
		// made, see the file-level comment.
		a.finishSnippetHost(run, hostID, hostID, snippetHostError, sshclient.ExecResult{}, err)
		return
	}
	label := sshHostLabel(host)

	hostCtx, hostCancel := context.WithTimeout(runCtx, snippetHostTimeout)
	defer hostCancel()

	conn, err := dial(hostCtx, host)
	if err != nil {
		a.finishSnippetHost(run, hostID, label, snippetHostError, sshclient.ExecResult{}, err)
		return
	}
	// Closed on every path from here down — a successful run, a non-zero
	// exit, a mid-command drop, or hostCtx expiring all reach this defer
	// exactly once. Nothing else in this function holds a reference to conn,
	// so a path that skipped this would leak the connection (and, through
	// dialSnippetConn, the whole jump-host chain behind it) with nothing left
	// that could ever close it.
	defer func() { _ = conn.Close() }()

	res, err := conn.Run(hostCtx, tpl.Text, snippetMaxOutputBytes)
	if err != nil {
		// Run's own contract: err != nil means "we don't know what it did",
		// never "it exited non-zero" — but res.Output may still hold whatever
		// the host printed before it dropped, which is worth keeping.
		a.finishSnippetHost(run, hostID, label, snippetHostError, res, err)
		return
	}
	state := snippetHostOK
	if res.ExitCode != 0 {
		state = snippetHostFailed
	}
	a.finishSnippetHost(run, hostID, label, state, res, nil)
}

// finishSnippetHost records a host's terminal result and emits it.
func (a *App) finishSnippetHost(run *snippetRun, hostID, label, state string, res sshclient.ExecResult, err error) {
	result := SnippetHostResult{
		HostID: hostID, HostLabel: label, State: state,
		ExitCode: res.ExitCode, Output: string(res.Output), Truncated: res.Truncated,
	}
	if err != nil {
		result.Error = err.Error()
	}
	run.setTerminal(result)
	a.emitSnippetProgress(run.id, result)
}

// emitSnippetProgress pushes one host's state change to the frontend. Guarded
// on eventsEmitter alone (not on a.ctx) the same way emitE2EEModeChanged is —
// tests substitute a no-op emitter rather than requiring a live Wails
// context.
func (a *App) emitSnippetProgress(runID string, result SnippetHostResult) {
	if a.eventsEmitter == nil {
		return
	}
	a.eventsEmitter(a.ctx, "snippet:run:progress", SnippetRunProgress{RunID: runID, Result: result})
}

// CancelSnippetRun stops a batch run: every host still queued behind the
// concurrency semaphore moves straight to "error" with a cancellation
// message, and runCtx cancellation reaches every host already dialling or
// running so its own timeout is no longer what has to end it. Hosts that
// already reached a terminal state are untouched — cancelling a run that is
// mostly done must not rewrite the results the user already saw arrive.
func (a *App) CancelSnippetRun(runID string) error {
	a.snippetRunsMu.Lock()
	run, ok := a.snippetRuns[runID]
	a.snippetRunsMu.Unlock()
	if !ok {
		return fmt.Errorf("no such snippet run: %s", runID)
	}

	run.cancel()
	for _, result := range run.cancelPending("snippet run cancelled before this host started") {
		a.emitSnippetProgress(run.id, result)
	}
	return nil
}

// findQuickTemplate looks a saved quick template up by id.
func findQuickTemplate(templates []QuickTemplate, id string) (QuickTemplate, bool) {
	for _, t := range templates {
		if t.ID == id {
			return t, true
		}
	}
	return QuickTemplate{}, false
}

// dialSnippetConn is the production snippetDialer: it goes through
// dialThroughJumps exactly like the tunnel path's dialTunnelConn does, so a
// batch run gets ProxyJump chains and per-hop host-key checking for free
// instead of a second copy of either.
//
// It always passes the zero acceptedHostKey — the same choice dialTunnelConn
// makes, and for the same reason spelled out on snippetHostKeyError: nothing
// here has a dialog to attach a TOFU prompt to, and firing one per host in a
// batch is how TOFU stops meaning anything.
func (a *App) dialSnippetConn(ctx context.Context, h SSHHost) (snippetConn, error) {
	chain, err := a.dialThroughJumps(ctx, h, acceptedHostKey{})
	if err != nil {
		var unknown *HostKeyUnknownError
		if errors.As(err, &unknown) {
			return nil, snippetHostKeyError(unknown)
		}
		return nil, err
	}
	return &jumpChainSnippetConn{chain: chain}, nil
}

// snippetHostKeyError turns an unresolved TOFU prompt into the batch run's
// refusal, instead of the trust dialog dialThroughJumps' callers normally
// show. A batch run has no per-host dialog to show N of, in sequence or
// otherwise — approving fingerprints unattended, sight unseen, is exactly the
// habit TOFU exists to prevent. The fix is the same one the tunnel path
// already asks for: connect to the host once, deliberately, in a terminal,
// and the key is trusted from then on.
func snippetHostKeyError(e *HostKeyUnknownError) error {
	return fmt.Errorf(
		"host key for %s is not trusted yet (fingerprint %s); open a terminal to this host once and accept the fingerprint, then run the snippet again",
		e.Host, e.Fingerprint)
}

// jumpChainSnippetConn adapts a *jumpChain to snippetConn. Run rides the
// chain's target — the last hop, i.e. the host itself. Close tears down the
// whole chain, bastions included: closing only the target would leave every
// jump host's login open with nothing left holding a reference to it.
type jumpChainSnippetConn struct {
	chain *jumpChain
}

func (c *jumpChainSnippetConn) Run(ctx context.Context, cmd string, limit int64) (sshclient.ExecResult, error) {
	return c.chain.Target().Run(ctx, cmd, limit)
}

func (c *jumpChainSnippetConn) Close() error {
	return c.chain.Close()
}
