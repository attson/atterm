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

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/net/proxy"
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

	mu           sync.Mutex
	conns        []*ssh.ServerConn
	opened       int
	closed       int
	refuseRemote bool // when true, every "tcpip-forward" request is denied

	// directDests records the DestAddr of every "direct-tcpip" channel the
	// client opened, exactly as it arrived on the wire. That is the far side of
	// the tunnel, so it is where "the name was not resolved locally" can
	// actually be observed rather than assumed.
	directDests []string
	// directTargets records the same channels as host:port. A jump-chain test
	// needs the port to tell one hop from the next: every hop in these tests
	// listens on 127.0.0.1, so DestAddr alone cannot say *which* server this
	// one was asked to reach.
	directTargets []string

	// authAttempts records every username/password pair presented to this
	// server, accepted or not. It is what makes "the bastion was dialled with
	// its own credential" an assertion rather than an inference: a chain that
	// reused the target's credential would still connect if both happened to
	// share one, and would show up here.
	authAttempts []string
}

// directDestHosts returns the host part of every direct-tcpip destination this
// server has been asked for, in order.
func (s *forwardTestServer) directDestHosts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.directDests...)
}

// directDestAddrs returns every direct-tcpip destination as host:port, in
// order — the chain topology as this server saw it.
func (s *forwardTestServer) directDestAddrs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.directTargets...)
}

// authCredentials returns every "user:password" this server was offered.
func (s *forwardTestServer) authCredentials() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.authAttempts...)
}

// setRefuseRemoteForward makes every subsequent "tcpip-forward" global
// request fail, the way a real sshd would refuse to bind a non-loopback
// address under `GatewayPorts no` or a privileged port without root. It
// exercises the failure path startRemote has to report with wording that
// points at the remote, not at a local port conflict.
func (s *forwardTestServer) setRefuseRemoteForward(v bool) {
	s.mu.Lock()
	s.refuseRemote = v
	s.mu.Unlock()
}

func (s *forwardTestServer) shouldRefuseRemoteForward() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refuseRemote
}

// remoteForwardState tracks the listeners one SSH connection has asked the
// test server to open via "tcpip-forward", keyed by the address+port the
// client used to register the forward (the same key `cancel-tcpip-forward`
// carries). Scoped to a single connection's serve goroutine, so it needs its
// own lock but nothing shared across connections.
type remoteForwardState struct {
	mu  sync.Mutex
	lns map[string]net.Listener
}

func newRemoteForwardState() *remoteForwardState {
	return &remoteForwardState{lns: map[string]net.Listener{}}
}

func (r *remoteForwardState) add(key string, ln net.Listener) {
	r.mu.Lock()
	r.lns[key] = ln
	r.mu.Unlock()
}

func (r *remoteForwardState) remove(key string) net.Listener {
	r.mu.Lock()
	ln := r.lns[key]
	delete(r.lns, key)
	r.mu.Unlock()
	return ln
}

func (r *remoteForwardState) closeAll() {
	r.mu.Lock()
	lns := r.lns
	r.lns = map[string]net.Listener{}
	r.mu.Unlock()
	for _, ln := range lns {
		_ = ln.Close()
	}
}

func startForwardingSSHTestServer(t *testing.T) *forwardTestServer {
	t.Helper()
	return startForwardingSSHTestServerAs(t, "u", "pw")
}

// startForwardingSSHTestServerAs is startForwardingSSHTestServer with a
// caller-chosen credential. Jump-chain tests give every hop a *different* one,
// so "each hop authenticated with its own saved credential" can be asserted
// instead of assumed.
func startForwardingSSHTestServerAs(t *testing.T, user, password string) *forwardTestServer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	s := &forwardTestServer{addr: ln.Addr().String(), hostPub: signer.PublicKey()}

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			s.mu.Lock()
			s.authAttempts = append(s.authAttempts, c.User()+":"+string(pass))
			s.mu.Unlock()
			if c.User() == user && string(pass) == password {
				return &ssh.Permissions{}, nil
			}
			return nil, io.EOF
		},
	}
	cfg.AddHostKey(signer)

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

	remoteFwd := newRemoteForwardState()
	defer func() {
		_ = sc.Close()
		remoteFwd.closeAll()
		s.mu.Lock()
		s.closed++
		s.mu.Unlock()
	}()

	go s.handleGlobalRequests(sc, reqs, remoteFwd)
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
			s.mu.Lock()
			s.directDests = append(s.directDests, p.DestAddr)
			s.directTargets = append(s.directTargets, target)
			s.mu.Unlock()
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

