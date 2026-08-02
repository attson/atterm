package main

import (
	"context"
	"fmt"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/sshclient"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

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
	case "privateKey":
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
