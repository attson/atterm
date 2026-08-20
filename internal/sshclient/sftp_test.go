package sshclient

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// subsystemRequest is the payload of a "subsystem" channel request
// (RFC 4254 §6.5): a single string naming the subsystem to start.
type subsystemRequest struct{ Name string }

// startSFTPTestServer starts an in-memory ssh server that accepts user="u" /
// password="pw" and, on a "subsystem" request naming "sftp", runs a real
// pkg/sftp server over the channel. Every channel request type it sees is
// recorded, so a test can assert on what the client asked for — the point
// being that OpenSFTP must ask for a subsystem and nothing else.
func startSFTPTestServer(t *testing.T) (addr string, hostPub ssh.PublicKey, rec *channelRequestRecorder) {
	t.Helper()
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "u" && string(pass) == "pw" {
				return &ssh.Permissions{}, nil
			}
			return nil, io.EOF
		},
	}
	hostKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	rec = &channelRequestRecorder{}
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go serveConnSFTP(nc, cfg, rec)
		}
	}()
	return ln.Addr().String(), signer.PublicKey(), rec
}

func serveConnSFTP(nc net.Conn, cfg *ssh.ServerConfig, rec *channelRequestRecorder) {
	sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	defer sc.Close()
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "only session")
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			return
		}
		go func(ch ssh.Channel, chReqs <-chan *ssh.Request) {
			for r := range chReqs {
				rec.record(r.Type)
				if r.Type != "subsystem" {
					if r.WantReply {
						_ = r.Reply(false, nil)
					}
					continue
				}
				var payload subsystemRequest
				if err := ssh.Unmarshal(r.Payload, &payload); err != nil || payload.Name != "sftp" {
					if r.WantReply {
						_ = r.Reply(false, nil)
					}
					continue
				}
				if r.WantReply {
					_ = r.Reply(true, nil)
				}
				srv, err := sftp.NewServer(ch)
				if err != nil {
					ch.Close()
					continue
				}
				go func() { _ = srv.Serve(); ch.Close() }()
			}
		}(ch, chReqs)
	}
}

// TestOpenSFTPSpeaksSFTPAndAsksForNothingElse is the load-bearing test for the
// accessor: it proves the returned stream really carries SFTP (a full round
// trip against a real sftp server lists a directory), and that getting there
// cost exactly one "subsystem" channel request — no pty-req, no shell, no
// exec. Conn keeps its *ssh.Client private precisely so this is the only way
// in; if this assertion ever loosens, the shell-bypass it guards against is
// back.
func TestOpenSFTPSpeaksSFTPAndAsksForNothingElse(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	addr, hostPub, rec := startSFTPTestServer(t)
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := DialConn(ctx, Config{
		Host: host, Port: port, User: "u",
		Auth:      PasswordAuth{Password: "pw"},
		HostKeyCb: ssh.FixedHostKey(hostPub),
	})
	if err != nil {
		t.Fatalf("DialConn: %v", err)
	}
	defer conn.Close()

	stream, err := conn.OpenSFTP()
	if err != nil {
		t.Fatalf("OpenSFTP: %v", err)
	}
	defer stream.Close()

	client, err := sftp.NewClientPipe(stream, stream)
	if err != nil {
		t.Fatalf("NewClientPipe: %v", err)
	}
	defer client.Close()

	infos, err := client.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(infos) != 1 || infos[0].Name() != "hello.txt" {
		t.Fatalf("ReadDir = %v, want one entry hello.txt", infos)
	}

	got := rec.ChannelRequests()
	want := []string{"subsystem"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("channel requests = %v, want %v (a shell/pty request here means the accessor leaks a way around the PTY path)", got, want)
	}
}

// TestSFTPStreamCloseIsIdempotent covers the double-close the desktop will do
// in practice: sftp.Client.Close closes the write side, and the owner of the
// stream closes it again on teardown.
func TestSFTPStreamCloseIsIdempotent(t *testing.T) {
	addr, hostPub, _ := startSFTPTestServer(t)
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := DialConn(ctx, Config{
		Host: host, Port: port, User: "u",
		Auth:      PasswordAuth{Password: "pw"},
		HostKeyCb: ssh.FixedHostKey(hostPub),
	})
	if err != nil {
		t.Fatalf("DialConn: %v", err)
	}
	defer conn.Close()

	stream, err := conn.OpenSFTP()
	if err != nil {
		t.Fatalf("OpenSFTP: %v", err)
	}
	first := stream.Close()
	second := stream.Close()
	if first != second {
		t.Fatalf("Close returned %v then %v; repeated Close must be stable", first, second)
	}

	// Closing the stream must not take the connection with it: the Conn is
	// shared with tunnels and (via Session) the terminal.
	select {
	case <-conn.Done():
		t.Fatal("closing the SFTP stream closed the whole connection")
	default:
	}
}
