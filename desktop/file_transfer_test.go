package main

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type gatedTransferSource struct {
	transferEndpoint
	started chan<- struct{}
	release <-chan struct{}
	active  *atomic.Int32
	maximum *atomic.Int32
}

func (e *gatedTransferSource) OpenReader(ctx context.Context, name string) (io.ReadCloser, error) {
	reader, err := e.transferEndpoint.OpenReader(ctx, name)
	if err != nil {
		return nil, err
	}
	current := e.active.Add(1)
	for {
		maximum := e.maximum.Load()
		if current <= maximum || e.maximum.CompareAndSwap(maximum, current) {
			break
		}
	}
	e.started <- struct{}{}
	return &gatedTransferReader{
		ReadCloser: reader,
		ctx:        ctx,
		release:    e.release,
		onClose:    func() { e.active.Add(-1) },
	}, nil
}

type gatedTransferReader struct {
	io.ReadCloser
	ctx     context.Context
	release <-chan struct{}
	onClose func()

	gateOnce  sync.Once
	closeOnce sync.Once
	gateErr   error
}

func (r *gatedTransferReader) Read(p []byte) (int, error) {
	r.gateOnce.Do(func() {
		select {
		case <-r.release:
		case <-r.ctx.Done():
			r.gateErr = r.ctx.Err()
		}
	})
	if r.gateErr != nil {
		return 0, r.gateErr
	}
	return r.ReadCloser.Read(p)
}

func (r *gatedTransferReader) Close() error {
	var err error
	r.closeOnce.Do(func() {
		err = r.ReadCloser.Close()
		r.onClose()
	})
	return err
}

type trackedTransferEndpoint struct {
	transferEndpoint
	closed *atomic.Int32
}

type fakeCredentialHostError struct {
	hostID string
}

func (e *fakeCredentialHostError) Error() string            { return errCredentialMissing }
func (e *fakeCredentialHostError) CredentialHostID() string { return e.hostID }

func (e *trackedTransferEndpoint) Close() error {
	e.closed.Add(1)
	return e.transferEndpoint.Close()
}

type transferEventRecorder struct {
	mu     sync.Mutex
	events map[string][]FileTransferJob
	ch     chan FileTransferJob
}

func newTransferEventRecorder() *transferEventRecorder {
	return &transferEventRecorder{events: map[string][]FileTransferJob{}, ch: make(chan FileTransferJob, 256)}
}

func (r *transferEventRecorder) emit(job FileTransferJob) {
	r.mu.Lock()
	r.events[job.ID] = append(r.events[job.ID], job)
	r.mu.Unlock()
	r.ch <- job
}

func (r *transferEventRecorder) snapshot(id string) []FileTransferJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]FileTransferJob(nil), r.events[id]...)
}

func waitForTransferStates(t *testing.T, recorder *transferEventRecorder, ids []string) map[string]FileTransferJob {
	t.Helper()
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	terminal := map[string]FileTransferJob{}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for len(want) > 0 {
		select {
		case job := <-recorder.ch:
			if !want[job.ID] || !isFileTransferTerminal(job.State) {
				continue
			}
			terminal[job.ID] = job
			delete(want, job.ID)
		case <-timer.C:
			t.Fatalf("timed out waiting for terminal transfers: %v", want)
		}
	}
	return terminal
}

func TestFileTransferManagerRejectsInvalidRequestsImmediately(t *testing.T) {
	m := newFileTransferManager(context.Background(), nil, nil)
	cases := []struct {
		name string
		req  fileTransferRequest
	}{
		{name: "no paths", req: fileTransferRequest{Source: FileEndpoint{Kind: "local"}, Destination: FileEndpoint{Kind: "local"}, DestinationDir: "/tmp", Conflict: conflictSkip}},
		{name: "unknown source kind", req: fileTransferRequest{Source: FileEndpoint{Kind: "relay"}, Destination: FileEndpoint{Kind: "local"}, Paths: []string{"/tmp/a"}, DestinationDir: "/tmp", Conflict: conflictSkip}},
		{name: "ssh host missing", req: fileTransferRequest{Source: FileEndpoint{Kind: "ssh"}, Destination: FileEndpoint{Kind: "local"}, Paths: []string{"/tmp/a"}, DestinationDir: "/tmp", Conflict: conflictSkip}},
		{name: "unknown conflict", req: fileTransferRequest{Source: FileEndpoint{Kind: "local"}, Destination: FileEndpoint{Kind: "ssh", HostID: "h"}, Paths: []string{"/tmp/a"}, DestinationDir: "/tmp", Conflict: transferConflict("merge")}},
		{name: "same target", req: fileTransferRequest{Source: FileEndpoint{Kind: "ssh", HostID: "h"}, Destination: FileEndpoint{Kind: "ssh", HostID: "h"}, Paths: []string{"/tmp/a"}, DestinationDir: "/tmp", Conflict: conflictSkip}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if id, err := m.Start(tc.req); err == nil || id != "" {
				t.Fatalf("Start = (%q, %v), want immediate rejection", id, err)
			}
		})
	}
	if got := m.List(); got == nil || len(got) != 0 {
		t.Fatalf("List = %#v, want a non-nil empty slice", got)
	}
}

