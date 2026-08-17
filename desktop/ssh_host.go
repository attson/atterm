package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/sshclient"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

// hostRunsProxyCommand reports whether h is configured with an arbitrary proxy
// command, and returns the user-facing reason to refuse it.
//
// ProxyCommand is never executed by atterm at all (it would be an RCE surface),
// and unlike ProxyJump it never will be — so this is a refusal, not a
// not-yet. Callers must run it *before* reading any credential and before any
// dial: the point is that nothing happens for a host we refuse.
//
// Until roadmap item 27 this function also refused ProxyJump. It no longer
// does: a ProxyJump host goes through the chain builder in ssh_jump.go, which
// dials each hop as its own saved host and verifies each hop's key. What has
// not changed is that both entry points that dial a saved host — the terminal
// (NewSshSessionByID) and the tunnel (StartForward) — gate on this one
// function. Item 26's review confirmed that property deliberately, and the
// design's risk 4 is exactly what spreading it would cost: relaxing one path
// and not the other gives "the terminal connects but the tunnel says
// unsupported", which is worse than a uniform refusal.
func hostRunsProxyCommand(h SSHHost) (bool, string) {
	if h.ProxyCommand != "" {
		return true, fmt.Sprintf(
			"host %q is configured with a ProxyCommand (%q); atterm never runs that command, so this host cannot be connected directly",
			h.Alias, h.ProxyCommand)
	}
	return false, ""
}

// findSSHHost looks a saved host up by ID.
func (a *App) findSSHHost(id string) (SSHHost, bool) {
	if id == "" {
		return SSHHost{}, false
	}
	for _, h := range a.ListSSHHosts() {
		if h.ID == id {
			return h, true
		}
	}
	return SSHHost{}, false
}

// NewSshSessionByID looks up a saved host + its credential by ID and connects,
// reusing NewSshSession (which carries the slice-1 known_hosts TOFU flow).
// Returns errCredentialMissing when no credential is stored so the frontend
// can prompt the user to supply one.
func (a *App) NewSshSessionByID(id string) (NewSessionResp, error) {
	if a.host == nil {
		return NewSessionResp{}, fmt.Errorf("relay host not ready")
	}
	found, ok := a.findSSHHost(id)
	if !ok {
		return NewSessionResp{}, fmt.Errorf("no such host: %s", id)
	}

	// Refuse hosts that ssh_config marked as needing an arbitrary proxy
	// command, before any credential read or dial. A ProxyJump host is not
	// refused: NewSshSession builds its chain from SSHHostID below.
	if refused, reason := hostRunsProxyCommand(found); refused {
		return NewSessionResp{}, errors.New(reason)
	}

	req := SSHConnectReq{
		Host: found.Host, Port: found.Port, User: found.User,
		AuthKind:  found.AuthKind,
		SSHHostID: found.ID, // carried into SessionInfo, and used to find the chain
	}
	switch found.AuthKind {
	case "key":
		sec, err := sshKeySecretSlot(found.KeyID).Load()
		if err != nil || sec.PrivateKey == "" {
			return NewSessionResp{}, errors.New(errKeyMissing)
		}
		req.PrivateKey = sec.PrivateKey
		req.Passphrase = sec.Passphrase
	default: // "password"
		cred, err := sshCredentialSlot(id).Load()
		if err != nil || cred == (sshCredential{}) {
			return NewSessionResp{}, errors.New(errCredentialMissing)
		}
		req.Password = cred.Password
	}
	return a.NewSshSession(req)
}

