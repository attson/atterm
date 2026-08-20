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

	for _, r := range rec.ChannelRequests() {
		if r == "pty-req" {
			t.Fatal("Run must never request a pty")
		}
	}
}