func TestFileTransferManagerRunsAtMostTwoJobsAndKeepsProgressMonotonic(t *testing.T) {
	source := newFakeTransferEndpoint("ssh:source")
	for _, name := range []string{"one", "two", "three"} {
		source.nodes["/"+name] = &fakeTransferNode{kind: transferFile, data: make([]byte, 512<<10), modTime: 1}
	}

	started := make(chan struct{}, 3)
	release := make(chan struct{}, 3)
	var active, maximum, closed atomic.Int32
	factory := func(_ context.Context, endpoint FileEndpoint, _ transferHostKeyAcceptance) (transferEndpoint, error) {
		switch endpoint.ID() {
		case "ssh:source":
			return &trackedTransferEndpoint{transferEndpoint: &gatedTransferSource{
				transferEndpoint: source,
				started:          started,
				release:          release,
				active:           &active,
				maximum:          &maximum,
			}, closed: &closed}, nil
		case "ssh:destination":
			destination := newFakeTransferEndpoint("ssh:destination")
			destination.nodes["/out"] = &fakeTransferNode{kind: transferDir}
			return &trackedTransferEndpoint{transferEndpoint: destination, closed: &closed}, nil
		default:
			return nil, errors.New("unexpected endpoint")
		}
	}
	recorder := newTransferEventRecorder()
	m := newFileTransferManager(context.Background(), factory, recorder.emit)

	ids := make([]string, 0, 3)
	for _, name := range []string{"one", "two", "three"} {
		id, err := m.Start(fileTransferRequest{
			Source:         FileEndpoint{Kind: "ssh", HostID: "source"},
			Destination:    FileEndpoint{Kind: "ssh", HostID: "destination"},
			Paths:          []string{"/" + name},
			DestinationDir: "/out",
			Conflict:       conflictSkip,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for transfer %d to start", i+1)
		}
	}
	select {
	case <-started:
		t.Fatal("a third transfer opened its source before either active transfer released its slot")
	case <-time.After(150 * time.Millisecond):
	}

	release <- struct{}{}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("queued third transfer did not start after a slot was released")
	}
	release <- struct{}{}
	release <- struct{}{}
	terminal := waitForTransferStates(t, recorder, ids)

	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum concurrent readers = %d, want 2", got)
	}
	if got := closed.Load(); got != 6 {
		t.Fatalf("closed endpoints = %d, want 6", got)
	}
	for _, id := range ids {
		if terminal[id].State != fileTransferCompleted {
			t.Fatalf("job %s state = %q, want completed", id, terminal[id].State)
		}
		events := recorder.snapshot(id)
		terminalCount := 0
		for i, event := range events {
			if isFileTransferTerminal(event.State) {
				terminalCount++
			}
			if i > 0 && (event.BytesDone < events[i-1].BytesDone || event.EntriesDone < events[i-1].EntriesDone) {
				t.Fatalf("job %s progress moved backwards: %+v then %+v", id, events[i-1], event)
			}
		}
		if terminalCount != 1 {
			t.Fatalf("job %s emitted %d terminal states, want 1", id, terminalCount)
		}
	}
}

