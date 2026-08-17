package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/sshclient"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// --- test doubles -----------------------------------------------------------

// forwardTestServer is an in-memory SSH server that, unlike the one in
// ssh_host_test.go, accepts "direct-tcpip" channels — the channel type local
// forwarding (`ssh -L`) is built on. startSSHTestServer's serve loop rejects
// every channel type except "session", so it cannot exercise a tunnel at all.
//
// It does the same thing a real sshd does for -L: read the target host/port
// out of the channel's extra data, dial it from the server side, and splice
// the TCP connection to the channel. "session" channels still echo, so one
// server can back both a terminal session and a tunnel in the same test —
// which is what the redline #2 test needs.
//
// It also counts SSH connections (to prove several rules share one) and can
// drop them all (to prove a lost connection tears its tunnels down).
type forwardTestServer struct {
	addr    string
	hostPub ssh.PublicKey

	mu     sync.Mutex
	conns  []*ssh.ServerConn
	opened int
	closed int
}

func startForwardingSSHTestServer(t *testing.T) *forwardTestServer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "u" && string(pass) == "pw" {
				return &ssh.Permissions{}, nil
			}
			return nil, io.EOF
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	s := &forwardTestServer{addr: ln.Addr().String(), hostPub: signer.PublicKey()}
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(nc, cfg)
		}
	}()
	return s
}

func (s *forwardTestServer) serve(nc net.Conn, cfg *ssh.ServerConfig) {
	sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		_ = nc.Close()
		return
	}
	s.mu.Lock()
	s.conns = append(s.conns, sc)
	s.opened++
	s.mu.Unlock()
	defer func() {
		_ = sc.Close()
		s.mu.Lock()
		s.closed++
		s.mu.Unlock()
	}()

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
					if r.WantReply {
						_ = r.Reply(r.Type == "pty-req" || r.Type == "shell" || r.Type == "window-change", nil)
					}
				}
			}()
			go func() { _, _ = io.Copy(ch, ch); _ = ch.Close() }()
		case "direct-tcpip":
			var p struct {
				DestAddr string
				DestPort uint32
				OrigAddr string
				OrigPort uint32
			}
			if err := ssh.Unmarshal(newCh.ExtraData(), &p); err != nil {
				_ = newCh.Reject(ssh.ConnectionFailed, "bad direct-tcpip payload")
				continue
			}
			target := net.JoinHostPort(p.DestAddr, strconv.Itoa(int(p.DestPort)))
			tc, err := net.DialTimeout("tcp", target, 2*time.Second)
			if err != nil {
				// Exactly what a real sshd does when the target refuses:
				// an *ssh.OpenChannelError on the client side, not a
				// transport failure.
				_ = newCh.Reject(ssh.ConnectionFailed, "dial "+target+": "+err.Error())
				continue
			}
			ch, chReqs, err := newCh.Accept()
			if err != nil {
				_ = tc.Close()
				return
			}
			go ssh.DiscardRequests(chReqs)
			go func() { _, _ = io.Copy(ch, tc); _ = ch.Close(); _ = tc.Close() }()
			go func() { _, _ = io.Copy(tc, ch); _ = ch.Close(); _ = tc.Close() }()
		default:
			_ = newCh.Reject(ssh.UnknownChannelType, "unsupported")
		}
	}
}

// dropConns kills every SSH connection the server has accepted, simulating the
// network drop from design §7 risk 1.
func (s *forwardTestServer) dropConns() {
	s.mu.Lock()
	conns := append([]*ssh.ServerConn(nil), s.conns...)
	s.conns = nil
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

func (s *forwardTestServer) counts() (opened, closed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opened, s.closed
}

// startEchoTarget stands in for the service behind the tunnel: a plain TCP
// server that echoes whatever it is sent.
func startEchoTarget(t *testing.T) (host, port string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(c, c); _ = c.Close() }()
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	return h, p
}

