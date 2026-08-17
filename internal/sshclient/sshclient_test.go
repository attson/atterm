package sshclient

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"strconv"
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

// startJumpTestServer starts an in-memory ssh server that accepts user="u" /
// password="pw" like startTestServer, but additionally accepts
// "direct-tcpip" channels and forwards them to target — standing in for a
// bastion host. Every destination address the server was asked to open is
// recorded and retrievable via the returned jumpRecorder, which is how a
// test proves traffic actually passed through this server rather than
// around it.
func startJumpTestServer(t *testing.T, target string) (addr string, hostPub ssh.PublicKey, jump *jumpRecorder) {
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

	jump = &jumpRecorder{}
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go serveConnJump(nc, cfg, nil, jump, target)
		}
	}()
	return ln.Addr().String(), signer.PublicKey(), jump
}

// directTCPIPPayload is the RFC 4254 §7.2 payload of a "direct-tcpip"
// channel open request: the destination the client asked the server to
// connect to on its side, plus the originator (which the tests ignore).
type directTCPIPPayload struct {
	DestAddr string
	DestPort uint32
	OrigAddr string
	OrigPort uint32
}

// jumpRecorder records the destination address of every "direct-tcpip"
// channel a test server accepted, so a test can prove traffic went
// *through* that server rather than around it. Safe for concurrent use.
type jumpRecorder struct {
	mu    sync.Mutex
	dests []string
}

func (r *jumpRecorder) record(dest string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dests = append(r.dests, dest)
}

// Destinations returns a snapshot of the direct-tcpip destinations seen so
// far. Safe to call once the dial under test has completed.
func (r *jumpRecorder) Destinations() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.dests))
	copy(out, r.dests)
	return out
}

// serveConn handles one accepted connection. rec is optional; pass nil to
// skip recording (kept as a parameter, not a global, so concurrent test
// servers don't share state).
//
// It also accepts "direct-tcpip" channels (what Conn.DialRemote opens), so
// this server can stand in for a jump host: the channel's requested
// destination is recorded via jump, and the channel is wired up to an
// in-process listener the test controls (target) rather than to whatever
// the payload's DestAddr says — the test never needs this server to make a
// real outbound connection, only to prove it was asked to.
func serveConn(nc net.Conn, cfg *ssh.ServerConfig, rec *channelRequestRecorder) {
	serveConnJump(nc, cfg, rec, nil, "")
}

