// Package sftpfs speaks SFTP to a remote host and offers the operation set the
// desktop's local filesystem executor offers for the local disk — listing,
// metadata, chunked reads, and a write path — so the file explorer can point
// at an SSH host as a third data source.
//
// It is a separate package rather than code inside desktop/ for two reasons.
// One is the dependency direction: internal/ must not import desktop/. The
// other is that everything here is protocol behaviour, and pkg/sftp ships an
// in-memory server, so all of it can be tested over a net.Pipe without an SSH
// server anywhere in sight.
//
// Two things differ deliberately from the local executor:
//
//   - A write onto an existing path is refused, not applied. See WriteFile.
//   - Every operation takes a context and honours it. os.ReadDir cannot be
//     cancelled, so the local executor does not try; SFTP can, and the desktop
//     runs these on a small fixed worker pool where a wedged operation eats a
//     slot for the life of the connection.
//
// Path arguments are remote POSIX paths and must be absolute. This package
// does not implement an allowlist: that gate lives with the caller, on the
// same permission check browsing already goes through.
//
// # Why github.com/pkg/sftp is pinned to v1.13.9
//
// v1.13.11 declares go 1.25 and requires golang.org/x/crypto v0.54. Taking it
// would move this module's own Go directive and drag x/crypto off v0.37.0,
// which is the version the jump-host code in internal/sshclient was written
// and reasoned against. Nothing in v1.13.10 or v1.13.11 is needed here.
//
// That is the whole justification, and it is temporary: the moment this
// module's Go directive moves for any unrelated reason, the pin costs nothing
// to lift and should be lifted. It is recorded here and in go.mod so that
// whoever next bumps the directive finds the reason rather than a bare version.
package sftpfs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/attson/atterm/internal/sshclient"
	"github.com/pkg/sftp"
)

const (
	// MaxEntries caps one directory listing. A cap is not optional here: a
	// listing crosses the relay as one frame, and a home directory on a build
	// server can hold six figures of entries. What matters as much as the cap
	// is that Listing reports having applied it — showing 2000 of 30000 files
	// with no indication is worse than refusing outright.
	MaxEntries = 2000

	maxWriteBytesHard = 5 * 1024 * 1024 // symmetric with the local executor
	readChunkMax      = 256 * 1024
	binaryProbeBytes  = 4096

	// tempPrefix names the staging file WriteFile renames into place. It is
	// dotted so it stays out of a casual listing if a crash ever leaves one.
	tempPrefix = ".atterm-tmp-"

	// posixRenameExt is the OpenSSH extension that makes rename overwrite its
	// target atomically. SFTP v3's own rename refuses an existing target, so
	// without this an overwrite needs an unlink first.
	posixRenameExt = "posix-rename@openssh.com"

	// orphanGrace is how long an abandoned operation is given to finish on its
	// own before it counts as stalled. Cancellation is not by itself evidence
	// of a wedged channel: a listing cancelled before its directory handle is
	// open returns within microseconds of the deadline (opendir takes ctx and
	// there is no handle left to close), and a burst of those must not be
	// mistaken for a dead host.
	orphanGrace = 250 * time.Millisecond

	// stalledOpCap is how many operations may be simultaneously orphaned —
	// abandoned by their caller on a deadline, and still not finished
	// orphanGrace later — before the channel is declared wedged and torn down.
	//
	// Returning promptly on cancellation is only half the job: the goroutine
	// behind the abandoned call is still parked on a response, and against a
	// host that has stopped answering it stays parked until the TCP connection
	// itself dies, which can be a very long time. Tearing the channel down
	// unblocks all of them at once (the client broadcasts a connection-lost
	// error to everything in flight) and closes Done so the owner knows to
	// redial.
	//
	// The number is a judgement call. It is above the desktop's four worker
	// slots per session — one session timing out its whole pool should not by
	// itself condemn a connection several sessions may share — and low enough
	// that a genuinely dead host is noticed in one round of timeouts rather
	// than accumulating goroutines indefinitely.
	stalledOpCap = 8
)