// writeKnownHostsFor pre-trusts the test server's host key. Tunnels never
// prompt for TOFU (there is no dialog behind a StartForward call), so the key
// has to already be in known_hosts — the same file the terminal path writes.
func writeKnownHostsFor(t *testing.T, addr string, pub ssh.PublicKey) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{knownhosts.Normalize(addr)}, pub)
	if err := os.WriteFile(p, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// newForwardTestApp wires an App with a saved host pointing at srv, carrying
// the given forward rules and a working password credential.
func newForwardTestApp(t *testing.T, srv *forwardTestServer, rules ...ForwardRule) (*App, SSHHost) {
	t.Helper()
	useIsolatedKeyring(t)
	host, port, _ := net.SplitHostPort(srv.addr)

	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}
	a.sshKnownHostsPath = writeKnownHostsFor(t, srv.addr, srv.hostPub)
	t.Cleanup(func() { a.tunnels.stopAll() })

	h, err := a.AddSSHHost(
		SSHHost{Host: host, Port: port, User: "u", AuthKind: "password", Forwards: rules},
		sshCredential{Password: "pw"},
	)
	if err != nil {
		t.Fatal(err)
	}
	return a, h
}

// localRule is a -L rule with an ephemeral bind port and no BindAddr, so the
// loopback default is what actually gets exercised.
func localRule(id, targetHost, targetPort string) ForwardRule {
	return ForwardRule{ID: id, Kind: "local", BindPort: "0", TargetHost: targetHost, TargetPort: targetPort}
}

func activeForward(t *testing.T, a *App, ruleID string) ActiveForward {
	t.Helper()
	for _, f := range a.ListActiveForwards() {
		if f.RuleID == ruleID {
			return f
		}
	}
	t.Fatalf("rule %s is not in ListActiveForwards: %+v", ruleID, a.ListActiveForwards())
	return ActiveForward{}
}

// --- the gate ---------------------------------------------------------------

// TestStartForwardRefusesProxiedHost pins that the tunnel path goes through
// the same jump-host gate as the terminal path, *before* it reads a
// credential. The host here has no credential stored at all, which is the
// discriminator: a gate that ran after the credential read would return
// errCredentialMissing instead of naming ProxyJump. Asserting no listener was
// opened covers the other half — a refused host must not grab a local port.
func TestStartForwardRefusesProxiedHost(t *testing.T) {
	useIsolatedKeyring(t)
	// A *reachable* address, so nothing here is unreachable by accident.
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	sshHost, sshPort, _ := net.SplitHostPort(srv.addr)

	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}
	a.sshKnownHostsPath = writeKnownHostsFor(t, srv.addr, srv.hostPub)
	t.Cleanup(func() { a.tunnels.stopAll() })

	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{
		ID: "p1", Alias: "db", Host: sshHost, Port: sshPort, User: "u",
		AuthKind:  "password",
		ProxyJump: "bastion",
		Forwards:  []ForwardRule{localRule("r1", targetHost, targetPort)},
	}}
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatal(err)
	}

	err := a.StartForward("p1", "r1")
	if err == nil {
		t.Fatal("must refuse to open a tunnel to a ProxyJump host")
	}
	if err.Error() == errCredentialMissing {
		t.Fatal("gate must fire before the credential read: got errCredentialMissing, " +
			"meaning StartForward resolved credentials first")
	}
	if !strings.Contains(err.Error(), "ProxyJump") {
		t.Fatalf("error must name the reason, got %v", err)
	}
	if got := a.ListActiveForwards(); len(got) != 0 {
		t.Fatalf("a refused host must not open a listener, got %+v", got)
	}
	if opened, _ := srv.counts(); opened != 0 {
		t.Fatalf("gate must fire before any dial, but the SSH server saw %d connections", opened)
	}
}

