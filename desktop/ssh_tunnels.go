package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/attson/atterm/internal/socks5"
	"github.com/attson/atterm/internal/sshclient"
	"golang.org/x/crypto/ssh"
)

// Port forwarding (roadmap item 26).
//
// A tunnel is deliberately *not* a relay session. It runs on its own
// shell-less sshclient.Conn, never calls AdoptSession, never lands in
// relayHost.sessions and never touches a session's subscriber lifecycle.
// That is redline #2 held by construction rather than by discipline: if a
// tunnel reused a terminal session's connection and counted as a subscriber,
// an open tunnel would keep PTY bytes uploading forever with nobody watching.

// Forward kinds.
const (
	forwardKindLocal   = "local"
	forwardKindRemote  = "remote"
	forwardKindDynamic = "dynamic"
)

// defaultForwardBindAddr is what an empty ForwardRule.BindAddr means, and it
// is loopback for a reason that outranks convenience: binding 0.0.0.0 puts the
// forwarded service in front of everyone on the same network **with no SSH
// credential at all** — and the rule syncs to every device, where it would
// bind the same way.
//
// For "dynamic" the stake is a different size, not a different colour. A
// non-loopback "local" rule exposes one service on one port. A non-loopback
// "dynamic" rule is an unauthenticated SOCKS5 proxy (see internal/socks5): it
// exposes *everything the SSH host can reach*, to any destination the caller
// names, using our credential and appearing in the far side's logs as us.
// That is the difference between leaking a database and becoming an open relay
// into the remote network.
//
// This constant is what makes the default safe when no UI is involved — a
// rule that arrives by sync, or one saved with the field left empty. The
// drawer's own warning (forwardBindWarning in SshHostsPanel.vue) covers the
// case where a user types a non-loopback address; the two have to keep saying
// the same thing about what "empty" means, which is why isLoopbackBind treats
// "" as loopback exactly as this does.
const defaultForwardBindAddr = "127.0.0.1"

// tunnelKeepalive is how often a tunnel connection pings the remote. It also
// bounds how long a dropped connection can go unnoticed while every tunnel on
// it is idle, because the failed ping is what closes sshclient.Conn.Done.
// Tests shorten it so a drop is observable in a test's lifetime.
var tunnelKeepalive = 30 * time.Second

