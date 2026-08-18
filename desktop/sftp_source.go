package main

// The file explorer's third data source: a saved SSH host, browsed over SFTP
// (roadmap item 28, design §4.2/§4.3).
//
// Everything here rides the per-host SSH connection item 26 already
// refcounts (hostConn): one login and one keepalive serve the host's tunnels
// and its file browsing alike. A dedicated connection for SFTP would show up
// as a second login on the remote and a second keepalive on the wire, for no
// benefit — the SFTP channel is just another channel on a connection that is
// already open, and through a ProxyJump chain it inherits the hops for free
// because it is opened on the chain's last link.
//
// The protocol work itself lives in internal/sftpfs. What lives here is the
// three things that cannot: which hosts may be offered, which paths may be
// touched, and the fact that an SFTP client is *not* durable — see
// sftpBrowser.client.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/attson/atterm/internal/sftpfs"
)

const (
	// sftpOpTimeout bounds one filesystem operation on a remote host.
	//
	// A bound is not optional. Task 1's review made the point precisely: the
	// resource a wedged operation holds is the desktop's own slot, and a
	// relay-side TTL cannot free it. On a local disk an unbounded operation is
	// a theoretical concern; over SSH "the host stopped answering" is an
	// ordinary Tuesday.
	sftpOpTimeout = 30 * time.Second

	// sftpOpenTimeout bounds the SFTP handshake specifically, via
	// sftpfs.OpenContext. The handshake needs its own bound rather than
	// inheriting an operation's: it is the redial path's constructor, and the
	// reads it makes (the subsystem reply, then VERSION) sit outside
	// internal/sftpfs's cancellable wrapper, so a host that accepts SSH and
	// never starts sftp-server is never counted as an orphan and never trips
	// the channel teardown that would otherwise free it.
	sftpOpenTimeout = 15 * time.Second

	// sftpReadFileMax is the ceiling on a whole-file read, matching the local
	// executor's maxReadBytesHard so the two sources refuse the same files.
	sftpReadFileMax = maxReadBytesHard
)

// ---------------------------------------------------------------------------
// the path gate
// ---------------------------------------------------------------------------

// sftpResolvePath is this source's half of the gate the local source gets from
// fsAccess.resolve. internal/sftpfs deliberately validates only that a path is
// absolute and says so in its package comment: the policy belongs to the
// caller, and this is the caller.
//
// What is carried over from the local source, and why:
//
//   - denyExact (.ssh, .gnupg, .aws) applies unchanged, and it is the same
//     variable rather than a copy of the list. A private key is a private key
//     on either machine, and the difference between "the user opened it" and
//     "the panel rendered it into a preview pane while they clicked around" is
//     the whole reason that list exists.
//
// What is deliberately *not* carried over, because it does not mean the same
// thing here:
//
//   - allowRoots. Locally it bounds the blast radius of a remote client
//     driving the FS channel with full permission. This source is reached only
//     from this machine's own webview, by the user who chose the host, using a
//     credential that already opens a shell on it one click away in atterm's
//     own terminal. Restricting the panel to a subtree while the terminal
//     beside it has none would be theatre, not a boundary. If this source is
//     ever exposed over the relay FS channel — where the driver may be someone
//     else's browser — that reasoning stops holding and a roots restriction has
//     to come back with it. That last sentence is enforced, not merely written:
//     .github/scripts/check-plugin-fs-isolation.sh fails the build if any of
//     this file's identifiers appear in desktop/uplink*.go or internal/, which
//     is exactly the moment somebody starts wiring it to a relay driver.
//
//   - .env. The local Wails path allows it (newFSAccess(..., false)) because
//     the bytes never leave the machine; the deny exists for the relay path,
//     where they would cross in the clear. This source has the local path's
//     shape, so it gets the local path's answer. Making it stricter than the
//     local panel would be an inconsistency with no transport reason behind it.
//
// Known gap, stated rather than papered over: the local gate calls
// EvalSymlinks first, so a symlink into ~/.ssh is caught. Here the check is
// lexical, because resolving a remote symlink costs a round trip and
// internal/sftpfs exposes no realpath. A remote symlink pointing into a denied
// directory is therefore followed. That is a real difference from the local
// source and it is why this is a hygiene gate, not a sandbox.
func sftpResolvePath(p string) (string, error) {
	clean, err := sftpCleanPath(p)
	if err != nil {
		return "", err
	}
	for _, seg := range strings.Split(clean, "/") {
		for _, denied := range denyExact {
			if seg == denied {
				return "", fmt.Errorf("%w: %s", ErrPathDenied, clean)
			}
		}
	}
	return clean, nil
}

