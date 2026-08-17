package main

import (
	"net"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The rule lifecycle and the tunnel lifecycle are joined by
// tunnelManager.reconcile, driven from configStore's post-commit observer.
// These tests pin the join from the outside: every one of them removes or
// edits a rule through a *real* entry point (the drawer's UpdateSSHHost,
// DeleteSSHHost, or an inbound sync) and then asserts on the socket, not on
// the manager's bookkeeping. A listener that is still bound is the bug; a map
// entry that is still present is only a symptom.

// savedHost reads a host back out of the store, so a test edits the record the
// user would actually be editing rather than a stale copy.
func savedHost(t *testing.T, a *App, id string) SSHHost {
	t.Helper()
	for _, h := range a.ListSSHHosts() {
		if h.ID == id {
			return h
		}
	}
	t.Fatalf("host %s is not saved", id)
	return SSHHost{}
}

// requireClosed fails unless nothing accepts on addr any more. Dialing is the
// only assertion that answers the question the user is actually asking ("is
// that port still open?") — ListActiveForwards agreeing is not the same claim.
func requireClosed(t *testing.T, addr, what string) {
	t.Helper()
	waitFor(t, 3*time.Second, what, func() bool {
		c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err != nil {
			return true
		}
		_ = c.Close()
		return false
	})
}

func requireNotListed(t *testing.T, a *App, ruleID string) {
	t.Helper()
	for _, f := range a.ListActiveForwards() {
		if f.RuleID == ruleID {
			t.Fatalf("rule %s is still listed after its rule went away: %+v", ruleID, f)
		}
	}
}

// TestDeletingARuleStopsItsTunnel is the core of the fix. Deleting a rule in
// the drawer is a plain list edit saved through UpdateSSHHost, and before the
// reconcile it left the tunnel running with no way to stop it: the tunnels tab
// renders from the saved rules, so the orphan had no row, and quitting the app
// was the only recovery.
//
// The host keeps a second running rule on purpose. That makes the assertion
// "reconcile stopped exactly the deleted one" rather than "reconcile stopped
// everything", and it keeps the shared SSH connection alive so r1's listener
// closing cannot be a side effect of the transport going away.
func TestDeletingARuleStopsItsTunnel(t *testing.T) {
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
	addr1 := activeForward(t, a, "r1").ListenAddr
	addr2 := activeForward(t, a, "r2").ListenAddr

	// Exactly what SshHostsPanel.vue does on "remove rule" + save: the whole
	// Forwards list minus one entry, through UpdateSSHHost.
	saved := savedHost(t, a, h.ID)
	saved.Forwards = []ForwardRule{saved.Forwards[1]}
	if err := a.UpdateSSHHost(saved, nil); err != nil {
		t.Fatalf("UpdateSSHHost: %v", err)
	}

	requireClosed(t, addr1, "the deleted rule's listener to be closed")
	requireNotListed(t, a, "r1")

	// r2 is untouched, and still actually forwards.
	if got := activeForward(t, a, "r2"); !got.Running {
		t.Fatalf("r2 must still be running: %+v", got)
	}
	c, err := net.DialTimeout("tcp", addr2, 3*time.Second)
	if err != nil {
		t.Fatalf("dial the surviving forward: %v", err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	echoThrough(t, c, "still up")
}

// TestDeletingAWideBoundRuleReleasesTheListener is the variant that makes this
// a safety fix rather than tidiness: a user who binds 0.0.0.0, thinks better
// of it and *deletes the rule* has not closed the exposure unless the listener
// actually goes away. The dial afterwards goes to the same port on loopback,
// which a 0.0.0.0 listener answers — so if anything is still bound, this
// catches it.
func TestDeletingAWideBoundRuleReleasesTheListener(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	wide := localRule("r1", targetHost, targetPort)
	wide.BindAddr = "0.0.0.0"
	a, h := newForwardTestApp(t, srv, wide)

	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward: %v", err)
	}
	listen := activeForward(t, a, "r1").ListenAddr
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		t.Fatalf("listen addr %q: %v", listen, err)
	}
	loopback := net.JoinHostPort("127.0.0.1", port)
	c, err := net.DialTimeout("tcp", loopback, 3*time.Second)
	if err != nil {
		t.Fatalf("the wide listener should be reachable before the delete: %v", err)
	}
	_ = c.Close()

	saved := savedHost(t, a, h.ID)
	saved.Forwards = nil
	if err := a.UpdateSSHHost(saved, nil); err != nil {
		t.Fatalf("UpdateSSHHost: %v", err)
	}

	requireClosed(t, loopback, "the 0.0.0.0 listener to be released")
	requireNotListed(t, a, "r1")
}

// TestDeletingTheHostStopsItsTunnels: DeleteSSHHost wipes the credential from
// the keyring a line after it rewrites the host list, so a tunnel that
// survived it would be a live authenticated SSH connection to a host atterm
// has no record of and no credential for.
func TestDeletingTheHostStopsItsTunnels(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv,
		localRule("r1", targetHost, targetPort),
		dynamicRule("d1"),
	)
	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward r1: %v", err)
	}
	if err := a.StartForward(h.ID, "d1"); err != nil {
		t.Fatalf("StartForward d1: %v", err)
	}
	addr1 := activeForward(t, a, "r1").ListenAddr
	addrD := activeForward(t, a, "d1").ListenAddr

	if err := a.DeleteSSHHost(h.ID); err != nil {
		t.Fatalf("DeleteSSHHost: %v", err)
	}

	requireClosed(t, addr1, "the local forward's listener to be closed")
	requireClosed(t, addrD, "the socks listener to be closed")
	if got := a.ListActiveForwards(); len(got) != 0 {
		t.Fatalf("tunnels survived the host delete: %+v", got)
	}
	// The last reference is gone, so the SSH connection must be closed too.
	waitFor(t, 3*time.Second, "the ssh connection to close", func() bool {
		opened, closed := srv.counts()
		return opened == 1 && closed == 1
	})
}