func TestStartForwardRefusesProxyCommandHost(t *testing.T) {
	useIsolatedKeyring(t)
	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}
	t.Cleanup(func() { a.tunnels.stopAll() })

	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{
		ID: "p2", Alias: "via-corkscrew", Host: "10.0.0.6", User: "root", AuthKind: "password",
		ProxyCommand: "corkscrew proxy 8080 %h %p",
		Forwards:     []ForwardRule{localRule("r1", "127.0.0.1", "5432")},
	}}
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatal(err)
	}

	err := a.StartForward("p2", "r1")
	if err == nil || !strings.Contains(err.Error(), "ProxyCommand") {
		t.Fatalf("error must name ProxyCommand, got %v", err)
	}
}

// --- binding ----------------------------------------------------------------

// TestStartForwardDefaultsBindToLoopback reads the *listener's* address rather
// than the rule's, because the rule's BindAddr is empty and the whole question
// is what an empty value resolves to. Binding 0.0.0.0 would put the forwarded
// service on the network for anyone with no SSH credential at all, on every
// device the rule syncs to.
func TestStartForwardDefaultsBindToLoopback(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv, localRule("r1", targetHost, targetPort))

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward: %v", err)
	}
	defer func() { _ = a.StopForward(h.ID, "r1") }()

	f := activeForward(t, a, "r1")
	bindHost, bindPort, err := net.SplitHostPort(f.ListenAddr)
	if err != nil {
		t.Fatalf("listen addr %q: %v", f.ListenAddr, err)
	}
	if bindHost != "127.0.0.1" {
		t.Fatalf("empty BindAddr must bind loopback, listener is on %q", f.ListenAddr)
	}
	if bindPort == "0" || bindPort == "" {
		t.Fatalf("listen addr must report the resolved port, got %q", f.ListenAddr)
	}
}

// --- traffic ----------------------------------------------------------------

func TestLocalForwardCarriesBytesBothWays(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv, localRule("r1", targetHost, targetPort))

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward: %v", err)
	}
	defer func() { _ = a.StopForward(h.ID, "r1") }()

	f := activeForward(t, a, "r1")
	c, err := net.DialTimeout("tcp", f.ListenAddr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial forwarded port: %v", err)
	}
	defer c.Close()

	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	const msg = "through the tunnel\n"
	if _, err := c.Write([]byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(buf) != msg {
		t.Fatalf("echo = %q, want %q", buf, msg)
	}

	if got := activeForward(t, a, "r1"); got.Conns != 1 {
		t.Fatalf("accepted connection count = %d, want 1", got.Conns)
	}
}

func TestStopForwardClosesListener(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv, localRule("r1", targetHost, targetPort))

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward: %v", err)
	}
	addr := activeForward(t, a, "r1").ListenAddr

	if err := a.StopForward(h.ID, "r1"); err != nil {
		t.Fatalf("StopForward: %v", err)
	}
	if got := a.ListActiveForwards(); len(got) != 0 {
		t.Fatalf("stopped forward still listed: %+v", got)
	}
	if c, err := net.DialTimeout("tcp", addr, 2*time.Second); err == nil {
		c.Close()
		t.Fatalf("connection to %s still accepted after StopForward", addr)
	}

	// The shared connection has no rules left, so it must be gone too — an
	// idle SSH login left behind after the last tunnel stops is exactly the
	// waste the shell-less Conn exists to avoid.
	waitFor(t, 3*time.Second, "ssh connection closed", func() bool {
		opened, closed := srv.counts()
		return opened == 1 && closed == 1
	})
}