func TestFileTransferManagerCancellationAndShutdownReleaseEndpointsOnce(t *testing.T) {
	source := newFakeTransferEndpoint("ssh:source")
	source.nodes["/file"] = &fakeTransferNode{kind: transferFile, data: make([]byte, 512<<10)}
	destination := newFakeTransferEndpoint("ssh:destination")
	destination.nodes["/out"] = &fakeTransferNode{kind: transferDir}
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	var active, maximum, closed atomic.Int32
	factory := func(_ context.Context, endpoint FileEndpoint, _ transferHostKeyAcceptance) (transferEndpoint, error) {
		if endpoint.ID() == "ssh:source" {
			return &trackedTransferEndpoint{transferEndpoint: &gatedTransferSource{transferEndpoint: source, started: started, release: release, active: &active, maximum: &maximum}, closed: &closed}, nil
		}
		return &trackedTransferEndpoint{transferEndpoint: destination, closed: &closed}, nil
	}
	recorder := newTransferEventRecorder()
	m := newFileTransferManager(context.Background(), factory, recorder.emit)
	req := fileTransferRequest{
		Source: FileEndpoint{Kind: "ssh", HostID: "source"}, Destination: FileEndpoint{Kind: "ssh", HostID: "destination"},
		Paths: []string{"/file"}, DestinationDir: "/out", Conflict: conflictSkip,
	}
	id, err := m.Start(req)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("transfer did not reach its reader")
	}
	if err := m.Cancel(id); err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel(id); err != nil {
		t.Fatalf("second cancel should be idempotent: %v", err)
	}
	terminal := waitForTransferStates(t, recorder, []string{id})[id]
	if terminal.State != fileTransferCancelled {
		t.Fatalf("state = %q, want cancelled", terminal.State)
	}
	if got := closed.Load(); got != 2 {
		t.Fatalf("closed endpoints after cancel = %d, want 2", got)
	}
	terminalCount := 0
	for _, event := range recorder.snapshot(id) {
		if isFileTransferTerminal(event.State) {
			terminalCount++
		}
	}
	if terminalCount != 1 {
		t.Fatalf("terminal events after repeated cancel = %d, want 1", terminalCount)
	}

	id2, err := m.Start(req)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("second transfer did not reach its reader")
	}
	m.Shutdown()
	jobs := m.List()
	for _, job := range jobs {
		if job.ID == id2 && job.State != fileTransferCancelled {
			t.Fatalf("shutdown job state = %q, want cancelled", job.State)
		}
	}
	if got := closed.Load(); got != 4 {
		t.Fatalf("closed endpoints after shutdown = %d, want 4", got)
	}
	if _, err := m.Start(req); err == nil {
		t.Fatal("Start succeeded after shutdown")
	}
	if err := m.Cancel("missing"); err == nil {
		t.Fatal("Cancel accepted an unknown job id")
	}
}

func TestFileTransferManagerPreservesHostKeyPromptOnAsyncOpenFailure(t *testing.T) {
	recorder := newTransferEventRecorder()
	factory := func(_ context.Context, _ FileEndpoint, _ transferHostKeyAcceptance) (transferEndpoint, error) {
		return nil, &HostKeyUnknownError{
			Host:        "jump.example:22",
			Fingerprint: "SHA256:jump",
			HopIndex:    1,
			HopName:     "jump",
		}
	}
	m := newFileTransferManager(context.Background(), factory, recorder.emit)
	t.Cleanup(m.Shutdown)

	id, err := m.Start(fileTransferRequest{
		Source:         FileEndpoint{Kind: "ssh", HostID: "source"},
		Destination:    FileEndpoint{Kind: "local"},
		Paths:          []string{"/from/a.txt"},
		DestinationDir: "/to",
		Conflict:       conflictSkip,
	})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTransferStates(t, recorder, []string{id})[id]
	if terminal.HostKey == nil {
		t.Fatalf("terminal job = %+v, want a structured host-key prompt", terminal)
	}
	if terminal.HostKey.Host != "jump.example:22" || terminal.HostKey.Fingerprint != "SHA256:jump" ||
		terminal.HostKey.HopIndex != 1 || terminal.HostKey.HopName != "jump" {
		t.Fatalf("host-key prompt = %+v", terminal.HostKey)
	}
}