// channelForwardMsg is RFC 4254 §7.1's payload for both "tcpip-forward" and
// "cancel-tcpip-forward" — same shape, same field order, used for both.
type channelForwardMsg struct {
	Addr string
	Port uint32
}

// forwardedTCPPayload is RFC 4254 §7.2's payload for a "forwarded-tcpip"
// channel open: the address/port the connection came in on, and the
// originator's address/port. golang.org/x/crypto/ssh's client matches an
// incoming "forwarded-tcpip" channel to the right net.Listener purely by
// comparing this Addr/Port against what it registered when it sent
// "tcpip-forward" — so Addr here must be the exact string the client sent,
// and Port must be the port that request actually got bound to.
type forwardedTCPPayload struct {
	Addr       string
	Port       uint32
	OriginAddr string
	OriginPort uint32
}

// handleGlobalRequests answers the global request pair remote forwarding
// rides on ("tcpip-forward" / "cancel-tcpip-forward") and discards anything
// else, which is what ssh.DiscardRequests did before this server needed to
// understand -R.
func (s *forwardTestServer) handleGlobalRequests(sc *ssh.ServerConn, reqs <-chan *ssh.Request, remoteFwd *remoteForwardState) {
	for req := range reqs {
		switch req.Type {
		case "tcpip-forward":
			s.handleTCPIPForward(sc, req, remoteFwd)
		case "cancel-tcpip-forward":
			handleCancelTCPIPForward(req, remoteFwd)
		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

// handleTCPIPForward is the server half of `ssh -R`: bind a real listener,
// reply with the bound port, and — this is the part a stub that only replies
// to the request would skip — actually run an accept loop that opens a
// "forwarded-tcpip" channel back to the client for every connection it
// accepts. Without that second half the client's Listen "succeeds" but never
// forwards a single byte, and a test asserting only on the reply would never
// notice.
func (s *forwardTestServer) handleTCPIPForward(sc *ssh.ServerConn, req *ssh.Request, remoteFwd *remoteForwardState) {
	var m channelForwardMsg
	if err := ssh.Unmarshal(req.Payload, &m); err != nil {
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
		return
	}
	if s.shouldRefuseRemoteForward() {
		// A real sshd would give no more detail than this either (`GatewayPorts
		// no` and "needs root for <1024" both just deny the request).
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
		return
	}

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(m.Port))))
	if err != nil {
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
		return
	}
	_, boundPortStr, _ := net.SplitHostPort(ln.Addr().String())
	boundPort, _ := strconv.Atoi(boundPortStr)

	key := m.Addr + ":" + strconv.Itoa(boundPort)
	remoteFwd.add(key, ln)

	if req.WantReply {
		_ = req.Reply(true, ssh.Marshal(&struct{ Port uint32 }{Port: uint32(boundPort)}))
	}

	go acceptRemoteForward(sc, ln, m.Addr, uint32(boundPort))
}

func handleCancelTCPIPForward(req *ssh.Request, remoteFwd *remoteForwardState) {
	var m channelForwardMsg
	if err := ssh.Unmarshal(req.Payload, &m); err != nil {
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
		return
	}
	key := m.Addr + ":" + strconv.Itoa(int(m.Port))
	if ln := remoteFwd.remove(key); ln != nil {
		_ = ln.Close()
		if req.WantReply {
			_ = req.Reply(true, nil)
		}
		return
	}
	if req.WantReply {
		_ = req.Reply(false, nil)
	}
}

// acceptRemoteForward runs the accept loop for one bound "tcpip-forward"
// listener, opening a "forwarded-tcpip" channel back over sc for each
// connection accepted — the half of -R that actually carries traffic.
func acceptRemoteForward(sc *ssh.ServerConn, ln net.Listener, addr string, port uint32) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return // closed by cancel-tcpip-forward or connection teardown
		}
		go forwardOneRemoteConn(sc, c, addr, port)
	}
}