// Sentinel errors. The strings match the local executor's so that the
// transport layer's error mapping stays a single vocabulary rather than
// growing a second one for remote hosts.
var (
	ErrStaleModTime  = errors.New("stale_modtime")
	ErrAlreadyExists = errors.New("already_exists")
	ErrNotFound      = errors.New("not_found")
	ErrIsDirectory   = errors.New("is_directory")
	ErrNotADirectory = errors.New("not_a_directory")
	ErrPathRelative  = errors.New("sftpfs: path must be absolute")
)

// DirEntry is one entry in a directory listing. Field names and JSON tags
// match the local executor's so the two sources serialize identically.
type DirEntry struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size,omitempty"`
	ModTime int64  `json:"modTime,omitempty"` // unix ms
}

// Listing is the result of ListDir. Truncated and Total exist so a caller can
// say "showing 2000 of 30000" instead of quietly presenting a partial
// directory as a complete one.
type Listing struct {
	Path      string     `json:"path"`
	Entries   []DirEntry `json:"entries"`
	Truncated bool       `json:"truncated,omitempty"`
	Total     int        `json:"total"`
}

// FileMetaInfo mirrors the local executor's type of the same name.
type FileMetaInfo struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	ModTime  int64  `json:"modTime"` // unix ms; see the note on WriteFile
	IsBinary bool   `json:"isBinary"`
}

// Chunk is one range of a file, as returned by ReadChunk.
type Chunk struct {
	Path        string `json:"path"`
	Offset      int64  `json:"offset"`
	Data        []byte `json:"data"`
	ContentType string `json:"contentType"`
	EOF         bool   `json:"eof"`
}

// Client is an SFTP executor bound to one channel on one host.
//
// It is safe for concurrent use: pkg/sftp multiplexes requests by id over a
// single channel, which is what makes sharing one client across a worker pool
// (rather than opening a channel per operation) the right shape.
type Client struct {
	sc          *sftp.Client
	maxEntries  int
	posixRename bool

	// inflight counts every goroutine this client leaves running: the one
	// behind each operation, and the watcher that an orphaned operation adds
	// (which parks on the same response, so it is a second goroutine and not a
	// bookkeeping detail). Counting both is what lets a test assert the
	// teardown path drained rather than assume it.
	inflight sync.WaitGroup
	stalled  atomic.Int64

	closeOnce sync.Once
	closeErr  error
	closed    chan struct{}
}

// New wraps an already-open SFTP byte stream. The stream is adopted: closing
// the Client closes it.
//
// New is unbounded and blocks for as long as the handshake takes — see
// NewContext, which is what callers on a worker pool should use.
func New(rwc io.ReadWriteCloser) (*Client, error) {
	sc, err := sftp.NewClientPipe(rwc, rwc)
	if err != nil {
		return nil, fmt.Errorf("sftpfs: start sftp client: %w", err)
	}
	_, hasPosixRename := sc.HasExtension(posixRenameExt)
	return &Client{
		sc:          sc,
		maxEntries:  MaxEntries,
		posixRename: hasPosixRename,
		closed:      make(chan struct{}),
	}, nil
}

// openResult is what a bounded open eventually produces, whether or not anyone
// is still waiting for it.
type openResult struct {
	c   *Client
	err error
}

// NewContext wraps an already-open SFTP byte stream, bounding the handshake by
// ctx.
//
// The handshake needs a bound of its own because it has none: NewClientPipe
// sends INIT and then waits for VERSION in a bare Read, with no context and no
// deadline anywhere under it (pkg/sftp's recvVersion). Against a host that is
// reachable and answers SSH but never gets sftp-server running — not installed,
// or stuck coming up — that read never returns. It happens in the constructor,
// so it is outside do(): not counted as an orphan, and never trips the channel
// teardown either. It is precisely the wedge this package exists to remove,
// sitting in the one place the package could not see it.
//
// On timeout the stream is closed, and that is the load-bearing half. Merely
// returning would free the caller and leave the handshake goroutine parked on
// the same read forever; closing the stream makes that read return EOF, so the
// goroutine unwinds too. The stream is adopted either way, so closing it is
// this function's to do.
func NewContext(ctx context.Context, rwc io.ReadWriteCloser) (*Client, error) {
	return newContext(ctx, &handshakeGuard{ReadWriteCloser: rwc})
}

