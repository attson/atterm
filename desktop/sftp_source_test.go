package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/attson/atterm/internal/sftpfs"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// --- fakes ------------------------------------------------------------------

// fakeSFTPClient stands in for a live SFTP channel. Everything the browser
// needs from *sftpfs.Client is here, plus a Done channel a test can close to
// reproduce the one thing Task 3 warned about: the executor tearing its own
// channel down when too many operations stall, which is collateral for every
// other caller sharing that client.
type fakeSFTPClient struct {
	id   int
	done chan struct{}

	mu       sync.Mutex
	listings map[string]sftpfs.Listing
	listErr  error
	writeErr error
	writes   []string
	closed   bool
}

func newFakeSFTPClient(id int) *fakeSFTPClient {
	return &fakeSFTPClient{id: id, done: make(chan struct{}), listings: map[string]sftpfs.Listing{}}
}

func (f *fakeSFTPClient) ListDir(ctx context.Context, dir string) (sftpfs.Listing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return sftpfs.Listing{}, f.listErr
	}
	if l, ok := f.listings[dir]; ok {
		return l, nil
	}
	return sftpfs.Listing{Path: dir}, nil
}

func (f *fakeSFTPClient) FileMeta(ctx context.Context, name string) (sftpfs.FileMetaInfo, error) {
	return sftpfs.FileMetaInfo{Path: name}, nil
}

func (f *fakeSFTPClient) ReadChunk(ctx context.Context, name string, offset, length int64) (sftpfs.Chunk, error) {
	return sftpfs.Chunk{Path: name, Offset: offset, EOF: true}, nil
}

func (f *fakeSFTPClient) WriteFile(ctx context.Context, name string, data []byte, expectedModTime int64, createIfMissing bool) (sftpfs.FileMetaInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return sftpfs.FileMetaInfo{}, f.writeErr
	}
	f.writes = append(f.writes, name)
	return sftpfs.FileMetaInfo{Path: name, Size: int64(len(data))}, nil
}

func (f *fakeSFTPClient) CreateFile(ctx context.Context, name string) (sftpfs.FileMetaInfo, error) {
	return sftpfs.FileMetaInfo{Path: name}, nil
}

func (f *fakeSFTPClient) Mkdir(ctx context.Context, name string) (sftpfs.FileMetaInfo, error) {
	return sftpfs.FileMetaInfo{Path: name}, nil
}

func (f *fakeSFTPClient) Rename(ctx context.Context, from, to string) (sftpfs.FileMetaInfo, error) {
	return sftpfs.FileMetaInfo{Path: to}, nil
}

func (f *fakeSFTPClient) Remove(ctx context.Context, name string, recursive bool) error { return nil }

func (f *fakeSFTPClient) Done() <-chan struct{} { return f.done }

func (f *fakeSFTPClient) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.done)
	}
	return nil
}

func (f *fakeSFTPClient) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// tearDown reproduces sftpfs's own stalled-operation teardown: the channel
// goes away and Done fires, without anybody having called Close on the
// browser's behalf.
func (f *fakeSFTPClient) tearDown() { f.Close() }

// fakeDialer hands out fakeSFTPClients and records dials and releases, so a
// test can assert both "how many logins did this cost" and "was the shared
// connection given back".
type fakeDialer struct {
	mu       sync.Mutex
	clients  []*fakeSFTPClient
	releases []string
	err      error
	setup    func(*fakeSFTPClient)
}

func (d *fakeDialer) dial(hostID string) (sftpClient, func(), error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return nil, nil, d.err
	}
	c := newFakeSFTPClient(len(d.clients) + 1)
	if d.setup != nil {
		d.setup(c)
	}
	d.clients = append(d.clients, c)
	return c, func() {
		_ = c.Close()
		d.mu.Lock()
		d.releases = append(d.releases, hostID)
		d.mu.Unlock()
	}, nil
}

func (d *fakeDialer) dialCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.clients)
}

