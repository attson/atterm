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

// Forward kinds. Only "local" is implemented here; "remote" and "dynamic"
// are roadmap item 26 tasks 3 and 4.
const (
	forwardKindLocal   = "local"
	forwardKindRemote  = "remote"
	forwardKindDynamic = "dynamic"
)

// defaultForwardBindAddr is what an empty ForwardRule.BindAddr means, and it
// is loopback for a reason that outranks convenience: binding 0.0.0.0 puts the
// forwarded service in front of everyone on the same network **with no SSH
// credential at all** — and the rule syncs to every device, where it would
// bind the same way. The UI warns about non-loopback values; this constant is
// what makes the default safe when no UI is involved.
const defaultForwardBindAddr = "127.0.0.1"

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
	case forwardKindRemote, forwardKindDynamic:
		return fmt.Errorf("%s forwarding is not implemented yet", rule.Kind)
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

// startLocal brings up a -L tunnel: listen locally, and hand every accepted
// connection to the remote host via a direct-tcpip channel.
func (m *tunnelManager) startLocal(h SSHHost, r ForwardRule, dial func() (*sshclient.Conn, error)) error {
	key := tunnelKey(h.ID, r.ID)

	m.mu.Lock()
	m.ensureLocked()
	if m.starting[key] {
		m.mu.Unlock()
		return fmt.Errorf("forward %s is already starting", r.ID)
	}
	if t, ok := m.tuns[key]; ok && t.running {
		m.mu.Unlock()
		return fmt.Errorf("forward %s is already running on %s", r.ID, t.listenAddr)
	}
	m.starting[key] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.starting, key)
		m.mu.Unlock()
	}()

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

	t := &tunnel{
		hostID: h.ID, ruleID: r.ID, kind: r.Kind,
		listener: ln, listenAddr: ln.Addr().String(),
		target: forwardTarget(r), startedAt: time.Now().Unix(),
		hc: hc, running: true,
		live: map[net.Conn]struct{}{},
	}

	m.mu.Lock()
	m.ensureLocked()
	// A previous entry here can only be a stopped one (the running check
	// above plus the starting flag rule that out); starting the rule again
	// replaces it and clears its error.
	m.tuns[key] = t
	m.mu.Unlock()

	go m.acceptLoop(t)
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
	close(hc.ready)
	if hc.err != nil {
		m.releaseConn(hostID, hc)
		return nil, hc.err
	}
	return hc, nil
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

func (m *tunnelManager) acceptLoop(t *tunnel) {
	for {
		c, err := t.listener.Accept()
		if err != nil {
			return // listener closed by stop/teardown
		}
		if !t.trackConn(c) {
			_ = c.Close() // raced with teardown
			continue
		}
		t.accepted.Add(1)
		go m.serveLocalConn(t, c)
	}
}

// serveLocalConn splices one accepted local connection to the remote target
// over a direct-tcpip channel.
func (m *tunnelManager) serveLocalConn(t *tunnel, local net.Conn) {
	defer t.untrackConn(local)

	remote, err := t.hc.conn.DialRemote("tcp", t.target)
	if err != nil {
		_ = local.Close()
		var openErr *ssh.OpenChannelError
		if errors.As(err, &openErr) {
			// The remote refused this one channel — nothing is listening on
			// the target, typically. The tunnel itself is fine.
			logWarn("ssh", "forward %s: remote refused %s: %v", t.ruleID, t.target, err)
			return
		}
		// Anything else means the SSH transport is gone.
		m.connectionLost(t.hostID, t.hc, err)
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
func (m *tunnelManager) connectionLost(hostID string, hc *hostConn, cause error) {
	reason := fmt.Sprintf("ssh connection lost: %v", cause)
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
