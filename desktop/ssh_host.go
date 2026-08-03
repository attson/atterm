package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/safekeyring"
	"github.com/attson/atterm/internal/sshclient"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

// NewSshSessionByID looks up a saved host + its credential by ID and connects,
// reusing NewSshSession (which carries the slice-1 known_hosts TOFU flow).
// Returns errCredentialMissing when no credential is stored so the frontend
// can prompt the user to supply one.
func (a *App) NewSshSessionByID(id string) (NewSessionResp, error) {
	if a.host == nil {
		return NewSessionResp{}, fmt.Errorf("relay host not ready")
	}
	var found *SSHHost
	for _, h := range a.ListSSHHosts() {
		if h.ID == id {
			hh := h
			found = &hh
			break
		}
	}
	if found == nil {
		return NewSessionResp{}, fmt.Errorf("no such host: %s", id)
	}

	req := SSHConnectReq{
		Host: found.Host, Port: found.Port, User: found.User,
		AuthKind:      found.AuthKind,
		AcceptHostKey: false,
		SSHHostID:     found.ID, // carried into SessionInfo for recovery reconnect
	}
	switch found.AuthKind {
	case "key":
		raw, err := safekeyring.Get(sshKeyService(), found.KeyID)
		if err != nil {
			return NewSessionResp{}, errors.New(errKeyMissing)
		}
		var sec sshKeySecret
		if err := json.Unmarshal([]byte(raw), &sec); err != nil {
			return NewSessionResp{}, errors.New(errKeyMissing)
		}
		req.PrivateKey = sec.PrivateKey
		req.Passphrase = sec.Passphrase
	default: // "password"
		raw, err := safekeyring.Get(sshCredentialService(), id)
		if err != nil {
			return NewSessionResp{}, errors.New(errCredentialMissing)
		}
		var cred sshCredential
		if err := json.Unmarshal([]byte(raw), &cred); err != nil {
			return NewSessionResp{}, errors.New(errCredentialMissing)
		}
		req.Password = cred.Password
	}
	return a.NewSshSession(req)
}

// NewSshSession opens an SSH remote shell as an adoptable session. On an
// unknown host key it returns *HostKeyUnknownError with the fingerprint; the
// frontend shows a TOFU dialog and retries with AcceptHostKey=true. It builds
// the known_hosts callback (defaulting to ~/.ssh/known_hosts) and delegates
// the actual dial + adopt to relayHost.OpenSSHSession.
func (a *App) NewSshSession(req SSHConnectReq) (NewSessionResp, error) {
	if a.host == nil {
		return NewSessionResp{}, fmt.Errorf("relay host not ready")
	}
	khPath := a.sshKnownHostsPath
	if khPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			khPath = filepath.Join(home, ".ssh", "known_hosts")
		}
	}

	var unknown *HostKeyUnknownError
	cb := sshclient.KnownHostsCallback(khPath, func(host, fp string) bool {
		if req.AcceptHostKey {
			return true // user already confirmed in the TOFU dialog
		}
		unknown = &HostKeyUnknownError{Fingerprint: fp, Host: host}
		return false
	})

	id, err := a.host.OpenSSHSession(a.ctx, req, cb)
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
func (h *relayHost) OpenSSHSession(ctx context.Context, req SSHConnectReq, hostKeyCb ssh.HostKeyCallback) (uuid.UUID, error) {
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
	})
	if err != nil {
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
		return uuid.Nil, fmt.Errorf("relay host stopped")
	}
	h.sessions[id] = &activeSession{host: host, cleanup: cleanup}
	h.mu.Unlock()
	h.notifyChange()

	// Watch for remote shell exit / disconnect → clean up the session, so a
	// dropped SSH connection ends the session the same way a local PTY exit
	// does.
	go func() {
		_ = sess.Wait()
		h.mu.Lock()
		delete(h.sessions, id)
		h.mu.Unlock()
		cleanup()
		_ = sess.Close()
		h.notifyChange()
	}()

	return id, nil
}