// ForwardRule is one port-forwarding rule saved on an SSHHost. It is
// configuration, not state: nothing starts it except an explicit StartForward.
type ForwardRule struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`                // "local" | "remote" | "dynamic"
	BindAddr string `json:"bind_addr,omitempty"` // empty → 127.0.0.1
	BindPort string `json:"bind_port"`
	// TargetHost/TargetPort are the far side of a local/remote forward,
	// resolved on the remote host for "local". Unused by "dynamic".
	TargetHost string `json:"target_host,omitempty"`
	TargetPort string `json:"target_port,omitempty"`
	Note       string `json:"note,omitempty"`
}

// ActiveForward is the frontend-facing view of one tunnel.
//
// It also carries tunnels that have *stopped on their own* (Running false,
// Error set) — a lost SSH connection has to stay visible somewhere, and the
// list the user is looking at is that somewhere. Such an entry disappears when
// the user stops (dismisses) or restarts the rule.
type ActiveForward struct {
	HostID string `json:"host_id"`
	RuleID string `json:"rule_id"`
	Kind   string `json:"kind"`
	// ListenAddr is the listener's *real* address, so an ephemeral or
	// defaulted bind is reported as what it actually resolved to.
	ListenAddr string `json:"listen_addr"`
	Target     string `json:"target,omitempty"`
	Conns      int64  `json:"conns"`
	StartedAt  int64  `json:"started_at"`
	Running    bool   `json:"running"`
	Error      string `json:"error,omitempty"`
}

// StartForward brings up the given rule of the given host. Tunnels are only
// ever started explicitly: a tunnel occupies a local port, so auto-starting
// one on connect would let opening a terminal grab 5432 while the user was not
// thinking about tunnels at all.
func (a *App) StartForward(hostID, ruleID string) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	host, rule, err := a.findForwardRule(hostID, ruleID)
	if err != nil {
		return err
	}

	// The jump-host gate, shared with NewSshSessionByID. It runs before the
	// credential read and before any dial: for a host we refuse, nothing at
	// all happens.
	if needsJump, reason := hostNeedsJump(host); needsJump {
		return errors.New(reason)
	}

	if err := validateForwardRule(rule); err != nil {
		return err
	}
	switch rule.Kind {
	case forwardKindLocal:
		return a.tunnels.startLocal(host, rule, func() (*sshclient.Conn, error) {
			return a.dialTunnelConn(host)
		})
	case forwardKindRemote:
		return a.tunnels.startRemote(host, rule, func() (*sshclient.Conn, error) {
			return a.dialTunnelConn(host)
		})
	case forwardKindDynamic:
		return a.tunnels.startDynamic(host, rule, func() (*sshclient.Conn, error) {
			return a.dialTunnelConn(host)
		})
	default:
		return fmt.Errorf("unknown forward kind %q", rule.Kind)
	}
}

// StopForward closes a running tunnel's listener and releases its share of the
// host's SSH connection. It also clears an entry that already stopped by
// itself (see ActiveForward).
func (a *App) StopForward(hostID, ruleID string) error {
	return a.tunnels.stop(hostID, ruleID)
}

// ListActiveForwards returns every tunnel this app is holding, running or
// stopped-with-an-error, sorted for a stable UI. Never nil: Wails marshals a
// nil slice to JSON null and the frontend's `.length` then throws.
func (a *App) ListActiveForwards() []ActiveForward {
	return a.tunnels.list()
}

// findForwardRule resolves a (hostID, ruleID) pair against the saved hosts.
func (a *App) findForwardRule(hostID, ruleID string) (SSHHost, ForwardRule, error) {
	for _, h := range a.ListSSHHosts() {
		if h.ID != hostID {
			continue
		}
		for _, r := range h.Forwards {
			if r.ID == ruleID {
				return h, r, nil
			}
		}
		return SSHHost{}, ForwardRule{}, fmt.Errorf("no such forward rule on host %s: %s", hostID, ruleID)
	}
	return SSHHost{}, ForwardRule{}, fmt.Errorf("no such host: %s", hostID)
}

func validateForwardRule(r ForwardRule) error {
	if _, err := parsePort(r.BindPort); err != nil {
		return fmt.Errorf("invalid bind port %q", r.BindPort)
	}
	if r.Kind == forwardKindLocal || r.Kind == forwardKindRemote {
		if strings.TrimSpace(r.TargetHost) == "" {
			return fmt.Errorf("forward rule %s has no target host", r.ID)
		}
		if _, err := parsePort(r.TargetPort); err != nil {
			return fmt.Errorf("invalid target port %q", r.TargetPort)
		}
	}
	return nil
}

func parsePort(p string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(p))
	if err != nil || n < 0 || n > 65535 {
		return 0, fmt.Errorf("bad port %q", p)
	}
	return n, nil
}

// forwardBindAddr is the address a rule's listener binds. See
// defaultForwardBindAddr for why the empty case is loopback.
func forwardBindAddr(r ForwardRule) string {
	addr := strings.TrimSpace(r.BindAddr)
	if addr == "" {
		addr = defaultForwardBindAddr
	}
	return net.JoinHostPort(addr, strings.TrimSpace(r.BindPort))
}

func forwardTarget(r ForwardRule) string {
	return net.JoinHostPort(strings.TrimSpace(r.TargetHost), strings.TrimSpace(r.TargetPort))
}

// sshAuthForHost resolves a saved host's credential into an auth method,
// returning the same errCredentialMissing / errKeyMissing sentinels the
// terminal path uses so the frontend can react identically.
func sshAuthForHost(h SSHHost) (sshclient.AuthMethod, error) {
	switch h.AuthKind {
	case "key":
		sec, err := sshKeySecretSlot(h.KeyID).Load()
		if err != nil || sec.PrivateKey == "" {
			return nil, errors.New(errKeyMissing)
		}
		return sshclient.PrivateKeyAuth{PEM: []byte(sec.PrivateKey), Passphrase: sec.Passphrase}, nil
	default: // "password"
		cred, err := sshCredentialSlot(h.ID).Load()
		if err != nil || cred == (sshCredential{}) {
			return nil, errors.New(errCredentialMissing)
		}
		return sshclient.PasswordAuth{Password: cred.Password}, nil
	}
}

// dialTunnelConn opens the shell-less SSH connection a host's tunnels ride on.
//
// Unlike the terminal path there is no TOFU dialog behind this call, so an
// unknown host key is refused outright with a message telling the user where
// the fingerprint can be accepted. Silently trusting it would mean a
// background action pinning a key the user never saw.
func (a *App) dialTunnelConn(h SSHHost) (*sshclient.Conn, error) {
	auth, err := sshAuthForHost(h)
	if err != nil {
		return nil, err
	}
	var unknownFP string
	cb := sshclient.KnownHostsCallback(a.knownHostsPath(), func(_, fp string) bool {
		unknownFP = fp
		return false
	})
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := sshclient.DialConn(ctx, sshclient.Config{
		Host: h.Host, Port: h.Port, User: h.User,
		Auth:      auth,
		HostKeyCb: cb,
		Timeout:   15 * time.Second,
		Keepalive: tunnelKeepalive,
	})
	if err != nil {
		if unknownFP != "" {
			return nil, fmt.Errorf(
				"host key for %s is not in known_hosts (fingerprint %s); open a terminal to this host once and accept the fingerprint, then start the tunnel",
				h.Host, unknownFP)
		}
		return nil, err
	}
	return conn, nil
}

// --- tunnel manager ---------------------------------------------------------

// tunnelManager owns every running tunnel. Its zero value is usable, so App
// can embed it as a plain field.
type tunnelManager struct {
	mu       sync.Mutex
	conns    map[string]*hostConn // hostID → shared SSH connection
	tuns     map[string]*tunnel   // hostID + "/" + ruleID
	starting map[string]bool      // keys with a start in flight
	// rules is the last reconciled view of the saved rules: key →
	// fingerprint. nil means reconcile has never run (nothing has written
	// config yet), which registerTunnel reads as "no opinion" rather than as
	// "no rules exist" — the difference between allowing a start and refusing
	// every start on a machine that has not touched its config this run.
	// ensureLocked deliberately does not initialise it.
	rules map[string]string
}

func tunnelKey(hostID, ruleID string) string { return hostID + "/" + ruleID }

// hostConn is one host's SSH connection, shared by all of that host's running
// rules. Per-rule connections would show N logins on the remote and multiply
// keepalives, so rules refcount a single one instead.
//
// ready is closed once dial completes, so a second rule starting while the
// first is still dialing waits for the same connection instead of opening
// another. refs is guarded by tunnelManager.mu; conn/err are written before
// ready closes and only read after.
type hostConn struct {
	ready chan struct{}
	conn  *sshclient.Conn
	err   error
	refs  int
}

// tunnel is one running rule.
type tunnel struct {
	hostID, ruleID string
	kind           string
	listener       net.Listener
	listenAddr     string
	target         string
	startedAt      int64
	hc             *hostConn
	accepted       atomic.Int64
	// fingerprint is forwardRuleFingerprint of the rule as it stood when this
	// tunnel started, so reconcile can tell "the rule still describes this
	// listener" from "the rule now describes a different one".
	fingerprint string

	// closeOnce makes teardown idempotent, which is what keeps the hostConn
	// refcount honest: whether a tunnel is stopped by the user, by a lost
	// connection or by app shutdown, exactly one release happens.
	closeOnce sync.Once

	// live tracks the accepted connections currently being forwarded, so
	// stopping a rule actually cuts them. Closing the listener alone would
	// leave an established psql session running until it ended by itself —
	// and when another rule keeps the shared SSH connection alive, that could
	// be hours after the user pressed stop.
	liveMu sync.Mutex
	live   map[net.Conn]struct{}

	// running/errMsg are guarded by tunnelManager.mu.
	running bool
	errMsg  string
}

// trackConn registers an accepted connection, reporting false when the tunnel
// is already tearing down (so the caller closes it instead of forwarding it).
func (t *tunnel) trackConn(c net.Conn) bool {
	t.liveMu.Lock()
	defer t.liveMu.Unlock()
	if t.live == nil {
		return false // teardown already ran
	}
	t.live[c] = struct{}{}
	return true
}

func (t *tunnel) untrackConn(c net.Conn) {
	t.liveMu.Lock()
	if t.live != nil {
		delete(t.live, c)
	}
	t.liveMu.Unlock()
}

// closeLive closes every forwarded connection and refuses further ones.
func (t *tunnel) closeLive() {
	t.liveMu.Lock()
	live := t.live
	t.live = nil
	t.liveMu.Unlock()
	for c := range live {
		_ = c.Close()
	}
}

func (m *tunnelManager) ensureLocked() {
	if m.conns == nil {
		m.conns = map[string]*hostConn{}
	}
	if m.tuns == nil {
		m.tuns = map[string]*tunnel{}
	}
	if m.starting == nil {
		m.starting = map[string]bool{}
	}
}

// claimStart reserves a rule's start slot and returns the release to defer.
//
// Every start kind needs this identically: refuse a concurrent start of the
// same rule, refuse a rule that is already running, and clear the flag however
// the start ends. The three kinds differ in what they do *after* this, never
// in this.
func (m *tunnelManager) claimStart(key string, r ForwardRule) (release func(), err error) {
	m.mu.Lock()
	m.ensureLocked()
	if m.starting[key] {
		m.mu.Unlock()
		return nil, fmt.Errorf("forward %s is already starting", r.ID)
	}
	if t, ok := m.tuns[key]; ok && t.running {
		m.mu.Unlock()
		return nil, fmt.Errorf("forward %s is already running on %s", r.ID, t.listenAddr)
	}
	m.starting[key] = true
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		delete(m.starting, key)
		m.mu.Unlock()
	}, nil
}

// checkConnAlive fails a start whose connection died between the dial and the
// registration, releasing the reference it was handed.
//
// The watcher for a connection only sees tunnels that are already registered,
// so a tunnel registered on an already-dead connection is one nobody ever
// marks stopped — exactly the "reports running forever" state the watcher
// exists to prevent. Failing the start instead costs the user one retry, which
// gets a fresh dial.
func (m *tunnelManager) checkConnAlive(h SSHHost, hc *hostConn) error {
	select {
	case <-hc.conn.Done():
		m.releaseConn(h.ID, hc)
		return fmt.Errorf("the ssh connection to %s dropped while the tunnel was starting", h.Host)
	default:
		return nil
	}
}

// registerTunnel builds the tunnel record and publishes it under its key.
//
// target is passed rather than derived because it is the one field the kinds
// disagree on: a dynamic forward has no configured target at all (the SOCKS
// client names one per connection), and deriving it from the rule would
// publish a meaningless ":" to the UI.
//
// It refuses to publish a tunnel whose rule disappeared (or changed) while the
// start was in flight. That window is not small: the start dials with a 15s
// timeout, and a rule deleted in the middle of it would otherwise produce
// exactly the orphan reconcile exists to prevent — a listener the UI cannot
// show, and so cannot stop.
func (m *tunnelManager) registerTunnel(h SSHHost, r ForwardRule, ln net.Listener, hc *hostConn, target string) (*tunnel, error) {
	t := &tunnel{
		hostID: h.ID, ruleID: r.ID, kind: r.Kind,
		listener: ln, listenAddr: ln.Addr().String(),
		target: target, startedAt: time.Now().Unix(),
		hc: hc, running: true, fingerprint: forwardRuleFingerprint(r),
		live: map[net.Conn]struct{}{},
	}
	m.mu.Lock()
	m.ensureLocked()
	if m.rules != nil {
		if fp, ok := m.rules[tunnelKey(h.ID, r.ID)]; !ok || fp != t.fingerprint {
			m.mu.Unlock()
			return nil, fmt.Errorf(
				"forward rule %s was deleted or changed while it was starting; nothing is listening", r.ID)
		}
	}
	// A previous entry here can only be a stopped one (claimStart's running
	// check plus the starting flag rule that out); starting the rule again
	// replaces it and clears its error.
	m.tuns[tunnelKey(h.ID, r.ID)] = t
	m.mu.Unlock()
	return t, nil
}

// forwardRuleFingerprint is everything about a rule that decides what its
// listener actually does: the kind, the address and port it binds, and (for
// -L/-R, which have one) the target it forwards to. The bind address goes
// through forwardBindAddr so it is the *resolved* one — typing "127.0.0.1"
// into a field that was empty describes the same listener and must not count
// as a change.
//
// Note is excluded on purpose: relabelling a rule is not a reason to cut a
// live database session.
func forwardRuleFingerprint(r ForwardRule) string {
	kind := strings.TrimSpace(r.Kind)
	parts := []string{kind, forwardBindAddr(r)}
	// A dynamic rule has no configured target — the SOCKS client names one per
	// connection — so leftover text in those fields says nothing about the
	// running listener and must not tear it down.
	if kind == forwardKindLocal || kind == forwardKindRemote {
		parts = append(parts, forwardTarget(r))
	}
	return strings.Join(parts, "\x00")
}

// reconcile joins the rule lifecycle to the tunnel lifecycle: it stops every
// tunnel whose rule is gone from the saved hosts, or whose rule now describes
// a different listener.
//
// Without it, deleting a rule (or the whole host) left the tunnel running with
// no way to stop it, because the tunnels tab renders from the *saved* rules —
// so the orphan had no row, the local port stayed bound, and an authenticated
// SSH connection outlived the credential the host delete had just wiped from
// the keyring. The 0.0.0.0 case is why this is a safety fix rather than
// tidiness: a user who binds wide, thinks better of it and deletes the rule
// has not closed the exposure until this runs.
//
// It is driven from configStore's post-commit observer, so it covers every
// path that can remove a rule — including an inbound sync, which rewrites the
// host list in the config store directly and never calls UpdateSSHHost.
func (m *tunnelManager) reconcile(hosts []SSHHost) {
	want := make(map[string]string)
	for _, h := range hosts {
		for _, r := range h.Forwards {
			want[tunnelKey(h.ID, r.ID)] = forwardRuleFingerprint(r)
		}
	}

	m.mu.Lock()
	m.ensureLocked()
	m.rules = want
	var stale []*tunnel
	for key, t := range m.tuns {
		if fp, ok := want[key]; ok && fp == t.fingerprint {
			continue
		}
		// Entries that already stopped on their own go too: their row is
		// rendered from a rule that no longer exists, so nothing would ever
		// show — or dismiss — them again.
		t.running = false
		delete(m.tuns, key)
		stale = append(stale, t)
	}
	m.mu.Unlock()

	for _, t := range stale {
		m.teardown(t) // takes m.mu via releaseConn — must be outside the lock
		logInfo("ssh", "forward %s stopped: its rule was deleted or changed (was listening on %s)",
			t.ruleID, t.listenAddr)
	}
}

// startLocal brings up a -L tunnel: listen locally, and hand every accepted
// connection to the remote host via a direct-tcpip channel.
func (m *tunnelManager) startLocal(h SSHHost, r ForwardRule, dial func() (*sshclient.Conn, error)) error {
	release, err := m.claimStart(tunnelKey(h.ID, r.ID), r)
	if err != nil {
		return err
	}
	defer release()

	// Bind first: a port conflict is the common failure and it should not
	// cost a remote login to discover.
	bind := forwardBindAddr(r)
	ln, err := net.Listen("tcp", bind)
	if err != nil {
		return listenError(r, bind, err)
	}

	hc, err := m.acquireConn(h.ID, dial)
	if err != nil {
		_ = ln.Close()
		return err
	}
	if err := m.checkConnAlive(h, hc); err != nil {
		_ = ln.Close()
		return err
	}

	t, err := m.registerTunnel(h, r, ln, hc, forwardTarget(r))
	if err != nil {
		_ = ln.Close()
		m.releaseConn(h.ID, hc)
		return err
	}
	go m.acceptLoop(t, m.serveLocalConn)
	return nil
}

// startDynamic brings up a -D tunnel: listen locally speaking SOCKS5, and open
// a direct-tcpip channel per CONNECT to whatever destination the SOCKS client
// names. Its ordering follows startLocal's (bind first, then dial) for the
// same reason — a local port conflict should not cost a remote login.
//
// The rule's TargetHost/TargetPort are unused here: there is no configured
// destination, which is the entire point of dynamic forwarding.
func (m *tunnelManager) startDynamic(h SSHHost, r ForwardRule, dial func() (*sshclient.Conn, error)) error {
	release, err := m.claimStart(tunnelKey(h.ID, r.ID), r)
	if err != nil {
		return err
	}
	defer release()

	bind := forwardBindAddr(r)
	ln, err := net.Listen("tcp", bind)
	if err != nil {
		return listenError(r, bind, err)
	}

	hc, err := m.acquireConn(h.ID, dial)
	if err != nil {
		_ = ln.Close()
		return err
	}
	if err := m.checkConnAlive(h, hc); err != nil {
		_ = ln.Close()
		return err
	}

	t, err := m.registerTunnel(h, r, ln, hc, "")
	if err != nil {
		_ = ln.Close()
		m.releaseConn(h.ID, hc)
		return err
	}
	// The accept loop is ours rather than socks5.Serve's, so every accepted
	// connection lands in tunnel.live and a stop actually cuts sessions that
	// are already proxying. socks5.Serve would accept them where the manager
	// cannot see them.
	go m.acceptLoop(t, m.serveDynamicConn)
	return nil
}

// listenError turns a bind failure into something worth reading. Go's raw
// "listen tcp 127.0.0.1:8080: bind: address already in use" buries the only
// fact that matters.
func listenError(r ForwardRule, bind string, err error) error {
	if errors.Is(err, syscall.EADDRINUSE) || strings.Contains(err.Error(), "address already in use") ||
		strings.Contains(err.Error(), "Only one usage of each socket address") {
		return fmt.Errorf("local port %s is already in use (cannot bind %s)", strings.TrimSpace(r.BindPort), bind)
	}
	return fmt.Errorf("cannot listen on %s: %w", bind, err)
}

// startRemote brings up a -R tunnel: ask the remote host to listen, and hand
// every connection *it* accepts to a local target via net.Dial.
//
// The order is the mirror of startLocal's on purpose. startLocal binds
// locally before touching the SSH connection, because a local port conflict
// is common and free to detect before paying for a login. Here there is no
// such cheap local step: the "listen" *is* the SSH round trip
// (ListenRemote sends the tcpip-forward global request), so acquiring the
// connection has to come first.
func (m *tunnelManager) startRemote(h SSHHost, r ForwardRule, dial func() (*sshclient.Conn, error)) error {
	release, err := m.claimStart(tunnelKey(h.ID, r.ID), r)
	if err != nil {
		return err
	}
	defer release()

	hc, err := m.acquireConn(h.ID, dial)
	if err != nil {
		return err
	}
	if err := m.checkConnAlive(h, hc); err != nil {
		return err
	}

	bind := forwardBindAddr(r)
	ln, err := hc.conn.ListenRemote("tcp", bind)
	if err != nil {
		m.releaseConn(h.ID, hc)
		return remoteListenError(r, bind, err)
	}

	t, err := m.registerTunnel(h, r, ln, hc, forwardTarget(r))
	if err != nil {
		_ = ln.Close()
		m.releaseConn(h.ID, hc)
		return err
	}
	go m.acceptLoop(t, m.serveRemoteConn)
	return nil
}

// remoteListenError turns a failed tcpip-forward request into something
// worth reading. x/crypto/ssh's raw error here ("ssh: tcpip-forward request
// denied by peer") is accurate but points nowhere useful, and — this is the
// part that must not be gotten wrong — it must never be phrased like
// listenError's local EADDRINUSE wording. A local bind failure is ours to
// fix; this is the *remote* sshd refusing, almost always because
// GatewayPorts is "no" (the default, which restricts non-loopback binds) or
// because the requested port needs root on that host. Telling a user "local
// port 80 is already in use" when the truth is "the remote refused to bind
// port 80" sends them to check the wrong machine.
func remoteListenError(r ForwardRule, bind string, err error) error {
	return fmt.Errorf(
		"the remote host refused to listen on %s (check the remote sshd's GatewayPorts setting, "+
			"and whether binding port %s needs root there — nothing on this machine is holding that port): %w",
		bind, strings.TrimSpace(r.BindPort), err)
}

// serveRemoteConn splices one connection the remote host accepted on our
// behalf to the local target — the mirror of serveLocalConn. The failure
// case is simpler than serveLocalConn's: dialing the target is a plain local
// net.Dial, so any error just means nothing is listening on the target
// locally. Unlike a remote-side dial failure over an SSH channel, a local
// net.Dial error can never mean the SSH transport itself is gone, so there is
// no *ssh.OpenChannelError split and no call into connectionLost here.
func (m *tunnelManager) serveRemoteConn(t *tunnel, remote net.Conn) {
	defer t.untrackConn(remote)

	local, err := net.DialTimeout("tcp", t.target, 10*time.Second)
	if err != nil {
		_ = remote.Close()
		logWarn("ssh", "forward %s: local target %s refused: %v", t.ruleID, t.target, err)
		return
	}
	go func() {
		_, _ = io.Copy(local, remote)
		_ = local.Close()
		_ = remote.Close()
	}()
	_, _ = io.Copy(remote, local)
	_ = remote.Close()
	_ = local.Close()
}

// acquireConn returns the host's shared connection, dialing it if this is the
// first rule to need it. The caller owns exactly one reference on success.
func (m *tunnelManager) acquireConn(hostID string, dial func() (*sshclient.Conn, error)) (*hostConn, error) {
	m.mu.Lock()
	m.ensureLocked()
	if hc, ok := m.conns[hostID]; ok {
		hc.refs++
		m.mu.Unlock()
		<-hc.ready // a concurrent first starter may still be dialing
		if hc.err != nil {
			m.releaseConn(hostID, hc)
			return nil, hc.err
		}
		return hc, nil
	}
	hc := &hostConn{ready: make(chan struct{}), refs: 1}
	m.conns[hostID] = hc
	m.mu.Unlock()

	hc.conn, hc.err = dial()
	if hc.err != nil {
		// Drop the poisoned entry before waking anyone. releaseConn only
		// removes it once refs reach zero, so while other waiters still hold
		// refs it would linger in m.conns — and a StartForward arriving in
		// that window would be handed the previous attempt's error instead of
		// redialing.
		m.mu.Lock()
		if m.conns[hostID] == hc {
			delete(m.conns, hostID)
		}
		m.mu.Unlock()
	}
	close(hc.ready)
	if hc.err != nil {
		m.releaseConn(hostID, hc)
		return nil, hc.err
	}

	// Watch for the connection dying on its own. sshclient's keepalive loop
	// notices a dead transport and closes Done; without this the drop is only
	// discovered on the next forwarded connection, so an *idle* tunnel would
	// keep reporting itself as running — and startLocal would then answer
	// "already running" to the user trying to restart it after a network
	// change, blocking the obvious recovery.
	go m.watchConn(hostID, hc)
	return hc, nil
}

// watchConn turns a dropped connection into stopped tunnels. Done also fires
// on a deliberate Close, which is harmless: stop/stopAll/connectionLost all
// set running=false under m.mu *before* teardown releases the connection, so
// a watcher waking up afterwards finds nothing left to tear down.
func (m *tunnelManager) watchConn(hostID string, hc *hostConn) {
	<-hc.conn.Done()
	m.connectionLost(hostID, hc, "ssh connection lost (keepalive failed or the peer closed it)")
}

// releaseConn drops one reference and closes the connection when the last
// rule using it goes away. The decrement and the map removal happen in one
// critical section, so an acquireConn racing with the last release either
// sees the entry (and cannot have hit zero) or misses it and dials fresh.
func (m *tunnelManager) releaseConn(hostID string, hc *hostConn) {
	m.mu.Lock()
	m.ensureLocked()
	hc.refs--
	last := hc.refs <= 0
	if last && m.conns[hostID] == hc {
		delete(m.conns, hostID)
	}
	m.mu.Unlock()
	if last && hc.conn != nil {
		_ = hc.conn.Close()
	}
}

// acceptLoop accepts on a tunnel's listener and hands each connection to
// serve. Every kind shares it, including -R, whose listener is a client-side
// listener over the SSH connection rather than a local socket: the accept side
// is identical, only what happens to an accepted connection differs.
//
// Tracking happens here rather than in the serve functions so that no kind can
// forget it — an untracked connection survives a stop, which is the bug
// tunnel.live exists to prevent.
func (m *tunnelManager) acceptLoop(t *tunnel, serve func(*tunnel, net.Conn)) {
	for {
		c, err := t.listener.Accept()
		if err != nil {
			m.acceptFailed(t, err)
			return
		}
		if !t.trackConn(c) {
			_ = c.Close() // raced with teardown
			continue
		}
		t.accepted.Add(1)
		go serve(t, c)
	}
}

// acceptFailed ends a tunnel's accept loop.
//
// Almost always the error means the listener was closed by stop/teardown, and
// then there is nothing to do: the tunnel is already unregistered or already
// marked not-running. The case that matters is the other one — an Accept that
// fails while the tunnel is still registered and running (EMFILE under fd
// exhaustion is the plausible one). Returning quietly there left the panel
// reporting Running for a tunnel that would never accept another connection,
// with no error and no way to notice. So it goes through the same bookkeeping
// connectionLost uses: mark it stopped with a reason, keep the entry so the
// reason is visible, and tear the tunnel down.
func (m *tunnelManager) acceptFailed(t *tunnel, err error) {
	m.mu.Lock()
	m.ensureLocked()
	live := m.tuns[tunnelKey(t.hostID, t.ruleID)] == t && t.running
	if live {
		t.running = false
		t.errMsg = fmt.Sprintf("stopped accepting connections: %v", err)
	}
	m.mu.Unlock()
	if !live {
		return
	}
	logWarn("ssh", "forward %s: accept failed on %s: %v; tunnel stopped", t.ruleID, t.listenAddr, err)
	m.teardown(t)
}

// serveLocalConn splices one accepted local connection to the remote target
// over a direct-tcpip channel.
//
// The DialRemote here has no deadline of its own. x/crypto/ssh's Dial takes no
// context, and the obvious wrapper — race it against a timer — would be worse
// than it looks: a timeout error is not an *ssh.OpenChannelError, so
// noteDialFailure would classify a merely slow remote as a dead transport and
// stop every tunnel on the connection. What actually bounds it is the
// keepalive: a transport that has died takes the pending channel open down
// with it within tunnelKeepalive. socks5.ServeConn documents the same fact
// from the other side.
func (m *tunnelManager) serveLocalConn(t *tunnel, local net.Conn) {
	defer t.untrackConn(local)

	remote, err := t.hc.conn.DialRemote("tcp", t.target)
	if err != nil {
		_ = local.Close()
		m.noteDialFailure(t, t.target, err)
		return
	}
	go func() {
		_, _ = io.Copy(remote, local)
		_ = remote.Close()
		_ = local.Close()
	}()
	_, _ = io.Copy(local, remote)
	_ = local.Close()
	_ = remote.Close()
}

// noteDialFailure decides whether a failed direct-tcpip open means "that one
// target refused" or "the SSH connection is gone", and reacts accordingly.
//
// The distinction is the whole point: a refused channel is an ordinary event
// (nothing listening on the target) and must leave the tunnel running, while
// any other failure means the transport is dead and every tunnel on it has to
// be marked stopped. Both -L and -D reach this, from the same DialRemote call,
// so the classification lives in one place.
//
// It reports that classification back rather than only acting on it, because
// -D has a second consumer for it: the SOCKS client is owed a reply code that
// says which of the two happened, and socks5 cannot work it out for itself
// without importing this dependency. Returning it here keeps the type switch
// on *ssh.OpenChannelError in exactly one place.
func (m *tunnelManager) noteDialFailure(t *tunnel, target string, err error) (destinationRefused bool) {
	var openErr *ssh.OpenChannelError
	if errors.As(err, &openErr) {
		logWarn("ssh", "forward %s: remote refused %s: %v", t.ruleID, target, err)
		return true
	}
	// The watcher usually gets there first; whichever arrives first wins and
	// the other finds nothing running.
	m.connectionLost(t.hostID, t.hc, fmt.Sprintf("ssh connection lost: %v", err))
	return false
}

// serveDynamicConn runs one SOCKS5 session on an accepted connection, opening
// a direct-tcpip channel to whatever destination the client asks for.
//
// The destination is resolved on the *remote* side: socks5 hands the name
// through unresolved and DialRemote makes the remote host look it up, which is
// what makes a SOCKS proxy over SSH useful for names that only exist there.
//
// socks5.ServeConn puts a deadline on the handshake but none on the injected
// dialer, and this dialer adds none either — for the reason spelled out on
// serveLocalConn: the only thing that can bound an SSH channel open here
// without lying about what failed is the keepalive.
func (m *tunnelManager) serveDynamicConn(t *tunnel, local net.Conn) {
	defer t.untrackConn(local)
	defer func() { _ = local.Close() }()

	err := socks5.ServeConn(local, func(network, addr string) (net.Conn, error) {
		remote, err := t.hc.conn.DialRemote(network, addr)
		if err != nil {
			// Report it exactly as -L does — including tearing the tunnel down
			// when the transport is gone — and still return the error, so the
			// SOCKS client gets a failure reply rather than a dropped
			// connection it cannot interpret.
			if m.noteDialFailure(t, addr, err) {
				// The remote declined this one target and the tunnel is fine,
				// so the client can be told the truth: X'05', connection
				// refused. Only this branch may claim that — everything else
				// reaching socks5 unwrapped becomes X'01', which is what stops
				// a dead tunnel from being reported as a refusal by the
				// destination.
				return nil, fmt.Errorf("%w: %w", socks5.ErrDestinationRefused, err)
			}
			return nil, err
		}
		return remote, nil
	})
	if err != nil {
		// Per-connection and expected in normal use (a client that hangs up
		// mid-handshake, a probe that is not SOCKS at all), so this is debug,
		// not a warning about the tunnel.
		logDebug("ssh", "forward %s: socks5 session ended: %v", t.ruleID, err)
	}
}

// connectionLost tears down every tunnel riding the dead connection and marks
// the rules stopped with a reason.
//
// It matches on the *connection instance*, not on the host id: a straggler
// connection failing on a connection the user already stopped must not take
// down a tunnel that has since been restarted on a fresh one.
//
// It deliberately does not reconnect. A silent retry loop re-attempts
// authentication in the background and can trip a remote's failed-login
// lockout while the user is looking at something else; restarting is one
// click, and the reason is on screen (see ActiveForward).
func (m *tunnelManager) connectionLost(hostID string, hc *hostConn, reason string) {
	m.mu.Lock()
	m.ensureLocked()
	var dead []*tunnel
	for _, t := range m.tuns {
		if t.hc == hc && t.running {
			t.running = false
			t.errMsg = reason
			dead = append(dead, t)
		}
	}
	m.mu.Unlock()
	for _, t := range dead {
		m.teardown(t) // takes m.mu via releaseConn — must be outside the lock
	}
	if len(dead) > 0 {
		logWarn("ssh", "host %s: %s; %d tunnel(s) stopped, not reconnecting", hostID, reason, len(dead))
	}
}

// teardown closes a tunnel's listener and drops its connection reference,
// exactly once however often it is called.
func (m *tunnelManager) teardown(t *tunnel) {
	t.closeOnce.Do(func() {
		if t.listener != nil {
			_ = t.listener.Close()
		}
		t.closeLive()
		if t.hc != nil {
			m.releaseConn(t.hostID, t.hc)
		}
	})
}

func (m *tunnelManager) stop(hostID, ruleID string) error {
	key := tunnelKey(hostID, ruleID)
	m.mu.Lock()
	m.ensureLocked()
	t, ok := m.tuns[key]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("forward %s is not running", ruleID)
	}
	delete(m.tuns, key)
	t.running = false
	m.mu.Unlock()

	m.teardown(t)
	return nil
}

// stopAll tears down every tunnel; called on app shutdown so no listener
// outlives the window.
func (m *tunnelManager) stopAll() {
	m.mu.Lock()
	m.ensureLocked()
	all := make([]*tunnel, 0, len(m.tuns))
	for k, t := range m.tuns {
		t.running = false
		all = append(all, t)
		delete(m.tuns, k)
	}
	m.mu.Unlock()
	for _, t := range all {
		m.teardown(t)
	}
}

func (m *tunnelManager) list() []ActiveForward {
	m.mu.Lock()
	m.ensureLocked()
	out := make([]ActiveForward, 0, len(m.tuns))
	for _, t := range m.tuns {
		out = append(out, ActiveForward{
			HostID: t.hostID, RuleID: t.ruleID, Kind: t.kind,
			ListenAddr: t.listenAddr, Target: t.target,
			Conns: t.accepted.Load(), StartedAt: t.startedAt,
			Running: t.running, Error: t.errMsg,
		})
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].HostID != out[j].HostID {
			return out[i].HostID < out[j].HostID
		}
		return out[i].RuleID < out[j].RuleID
	})
	return out
}