// forwardOneRemoteConn is the server-side splice for one accepted
// -R connection: open the channel the client is waiting on, then copy bytes
// both ways between it and the TCP connection that arrived here — the exact
// mirror of what direct-tcpip does for -L above.
func forwardOneRemoteConn(sc *ssh.ServerConn, c net.Conn, addr string, port uint32) {
	originHost, originPortStr, _ := net.SplitHostPort(c.RemoteAddr().String())
	originPort, _ := strconv.Atoi(originPortStr)

	payload := forwardedTCPPayload{
		Addr: addr, Port: port,
		OriginAddr: originHost, OriginPort: uint32(originPort),
	}
	ch, chReqs, err := sc.OpenChannel("forwarded-tcpip", ssh.Marshal(&payload))
	if err != nil {
		_ = c.Close()
		return
	}
	go ssh.DiscardRequests(chReqs)
	go func() { _, _ = io.Copy(ch, c); _ = ch.Close(); _ = c.Close() }()
	go func() { _, _ = io.Copy(c, ch); _ = ch.Close(); _ = c.Close() }()
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
	// The same wiring NewApp/startup does, so every test in this file runs
	// with the production config observer installed — which is also what keeps
	// the "an ordinary edit does not tear a tunnel down" claim honest: if
	// reconcile were trigger-happy, tests here that touch config would break.
	a.observeConfigStore()
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

// remoteRule is a -R rule with an ephemeral remote bind port and no
// BindAddr, mirroring localRule.
func remoteRule(id, targetHost, targetPort string) ForwardRule {
	return ForwardRule{ID: id, Kind: "remote", BindPort: "0", TargetHost: targetHost, TargetPort: targetPort}
}

// dynamicRule is a -D rule. It carries no target at all, which is the point:
// the SOCKS client names a destination per connection.
func dynamicRule(id string) ForwardRule {
	return ForwardRule{ID: id, Kind: "dynamic", BindPort: "0"}
}

// socksDial goes through a dynamic forward using golang.org/x/net/proxy — a
// SOCKS5 client we did not write. Hand-rolling the client here would prove the
// server agrees with our own encoder and nothing more; this fails if a reply
// is malformed in any way a real client cares about.
func socksDial(t *testing.T, proxyAddr, target string) net.Conn {
	t.Helper()
	d, err := proxy.SOCKS5("tcp", proxyAddr, nil, proxy.Direct)
	if err != nil {
		t.Fatalf("build socks5 client: %v", err)
	}
	c, err := d.Dial("tcp", target)
	if err != nil {
		t.Fatalf("socks5 dial %s via %s: %v", target, proxyAddr, err)
	}
	t.Cleanup(func() { _ = c.Close() })
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	return c
}

func echoThrough(t *testing.T, c net.Conn, msg string) {
	t.Helper()
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

// TestStartForwardRefusesAnUnsavedJumpHost is what is left of the tunnel
// path's item-25 refusal once item 27 wires the chain in: a ProxyJump host is
// no longer refused for having a ProxyJump, but a hop that is not a saved host
// still is (design §5.1 — a bastion needs its own credential, and the target's
// must never be sent to a different machine).
//
// The host has a working credential on purpose, so the error cannot be the
// credential one, and the two counts are the assertion: nothing dialled, no
// local port grabbed.
func TestStartForwardRefusesAnUnsavedJumpHost(t *testing.T) {
	useIsolatedKeyring(t)
	// A *reachable* address, so nothing here is unreachable by accident.
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	sshHost, sshPort, _ := net.SplitHostPort(srv.addr)

	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}
	a.sshKnownHostsPath = writeKnownHostsFor(t, srv.addr, srv.hostPub)
	t.Cleanup(func() { a.tunnels.stopAll() })

	h, err := a.AddSSHHost(SSHHost{
		Alias: "db", Host: sshHost, Port: sshPort, User: "u", AuthKind: "password",
		ProxyJump: "bastion", // not a saved host
		Forwards:  []ForwardRule{localRule("r1", targetHost, targetPort)},
	}, sshCredential{Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}

	err = a.StartForward(h.ID, "r1")
	if err == nil {
		t.Fatal("must refuse to open a tunnel through a jump host that is not saved")
	}
	if !strings.Contains(err.Error(), "bastion") {
		t.Fatalf("error must name the unresolved hop, got %v", err)
	}
	if !strings.Contains(err.Error(), "add") {
		t.Fatalf("error must tell the user to add the hop as a host, got %v", err)
	}
	if got := a.ListActiveForwards(); len(got) != 0 {
		t.Fatalf("a refused host must not open a listener, got %+v", got)
	}
	if opened, _ := srv.counts(); opened != 0 {
		t.Fatalf("an unresolvable chain must not dial, but the SSH server saw %d connections", opened)
	}
}

// TestStartForwardRefusesProxyCommandHost is the regression guard for the arm
// of the gate item 27 does not relax: ProxyCommand is an arbitrary command
// atterm never runs, so the wording must not change.
//
// The host has no credential stored, which is the discriminator that the gate
// still runs *before* the credential read: a gate that ran after it would
// return errCredentialMissing instead of naming ProxyCommand.
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
	if err.Error() == errCredentialMissing {
		t.Fatal("gate must fire before the credential read: got errCredentialMissing, " +
			"meaning StartForward resolved credentials first")
	}
	if got := a.ListActiveForwards(); len(got) != 0 {
		t.Fatalf("a refused host must not open a listener, got %+v", got)
	}
}

// --- jump hosts (roadmap item 27) --------------------------------------------

// TestStartForwardThroughAJumpHost is the tunnel half of item 27, and it has to
// separate two different direct-tcpip channels to mean anything:
//
//   - the bastion is asked to reach *the target's SSH port* — that is the chain,
//     and a build that ignored ProxyJump would leave this empty while still
//     forwarding bytes, because the target is reachable from the test process;
//   - the target is asked to reach *the echo service* — that is the tunnel
//     itself, and it must ride the target's connection, not the bastion's.
func TestStartForwardThroughAJumpHost(t *testing.T) {
	echoHost, echoPort := startEchoTarget(t)
	bastion := startForwardingSSHTestServerAs(t, "bastion-user", "bastion-pw")
	target := startForwardingSSHTestServerAs(t, "target-user", "target-pw")

	a := newJumpTestApp(t, bastion, target)
	a.observeConfigStore()
	t.Cleanup(func() { a.tunnels.stopAll() })

	addServerHost(t, a, "bastion", bastion, "bastion-user", "bastion-pw", "")
	targetHost, targetPort, _ := net.SplitHostPort(target.addr)
	h, err := a.AddSSHHost(SSHHost{
		Alias: "db", Host: targetHost, Port: targetPort, User: "target-user", AuthKind: "password",
		ProxyJump: "bastion",
		Forwards:  []ForwardRule{localRule("r1", echoHost, echoPort)},
	}, sshCredential{Password: "target-pw"})
	if err != nil {
		t.Fatal(err)
	}

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("a ProxyJump host must be able to start a tunnel: %v", err)
	}

	if got := bastion.directDestAddrs(); len(got) != 1 || got[0] != target.addr {
		t.Fatalf("the bastion must be asked to reach the target %s, got %q", target.addr, got)
	}
	if got := bastion.authCredentials(); len(got) != 1 || got[0] != "bastion-user:bastion-pw" {
		t.Fatalf("the bastion must see only its own credential, got %q", got)
	}

	f := activeForward(t, a, "r1")
	c, err := net.DialTimeout("tcp", f.ListenAddr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial forwarded port: %v", err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	echoThrough(t, c, "through a bastion and a tunnel")

	if got := target.directDestAddrs(); len(got) != 1 || got[0] != net.JoinHostPort(echoHost, echoPort) {
		t.Fatalf("the tunnel's own channel must be opened on the target, got %q", got)
	}

	// Stopping the tunnel releases the whole chain: the bastion login exists
	// only to carry this connection, and closing the target does not close it.
	if err := a.StopForward(h.ID, "r1"); err != nil {
		t.Fatalf("StopForward: %v", err)
	}
	waitClosed(t, bastion, 1, "the bastion connection after the tunnel stopped")
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

// --- remote forwarding (-R) --------------------------------------------------

// TestRemoteForwardCarriesBytesBothWays is the core proof for this task: a
// connection that originates on the *remote* side — here, dialing the
// address the remote host is now listening on, exactly what activeForward's
// ListenAddr reports for a remote rule — gets spliced through an SSH
// "forwarded-tcpip" channel to net.Dial'd local target and back. Asserting
// only that ListenRemote returned a listener would prove nothing: this test
// fails unless bytes actually round-trip through a real channel open.
func TestRemoteForwardCarriesBytesBothWays(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv, remoteRule("r1", targetHost, targetPort))

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward: %v", err)
	}
	defer func() { _ = a.StopForward(h.ID, "r1") }()

	f := activeForward(t, a, "r1")
	// ListenAddr is the address the *remote* host bound on our behalf; dialing
	// it here simulates a peer connecting to that listener on the remote side.
	c, err := net.DialTimeout("tcp", f.ListenAddr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial remote-bound port: %v", err)
	}
	defer c.Close()

	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	const msg = "through the reverse tunnel\n"
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

// TestStartRemoteForwardRefusedByRemoteWording pins design §5.5: a refused
// tcpip-forward request must be reported as a *remote* configuration problem
// (GatewayPorts / needing root there), never with local-EADDRINUSE wording —
// a user told "local port X is already in use" when the truth is "the remote
// refused to bind" looks in the wrong place entirely.
func TestStartRemoteForwardRefusedByRemoteWording(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	srv.setRefuseRemoteForward(true)
	a, h := newForwardTestApp(t, srv, remoteRule("r1", targetHost, targetPort))

	err := a.StartForward(h.ID, "r1")
	if err == nil {
		t.Fatal("expected the remote tcpip-forward request to be refused")
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "already in use") || strings.Contains(msg, "local port") {
		t.Fatalf("error must not use the local EADDRINUSE wording, got %v", err)
	}
	if !strings.Contains(msg, "remote") {
		t.Fatalf("error must point at the remote host/sshd, got %v", err)
	}
	if got := a.ListActiveForwards(); len(got) != 0 {
		t.Fatalf("a refused remote listen must leave nothing behind, got %+v", got)
	}
	// The connection this cost must still be released — a refused -R request
	// is not a reason to leak the shared login.
	waitFor(t, 3*time.Second, "ssh connection released after a refused remote listen", func() bool {
		opened, closed := srv.counts()
		return opened == 1 && closed == 1
	})
}

// TestStopForwardCutsEstablishedRemoteConnections is TestStopForwardCutsEstablishedConnections
// for -R: two remote rules share one connection so stopping one cannot rely
// on the whole SSH connection dying to also kill the established forward —
// that would hide a leak in tunnel.live for the remote path specifically.
func TestStopForwardCutsEstablishedRemoteConnections(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv,
		remoteRule("r1", targetHost, targetPort),
		remoteRule("r2", targetHost, targetPort),
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
		t.Fatalf("dial remote-bound port: %v", err)
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

// TestLostConnectionStopsRemoteTunnelWithReason is
// TestLostConnectionStopsIdleTunnel for -R: watchConn/connectionLost are
// shared code, but the tunnel's listener type (a client-side tcpListener
// over the SSH connection, not a plain net.Listener) is not, so this proves
// the shared teardown path actually reaches it.
func TestLostConnectionStopsRemoteTunnelWithReason(t *testing.T) {
	old := jumpKeepalive
	jumpKeepalive = 50 * time.Millisecond
	t.Cleanup(func() { jumpKeepalive = old })

	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv, remoteRule("r1", targetHost, targetPort))

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward: %v", err)
	}

	srv.dropConns()

	waitFor(t, 5*time.Second, "remote rule marked stopped with a reason", func() bool {
		for _, f := range a.ListActiveForwards() {
			if f.RuleID == "r1" {
				return !f.Running && f.Error != ""
			}
		}
		return false
	})
	if opened, _ := srv.counts(); opened != 1 {
		t.Fatalf("a dropped connection must not be redialled automatically, server saw %d connections", opened)
	}
}