// sftpCleanPath normalizes a remote POSIX path. It is not filepath.Clean:
// these paths live on the far side and are POSIX regardless of what this
// binary runs on, and filepath would rewrite the separators on Windows.
func sftpCleanPath(p string) (string, error) {
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("%w: %s", ErrPathRelative, p)
	}
	segments := strings.Split(p, "/")
	out := make([]string, 0, len(segments))
	for _, seg := range segments {
		switch seg {
		case "", ".":
			continue
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, seg)
		}
	}
	return "/" + strings.Join(out, "/"), nil
}

// ---------------------------------------------------------------------------
// what crosses to the frontend
// ---------------------------------------------------------------------------

// SFTPListing is one remote directory listing. It is not []DirEntry because a
// remote listing can be capped (sftpfs.MaxEntries) and the cap has to be
// visible: showing 12 entries out of 3000 with no indication is the failure
// design §5.2 exists to prevent, and a bare slice cannot say it happened.
// Total is the real count so the panel can say "showing 2000 of 3000" rather
// than only "some are missing".
type SFTPListing struct {
	Path      string     `json:"path"`
	Entries   []DirEntry `json:"entries"`
	Truncated bool       `json:"truncated"`
	Total     int        `json:"total"`
}

// ---------------------------------------------------------------------------
// the client
// ---------------------------------------------------------------------------

// sftpClient is the slice of *sftpfs.Client this file uses. It is an interface
// so the browser's lifecycle — which is the part with the interesting failure
// modes — can be tested without an SSH server anywhere in sight.
type sftpClient interface {
	ListDir(ctx context.Context, dir string) (sftpfs.Listing, error)
	FileMeta(ctx context.Context, name string) (sftpfs.FileMetaInfo, error)
	ReadChunk(ctx context.Context, name string, offset, length int64) (sftpfs.Chunk, error)
	WriteFile(ctx context.Context, name string, data []byte, expectedModTime int64, createIfMissing bool) (sftpfs.FileMetaInfo, error)
	CreateFile(ctx context.Context, name string) (sftpfs.FileMetaInfo, error)
	Mkdir(ctx context.Context, name string) (sftpfs.FileMetaInfo, error)
	Rename(ctx context.Context, from, to string) (sftpfs.FileMetaInfo, error)
	Remove(ctx context.Context, name string, recursive bool) error
	Done() <-chan struct{}
	Close() error
}

// ---------------------------------------------------------------------------
// the browser
// ---------------------------------------------------------------------------

// sftpBrowser holds at most one SFTP client per host and hands it to
// operations, redialing whenever the one it holds has gone.
//
// The redial is the point of this type, not an optimisation. internal/sftpfs
// tears its own channel down when enough operations stall, to unblock the
// goroutines parked in pkg/sftp calls that take no context — and that teardown
// is collateral for every other caller sharing the client. A source that
// treated a client as living for the life of the host connection would go
// silently dead at the first teardown and stay dead until the app restarted,
// presenting as an unrelated bug some time later.
type sftpBrowser struct {
	ctx context.Context
	// dial opens an SFTP client on the host's shared connection and returns
	// the release to call when the client is retired. Injected so the
	// lifecycle above is testable without SSH.
	dial func(hostID string) (sftpClient, func(), error)

	mu      sync.Mutex
	entries map[string]*sftpBrowserEntry
}

// sftpBrowserEntry is one host's client, or the in-flight dial for it. ready
// closes once cl/err are final, so a second browse arriving mid-dial waits for
// the same connection instead of opening another — the same shape hostConn
// uses, for the same reason.
type sftpBrowserEntry struct {
	ready   chan struct{}
	cl      sftpClient
	release func()
	err     error

	retireOnce sync.Once
}

func newSFTPBrowser(ctx context.Context, dial func(string) (sftpClient, func(), error)) *sftpBrowser {
	if ctx == nil {
		ctx = context.Background()
	}
	return &sftpBrowser{ctx: ctx, dial: dial, entries: map[string]*sftpBrowserEntry{}}
}