// newContext is NewContext with the stream already wrapped, so that OpenContext
// can hand the same wrapper to its abandon path rather than the raw stream.
func newContext(ctx context.Context, g *handshakeGuard) (*Client, error) {
	done := make(chan openResult, 1)
	go func() {
		c, err := New(g)
		g.disarm()
		done <- openResult{c: c, err: err}
	}()
	select {
	case r := <-done:
		return r.c, r.err
	case <-ctx.Done():
		_ = g.Close()
		// The handshake may still have won the race; if it did, its client owns
		// the stream and has to be closed rather than dropped.
		go func() {
			if r := <-done; r.c != nil {
				_ = r.c.Close()
			}
		}()
		return nil, ctx.Err()
	}
}

// handshakeGuard serializes the handshake's one write against the Close that
// interrupts it.
//
// The interrupting Close is what makes the bound above real, and it arrives
// from a different goroutine than the write — which is a data race, not a
// theoretical one: x/crypto/ssh keeps the channel's sentEOF flag as a plain
// bool that Write reads and CloseWrite writes, with no lock between them, so
// there is no happens-before edge at all and the detector flags the pair
// whatever the timing. pkg/sftp has the same requirement and meets it the same
// way, taking its connection lock in both sendPacket and Close.
//
// The guard disarms once the handshake is over, and that is deliberate rather
// than tidiness: from then on pkg/sftp's own lock provides the ordering, and a
// guard still in the path would make Close wait behind a multi-megabyte write
// to a host that has stopped reading — which is precisely the teardown the
// stalled-operation cap needs to be able to force through.
//
// Only the write side is guarded: the read the timeout has to interrupt is
// exactly the one that must stay interruptible, so Read is never behind the
// lock.
type handshakeGuard struct {
	io.ReadWriteCloser
	mu  sync.Mutex
	off atomic.Bool
}

func (g *handshakeGuard) disarm() { g.off.Store(true) }

func (g *handshakeGuard) Write(p []byte) (int, error) {
	if g.off.Load() {
		return g.ReadWriteCloser.Write(p)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.ReadWriteCloser.Write(p)
}

func (g *handshakeGuard) Close() error {
	if g.off.Load() {
		return g.ReadWriteCloser.Close()
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.ReadWriteCloser.Close()
}

// Open starts SFTP on an existing SSH connection. It rides the connection the
// host already has — including, transparently, one that is the far end of a
// jump-host chain — rather than dialing a second one, which would cost another
// login and another keepalive on the remote side for no benefit.
//
// Open is unbounded; prefer OpenContext.
func Open(conn *sshclient.Conn) (*Client, error) {
	stream, err := conn.OpenSFTP()
	if err != nil {
		return nil, err
	}
	c, err := New(stream)
	if err != nil {
		_ = stream.Close()
		return nil, err
	}
	return c, nil
}

// OpenContext is Open under a deadline, and is what the desktop should call.
//
// Bounding this one matters more than it looks. Done() means "redial before the
// next operation", so opening is the redial path: a client torn down for
// stalling is reopened here, and an open that never returns burns the worker
// slot that a torn-down client was supposed to free.
//
// Both halves are covered, because both can hang against the same host. The
// subsystem request waits for a channel reply that a wedged sshd need never
// send; the handshake behind it waits for VERSION (see NewContext). A caller
// that bounded only the second would still park on the first.
func OpenContext(ctx context.Context, conn *sshclient.Conn) (*Client, error) {
	// streams publishes the stream the moment it exists, so the abandon path
	// below can close it even while this goroutine is still inside OpenSFTP. It
	// publishes the guard rather than the raw stream, so that close is ordered
	// against the handshake's write like every other one.
	streams := make(chan io.Closer, 1)
	done := make(chan openResult, 1)
	go func() {
		stream, err := conn.OpenSFTP()
		if err != nil {
			done <- openResult{err: err}
			return
		}
		g := &handshakeGuard{ReadWriteCloser: stream}
		streams <- g
		c, err := newContext(ctx, g)
		if err != nil {
			_ = g.Close()
			done <- openResult{err: err}
			return
		}
		done <- openResult{c: c}
	}()
	select {
	case r := <-done:
		return r.c, r.err
	case <-ctx.Done():
		go abandonOpen(streams, done)
		return nil, ctx.Err()
	}
}

// abandonOpen cleans up behind an open whose caller has given up. It closes the
// stream — ending the subsystem channel, and with it any read parked on the
// other side of it — and closes the client instead if the open turned out to
// win the race. Closing the stream does not touch the Conn, which is shared
// with the terminal and every tunnel on the host.
//
// If the goroutine is wedged in OpenSFTP itself, nothing has been created yet
// to close; this watcher waits for whichever of the two arrives and cleans that
// up. It is one parked goroutine, not a growing set.
func abandonOpen(streams <-chan io.Closer, done <-chan openResult) {
	select {
	case s := <-streams:
		_ = s.Close()
	case r := <-done:
		if r.c != nil {
			_ = r.c.Close()
		}
		return
	}
	if r := <-done; r.c != nil {
		_ = r.c.Close()
	}
}

// Done is closed when the client is finished — either because Close was called
// or because too many operations stalled and the channel was torn down. A
// caller holding a Client should treat this as "redial before the next
// operation".
func (c *Client) Done() <-chan struct{} { return c.closed }

// Close ends the SFTP channel and the stream under it. Safe to call
// repeatedly and from several goroutines.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.closeErr = c.sc.Close()
	})
	return c.closeErr
}