// TestStopForwardCutsEstablishedConnections: closing the listener alone would
// leave an already-open forwarded connection running. Two rules share the SSH
// connection here on purpose — with only one rule, stopping it closes the
// whole SSH connection and every channel dies for free, which would hide the
// bug.
func TestStopForwardCutsEstablishedConnections(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv,
		localRule("r1", targetHost, targetPort),
		localRule("r2", targetHost, targetPort),
	)
	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward r1: %v", err)
	}
	if err := a.StartForward(h.ID, "r2"); err != nil {
		t.Fatalf("StartForward r2: %v", err)
	}
	defer func() { _ = a.StopForward(h.ID, "r2") }()

	c, err := net.DialTimeout("tcp", activeForward(t, a, "r1").ListenAddr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial forwarded port: %v", err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte("x")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := io.ReadFull(c, make([]byte, 1)); err != nil {
		t.Fatalf("echo before stop: %v", err)
	}

	if err := a.StopForward(h.ID, "r1"); err != nil {
		t.Fatalf("StopForward r1: %v", err)
	}
	// The deadline must not be what ends this read: a timeout is also a
	// non-nil error, so a test that only checks err != nil passes just as
	// happily when the connection is left dangling.
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	_, err = io.ReadFull(c, make([]byte, 1))
	if err == nil {
		t.Fatal("the established forwarded connection survived StopForward")
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatal("StopForward left the established forwarded connection open: " +
			"the read timed out instead of failing on a closed connection")
	}
}

// --- redline #2 -------------------------------------------------------------

// TestStartForwardDoesNotTouchSubscriberCounts is the core assertion of this
// task. Redline #2 is implemented by Session.SetSubscriberLifecycle: the
// 0→1 hook drives STREAM_REQUEST and the N→0 hook drives STREAM_STOP, so a
// tunnel that counted as a subscriber would keep PTY bytes uploading forever
// with nobody watching.
//
// So this installs the redline's own mechanism — the real lifecycle callbacks,
// the same way internal/relay's tests do — on a live SSH terminal session, and
// asserts neither one fires while a tunnel to the same host is started, used
// to carry traffic, and stopped. That is stronger than reading a derived
// count: it pins the exact hook the uplink hangs off.
func TestStartForwardDoesNotTouchSubscriberCounts(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv, localRule("r1", targetHost, targetPort))

	sshHost, sshPort, _ := net.SplitHostPort(srv.addr)
	sid, err := a.host.OpenSSHSession(context.Background(), SSHConnectReq{
		Host: sshHost, Port: sshPort, User: "u", AuthKind: "password", Password: "pw",
		AcceptHostKey: true,
	}, ssh.FixedHostKey(srv.hostPub))
	if err != nil {
		t.Fatalf("OpenSSHSession: %v", err)
	}
	defer func() { _ = a.host.CloseSession(sid) }()

	sess, ok := a.host.server.Registry().Get(sid)
	if !ok {
		t.Fatal("session not registered")
	}
	first := make(chan struct{}, 1)
	last := make(chan struct{}, 1)
	sess.SetSubscriberLifecycle(
		func() { first <- struct{}{} },
		func() { last <- struct{}{} },
	)

	sessionsBefore := len(a.host.server.Registry().List())

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward: %v", err)
	}
	// Not merely open: carrying bytes. A subscriber-ish implementation would
	// most plausibly register on first use.
	f := activeForward(t, a, "r1")
	c, err := net.DialTimeout("tcp", f.ListenAddr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial forwarded port: %v", err)
	}
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte("ping\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read back: %v", err)
	}
	_ = c.Close()

	if err := a.StopForward(h.ID, "r1"); err != nil {
		t.Fatalf("StopForward: %v", err)
	}
	// The hooks fire asynchronously; give them a chance to be wrong.
	time.Sleep(150 * time.Millisecond)

	select {
	case <-first:
		t.Fatal("starting a tunnel fired the 0→1 subscriber hook: the tunnel is being " +
			"counted as a subscriber, which would make an open tunnel upload PTY bytes forever")
	default:
	}
	select {
	case <-last:
		t.Fatal("stopping a tunnel fired the N→0 subscriber hook: the tunnel is entangled " +
			"with subscriber lifecycles")
	default:
	}

	if got := len(a.host.server.Registry().List()); got != sessionsBefore {
		t.Fatalf("session count changed from %d to %d: a tunnel must not be adopted as a session",
			sessionsBefore, got)
	}
	a.host.mu.Lock()
	nsessions := len(a.host.sessions)
	a.host.mu.Unlock()
	if nsessions != 1 {
		t.Fatalf("relayHost.sessions = %d, want just the terminal session", nsessions)
	}

	// Positive control: the assertions above are "nothing happened", which is
	// also what a test with dead hooks looks like. A real subscriber on the
	// same session, through the same channels, must fire them — otherwise the
	// silence above proves nothing.
	sub, _ := sess.Subscribe(0, "control-client", "control")
	select {
	case <-first:
	case <-time.After(2 * time.Second):
		t.Fatal("the lifecycle hooks never fire at all: this test cannot detect a violation")
	}
	sess.Unsubscribe(sub)
	select {
	case <-last:
	case <-time.After(2 * time.Second):
		t.Fatal("the N→0 hook never fires at all: this test cannot detect a violation")
	}
}