// client returns a usable client for hostID, dialing or redialing as needed.
//
// "Usable" is checked, not assumed: an entry whose Done has fired is replaced.
// The loop exists only for the case where two callers notice the same dead
// client at once — the loser takes the winner's replacement rather than
// dialing a second one.
func (b *sftpBrowser) client(hostID string) (sftpClient, error) {
	for attempt := 0; attempt < 3; attempt++ {
		b.mu.Lock()
		existing, ok := b.entries[hostID]
		if !ok {
			fresh := &sftpBrowserEntry{ready: make(chan struct{})}
			b.entries[hostID] = fresh
			b.mu.Unlock()
			return b.fill(hostID, fresh, nil)
		}
		b.mu.Unlock()

		<-existing.ready
		if existing.err != nil {
			// A failed dial is removed from the map before ready closes, so
			// this can only be a straggler that read the entry first. Report
			// it; the next call dials again.
			return nil, existing.err
		}
		if sftpClientAlive(existing.cl) {
			return existing.cl, nil
		}

		b.mu.Lock()
		if b.entries[hostID] != existing {
			b.mu.Unlock()
			continue // somebody else already replaced it; take theirs
		}
		fresh := &sftpBrowserEntry{ready: make(chan struct{})}
		b.entries[hostID] = fresh
		b.mu.Unlock()
		return b.fill(hostID, fresh, existing)
	}
	return nil, fmt.Errorf("sftp: could not get a usable sftp session for host %s", hostID)
}

// fill dials into e and, only once e has a connection reference of its own,
// retires the session it replaced.
//
// That order is the whole point. Releasing the dead session first would drop
// the host connection's refcount to zero, close the login, and make the
// replacement dial a new one — turning an SFTP *channel* teardown into a full
// reconnect, with another line in the remote's auth log every time. Item 26's
// refcount is what makes the overlap free: the replacement takes a second
// reference on the same connection before the first is given back.
func (b *sftpBrowser) fill(hostID string, e, replaced *sftpBrowserEntry) (sftpClient, error) {
	e.cl, e.release, e.err = b.dial(hostID)
	if e.err != nil {
		// Never cache a failed dial: a wrong password the user then fixes, or
		// a host that was simply down, must not answer the retry with the
		// previous attempt's error forever.
		b.mu.Lock()
		if b.entries[hostID] == e {
			delete(b.entries, hostID)
		}
		b.mu.Unlock()
	}
	close(e.ready)
	if replaced != nil {
		replaced.retire()
	}
	if e.err != nil {
		return nil, e.err
	}
	return e.cl, nil
}

// sftpClientAlive reports whether a client is still worth handing out.
func sftpClientAlive(cl sftpClient) bool {
	if cl == nil {
		return false
	}
	select {
	case <-cl.Done():
		return false
	default:
		return true
	}
}

// retire gives an entry's connection reference back, exactly once however many
// callers race to notice the same dead client. It never runs under b.mu: the
// release reaches into tunnelManager's own lock.
func (e *sftpBrowserEntry) retire() {
	e.retireOnce.Do(func() {
		if e.release != nil {
			e.release()
		}
	})
}

// disconnect ends browsing of one host: the SFTP channel closes and the host's
// shared connection loses this reference, so a host nobody is tunnelling to
// stops holding a login open after the panel moved on.
func (b *sftpBrowser) disconnect(hostID string) {
	b.mu.Lock()
	e, ok := b.entries[hostID]
	if ok {
		delete(b.entries, hostID)
	}
	b.mu.Unlock()
	if !ok {
		return
	}
	<-e.ready
	e.retire()
}

// closeAll releases every host. Called on shutdown, and safe to call with
// browsing still in flight: an operation already inside a call gets a
// connection-lost error rather than hanging.
func (b *sftpBrowser) closeAll() {
	b.mu.Lock()
	entries := make(map[string]*sftpBrowserEntry, len(b.entries))
	for id, e := range b.entries {
		entries[id] = e
	}
	b.entries = map[string]*sftpBrowserEntry{}
	b.mu.Unlock()
	for _, e := range entries {
		<-e.ready
		e.retire()
	}
}

// sftpDo runs one operation on a host under a deadline. It is a function
// rather than a method because Go has no generic methods, and the alternative
// — one hand-written copy of this per operation — is where a missing timeout
// hides.
func sftpDo[T any](b *sftpBrowser, hostID string, fn func(context.Context, sftpClient) (T, error)) (T, error) {
	var zero T
	cl, err := b.client(hostID)
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(b.ctx, sftpOpTimeout)
	defer cancel()
	return fn(ctx, cl)
}

// --- operations -------------------------------------------------------------

