package sshclient

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// channelRequestRecorder records the type of every channel request a test
// server receives, so tests can assert on what the client did (or didn't)
// ask for — in particular, whether it opened a pty/shell.
type channelRequestRecorder struct {
	mu   sync.Mutex
	reqs []string
}

func (r *channelRequestRecorder) record(reqType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, reqType)
}

// ChannelRequests returns a snapshot of the channel request types seen so
// far. Safe to call once the dial under test has completed.
func (r *channelRequestRecorder) ChannelRequests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.reqs))
	copy(out, r.reqs)
	return out
}

// startTestServer starts an in-memory ssh server that only accepts
// user="u" / password="pw". The remote "shell" echoes every byte back.
// It returns the listen address and the server host public key.
func startTestServer(t *testing.T) (addr string, hostPub ssh.PublicKey) {
	t.Helper()
	addr, hostPub, _ = startTestServerRecorded(t)
	return addr, hostPub
}

// startTestServerRecorded is startTestServer plus a recorder the test can
// use to inspect which channel requests the client sent.
func startTestServerRecorded(t *testing.T) (addr string, hostPub ssh.PublicKey, rec *channelRequestRecorder) {
	t.Helper()
	return startTestServerCfg(t, &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "u" && string(pass) == "pw" {
				return &ssh.Permissions{}, nil
			}
			return nil, io.EOF
		},
	})
}

// startTestServerWithKey starts a server that accepts user="u" authenticating
// with the given authorized public key.
func startTestServerWithKey(t *testing.T, authorized ssh.PublicKey) (addr string, hostPub ssh.PublicKey) {
	t.Helper()
	addr, hostPub, _ = startTestServerCfg(t, &ssh.ServerConfig{
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() == "u" && keyMarshalEqual(key, authorized) {
				return &ssh.Permissions{}, nil
			}
			return nil, io.EOF
		},
	})
	return addr, hostPub
}

func startTestServerCfg(t *testing.T, cfg *ssh.ServerConfig) (addr string, hostPub ssh.PublicKey, rec *channelRequestRecorder) {
	t.Helper()
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
			go serveConn(nc, cfg, rec)
		}
	}()
	return ln.Addr().String(), signer.PublicKey(), rec
}

// serveConn handles one accepted connection. rec is optional; pass nil to
// skip recording (kept as a parameter, not a global, so concurrent test
// servers don't share state).
func serveConn(nc net.Conn, cfg *ssh.ServerConfig, rec *channelRequestRecorder) {
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
		go func() {
			for r := range chReqs {
				if rec != nil {
					rec.record(r.Type)
				}
				if r.WantReply {
					_ = r.Reply(r.Type == "pty-req" || r.Type == "shell" || r.Type == "window-change", nil)
				}
			}
		}()
		go func() { _, _ = io.Copy(ch, ch); ch.Close() }() // echo
	}
}

func keyMarshalEqual(a, b ssh.PublicKey) bool {
	return string(a.Marshal()) == string(b.Marshal())
}