// do runs one operation on its own goroutine so the caller can leave the
// moment ctx is done, rather than when the far side gets around to answering.
//
// The goroutine that is left behind is accounted for, not ignored: see
// stalledOpCap.
func (c *Client) do(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	done := make(chan error, 1)
	// Two counts, because a cancelled operation leaves two goroutines behind:
	// the one running fn, and the watcher orphaned() starts, which parks on the
	// same response. The second count is handed to orphaned() on the
	// cancellation path and released immediately on the normal one, so it is
	// reserved before either goroutine can be observed.
	c.inflight.Add(2)
	go func() {
		defer c.inflight.Done()
		done <- fn()
	}()
	select {
	case err := <-done:
		c.inflight.Done() // no watcher will be started
		return err
	case <-ctx.Done():
		c.orphaned(done)
		return ctx.Err()
	}
}

// orphaned watches an operation whose caller has walked away while its round
// trip is still outstanding, and tears the channel down once enough of them
// have failed to finish for the channel itself to be the likely explanation.
//
// It consumes the second inflight count do() reserved.
func (c *Client) orphaned(done <-chan error) {
	go func() {
		defer c.inflight.Done()
		select {
		case <-done:
			// Finished right behind its caller. Nothing to account for.
			return
		case <-time.After(orphanGrace):
		}
		if c.stalled.Add(1) >= stalledOpCap {
			// Asynchronously: Close waits for the client's reader goroutine to
			// finish, and closing is what is supposed to make that happen.
			go c.Close()
		}
		<-done
		c.stalled.Add(-1)
	}()
}

// ---------------------------------------------------------------------------
// reads
// ---------------------------------------------------------------------------