func (b *sftpBrowser) listDir(hostID, path string) (SFTPListing, error) {
	p, err := sftpResolvePath(path)
	if err != nil {
		return SFTPListing{}, err
	}
	return sftpDo(b, hostID, func(ctx context.Context, cl sftpClient) (SFTPListing, error) {
		listing, err := cl.ListDir(ctx, p)
		if err != nil {
			return SFTPListing{}, err
		}
		entries := make([]DirEntry, 0, len(listing.Entries))
		for _, e := range listing.Entries {
			// A denied directory is hidden from the listing as well as refused
			// on entry, so the panel does not offer a row that cannot be
			// opened.
			if sftpEntryDenied(e.Name) {
				continue
			}
			entries = append(entries, DirEntry{
				Name: e.Name, IsDir: e.IsDir, Size: e.Size, ModTime: e.ModTime,
			})
		}
		return SFTPListing{
			Path:      listing.Path,
			Entries:   entries,
			Truncated: listing.Truncated,
			Total:     listing.Total,
		}, nil
	})
}

func sftpEntryDenied(name string) bool {
	for _, denied := range denyExact {
		if name == denied {
			return true
		}
	}
	return false
}

func (b *sftpBrowser) fileMeta(hostID, path string) (FileMetaInfo, error) {
	p, err := sftpResolvePath(path)
	if err != nil {
		return FileMetaInfo{}, err
	}
	return sftpDo(b, hostID, func(ctx context.Context, cl sftpClient) (FileMetaInfo, error) {
		meta, err := cl.FileMeta(ctx, p)
		if err != nil {
			return FileMetaInfo{}, err
		}
		return FileMetaInfo{Path: meta.Path, Size: meta.Size, ModTime: meta.ModTime, IsBinary: meta.IsBinary}, nil
	})
}

// readFile assembles a whole file from chunks. internal/sftpfs offers no
// whole-file read on purpose — the local one exists to feed the editor, and
// §3 excludes remote editing — but the preview pane needs the bytes, and
// chunking them here keeps the 256 KiB ceiling sftpfs already enforces.
func (b *sftpBrowser) readFile(hostID, path string, maxBytes int64) (FileContent, error) {
	p, err := sftpResolvePath(path)
	if err != nil {
		return FileContent{}, err
	}
	if maxBytes <= 0 || maxBytes > sftpReadFileMax {
		maxBytes = sftpReadFileMax
	}
	return sftpDo(b, hostID, func(ctx context.Context, cl sftpClient) (FileContent, error) {
		meta, err := cl.FileMeta(ctx, p)
		if err != nil {
			return FileContent{}, err
		}
		out := FileContent{Path: p, IsBinary: meta.IsBinary}
		if meta.IsBinary {
			// Same contract as the local executor: a binary file reports its
			// binary-ness and no bytes, so the frontend shows its banner
			// instead of pushing megabytes at a text editor.
			return out, nil
		}
		var data []byte
		for int64(len(data)) < maxBytes {
			want := maxBytes - int64(len(data))
			if want > readChunkMax {
				want = readChunkMax
			}
			chunk, err := cl.ReadChunk(ctx, p, int64(len(data)), want)
			if err != nil {
				return FileContent{}, err
			}
			data = append(data, chunk.Data...)
			if chunk.EOF || len(chunk.Data) == 0 {
				break
			}
		}
		out.Data = data
		if meta.Size > int64(len(data)) {
			out.TruncatedAt = meta.Size
		}
		return out, nil
	})
}

func (b *sftpBrowser) writeFile(hostID, path string, data []byte, expectedModTime int64, createIfMissing bool) (FileMetaInfo, error) {
	p, err := sftpResolvePath(path)
	if err != nil {
		return FileMetaInfo{}, err
	}
	return sftpDo(b, hostID, func(ctx context.Context, cl sftpClient) (FileMetaInfo, error) {
		meta, err := cl.WriteFile(ctx, p, data, expectedModTime, createIfMissing)
		if err != nil {
			return FileMetaInfo{}, err
		}
		return FileMetaInfo{Path: meta.Path, Size: meta.Size, ModTime: meta.ModTime, IsBinary: meta.IsBinary}, nil
	})
}

func (b *sftpBrowser) createFile(hostID, path string) (FileMetaInfo, error) {
	p, err := sftpResolvePath(path)
	if err != nil {
		return FileMetaInfo{}, err
	}
	return sftpDo(b, hostID, func(ctx context.Context, cl sftpClient) (FileMetaInfo, error) {
		meta, err := cl.CreateFile(ctx, p)
		if err != nil {
			return FileMetaInfo{}, err
		}
		return FileMetaInfo{Path: meta.Path, Size: meta.Size, ModTime: meta.ModTime}, nil
	})
}