// --- failure modes ----------------------------------------------------------

// TestStartForwardPortInUse: Go's raw error is "listen tcp 127.0.0.1:x: bind:
// address already in use", which buries the one fact the user needs.
func TestStartForwardPortInUse(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)

	squatter, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer squatter.Close()
	_, busyPort, _ := net.SplitHostPort(squatter.Addr().String())

	rule := localRule("r1", targetHost, targetPort)
	rule.BindPort = busyPort
	a, h := newForwardTestApp(t, srv, rule)

	err = a.StartForward(h.ID, "r1")
	if err == nil {
		t.Fatal("expected a bind failure")
	}
	if !strings.Contains(err.Error(), "already in use") || !strings.Contains(err.Error(), busyPort) {
		t.Fatalf("error must say the local port is already in use and name it, got %v", err)
	}
	if got := a.ListActiveForwards(); len(got) != 0 {
		t.Fatalf("failed start must leave nothing behind, got %+v", got)
	}
	if opened, _ := srv.counts(); opened != 0 {
		t.Fatalf("a port conflict must not cost an SSH login, server saw %d connections", opened)
	}
}

// TestRulesOnOneHostShareOneConnection pins the refcount: N rules on a host
// mean one remote login, and the connection outlives every stop but the last.
func TestRulesOnOneHostShareOneConnection(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv,
		localRule("r1", targetHost, targetPort),
		localRule("r2", targetHost, targetPort),
	)

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward r1: %v", err)
	}
	if err := a.StartForward(h.ID, "r2"); err != nil {
		t.Fatalf("StartForward r2: %v", err)
	}
	if opened, _ := srv.counts(); opened != 1 {
		t.Fatalf("two rules on one host opened %d SSH connections, want 1", opened)
	}

	// Interleaved stop: the first stop must not pull the connection out from
	// under the rule still running.
	if err := a.StopForward(h.ID, "r1"); err != nil {
		t.Fatalf("StopForward r1: %v", err)
	}
	if _, closed := srv.counts(); closed != 0 {
		t.Fatal("connection closed while another rule is still using it")
	}
	f := activeForward(t, a, "r2")
	c, err := net.DialTimeout("tcp", f.ListenAddr, 3*time.Second)
	if err != nil {
		t.Fatalf("surviving rule stopped working after the other one stopped: %v", err)
	}
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte("x")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 1)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read back through surviving rule: %v", err)
	}
	_ = c.Close()

	if err := a.StopForward(h.ID, "r2"); err != nil {
		t.Fatalf("StopForward r2: %v", err)
	}
	waitFor(t, 3*time.Second, "connection closed after the last rule stopped", func() bool {
		_, closed := srv.counts()
		return closed == 1
	})

	// Starting again must dial a fresh connection rather than reuse a closed one.
	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("restart after full stop: %v", err)
	}
	defer func() { _ = a.StopForward(h.ID, "r1") }()
	if opened, _ := srv.counts(); opened != 2 {
		t.Fatalf("restart opened %d connections total, want 2", opened)
	}
}