// TestEditingAnUnrelatedFieldKeepsTheTunnelRunning is the other half of the
// contract, and the one that would make the fix worse than the bug if it
// failed: saving the drawer re-writes the whole host record, so an
// over-eager reconcile would cut a live database session every time the user
// renamed a host or typed a label. The rule ID survives an edit
// (cloneForwards copies r.id through), which is what makes "gone" detectable
// at all.
func TestEditingAnUnrelatedFieldKeepsTheTunnelRunning(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	a, h := newForwardTestApp(t, srv, localRule("r1", targetHost, targetPort))
	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward: %v", err)
	}
	addr := activeForward(t, a, "r1").ListenAddr

	// An open, established connection through the tunnel: cutting it is the
	// user-visible harm, so the test holds one across the save.
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial forwarded port: %v", err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	echoThrough(t, c, "before the edit")

	saved := savedHost(t, a, h.ID)
	saved.Alias = "renamed"
	saved.Tags = []string{"prod"}
	saved.Forwards[0].Note = "postgres on the box"
	// Spelling the defaulted bind address out explicitly describes the same
	// listener, so it must not read as a change either.
	saved.Forwards[0].BindAddr = defaultForwardBindAddr
	if err := a.UpdateSSHHost(saved, nil); err != nil {
		t.Fatalf("UpdateSSHHost: %v", err)
	}

	if got := activeForward(t, a, "r1"); !got.Running || got.ListenAddr != addr {
		t.Fatalf("an unrelated edit tore the tunnel down: %+v", got)
	}
	echoThrough(t, c, "after the edit")
}

// TestChangingARuleBindStopsItsTunnel: a rule whose bind address changed while
// running is stale in the same way a deleted one is. The panel renders the
// *rule*, so after this edit it would claim 127.0.0.1 while the listener was
// still on 0.0.0.0 — and a user narrowing a wide bind is doing it precisely to
// close the exposure. Stopping is the honest answer; restarting is one click
// and nothing here ever auto-starts a tunnel.
func TestChangingARuleBindStopsItsTunnel(t *testing.T) {
	targetHost, targetPort := startEchoTarget(t)
	srv := startForwardingSSHTestServer(t)
	wide := localRule("r1", targetHost, targetPort)
	wide.BindAddr = "0.0.0.0"
	a, h := newForwardTestApp(t, srv, wide)
	if err := a.StartForward(h.ID, "r1"); err != nil {
		t.Fatalf("StartForward: %v", err)
	}
	listen := activeForward(t, a, "r1").ListenAddr
	_, port, _ := net.SplitHostPort(listen)

	saved := savedHost(t, a, h.ID)
	saved.Forwards[0].BindAddr = "127.0.0.1"
	if err := a.UpdateSSHHost(saved, nil); err != nil {
		t.Fatalf("UpdateSSHHost: %v", err)
	}

	requireClosed(t, net.JoinHostPort("127.0.0.1", port), "the old wide listener to be released")
	requireNotListed(t, a, "r1")
}

