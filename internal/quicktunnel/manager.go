package quicktunnel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	defaultStartTimeout = 30 * time.Second
	defaultStopTimeout  = 5 * time.Second
	maxOutputLineBytes  = 64 * 1024
)

var (
	// ErrExecutableNotFound means no explicitly supplied or PATH-installed
	// cloudflared binary was available. This package never downloads one.
	ErrExecutableNotFound = errors.New("cloudflared executable not found")
	ErrStartTimeout       = errors.New("Quick Tunnel startup timed out")
	ErrStartInProgress    = errors.New("Quick Tunnel startup already in progress")
)

// Config controls one Quick Tunnel process and its private loopback gateway.
type Config struct {
	Executable   string
	Handler      http.Handler
	StartTimeout time.Duration
	StopTimeout  time.Duration
}

// Status is a point-in-time lifecycle snapshot. PublicURL is set only after a
// strictly validated trycloudflare.com URL has been observed.
type Status struct {
	Running     bool
	Starting    bool
	PublicURL   string
	LocalOrigin string
}

type commandFunc func(executable string, args ...string) *exec.Cmd

type tunnelRun struct {
	cmd     *exec.Cmd
	gateway *Gateway
	done    chan struct{}

	stopOnce sync.Once
	exitMu   sync.Mutex
	exitErr  error
}

// Manager owns exactly one cloudflared process and gateway at a time.
type Manager struct {
	mu sync.Mutex

	cfg      Config
	command  commandFunc
	lookPath func(string) (string, error)

	starting    bool
	startCancel context.CancelFunc
	startDone   chan struct{}
	run         *tunnelRun
	publicURL   string
}

// New constructs an idle manager. It performs no network or process work.
func New(cfg Config) *Manager {
	if cfg.StartTimeout <= 0 {
		cfg.StartTimeout = defaultStartTimeout
	}
	if cfg.StopTimeout <= 0 {
		cfg.StopTimeout = defaultStopTimeout
	}
	return &Manager{
		cfg:      cfg,
		command:  exec.Command,
		lookPath: exec.LookPath,
	}
}

// Start opens the loopback gateway, launches cloudflared, and waits for a
// validated public URL. Starting an already published tunnel is idempotent.
func (m *Manager) Start(ctx context.Context) (Status, error) {
	m.mu.Lock()
	if m.run != nil && m.publicURL != "" {
		status := m.statusLocked()
		m.mu.Unlock()
		return status, nil
	}
	if m.starting {
		m.mu.Unlock()
		return Status{}, ErrStartInProgress
	}
	startCtx, cancel := context.WithCancel(ctx)
	startDone := make(chan struct{})
	m.starting = true
	m.startCancel = cancel
	m.startDone = startDone
	m.mu.Unlock()
	defer m.finishStart(startDone)

	executable, err := m.resolveExecutable()
	if err != nil {
		cancel()
		return Status{}, err
	}
	gateway, err := OpenGateway(m.cfg.Handler)
	if err != nil {
		cancel()
		return Status{}, fmt.Errorf("open Quick Tunnel gateway: %w", err)
	}
	if err := startCtx.Err(); err != nil {
		cancel()
		closeGateway(gateway, m.cfg.StopTimeout)
		return Status{}, err
	}

	args := []string{"tunnel", "--no-autoupdate", "--url", gateway.Origin()}
	cmd := m.command(executable, args...)
	if err := startCtx.Err(); err != nil {
		cancel()
		closeGateway(gateway, m.cfg.StopTimeout)
		return Status{}, err
	}
	prepareProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		closeGateway(gateway, m.cfg.StopTimeout)
		return Status{}, fmt.Errorf("capture cloudflared stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdout.Close()
		cancel()
		closeGateway(gateway, m.cfg.StopTimeout)
		return Status{}, fmt.Errorf("capture cloudflared stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		cancel()
		closeGateway(gateway, m.cfg.StopTimeout)
		return Status{}, fmt.Errorf("start cloudflared: %w", err)
	}

	run := &tunnelRun{cmd: cmd, gateway: gateway, done: make(chan struct{})}
	m.mu.Lock()
	m.run = run
	m.mu.Unlock()

	urls := make(chan string, 4)
	go scanOutput(stdout, urls)
	go scanOutput(stderr, urls)
	go m.reap(run)

	timer := time.NewTimer(m.cfg.StartTimeout)
	defer timer.Stop()
	for {
		select {
		case publicURL := <-urls:
			select {
			case <-run.done:
				return Status{}, m.startExitError(run)
			default:
			}
			m.mu.Lock()
			if m.run != run {
				m.mu.Unlock()
				return Status{}, m.startExitError(run)
			}
			m.publicURL = publicURL
			m.starting = false
			m.startCancel = nil
			status := m.statusLocked()
			m.mu.Unlock()
			cancel()
			return status, nil
		case <-run.done:
			cancel()
			return Status{}, m.startExitError(run)
		case <-timer.C:
			cancel()
			if err := m.stopRun(run); err != nil {
				return Status{}, fmt.Errorf("%w: cleanup: %v", ErrStartTimeout, err)
			}
			return Status{}, ErrStartTimeout
		case <-startCtx.Done():
			err := startCtx.Err()
			if stopErr := m.stopRun(run); stopErr != nil {
				return Status{}, fmt.Errorf("%w: cleanup: %v", err, stopErr)
			}
			return Status{}, err
		}
	}
}

// Stop removes the public route and closes the local gateway. It is safe to
// call repeatedly and while Start is waiting for cloudflared output.
func (m *Manager) Stop() error {
	m.mu.Lock()
	cancel := m.startCancel
	run := m.run
	startDone := m.startDone
	stopTimeout := m.cfg.StopTimeout
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if run == nil {
		if startDone == nil {
			return nil
		}
		timer := time.NewTimer(stopTimeout)
		defer timer.Stop()
		select {
		case <-startDone:
			return nil
		case <-timer.C:
			return errors.New("Quick Tunnel startup did not stop before shutdown timeout")
		}
	}
	return m.stopRun(run)
}

// Status returns a consistent lifecycle snapshot.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

func (m *Manager) statusLocked() Status {
	status := Status{Starting: m.starting, PublicURL: m.publicURL}
	if m.run != nil {
		status.Running = true
		status.LocalOrigin = m.run.gateway.Origin()
	}
	return status
}

func (m *Manager) resolveExecutable() (string, error) {
	name := m.cfg.Executable
	if name == "" {
		name = "cloudflared"
	}
	path, err := m.lookPath(name)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %v", ErrExecutableNotFound, name, err)
	}
	return path, nil
}