// NewSshSession opens an SSH remote shell as an adoptable session. On an
// unknown host key it returns *HostKeyUnknownError with the fingerprint; the
// frontend shows a TOFU dialog and retries with that host + fingerprint echoed
// back. It builds the known_hosts callback (defaulting to ~/.ssh/known_hosts)
// and delegates the actual dial + adopt to relayHost.OpenSSHSession.
//
// When the request came from a saved host (SSHHostID set), the host's jump
// hosts are dialled first and handed to OpenSSHSession as the transport for the
// last link. It is dialJumpHops rather than dialThroughJumps because the last
// link is not a plain connection: sshclient.Dial is the only thing that puts a
// PTY on one, and it opens its own connection with Via set. Building the whole
// chain here would open a target connection with no shell on it — a wasted
// login, an extra line in the remote's `who`, and one more thing to close.
func (a *App) NewSshSession(req SSHConnectReq) (NewSessionResp, error) {
	if a.host == nil {
		return NewSessionResp{}, fmt.Errorf("relay host not ready")
	}
	khPath := a.knownHostsPath()

	// An ad-hoc request (no SSHHostID) has no saved record and so no chain:
	// nil chain, nil Via, hop 0 — exactly the direct connection this path has
	// always made.
	var chain *jumpChain
	hopName := ""
	if h, ok := a.findSSHHost(req.SSHHostID); ok {
		hopName = sshHostLabel(h)
		c, err := a.dialJumpHops(a.ctx, h, req.acceptedHostKey())
		if err != nil {
			return NewSessionResp{}, err
		}
		chain = c
	}
	// The hop number the destination carries in a TOFU prompt, read off the
	// chain rather than recomputed, so the two paths cannot disagree about
	// which machine "hop 3" is. 0 for a direct connection.
	hopIndex := chain.targetHopIndex()

	var unknown *HostKeyUnknownError
	cb := sshclient.KnownHostsCallback(khPath, func(host, fp string) bool {
		// Only the exact key the user was shown and agreed to — never "the next
		// unknown key", which on a chain would write a stranger's key into
		// known_hosts. See acceptedHostKey.
		if req.acceptedHostKey().accepts(host, fp) {
			return true
		}
		unknown = &HostKeyUnknownError{
			Fingerprint: fp, Host: host,
			HopIndex: hopIndex, HopName: hopName,
		}
		return false
	})

	id, err := a.host.OpenSSHSession(a.ctx, req, cb, chain)
	if err != nil {
		if unknown != nil {
			return NewSessionResp{}, unknown // typed → frontend shows fingerprint
		}
		return NewSessionResp{}, err
	}
	return NewSessionResp{SessionID: id.String()}, nil
}

// sshPtyHost adapts *sshclient.Session to relay.PtyHost + sessionPTY. The
// embedded *sshclient.Session already provides Read/Write/Resize/Close.
type sshPtyHost struct{ *sshclient.Session }

// OpenSSHSession dials an SSH host, opens a remote shell, and adopts it as a
// relay session so it flows through the same takeover + E2EE pipeline as a
// local shell. hostKeyCb is injected: the real known_hosts callback in prod,
// FixedHostKey in tests. Credentials in req are used for this connection only
// and are never persisted (slice 1).
//
// chain, when non-nil, is the host's jump hosts, already connected: the shell
// is opened over its last hop. From here the chain belongs to this function —
// Session.Close closes only the target connection, so every exit below has to
// close the chain itself or the bastion logins outlive the terminal that needed
// them. A nil chain is a direct connection and every call below is a no-op.
func (h *relayHost) OpenSSHSession(ctx context.Context, req SSHConnectReq, hostKeyCb ssh.HostKeyCallback, chain *jumpChain) (uuid.UUID, error) {
	var auth sshclient.AuthMethod
	switch req.AuthKind {
	case "key":
		auth = sshclient.PrivateKeyAuth{PEM: []byte(req.PrivateKey), Passphrase: req.Passphrase}
	default:
		auth = sshclient.PasswordAuth{Password: req.Password}
	}

	cols, rows := req.Cols, req.Rows
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}

	sess, err := sshclient.Dial(ctx, sshclient.Config{
		Host: req.Host, Port: req.Port, User: req.User,
		Auth:      auth,
		HostKeyCb: hostKeyCb,
		Cols:      cols, Rows: rows,
		Timeout: 15 * time.Second,
		Via:     chain.Target(), // nil for a direct connection
	})
	if err != nil {
		_ = chain.Close()
		return uuid.Nil, err
	}

	id := uuid.New()
	title := "ssh " + req.User + "@" + req.Host
	info := proto.SessionInfo{
		Command:   title,
		Title:     title,
		Cols:      cols,
		Rows:      rows,
		HostID:    h.hostID,
		Host:      req.Host,
		User:      req.User,
		SSHHostID: req.SSHHostID, // empty for ad-hoc; set for saved-host connects
		StartedAt: time.Now().Unix(),
	}

	host := &sshPtyHost{Session: sess}
	cleanup := h.server.AdoptSession(ctx, id, info, host, h.adminUserID)

	h.mu.Lock()
	if h.sessions == nil {
		h.mu.Unlock()
		cleanup()
		_ = sess.Close()
		_ = chain.Close()
		return uuid.Nil, fmt.Errorf("relay host stopped")
	}
	h.sessions[id] = &activeSession{host: host, cleanup: cleanup}
	h.mu.Unlock()
	h.notifyChange()

	// Watch for remote shell exit / disconnect → clean up the session, so a
	// dropped SSH connection ends the session the same way a local PTY exit
	// does. This is also where a jump-host chain ends: the hops exist only to
	// carry this shell, and closing the shell does not close them.
	go func() {
		_ = sess.Wait()
		h.mu.Lock()
		delete(h.sessions, id)
		h.mu.Unlock()
		cleanup()
		_ = sess.Close()
		_ = chain.Close()
		h.notifyChange()
	}()

	return id, nil
}
