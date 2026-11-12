package sshclient

import (
	"bufio"
	"context"
	"encoding/base64"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHTTPProxyCarriesSSHAndSeparateProxyAuth(t *testing.T) {
	target, pub := startTestServer(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	observed := make(chan *http.Request, 1)
	go func() {
		c, e := listener.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		r, e := http.ReadRequest(bufio.NewReader(c))
		if e != nil {
			return
		}
		observed <- r
		upstream, e := net.Dial("tcp", target)
		if e != nil {
			return
		}
		defer upstream.Close()
		_, _ = io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n")
		go func() { _, _ = io.Copy(upstream, c) }()
		_, _ = io.Copy(c, upstream)
	}()
	host, port, _ := net.SplitHostPort(target)
	ph, pp, _ := net.SplitHostPort(listener.Addr().String())
	conn, err := DialConn(context.Background(), Config{Host: host, Port: port, User: "u", Auth: PasswordAuth{Password: "pw"}, HostKeyCb: ssh.FixedHostKey(pub), Proxy: &ProxyConfig{Kind: "http", Host: ph, Port: pp, Username: "proxy-user", Password: "proxy-pw"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r := <-observed
	if r.Host != target || r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("proxy-user:proxy-pw")) {
		t.Fatal("wrong CONNECT destination or proxy authentication")
	}
}
func TestHTTPProxyPreservesBufferedBytesAndRejectsRefusal(t *testing.T) {
	for _, status := range []string{"200 OK", "407 password-canary"} {
		t.Run(status[:3], func(t *testing.T) {
			l, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer l.Close()
			go func() {
				c, e := l.Accept()
				if e != nil {
					return
				}
				defer c.Close()
				_, _ = http.ReadRequest(bufio.NewReader(c))
				_, _ = io.WriteString(c, "HTTP/1.1 "+status+"\r\n\r\nSSH-banner")
			}()
			host, port, _ := net.SplitHostPort(l.Addr().String())
			p := ProxyConfig{Kind: "http", Host: host, Port: port}
			c, e := p.dial(context.Background(), "example.invalid:22", time.Second)
			if strings.HasPrefix(status, "407") {
				if e == nil || strings.Contains(e.Error(), "password-canary") {
					t.Fatal("proxy refusal ignored or leaked server text")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			b := make([]byte, 10)
			if _, e := io.ReadFull(c, b); e != nil || string(b) != "SSH-banner" {
				t.Fatal("buffered SSH data was lost", e)
			}
		})
	}
}
func TestSOCKS5ProxyUsesRemoteDNS(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	target := make(chan string, 1)
	go func() {
		c, e := l.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		g := make([]byte, 2)
		if _, e = io.ReadFull(c, g); e != nil {
			return
		}
		_, _ = io.CopyN(io.Discard, c, int64(g[1]))
		_, _ = c.Write([]byte{5, 0})
		head := make([]byte, 5)
		if _, e = io.ReadFull(c, head); e != nil {
			return
		}
		if head[3] != 3 {
			target <- "not-domain"
			return
		}
		name := make([]byte, int(head[4])+2)
		_, _ = io.ReadFull(c, name)
		target <- string(name[:len(name)-2])
		_, _ = c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 22})
	}()
	host, port, _ := net.SplitHostPort(l.Addr().String())
	p := ProxyConfig{Kind: "socks5", Host: host, Port: port}
	c, e := p.dial(context.Background(), "unresolvable.invalid:22", time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if got := <-target; got != "unresolvable.invalid" {
		t.Fatal("target was resolved locally", got)
	}
}
func TestProxyTimeoutAndNoDirectFallback(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		c, e := l.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		<-done
	}()
	host, port, _ := net.SplitHostPort(l.Addr().String())
	p := ProxyConfig{Kind: "http", Host: host, Port: port}
	start := time.Now()
	c, e := p.dial(context.Background(), "example.invalid:22", 50*time.Millisecond)
	if e == nil {
		c.Close()
		t.Fatal("stalling proxy succeeded")
	}
	if time.Since(start) > time.Second {
		t.Fatal("proxy timeout unbounded")
	}
	_, e = DialConn(context.Background(), Config{Host: "localhost", Port: "22", User: "u", Auth: PasswordAuth{Password: "pw"}, HostKeyCb: ssh.InsecureIgnoreHostKey(), Proxy: &ProxyConfig{Kind: "unknown"}})
	if e == nil || !strings.Contains(e.Error(), "unsupported") {
		t.Fatal("unknown proxy did not fail closed", e)
	}
}
