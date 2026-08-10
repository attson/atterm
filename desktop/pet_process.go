package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// petProcess supervises the companion ("AI 宠物") window, which runs as a
// second process of this same executable launched with --pet.
//
// Why a child process at all: Wails v2 is single-window, and the pet needs a
// frameless always-on-top window that outlives focus changes on the main one.
// Why the SAME binary rather than a second one: a separate executable would
// need its own CI artifact per platform, its own macOS signing/notarization,
// and its own slot in the signed-release verification chain (red line #8).
// Reusing the already-signed executable costs none of that.
//
// Why the child connects to nothing: the remote session list is a second WS
// stream whose contents may be E2EE-sealed, needing account_key to open — and
// red line #21 forbids account_key leaving the main process. So the main app
// pushes an already-merged, already-unsealed projection down a pipe instead.
// Nothing secret ever reaches the child.
//
// Wire format both ways is newline-delimited JSON over the child's stdin and
// stdout. No port, no auth, no discovery: the OS guarantees only parent and
// child share these descriptors.
type petProcess struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	running bool

	// lastPayload dedupes identical pushes. META frames can arrive far faster
	// than a human can read, and re-rendering the same snapshot is pure waste.
	lastPayload string
	lastPushAt  time.Time

	// onEvent receives decoded child→parent events. Set once before Start.
	onEvent func(petEvent)
}

// petPushInterval throttles state pushes. 200ms is below the threshold where
// a status change feels laggy but well above the rate at which a busy build
// emits META frames.
const petPushInterval = 200 * time.Millisecond

// petEvent is a message from the pet window back to the main app.
type petEvent struct {
	// Type is "activate" | "collapse" | "move" | "mute" | "hide".
	Type string `json:"type"`
	// SessionID is set for "activate": the row the user clicked.
	SessionID string `json:"sessionId,omitempty"`
	// Collapsed is set for "collapse".
	Collapsed bool `json:"collapsed,omitempty"`
	// X / Y are set for "move": the window's new screen position.
	X int `json:"x,omitempty"`
	Y int `json:"y,omitempty"`
	// MutedUntilUnix is set for "mute"; 0 clears the mute.
	MutedUntilUnix int64 `json:"mutedUntilUnix,omitempty"`
}

// petBootstrap is the first line written to the child, before any state. It
// carries only presentation preferences — never credentials.
type petBootstrap struct {
	Type      string `json:"type"` // always "bootstrap"
	Collapsed bool   `json:"collapsed"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Locale    string `json:"locale"`
}

func newPetProcess(onEvent func(petEvent)) *petProcess {
	return &petProcess{onEvent: onEvent}
}

// Running reports whether a pet process is currently supervised.
func (p *petProcess) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// Start launches the companion window. It is idempotent: starting an already
// running pet is a no-op, so a config reconcile can call it unconditionally.
func (p *petProcess) Start(boot petBootstrap) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate own executable: %w", err)
	}

	cmd := exec.Command(exe, "--pet")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("pet stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("pet stdout pipe: %w", err)
	}
	// The child's stderr joins ours so a crashing pet is diagnosable from the
	// same log the user already knows how to collect.
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("start pet process: %w", err)
	}

	p.cmd = cmd
	p.stdin = stdin
	p.running = true
	p.lastPayload = ""
	p.lastPushAt = time.Time{}

	boot.Type = "bootstrap"
	if err := writeNDJSON(stdin, boot); err != nil {
		logWarn("pet", "bootstrap write failed: %v", err)
	}

	go p.readEvents(stdout)
	go p.reap(cmd)

	logInfo("pet", "companion window started (pid %d)", cmd.Process.Pid)
	return nil
}

// reap waits for the child and clears the running flag.
//
// It deliberately does NOT restart. A pet that crashes on startup would
// otherwise turn into an unbounded fork loop; the user re-enables it from
// Settings, which is a rare, cheap, and observable action.
func (p *petProcess) reap(cmd *exec.Cmd) {
	err := cmd.Wait()

	p.mu.Lock()
	// A newer Start may have replaced cmd already; only clear our own.
	if p.cmd == cmd {
		p.running = false
		p.cmd = nil
		if p.stdin != nil {
			_ = p.stdin.Close()
			p.stdin = nil
		}
	}
	p.mu.Unlock()

	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		logWarn("pet", "companion window exited: %v", err)
		return
	}
	logInfo("pet", "companion window exited")
}

// readEvents decodes child→parent NDJSON until the pipe closes.
func (p *petProcess) readEvents(stdout io.ReadCloser) {
	defer func() { _ = stdout.Close() }()
	sc := bufio.NewScanner(stdout)
	// Pet events are tiny; a modest cap keeps a wedged child from growing the
	// buffer without bound.
	sc.Buffer(make([]byte, 0, 4096), 64*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev petEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			logWarn("pet", "undecodable event from companion: %v", err)
			continue
		}
		if p.onEvent != nil {
			p.onEvent(ev)
		}
	}
	if err := sc.Err(); err != nil {
		logDebug("pet", "event stream ended: %v", err)
	}
}

// PushState sends a rendered PetState JSON payload to the companion window.
//
// `payload` is passed through opaquely: the projection lives in the frontend
// (lib/petState.ts) so it can be unit-tested there, and duplicating the shape
// in Go would give two definitions to keep in sync.
//
// Returns nil when the pet is not running — callers push on every session
// list change and should not have to check first.
func (p *petProcess) PushState(payload string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running || p.stdin == nil {
		return nil
	}
	if payload == p.lastPayload {
		return nil
	}
	if !p.lastPushAt.IsZero() && time.Since(p.lastPushAt) < petPushInterval {
		// Drop rather than queue: the next push carries the whole snapshot,
		// so a skipped intermediate state is never missing information.
		return nil
	}

	if _, err := io.WriteString(p.stdin, payload+"\n"); err != nil {
		return fmt.Errorf("push pet state: %w", err)
	}
	p.lastPayload = payload
	p.lastPushAt = time.Now()
	return nil
}

// Stop terminates the companion window. Idempotent.
func (p *petProcess) Stop() {
	p.mu.Lock()
	cmd := p.cmd
	stdin := p.stdin
	p.running = false
	p.cmd = nil
	p.stdin = nil
	p.mu.Unlock()

	// Closing stdin is the graceful signal: the child treats EOF as "parent is
	// gone" and exits on its own, which also covers the case where the parent
	// is SIGKILLed and never reaches the Kill below.
	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd == nil || cmd.Process == nil {
		return
	}

	done := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		logWarn("pet", "companion window ignored EOF; killing")
		_ = cmd.Process.Kill()
	}
}

func writeNDJSON(w io.Writer, v any) error {
	blob, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(blob, '\n'))
	return err
}