// TestLocalAndRemoteRulesShareOneConnection pins design §4.3's refcount for
// mixed kinds: a remote rule is not special-cased out of the accounting that
// makes N rules on one host share one login.
func TestLocalAndRemoteRulesShareOneConnection(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv,
		localRule("r1", targetHost, targetPort),
		remoteRule("r2", targetHost, targetPort),
	)

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward r1: %v", err)
	}
	if err := a.StartForward(h.ID, "r2"); err != nil {
		t.Fatalf("StartForward r2: %v", err)
	}
	if opened, _ := srv.counts(); opened != 1 {
		t.Fatalf("a local + a remote rule on one host opened %d SSH connections, want 1", opened)
	}

	if err := a.StopForward(h.ID, "r1"); err != nil {
		t.Fatalf("StopForward r1: %v", err)
	}
	if _, closed := srv.counts(); closed != 0 {
		t.Fatal("connection closed while the remote rule is still using it")
	}

	if err := a.StopForward(h.ID, "r2"); err != nil {
		t.Fatalf("StopForward r2: %v", err)
	}
	waitFor(t, 3*time.Second, "connection closed after the last rule stopped", func() bool {
		_, closed := srv.counts()
		return closed == 1
	})
}

// --- dynamic forwarding (-D) -------------------------------------------------

