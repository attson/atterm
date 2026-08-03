// Package sshclient wraps golang.org/x/crypto/ssh to open a remote shell as a
// PTY-like stream (Read/Write/Resize/Close), so the desktop app can adopt it
// as a session. It does not depend on any desktop code.
package sshclient

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// AuthMethod produces the ssh.AuthMethod list for a connection.
type AuthMethod interface {
	sshAuthMethods() ([]ssh.AuthMethod, error)
}

// PasswordAuth authenticates with a username/password.
type PasswordAuth struct{ Password string }

func (a PasswordAuth) sshAuthMethods() ([]ssh.AuthMethod, error) {
	return []ssh.AuthMethod{ssh.Password(a.Password)}, nil
}

// PrivateKeyAuth authenticates with a PEM private key, optionally encrypted
// with a passphrase.
type PrivateKeyAuth struct {
	PEM        []byte
	Passphrase string // empty → key assumed unencrypted
}

func (a PrivateKeyAuth) sshAuthMethods() ([]ssh.AuthMethod, error) {
	var signer ssh.Signer
	var err error
	if a.Passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(a.PEM, []byte(a.Passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(a.PEM)
	}
	if err != nil {
		return nil, fmt.Errorf("sshclient: parse private key: %w", err)
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
}

// Config describes one SSH connection.
type Config struct {
	Host, Port, User string
	Auth             AuthMethod
	HostKeyCb        ssh.HostKeyCallback
	Cols, Rows       uint16
	Timeout          time.Duration // dial timeout; 0 → 15s
	Keepalive        time.Duration // keepalive interval; 0 → 30s
}

// Session is an opened remote shell satisfying Read/Write/Resize/Close.
// In PTY mode the remote stderr is merged into stdout by the remote tty, so a
// single stdout stream carries all output.
type Session struct {
	client  *ssh.Client
	sess    *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	closeCh chan struct{}
}

// Dial connects, authenticates, requests a PTY and starts a shell.
func Dial(ctx context.Context, cfg Config) (*Session, error) {
	if cfg.Auth == nil {
		return nil, fmt.Errorf("sshclient: nil auth")
	}
	methods, err := cfg.Auth.sshAuthMethods()
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	port := cfg.Port
	if port == "" {
		port = "22"
	}
	clientCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            methods,
		HostKeyCallback: cfg.HostKeyCb,
		Timeout:         timeout,
	}
	addr := net.JoinHostPort(cfg.Host, port)
	client, err := ssh.Dial("tcp", addr, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("sshclient: dial %s: %w", addr, err)
	}
	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("sshclient: new session: %w", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		client.Close()
		return nil, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		client.Close()
		return nil, err
	}
	cols, rows := cfg.Cols, cfg.Rows
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1}
	if err := sess.RequestPty("xterm-256color", int(rows), int(cols), modes); err != nil {
		client.Close()
		return nil, fmt.Errorf("sshclient: request pty: %w", err)
	}
	if err := sess.Shell(); err != nil {
		client.Close()
		return nil, fmt.Errorf("sshclient: shell: %w", err)
	}
	s := &Session{client: client, sess: sess, stdin: stdin, stdout: stdout, closeCh: make(chan struct{})}
	go s.keepalive(cfg.Keepalive)
	return s, nil
}

func (s *Session) Read(p []byte) (int, error)  { return s.stdout.Read(p) }
func (s *Session) Write(p []byte) (int, error) { return s.stdin.Write(p) }

func (s *Session) Resize(cols, rows uint16) error {
	return s.sess.WindowChange(int(rows), int(cols))
}

// Wait blocks until the remote shell exits or the connection drops.
func (s *Session) Wait() error { return s.sess.Wait() }

func (s *Session) Close() error {
	select {
	case <-s.closeCh:
	default:
		close(s.closeCh)
	}
	_ = s.sess.Close()
	return s.client.Close()
}

func (s *Session) keepalive(interval time.Duration) {
	if interval == 0 {
		interval = 30 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-s.closeCh:
			return
		case <-t.C:
			if _, _, err := s.client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				s.Close()
				return
			}
		}
	}
}

// IsAuthError reports whether err looks like an SSH authentication failure
// (bad password / invalid key) rather than a network/dial error.
func IsAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "unable to authenticate") ||
		strings.Contains(msg, "no supported methods remain") ||
		strings.Contains(msg, "handshake failed")
}
