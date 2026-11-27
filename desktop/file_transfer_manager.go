package main

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	fileTransferQueued    = "queued"
	fileTransferRunning   = "running"
	fileTransferCompleted = "completed"
	fileTransferFailed    = "failed"
	fileTransferCancelled = "cancelled"

	fileTransferMaxConcurrent = 2
	fileTransferEmitInterval  = 100 * time.Millisecond
	fileTransferShutdownWait  = 5 * time.Second
)

type FileEndpoint struct {
	Kind   string `json:"kind"`
	HostID string `json:"host_id,omitempty"`
}

func (e FileEndpoint) ID() string {
	if e.Kind == "local" {
		return "local"
	}
	return e.Kind + ":" + e.HostID
}

type FileTransferJob struct {
	ID               string                     `json:"id"`
	State            string                     `json:"state"`
	CurrentPath      string                     `json:"current_path"`
	Error            string                     `json:"error,omitempty"`
	BytesDone        int64                      `json:"bytes_done"`
	BytesTotal       int64                      `json:"bytes_total"`
	EntriesDone      int                        `json:"entries_done"`
	EntriesTotal     int                        `json:"entries_total"`
	Warnings         []string                   `json:"warnings"`
	HostKey          *FileTransferHostKeyPrompt `json:"host_key,omitempty"`
	CredentialHostID string                     `json:"credential_host_id,omitempty"`
}

type FileTransferHostKeyPrompt struct {
	Fingerprint string `json:"fingerprint"`
	Host        string `json:"host"`
	HopIndex    int    `json:"hop_index"`
	HopName     string `json:"hop_name"`
}

type transferHostKeyAcceptance struct {
	Host        string
	Fingerprint string
}

type fileTransferRequest struct {
	Source          FileEndpoint
	Destination     FileEndpoint
	Paths           []string
	DestinationDir  string
	Conflict        transferConflict
	AcceptedHostKey transferHostKeyAcceptance
}

type transferEndpointFactory func(context.Context, FileEndpoint, transferHostKeyAcceptance) (transferEndpoint, error)

type fileTransferRecord struct {
	job      FileTransferJob
	req      fileTransferRequest
	ctx      context.Context
	cancel   context.CancelFunc
	terminal bool
	lastEmit time.Time
}

type fileTransferManager struct {
	ctx    context.Context
	cancel context.CancelFunc
	open   transferEndpointFactory
	emit   func(FileTransferJob)
	sem    chan struct{}

	mu              sync.Mutex
	jobs            map[string]*fileTransferRecord
	order           []string
	closed          bool
	wg              sync.WaitGroup
	shutdownTimeout time.Duration

	shutdownOnce sync.Once
}

func newFileTransferManager(parent context.Context, open transferEndpointFactory, emit func(FileTransferJob)) *fileTransferManager {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	return &fileTransferManager{
		ctx:             ctx,
		cancel:          cancel,
		open:            open,
		emit:            emit,
		sem:             make(chan struct{}, fileTransferMaxConcurrent),
		jobs:            map[string]*fileTransferRecord{},
		order:           []string{},
		shutdownTimeout: fileTransferShutdownWait,
	}
}

func (m *fileTransferManager) Start(req fileTransferRequest) (string, error) {
	if err := validateFileTransferRequest(req); err != nil {
		return "", err
	}
	if m.open == nil {
		return "", errors.New("file transfer manager has no endpoint factory")
	}
	req = cloneFileTransferRequest(req)

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return "", errors.New("file transfer manager is shut down")
	}
	jobCtx, cancel := context.WithCancel(m.ctx)
	record := &fileTransferRecord{
		job: FileTransferJob{
			ID:       uuid.NewString(),
			State:    fileTransferQueued,
			Warnings: []string{},
		},
		req:    req,
		ctx:    jobCtx,
		cancel: cancel,
	}
	m.jobs[record.job.ID] = record
	m.order = append(m.order, record.job.ID)
	m.wg.Add(1)
	snapshot := cloneFileTransferJob(record.job)
	m.mu.Unlock()

	m.emitJob(snapshot)
	go m.run(record)
	return record.job.ID, nil
}