// TestDynamicForwardPicksTargetPerConnection is the proof that dynamic
// forwarding is not just local forwarding with a different name: one rule,
// with no target configured on it at all, carries traffic to two different
// destinations chosen by the client at CONNECT time.
func TestDynamicForwardPicksTargetPerConnection(t *testing.T) {
	hostA, portA := startEchoTarget(t)
	hostB, portB := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv, dynamicRule("r1"))

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward: %v", err)
	}
	defer func() { _ = a.StopForward(h.ID, "r1") }()

	f := activeForward(t, a, "r1")
	echoThrough(t, socksDial(t, f.ListenAddr, net.JoinHostPort(hostA, portA)), "to target A")
	echoThrough(t, socksDial(t, f.ListenAddr, net.JoinHostPort(hostB, portB)), "to target B")

	if got := activeForward(t, a, "r1").Conns; got != 2 {
		t.Fatalf("accepted connection count = %d, want 2", got)
	}
}

// TestDynamicForwardCarriesADomainNameTargetUnresolved drives the DOMAINNAME
// address type (ATYP=3) end to end with an independent client, and asserts the
// name reached the *far side* still a name.
//
// Why this test has to exist alongside the others: x/net/proxy picks the
// address type with net.ParseIP, so every other wiring test here — all of which
// hand it an IP literal — exercises only ATYP=1. DOMAINNAME is the one address
// type carrying a length prefix, which makes it both the likeliest place for
// our encoder and a real client's decoder to disagree, and the one most worth
// checking against a decoder we did not write. Promoting x/net to a direct
// dependency buys nothing if the third-party client never drives this path.
//
// Why it asserts on the server and not just on the echo: name *passthrough* is
// the entire reason to run SOCKS over SSH rather than a fixed -L. If the name
// were resolved on this side, the bytes would still echo and this test would
// still pass — resolution simply would have happened on the wrong machine, and
// a name that only exists on the remote network would break in production while
// "localhost" kept working here. The direct-tcpip payload is the only place
// that difference is visible.
func TestDynamicForwardCarriesADomainNameTargetUnresolved(t *testing.T) {
	_, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv, dynamicRule("r1"))

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward: %v", err)
	}
	defer func() { _ = a.StopForward(h.ID, "r1") }()

	f := activeForward(t, a, "r1")
	echoThrough(t, socksDial(t, f.ListenAddr, "localhost:"+targetPort), "by name, not by address")

	dests := srv.directDestHosts()
	if len(dests) != 1 {
		t.Fatalf("ssh server saw %d direct-tcpip opens (%q), want 1", len(dests), dests)
	}
	got := dests[0]
	// The loud half: if a future x/net/proxy resolves hostnames client-side,
	// this test would otherwise quietly degrade into a second ATYP=1 test that
	// still passes. An IP literal here means the CONNECT never carried a name,
	// so nothing above is testing DOMAINNAME any more.
	if net.ParseIP(got) != nil {
		t.Fatalf("the ssh server was asked to dial the IP literal %q: the client resolved the name "+
			"before sending CONNECT, so this test is no longer exercising ATYP=3 (DOMAINNAME) at all "+
			"— and name resolution is happening on the wrong side of the tunnel", got)
	}
	if got != "localhost" {
		t.Fatalf("ssh server was asked to dial %q, want the unresolved name \"localhost\"", got)
	}
}