func (d *fakeDialer) releaseCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.releases)
}

func (d *fakeDialer) client(i int) *fakeSFTPClient {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.clients[i]
}

func newTestSFTPBrowser(d *fakeDialer) *sftpBrowser {
	return newSFTPBrowser(context.Background(), d.dial)
}

// --- the source list --------------------------------------------------------

// TestListSFTPHostsHidesProxyCommandHosts pins design §4.3: a ProxyCommand
// host cannot be connected at all, so offering it as a data source only
// produces a failure the user cannot act on. A ProxyJump host does appear —
// item 27 made the chain dialable and SFTP inherits it for free.
func TestListSFTPHostsHidesProxyCommandHosts(t *testing.T) {
	useIsolatedKeyring(t)
	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}

	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{
		{ID: "plain", Alias: "plain", Host: "10.0.0.1", User: "root", AuthKind: "password"},
		{ID: "jump", Alias: "behind-bastion", Host: "10.0.0.2", User: "root", AuthKind: "password", ProxyJump: "plain"},
		{ID: "proxied", Alias: "corkscrew", Host: "10.0.0.3", User: "root", AuthKind: "password",
			ProxyCommand: "corkscrew proxy 8080 %h %p"},
	}
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatal(err)
	}

	got := a.ListSFTPHosts()
	ids := make([]string, 0, len(got))
	for _, h := range got {
		ids = append(ids, h.ID)
	}
	if len(ids) != 2 || ids[0] != "plain" || ids[1] != "jump" {
		t.Fatalf("source list must be the connectable hosts in order, got %v", ids)
	}
	for _, h := range got {
		if h.ProxyCommand != "" {
			t.Fatalf("host %q carries a ProxyCommand and must not be offered as a source", h.Alias)
		}
	}
}

// TestListSFTPHostsIsNeverNil guards the Wails marshalling trap the tunnels
// list already documents: a nil slice arrives in the frontend as null and the
// selector's .map throws before it can render anything.
func TestListSFTPHostsIsNeverNil(t *testing.T) {
	useIsolatedKeyring(t)
	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}
	if a.ListSFTPHosts() == nil {
		t.Fatal("ListSFTPHosts must return an empty slice, never nil")
	}
}

// --- the browser ------------------------------------------------------------

// TestSFTPBrowserReusesOneClientPerHost is the other half of design §4.2's
// "do not open a second connection for SFTP": one channel serves every browse
// of a host, so a directory walk does not cost one login per directory.
func TestSFTPBrowserReusesOneClientPerHost(t *testing.T) {
	d := &fakeDialer{}
	b := newTestSFTPBrowser(d)
	defer b.closeAll()

	for i := 0; i < 5; i++ {
		if _, err := b.listDir("h1", "/srv"); err != nil {
			t.Fatalf("listDir %d: %v", i, err)
		}
	}
	if got := d.dialCount(); got != 1 {
		t.Fatalf("five listings must ride one SFTP channel, dialed %d times", got)
	}
}

// TestSFTPBrowserRedialsAfterChannelTeardown is the load-bearing test of this
// task's Go side.
//
// Task 3's cancellation story is deliberately partial: pkg/sftp's Stat / Open
// / ReadAt take no context, so an executor with too many orphaned operations
// tears the whole SFTP channel down to unblock them. That teardown hits every
// caller sharing the client, not just the one that timed out. A source that
// assumed a client lives as long as the host connection would go silently
// dead at the first teardown and stay dead until the app restarts — and it
// would look like an unrelated bug.
func TestSFTPBrowserRedialsAfterChannelTeardown(t *testing.T) {
	d := &fakeDialer{}
	b := newTestSFTPBrowser(d)
	defer b.closeAll()

	if _, err := b.listDir("h1", "/srv"); err != nil {
		t.Fatalf("first listing: %v", err)
	}
	if got := d.dialCount(); got != 1 {
		t.Fatalf("want 1 dial before the teardown, got %d", got)
	}

	// The executor tears its own channel down. Nobody told the browser.
	d.client(0).tearDown()

	if _, err := b.listDir("h1", "/srv"); err != nil {
		t.Fatalf("browsing after an SFTP channel teardown must redial, not fail: %v", err)
	}
	if got := d.dialCount(); got != 2 {
		t.Fatalf("want a redial after the teardown (2 dials), got %d", got)
	}
	if got := d.releaseCount(); got != 1 {
		t.Fatalf("the torn-down client's connection reference must be given back exactly once, got %d releases", got)
	}
}