func (b *sftpBrowser) mkdir(hostID, path string) (FileMetaInfo, error) {
	p, err := sftpResolvePath(path)
	if err != nil {
		return FileMetaInfo{}, err
	}
	return sftpDo(b, hostID, func(ctx context.Context, cl sftpClient) (FileMetaInfo, error) {
		meta, err := cl.Mkdir(ctx, p)
		if err != nil {
			return FileMetaInfo{}, err
		}
		return FileMetaInfo{Path: meta.Path, Size: meta.Size, ModTime: meta.ModTime}, nil
	})
}

func (b *sftpBrowser) rename(hostID, from, to string) (FileMetaInfo, error) {
	src, err := sftpResolvePath(from)
	if err != nil {
		return FileMetaInfo{}, err
	}
	dst, err := sftpResolvePath(to)
	if err != nil {
		return FileMetaInfo{}, err
	}
	return sftpDo(b, hostID, func(ctx context.Context, cl sftpClient) (FileMetaInfo, error) {
		meta, err := cl.Rename(ctx, src, dst)
		if err != nil {
			return FileMetaInfo{}, err
		}
		return FileMetaInfo{Path: meta.Path, Size: meta.Size, ModTime: meta.ModTime}, nil
	})
}

func (b *sftpBrowser) remove(hostID, path string, recursive bool) error {
	p, err := sftpResolvePath(path)
	if err != nil {
		return err
	}
	_, err = sftpDo(b, hostID, func(ctx context.Context, cl sftpClient) (struct{}, error) {
		return struct{}{}, cl.Remove(ctx, p, recursive)
	})
	return err
}

// ---------------------------------------------------------------------------
// App wiring
// ---------------------------------------------------------------------------

// sftpBrowser returns the app's browser, building it on first use. It is lazy
// because App is constructed in several shapes (NewApp, and half a dozen test
// literals) and none of them should have to remember this.
func (a *App) sftpBrowser() *sftpBrowser {
	a.sftpMu.Lock()
	defer a.sftpMu.Unlock()
	if a.sftp == nil {
		ctx := a.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		a.sftp = newSFTPBrowser(ctx, a.dialSFTP)
	}
	return a.sftp
}

// dialSFTP borrows the host's shared SSH connection and starts SFTP on it.
//
// The connection comes from tunnelManager.acquireConn — the same refcount a
// tunnel takes — so browsing a host the user is already tunnelling to costs no
// extra login, and the last of the two to go closes it.
func (a *App) dialSFTP(hostID string) (sftpClient, func(), error) {
	host, err := a.connectableSSHHost(hostID)
	if err != nil {
		return nil, nil, err
	}
	hc, err := a.tunnels.acquireConn(hostID, func() (*jumpChain, error) {
		return a.dialTunnelConn(host)
	})
	if err != nil {
		return nil, nil, err
	}
	// The shared connection may have died while somebody else still held a
	// reference to it, in which case acquireConn hands back the corpse. Say so
	// rather than letting an sftp handshake fail with a transport error the
	// user cannot read. Releasing it is what lets the next attempt redial.
	select {
	case <-hc.conn().Done():
		a.tunnels.releaseConn(hostID, hc)
		return nil, nil, fmt.Errorf("the ssh connection to %s has dropped; try again to reconnect", host.Host)
	default:
	}

	ctx, cancel := context.WithTimeout(a.browserContext(), sftpOpenTimeout)
	defer cancel()
	conn := hc.conn()
	// sftpfs.OpenContext bounds both halves of the handshake — the subsystem
	// request and the VERSION exchange behind it — and, on the deadline, closes
	// the abandoned stream on the far side. That second part is what a bound
	// applied here could not do: releaseConn only closes at refcount zero, so
	// with a tunnel up to the same host every wedged attempt would otherwise
	// leave an SSH session channel open until sshd's MaxSessions refused the
	// next one.
	//
	// Note the concrete variable: OpenContext returns *sftpfs.Client, and a nil
	// one assigned into the sftpClient interface would be a non-nil interface
	// wrapping a nil pointer — it would pass sftpClientAlive and fail much
	// later, somewhere far less obvious. err is checked before the conversion.
	client, err := sftpfs.OpenContext(ctx, conn)
	if err != nil {
		a.tunnels.releaseConn(hostID, hc)
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, nil, fmt.Errorf(
				"the ssh connection is up but the host did not start an sftp session in time "+
					"(is sftp-server installed and enabled in its sshd_config?): %w", err)
		}
		return nil, nil, err
	}
	var cl sftpClient = client

	// A dead SSH connection has to close the SFTP channel riding it, so the
	// browser has exactly one liveness signal to check (Done) rather than two
	// that can disagree.
	go func() {
		select {
		case <-conn.Done():
			_ = cl.Close()
		case <-cl.Done():
		}
	}()

	return cl, func() {
		_ = cl.Close()
		a.tunnels.releaseConn(hostID, hc)
	}, nil
}

