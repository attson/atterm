# SSH 连接内核(切片 1)Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 atterm 内建 Go SSH 客户端,把远程 shell 作为一种新的 PTY 来源经 `AdoptSession` 接入现有接管 + E2EE 管线,凭据用完即弃、不落盘。

**Architecture:** 新增 `internal/sshclient`(纯 SSH 协议封装,不依赖 desktop)提供满足 `Read/Write/Resize/Close` 的 `Session`;`desktop/ssh_host.go` 把它适配成 `relay.PtyHost` 并 `AdoptSession`。为让 SSH 会话复用现有 `relayHost.sessions` map 与 `CloseSession`,把 `activeSession.host` 从具体的 `*ptyhost.Host` 抽象为一个最小接口 `sessionPTY`。不新增任何 proto 帧类型。

**Tech Stack:** Go 1.23,`golang.org/x/crypto/ssh` v0.37.0(已在依赖),`golang.org/x/crypto/ssh/knownhosts`,Wails v2 binding,Vue 3 + TS 前端。

---

## File Structure

**新建:**
- `internal/sshclient/sshclient.go` — `Config` / `AuthMethod`(`PasswordAuth`/`PrivateKeyAuth`)/ `Session`(Dial/Read/Write/Resize/Wait/Close/keepalive)
- `internal/sshclient/sshclient_test.go` — 用内存 ssh server 的单测
- `internal/sshclient/knownhosts.go` — `KnownHostsCallback`(TOFU:未知回调、不匹配拒绝、接受后写盘)
- `internal/sshclient/knownhosts_test.go`
- `desktop/ssh_host.go` — `sshPtyHost` 适配 + `relayHost.OpenSSHSession`
- `desktop/ssh_host_test.go`
- `desktop/frontend/src/components/NewSshDialog.vue` — 最小新建 SSH 表单 + 指纹确认

**修改:**
- `desktop/relay_host.go` — 抽象 `activeSession.host` 类型为接口 `sessionPTY`;`OpenSSHSession` 注册进 `sessions`
- `desktop/app.go` — 新增 `SSHConnectReq` 类型与 `NewSshSession` binding + 错误码常量
- `desktop/paste_image.go` — `desktopPtyHost` 已满足接口,无需改(仅确认)
- 前端会话新建入口挂 `NewSshDialog`(具体入口文件在 Task 8 定位)

---

## Task 1: 抽象 `activeSession.host` 为接口(解耦具体 PTY 类型)

**Files:**
- Modify: `desktop/relay_host.go`(`activeSession` 结构 337-341;引用点 620/636/641/671/691/931)

**背景:** `activeSession.host` 现为具体 `*ptyhost.Host`,被 `Resize`/`Close` 使用。SSH 会话不是该类型,需先抽象出最小接口才能复用 `sessions` map 与 `CloseSession`。此任务是纯重构,不改行为。

- [ ] **Step 1: 定义接口并改字段类型**

在 `desktop/relay_host.go` 的 `activeSession` 定义上方新增接口,并把字段类型改为接口:

```go
// sessionPTY is the minimal contract activeSession needs from whatever
// backs a session — a local PTY (*ptyhost.Host) or an SSH remote shell
// (*sshclient.Session). Both satisfy it. Resize's signature matches
// *ptyhost.Host so the local path is unchanged.
type sessionPTY interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(cols, rows uint16) error
	Close() error
}

type activeSession struct {
	host     sessionPTY
	cleanup  func()
	restored bool
}
```

- [ ] **Step 2: 编译验证重构不破坏本地路径**

`*ptyhost.Host` 已有 `Read/Write/Resize/Close`(见 `internal/ptyhost/ptyhost.go` 76-132),自动满足接口。`watchCwd(id, pty, ...)` 等仍收具体 `*ptyhost.Host`(局部变量 `pty` 类型不变),不受影响。

Run: `cd /home/attson/GolandProjects/atterm/.claude/worktrees/ssh-slice1 && go build ./desktop/...`
Expected: 编译通过,无错误。

- [ ] **Step 3: 跑现有会话相关测试确认无回归**

Run: `go test ./desktop/ -run 'TestApp.*Session|TestRelayHost' -count=1`
Expected: PASS(与重构前一致)。

- [ ] **Step 4: Commit**