// ListDir lists a remote directory, capped at the client's entry limit.
//
// The cap is applied after the listing is collected rather than by stopping
// early, so Total is the real number of entries and the caller can report the
// shortfall honestly. ctx is passed down into the listing loop as well, so a
// cancelled listing of a huge directory stops asking for more pages instead of
// running to completion for a caller who has gone.
//
// It stops asking, and then it blocks. ReadDirContext closes the directory
// handle on its way out with a hard-coded context.Background() (pkg/sftp
// client.go), so against a host that has stopped answering, a cancellation
// landing after opendir has succeeded — which is the whole of the paging phase,
// and the reason ctx is threaded here at all — parks the goroutine on that
// CLOSE exactly as FileMeta's parks on a Stat. The caller still returns on its
// own deadline; what is left behind is a genuine orphan, reaped by the
// machinery around stalledOpCap rather than unwinding by itself. Only a
// cancellation during opendir gets out cleanly, because then there is no handle
// to close.
func (c *Client) ListDir(ctx context.Context, dir string) (Listing, error) {
	p, err := cleanPath(dir)
	if err != nil {
		return Listing{}, err
	}
	var infos []os.FileInfo
	if err := c.do(ctx, func() error {
		var derr error
		infos, derr = c.sc.ReadDirContext(ctx, p)
		// Servers word "you asked me to list a file" differently, and some only
		// say "failure". Clicking a file in a tree is a normal enough mistake
		// to be worth one extra round trip on the error path to name it
		// properly — but only while the caller is still waiting, since Stat
		// takes no context and would undo the cancellation this whole layer
		// exists to honour.
		if derr != nil && !errors.Is(derr, os.ErrNotExist) && ctx.Err() == nil {
			if info, serr := c.sc.Stat(p); serr == nil && !info.IsDir() {
				return ErrNotADirectory
			}
		}
		return derr
	}); err != nil {
		return Listing{}, translate("list", p, err)
	}

	total := len(infos)
	truncated := false
	if total > c.maxEntries {
		infos = infos[:c.maxEntries]
		truncated = true
	}
	entries := make([]DirEntry, 0, len(infos))
	for _, fi := range infos {
		entries = append(entries, DirEntry{
			Name:    fi.Name(),
			IsDir:   fi.IsDir(),
			Size:    fi.Size(),
			ModTime: fi.ModTime().UnixMilli(),
		})
	}
	return Listing{Path: p, Entries: entries, Truncated: truncated, Total: total}, nil
}

// FileMeta stats a remote path and, for regular files, probes the head of the
// file for NUL bytes to classify it as binary — the same heuristic the local
// executor uses, so the two sources agree about what is displayable.
func (c *Client) FileMeta(ctx context.Context, name string) (FileMetaInfo, error) {
	p, err := cleanPath(name)
	if err != nil {
		return FileMetaInfo{}, err
	}
	var out FileMetaInfo
	if err := c.do(ctx, func() error {
		info, err := c.sc.Stat(p)
		if err != nil {
			return err
		}
		out = FileMetaInfo{Path: p, Size: info.Size(), ModTime: info.ModTime().UnixMilli()}
		if info.IsDir() || info.Size() == 0 {
			return nil
		}
		f, err := c.sc.Open(p)
		if err != nil {
			// Metadata is still useful without the binary verdict; an
			// unreadable file is not a reason to fail the stat.
			return nil
		}
		defer f.Close()
		probe := make([]byte, binaryProbeBytes)
		n, rerr := io.ReadFull(f, probe)
		if rerr != nil && !errors.Is(rerr, io.EOF) && !errors.Is(rerr, io.ErrUnexpectedEOF) {
			return nil
		}
		out.IsBinary = isBinary(probe[:n])
		return nil
	}); err != nil {
		return FileMetaInfo{}, translate("stat", p, err)
	}
	return out, nil
}