// TestDynamicForwardListsWithoutATarget: TargetHost/TargetPort are unused for
// this kind, so the panel must not invent one. forwardTarget on an empty rule
// yields ":", which is what a copy-paste from the local path would publish.
func TestDynamicForwardListsWithoutATarget(t *testing.T) {
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv, dynamicRule("r1"))

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward: %v", err)
	}
	defer func() { _ = a.StopForward(h.ID, "r1") }()

	f := activeForward(t, a, "r1")
	if f.Kind != forwardKindDynamic {
		t.Fatalf("kind = %q, want %q", f.Kind, forwardKindDynamic)
	}
	if f.Target != "" {
		t.Fatalf("a dynamic forward has no configured target, but the panel shows %q", f.Target)
	}
	// The SOCKS proxy is unauthenticated, so where it binds is the only thing
	// standing between it and being an open proxy for the whole network.
	bindHost, _, err := net.SplitHostPort(f.ListenAddr)
	if err != nil {
		t.Fatalf("listen addr %q: %v", f.ListenAddr, err)
	}
	if bindHost != "127.0.0.1" {
		t.Fatalf("an unauthenticated socks proxy must default to loopback, listener is on %q", f.ListenAddr)
	}
}

// TestDynamicForwardRuleNeedsNoTarget: validateForwardRule rejects a
// local/remote rule with no target. A dynamic rule legitimately has none, and
// requiring one would make the UI ask for a value it must then ignore.
func TestDynamicForwardRuleNeedsNoTarget(t *testing.T) {
	if err := validateForwardRule(dynamicRule("r1")); err != nil {
		t.Fatalf("a dynamic rule needs no target, got %v", err)
	}
	bad := dynamicRule("r2")
	bad.BindPort = "not-a-port"
	if err := validateForwardRule(bad); err == nil {
		t.Fatal("a dynamic rule still needs a valid bind port")
	}
}