```bash
git add desktop/relay_host.go
git commit -m "refactor(desktop): activeSession.host 抽象为 sessionPTY 接口

为 SSH 会话复用 sessions map / CloseSession 做准备,纯重构不改行为。

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: `sshclient` — 密码认证 Dial 打通(用内存 ssh server 测)

**Files:**
- Create: `internal/sshclient/sshclient.go`
- Test: `internal/sshclient/sshclient_test.go`

- [ ] **Step 1: 写失败测试(内存 ssh server + 密码认证)**

`internal/sshclient/sshclient_test.go`:

```go
package sshclient

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// startTestServer 起一个只接受 password="pw"、user="u" 的内存 ssh server,
// 远程 shell 行为:把收到的每个字节回显(echo)。返回监听地址与 host public key。
func startTestServer(t *testing.T) (addr string, hostPub ssh.PublicKey) {
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
			newCh.Reject(ssh.UnknownChannelType, "only session")
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			return
		}
		go func() {
			for r := range chReqs {
				// 接受 pty-req / shell / window-change
				if r.WantReply {
					r.Reply(r.Type == "pty-req" || r.Type == "shell" || r.Type == "window-change", nil)
				}
			}
		}()
		go func() { io.Copy(ch, ch); ch.Close() }() // echo
	}
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
```

- [ ] **Step 2: 运行,确认因 sshclient.go 不存在而失败**

Run: `go test ./internal/sshclient/ -run TestDialPasswordEchoRoundTrip -v`
Expected: FAIL(`undefined: Dial` / `undefined: Config` / `undefined: PasswordAuth`)。

- [ ] **Step 3: 写最小实现**

`internal/sshclient/sshclient.go`:

```go
// Package sshclient wraps golang.org/x/crypto/ssh to open a remote shell
// as a PTY-like stream (Read/Write/Resize/Close), so the desktop app can
// adopt it as a session. It does not depend on any desktop code.
package sshclient

import (
	"context"
	"fmt"
	"io"
	"net"
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
	sess.Stderr = writerToStdout{stdout} // fold stderr into the same stream
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

// writerToStdout lets us point Stderr at the stdout pipe reader's writer end.
// Since StdoutPipe returns a reader, we instead just leave Stderr default and
// rely on the pty merging streams; this type is a no-op placeholder removed
// below.
type writerToStdout struct{ io.Reader }

func (s *Session) Read(p []byte) (int, error)  { return s.stdout.Read(p) }
func (s *Session) Write(p []byte) (int, error) { return s.stdin.Write(p) }

func (s *Session) Resize(cols, rows uint16) error {
	return s.sess.WindowChange(int(rows), int(cols))
}

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
			_, _, err := s.client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				s.Close()
				return
			}
		}
	}
}
```

**注意:** 上面 `writerToStdout` / `sess.Stderr = ...` 这段是错的占位——`RequestPty` 后 stdout 已合并 stderr(pty 会把两者并到一个流)。实现时**删掉 `sess.Stderr = writerToStdout{...}` 那一行和 `writerToStdout` 类型**;pty 模式下 stderr 自动并入 stdout。保留其余。

- [ ] **Step 4: 删除 Step 3 标注的 stderr 占位后运行测试**

Run: `go test ./internal/sshclient/ -run TestDialPasswordEchoRoundTrip -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/sshclient/sshclient.go internal/sshclient/sshclient_test.go
git commit -m "feat(sshclient): 密码认证 Dial + 远程 shell Read/Write/Resize/Close

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: `sshclient` — 私钥认证 + passphrase

**Files:**
- Modify: `internal/sshclient/sshclient.go`(新增 `PrivateKeyAuth`)
- Modify: `internal/sshclient/sshclient_test.go`(server 增 publickey 回调 + 私钥测试)

- [ ] **Step 1: 写失败测试(私钥认证)**

在 `sshclient_test.go` 追加。先给 `startTestServer` 增加 publickey 支持——改为额外接受一个授权公钥:

```go
func startTestServerWithKey(t *testing.T, authorized ssh.PublicKey) (addr string, hostPub ssh.PublicKey) {
	t.Helper()
	hostKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer, _ := ssh.NewSignerFromKey(hostKey)
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() == "u" && keyMarshalEqual(key, authorized) {
				return &ssh.Permissions{}, nil
			}
			return nil, io.EOF
		},
	}
	cfg.AddHostKey(signer)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
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

func keyMarshalEqual(a, b ssh.PublicKey) bool {
	return string(a.Marshal()) == string(b.Marshal())
}

func TestDialPrivateKey(t *testing.T) {
	clientKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer, _ := ssh.NewSignerFromKey(clientKey)
	pemBytes := marshalPEM(t, clientKey) // helper below

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
```

