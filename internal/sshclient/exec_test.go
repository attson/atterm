package sshclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// execReqPayload is the RFC 4254 §6.5 payload of an "exec" channel request:
// the command line to run.
type execReqPayload struct{ Command string }

// serverExitStatusMsg is the RFC 4254 §6.10 payload of an "exit-status"
// channel request. x/crypto/ssh has an identical type internally but does
// not export it, so the test server needs its own to marshal one.
type serverExitStatusMsg struct{ Status uint32 }

// execHandler is invoked once per "exec" request a test server channel
// receives, with the channel already accepted and the exec payload already
// replied to. It owns writing output and reporting an exit status (or not,
// for the cancellation test) via sendExitStatus.
type execHandler func(ch ssh.Channel, cmd string)

// sendExitStatus reports code and closes ch, mirroring what a real sshd
// does once the remote process exits: an exit-status request first, then
// channel close, both of which are what sess.Wait on the client side is
// waiting for.
func sendExitStatus(ch ssh.Channel, code int) {
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(&serverExitStatusMsg{Status: uint32(code)}))
	_ = ch.Close()
}

// execWriteAndExit builds the common execHandler shape used by most of
// these tests: write canned stdout/stderr, then report code and close.
func execWriteAndExit(stdout, stderr []byte, code int) execHandler {
	return func(ch ssh.Channel, cmd string) {
		if len(stdout) > 0 {
			_, _ = ch.Write(stdout)
		}
		if len(stderr) > 0 {
			_, _ = ch.Stderr().Write(stderr)
		}
		sendExitStatus(ch, code)
	}
}

// startExecTestServer starts an in-memory ssh server that accepts user="u" /
// password="pw", accepts "session" channels, and hands every "exec" request
// it sees to handle. Every channel request type is recorded via the same
// channelRequestRecorder the rest of this package's tests use, which is how
// TestRunDoesNotRequestPty proves Run never sends a "pty-req".
func startExecTestServer(t *testing.T, handle execHandler) (addr string, hostPub ssh.PublicKey, rec *channelRequestRecorder) {
	t.Helper()
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "u" && string(pass) == "pw" {
				return &ssh.Permissions{}, nil
			}
			return nil, io.EOF
		},
	}
	hostKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	rec = &channelRequestRecorder{}
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go serveExecConn(nc, cfg, rec, handle)
		}
	}()
	return ln.Addr().String(), signer.PublicKey(), rec
}

func serveExecConn(nc net.Conn, cfg *ssh.ServerConfig, rec *channelRequestRecorder, handle execHandler) {
	sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	defer sc.Close()
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "only session")
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			return
		}
		go func(ch ssh.Channel, chReqs <-chan *ssh.Request) {
			for r := range chReqs {
				rec.record(r.Type)
				if r.Type != "exec" {
					if r.WantReply {
						_ = r.Reply(false, nil)
					}
					continue
				}
				var payload execReqPayload
				if err := ssh.Unmarshal(r.Payload, &payload); err != nil {
					if r.WantReply {
						_ = r.Reply(false, nil)
					}
					continue
				}
				if r.WantReply {
					_ = r.Reply(true, nil)
				}
				handle(ch, payload.Command)
			}
		}(ch, chReqs)
	}
}

