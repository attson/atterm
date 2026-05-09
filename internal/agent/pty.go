// Package agent is the CLI-side PTY host. It wraps internal/ptyhost (the
// pure PTY layer) and adds local-TTY niceties — raw mode on stdin, SIGWINCH
// forwarding to the child, and a Pipe helper that mirrors PTY output to the
// caller's stdout. Desktop / GUI hosts should use internal/ptyhost directly
// and skip this package's local-TTY plumbing.
package agent

import (
	"context"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/attson/atterm/internal/ptyhost"
	"github.com/creack/pty"
	"golang.org/x/term"
)

// Pty is the CLI-flavored PTY: a ptyhost.Host plus optional local-TTY plumbing.
type Pty struct {
	host *ptyhost.Host

	localTTY        *os.File
	localTTYOldMode *term.State

	mu        sync.Mutex
	closed    bool
	resizeCbs []func(cols, rows uint16)
}

// Start launches argv[0] argv[1:] under a PTY. If stdin is a terminal, this
// also puts it in raw mode and forwards SIGWINCH to the child PTY so that
// resizing the host terminal resizes the wrapped child.
func Start(ctx context.Context, argv []string, env []string) (*Pty, error) {
	host, err := ptyhost.Open(ctx, ptyhost.Config{Argv: argv, Env: env})
	if err != nil {
		return nil, err
	}
	p := &Pty{host: host}

	if term.IsTerminal(int(os.Stdin.Fd())) {
		p.localTTY = os.Stdin
		if oldState, err := term.MakeRaw(int(os.Stdin.Fd())); err == nil {
			p.localTTYOldMode = oldState
		}
		_ = pty.InheritSize(os.Stdin, host.File())
		go p.watchSIGWINCH()
	}
	return p, nil
}

// File returns the underlying PTY master.
func (p *Pty) File() *os.File { return p.host.File() }

// Resize forwards a resize request to the PTY.
func (p *Pty) Resize(cols, rows uint16) error { return p.host.Resize(cols, rows) }

// Size returns the PTY's current size.
func (p *Pty) Size() (cols, rows uint16, err error) { return p.host.Size() }

// OnResize registers a callback fired when local SIGWINCH changes the size.
func (p *Pty) OnResize(fn func(cols, rows uint16)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resizeCbs = append(p.resizeCbs, fn)
}

func (p *Pty) watchSIGWINCH() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	defer signal.Stop(ch)
	for range ch {
		_ = pty.InheritSize(os.Stdin, p.host.File())
		cols, rows, err := p.host.Size()
		if err != nil {
			continue
		}
		p.mu.Lock()
		cbs := append([]func(cols, rows uint16){}, p.resizeCbs...)
		p.mu.Unlock()
		for _, cb := range cbs {
			cb(cols, rows)
		}
	}
}

// PipeLocal copies local stdin to PTY and PTY output to local stdout, while
// returning a tee reader so the caller can also forward PTY output elsewhere.
func (p *Pty) PipeLocal() io.Reader {
	if p.localTTY != nil {
		go func() { _, _ = io.Copy(p.host.File(), os.Stdin) }()
		return io.TeeReader(p.host.File(), os.Stdout)
	}
	return p.host.File()
}

// Wait blocks until the child process exits.
func (p *Pty) Wait() error { return p.host.Wait() }

// Close restores the local TTY mode and tears down the PTY.
func (p *Pty) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()
	if p.localTTYOldMode != nil {
		_ = term.Restore(int(p.localTTY.Fd()), p.localTTYOldMode)
	}
	return p.host.Close()
}

// ExitCode reports the child's exit status (after Wait returns).
func (p *Pty) ExitCode() int { return p.host.ExitCode() }