func (a *App) browserContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// ---------------------------------------------------------------------------
// bindings
// ---------------------------------------------------------------------------

// ListSFTPHosts is the file explorer's source list: every saved host atterm
// will actually dial.
//
// It is derived from connectableSSHHost rather than from its own predicate, so
// the list and the refusal cannot drift apart — a host that disappears from
// here is exactly a host that would have been refused on click. That is also
// why there is no third caller of hostRunsProxyCommand: this asks the gate
// instead of re-deciding.
//
// Never nil: Wails marshals a nil slice as null and the selector's .map then
// throws before it renders anything.
func (a *App) ListSFTPHosts() []SSHHost {
	hosts := a.ListSSHHosts()
	out := make([]SSHHost, 0, len(hosts))
	for _, h := range hosts {
		if _, err := a.connectableSSHHost(h.ID); err != nil {
			continue
		}
		out = append(out, h)
	}
	return out
}

// SFTPListDir lists one remote directory. See SFTPListing for why it is not a
// bare slice.
func (a *App) SFTPListDir(hostID, path string) (SFTPListing, error) {
	return a.sftpBrowser().listDir(hostID, path)
}

// SFTPFileMeta stats one remote path.
func (a *App) SFTPFileMeta(hostID, path string) (FileMetaInfo, error) {
	return a.sftpBrowser().fileMeta(hostID, path)
}

// SFTPReadFile reads a remote file for preview, capped at maxBytes (0 means
// the hard cap). A binary file comes back flagged and empty, as it does
// locally.
func (a *App) SFTPReadFile(hostID, path string, maxBytes int64) (FileContent, error) {
	return a.sftpBrowser().readFile(hostID, path, maxBytes)
}

// SFTPWriteFile uploads to a remote path.
//
// It refuses an upload onto a path that already exists unless the caller
// passes the ModTime it last saw (design §5.1). That is stricter than the
// local executor on purpose: there is no trash and no versioning on the far
// side, so a mistaken overwrite is unrecoverable, while a refusal costs one
// confirmation. The refusal arrives as sftpfs.ErrAlreadyExists ("already_exists")
// and the frontend turns it into a sentence the user can act on.
func (a *App) SFTPWriteFile(hostID, path string, data []byte, expectedModTime int64, createIfMissing bool) (FileMetaInfo, error) {
	return a.sftpBrowser().writeFile(hostID, path, data, expectedModTime, createIfMissing)
}

// SFTPCreateFile creates an empty remote file; an existing path is refused.
func (a *App) SFTPCreateFile(hostID, path string) (FileMetaInfo, error) {
	return a.sftpBrowser().createFile(hostID, path)
}

// SFTPMkdir creates one remote directory; the parent must exist.
func (a *App) SFTPMkdir(hostID, path string) (FileMetaInfo, error) {
	return a.sftpBrowser().mkdir(hostID, path)
}

// SFTPRename moves a remote path. An existing target is refused, never
// replaced — same reasoning as SFTPWriteFile.
func (a *App) SFTPRename(hostID, from, to string) (FileMetaInfo, error) {
	return a.sftpBrowser().rename(hostID, from, to)
}

// SFTPRemove deletes a remote path. There is no trash on the far side: this is
// a real delete and the UI says so before calling it.
func (a *App) SFTPRemove(hostID, path string, recursive bool) error {
	return a.sftpBrowser().remove(hostID, path, recursive)
}

// SFTPDisconnect ends browsing of one host, releasing its share of the shared
// connection. Without it, opening the panel on a host would hold a login open
// until the app quit.
func (a *App) SFTPDisconnect(hostID string) {
	a.sftpBrowser().disconnect(hostID)
}