// TestSFTPBrowserReleasesTheConnectionOnClose covers the refcount contract
// item 26 owns: the SFTP session borrows the host's shared connection and has
// to hand it back, or the login and its keepalive outlive the panel that
// opened them.
func TestSFTPBrowserReleasesTheConnectionOnClose(t *testing.T) {
	d := &fakeDialer{}
	b := newTestSFTPBrowser(d)

	if _, err := b.listDir("h1", "/srv"); err != nil {
		t.Fatal(err)
	}
	b.closeAll()

	if got := d.releaseCount(); got != 1 {
		t.Fatalf("want the borrowed connection released once, got %d", got)
	}
	if !d.client(0).isClosed() {
		t.Fatal("closing the browser must close the SFTP channel it opened")
	}

	// And closing is not a one-way door: the panel can be reopened.
	if _, err := b.listDir("h1", "/srv"); err != nil {
		t.Fatalf("browsing after closeAll must dial again: %v", err)
	}
	if got := d.dialCount(); got != 2 {
		t.Fatalf("want a fresh dial after closeAll, got %d", got)
	}
	b.closeAll()
}

// TestSFTPBrowserDoesNotCacheAFailedDial: a wrong password or an unreachable
// host must not poison the entry, or the user's retry after fixing it returns
// the stale error forever.
func TestSFTPBrowserDoesNotCacheAFailedDial(t *testing.T) {
	d := &fakeDialer{err: errors.New("connection refused")}
	b := newTestSFTPBrowser(d)
	defer b.closeAll()

	if _, err := b.listDir("h1", "/srv"); err == nil {
		t.Fatal("want the dial error")
	}
	d.mu.Lock()
	d.err = nil
	d.mu.Unlock()

	if _, err := b.listDir("h1", "/srv"); err != nil {
		t.Fatalf("a retry after a failed dial must dial again: %v", err)
	}
}

// TestSFTPBrowserSurvivesConcurrentFirstBrowses: the desktop runs FS requests
// on a pool, so several operations can reach an unopened host at once. They
// must share one dial rather than each opening a channel.
func TestSFTPBrowserSurvivesConcurrentFirstBrowses(t *testing.T) {
	d := &fakeDialer{}
	b := newTestSFTPBrowser(d)
	defer b.closeAll()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = b.listDir("h1", "/srv")
		}()
	}
	wg.Wait()
	if got := d.dialCount(); got != 1 {
		t.Fatalf("eight concurrent first browses must share one dial, got %d", got)
	}
}

// --- what the frontend is handed --------------------------------------------

// TestSFTPListDirCarriesTruncationToTheFrontend is design §5.2: a user seeing
// 12 of 3000 files with no indication is the failure the cap exists to
// prevent, so the flag and the real total have to reach the binding surface —
// dropping them here would make the UI's banner unrenderable.
func TestSFTPListDirCarriesTruncationToTheFrontend(t *testing.T) {
	d := &fakeDialer{setup: func(c *fakeSFTPClient) {
		c.listings["/big"] = sftpfs.Listing{
			Path:      "/big",
			Entries:   []sftpfs.DirEntry{{Name: "a"}, {Name: "b"}},
			Truncated: true,
			Total:     3000,
		}
	}}
	a := &App{ctx: context.Background(), sftp: newTestSFTPBrowser(d)}
	t.Cleanup(func() { a.sftp.closeAll() })

	got, err := a.SFTPListDir("h1", "/big")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Truncated {
		t.Fatal("a truncated listing must say so: without the flag the UI cannot tell the user")
	}
	if got.Total != 3000 {
		t.Fatalf("want the real total 3000 so the UI can say 'showing 2 of 3000', got %d", got.Total)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("want the capped entries, got %d", len(got.Entries))
	}
}