// TestStopForwardCutsEstablishedDynamicConnections is the tunnel.live proof
// for -D. Two dynamic rules share the SSH connection so that stopping one
// cannot rely on the whole connection dying — which would hide a SOCKS session
// that was never registered as live.
func TestStopForwardCutsEstablishedDynamicConnections(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv, dynamicRule("r1"), dynamicRule("r2"))

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward r1: %v", err)
	}
	if err := a.StartForward(h.ID, "r2"); err != nil {
		t.Fatalf("StartForward r2: %v", err)
	}
	defer func() { _ = a.StopForward(h.ID, "r2") }()

	c := socksDial(t, activeForward(t, a, "r1").ListenAddr, net.JoinHostPort(targetHost, targetPort))
	echoThrough(t, c, "x")

	if err := a.StopForward(h.ID, "r1"); err != nil {
		t.Fatalf("StopForward r1: %v", err)
	}
	// A timeout is also a non-nil error, so the timeout case is checked
	// separately: it would mean the proxied session was left dangling.
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	_, err := io.ReadFull(c, make([]byte, 1))
	if err == nil {
		t.Fatal("the established SOCKS session survived StopForward")
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatal("StopForward left the established SOCKS session open: " +
			"the read timed out instead of failing on a closed connection")
	}
}

// TestDynamicForwardSurvivesARefusedTarget: a CONNECT to something that is not
// listening is an ordinary event for a proxy (a browser probing a dead host).
// It must fail that one request and leave the tunnel serving.
func TestDynamicForwardSurvivesARefusedTarget(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	// A port that was bound and then released, so nothing is listening on it.
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := dead.Addr().String()
	_ = dead.Close()

	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv, dynamicRule("r1"))
	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward: %v", err)
	}
	defer func() { _ = a.StopForward(h.ID, "r1") }()

	proxyAddr := activeForward(t, a, "r1").ListenAddr
	d, err := proxy.SOCKS5("tcp", proxyAddr, nil, proxy.Direct)
	if err != nil {
		t.Fatal(err)
	}
	c, dialErr := d.Dial("tcp", deadAddr)
	if dialErr == nil {
		_ = c.Close()
		t.Fatal("CONNECT to a dead port reported success")
	}
	// And it must arrive as a *refusal*, X'05', not as a general server
	// failure. This is the end of the wire the sentinel exists for: the manager
	// saw an *ssh.OpenChannelError (the remote declined this one channel, the
	// tunnel is healthy), wrapped socks5.ErrDestinationRefused around it, and
	// that has to survive all the way to a client we did not write. Telling the
	// user "the proxy could not do it" here would hide the one useful fact —
	// nothing is listening on that port. The inverse mistake, claiming a
	// refusal when the tunnel itself is dead, is pinned in the socks5 package.
	if !strings.Contains(dialErr.Error(), "connection refused") {
		t.Fatalf("a refused target must reach the client as X'05' connection refused, got %v", dialErr)
	}

	// The tunnel is still running and still serving.
	if f := activeForward(t, a, "r1"); !f.Running || f.Error != "" {
		t.Fatalf("one refused target stopped the whole tunnel: %+v", f)
	}
	echoThrough(t, socksDial(t, proxyAddr, net.JoinHostPort(targetHost, targetPort)), "still works")
}