// ReadChunk reads up to length bytes from offset. length is clamped to
// readChunkMax rather than rejected, matching the local executor.
func (c *Client) ReadChunk(ctx context.Context, name string, offset, length int64) (Chunk, error) {
	if offset < 0 {
		return Chunk{}, fmt.Errorf("sftpfs: offset must be non-negative")
	}
	if length < 0 {
		return Chunk{}, fmt.Errorf("sftpfs: length must be non-negative")
	}
	if length > readChunkMax {
		length = readChunkMax
	}
	p, err := cleanPath(name)
	if err != nil {
		return Chunk{}, err
	}

	var out Chunk
	if err := c.do(ctx, func() error {
		info, err := c.sc.Stat(p)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return ErrIsDirectory
		}
		data := []byte{}
		var rerr error
		// A zero-length read must not reach ReadAt: an empty buffer makes it
		// ambiguous whether io.EOF means "end of file" or "nothing asked for",
		// and reporting EOF at offset 0 of a large file would tell a chunked
		// download it was finished.
		if length > 0 {
			f, err := c.sc.Open(p)
			if err != nil {
				return err
			}
			defer f.Close()
			buf := make([]byte, length)
			var n int
			n, rerr = f.ReadAt(buf, offset)
			if rerr != nil && !errors.Is(rerr, io.EOF) {
				return rerr
			}
			data = buf[:n]
		}
		n := len(data)
		contentType := "application/octet-stream"
		if len(data) > 0 {
			contentType = http.DetectContentType(data)
		}
		out = Chunk{
			Path:        p,
			Offset:      offset,
			Data:        data,
			ContentType: contentType,
			EOF:         errors.Is(rerr, io.EOF) || offset+int64(n) >= info.Size(),
		}
		return nil
	}); err != nil {
		return Chunk{}, translate("read", p, err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// writes
// ---------------------------------------------------------------------------

// WriteFile uploads data to a remote path.
//
// Overwrite semantics are the point of this function, and they are stricter
// than the local executor's on purpose. Locally, expectedModTime==0 means
// "skip the compare-and-swap and just write"; here it means "I believe nothing
// is there", and if something is there the write is refused with
// ErrAlreadyExists. There is no trash and no versioning on the far side, so a
// mistaken overwrite is unrecoverable, while a refusal costs the user one
// confirmation. To overwrite deliberately, pass the ModTime you last saw —
// which is the same optimistic-concurrency handle the local path already uses,
// not a second mechanism.
//
//	target missing, createIfMissing  -> created
//	target missing, !createIfMissing -> ErrNotFound
//	target exists, expectedModTime 0 -> ErrAlreadyExists  (refuse, don't clobber)
//	target exists, modTime mismatch  -> ErrStaleModTime
//	target exists, modTime matches   -> overwritten
//	target is a directory            -> ErrIsDirectory
//
// Note that SFTP v3 carries mtime in whole seconds, so ModTime here is always
// second-granular. Two writes inside the same second are indistinguishable to
// the CAS — an inherent limit of the protocol version, not something this
// layer can paper over.
//
// The write itself stages into a temp file beside the target and renames it
// into place, so a failure partway through leaves the original intact rather
// than a half-written file. When overwriting, the target's existing mode is
// carried onto the replacement: staging through a fresh file otherwise turns a
// 0600 file into whatever the remote umask says, which is a permission change
// nobody asked for and nobody would notice.
//
// A cancellation that lands exactly as the write completes is reported as a
// cancellation even though the bytes went through. That ambiguity is inherent,
// and it fails in the safe direction: retrying the upload then finds the path
// occupied and is refused rather than writing twice.
func (c *Client) WriteFile(ctx context.Context, name string, data []byte, expectedModTime int64, createIfMissing bool) (FileMetaInfo, error) {
	p, err := cleanPath(name)
	if err != nil {
		return FileMetaInfo{}, err
	}
	if int64(len(data)) > maxWriteBytesHard {
		return FileMetaInfo{}, fmt.Errorf("write %s: write_denied: exceeds %d bytes", p, maxWriteBytesHard)
	}

	var out FileMetaInfo
	if err := c.do(ctx, func() error {
		existed := false
		info, statErr := c.sc.Stat(p)
		switch {
		case statErr == nil:
			if info.IsDir() {
				return ErrIsDirectory
			}
			if expectedModTime == 0 {
				return ErrAlreadyExists
			}
			if info.ModTime().UnixMilli() != expectedModTime {
				return fmt.Errorf("%w: current=%d", ErrStaleModTime, info.ModTime().UnixMilli())
			}
			existed = true
		case errors.Is(statErr, os.ErrNotExist):
			if !createIfMissing {
				return ErrNotFound
			}
		default:
			return statErr
		}

		// Every write_denied below names the target rather than the staging file
		// it may literally have failed on. That is deliberate: the staging file
		// is an implementation detail with a random name, and translate attaches
		// the path the user asked about — the same thing os.WriteFile's
		// *PathError would have said.
		tmp, err := c.stage(path.Dir(p), data)
		if err != nil {
			return err
		}
		committed := false
		defer func() {
			if !committed {
				_ = c.sc.Remove(tmp)
			}
		}()

		if existed {
			if err := c.sc.Chmod(tmp, info.Mode().Perm()); err != nil {
				return fmt.Errorf("write_denied: %w", err)
			}
		}

		if !existed {
			// Plain rename refuses an existing target in SFTP v3, which is
			// exactly what is wanted here: if something appeared at the path
			// between the stat above and now, the upload fails rather than
			// destroying it.
			if err := c.sc.Rename(tmp, p); err != nil {
				return fmt.Errorf("write_denied: %w", err)
			}
		} else if c.posixRename {
			if err := c.sc.PosixRename(tmp, p); err != nil {
				return fmt.Errorf("write_denied: %w", err)
			}
		} else {
			// No atomic overwrite available. Unlink first, accepting a narrow
			// window where the path does not exist; the alternative is not
			// supporting overwrite at all on those servers.
			if err := c.sc.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("write_denied: %w", err)
			}
			if err := c.sc.Rename(tmp, p); err != nil {
				return fmt.Errorf("write_denied: %w", err)
			}
		}
		committed = true

		newInfo, err := c.sc.Stat(p)
		if err != nil {
			return err
		}
		out = FileMetaInfo{
			Path:     p,
			Size:     newInfo.Size(),
			ModTime:  newInfo.ModTime().UnixMilli(),
			IsBinary: isBinary(data),
		}
		return nil
	}); err != nil {
		return FileMetaInfo{}, translate("write", p, err)
	}
	return out, nil
}

// stage writes data to a fresh temp file in dir and returns its path. The name
// is random rather than sequential so two uploads into the same directory,
// possibly from two different clients, cannot collide.
func (c *Client) stage(dir string, data []byte) (string, error) {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("write_denied: %w", err)
	}
	tmp := path.Join(dir, tempPrefix+hex.EncodeToString(suffix[:]))
	f, err := c.sc.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return "", fmt.Errorf("write_denied: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = c.sc.Remove(tmp)
		return "", fmt.Errorf("write_denied: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = c.sc.Remove(tmp)
		return "", fmt.Errorf("write_denied: %w", err)
	}
	return tmp, nil
}

// CreateFile creates an empty file, failing if anything is already there.
func (c *Client) CreateFile(ctx context.Context, name string) (FileMetaInfo, error) {
	p, err := cleanPath(name)
	if err != nil {
		return FileMetaInfo{}, err
	}
	var out FileMetaInfo
	if err := c.do(ctx, func() error {
		if _, err := c.sc.Stat(p); err == nil {
			return ErrAlreadyExists
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		f, err := c.sc.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		if err != nil {
			return fmt.Errorf("write_denied: %w", err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("write_denied: %w", err)
		}
		info, err := c.sc.Stat(p)
		if err != nil {
			return err
		}
		out = FileMetaInfo{Path: p, Size: info.Size(), ModTime: info.ModTime().UnixMilli()}
		return nil
	}); err != nil {
		return FileMetaInfo{}, translate("create", p, err)
	}
	return out, nil
}

// Rename moves from to to. An existing target is refused, never replaced —
// same reasoning as WriteFile.
//
// Failures on the destination side are labelled with the destination. Blaming
// the source for "the target already exists", or for a permission error on the
// directory being moved into, tells the user to go and look at the wrong file.
func (c *Client) Rename(ctx context.Context, from, to string) (FileMetaInfo, error) {
	src, err := cleanPath(from)
	if err != nil {
		return FileMetaInfo{}, err
	}
	dst, err := cleanPath(to)
	if err != nil {
		return FileMetaInfo{}, err
	}
	var out FileMetaInfo
	if err := c.do(ctx, func() error {
		if _, err := c.sc.Stat(src); errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if _, err := c.sc.Stat(dst); err == nil {
			return &pathError{path: dst, err: ErrAlreadyExists}
		} else if !errors.Is(err, os.ErrNotExist) {
			return &pathError{path: dst, err: err}
		}
		if err := c.sc.Rename(src, dst); err != nil {
			return fmt.Errorf("write_denied: %w", err)
		}
		info, err := c.sc.Stat(dst)
		if err != nil {
			return &pathError{path: dst, err: err}
		}
		out = FileMetaInfo{Path: dst, Size: info.Size(), ModTime: info.ModTime().UnixMilli()}
		return nil
	}); err != nil {
		return FileMetaInfo{}, translate("rename", src, err)
	}
	return out, nil
}

// Remove deletes a path. There is no trash to fall back on here — the local
// executor's trashPath has no remote equivalent — so a caller offering this
// should say so.
func (c *Client) Remove(ctx context.Context, name string, recursive bool) error {
	p, err := cleanPath(name)
	if err != nil {
		return err
	}
	if err := c.do(ctx, func() error {
		if _, err := c.sc.Stat(p); errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if recursive {
			if err := c.sc.RemoveAll(p); err != nil {
				return fmt.Errorf("write_denied: %w", err)
			}
			return nil
		}
		if err := c.sc.Remove(p); err != nil {
			return fmt.Errorf("write_denied: %w", err)
		}
		return nil
	}); err != nil {
		return translate("remove", p, err)
	}
	return nil
}

// Mkdir creates a single directory; the parent must already exist.
func (c *Client) Mkdir(ctx context.Context, name string) (FileMetaInfo, error) {
	p, err := cleanPath(name)
	if err != nil {
		return FileMetaInfo{}, err
	}
	var out FileMetaInfo
	if err := c.do(ctx, func() error {
		if _, err := c.sc.Stat(p); err == nil {
			return ErrAlreadyExists
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := c.sc.Mkdir(p); err != nil {
			return fmt.Errorf("write_denied: %w", err)
		}
		info, err := c.sc.Stat(p)
		if err != nil {
			return err
		}
		out = FileMetaInfo{Path: p, Size: info.Size(), ModTime: info.ModTime().UnixMilli()}
		return nil
	}); err != nil {
		return FileMetaInfo{}, translate("mkdir", p, err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// cleanPath normalizes a remote path. It uses path, not filepath: these are
// POSIX paths on the far side regardless of what this binary is running on,
// and filepath would mangle them on Windows.
//
// Absolute is required because SFTP resolves a relative path against the
// server's idea of a working directory, which is not something either side
// here has agreed on.
func cleanPath(p string) (string, error) {
	if !path.IsAbs(p) {
		return "", fmt.Errorf("%w: %s", ErrPathRelative, p)
	}
	return path.Clean(p), nil
}

// pathError carries the path an operation failed on out of the operation's
// closure, for the cases where it is not the path the operation was named with
// — Rename's destination, mainly.
//
// It exists rather than a variable captured by the closure because the closure
// runs on its own goroutine and may still be running when a cancelled caller
// reads the result: a captured variable would be a data race on exactly the
// path this package is built to handle.
type pathError struct {
	path string
	err  error
}

func (e *pathError) Error() string { return e.err.Error() }
func (e *pathError) Unwrap() error { return e.err }

// translate maps the transport's errors onto this package's vocabulary, so
// callers can branch on ErrNotFound rather than on os.ErrNotExist leaking out
// of a dependency, and names the operation and the path in the message.
//
// Naming both is not decoration. The local executor gets it for nothing —
// os.Stat returns a *PathError, so a permission failure reads "stat
// /srv/app.conf: permission denied". The same failure over SFTP arrives as
// `sftp: "Permission denied" (SSH_FX_PERMISSION_DENIED)`: no path, no
// operation, in a UI where several files can be in flight at once and the
// message is all the user gets. The sentinels are therefore returned bare from
// the operation closures and dressed here, in one place, rather than each site
// remembering to append its own path — and the transport's own errors, which
// carry no path at all, get the same treatment for free.
func translate(op, p string, err error) error {
	if err == nil {
		return nil
	}
	var pe *pathError
	if errors.As(err, &pe) {
		p, err = pe.path, pe.err
	}
	if errors.Is(err, os.ErrNotExist) {
		err = ErrNotFound
	}
	return fmt.Errorf("%s %s: %w", op, p, err)
}

func isBinary(data []byte) bool {
	probe := data
	if len(probe) > binaryProbeBytes {
		probe = probe[:binaryProbeBytes]
	}
	for _, b := range probe {
		if b == 0 {
			return true
		}
	}
	return false
}