// serveConnJump is serveConn plus jump-host behaviour: "direct-tcpip"
// channels are accepted, their destination recorded into jump, and the
// channel bytes are piped to a connection dialed to target (the address of
// a server the test controls). If target is empty, direct-tcpip channels
// are rejected like any other unknown-to-this-server-mode channel type.
func serveConnJump(nc net.Conn, cfg *ssh.ServerConfig, rec *channelRequestRecorder, jump *jumpRecorder, target string) {
	sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	defer sc.Close()
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		switch newCh.ChannelType() {
		case "session":
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
		case "direct-tcpip":
			if target == "" {
				_ = newCh.Reject(ssh.UnknownChannelType, "only session")
				continue
			}
			var payload directTCPIPPayload
			if err := ssh.Unmarshal(newCh.ExtraData(), &payload); err != nil {
				_ = newCh.Reject(ssh.ConnectionFailed, "bad direct-tcpip payload")
				continue
			}
			if jump != nil {
				jump.record(net.JoinHostPort(payload.DestAddr, strconv.FormatUint(uint64(payload.DestPort), 10)))
			}
			ch, chReqs, err := newCh.Accept()
			if err != nil {
				return
			}
			go ssh.DiscardRequests(chReqs)
			targetConn, err := net.Dial("tcp", target)
			if err != nil {
				ch.Close()
				continue
			}
			go func() { _, _ = io.Copy(targetConn, ch); targetConn.Close() }()
			go func() { _, _ = io.Copy(ch, targetConn); ch.Close() }()
		default:
			_ = newCh.Reject(ssh.UnknownChannelType, "only session")
		}
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

// TestDialConnViaAnotherConn is the positive case for jump-host dialing: a
// Conn already established to a bastion is handed in as Via, and DialConn
// must reach the target *through* that bastion (Conn.DialRemote) instead of
// opening its own TCP connection to it. The two servers here are
// independent SSH test servers with independent host keys and independent
// listeners, so nothing about the target's own address is reachable from
// this test except via the bastion's direct-tcpip forwarding.
func TestDialConnViaAnotherConn(t *testing.T) {
	targetAddr, targetHostPub := startTestServer(t)
	bastionAddr, bastionHostPub, jump := startJumpTestServer(t, targetAddr)

	bastionHost, bastionPort, _ := net.SplitHostPort(bastionAddr)
	bastion, err := DialConn(context.Background(), Config{
		Host: bastionHost, Port: bastionPort, User: "u",
		Auth:      PasswordAuth{Password: "pw"},
		HostKeyCb: ssh.FixedHostKey(bastionHostPub),
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("DialConn(bastion): %v", err)
	}
	defer bastion.Close()

	targetHost, targetPort, _ := net.SplitHostPort(targetAddr)
	target, err := DialConn(context.Background(), Config{
		Host: targetHost, Port: targetPort, User: "u",
		Auth:      PasswordAuth{Password: "pw"},
		HostKeyCb: ssh.FixedHostKey(targetHostPub),
		Timeout:   5 * time.Second,
		Via:       bastion,
	})
	if err != nil {
		t.Fatalf("DialConn(target via bastion): %v", err)
	}
	defer target.Close()

	// The assertion that distinguishes "went through the bastion" from
	// "reached the target some other way": the *bastion* recorded the
	// direct-tcpip destination it was asked to open, on its own goroutine,
	// independent of anything the client claims. A DialConn that ignored
	// Via and dialed the target directly would pass every check above
	// (the handshake would still succeed) but leave this slice empty,
	// because the bastion would never have been contacted at all.
	dests := jump.Destinations()
	if len(dests) != 1 || dests[0] != targetAddr {
		t.Fatalf("bastion should have recorded exactly one direct-tcpip request for %q, got %v", targetAddr, dests)
	}

	// Confirm the resulting Conn is actually usable end to end (the SSH
	// transport on top of the forwarded channel works both ways), not just
	// that the handshake bytes happened to parse.
	sess, err := target.client.NewSession()
	if err != nil {
		t.Fatalf("NewSession over jumped Conn: %v", err)
	}
	sess.Close()
}

// TestDialConnWithoutViaStillDialsDirectly is the regression guard for this
// task: every SSH session and port forward in the app today calls DialConn
// with Via == nil, so that path must behave exactly as it did before Via
// existed. In particular, if a future change accidentally took the Via
// branch even when cfg.Via is nil, it would nil-pointer-dereference on
// cfg.Via.DialRemote — this test would panic instead of passing.
func TestDialConnWithoutViaStillDialsDirectly(t *testing.T) {
	addr, hostPub, rec := startTestServerRecorded(t)
	host, port, _ := net.SplitHostPort(addr)

	c, err := DialConn(context.Background(), Config{
		Host: host, Port: port, User: "u",
		Auth:      PasswordAuth{Password: "pw"},
		HostKeyCb: ssh.FixedHostKey(hostPub),
		Timeout:   5 * time.Second,
		// Via deliberately left unset: this is the default every existing
		// caller uses, and must keep dialing straight to addr.
	})
	if err != nil {
		t.Fatalf("DialConn: %v", err)
	}
	defer c.Close()

	sess, err := c.client.NewSession()
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess.Close()

	for _, r := range rec.ChannelRequests() {
		if r == "pty-req" || r == "shell" {
			t.Fatalf("DialConn must not open a shell; saw %q", r)
		}
	}
}