func dialExecTestConn(t *testing.T, addr string, hostPub ssh.PublicKey) *Conn {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	c, err := DialConn(context.Background(), Config{
		Host: host, Port: port, User: "u",
		Auth:      PasswordAuth{Password: "pw"},
		HostKeyCb: ssh.FixedHostKey(hostPub),
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("DialConn: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestRunCapturesStdoutAndStderrAndZeroExit(t *testing.T) {
	addr, hostPub, _ := startExecTestServer(t, execWriteAndExit([]byte("out-bytes"), []byte("err-bytes"), 0))
	c := dialExecTestConn(t, addr, hostPub)

	res, err := c.Run(context.Background(), "irrelevant", 0)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
	if !bytes.Contains(res.Output, []byte("out-bytes")) || !bytes.Contains(res.Output, []byte("err-bytes")) {
		t.Fatalf("Output = %q, want it to contain both stdout and stderr", res.Output)
	}
	if res.Truncated {
		t.Fatal("Truncated = true, want false")
	}
}

func TestRunNonZeroExitIsNotAnError(t *testing.T) {
	addr, hostPub, _ := startExecTestServer(t, execWriteAndExit([]byte("boom"), nil, 3))
	c := dialExecTestConn(t, addr, hostPub)

	res, err := c.Run(context.Background(), "false", 0)
	if err != nil {
		t.Fatalf("Run returned an error for a non-zero exit: %v", err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("ExitCode = %d, want 3", res.ExitCode)
	}
	if string(res.Output) != "boom" {
		t.Fatalf("Output = %q, want %q", res.Output, "boom")
	}
}

func TestRunTruncatesAtLimitAndStillReportsExitCode(t *testing.T) {
	big := bytes.Repeat([]byte("x"), 10_000)
	addr, hostPub, _ := startExecTestServer(t, execWriteAndExit(big, nil, 5))
	c := dialExecTestConn(t, addr, hostPub)

	const limit = 100
	res, err := c.Run(context.Background(), "yes x", limit)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Output) != limit {
		t.Fatalf("len(Output) = %d, want exactly %d", len(res.Output), limit)
	}
	if !res.Truncated {
		t.Fatal("Truncated = false, want true")
	}
	if res.ExitCode != 5 {
		t.Fatalf("ExitCode = %d, want 5 — the command must still run to completion when truncated", res.ExitCode)
	}
}

func TestRunUnlimitedWhenLimitIsZero(t *testing.T) {
	payload := bytes.Repeat([]byte("y"), 5_000)
	addr, hostPub, _ := startExecTestServer(t, execWriteAndExit(payload, nil, 0))
	c := dialExecTestConn(t, addr, hostPub)

	res, err := c.Run(context.Background(), "cat big", 0)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !bytes.Equal(res.Output, payload) {
		t.Fatalf("len(Output) = %d, want %d (unlimited)", len(res.Output), len(payload))
	}
	if res.Truncated {
		t.Fatal("Truncated = true, want false when limit <= 0")
	}
}

func TestRunCancelledContextReturnsCtxErr(t *testing.T) {
	// The handler never sends exit-status or closes the channel — it just
	// blocks until the test's cleanup tears the listener down — so the only
	// way Run can return is via ctx cancellation closing the session itself.
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	addr, hostPub, _ := startExecTestServer(t, func(ch ssh.Channel, cmd string) {
		<-block
	})
	c := dialExecTestConn(t, addr, hostPub)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.Run(ctx, "sleep forever", 0)
	elapsed := time.Since(start)
	if err != context.DeadlineExceeded {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Run took %s to return after ctx expired, want it bounded near the 100ms timeout", elapsed)
	}
}

// A per-host timeout is how a hung command ends, and the commonest hang is a
// sudo prompt a non-PTY exec can never answer — so whatever the host managed
// to print before it stalled is the only thing explaining the timeout. An
// earlier version of this branch returned a zero ExecResult and threw it away.
func TestRunCancelledContextKeepsWhatTheHostAlreadyPrinted(t *testing.T) {
	const printed = "sudo: a password is required\n"

	wrote := make(chan struct{})
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	addr, hostPub, _ := startExecTestServer(t, func(ch ssh.Channel, cmd string) {
		_, _ = ch.Write([]byte(printed))
		close(wrote)
		// Never sends exit-status: the command is stuck at a prompt.
		<-block
	})
	c := dialExecTestConn(t, addr, hostPub)

	// Cancel only after the bytes are demonstrably on the wire, so this test
	// pins "output is kept" rather than racing "output arrived at all".
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-wrote
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	res, err := c.Run(ctx, "sudo systemctl restart nginx", 0)
	if err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if string(res.Output) != printed {
		t.Fatalf("Output = %q, want %q — the cancellation path must not discard what the host printed", res.Output, printed)
	}
}

func TestRunConcurrentCallsOnOneConn(t *testing.T) {
	addr, hostPub, _ := startExecTestServer(t, func(ch ssh.Channel, cmd string) {
		_, _ = ch.Write([]byte(cmd))
		sendExitStatus(ch, 0)
	})
	c := dialExecTestConn(t, addr, hostPub)

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	outs := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := fmt.Sprintf("echo %d", i)
			res, err := c.Run(context.Background(), cmd, 0)
			errs[i] = err
			outs[i] = string(res.Output)
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: Run: %v", i, errs[i])
		}
		want := fmt.Sprintf("echo %d", i)
		if outs[i] != want {
			t.Fatalf("goroutine %d: Output = %q, want %q", i, outs[i], want)
		}
	}
}

func TestRunDoesNotRequestPty(t *testing.T) {
	addr, hostPub, rec := startExecTestServer(t, execWriteAndExit([]byte("ok"), nil, 0))
	c := dialExecTestConn(t, addr, hostPub)

	if _, err := c.Run(context.Background(), "true", 0); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := rec.ChannelRequests()
	sawExec := false
	for _, r := range reqs {
		if r == "pty-req" {
			t.Fatal("Run must never request a pty")
		}
		if r == "exec" {
			sawExec = true
		}
	}
	// Without this, the loop above passes vacuously if the recorder ever
	// comes back empty (e.g. Run silently not sending anything at all) —
	// the negative check alone doesn't prove Run did the thing it was
	// supposed to do, only that it didn't do the one thing it mustn't.
	if !sawExec {
		t.Fatalf(`channel requests = %v, want an "exec" request among them`, reqs)
	}
}

// TestLimitedBufferNeverHoldsMoreThanLimit is the white-box pin for
// limitedBuffer's actual memory bound: every other test only observes the
// bytes Run hands back, which a buffer-everything-then-slice implementation
// would also get right. Only inspecting the unexported buf field mid-write
// (same package, so this is legal) proves the cap is enforced as data
// arrives rather than after the fact.
func TestLimitedBufferNeverHoldsMoreThanLimit(t *testing.T) {
	const limit = 100
	b := newLimitedBuffer(limit)
	chunk := bytes.Repeat([]byte("z"), 4096)

	const totalWant = 10 * 1024 * 1024
	written := 0
	for written < totalWant {
		n, err := b.Write(chunk)
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		if n != len(chunk) {
			t.Fatalf("Write returned a short write (n=%d, want %d) — Run's copy goroutines rely on Write "+
				"never doing this, or a large enough command output stalls instead of finishing", n, len(chunk))
		}
		written += n
		if got := b.buf.Len(); got > limit {
			t.Fatalf("underlying buffer holds %d bytes after writing %d total, want <= %d at every point", got, written, limit)
		}
	}

	if !b.Truncated() {
		t.Fatal("Truncated() = false after writing far past the limit")
	}
	if got := len(b.Bytes()); got != limit {
		t.Fatalf("len(Bytes()) = %d, want exactly %d", got, limit)
	}
}

// blackholeProxy sits between the test's ssh client and a real ssh test
// server. Until frozen it relays bytes verbatim in both directions; once
// frozen, it keeps draining both sides (so neither end ever sees a write
// error or backpressure) but stops forwarding anything at all, so nothing
// further crosses it — a network partition, not a clean disconnect and not
// a closed socket. MAJOR 1's bug only reproduces against exactly this
// shape: a link that still looks alive but acks nothing, ever.
type blackholeProxy struct {
	frozen atomic.Bool
}

func (p *blackholeProxy) relay(dst, src net.Conn) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 && !p.frozen.Load() {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// startBlackholeProxy starts a TCP proxy in front of backend and returns its
// own address (what the test should dial instead of backend directly) and
// the proxy so the test can freeze it mid-connection.
func startBlackholeProxy(t *testing.T, backend string) (addr string, proxy *blackholeProxy) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	proxy = &blackholeProxy{}
	go func() {
		for {
			clientConn, err := ln.Accept()
			if err != nil {
				return
			}
			serverConn, err := net.Dial("tcp", backend)
			if err != nil {
				clientConn.Close()
				continue
			}
			go proxy.relay(serverConn, clientConn)
			go proxy.relay(clientConn, serverConn)
		}
	}()
	return ln.Addr().String(), proxy
}

// TestRunCancelledContextBoundsRunEvenIfPeerNeverAcksClose is MAJOR 1's
// regression pin: a peer that stops acknowledging anything — as opposed to
// one that is merely slow, or a connection that drops cleanly — must not be
// able to turn ctx cancellation into an unbounded wait. The exec handler
// also never sends an exit-status, so with the pre-fix code (which waited
// on <-done inside the ctx.Done() branch) this hangs forever: sess.Close
// only sends our half of the channel close, the frozen proxy swallows it,
// and neither the copiers nor sess.Wait have anything left to unblock them.
//
// The test bounds its own wait with a 5s select instead of just calling Run
// and letting it block: that is the difference between a regression here
// failing this test in a few seconds versus hanging the whole test binary,
// which is what actually happened before this test existed (a 300ms ctx
// timeout, Run still blocked 5s later).
func TestRunCancelledContextBoundsRunEvenIfPeerNeverAcksClose(t *testing.T) {
	backendAddr, hostPub, _ := startExecTestServer(t, func(ch ssh.Channel, cmd string) {
		select {} // the remote command "runs" forever; it never gets the chance to matter
	})
	proxyAddr, proxy := startBlackholeProxy(t, backendAddr)

	host, port, err := net.SplitHostPort(proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	c, err := DialConn(context.Background(), Config{
		Host: host, Port: port, User: "u",
		Auth:      PasswordAuth{Password: "pw"},
		HostKeyCb: ssh.FixedHostKey(hostPub),
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("DialConn: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	go func() {
		// Give the exec request/reply — already in flight when Run starts —
		// time to land before the link goes dark. Freezing has to happen
		// after Start succeeds, or this would just be re-proving DialConn's
		// own connect-time behaviour instead of MAJOR 1's Wait-time bug.
		time.Sleep(200 * time.Millisecond)
		proxy.frozen.Store(true)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	type result struct {
		err     error
		elapsed time.Duration
	}
	resCh := make(chan result, 1)
	start := time.Now()
	go func() {
		_, runErr := c.Run(ctx, "sleep forever", 0)
		resCh <- result{err: runErr, elapsed: time.Since(start)}
	}()

	select {
	case res := <-resCh:
		if res.err != context.DeadlineExceeded {
			t.Fatalf("err = %v, want context.DeadlineExceeded", res.err)
		}
		if res.elapsed > 5*time.Second {
			t.Fatalf("Run took %s to return after the link went dark, want it bounded near the 400ms ctx timeout", res.elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s of ctx expiring against an unresponsive peer " +
			"(MAJOR 1 regression: ctx cancellation is not actually bounding Run)")
	}
}

// startExecDropConnTestServer starts an in-memory ssh server that accepts
// user="u"/password="pw", accepts one "session" channel, replies to a single
// "exec" request, writes partial to it, and then slams the whole ssh
// connection shut without ever sending an exit-status or a clean channel
// close — standing in for a host whose connection drops mid-command.
//
// This is deliberately not built on serveExecConn/execHandler: that harness
// only ever closes the ssh *channel* through a connection that stays alive,
// which can't produce the failure MAJOR 2 needs (the whole transport dying
// mid-command), so this needs direct access to the ssh.Conn that
// serveExecConn's callers never see.
func startExecDropConnTestServer(t *testing.T, partial []byte) (addr string, hostPub ssh.PublicKey) {
	t.Helper()
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "u" && string(pass) == "pw" {
				return &ssh.Permissions{}, nil
			}
			return nil, io.EOF
		},
	}
	hostKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
		if err != nil {
			return
		}
		go ssh.DiscardRequests(reqs)
		for newCh := range chans {
			if newCh.ChannelType() != "session" {
				_ = newCh.Reject(ssh.UnknownChannelType, "only session")
				continue
			}
			ch, chReqs, err := newCh.Accept()
			if err != nil {
				return
			}
			go func() {
				for r := range chReqs {
					if r.Type != "exec" {
						if r.WantReply {
							_ = r.Reply(false, nil)
						}
						continue
					}
					if r.WantReply {
						_ = r.Reply(true, nil)
					}
					if len(partial) > 0 {
						_, _ = ch.Write(partial)
					}
					// Give the bytes time to actually land on the client
					// before yanking the connection out from under it —
					// otherwise this test would be racing TCP delivery
					// against the drop instead of reliably exercising it.
					time.Sleep(100 * time.Millisecond)
					_ = sc.Close() // hard drop: no exit-status, no clean channel close
				}
			}()
		}
	}()
	return ln.Addr().String(), signer.PublicKey()
}

// TestRunConnectionDropMidCommandIsAnError is MAJOR 2's regression pin: the
// "could not run it" half of Run's contract was previously unpinned by any
// test, so an implementation that never returns an error (e.g. mutating the
// non-ExitError branch to `return res, nil`) passed the whole suite. This
// drops the entire connection mid-command — no exit-status, no clean
// channel close — and requires a non-nil error back.
//
// It also pins RULING 6: res.Output must still hold what the command
// printed before the drop, since for a multi-host snippet run that partial
// output is usually the most useful diagnostic a failed host can offer.
func TestRunConnectionDropMidCommandIsAnError(t *testing.T) {
	const partial = "partial-output-before-drop"
	addr, hostPub := startExecDropConnTestServer(t, []byte(partial))

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	c, err := DialConn(context.Background(), Config{
		Host: host, Port: port, User: "u",
		Auth:      PasswordAuth{Password: "pw"},
		HostKeyCb: ssh.FixedHostKey(hostPub),
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("DialConn: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	res, err := c.Run(context.Background(), "long running command", 0)
	if err == nil {
		t.Fatal("Run must return a non-nil error when the connection drops mid-command")
	}
	if string(res.Output) != partial {
		t.Fatalf("Output = %q, want the partial output printed before the drop (%q) to be kept alongside the error", res.Output, partial)
	}
}
