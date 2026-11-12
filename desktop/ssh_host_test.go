package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"net"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

// startSSHTestServer starts an in-memory ssh server accepting user="u" /
// password="pw"; the remote shell echoes bytes back. Returns the listen
// address and host public key. Shared by ssh_host / app_ssh tests.
func startSSHTestServer(t *testing.T) (addr string, hostPub ssh.PublicKey) {
	t.Helper()
	hostKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "u" && string(pass) == "pw" {
				return &ssh.Permissions{}, nil
			}
			return nil, io.EOF
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go sshServeConn(nc, cfg)
		}
	}()
	return ln.Addr().String(), signer.PublicKey()
}

func sshServeConn(nc net.Conn, cfg *ssh.ServerConfig) {
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
		go func() {
			for r := range chReqs {
				if r.WantReply {
					_ = r.Reply(r.Type == "pty-req" || r.Type == "shell" || r.Type == "window-change", nil)
				}
			}
		}()
		go func() { _, _ = io.Copy(ch, ch); ch.Close() }()
	}
}

func testFixedHostKeyCb(pub ssh.PublicKey) ssh.HostKeyCallback {
	return ssh.FixedHostKey(pub)
}

func TestOpenSSHSessionAdoptsAndCloses(t *testing.T) {
	addr, hostPub := startSSHTestServer(t)
	host, port, _ := net.SplitHostPort(addr)

	h := newTestRelayHost(t)

	id, err := h.OpenSSHSession(context.Background(), SSHConnectReq{
		Host: host, Port: port, User: "u",
		AuthKind: "password", Password: "pw",
		Cols: 80, Rows: 24,
	}, testFixedHostKeyCb(hostPub), nil)
	if err != nil {
		t.Fatalf("OpenSSHSession: %v", err)
	}
	if id == uuid.Nil {
		t.Fatal("nil session id")
	}

	h.mu.Lock()
	_, ok := h.sessions[id]
	h.mu.Unlock()
	if !ok {
		t.Fatal("session not registered")
	}

	if err := h.CloseSession(id); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
}

func TestOpenSSHSessionRecordsOnlyEchoConfirmedClientInput(t *testing.T) {
	addr, hostPub := startSSHTestServer(t)
	host, port, _ := net.SplitHostPort(addr)
	h := newTestRelayHost(t)

	id, err := h.OpenSSHSession(context.Background(), SSHConnectReq{
		Host: host, Port: port, User: "u", AuthKind: "password", Password: "pw",
		StartupCommand: "automatic-startup", StartupDelayMs: 100,
	}, testFixedHostKeyCb(hostPub), nil)
	if err != nil {
		t.Fatalf("OpenSSHSession: %v", err)
	}
	t.Cleanup(func() { _ = h.CloseSession(id) })
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		t.Fatal("session not registered")
	}
	if !sess.SendInbound(proto.Frame{Type: proto.TypeIn, SessionID: id, Payload: []byte("docker compose ps\r")}) {
		t.Fatal("client input was not accepted")
	}

	deadline := time.Now().Add(2 * time.Second)
	recorded := false
	for time.Now().Before(deadline) {
		snapshot, err := h.sessionInputHistory(id)
		if err == nil && len(snapshot.Items) == 1 {
			if snapshot.Items[0].Text != "docker compose ps" {
				t.Fatalf("history = %#v", snapshot.Items)
			}
			recorded = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !recorded {
		snapshot, _ := h.sessionInputHistory(id)
		t.Fatalf("echo-confirmed client input not recorded; history=%#v", snapshot.Items)
	}
	// The configured startup command writes directly to the SSH PTY, not
	// through Session.SendInbound. Give it enough time to echo and prove it did
	// not become a second user-history entry.
	time.Sleep(250 * time.Millisecond)
	snapshot, _ := h.sessionInputHistory(id)
	if len(snapshot.Items) != 1 {
		t.Fatalf("startup command leaked into history: %#v", snapshot.Items)
	}
}