// TestConcurrentStartsShareOneConnection exercises the other order the
// refcount has to survive: several rules starting at once, so all but the
// first arrive while the connection is still being dialed. They must wait for
// that one connection rather than each opening their own, and stopping them
// concurrently must still close it exactly once. Run under -race this also
// covers the manager's locking.
func TestConcurrentStartsShareOneConnection(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	rules := []ForwardRule{
		localRule("r1", targetHost, targetPort),
		localRule("r2", targetHost, targetPort),
		localRule("r3", targetHost, targetPort),
		localRule("r4", targetHost, targetPort),
	}
	a, h := newForwardTestApp(t, srv, rules...)

	var wg sync.WaitGroup
	errs := make([]error, len(rules))
	for i, r := range rules {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			errs[i] = a.StartForward(h.ID, id)
		}(i, r.ID)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("StartForward %s: %v", rules[i].ID, err)
		}
	}
	if opened, _ := srv.counts(); opened != 1 {
		t.Fatalf("%d concurrent starts opened %d SSH connections, want 1", len(rules), opened)
	}
	if got := len(a.ListActiveForwards()); got != len(rules) {
		t.Fatalf("active forwards = %d, want %d", got, len(rules))
	}

	for _, r := range rules {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := a.StopForward(h.ID, id); err != nil {
				t.Errorf("StopForward %s: %v", id, err)
			}
		}(r.ID)
	}
	wg.Wait()
	waitFor(t, 3*time.Second, "the shared connection closed exactly once", func() bool {
		opened, closed := srv.counts()
		return opened == 1 && closed == 1
	})
}

// TestLostConnectionStopsTunnelsWithReason covers design §7 risk 1: when the
// SSH connection dies the listener is closed and the rule is marked stopped
// with a reason, and nothing silently redials (a retry loop re-attempts auth
// and can trip remote lockout).
func TestLostConnectionStopsTunnelsWithReason(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv, localRule("r1", targetHost, targetPort))

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward: %v", err)
	}
	addr := activeForward(t, a, "r1").ListenAddr

	srv.dropConns()

	// The drop is noticed on next use: dial the forwarded port and let the
	// remote dial fail.
	if c, err := net.DialTimeout("tcp", addr, 2*time.Second); err == nil {
		_ = c.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = c.Write([]byte("x"))
		_, _ = io.ReadFull(c, make([]byte, 1))
		_ = c.Close()
	}

	waitFor(t, 5*time.Second, "rule marked stopped with a reason", func() bool {
		for _, f := range a.ListActiveForwards() {
			if f.RuleID == "r1" {
				return !f.Running && f.Error != ""
			}
		}
		return false
	})
	if c, err := net.DialTimeout("tcp", addr, 2*time.Second); err == nil {
		c.Close()
		t.Fatal("listener still open after the SSH connection dropped")
	}
	if opened, _ := srv.counts(); opened != 1 {
		t.Fatalf("a dropped connection must not be redialled automatically, server saw %d connections", opened)
	}
}

// TestLostConnectionStopsIdleTunnel is the case on-use detection cannot cover:
// nothing ever connects to the forwarded port, so no DialRemote ever fails.
// Without the keepalive watcher the rule reports Running: true forever, the
// panel lies about a tunnel that cannot carry a byte, and startLocal answers
// "already running" to a user trying to restart it after a network change —
// blocking the one action that would fix it.
func TestLostConnectionStopsIdleTunnel(t *testing.T) {
	old := tunnelKeepalive
	tunnelKeepalive = 50 * time.Millisecond
	t.Cleanup(func() { tunnelKeepalive = old })

	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv, localRule("r1", targetHost, targetPort))

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward: %v", err)
	}
	addr := activeForward(t, a, "r1").ListenAddr

	srv.dropConns() // and nothing ever dials the forwarded port

	waitFor(t, 5*time.Second, "idle rule marked stopped with a reason", func() bool {
		for _, f := range a.ListActiveForwards() {
			if f.RuleID == "r1" {
				return !f.Running && f.Error != ""
			}
		}
		return false
	})
	if c, err := net.DialTimeout("tcp", addr, 2*time.Second); err == nil {
		c.Close()
		t.Fatal("listener still open after the SSH connection dropped")
	}
	if opened, _ := srv.counts(); opened != 1 {
		t.Fatalf("a dropped connection must not be redialled automatically, server saw %d connections", opened)
	}

	// And the user can actually recover: restarting must not be refused with
	// "already running".
	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("restart after a drop must be allowed: %v", err)
	}
	defer func() { _ = a.StopForward(h.ID, "r1") }()
	f := activeForward(t, a, "r1")
	if !f.Running || f.Error != "" {
		t.Fatalf("restarted rule should be running with the error cleared: %+v", f)
	}
}

