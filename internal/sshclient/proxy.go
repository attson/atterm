package sshclient

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// ProxyConfig selects a native network proxy. It never runs shell commands.
// Credentials belong to the caller's credential store, not host metadata.
type ProxyConfig struct {
	Kind, Host, Port, Username string
	Password                   string `json:"-"`
}

func (p ProxyConfig) dial(ctx context.Context, target string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addr := net.JoinHostPort(p.Host, p.Port)
	dialer := &net.Dialer{Timeout: timeout}
	switch p.Kind {
	case "socks5":
		var auth *proxy.Auth
		if p.Username != "" || p.Password != "" {
			auth = &proxy.Auth{User: p.Username, Password: p.Password}
		}
		d, err := proxy.SOCKS5("tcp", addr, auth, dialer)
		if err != nil {
			return nil, errors.New("sshclient: invalid SOCKS5 proxy configuration")
		}
		c, err := d.(proxy.ContextDialer).DialContext(ctx, "tcp", target)
		if err != nil {
			return nil, errors.New("sshclient: SOCKS5 proxy connection failed")
		}
		return c, nil
	case "http":
		c, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, errors.New("sshclient: HTTP proxy connection failed")
		}
		failed := true
		defer func() {
			if failed {
				_ = c.Close()
			}
		}()
		deadline, _ := ctx.Deadline()
		_ = c.SetDeadline(deadline)
		stop := context.AfterFunc(ctx, func() { _ = c.Close() })
		defer stop()
		if strings.ContainsAny(target, "\r\n") {
			return nil, errors.New("sshclient: invalid proxy destination")
		}
		header := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n"
		if p.Username != "" || p.Password != "" {
			header += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(p.Username+":"+p.Password)) + "\r\n"
		}
		if _, err = fmt.Fprint(c, header+"\r\n"); err != nil {
			return nil, errors.New("sshclient: HTTP proxy request failed")
		}
		reader := bufio.NewReader(c)
		resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
		if err != nil {
			return nil, errors.New("sshclient: invalid HTTP proxy response")
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("sshclient: HTTP proxy refused CONNECT (status %d)", resp.StatusCode)
		}
		_ = c.SetDeadline(time.Time{})
		failed = false
		return &bufferedProxyConn{Conn: c, reader: reader}, nil
	default:
		return nil, errors.New("sshclient: unsupported network proxy type")
	}
}

type bufferedProxyConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedProxyConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