func TestFileTransferManagerRetriesFailedJobFromRetainedRequest(t *testing.T) {
	recorder := newTransferEventRecorder()
	var attempts atomic.Int32
	var gotAccepted transferHostKeyAcceptance
	factory := func(_ context.Context, endpoint FileEndpoint, accepted transferHostKeyAcceptance) (transferEndpoint, error) {
		if endpoint.ID() == "ssh:source" {
			gotAccepted = accepted
			if attempts.Add(1) == 1 {
				return nil, errors.New("temporary network failure")
			}
			source := newFakeTransferEndpoint("ssh:source")
			source.nodes["/from.txt"] = &fakeTransferNode{kind: transferFile, data: []byte("data")}
			return source, nil
		}
		destination := newFakeTransferEndpoint("local")
		destination.nodes["/to"] = &fakeTransferNode{kind: transferDir}
		return destination, nil
	}
	m := newFileTransferManager(context.Background(), factory, recorder.emit)
	t.Cleanup(m.Shutdown)

	paths := []string{"/from.txt"}
	id, err := m.Start(fileTransferRequest{
		Source: FileEndpoint{Kind: "ssh", HostID: "source"}, Destination: FileEndpoint{Kind: "local"},
		Paths: paths, DestinationDir: "/to", Conflict: conflictSkip,
	})
	if err != nil {
		t.Fatal(err)
	}
	paths[0] = "/mutated-after-start"
	if got := waitForTransferStates(t, recorder, []string{id})[id].State; got != fileTransferFailed {
		t.Fatalf("first state = %q, want failed", got)
	}

	retryID, err := m.Retry(id, transferHostKeyAcceptance{Host: "source:22", Fingerprint: "SHA256:source"})
	if err != nil {
		t.Fatal(err)
	}
	if retryID == id {
		t.Fatal("retry reused the failed job id")
	}
	terminal := waitForTransferStates(t, recorder, []string{retryID})[retryID]
	if terminal.State != fileTransferCompleted {
		t.Fatalf("retry state = %q, want completed (%s)", terminal.State, terminal.Error)
	}
	if gotAccepted.Host != "source:22" || gotAccepted.Fingerprint != "SHA256:source" {
		t.Fatalf("retry accepted host key = %+v", gotAccepted)
	}
}

func TestFileTransferManagerMarksCredentialFailureWithEndpointHost(t *testing.T) {
	recorder := newTransferEventRecorder()
	factory := func(_ context.Context, endpoint FileEndpoint, _ transferHostKeyAcceptance) (transferEndpoint, error) {
		if endpoint.ID() == "ssh:source" {
			return nil, errors.New(errCredentialMissing)
		}
		return nil, errors.New("unexpected endpoint")
	}
	m := newFileTransferManager(context.Background(), factory, recorder.emit)
	t.Cleanup(m.Shutdown)
	id, err := m.Start(fileTransferRequest{
		Source: FileEndpoint{Kind: "ssh", HostID: "source"}, Destination: FileEndpoint{Kind: "local"},
		Paths: []string{"/from.txt"}, DestinationDir: "/to", Conflict: conflictSkip,
	})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTransferStates(t, recorder, []string{id})[id]
	if terminal.CredentialHostID != "source" {
		t.Fatalf("credential host id = %q, want source", terminal.CredentialHostID)
	}
}

func TestFileTransferManagerMarksCredentialFailureWithJumpHost(t *testing.T) {
	recorder := newTransferEventRecorder()
	factory := func(_ context.Context, endpoint FileEndpoint, _ transferHostKeyAcceptance) (transferEndpoint, error) {
		if endpoint.ID() == "ssh:source" {
			return nil, &fakeCredentialHostError{hostID: "jump-host"}
		}
		return nil, errors.New("unexpected endpoint")
	}
	m := newFileTransferManager(context.Background(), factory, recorder.emit)
	t.Cleanup(m.Shutdown)
	id, err := m.Start(fileTransferRequest{
		Source: FileEndpoint{Kind: "ssh", HostID: "source"}, Destination: FileEndpoint{Kind: "local"},
		Paths: []string{"/from.txt"}, DestinationDir: "/to", Conflict: conflictSkip,
	})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTransferStates(t, recorder, []string{id})[id]
	if terminal.CredentialHostID != "jump-host" {
		t.Fatalf("credential host id = %q, want jump-host", terminal.CredentialHostID)
	}
}

func TestFileTransferManagerShutdownIsBoundedWhenFactoryIgnoresContext(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	factory := func(_ context.Context, _ FileEndpoint, _ transferHostKeyAcceptance) (transferEndpoint, error) {
		close(started)
		<-release
		return nil, errors.New("released")
	}
	m := newFileTransferManager(context.Background(), factory, nil)
	m.shutdownTimeout = 20 * time.Millisecond
	if _, err := m.Start(fileTransferRequest{
		Source: FileEndpoint{Kind: "local"}, Destination: FileEndpoint{Kind: "local"},
		Paths: []string{"/from"}, DestinationDir: "/to", Conflict: conflictSkip,
	}); err != nil {
		t.Fatal(err)
	}
	<-started

	shutdownDone := make(chan struct{})
	go func() {
		m.Shutdown()
		close(shutdownDone)
	}()
	select {
	case <-shutdownDone:
	case <-time.After(200 * time.Millisecond):
		close(release)
		<-shutdownDone
		t.Fatal("Shutdown waited indefinitely for an endpoint factory that ignored cancellation")
	}
	close(release)
}