// TestDeliberateStopDoesNotSelfReport: Conn.Done fires on an ordinary Close
// too, so the watcher runs on every normal stop. It must not resurrect the
// rule as a stopped-with-error entry — the ordering that prevents it is
// running=false under m.mu *before* teardown releases the connection.
func TestDeliberateStopDoesNotSelfReport(t *testing.T) {
	old := tunnelKeepalive
	tunnelKeepalive = 50 * time.Millisecond
	t.Cleanup(func() { tunnelKeepalive = old })

	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv, localRule("r1", targetHost, targetPort))

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward: %v", err)
	}
	if err := a.StopForward(h.ID, "r1"); err != nil {
		t.Fatalf("StopForward: %v", err)
	}
	// Restart immediately, before the watcher for the closed connection has
	// necessarily woken up: it must not mistake the new tunnel for one of its
	// own and stop it.
	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	defer func() { _ = a.StopForward(h.ID, "r1") }()

	time.Sleep(300 * time.Millisecond) // let the old watcher wake up and find nothing
	f := activeForward(t, a, "r1")
	if !f.Running || f.Error != "" {
		t.Fatalf("the previous connection's watcher stopped the restarted tunnel: %+v", f)
	}
	// And it still works.
	c, err := net.DialTimeout("tcp", f.ListenAddr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial restarted forward: %v", err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte("x")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := io.ReadFull(c, make([]byte, 1)); err != nil {
		t.Fatalf("echo through restarted forward: %v", err)
	}
}

// TestFailedDialLeavesNoPoisonedConnection: a failed dial must not leave its
// entry in m.conns, or the next StartForward is handed the previous attempt's
// error instead of redialing — the user retries after fixing the network and
// gets the stale failure back.
func TestFailedDialLeavesNoPoisonedConnection(t *testing.T) {
	m := &tunnelManager{}
	dials := 0
	dial := func() (*sshclient.Conn, error) {
		dials++
		return nil, errors.New("dial refused")
	}

	if _, err := m.acquireConn("h", dial); err == nil {
		t.Fatal("expected the dial error")
	}
	m.mu.Lock()
	_, poisoned := m.conns["h"]
	m.mu.Unlock()
	if poisoned {
		t.Fatal("a failed dial left its entry in m.conns")
	}
	if _, err := m.acquireConn("h", dial); err == nil {
		t.Fatal("expected the dial error")
	}
	if dials != 2 {
		t.Fatalf("second acquire replayed the cached error instead of redialing (dials=%d)", dials)
	}
}