// TestSFTPWriteFileSurfacesTheExistingPathRefusal: Task 3 refuses the write;
// this asserts the refusal reaches the binding surface intact instead of
// being flattened into a generic failure the frontend cannot recognise.
func TestSFTPWriteFileSurfacesTheExistingPathRefusal(t *testing.T) {
	d := &fakeDialer{setup: func(c *fakeSFTPClient) {
		c.writeErr = sftpfs.ErrAlreadyExists
	}}
	a := &App{ctx: context.Background(), sftp: newTestSFTPBrowser(d)}
	t.Cleanup(func() { a.sftp.closeAll() })

	_, err := a.SFTPWriteFile("h1", "/srv/app.conf", []byte("x"), 0, true)
	if err == nil {
		t.Fatal("uploading onto an existing path must be refused by default")
	}
	if !strings.Contains(err.Error(), "already_exists") {
		t.Fatalf("the refusal must stay recognisable to the frontend, got %q", err)
	}
}

// --- end to end over a real SSH connection ----------------------------------

// TestSFTPListDirOverARealSSHConnection walks the whole path the user does:
// a saved host, the shared connection item 26 refcounts, an SFTP channel on
// it, and a real directory listed on the far side.
func TestSFTPListDirOverARealSSHConnection(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "deploy.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}

	addr, hostPub, _ := startSFTPSSHTestServer(t)
	a := newSFTPTestApp(t, addr, hostPub)
	h, err := a.AddSSHHost(
		sshHostAt(addr),
		sshCredential{Password: "pw"},
	)
	if err != nil {
		t.Fatal(err)
	}

	got, err := a.SFTPListDir(h.ID, root)
	if err != nil {
		t.Fatalf("listing a directory on an SSH host: %v", err)
	}
	names := map[string]bool{}
	for _, e := range got.Entries {
		names[e.Name] = e.IsDir
	}
	if isDir, ok := names["deploy.sh"]; !ok || isDir {
		t.Fatalf("want deploy.sh as a file, got %v", got.Entries)
	}
	if isDir, ok := names["logs"]; !ok || !isDir {
		t.Fatalf("want logs as a directory, got %v", got.Entries)
	}
	if got.Truncated {
		t.Fatal("a two-entry directory must not be reported as truncated")
	}

	// The same connection serves a second browse — this is the reuse §4.2
	// asks for, observed from the far side rather than from a counter.
	if _, err := a.SFTPFileMeta(h.ID, filepath.Join(root, "deploy.sh")); err != nil {
		t.Fatalf("file meta over the reused channel: %v", err)
	}
}

