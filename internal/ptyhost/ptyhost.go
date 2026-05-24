//go:build !windows

// Package ptyhost is a thin, side-effect-free wrapper around a child process
// running under a pseudo-terminal. It does not touch os.Stdin / os.Stdout,
// does not handle SIGWINCH, and does not put any local terminal into raw
// mode — it only owns the PTY master file descriptor and the child process.
//
// Higher-level integrations (CLI wrapper, desktop app) layer their own
// concerns on top: the CLI wrapper bridges the local TTY in addition; the
// desktop app feeds the PTY's bytes through a relay session.
package ptyhost

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// Config configures a PTY child process.
type Config struct {
	Argv []string // argv[0] is the binary, rest are args
	Env  []string // optional; nil means inherit os.Environ()
	Cwd  string   // optional working directory
	Cols uint16   // initial PTY cols (0 → don't set)
	Rows uint16   // initial PTY rows (0 → don't set)
}

// Host is a running PTY child.
type Host struct {
	cmd  *exec.Cmd
	ptmx *os.File

	mu     sync.Mutex
	closed bool
}

// Open launches the child under a PTY and returns a Host. The caller owns
// the lifecycle: read until Read returns EOF (child exited or PTY closed),
// then call Wait to reap and Close to release the master fd.
func Open(ctx context.Context, cfg Config) (*Host, error) {
	if len(cfg.Argv) == 0 {
		return nil, errors.New("ptyhost: empty argv")
	}
	cmd := exec.CommandContext(ctx, cfg.Argv[0], cfg.Argv[1:]...)
	if cfg.Env != nil {
		cmd.Env = cfg.Env
	}
	if cfg.Cwd != "" {
		cmd.Dir = cfg.Cwd
	}
	var ws *pty.Winsize
	if cfg.Cols > 0 && cfg.Rows > 0 {
		ws = &pty.Winsize{Cols: cfg.Cols, Rows: cfg.Rows}
	}
	ptmx, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		return nil, err
	}
	// Tell the kernel line discipline that input is UTF-8. Without this,
	// echo + backspace handling on multi-byte characters whose continuation
	// bytes fall in the C1 control range (0x80-0x9F) — e.g. "发布" → e5 8f
	// 91 e5 b8 83 — gets each C1-range byte expanded into a literal "<00NN>"
	// glyph by the kernel's control-character display path, which the client
	// xterm then renders as garbled boxes. The implementation is platform-
	// gated (termios_*.go); on Windows it's a no-op.
	applyTermiosTweaks(ptmx.Fd())
	return &Host{cmd: cmd, ptmx: ptmx}, nil
}

// Read pulls bytes from the PTY master. Returns io.EOF when the PTY is
// torn down (typically because the child exited).
func (h *Host) Read(p []byte) (int, error) { return h.ptmx.Read(p) }

// Write pushes bytes into the PTY master (i.e. as if the user typed them).
func (h *Host) Write(p []byte) (int, error) { return h.ptmx.Write(p) }

// Resize changes the PTY's window size, which the kernel forwards to the
// child as SIGWINCH.
func (h *Host) Resize(cols, rows uint16) error {
	return pty.Setsize(h.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}

// Size reports the current PTY size.
func (h *Host) Size() (cols, rows uint16, err error) {
	ws, err := pty.GetsizeFull(h.ptmx)
	if err != nil {
		return 0, 0, err
	}
	return ws.Cols, ws.Rows, nil
}

// File returns the PTY master *os.File. Prefer Read/Write where possible;
// the file is exposed mainly for callers that need io.Copy semantics.
func (h *Host) File() *os.File { return h.ptmx }

// Pid returns the child process id, or 0 if the process is not running yet.
func (h *Host) Pid() int {
	if h.cmd == nil || h.cmd.Process == nil {
		return 0
	}
	return h.cmd.Process.Pid
}

// Cwd returns the child process's current working directory, or empty string
// if it cannot be determined (typically: process gone, or unsupported OS).
// On Linux this reads /proc/<pid>/cwd; other platforms return "".
func (h *Host) Cwd() string {
	pid := h.Pid()
	if pid <= 0 {
		return ""
	}
	return readCwd(pid)
}

// Wait blocks until the child exits and returns the OS error from cmd.Wait.
// Use ExitCode to read the status afterward.
func (h *Host) Wait() error { return h.cmd.Wait() }

// ExitCode returns the child's exit status. Only meaningful after Wait.
func (h *Host) ExitCode() int {
	if h.cmd.ProcessState == nil {
		return -1
	}
	return h.cmd.ProcessState.ExitCode()
}

// Close releases the PTY master. Safe to call multiple times.
func (h *Host) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	return h.ptmx.Close()
}
