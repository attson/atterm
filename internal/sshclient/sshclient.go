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
	"sync"
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

	// Via, when non-nil, is the connection this dial rides on: the TCP
	// connection to Host:Port is opened *from that host* (via its
	// DialRemote, a direct-tcpip channel) rather than from this machine,
	// and the SSH handshake happens on top of it. This is what makes jump
	// hosts possible — a chain is just this, folded: hop 1 dialled
	// directly, hop 2 via hop 1, the target via the last hop. Nil means
	// dial directly, which is what every caller other than the chain
	// builder does, and must keep doing.
	Via *Conn
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
	// closeOnce is what makes Close safe to call from several goroutines at
	// once, which is not hypothetical: the keepalive loop closes on a failed
	// ping while the tunnel manager's last releaseConn closes on the same
	// transport death, milliseconds apart. A check-then-close on closeCh lets
	// both take the "not closed yet" branch and the second close panics
	// ("close of closed channel"), which is unrecoverable and takes every
	// terminal session in the app down with it.
	closeOnce sync.Once
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
	var client *ssh.Client
	if cfg.Via == nil {
		client, err = ssh.Dial("tcp", addr, clientCfg)
		if err != nil {
			return nil, fmt.Errorf("sshclient: dial %s: %w", addr, err)
		}
	} else {
		raw, derr := cfg.Via.DialRemote("tcp", addr)
		if derr != nil {
			return nil, fmt.Errorf("sshclient: reach %s through jump host: %w", addr, derr)
		}
		cc, chans, reqs, herr := newClientConnBounded(raw, addr, clientCfg, timeout)
		if herr != nil {
			// DialRemote already opened a channel on the jump host; if the
			// handshake on top of it fails (including on our own timeout)
			// we still have to close raw ourselves, or that channel is left
			// dangling on the jump host with nothing left downstream to
			// ever close it. x/crypto v0.37.0 does close it on both of its
			// own error paths, and newClientConnBounded closes it on a
			// timeout — but this stays unconditional rather than relying on
			// a detail of the dependency's error handling.
			_ = raw.Close()
			return nil, fmt.Errorf("sshclient: handshake with %s through jump host: %w", addr, herr)
		}
		client = ssh.NewClient(cc, chans, reqs)
	}
	c := &Conn{client: client, closeCh: make(chan struct{})}
	go c.keepalive(cfg.Keepalive)
	return c, nil
}

// newClientConnBounded runs ssh.NewClientConn with a bound on the whole
// handshake, for the one case that needs one: raw is the net.Conn
// DialConn's Via branch gets from Conn.DialRemote, which is backed by an SSH
// channel (x/crypto/ssh's chanConn), not a socket — its SetDeadline always
// returns "deadline not supported". That is why ClientConfig.Timeout, which
// becomes a net.DialTimeout on the direct path (ssh.Dial), bounds nothing
// here: there is no OS-level deadline to hand it. Left alone, a hop whose
// handshake never completes (the bastion accepted the TCP/channel but the
// far side's sshd stalls) blocks until *that* side gives up, which is
// nowhere near timeout.
//
// The workaround is a timer instead of a deadline: the handshake runs on its
// own goroutine, and if timeout elapses before it returns, raw is closed out
// from under it and the hang turns into an error. Note the bound is soft, not
// hard: raw is an SSH channel, not a socket, so closing it only *sends* a
// close message — the blocked read unblocks when the jump host answers, or
// when that connection's own keepalive tears the mux down. Against a healthy
// jump host that is one round trip; against one that is TCP-alive but not
// answering, it is bounded by Keepalive instead. The timer is stopped the instant the handshake
// returns on its own, so once a connection is established nothing is left
// armed to cut it later — that is the failure mode to avoid, and it is why
// this must not be a deadline set once and left on raw.
func newClientConnBounded(raw net.Conn, addr string, cfg *ssh.ClientConfig, timeout time.Duration) (ssh.Conn, <-chan ssh.NewChannel, <-chan *ssh.Request, error) {
	type result struct {
		cc    ssh.Conn
		chans <-chan ssh.NewChannel
		reqs  <-chan *ssh.Request
		err   error
	}
	done := make(chan result, 1)
	go func() {
		cc, chans, reqs, err := ssh.NewClientConn(raw, addr, cfg)
		done <- result{cc, chans, reqs, err}
	}()

	timer := time.AfterFunc(timeout, func() { _ = raw.Close() })
	r := <-done
	if !timer.Stop() {
		// The timer already fired (and closed raw) by the time the
		// handshake goroutine returned. Closing raw normally turns
		// NewClientConn into an error, but if it won the race and reported
		// success anyway, that success is riding a transport we just told
		// to die — close it and report the timeout rather than hand back a
		// client nothing can trust.
		if r.err == nil {
			_ = r.cc.Close()
		}
		return nil, nil, nil, fmt.Errorf("handshake did not complete within %s", timeout)
	}
	return r.cc, r.chans, r.reqs, r.err
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

// Close closes the connection and Done. It is safe to call concurrently and
// repeatedly; only the first call closes Done. ssh.Client.Close is idempotent
// in the way that matters here (it ends in net.Conn.Close, which reports an
// error rather than misbehaving on a second call), so a late caller gets that
// error and nothing else happens.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() { close(c.closeCh) })
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