// TestSFTPRedialsOverARealConnectionAfterTeardown is the fake-free half of
// TestSFTPBrowserRedialsAfterChannelTeardown.
//
// Closing the client is exactly what internal/sftpfs does to itself when
// stalled operations reach the cap (`go c.Close()`), so this reproduces the
// real teardown rather than a stand-in for it. The assertions that matter are
// both here: browsing works again, and it did *not* cost a second SSH login —
// the redial opens a new channel on the connection item 26 already refcounts.
func TestSFTPRedialsOverARealConnectionAfterTeardown(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "still-here.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	addr, hostPub, logins := startSFTPSSHTestServer(t)
	a := newSFTPTestApp(t, addr, hostPub)
	h, err := a.AddSSHHost(sshHostAt(addr), sshCredential{Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := a.SFTPListDir(h.ID, root); err != nil {
		t.Fatalf("first listing: %v", err)
	}
	beforeTeardown := logins()
	if beforeTeardown != 1 {
		t.Fatalf("want exactly one ssh login for the first browse, got %d", beforeTeardown)
	}

	// The executor tears its own channel down to unblock goroutines parked in
	// pkg/sftp calls that take no context.
	cl, err := a.sftpBrowser().client(h.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = cl.Close()

	got, err := a.SFTPListDir(h.ID, root)
	if err != nil {
		t.Fatalf("browsing after a real sftp teardown must redial, not fail: %v", err)
	}
	if len(got.Entries) != 1 || got.Entries[0].Name != "still-here.txt" {
		t.Fatalf("the redialed session must list the same directory, got %v", got.Entries)
	}
	if after := logins(); after != beforeTeardown {
		t.Fatalf("the redial must reuse the host's connection, not log in again: %d logins before, %d after",
			beforeTeardown, after)
	}
}

// TestSFTPUploadOntoAnExistingPathIsRefusedOverARealConnection is §5.1 end to
// end: the far side keeps its bytes.
func TestSFTPUploadOntoAnExistingPathIsRefusedOverARealConnection(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "app.conf")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	addr, hostPub, _ := startSFTPSSHTestServer(t)
	a := newSFTPTestApp(t, addr, hostPub)
	h, err := a.AddSSHHost(sshHostAt(addr), sshCredential{Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := a.SFTPWriteFile(h.ID, target, []byte("replacement"), 0, true); err == nil {
		t.Fatal("upload onto an existing remote path must be refused: there is no trash on the far side")
	} else if !strings.Contains(err.Error(), "already_exists") {
		t.Fatalf("want a refusal the frontend can recognise, got %q", err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "original" {
		t.Fatalf("the refused upload must leave the far side untouched, got %q", data)
	}

	// A fresh path still uploads.
	fresh := filepath.Join(root, "new.conf")
	if _, err := a.SFTPWriteFile(h.ID, fresh, []byte("hello"), 0, true); err != nil {
		t.Fatalf("uploading to a free path: %v", err)
	}
	data, _ = os.ReadFile(fresh)
	if string(data) != "hello" {
		t.Fatalf("uploaded bytes: %q", data)
	}
}

// TestSFTPBrowseIsRefusedForAProxyCommandHost: the list hides it, but the
// browse path has to refuse it too — a stale frontend list, or a host that
// grew a ProxyCommand after the panel loaded, must not reach a dial.
func TestSFTPBrowseIsRefusedForAProxyCommandHost(t *testing.T) {
	useIsolatedKeyring(t)
	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}
	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{
		ID: "p1", Alias: "corkscrew", Host: "10.0.0.9", User: "root", AuthKind: "password",
		ProxyCommand: "corkscrew proxy 8080 %h %p",
	}}
	if err := a.cfgStore.Set(cfg); err != nil {
		t.Fatal(err)
	}

	_, err := a.SFTPListDir("p1", "/")
	if err == nil {
		t.Fatal("browsing a ProxyCommand host must be refused")
	}
	if !strings.Contains(err.Error(), "ProxyCommand") {
		t.Fatalf("the refusal must name the reason, got %q", err)
	}
}

// --- helpers ----------------------------------------------------------------

func sshHostAt(addr string) SSHHost {
	host, port, _ := net.SplitHostPort(addr)
	return SSHHost{Host: host, Port: port, User: "u", AuthKind: "password"}
}

func newSFTPTestApp(t *testing.T, addr string, hostPub ssh.PublicKey) *App {
	t.Helper()
	useIsolatedKeyring(t)
	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}
	a.sshKnownHostsPath = writeKnownHostsFor(t, addr, hostPub)
	t.Cleanup(func() {
		a.sftpBrowser().closeAll()
		a.tunnels.stopAll()
	})
	return a
}

// sftpSubsystemRequest is the payload of a "subsystem" channel request
// (RFC 4254 §6.5).
type sftpSubsystemRequest struct{ Name string }

// startSFTPSSHTestServer is startSSHTestServer's sibling for this file: same
// credentials, but the session channel answers a "subsystem"/"sftp" request
// with a real pkg/sftp server instead of a shell. Nothing here fakes SFTP —
// the bytes on the channel are the protocol.
func startSFTPSSHTestServer(t *testing.T) (addr string, hostPub ssh.PublicKey, logins func() int) {
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
	t.Cleanup(func() { _ = ln.Close() })

	// Counting logins is how "SFTP does not open its own connection" is
	// observed from the far side rather than from a counter on our own.
	var accepted atomic.Int64
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			go serveSFTPConn(nc, cfg)
		}
	}()
	return ln.Addr().String(), signer.PublicKey(), func() int { return int(accepted.Load()) }
}

func serveSFTPConn(nc net.Conn, cfg *ssh.ServerConfig) {
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
				if r.Type != "subsystem" {
					if r.WantReply {
						_ = r.Reply(false, nil)
					}
					continue
				}
				var payload sftpSubsystemRequest
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
					_ = ch.Close()
					continue
				}
				go func() { _ = srv.Serve(); _ = ch.Close() }()
			}
		}(ch, chReqs)
	}
}

