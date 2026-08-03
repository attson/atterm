package sshclient

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// startTestServer starts an in-memory ssh server that only accepts
// user="u" / password="pw". The remote "shell" echoes every byte back.
// It returns the listen address and the server host public key.
func startTestServer(t *testing.T) (addr string, hostPub ssh.PublicKey) {
	t.Helper()
	return startTestServerCfg(t, &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "u" && string(pass) == "pw" {
				return &ssh.Permissions{}, nil
			}
			return nil, io.EOF
		},
	})
}

// startTestServerWithKey starts a server that accepts user="u" authenticating
// with the given authorized public key.
func startTestServerWithKey(t *testing.T, authorized ssh.PublicKey) (addr string, hostPub ssh.PublicKey) {
	t.Helper()
	return startTestServerCfg(t, &ssh.ServerConfig{
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() == "u" && keyMarshalEqual(key, authorized) {
				return &ssh.Permissions{}, nil
			}
			return nil, io.EOF
		},
	})
}

func startTestServerCfg(t *testing.T, cfg *ssh.ServerConfig) (addr string, hostPub ssh.PublicKey) {
	t.Helper()
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

	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go serveConn(nc, cfg)
		}
	}()
	return ln.Addr().String(), signer.PublicKey()
}

func serveConn(nc net.Conn, cfg *ssh.ServerConfig) {
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
		go func() { _, _ = io.Copy(ch, ch); ch.Close() }() // echo
	}
}

func keyMarshalEqual(a, b ssh.PublicKey) bool {
	return string(a.Marshal()) == string(b.Marshal())
}

func TestDialPasswordEchoRoundTrip(t *testing.T) {
	addr, hostPub := startTestServer(t)
	host, port, _ := net.SplitHostPort(addr)

	sess, err := Dial(context.Background(), Config{
		Host: host, Port: port, User: "u",
		Auth:      PasswordAuth{Password: "pw"},
		HostKeyCb: ssh.FixedHostKey(hostPub),
		Cols:      80, Rows: 24,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer sess.Close()

	if _, err := sess.Write([]byte("ping")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(sess, buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf) != "ping" {
		t.Fatalf("echo mismatch: %q", buf)
	}
}

func TestDialPrivateKey(t *testing.T) {
	clientKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer, _ := ssh.NewSignerFromKey(clientKey)
	pemBytes := marshalPEM(t, clientKey)

	addr, hostPub := startTestServerWithKey(t, signer.PublicKey())
	host, port, _ := net.SplitHostPort(addr)

	sess, err := Dial(context.Background(), Config{
		Host: host, Port: port, User: "u",
		Auth:      PrivateKeyAuth{PEM: pemBytes},
		HostKeyCb: ssh.FixedHostKey(hostPub),
		Cols:      80, Rows: 24, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	sess.Close()
}

func marshalPEM(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	der := x509.MarshalPKCS1PrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})
}

func TestDialWrongPasswordAuthError(t *testing.T) {
	addr, hostPub := startTestServer(t)
	host, port, _ := net.SplitHostPort(addr)
	_, err := Dial(context.Background(), Config{
		Host: host, Port: port, User: "u",
		Auth:      PasswordAuth{Password: "WRONG"},
		HostKeyCb: ssh.FixedHostKey(hostPub),
		Timeout:   5 * time.Second,
	})
	if err == nil {
		t.Fatal("expected auth error")
	}
	if !IsAuthError(err) {
		t.Fatalf("expected auth-classified error, got %v", err)
	}
}