func (m *Manager) finishStart(startDone chan struct{}) {
	m.mu.Lock()
	if m.startDone == startDone {
		m.starting = false
		m.startCancel = nil
		m.startDone = nil
	}
	m.mu.Unlock()
	close(startDone)
}

func (m *Manager) reap(run *tunnelRun) {
	err := run.cmd.Wait()
	run.exitMu.Lock()
	run.exitErr = err
	run.exitMu.Unlock()
	closeGateway(run.gateway, m.cfg.StopTimeout)

	m.mu.Lock()
	if m.run == run {
		m.run = nil
		m.publicURL = ""
		m.starting = false
		m.startCancel = nil
	}
	m.mu.Unlock()
	close(run.done)
}

func (m *Manager) startExitError(run *tunnelRun) error {
	run.exitMu.Lock()
	err := run.exitErr
	run.exitMu.Unlock()
	if err == nil {
		return errors.New("cloudflared exited before publishing a valid Quick Tunnel URL")
	}
	return fmt.Errorf("cloudflared exited before publishing a valid Quick Tunnel URL: %w", err)
}

func (m *Manager) stopRun(run *tunnelRun) error {
	run.stopOnce.Do(func() {
		_ = terminateProcess(run.cmd)
	})

	timer := time.NewTimer(m.cfg.StopTimeout)
	defer timer.Stop()
	select {
	case <-run.done:
		return nil
	case <-timer.C:
	}
	if err := killProcess(run.cmd); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("force stop cloudflared process tree: %w", err)
	}

	forceWait := time.NewTimer(m.cfg.StopTimeout)
	defer forceWait.Stop()
	select {
	case <-run.done:
		return nil
	case <-forceWait.C:
		return errors.New("cloudflared process tree did not exit after force stop")
	}
}

func scanOutput(reader io.ReadCloser, urls chan<- string) {
	defer func() { _ = reader.Close() }()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 4096), maxOutputLineBytes)
	for scanner.Scan() {
		for _, publicURL := range extractPublicURLs(scanner.Text()) {
			select {
			case urls <- publicURL:
			default:
			}
		}
	}
}

func closeGateway(gateway *Gateway, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = gateway.Close(ctx)
}
