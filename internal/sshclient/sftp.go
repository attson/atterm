package sshclient

import (
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/ssh"
)

// SFTPStream is one open SFTP subsystem channel on a Conn: Read yields the
// remote sftp-server's stdout, Write feeds its stdin, Close ends that channel
// and only that channel — the Conn, and every tunnel and shell riding on it,
// stay up.
//
// This is deliberately the narrowest thing that lets a caller speak SFTP, and
// it is the only SFTP-shaped thing sshclient hands out. Conn keeps its
// *ssh.Client unexported on purpose (see the Conn doc comment): whoever holds
// an *ssh.Client can NewSession + RequestPty + Shell, which would put a second
// remote shell outside the one code path this package exists to funnel shells
// through — invisible to the session table and to `who` accounting alike. The
// subsystem name below is a constant, so what comes back from OpenSFTP can
// carry SFTP bytes and nothing else: no shell, no exec, no pty, no port
// forward, no second subsystem.
type SFTPStream struct {
	sess   *ssh.Session
	stdin  io.WriteCloser
	stdout io.Reader

	closeOnce sync.Once
	closeErr  error
}

// OpenSFTP opens a session channel on the connection and starts the "sftp"
// subsystem on it, returning the channel's byte stream. It requests no pty and
// starts no shell, so it costs no remote tty and leaves no login session
// behind — the same reasoning that keeps DialConn shell-less.
//
// The caller owns the returned stream and must Close it; closing it does not
// close the Conn, which is shared (per the design's §4.2 decision to reuse the
// host's existing connection rather than dial a second one for SFTP).
func (c *Conn) OpenSFTP() (*SFTPStream, error) {
	sess, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("sshclient: new sftp session: %w", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("sshclient: sftp stdin: %w", err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("sshclient: sftp stdout: %w", err)
	}
	if err := sess.RequestSubsystem("sftp"); err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("sshclient: request sftp subsystem: %w", err)
	}
	return &SFTPStream{sess: sess, stdin: stdin, stdout: stdout}, nil
}

func (s *SFTPStream) Read(p []byte) (int, error)  { return s.stdout.Read(p) }
func (s *SFTPStream) Write(p []byte) (int, error) { return s.stdin.Write(p) }

// Close ends the subsystem channel. It is safe to call repeatedly and returns
// the same result every time, which is not cosmetic: an SFTP client library
// closes the write side it was handed when it shuts down, and the owner of the
// stream closes it again on teardown, so the second call is the normal case
// rather than a bug.
func (s *SFTPStream) Close() error {
	s.closeOnce.Do(func() {
		_ = s.stdin.Close()
		if err := s.sess.Close(); err != nil && err != io.EOF {
			s.closeErr = err
		}
	})
	return s.closeErr
}