// TestLostConnectionStopsDynamicTunnel is TestLostConnectionStopsIdleTunnel
// for -D: nothing ever connects to the SOCKS port, so only the keepalive
// watcher can notice the drop.
func TestLostConnectionStopsDynamicTunnel(t *testing.T) {
	old := jumpKeepalive
	jumpKeepalive = 50 * time.Millisecond
	t.Cleanup(func() { jumpKeepalive = old })

	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv, dynamicRule("r1"))
	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward: %v", err)
	}
	addr := activeForward(t, a, "r1").ListenAddr

	srv.dropConns()

	waitFor(t, 5*time.Second, "dynamic rule marked stopped with a reason", func() bool {
		for _, f := range a.ListActiveForwards() {
			if f.RuleID == "r1" {
				return !f.Running && f.Error != ""
			}
		}
		return false
	})
	if c, err := net.DialTimeout("tcp", addr, 2*time.Second); err == nil {
		_ = c.Close()
		t.Fatal("socks listener still open after the SSH connection dropped")
	}
	if opened, _ := srv.counts(); opened != 1 {
		t.Fatalf("a dropped connection must not be redialled automatically, server saw %d connections", opened)
	}
}

// TestDynamicSharesOneConnectionWithOtherKinds pins that -D is not
// special-cased out of the refcount that makes N rules on a host one login.
func TestDynamicSharesOneConnectionWithOtherKinds(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv,
		localRule("r1", targetHost, targetPort),
		dynamicRule("r2"),
	)

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward r1: %v", err)
	}
	if err := a.StartForward(h.ID, "r2"); err != nil {
		t.Fatalf("StartForward r2: %v", err)
	}
	if opened, _ := srv.counts(); opened != 1 {
		t.Fatalf("a local + a dynamic rule on one host opened %d SSH connections, want 1", opened)
	}

	if err := a.StopForward(h.ID, "r1"); err != nil {
		t.Fatalf("StopForward r1: %v", err)
	}
	if _, closed := srv.counts(); closed != 0 {
		t.Fatal("connection closed while the dynamic rule is still using it")
	}
	// And the dynamic rule still works on the surviving connection.
	echoThrough(t, socksDial(t, activeForward(t, a, "r2").ListenAddr,
		net.JoinHostPort(targetHost, targetPort)), "shared")

	if err := a.StopForward(h.ID, "r2"); err != nil {
		t.Fatalf("StopForward r2: %v", err)
	}
	waitFor(t, 3*time.Second, "connection closed after the last rule stopped", func() bool {
		_, closed := srv.counts()
		return closed == 1
	})
}

// TestStartDynamicForwardPortInUse: the bind-then-dial ordering -D inherits
// from -L means a port conflict must still be free — reported without an SSH
// login having been paid for.
func TestStartDynamicForwardPortInUse(t *testing.T) {
	srv := startForwardingSSHTestServer(t)
	squatter, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer squatter.Close()
	_, busyPort, _ := net.SplitHostPort(squatter.Addr().String())

	rule := dynamicRule("r1")
	rule.BindPort = busyPort
	a, h := newForwardTestApp(t, srv, rule)

	err = a.StartForward(h.ID, "r1")
	if err == nil {
		t.Fatal("expected a bind failure")
	}
	if !strings.Contains(err.Error(), "already in use") || !strings.Contains(err.Error(), busyPort) {
		t.Fatalf("error must say the local port is already in use and name it, got %v", err)
	}
	if opened, _ := srv.counts(); opened != 0 {
		t.Fatalf("a port conflict must not cost an SSH login, server saw %d connections", opened)
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
	}, ssh.FixedHostKey(srv.hostPub), nil)
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
	old := jumpKeepalive
	jumpKeepalive = 50 * time.Millisecond
	t.Cleanup(func() { jumpKeepalive = old })

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
	old := jumpKeepalive
	jumpKeepalive = 50 * time.Millisecond
	t.Cleanup(func() { jumpKeepalive = old })

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
	dial := func() (*jumpChain, error) {
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