在 import 里补 `crypto/x509`、`encoding/pem`。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/sshclient/ -run TestDialPrivateKey -v`
Expected: FAIL(`undefined: PrivateKeyAuth`)。

- [ ] **Step 3: 实现 PrivateKeyAuth**

在 `sshclient.go` 追加:

```go
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
```

- [ ] **Step 4: 运行测试通过**

Run: `go test ./internal/sshclient/ -run TestDialPrivateKey -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/sshclient/
git commit -m "feat(sshclient): 私钥认证(含 passphrase)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: `sshclient` — known_hosts TOFU 回调

**Files:**
- Create: `internal/sshclient/knownhosts.go`
- Test: `internal/sshclient/knownhosts_test.go`

**背景:** `golang.org/x/crypto/ssh/knownhosts.New(path)` 返回一个 `HostKeyCallback`;当主机不在库里,它返回 `*knownhosts.KeyError`,其 `Want` 字段为空表示「未知」,非空表示「已有记录但不匹配」(疑似 MITM)。据此区分 TOFU 与拒绝。

- [ ] **Step 1: 写失败测试**

`internal/sshclient/knownhosts_test.go`:

```go
package sshclient

import (
	"crypto/rand"
	"crypto/rsa"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func testHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	s, _ := ssh.NewSignerFromKey(k)
	return s.PublicKey()
}

func TestKnownHostsUnknownTriggersTOFUAndPersists(t *testing.T) {
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(khPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	pub := testHostKey(t)
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2222}

	var askedFingerprint string
	cb := KnownHostsCallback(khPath, func(host, fp string) bool {
		askedFingerprint = fp
		return true // accept
	})

	// 首次:未知 → 触发 onUnknown → accept → 写盘,返回 nil error
	if err := cb("127.0.0.1:2222", addr, pub); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	if askedFingerprint == "" {
		t.Fatal("onUnknown not called")
	}

	// 第二次:已写盘 → 不再询问,直接放行
	askedFingerprint = ""
	cb2 := KnownHostsCallback(khPath, func(host, fp string) bool {
		t.Fatal("should not ask again")
		return false
	})
	if err := cb2("127.0.0.1:2222", addr, pub); err != nil {
		t.Fatalf("second connect: %v", err)
	}
	_ = cb2
}

func TestKnownHostsMismatchRejected(t *testing.T) {
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	pub1 := testHostKey(t)
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2222}

	// 先写入 pub1
	accept := KnownHostsCallback(khPath, func(string, string) bool { return true })
	if err := accept("127.0.0.1:2222", addr, pub1); err != nil {
		t.Fatal(err)
	}

	// 换成 pub2 → 不匹配 → 即使 onUnknown 返回 true 也必须拒绝
	pub2 := testHostKey(t)
	cb := KnownHostsCallback(khPath, func(string, string) bool { return true })
	if err := cb("127.0.0.1:2222", addr, pub2); err == nil {
		t.Fatal("mismatch must be rejected, got nil error")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/sshclient/ -run TestKnownHosts -v`
Expected: FAIL(`undefined: KnownHostsCallback`)。

- [ ] **Step 3: 实现 KnownHostsCallback**

`internal/sshclient/knownhosts.go`:

```go
package sshclient

import (
	"errors"
	"fmt"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// KnownHostsCallback returns an ssh.HostKeyCallback backed by the known_hosts
// file at path. On an unknown host it calls onUnknown(host, fingerprint); if
// that returns true (TOFU accept) the key is appended to the file and the
// connection is allowed. A key that is present but differs (possible MITM)
// is always rejected, regardless of onUnknown.
func KnownHostsCallback(path string, onUnknown func(host, fingerprint string) bool) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		base, err := knownhosts.New(path)
		if err != nil {
			// Missing file is fine — treat as empty (everything unknown).
			if !os.IsNotExist(err) {
				return fmt.Errorf("sshclient: load known_hosts: %w", err)
			}
			return handleUnknown(path, hostname, key, onUnknown)
		}
		err = base(hostname, remote, key)
		if err == nil {
			return nil // known & matches
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			if len(keyErr.Want) == 0 {
				return handleUnknown(path, hostname, key, onUnknown)
			}
			// Want non-empty → recorded but different → possible MITM.
			return fmt.Errorf("sshclient: host key mismatch for %s (possible MITM)", hostname)
		}
		return err
	}
}

func handleUnknown(path, hostname string, key ssh.PublicKey, onUnknown func(string, string) bool) error {
	fp := ssh.FingerprintSHA256(key)
	if onUnknown == nil || !onUnknown(hostname, fp) {
		return fmt.Errorf("sshclient: host key not accepted for %s", hostname)
	}
	if err := appendKnownHost(path, hostname, key); err != nil {
		return fmt.Errorf("sshclient: persist host key: %w", err)
	}
	return nil
}

func appendKnownHost(path, hostname string, key ssh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	_, err = f.WriteString(line + "\n")
	return err
}
```

- [ ] **Step 4: 运行测试通过**

Run: `go test ./internal/sshclient/ -run TestKnownHosts -v`
Expected: PASS(两个用例)。

- [ ] **Step 5: 全包测试**

Run: `go test ./internal/sshclient/ -count=1`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/sshclient/knownhosts.go internal/sshclient/knownhosts_test.go
git commit -m "feat(sshclient): known_hosts TOFU 回调(未知询问/不匹配拒绝/接受写盘)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: `desktop/ssh_host.go` — 接入 relayHost + AdoptSession

**Files:**
- Create: `desktop/ssh_host.go`
- Test: `desktop/ssh_host_test.go`
- Modify: `desktop/app.go`(新增 `SSHConnectReq` + 错误码常量)

- [ ] **Step 1: 定义请求类型与错误码(app.go)**

在 `desktop/app.go` 靠近 `NewSessionReq` 处新增:

```go
// SSHConnectReq describes one SSH connection request from the frontend.
// Credentials are used for this connection only and never persisted (slice 1).
type SSHConnectReq struct {
	Host       string `json:"host"`
	Port       string `json:"port,omitempty"`
	User       string `json:"user"`
	AuthKind   string `json:"auth_kind"` // "password" | "privateKey"
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"` // PEM content (pasted or file-read)
	Passphrase string `json:"passphrase,omitempty"`
	Cols       uint16 `json:"cols,omitempty"`
	Rows       uint16 `json:"rows,omitempty"`
	// AcceptHostKey is set on a retry after the user confirmed an unknown
	// host fingerprint in the TOFU dialog.
	AcceptHostKey bool `json:"accept_host_key,omitempty"`
}

// SSH connect error codes returned (as error strings) to the frontend.
const (
	errCodeHostKeyUnknown  = "ssh_host_key_unknown"
	errCodeHostKeyMismatch = "ssh_host_key_mismatch"
)

// HostKeyUnknownError carries the fingerprint so the frontend can show the
// TOFU dialog and retry with AcceptHostKey=true.
type HostKeyUnknownError struct {
	Fingerprint string
	Host        string
}