func validateFileTransferRequest(req fileTransferRequest) error {
	if err := validateFileEndpoint("source", req.Source); err != nil {
		return err
	}
	if err := validateFileEndpoint("destination", req.Destination); err != nil {
		return err
	}
	if len(req.Paths) == 0 {
		return errors.New("file transfer: no source paths")
	}
	if strings.TrimSpace(req.DestinationDir) == "" {
		return errors.New("file transfer: destination directory is required")
	}
	if req.Conflict != conflictSkip && req.Conflict != conflictReplace && req.Conflict != conflictRename {
		return fmt.Errorf("file transfer: unsupported conflict policy %q", req.Conflict)
	}
	for _, sourcePath := range req.Paths {
		if strings.TrimSpace(sourcePath) == "" {
			return errors.New("file transfer: source path is required")
		}
		if req.Source.ID() == req.Destination.ID() && sameTransferTarget(req.Source.Kind, sourcePath, req.DestinationDir) {
			return fmt.Errorf("file transfer: source and destination are the same path: %s", sourcePath)
		}
	}
	return nil
}

func validateFileEndpoint(label string, endpoint FileEndpoint) error {
	switch endpoint.Kind {
	case "local":
		if endpoint.HostID != "" {
			return fmt.Errorf("file transfer: %s local endpoint must not include a host id", label)
		}
	case "ssh":
		if strings.TrimSpace(endpoint.HostID) == "" {
			return fmt.Errorf("file transfer: %s SSH endpoint requires a host id", label)
		}
	default:
		return fmt.Errorf("file transfer: unsupported %s endpoint kind %q", label, endpoint.Kind)
	}
	return nil
}

func sameTransferTarget(kind, source, destinationDir string) bool {
	if kind == "ssh" {
		if !path.IsAbs(source) || !path.IsAbs(destinationDir) {
			return false
		}
		cleanSource := path.Clean(source)
		return cleanSource == path.Join(path.Clean(destinationDir), path.Base(cleanSource))
	}
	if !filepath.IsAbs(source) || !filepath.IsAbs(destinationDir) {
		return false
	}
	cleanSource := filepath.Clean(source)
	return cleanSource == filepath.Join(filepath.Clean(destinationDir), filepath.Base(cleanSource))
}

func (m *fileTransferManager) run(record *fileTransferRecord) {
	defer m.wg.Done()
	defer record.cancel()

	select {
	case m.sem <- struct{}{}:
		defer func() { <-m.sem }()
	case <-record.ctx.Done():
		m.finish(record, FileTransferResult{State: fileTransferCancelled, Warnings: []string{}})
		return
	}
	if !m.markRunning(record) {
		return
	}

	source, err := m.open(record.ctx, record.req.Source, record.req.AcceptedHostKey)
	if err != nil {
		m.finish(record, transferResultForEndpointError(record.ctx, record.req.Source, err))
		return
	}
	destination, err := m.open(record.ctx, record.req.Destination, record.req.AcceptedHostKey)
	if err != nil {
		_ = source.Close()
		m.finish(record, transferResultForEndpointError(record.ctx, record.req.Destination, err))
		return
	}

	manifest, err := buildTransferManifest(record.ctx, source, record.req.Paths)
	if err != nil {
		_ = destination.Close()
		_ = source.Close()
		m.finish(record, transferResultForError(record.ctx, err))
		return
	}
	result := runTransfer(record.ctx, source, destination, manifest, record.req.DestinationDir, record.req.Conflict, func(progress FileTransferProgress) {
		m.updateProgress(record, progress)
	})
	_ = destination.Close()
	_ = source.Close()
	m.finish(record, result)
}

func transferResultForError(ctx context.Context, err error) FileTransferResult {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return FileTransferResult{State: fileTransferCancelled, Warnings: []string{}}
	}
	result := FileTransferResult{State: fileTransferFailed, Error: err.Error(), Warnings: []string{}}
	var hostKey *HostKeyUnknownError
	if errors.As(err, &hostKey) {
		result.HostKey = &FileTransferHostKeyPrompt{
			Fingerprint: hostKey.Fingerprint,
			Host:        hostKey.Host,
			HopIndex:    hostKey.HopIndex,
			HopName:     hostKey.HopName,
		}
	}
	return result
}

func transferResultForEndpointError(ctx context.Context, endpoint FileEndpoint, err error) FileTransferResult {
	result := transferResultForError(ctx, err)
	if result.State == fileTransferFailed && endpoint.Kind == "ssh" &&
		(err.Error() == errCredentialMissing || err.Error() == errKeyMissing) {
		result.CredentialHostID = endpoint.HostID
	}
	return result
}

func (m *fileTransferManager) markRunning(record *fileTransferRecord) bool {
	m.mu.Lock()
	if record.terminal {
		m.mu.Unlock()
		return false
	}
	record.job.State = fileTransferRunning
	record.lastEmit = time.Now()
	snapshot := cloneFileTransferJob(record.job)
	m.mu.Unlock()
	m.emitJob(snapshot)
	return true
}