func TestDialPasswordEchoRoundTrip(t *testing.T) {
	addr, hostPub := startTestServer(t)
	host, port, _ := net.SplitHostPort(addr)

	sess, err := Dial(context.Background(), Config{
		Host: host, Port: port, User: "u",
		Auth:      PasswordAuth{Password: "pw"},
		HostKeyCb: ssh.FixedHostKey(hostPub),
		Cols:      80, Rows: 24,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer sess.Close()

	if _, err := sess.Write([]byte("ping")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(sess, buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf) != "ping" {
		t.Fatalf("echo mismatch: %q", buf)
	}
}

func TestDialPrivateKey(t *testing.T) {
	clientKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer, _ := ssh.NewSignerFromKey(clientKey)
	pemBytes := marshalPEM(t, clientKey)

	addr, hostPub := startTestServerWithKey(t, signer.PublicKey())
	host, port, _ := net.SplitHostPort(addr)

	sess, err := Dial(context.Background(), Config{
		Host: host, Port: port, User: "u",
		Auth:      PrivateKeyAuth{PEM: pemBytes},
		HostKeyCb: ssh.FixedHostKey(hostPub),
		Cols:      80, Rows: 24, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	sess.Close()
}

func marshalPEM(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	der := x509.MarshalPKCS1PrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})
}

func TestDialWrongPasswordAuthError(t *testing.T) {
	addr, hostPub := startTestServer(t)
	host, port, _ := net.SplitHostPort(addr)
	_, err := Dial(context.Background(), Config{
		Host: host, Port: port, User: "u",
		Auth:      PasswordAuth{Password: "WRONG"},
		HostKeyCb: ssh.FixedHostKey(hostPub),
		Timeout:   5 * time.Second,
	})
	if err == nil {
		t.Fatal("expected auth error")
	}
	if !IsAuthError(err) {
		t.Fatalf("expected auth-classified error, got %v", err)
	}
}

func TestDialConnRequestsNoPTYAndNoShell(t *testing.T) {
	addr, hostPub, rec := startTestServerRecorded(t)
	host, port, _ := net.SplitHostPort(addr)

	c, err := DialConn(context.Background(), Config{
		Host: host, Port: port, User: "u",
		Auth:      PasswordAuth{Password: "pw"},
		HostKeyCb: ssh.FixedHostKey(hostPub),
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("DialConn: %v", err)
	}
	defer c.Close()

	for _, r := range rec.ChannelRequests() {
		if r == "pty-req" || r == "shell" {
			t.Fatalf("DialConn must not open a shell; saw %q", r)
		}
	}
}

// TestDialStillOpensShell is the regression guard for this refactor: Dial's
// externally-visible behaviour (PTY + shell) must survive being rebuilt on
// top of DialConn.
func TestDialStillOpensShell(t *testing.T) {
	addr, hostPub, rec := startTestServerRecorded(t)
	host, port, _ := net.SplitHostPort(addr)

	s, err := Dial(context.Background(), Config{
		Host: host, Port: port, User: "u",
		Auth:      PasswordAuth{Password: "pw"},
		HostKeyCb: ssh.FixedHostKey(hostPub),
		Cols:      80, Rows: 24,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer s.Close()

	var sawPty, sawShell bool
	for _, r := range rec.ChannelRequests() {
		sawPty = sawPty || r == "pty-req"
		sawShell = sawShell || r == "shell"
	}
	if !sawPty || !sawShell {
		t.Fatalf("Dial must still request a pty and a shell; pty=%v shell=%v", sawPty, sawShell)
	}
}

// TestConnCloseFromManyGoroutines pins that Close is safe to call
// concurrently. The old implementation was a check-then-close on closeCh
// (`select { case <-closeCh: default: close(closeCh) }`), which lets two
// goroutines both observe "not closed" and both call close — a
// "close of closed channel" panic that no recover in this codebase catches
// and that takes every terminal session down with it.
//
// This is not a theoretical race: the keepalive loop closes on a failed ping
// while the tunnel manager's last releaseConn closes on the same transport
// death, and both fire within milliseconds of each other.
//
// The shape matters. All closers wait on one barrier channel so they enter
// Close at the same instant, and the whole thing repeats over many fresh
// connections, because a single attempt lands in the window only sometimes.
// Run it with -race.
func TestConnCloseFromManyGoroutines(t *testing.T) {
	addr, hostPub := startTestServer(t)
	host, port, _ := net.SplitHostPort(addr)

	const rounds, closers = 40, 12
	for i := 0; i < rounds; i++ {
		c, err := DialConn(context.Background(), Config{
			Host: host, Port: port, User: "u",
			Auth:      PasswordAuth{Password: "pw"},
			HostKeyCb: ssh.FixedHostKey(hostPub),
			Timeout:   5 * time.Second,
			// Short enough that the keepalive goroutine is also live and
			// pinging while the closers run, so the real racing pair
			// (keepalive vs. an explicit Close) is exercised too.
			Keepalive: time.Millisecond,
		})
		if err != nil {
			t.Fatalf("DialConn: %v", err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		for j := 0; j < closers; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_ = c.Close()
			}()
		}
		close(start)
		wg.Wait()

		select {
		case <-c.Done():
		default:
			t.Fatal("Done must be closed after Close returned")
		}
	}
}