// --- the path gate ----------------------------------------------------------

// TestSFTPPathGateMatchesTheLocalDenyList is the asymmetry this task owns.
//
// The local source runs every path through fsAccess.resolve, which refuses
// .ssh / .gnupg / .aws. internal/sftpfs validates only that a path is absolute
// and says in its own package comment that the policy belongs to the caller.
// Without this gate the two data sources would ship with different security
// surfaces and nothing anywhere would say so.
func TestSFTPPathGateMatchesTheLocalDenyList(t *testing.T) {
	d := &fakeDialer{}
	b := newTestSFTPBrowser(d)
	defer b.closeAll()

	denied := []string{
		"/home/u/.ssh",
		"/home/u/.ssh/id_ed25519",
		"/root/.gnupg/secring.gpg",
		"/home/u/.aws/credentials",
		"/home/u/projects/../.ssh/config", // the cleaner must not be evadable
	}
	for _, p := range denied {
		if _, err := b.listDir("h1", p); err == nil {
			t.Fatalf("%s must be refused: the local source refuses it", p)
		} else if !errors.Is(err, ErrPathDenied) {
			t.Fatalf("%s: want ErrPathDenied, got %v", p, err)
		}
	}
	if got := d.dialCount(); got != 0 {
		t.Fatalf("a denied path must be refused before any dial, got %d dials", got)
	}

	if _, err := b.listDir("h1", "relative/path"); !errors.Is(err, ErrPathRelative) {
		t.Fatalf("want ErrPathRelative for a relative path, got %v", err)
	}
	if _, err := b.listDir("h1", "/srv/app"); err != nil {
		t.Fatalf("an ordinary path must still be allowed: %v", err)
	}
}

// TestSFTPListingHidesDeniedEntries: refusing entry is not enough on its own.
// A row the panel renders and then refuses to open is a worse experience than
// one that was never offered, and it also puts the name of every key file in
// the tree.
func TestSFTPListingHidesDeniedEntries(t *testing.T) {
	d := &fakeDialer{setup: func(c *fakeSFTPClient) {
		c.listings["/home/u"] = sftpfs.Listing{
			Path: "/home/u",
			Entries: []sftpfs.DirEntry{
				{Name: ".ssh", IsDir: true},
				{Name: "notes.md"},
			},
			Total: 2,
		}
	}}
	b := newTestSFTPBrowser(d)
	defer b.closeAll()

	got, err := b.listDir("h1", "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got.Entries {
		if e.Name == ".ssh" {
			t.Fatal(".ssh must not be offered as a row: entering it is refused anyway")
		}
	}
	if len(got.Entries) != 1 {
		t.Fatalf("want the remaining entry, got %v", got.Entries)
	}
}

// The handshake's bound and the abandoned channel it has to close are pinned
// where they now live: sftpfs.OpenContext, against a real SSH server
// (internal/sftpfs). dialSFTP calls it directly.