func (m *fileTransferManager) updateProgress(record *fileTransferRecord, progress FileTransferProgress) {
	m.mu.Lock()
	if record.terminal {
		m.mu.Unlock()
		return
	}
	record.job.CurrentPath = progress.CurrentPath
	record.job.BytesDone = progress.BytesDone
	record.job.BytesTotal = progress.BytesTotal
	record.job.EntriesDone = progress.EntriesDone
	record.job.EntriesTotal = progress.EntriesTotal

	// runTransfer reports a terminal progress snapshot before returning its
	// result. The manager owns terminal emission because endpoints must be
	// closed first and warnings/errors only exist on the result.
	if isFileTransferTerminal(progress.State) || time.Since(record.lastEmit) < fileTransferEmitInterval {
		m.mu.Unlock()
		return
	}
	record.lastEmit = time.Now()
	snapshot := cloneFileTransferJob(record.job)
	m.mu.Unlock()
	m.emitJob(snapshot)
}

func (m *fileTransferManager) finish(record *fileTransferRecord, result FileTransferResult) {
	state := result.State
	if state != fileTransferCompleted && state != fileTransferFailed && state != fileTransferCancelled {
		state = fileTransferFailed
		if result.Error == "" {
			result.Error = fmt.Sprintf("file transfer returned invalid terminal state %q", result.State)
		}
	}

	m.mu.Lock()
	if record.terminal {
		m.mu.Unlock()
		return
	}
	record.terminal = true
	record.job.State = state
	record.job.Error = result.Error
	record.job.Warnings = append([]string{}, result.Warnings...)
	record.job.HostKey = cloneFileTransferHostKeyPrompt(result.HostKey)
	record.job.CredentialHostID = result.CredentialHostID
	if state == fileTransferCompleted {
		record.job.CurrentPath = ""
	}
	snapshot := cloneFileTransferJob(record.job)
	m.mu.Unlock()
	m.emitJob(snapshot)
}

func (m *fileTransferManager) Cancel(id string) error {
	m.mu.Lock()
	record, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("no such file transfer: %s", id)
	}
	if record.terminal {
		m.mu.Unlock()
		return nil
	}
	cancel := record.cancel
	m.mu.Unlock()
	cancel()
	return nil
}

func (m *fileTransferManager) Retry(id string, accepted transferHostKeyAcceptance) (string, error) {
	m.mu.Lock()
	record, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return "", fmt.Errorf("no such file transfer: %s", id)
	}
	if !record.terminal || record.job.State != fileTransferFailed {
		m.mu.Unlock()
		return "", fmt.Errorf("file transfer is not retryable: %s", id)
	}
	req := cloneFileTransferRequest(record.req)
	m.mu.Unlock()
	if accepted.Host != "" && accepted.Fingerprint != "" {
		req.AcceptedHostKey = accepted
	}
	return m.Start(req)
}

func (m *fileTransferManager) List() []FileTransferJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	jobs := make([]FileTransferJob, 0, len(m.order))
	for _, id := range m.order {
		if record := m.jobs[id]; record != nil {
			jobs = append(jobs, cloneFileTransferJob(record.job))
		}
	}
	return jobs
}

func (m *fileTransferManager) Shutdown() {
	m.shutdownOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		m.cancel()
		for _, record := range m.jobs {
			if !record.terminal {
				record.cancel()
			}
		}
		m.mu.Unlock()
		done := make(chan struct{})
		go func() {
			m.wg.Wait()
			close(done)
		}()
		timeout := m.shutdownTimeout
		if timeout <= 0 {
			timeout = fileTransferShutdownWait
		}
		select {
		case <-done:
		case <-time.After(timeout):
		}
	})
}

func (m *fileTransferManager) emitJob(job FileTransferJob) {
	if m.emit != nil {
		m.emit(job)
	}
}

func cloneFileTransferJob(job FileTransferJob) FileTransferJob {
	job.Warnings = append([]string{}, job.Warnings...)
	job.HostKey = cloneFileTransferHostKeyPrompt(job.HostKey)
	return job
}

func cloneFileTransferRequest(req fileTransferRequest) fileTransferRequest {
	req.Paths = append([]string(nil), req.Paths...)
	return req
}

func cloneFileTransferHostKeyPrompt(prompt *FileTransferHostKeyPrompt) *FileTransferHostKeyPrompt {
	if prompt == nil {
		return nil
	}
	copy := *prompt
	return &copy
}

func isFileTransferTerminal(state string) bool {
	return state == fileTransferCompleted || state == fileTransferFailed || state == fileTransferCancelled
}