// TestConnectionLostOnlyTouchesItsOwnConnection pins the instance match
// deterministically, which the stop/restart test above cannot: whether the old
// connection's watcher wakes before or after the restart is a race, so that
// test can pass with a hostID match by pure timing. Here two tunnels on the
// *same host* ride two different connections, and only the one on the dead
// connection may be stopped — the shape you get for real when a rule is
// restarted onto a fresh connection while the old watcher is still in flight.
func TestConnectionLostOnlyTouchesItsOwnConnection(t *testing.T) {
	m := &tunnelManager{}

	newTun := func(ruleID string, hc *hostConn) (*tunnel, net.Listener) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		return &tunnel{
			hostID: "h", ruleID: ruleID, kind: forwardKindLocal,
			listener: ln, listenAddr: ln.Addr().String(),
			hc: hc, running: true, live: map[net.Conn]struct{}{},
		}, ln
	}

	dead := &hostConn{ready: make(chan struct{}), refs: 1}
	live := &hostConn{ready: make(chan struct{}), refs: 1}
	close(dead.ready)
	close(live.ready)

	tDead, _ := newTun("r1", dead)
	tLive, liveLn := newTun("r2", live)

	m.mu.Lock()
	m.ensureLocked()
	m.tuns[tunnelKey("h", "r1")] = tDead
	m.tuns[tunnelKey("h", "r2")] = tLive
	m.conns["h"] = live // the dead one has already been evicted
	m.mu.Unlock()

	m.connectionLost("h", dead, "boom")

	for _, f := range m.list() {
		switch f.RuleID {
		case "r1":
			if f.Running || f.Error == "" {
				t.Fatalf("the tunnel on the dead connection should be stopped with a reason: %+v", f)
			}
		case "r2":
			if !f.Running || f.Error != "" {
				t.Fatalf("a tunnel on a different connection must be untouched: %+v", f)
			}
		}
	}
	if c, err := net.DialTimeout("tcp", liveLn.Addr().String(), 2*time.Second); err != nil {
		t.Fatalf("the other connection's listener was closed: %v", err)
	} else {
		_ = c.Close()
	}
	m.mu.Lock()
	stillThere := m.conns["h"] == live
	m.mu.Unlock()
	if !stillThere {
		t.Fatal("the live connection was evicted by another connection's failure")
	}
}

// TestStartForwardUnknownHostKeyRefuses: there is no TOFU dialog behind a
// StartForward call, so an unknown fingerprint must be an error that tells the
// user where to accept it — never a silent accept.
func TestStartForwardUnknownHostKeyRefuses(t *testing.T) {
	useIsolatedKeyring(t)
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	sshHost, sshPort, _ := net.SplitHostPort(srv.addr)

	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}
	a.sshKnownHostsPath = filepath.Join(t.TempDir(), "known_hosts") // empty: nothing trusted
	t.Cleanup(func() { a.tunnels.stopAll() })

	h, err := a.AddSSHHost(
		SSHHost{Host: sshHost, Port: sshPort, User: "u", AuthKind: "password",
			Forwards: []ForwardRule{localRule("r1", targetHost, targetPort)}},
		sshCredential{Password: "pw"},
	)
	if err != nil {
		t.Fatal(err)
	}
	err = a.StartForward(h.ID, "r1")
	if err == nil {
		t.Fatal("must not connect to a host whose key is not in known_hosts")
	}
	if !strings.Contains(err.Error(), "known_hosts") {
		t.Fatalf("error should point at known_hosts, got %v", err)
	}
	if got := a.ListActiveForwards(); len(got) != 0 {
		t.Fatalf("failed start must leave nothing behind, got %+v", got)
	}
	if data, _ := os.ReadFile(a.sshKnownHostsPath); len(data) != 0 {
		t.Fatal("a tunnel must never write a host key to known_hosts on its own")
	}
}

func TestStartForwardRejectsUnknownRuleAndKind(t *testing.T) {
	srv := startForwardingSSHTestServer(t)
	bad := ForwardRule{ID: "r9", Kind: "sideways", BindPort: "0"}
	a, h := newForwardTestApp(t, srv, bad)

	if err := a.StartForward(h.ID, "nope"); err == nil {
		t.Fatal("expected an error for an unknown rule id")
	}
	if err := a.StartForward("nope", "r9"); err == nil {
		t.Fatal("expected an error for an unknown host id")
	}
	if err := a.StartForward(h.ID, "r9"); err == nil {
		t.Fatal("expected an error for an unknown forward kind")
	}
	if opened, _ := srv.counts(); opened != 0 {
		t.Fatalf("invalid rules must not dial, server saw %d connections", opened)
	}
}

func TestStopForwardUnknownIsAnError(t *testing.T) {
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv)
	if err := a.StopForward(h.ID, "never-started"); err == nil {
		t.Fatal("stopping a forward that is not running should report it")
	}
}

func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