// TestInboundSyncDeletingARuleStopsItsTunnel is why the reconcile is hooked to
// the config store rather than to UpdateSSHHost: a rule deleted on another
// device arrives through appConfigAdapter.WriteValue, which decrypts the
// sealed blob and writes the host list into the store directly. Nothing in
// this path goes anywhere near UpdateSSHHost or DeleteSSHHost, and the local
// user takes no action at all — so a UI-level orphan row would never have
// covered it.
func TestInboundSyncDeletingARuleStopsItsTunnel(t *testing.T) {
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
	addr1 := activeForward(t, a, "r1").ListenAddr
	addr2 := activeForward(t, a, "r2").ListenAddr

	key := testAccountKey(t)
	adapter := newAppConfigAdapter(a.cfgStore, func() []byte { return key })

	// What the other device would push: the same host, minus r1.
	remote := savedHost(t, a, h.ID)
	remote.Forwards = []ForwardRule{remote.Forwards[1]}
	blob, err := sealSSHHosts(key,
		[]SSHHost{remote},
		map[string]sshCredential{remote.ID: {Password: "pw"}},
		nil, nil)
	if err != nil {
		t.Fatalf("sealSSHHosts: %v", err)
	}
	if err := adapter.WriteValue("ssh_hosts_encrypted", blob); err != nil {
		t.Fatalf("WriteValue: %v", err)
	}

	requireClosed(t, addr1, "the remotely-deleted rule's listener to be closed")
	requireNotListed(t, a, "r1")
	if got := activeForward(t, a, "r2"); !got.Running || got.ListenAddr != addr2 {
		t.Fatalf("the surviving rule's tunnel was disturbed: %+v", got)
	}
}

// failingListener wraps a real listener but fails Accept with a non-fatal
// error — EMFILE under fd exhaustion is the plausible one. It keeps a real
// Addr and a real Close so the tunnel's bookkeeping is exercised unchanged.
type failingListener struct {
	net.Listener
	err error
}

func (l *failingListener) Accept() (net.Conn, error) { return nil, l.err }

// TestAcceptFailureMarksTheTunnelStopped: acceptLoop used to return on any
// Accept error without touching the tunnel's state, on the assumption that the
// only way Accept fails is a listener closed by stop/teardown. It is not: an
// Accept that fails while the tunnel is still registered and running (EMFILE)
// killed the accept loop silently, and the panel went on reporting Running for
// a tunnel that would never accept another connection.
func TestAcceptFailureMarksTheTunnelStopped(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var m tunnelManager
	// A hostConn with no *sshclient.Conn: releaseConn drops the reference and
	// skips the close, which is all this test needs from it.
	hc := &hostConn{ready: make(chan struct{}), refs: 1}
	close(hc.ready)

	h := SSHHost{ID: "h1"}
	r := localRule("r1", "127.0.0.1", "5432")
	tun, err := m.registerTunnel(h, r, &failingListener{Listener: ln, err: syscall.EMFILE}, hc, forwardTarget(r))
	if err != nil {
		t.Fatalf("registerTunnel: %v", err)
	}

	m.acceptLoop(tun, func(*tunnel, net.Conn) { t.Error("serve must not run") })

	got := m.list()
	if len(got) != 1 {
		t.Fatalf("the tunnel must stay listed so its reason is visible: %+v", got)
	}
	if got[0].Running {
		t.Fatalf("a tunnel whose accept loop died must not report Running: %+v", got[0])
	}
	if !strings.Contains(got[0].Error, "stopped accepting") {
		t.Fatalf("the reason must say what happened, got %q", got[0].Error)
	}
}

// TestRuleDeletedDuringStartIsNotPublished closes the window between "the rule
// existed when the start began" and "the tunnel is registered": the start
// dials with a 15s timeout, and a rule deleted in the middle of it would
// otherwise register a tunnel nothing can ever show or stop — the same orphan,
// arrived at from the other direction.
func TestRuleDeletedDuringStartIsNotPublished(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var m tunnelManager
	hc := &hostConn{ready: make(chan struct{}), refs: 1}
	close(hc.ready)

	h := SSHHost{ID: "h1"}
	r := localRule("r1", "127.0.0.1", "5432")
	// The reconcile that ran while the start was in flight: the host has no
	// rules left.
	m.reconcile([]SSHHost{{ID: "h1"}})

	if _, err := m.registerTunnel(h, r, ln, hc, forwardTarget(r)); err == nil {
		t.Fatal("registering a tunnel for a deleted rule must fail")
	}
	if got := m.list(); len(got) != 0 {
		t.Fatalf("nothing must be published: %+v", got)
	}
}