func (e *HostKeyUnknownError) Error() string { return errCodeHostKeyUnknown }
```

- [ ] **Step 2: 写失败测试(ssh_host_test.go)**

用 Task 2 的内存 ssh server 复用一个本地 helper(测试内自带,不 import sshclient 的测试文件)。测试验证:Dial 成功后 `OpenSSHSession` 调用了 `AdoptSession` 并把会话登记进 `sessions`,`CloseSession` 能关闭它。

```go
package main

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOpenSSHSessionAdoptsAndCloses(t *testing.T) {
	addr, hostPub := startSSHTestServer(t) // helper defined in this test file
	host, port, _ := net.SplitHostPort(addr)

	h := newTestRelayHost(t) // existing helper pattern; see app_shells_test.go
	defer h.Stop()

	id, err := h.OpenSSHSession(context.Background(), SSHConnectReq{
		Host: host, Port: port, User: "u",
		AuthKind: "password", Password: "pw",
		Cols: 80, Rows: 24, AcceptHostKey: true,
	}, testFixedHostKeyCb(hostPub))
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
```

> **实现者注:** `OpenSSHSession` 的签名比生产多一个 `hostKeyCb ssh.HostKeyCallback` 尾参,便于测试注入 `ssh.FixedHostKey`。生产 binding(Task 6)传入基于 `~/.ssh/known_hosts` 的真实回调。`startSSHTestServer`/`testFixedHostKeyCb`/`newTestRelayHost` 三个 helper:前两个照抄 Task 2 的 `startTestServer` 逻辑(放本测试文件),`newTestRelayHost` 复用现有测试里构造 relayHost 的方式(参考 `desktop/app_shells_test.go` / `relay_host_test.go` 里已有的构造 helper;若已有 `newTestRelayHost` 直接用)。

- [ ] **Step 3: 运行确认失败**

Run: `go test ./desktop/ -run TestOpenSSHSessionAdoptsAndCloses -v`
Expected: FAIL(`undefined: h.OpenSSHSession`)。

- [ ] **Step 4: 实现 ssh_host.go**

```go
package main

import (
	"context"
	"strings"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/sshclient"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

// sshPtyHost adapts *sshclient.Session to relay.PtyHost + sessionPTY.
type sshPtyHost struct{ *sshclient.Session }

// OpenSSHSession dials an SSH host, opens a remote shell, and adopts it as a
// relay session so it flows through the same takeover + E2EE pipeline as a
// local shell. hostKeyCb is injected (real known_hosts callback in prod,
// FixedHostKey in tests).
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
	port := req.Port
	if port == "" {
		port = "22"
	}
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
	_ = strings.TrimSpace // keep import tidy if unused; remove if lint complains

	host := &sshPtyHost{Session: sess}
	cleanup := h.server.AdoptSession(ctx, id, info, host, h.adminUserID)

	h.mu.Lock()
	if h.sessions == nil {
		h.mu.Unlock()
		cleanup()
		_ = sess.Close()
		return uuid.Nil, errRelayStopped
	}
	h.sessions[id] = &activeSession{host: host, cleanup: cleanup}
	h.mu.Unlock()
	h.notifyChange()

	// Watch for remote shell exit / disconnect → clean up the session.
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
```

> **实现者注:**
> - `errRelayStopped`:若 `relay_host.go` 已有等价 sentinel(如 `NewSession` 里用的 `fmt.Errorf("relay host stopped")`)则改用 `fmt.Errorf(...)` 直接返回,不必新建常量;保持与本地路径一致即可。
> - 删掉 `_ = strings.TrimSpace` 占位与 `strings` import(仅为提示保持 import 整洁)。`sshPtyHost` 内嵌 `*sshclient.Session` 已满足 `Read/Write/Resize/Close`(sessionPTY)与 relay.PtyHost。

- [ ] **Step 5: 运行测试通过**

Run: `go test ./desktop/ -run TestOpenSSHSessionAdoptsAndCloses -v`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add desktop/ssh_host.go desktop/ssh_host_test.go desktop/app.go
git commit -m "feat(desktop): OpenSSHSession 经 AdoptSession 接入 SSH 远程 shell

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: `NewSshSession` binding + 真实 known_hosts 回调 + TOFU 重试

**Files:**
- Modify: `desktop/app.go`(新增 `NewSshSession`)
- Modify: `desktop/ssh_host.go`(生产 known_hosts 回调构造)
- Test: `desktop/app_ssh_test.go`

- [ ] **Step 1: 写失败测试(未知主机返回 HostKeyUnknownError,携带指纹)**

`desktop/app_ssh_test.go`:

```go
package main

import (
	"errors"
	"net"
	"testing"
)

func TestNewSshSessionUnknownHostReturnsFingerprint(t *testing.T) {
	addr, _ := startSSHTestServer(t) // reuse helper from ssh_host_test.go
	host, port, _ := net.SplitHostPort(addr)

	a := newTestApp(t) // existing app test helper
	// point known_hosts at an empty temp file so the host is unknown
	a.sshKnownHostsPath = filepath.Join(t.TempDir(), "known_hosts")

	_, err := a.NewSshSession(SSHConnectReq{
		Host: host, Port: port, User: "u",
		AuthKind: "password", Password: "pw",
		AcceptHostKey: false,
	})
	var hkErr *HostKeyUnknownError
	if !errors.As(err, &hkErr) {
		t.Fatalf("expected HostKeyUnknownError, got %v", err)
	}
	if hkErr.Fingerprint == "" {
		t.Fatal("empty fingerprint")
	}
}
```

> **实现者注:** `a.sshKnownHostsPath` 是新增的可覆盖字段(默认 `~/.ssh/known_hosts`),便于测试隔离。`newTestApp` 用现有 app 测试的构造方式(参考 `app_test.go` / `app_test_helpers_test.go`)。import 补 `path/filepath`。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./desktop/ -run TestNewSshSessionUnknownHostReturnsFingerprint -v`
Expected: FAIL(`undefined: NewSshSession` / `sshKnownHostsPath`)。

- [ ] **Step 3: 实现 binding + 生产回调**

在 `desktop/app.go` App 结构体加字段(靠近其它路径配置处):

```go
	// sshKnownHostsPath overrides the known_hosts file (tests set a temp
	// path). Empty → ~/.ssh/known_hosts.
	sshKnownHostsPath string
```

新增 binding:

```go
// NewSshSession opens an SSH remote shell as an adoptable session. On an
// unknown host key it returns *HostKeyUnknownError with the fingerprint; the
// frontend shows a TOFU dialog and retries with AcceptHostKey=true.
func (a *App) NewSshSession(req SSHConnectReq) (NewSessionResp, error) {
	if a.host == nil {
		return NewSessionResp{}, fmt.Errorf("relay host not ready")
	}
	khPath := a.sshKnownHostsPath
	if khPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			khPath = filepath.Join(home, ".ssh", "known_hosts")
		}
	}

	var unknown *HostKeyUnknownError
	cb := sshclient.KnownHostsCallback(khPath, func(host, fp string) bool {
		if req.AcceptHostKey {
			return true // user already confirmed in the TOFU dialog
		}
		unknown = &HostKeyUnknownError{Fingerprint: fp, Host: host}
		return false
	})

	id, err := a.host.OpenSSHSession(a.ctx, req, cb)
	if err != nil {
		if unknown != nil {
			return NewSessionResp{}, unknown // typed → frontend shows fingerprint
		}
		return NewSessionResp{}, err
	}
	return NewSessionResp{SessionID: id.String()}, nil
}
```

在 `desktop/app.go` import 补 `os` / `path/filepath` / `github.com/attson/atterm/internal/sshclient`(若尚未引入)。

- [ ] **Step 4: 运行测试通过**

Run: `go test ./desktop/ -run TestNewSshSessionUnknownHostReturnsFingerprint -v`
Expected: PASS。

- [ ] **Step 5: 加一个 TOFU 重试成功用例**

在 `app_ssh_test.go` 追加:AcceptHostKey=true 时应连上并返回 session id、known_hosts 被写入。

```go
func TestNewSshSessionAcceptHostKeyConnects(t *testing.T) {
	addr, _ := startSSHTestServer(t)
	host, port, _ := net.SplitHostPort(addr)
	a := newTestApp(t)
	a.sshKnownHostsPath = filepath.Join(t.TempDir(), "known_hosts")

	resp, err := a.NewSshSession(SSHConnectReq{
		Host: host, Port: port, User: "u",
		AuthKind: "password", Password: "pw",
		AcceptHostKey: true,
	})
	if err != nil {
		t.Fatalf("NewSshSession: %v", err)
	}
	if resp.SessionID == "" {
		t.Fatal("empty session id")
	}
	data, _ := os.ReadFile(a.sshKnownHostsPath)
	if len(data) == 0 {
		t.Fatal("known_hosts not written")
	}
}
```

Run: `go test ./desktop/ -run 'TestNewSshSession' -v`
Expected: 两个用例 PASS。

- [ ] **Step 6: 全 desktop 包测试确认无回归**

Run: `go test ./desktop/ -count=1`
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
git add desktop/app.go desktop/ssh_host.go desktop/app_ssh_test.go
git commit -m "feat(desktop): NewSshSession binding + 真实 known_hosts 回调 + TOFU 重试

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: 认证失败错误分类(密码错 / 私钥无效)

**Files:**
- Modify: `internal/sshclient/sshclient.go`(可选:包装认证错误)
- Test: `internal/sshclient/sshclient_test.go`

- [ ] **Step 1: 写失败测试(密码错 → 明确认证错误)**

```go
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
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/sshclient/ -run TestDialWrongPassword -v`
Expected: FAIL(`undefined: IsAuthError`)。

- [ ] **Step 3: 实现 IsAuthError**

在 `sshclient.go` 追加。crypto/ssh 认证失败返回的错误消息含 "unable to authenticate" / "ssh: handshake failed"。用字符串匹配分类(crypto/ssh 未导出结构化认证错误类型):

```go
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
```

在 import 补 `strings`。

- [ ] **Step 4: 运行测试通过**

Run: `go test ./internal/sshclient/ -run TestDialWrongPassword -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/sshclient/
git commit -m "feat(sshclient): IsAuthError 区分认证失败与网络错误

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: 前端最小新建 SSH 对话框 + TOFU 确认

**Files:**
- Create: `desktop/frontend/src/components/NewSshDialog.vue`
- Test: `desktop/frontend/src/components/NewSshDialog.test.ts`
- Modify: 会话新建入口(定位后挂载)

- [ ] **Step 1: 定位现有「新建会话」入口**

Run: `cd desktop/frontend && grep -rn "NewSession\|newSession\|new-session\|新建" src/ --include=*.vue --include=*.ts | grep -iv test | head -20`
Expected: 找到触发 `NewSession` binding 的按钮/菜单(记下文件与函数),SSH 入口挂在同处旁边。

- [ ] **Step 2: 写失败测试(表单提交调用 NewSshSession;未知主机弹指纹确认)**

`NewSshDialog.test.ts`(参考现有 `*.test.ts` 里对 wailsjs 的 mock 方式,如 `ConfirmInstallDialog.test.ts`):

```ts
import { describe, it, expect, vi, beforeEach } from "vitest";
import { mount } from "@vue/test-utils";
import NewSshDialog from "./NewSshDialog.vue";

const newSshSession = vi.fn();
vi.mock("../../wailsjs/go/main/App", () => ({
  NewSshSession: (...a: unknown[]) => newSshSession(...a),
}));

beforeEach(() => newSshSession.mockReset());

describe("NewSshDialog", () => {
  it("提交时用表单字段调用 NewSshSession", async () => {
    newSshSession.mockResolvedValue({ session_id: "s1" });
    const wrapper = mount(NewSshDialog, { props: { open: true } });
    await wrapper.find('[data-test="ssh-host"]').setValue("h");
    await wrapper.find('[data-test="ssh-user"]').setValue("u");
    await wrapper.find('[data-test="ssh-password"]').setValue("pw");
    await wrapper.find('[data-test="ssh-connect"]').trigger("click");
    expect(newSshSession).toHaveBeenCalledWith(
      expect.objectContaining({ host: "h", user: "u", password: "pw", auth_kind: "password" }),
    );
  });

  it("未知主机错误时展示指纹确认,确认后带 accept_host_key 重试", async () => {
    newSshSession
      .mockRejectedValueOnce({ Fingerprint: "SHA256:abc", Host: "h" })
      .mockResolvedValueOnce({ session_id: "s1" });
    const wrapper = mount(NewSshDialog, { props: { open: true } });
    await wrapper.find('[data-test="ssh-host"]').setValue("h");
    await wrapper.find('[data-test="ssh-user"]').setValue("u");
    await wrapper.find('[data-test="ssh-password"]').setValue("pw");
    await wrapper.find('[data-test="ssh-connect"]').trigger("click");
    await wrapper.vm.$nextTick();
    expect(wrapper.text()).toContain("SHA256:abc");
    await wrapper.find('[data-test="ssh-accept-hostkey"]').trigger("click");
    expect(newSshSession).toHaveBeenLastCalledWith(
      expect.objectContaining({ accept_host_key: true }),
    );
  });
});
```

- [ ] **Step 3: 运行确认失败**

Run: `cd desktop/frontend && npx vitest run src/components/NewSshDialog.test.ts`
Expected: FAIL(组件不存在)。

- [ ] **Step 4: 实现 NewSshDialog.vue(最小)**

字段:host / port(默认 22)/ user / auth 方式(password | privateKey)/ 密码 或 私钥(textarea 粘贴,file 选择可后置)/ passphrase。提交调用 `NewSshSession`;捕获带 `Fingerprint` 的错误 → 显示指纹 + 「接受并连接」按钮 → 带 `accept_host_key: true` 重连;成功后 emit 关闭并让父组件打开该 session tab。样式参考现有 dialog 组件(如 `ConfirmInstallDialog.vue` / `SessionPickerDialog.vue`),遵守 `docs/spec/component-style.md`。所有交互元素带 `data-test` 属性以匹配测试。

> 完整组件代码在实现时按现有 dialog 组件的结构编写(props `open`、emit `close`/`opened`,i18n key 走现有 `i18n` 机制)。关键行为契约由 Step 2 测试固定:①提交用正确字段调 `NewSshSession`;②未知主机显示指纹并支持带 `accept_host_key` 重试。

- [ ] **Step 5: 运行测试通过**

Run: `cd desktop/frontend && npx vitest run src/components/NewSshDialog.test.ts`
Expected: PASS(两个用例)。

- [ ] **Step 6: 挂载入口 + 前端全量测试**

在 Step 1 定位的入口旁加「新建 SSH 连接」触发,打开本对话框。

Run: `cd desktop/frontend && npx vitest run`
Expected: PASS(无回归)。

- [ ] **Step 7: Commit**

```bash
git add desktop/frontend/src/components/NewSshDialog.vue desktop/frontend/src/components/NewSshDialog.test.ts
git commit -m "feat(frontend): 最小新建 SSH 对话框 + known_hosts TOFU 指纹确认

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: 端到端手动验证 + 收尾

**Files:** 无(验证 + 文档)

- [ ] **Step 1: 全量 Go 测试**

Run: `go test ./internal/sshclient/... ./desktop/... -count=1`
Expected: 全 PASS。

- [ ] **Step 2: go vet + 前端类型检查**

Run: `go vet ./internal/sshclient/... ./desktop/... && cd desktop/frontend && npx vue-tsc --noEmit`
Expected: 无错误。

- [ ] **Step 3: 手动冒烟(需一台真实/本地 SSH 可达主机)**

`wails dev` 起桌面端(见 README),用「新建 SSH 连接」连一台真实主机:
1. 首次连 → 出现指纹确认框 → 接受 → 终端可用、可输入。
2. 关掉再连同一台 → 不再弹指纹(已写入 known_hosts)。
3. 用手机/浏览器客户端 attach 该 SSH 会话 → 能看到输出并接管输入(验证 AdoptSession 链路)。
4. 密码错 → 明确认证失败提示;host 填错 → 连接超时/不可达提示。

- [ ] **Step 4: 更新 AGENTS.md 的 v0.3.x 主线新增段(一句话)**

在 `AGENTS.md` 相应段落追加一句:内建 SSH 连接内核(切片 1)——SSH 远程 shell 经 `AdoptSession` 接入接管管线,`internal/sshclient` + `desktop/ssh_host.go`,凭据用完即弃;设计见本 plan 对应 spec。

- [ ] **Step 5: 最终 commit**

```bash
git add AGENTS.md
git commit -m "docs: AGENTS.md 记录 SSH 连接内核(切片1)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**
- SSH 连接 + 密码/私钥认证 → Task 2/3 ✅
- known_hosts TOFU + 不匹配拒绝 → Task 4 ✅
- 作为可接管会话经 AdoptSession → Task 5 ✅
- 用完即弃不落盘 → 全程无持久化(仅 known_hosts 写盘,属安全机制)✅
- 连接超时 + keepalive → Task 2(Config.Timeout / keepalive goroutine)✅
- 错误分类回传 → Task 6(HostKeyUnknown/Mismatch)+ Task 7(IsAuthError)✅
- 最小 UI + TOFU 确认 → Task 8 ✅
- 不新增 proto 帧 / internal 不依赖 desktop → 架构满足(sshclient 无 desktop import)✅

**Placeholder scan:** Task 2 的 `writerToStdout` 与 Task 5 的 `strings.TrimSpace`/`errRelayStopped` 均已在「实现者注」中明确标注为删除/替换项,非隐藏占位。

**Type consistency:** `SSHConnectReq`(app.go 定义,Task 5/6 使用)、`OpenSSHSession(ctx, req, cb)` 三参签名(Task 5 定义、Task 6 调用一致)、`HostKeyUnknownError{Fingerprint,Host}`(Task 5 定义、Task 6/8 使用一致)、`sessionPTY` 接口(Task 1 定义、Task 5 用)、`KnownHostsCallback(path, onUnknown)`(Task 4 定义、Task 6 用)—— 签名前后一致。
