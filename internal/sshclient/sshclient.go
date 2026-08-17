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

// Conn is an authenticated SSH connection with no remote shell attached.
// It exists purely to carry forwarded traffic (local -L, remote -R, dynamic
// -D): dialing one does not touch the remote PTY table and never shows up
// in `who`. Session is built on top of a Conn (not the other way around),
// so the shell-less path (DialConn) has no code left in it that could
// accidentally request a pty — Dial is the only place RequestPty is called.
type Conn struct {
	client  *ssh.Client
	closeCh chan struct{}
}

// Session is an opened remote shell satisfying Read/Write/Resize/Close.
// In PTY mode the remote stderr is merged into stdout by the remote tty, so a
// single stdout stream carries all output.
type Session struct {
	conn   *Conn
	sess   *ssh.Session
	stdin  io.WriteCloser
	stdout io.Reader
}

// DialConn connects, authenticates and starts a keepalive loop. It
// deliberately does not request a PTY or start a shell — callers that want
// an interactive session should use Dial. DialConn is for port forwarding,
// where opening a shell would waste a remote PTY and leave a spurious login
// session behind for no reason.
func DialConn(ctx context.Context, cfg Config) (*Conn, error) {
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
	c := &Conn{client: client, closeCh: make(chan struct{})}
	go c.keepalive(cfg.Keepalive)
	return c, nil
}

// DialRemote opens a connection through the remote host, as `ssh -L` does:
// the remote host dials network/addr on its side and the returned net.Conn
// carries traffic to/from it.
func (c *Conn) DialRemote(network, addr string) (net.Conn, error) {
	return c.client.Dial(network, addr)
}

// ListenRemote asks the remote host to listen on network/addr and forward
// accepted connections back to us, as `ssh -R` does.
func (c *Conn) ListenRemote(network, addr string) (net.Listener, error) {
	return c.client.Listen(network, addr)
}

// Done is closed when the connection is gone — either because Close was
// called or because the keepalive loop found the transport dead. It is a
// read-only view of the same channel Close closes, so a caller can react to a
// drop instead of only discovering it on the next DialRemote. Without it a
// port forward on an idle connection keeps reporting itself as running.
func (c *Conn) Done() <-chan struct{} { return c.closeCh }

func (c *Conn) Close() error {
	select {
	case <-c.closeCh:
	default:
		close(c.closeCh)
	}
	return c.client.Close()
}

func (c *Conn) keepalive(interval time.Duration) {
	if interval == 0 {
		interval = 30 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-c.closeCh:
			return
		case <-t.C:
			if _, _, err := c.client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				c.Close()
				return
			}
		}
	}
}

// Dial connects, authenticates, requests a PTY and starts a shell.
func Dial(ctx context.Context, cfg Config) (*Session, error) {
	c, err := DialConn(ctx, cfg)
	if err != nil {
		return nil, err
	}
	sess, err := c.client.NewSession()
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("sshclient: new session: %w", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		c.Close()
		return nil, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		c.Close()
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
		c.Close()
		return nil, fmt.Errorf("sshclient: request pty: %w", err)
	}
	if err := sess.Shell(); err != nil {
		c.Close()
		return nil, fmt.Errorf("sshclient: shell: %w", err)
	}
	s := &Session{conn: c, sess: sess, stdin: stdin, stdout: stdout}
	return s, nil
}

func (s *Session) Read(p []byte) (int, error)  { return s.stdout.Read(p) }
func (s *Session) Write(p []byte) (int, error) { return s.stdin.Write(p) }

func (s *Session) Resize(cols, rows uint16) error {
	return s.sess.WindowChange(int(rows), int(cols))
}

// Wait blocks until the remote shell exits or the connection drops.
func (s *Session) Wait() error { return s.sess.Wait() }

// Close closes the remote shell and the underlying connection (and, with
// it, stops the connection's keepalive goroutine).
func (s *Session) Close() error {
	_ = s.sess.Close()
	return s.conn.Close()
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
