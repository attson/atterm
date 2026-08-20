package sshclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/ssh"
)

// ExecResult is the outcome of Run: what the command printed and how it
// exited.
type ExecResult struct {
	Output    []byte
	ExitCode  int
	Truncated bool
}

// Run executes cmd on the remote host over a plain "exec" channel — never a
// PTY. Running a snippet across many hosts needs a real exit code and
// byte-for-byte output, not something scraped off a shell that thinks it is
// talking to a human; DialConn already keeps this package's shell-less path
// pty-free, and Run is the exec-shaped way to use it.
//
// A non-zero exit is a normal outcome, not a Go error: it comes back in
// ExitCode with err == nil and Output still holding whatever the command
// printed. err is reserved for "we don't know what happened" — the session
// could not be opened, the connection dropped mid-command, or ctx expired
// first.
//
// limit <= 0 captures everything. Above the limit, Output stops growing at
// exactly limit bytes and Truncated is set, but both pipes are drained to
// EOF regardless — the command still has to run to completion for ExitCode
// to mean anything, and on a large enough write, refusing to keep reading
// would stall it on the channel's own flow-control window instead.
func (c *Conn) Run(ctx context.Context, cmd string, limit int64) (ExecResult, error) {
	sess, err := c.client.NewSession()
	if err != nil {
		return ExecResult{}, fmt.Errorf("sshclient: new session: %w", err)
	}
	defer sess.Close()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		return ExecResult{}, fmt.Errorf("sshclient: stdout pipe: %w", err)
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return ExecResult{}, fmt.Errorf("sshclient: stderr pipe: %w", err)
	}

	if err := sess.Start(cmd); err != nil {
		return ExecResult{}, fmt.Errorf("sshclient: start %q: %w", cmd, err)
	}

	// The copiers start only once Start has succeeded: starting them earlier
	// (they were, briefly) meant a failed Start left them reading from pipes
	// nobody was ever going to close, two goroutines leaked forever on every
	// call that hit this path against an otherwise-healthy connection.
	buf := newLimitedBuffer(limit)
	var copiers sync.WaitGroup
	copiers.Add(2)
	go func() { defer copiers.Done(); _, _ = io.Copy(buf, stdout) }()
	go func() { defer copiers.Done(); _, _ = io.Copy(buf, stderr) }()

	done := make(chan error, 1)
	go func() {
		// Both pipes have to reach EOF before Wait's own view of the channel
		// closing is meaningful, and waiting for them here (rather than
		// letting Wait race the copiers) is what guarantees Output holds
		// everything the command printed by the time this goroutine reports.
		copiers.Wait()
		done <- sess.Wait()
	}()

	select {
	case <-ctx.Done():
		// sess.Close only sends our half of the channel close — on a link
		// that looks alive but acks nothing (one dark host in a fan-out,
		// not a cleanly dropped connection), the peer never replies, so
		// neither the copiers nor sess.Wait ever unblock. Waiting on done
		// here would make ctx cancellation not actually bound Run at all,
		// which defeats the reason a caller passes ctx in the first place.
		// done is buffered, so the goroutine's eventual send (if the
		// connection ever does die) still never blocks; sess is unreachable
		// to any other caller once this returns, so leaving it to a
		// goroutine nobody waits on costs nothing.
		//
		// The output IS returned, unlike an earlier version of this branch.
		// Cancellation is not an exotic path: a per-host timeout is the one
		// way a hung command ends, and the commonest hang is a sudo prompt
		// that a non-PTY exec can never answer. Discarding the buffer there
		// means the caller sees "context deadline exceeded" and nothing
		// else, when the host had already printed the line explaining
		// itself. Bytes() copies under limitedBuffer's own mutex, so
		// reading it while the copiers are still running is safe — the
		// snapshot may simply be short, which is the honest answer for a
		// command that never finished.
		_ = sess.Close()
		return ExecResult{Output: buf.Bytes(), Truncated: buf.Truncated()}, ctx.Err()
	case waitErr := <-done:
		res := ExecResult{Output: buf.Bytes(), Truncated: buf.Truncated()}
		if waitErr == nil {
			return res, nil
		}
		var exitErr *ssh.ExitError
		if errors.As(waitErr, &exitErr) {
			res.ExitCode = exitErr.ExitStatus()
			return res, nil
		}
		// Not an exit status at all (channel closed, connection dropped,
		// etc.) — this is the "could not run it" case, so err is non-nil,
		// but res.Output is kept: for a multi-host snippet run, whatever a
		// host printed before it dropped is usually the most useful thing
		// to show alongside the failure, not something to throw away.
		return res, fmt.Errorf("sshclient: run %q: %w", cmd, waitErr)
	}
}

// limitedBuffer collects bytes from concurrent writers — Run copies stdout
// and stderr into it on two goroutines at once — up to a fixed cap. Writes
// past the cap are accepted and reported as fully written rather than
// short-written: the point is bounding memory, not stopping the source, so
// Run's copy goroutines keep draining (and the remote command keeps
// finishing) even once nothing more is being kept.
type limitedBuffer struct {
	mu        sync.Mutex
	limit     int64 // <= 0 means unlimited
	buf       bytes.Buffer
	truncated bool
}

func newLimitedBuffer(limit int64) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if b.limit <= 0 {
		b.buf.Write(p)
		return n, nil
	}
	if room := b.limit - int64(b.buf.Len()); room > 0 {
		if int64(n) <= room {
			b.buf.Write(p)
			return n, nil
		}
		b.buf.Write(p[:room])
		b.truncated = true
		return n, nil
	}
	// Already at the cap: nothing more is kept, but this write still isn't
	// short — the whole point of never returning less than n here is that
	// the caller (io.Copy) keeps reading instead of treating a full buffer
	// as a reason to stop.
	b.truncated = true
	return n, nil
}

func (b *limitedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, b.buf.Len())
	copy(out, b.buf.Bytes())
	return out
}

func (b *limitedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}
