package sftpfs

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attson/atterm/internal/sshclient"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// ---------------------------------------------------------------------------
// harness
// ---------------------------------------------------------------------------

// gatedPipe is the client end of an in-memory connection to a test SFTP
// server, with a valve on the client→server direction. While stalled, packets
// the client writes are held rather than delivered, so the server never sees
// the request and therefore never answers it.
//
// That is the failure this package has to survive: a host that is still
// connected and simply stops answering. It is not the same as "the server is
// slow" and it is not reproducible by just not starting a server — the
// connection must come up, complete the SFTP handshake, and only then go
// quiet, because that is when there is an operation to cancel.
type gatedPipe struct {
	net.Conn
	mu      sync.Mutex
	stalled bool
	pass    int // requests still let through before the valve shuts
	held    [][]byte
	holds   chan struct{} // one send per held write
}

func (g *gatedPipe) Write(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stalled && g.pass > 0 {
		g.pass--
		return g.Conn.Write(p)
	}
	if g.stalled {
		g.held = append(g.held, append([]byte(nil), p...))
		select {
		case g.holds <- struct{}{}:
		default:
		}
		return len(p), nil
	}
	return g.Conn.Write(p)
}

func (g *gatedPipe) stall() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stalled = true
	g.pass = 0
}

// stallAfter closes the valve only once n more requests have gone through, and
// returns a channel that fires as each later one is held.
//
// It is what lets a test cancel at a chosen point in a multi-round-trip
// operation: "the first request was held" is proof that everything before it
// was not merely sent but answered, since the client would not have moved on
// otherwise.
func (g *gatedPipe) stallAfter(n int) <-chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stalled = true
	g.pass = n
	return g.holds
}

// release re-opens the valve and delivers everything held, so a test can show
// that an operation the caller walked away from still completes underneath —
// and that the client survived it.
func (g *gatedPipe) release() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stalled = false
	held := g.held
	g.held = nil
	for _, p := range held {
		if _, err := g.Conn.Write(p); err != nil {
			return err
		}
	}
	return nil
}

// newTestClient wires a Client to an in-process pkg/sftp server over net.Pipe.
// No SSH anywhere: this package is separate from sshclient exactly so its
// protocol behaviour can be tested without one.
func newTestClient(t *testing.T) (*Client, string, *gatedPipe) {
	t.Helper()
	root := t.TempDir()
	// EvalSymlinks because on macOS t.TempDir() lives under /var, a symlink to
	// /private/var, and the server reports back the resolved path.
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	clientEnd, serverEnd := net.Pipe()
	srv, err := sftp.NewServer(serverEnd)
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan struct{})
	go func() { defer close(served); _ = srv.Serve(); serverEnd.Close() }()

	gated := &gatedPipe{Conn: clientEnd, holds: make(chan struct{}, 8)}
	c, err := New(gated)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close()
		select {
		case <-served:
		case <-time.After(5 * time.Second):
			t.Error("sftp test server did not stop")
		}
	})
	return c, filepath.ToSlash(resolved), gated
}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readBack(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// awaitStalled polls the stalled counter, which is how a test observes that an
// abandoned operation is still parked: the counter only rises once the
// operation has failed to finish within orphanGrace.
func awaitStalled(n *atomic.Int64, want int64, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if n.Load() == want {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return n.Load() == want
}

func waitGroup(wg *sync.WaitGroup, d time.Duration) bool {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// ---------------------------------------------------------------------------
// listing
// ---------------------------------------------------------------------------

func TestListDirReturnsEntries(t *testing.T) {
	c, root, _ := newTestClient(t)
	mustWrite(t, filepath.Join(root, "a.txt"), "hello")
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := c.ListDir(context.Background(), root)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if got.Truncated {
		t.Error("Truncated = true for a 2-entry directory")
	}
	if got.Total != 2 {
		t.Errorf("Total = %d, want 2", got.Total)
	}
	names := map[string]DirEntry{}
	for _, e := range got.Entries {
		names[e.Name] = e
	}
	if len(names) != 2 {
		t.Fatalf("entries = %+v, want 2", got.Entries)
	}
	if e := names["a.txt"]; e.IsDir || e.Size != 5 || e.ModTime == 0 {
		t.Errorf("a.txt = %+v, want file of size 5 with a modTime", e)
	}
	if e := names["sub"]; !e.IsDir {
		t.Errorf("sub = %+v, want a directory", e)
	}
}

// TestListDirCapsAndReportsTruncation is §5.2: a user shown 3 of 10 files with
// no indication is the worst outcome, so the cap must come with a flag and the
// real count.
func TestListDirCapsAndReportsTruncation(t *testing.T) {
	c, root, _ := newTestClient(t)
	c.maxEntries = 3
	for i := 0; i < 10; i++ {
		mustWrite(t, filepath.Join(root, fmt.Sprintf("f%02d", i)), "x")
	}

	got, err := c.ListDir(context.Background(), root)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(got.Entries) != 3 {
		t.Errorf("len(Entries) = %d, want 3", len(got.Entries))
	}
	if !got.Truncated {
		t.Error("Truncated = false; the caller cannot tell it is missing 7 entries")
	}
	if got.Total != 10 {
		t.Errorf("Total = %d, want 10 (the real count, so the UI can say '3 of 10')", got.Total)
	}
}

func TestDefaultEntryCap(t *testing.T) {
	c, _, _ := newTestClient(t)
	if c.maxEntries != MaxEntries {
		t.Fatalf("maxEntries = %d, want MaxEntries", c.maxEntries)
	}
	if MaxEntries != 2000 {
		t.Fatalf("MaxEntries = %d, want 2000", MaxEntries)
	}
}

func TestListDirOnMissingPathIsNotFound(t *testing.T) {
	c, root, _ := newTestClient(t)
	_, err := c.ListDir(context.Background(), path.Join(root, "nope"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListDirOnAFileIsNotADirectory(t *testing.T) {
	c, root, _ := newTestClient(t)
	mustWrite(t, filepath.Join(root, "f"), "x")
	_, err := c.ListDir(context.Background(), path.Join(root, "f"))
	if !errors.Is(err, ErrNotADirectory) {
		t.Fatalf("err = %v, want ErrNotADirectory", err)
	}
}

func TestOperationsRejectRelativePaths(t *testing.T) {
	c, _, _ := newTestClient(t)
	ctx := context.Background()
	if _, err := c.ListDir(ctx, "relative/dir"); !errors.Is(err, ErrPathRelative) {
		t.Errorf("ListDir err = %v, want ErrPathRelative", err)
	}
	if _, err := c.FileMeta(ctx, "relative/file"); !errors.Is(err, ErrPathRelative) {
		t.Errorf("FileMeta err = %v, want ErrPathRelative", err)
	}
	if _, err := c.WriteFile(ctx, "relative/file", nil, 0, true); !errors.Is(err, ErrPathRelative) {
		t.Errorf("WriteFile err = %v, want ErrPathRelative", err)
	}
}

// ---------------------------------------------------------------------------
// metadata and reads
// ---------------------------------------------------------------------------

func TestFileMeta(t *testing.T) {
	c, root, _ := newTestClient(t)
	mustWrite(t, filepath.Join(root, "text"), "plain text")
	if err := os.WriteFile(filepath.Join(root, "bin"), []byte{'a', 0x00, 'b'}, 0o644); err != nil {
		t.Fatal(err)
	}

	text, err := c.FileMeta(context.Background(), path.Join(root, "text"))
	if err != nil {
		t.Fatalf("FileMeta(text): %v", err)
	}
	if text.Size != 10 || text.IsBinary || text.ModTime == 0 {
		t.Errorf("text meta = %+v, want size 10, not binary, with a modTime", text)
	}

	bin, err := c.FileMeta(context.Background(), path.Join(root, "bin"))
	if err != nil {
		t.Fatalf("FileMeta(bin): %v", err)
	}
	if !bin.IsBinary {
		t.Errorf("bin meta = %+v, want IsBinary", bin)
	}
}

func TestReadChunkReadsAtOffsetAndReportsEOF(t *testing.T) {
	c, root, _ := newTestClient(t)
	body := strings.Repeat("abcdefghij", 100) // 1000 bytes
	mustWrite(t, filepath.Join(root, "big"), body)
	p := path.Join(root, "big")

	head, err := c.ReadChunk(context.Background(), p, 0, 100)
	if err != nil {
		t.Fatalf("ReadChunk head: %v", err)
	}
	if string(head.Data) != body[:100] {
		t.Errorf("head data mismatch")
	}
	if head.EOF {
		t.Error("head EOF = true at offset 0 of a 1000-byte file")
	}
	if head.ContentType == "" {
		t.Error("ContentType empty")
	}

	tail, err := c.ReadChunk(context.Background(), p, 990, 100)
	if err != nil {
		t.Fatalf("ReadChunk tail: %v", err)
	}
	if string(tail.Data) != body[990:] {
		t.Errorf("tail data = %q, want %q", tail.Data, body[990:])
	}
	if !tail.EOF {
		t.Error("tail EOF = false at the end of the file")
	}
	if tail.Offset != 990 {
		t.Errorf("Offset = %d, want 990", tail.Offset)
	}
}

// TestReadChunkZeroLengthDoesNotClaimEOF guards a chunked download against
// being told it is finished when it merely asked for nothing.
func TestReadChunkZeroLengthDoesNotClaimEOF(t *testing.T) {
	c, root, _ := newTestClient(t)
	mustWrite(t, filepath.Join(root, "big"), strings.Repeat("x", 1000))

	got, err := c.ReadChunk(context.Background(), path.Join(root, "big"), 0, 0)
	if err != nil {
		t.Fatalf("ReadChunk: %v", err)
	}
	if len(got.Data) != 0 {
		t.Errorf("Data = %q, want empty", got.Data)
	}
	if got.EOF {
		t.Error("EOF = true for a zero-length read at offset 0 of a 1000-byte file")
	}
}

func TestReadChunkRejectsDirectoryAndBadArgs(t *testing.T) {
	c, root, _ := newTestClient(t)
	ctx := context.Background()
	if _, err := c.ReadChunk(ctx, root, 0, 10); !errors.Is(err, ErrIsDirectory) {
		t.Errorf("ReadChunk(dir) err = %v, want ErrIsDirectory", err)
	}
	mustWrite(t, filepath.Join(root, "f"), "x")
	if _, err := c.ReadChunk(ctx, path.Join(root, "f"), -1, 10); err == nil {
		t.Error("negative offset accepted")
	}
	if _, err := c.ReadChunk(ctx, path.Join(root, "f"), 0, -1); err == nil {
		t.Error("negative length accepted")
	}
}

// ---------------------------------------------------------------------------
// the write path — §5.1
// ---------------------------------------------------------------------------

func TestWriteFileCreatesMissingFile(t *testing.T) {
	c, root, _ := newTestClient(t)
	p := path.Join(root, "new.txt")

	meta, err := c.WriteFile(context.Background(), p, []byte("fresh"), 0, true)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if meta.Size != 5 {
		t.Errorf("Size = %d, want 5", meta.Size)
	}
	if got := readBack(t, filepath.Join(root, "new.txt")); got != "fresh" {
		t.Errorf("on-disk = %q, want %q", got, "fresh")
	}
}

func TestWriteFileOnMissingPathWithoutCreateIsNotFound(t *testing.T) {
	c, root, _ := newTestClient(t)
	_, err := c.WriteFile(context.Background(), path.Join(root, "ghost"), []byte("x"), 0, false)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestWriteFileRefusesExistingPathByDefault is §5.1. There is no trash and no
// versioning on a remote host, so an upload onto an existing path must fail
// rather than destroy what is there. The assertion that matters is the second
// one: the bytes on the far side are untouched.
func TestWriteFileRefusesExistingPathByDefault(t *testing.T) {
	c, root, _ := newTestClient(t)
	disk := filepath.Join(root, "keep.txt")
	mustWrite(t, disk, "original")

	_, err := c.WriteFile(context.Background(), path.Join(root, "keep.txt"), []byte("REPLACED"), 0, true)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("err = %v, want ErrAlreadyExists", err)
	}
	if got := readBack(t, disk); got != "original" {
		t.Fatalf("on-disk = %q; the refusal silently overwrote the file", got)
	}
	assertNoTempLeftovers(t, root)
}

func TestWriteFileOverwritesWhenModTimeMatches(t *testing.T) {
	c, root, _ := newTestClient(t)
	if !c.posixRename {
		t.Fatal("test server does not advertise posix-rename; this test would silently exercise the fallback instead")
	}
	disk := filepath.Join(root, "doc.txt")
	mustWrite(t, disk, "original")

	before, err := c.FileMeta(context.Background(), path.Join(root, "doc.txt"))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := c.WriteFile(context.Background(), path.Join(root, "doc.txt"), []byte("REPLACED"), before.ModTime, false)
	if err != nil {
		t.Fatalf("WriteFile with matching modTime: %v", err)
	}
	if meta.Size != 8 {
		t.Errorf("Size = %d, want 8", meta.Size)
	}
	if got := readBack(t, disk); got != "REPLACED" {
		t.Fatalf("on-disk = %q, want REPLACED", got)
	}
	assertNoTempLeftovers(t, root)
}

func TestWriteFileRejectsStaleModTime(t *testing.T) {
	c, root, _ := newTestClient(t)
	disk := filepath.Join(root, "doc.txt")
	mustWrite(t, disk, "original")

	before, err := c.FileMeta(context.Background(), path.Join(root, "doc.txt"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.WriteFile(context.Background(), path.Join(root, "doc.txt"), []byte("REPLACED"), before.ModTime-60_000, false)
	if !errors.Is(err, ErrStaleModTime) {
		t.Fatalf("err = %v, want ErrStaleModTime", err)
	}
	if got := readBack(t, disk); got != "original" {
		t.Fatalf("on-disk = %q; a stale write went through", got)
	}
	assertNoTempLeftovers(t, root)
}

func TestWriteFileRefusesDirectory(t *testing.T) {
	c, root, _ := newTestClient(t)
	if err := os.Mkdir(filepath.Join(root, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := c.WriteFile(context.Background(), path.Join(root, "d"), []byte("x"), 0, true)
	if !errors.Is(err, ErrIsDirectory) {
		t.Fatalf("err = %v, want ErrIsDirectory", err)
	}
}

// TestWriteFileOverwriteWithoutPosixRename covers the servers that do not
// advertise posix-rename@openssh.com: SFTP v3's plain rename refuses an
// existing target, so the overwrite has to unlink first. Same observable
// outcome, one more round trip and a narrow window.
func TestWriteFileOverwriteWithoutPosixRename(t *testing.T) {
	c, root, _ := newTestClient(t)
	c.posixRename = false
	disk := filepath.Join(root, "doc.txt")
	mustWrite(t, disk, "original")

	before, err := c.FileMeta(context.Background(), path.Join(root, "doc.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.WriteFile(context.Background(), path.Join(root, "doc.txt"), []byte("REPLACED"), before.ModTime, false); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := readBack(t, disk); got != "REPLACED" {
		t.Fatalf("on-disk = %q, want REPLACED", got)
	}
	assertNoTempLeftovers(t, root)
}

// TestWriteFileOverwritePreservesMode: staging through a fresh temp file and
// renaming would otherwise hand the target whatever the remote umask produces,
// quietly widening a 0600 file to 0644.
func TestWriteFileOverwritePreservesMode(t *testing.T) {
	for _, posix := range []bool{true, false} {
		name := "posix-rename"
		if !posix {
			name = "unlink-fallback"
		}
		t.Run(name, func(t *testing.T) {
			c, root, _ := newTestClient(t)
			c.posixRename = posix
			disk := filepath.Join(root, "secret")
			mustWrite(t, disk, "original")
			if err := os.Chmod(disk, 0o600); err != nil {
				t.Fatal(err)
			}

			before, err := c.FileMeta(context.Background(), path.Join(root, "secret"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.WriteFile(context.Background(), path.Join(root, "secret"), []byte("REPLACED"), before.ModTime, false); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			info, err := os.Stat(disk)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Fatalf("mode = %o after overwrite, want 600", got)
			}
		})
	}
}

func TestWriteFileRejectsOversizedPayload(t *testing.T) {
	c, root, _ := newTestClient(t)
	_, err := c.WriteFile(context.Background(), path.Join(root, "huge"), make([]byte, maxWriteBytesHard+1), 0, true)
	if err == nil {
		t.Fatal("oversized write accepted")
	}
	assertNoTempLeftovers(t, root)
}

func assertNoTempLeftovers(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var leftovers []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tempPrefix) {
			leftovers = append(leftovers, e.Name())
		}
	}
	if len(leftovers) > 0 {
		sort.Strings(leftovers)
		t.Fatalf("temp files left behind: %v", leftovers)
	}
}

// ---------------------------------------------------------------------------
// the rest of the write path
// ---------------------------------------------------------------------------

func TestCreateFile(t *testing.T) {
	c, root, _ := newTestClient(t)
	meta, err := c.CreateFile(context.Background(), path.Join(root, "empty"))
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if meta.Size != 0 {
		t.Errorf("Size = %d, want 0", meta.Size)
	}
	if _, err := c.CreateFile(context.Background(), path.Join(root, "empty")); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second CreateFile err = %v, want ErrAlreadyExists", err)
	}
}

func TestRename(t *testing.T) {
	c, root, _ := newTestClient(t)
	mustWrite(t, filepath.Join(root, "from"), "body")
	mustWrite(t, filepath.Join(root, "taken"), "other")
	ctx := context.Background()

	if _, err := c.Rename(ctx, path.Join(root, "from"), path.Join(root, "to")); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got := readBack(t, filepath.Join(root, "to")); got != "body" {
		t.Errorf("renamed content = %q", got)
	}

	mustWrite(t, filepath.Join(root, "from"), "body")
	if _, err := c.Rename(ctx, path.Join(root, "from"), path.Join(root, "taken")); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("Rename onto existing err = %v, want ErrAlreadyExists", err)
	}
	if got := readBack(t, filepath.Join(root, "taken")); got != "other" {
		t.Errorf("clobbered the rename target: %q", got)
	}
	if _, err := c.Rename(ctx, path.Join(root, "ghost"), path.Join(root, "x")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Rename of missing source err = %v, want ErrNotFound", err)
	}
}

func TestRemove(t *testing.T) {
	c, root, _ := newTestClient(t)
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Join(root, "tree", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "tree", "inner", "leaf"), "x")

	if err := c.Remove(ctx, path.Join(root, "tree"), false); err == nil {
		t.Error("non-recursive Remove of a non-empty directory succeeded")
	}
	if err := c.Remove(ctx, path.Join(root, "tree"), true); err != nil {
		t.Fatalf("recursive Remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "tree")); !os.IsNotExist(err) {
		t.Error("tree still present after recursive Remove")
	}
	if err := c.Remove(ctx, path.Join(root, "ghost"), false); !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove of missing path err = %v, want ErrNotFound", err)
	}
}

func TestMkdir(t *testing.T) {
	c, root, _ := newTestClient(t)
	ctx := context.Background()
	meta, err := c.Mkdir(ctx, path.Join(root, "d"))
	if err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if meta.Path == "" {
		t.Error("Mkdir returned no path")
	}
	if _, err := c.Mkdir(ctx, path.Join(root, "d")); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second Mkdir err = %v, want ErrAlreadyExists", err)
	}
}

// ---------------------------------------------------------------------------
// cancellation
// ---------------------------------------------------------------------------

// TestCancelledOpReturnsPromptly is the Task 1 review's concern turned into a
// test: the desktop worker pool has four slots per session and no per-request
// timeout, so an operation that never returns permanently eats a slot. Against
// a host that has stopped answering, the caller must come back on its own
// deadline, not on the network's.
func TestCancelledOpReturnsPromptly(t *testing.T) {
	c, root, gated := newTestClient(t)
	gated.stall()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.ListDir(ctx, root)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("ListDir took %s to notice a 50ms deadline", elapsed)
	}
}

// TestCancelledListDirBeforeOpendirLeavesNothingBehind covers the half of
// ListDir's cancellation that really does unwind on its own: the deadline lands
// while opendir is still outstanding, so there is no directory handle yet and
// nothing to close on the way out.
//
// This is the easy half, and on its own it is an unrepresentative sample — see
// TestCancelledListDirAfterOpendirIsAnOrphan for the half that matters, where
// cancellation arrives during paging.
func TestCancelledListDirBeforeOpendirLeavesNothingBehind(t *testing.T) {
	c, root, gated := newTestClient(t)
	gated.stall()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.ListDir(ctx, root); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if !waitGroup(&c.inflight, 2*time.Second) {
		t.Fatal("the cancelled listing's goroutine stayed blocked")
	}
}

// TestCancelledListDirAfterOpendirIsAnOrphan is the representative half.
//
// Threading ctx into ReadDirContext buys a prompt return for the caller and
// nothing more: pkg/sftp closes the directory handle with a hard-coded
// context.Background(), so a cancellation arriving during paging — the entire
// reason ctx is threaded here — leaves the goroutine parked on that CLOSE
// against a host that has stopped answering, exactly like FileMeta's parks on a
// Stat. This is asserted rather than assumed, because the package doc used to
// claim the opposite.
//
// The sequencing is what makes the sample representative: the valve lets the
// OPENDIR through and fires as the first READDIR is held, and the client would
// not have sent that READDIR unless opendir had already come back with a
// handle.
func TestCancelledListDirAfterOpendirIsAnOrphan(t *testing.T) {
	c, root, gated := newTestClient(t)
	mustWrite(t, filepath.Join(root, "a.txt"), "hello")

	held := gated.stallAfter(1) // OPENDIR goes through; READDIR is held
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-held // opendir has answered and paging has begun
		cancel()
	}()

	start := time.Now()
	if _, err := c.ListDir(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("ListDir took %s to notice cancellation", d)
	}

	// The point of the test: the goroutine is still there, parked on the CLOSE.
	// The stalled counter is the assertion rather than the WaitGroup because it
	// only moves once the operation has failed to finish within orphanGrace —
	// so reaching 1 is both "still parked" and "accounted for", and it does not
	// need a Wait that would then be racing the release below.
	if !awaitStalled(&c.stalled, 1, 5*time.Second) {
		t.Fatal("the listing's goroutine unwound on its own; if pkg/sftp now closes the directory handle under ctx, the doc on ListDir and orphanGrace can be simplified")
	}

	// And it is a stall, not a leak: when the host answers, everything unwinds
	// and the shared client is still usable.
	if err := gated.release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if !waitGroup(&c.inflight, 5*time.Second) {
		t.Fatal("the parked goroutines never unwound after the host answered")
	}
	select {
	case <-c.Done():
		t.Fatal("client tore itself down after a single cancelled listing")
	default:
	}
	if _, err := c.ListDir(context.Background(), root); err != nil {
		t.Fatalf("ListDir after an orphaned listing: %v", err)
	}
}

// TestOrphanedOpBelowCapKeepsTheClientUsable pins the other half of the
// trade-off: one caller giving up must not poison the shared client for
// everyone else. FileMeta is used because it is unambiguously an orphan —
// pkg/sftp's Stat takes no context, so its goroutine really is parked on the
// stalled host until the answer arrives. (A listing cancelled during paging is
// one too, for a different reason: see
// TestCancelledListDirAfterOpendirIsAnOrphan.) When the answer comes, the
// goroutine unwinds and the next operation still works.
func TestOrphanedOpBelowCapKeepsTheClientUsable(t *testing.T) {
	c, root, gated := newTestClient(t)
	mustWrite(t, filepath.Join(root, "a.txt"), "hello")
	gated.stall()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.FileMeta(ctx, path.Join(root, "a.txt")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}

	if err := gated.release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if !waitGroup(&c.inflight, 5*time.Second) {
		t.Fatal("the abandoned operation's goroutine never unwound")
	}
	select {
	case <-c.Done():
		t.Fatal("client tore itself down after a single cancelled operation")
	default:
	}

	got, err := c.ListDir(context.Background(), root)
	if err != nil {
		t.Fatalf("ListDir after an orphaned op: %v", err)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("entries = %+v, want 1", got.Entries)
	}
}

// TestOrphanedOpsAtCapTearDownTheClient is the "does it actually release, or
// does it just stop caring" question. Cancelling returns promptly either way;
// what is asserted here is that the goroutines behind those cancelled calls do
// not stay parked on a host that will never answer. Once stalledOpCap of them
// have piled up the channel is declared wedged and closed, which unblocks
// every one of them, and Done tells the owner to redial.
func TestOrphanedOpsAtCapTearDownTheClient(t *testing.T) {
	c, root, gated := newTestClient(t)
	gated.stall()

	for i := 0; i < stalledOpCap; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, err := c.FileMeta(ctx, path.Join(root, "whatever"))
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("op %d: err = %v, want context.DeadlineExceeded", i, err)
		}
	}

	if !waitGroup(&c.inflight, 5*time.Second) {
		t.Fatal("stalled goroutines stayed blocked after the cap was reached")
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("client did not report itself closed after tearing the channel down")
	}

	// A wedged client must fail fast rather than hang the next caller too.
	done := make(chan error, 1)
	go func() {
		_, err := c.ListDir(context.Background(), root)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ListDir on a torn-down client succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ListDir on a torn-down client hung")
	}
}

func TestAlreadyCancelledContextDoesNoWork(t *testing.T) {
	c, root, gated := newTestClient(t)
	gated.stall()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.ListDir(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if !waitGroup(&c.inflight, time.Second) {
		t.Fatal("an op ran despite an already-cancelled context")
	}
}

func TestReadChunkHonoursCancellation(t *testing.T) {
	c, root, gated := newTestClient(t)
	mustWrite(t, filepath.Join(root, "f"), "body")
	gated.stall()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.ReadChunk(ctx, path.Join(root, "f"), 0, 4)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("ReadChunk took %s to notice a 50ms deadline", d)
	}
}

func TestWriteFileHonoursCancellation(t *testing.T) {
	c, root, gated := newTestClient(t)
	gated.stall()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.WriteFile(ctx, path.Join(root, "x"), bytes.Repeat([]byte("a"), 16), 0, true)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("WriteFile took %s to notice a 50ms deadline", d)
	}
}

// ---------------------------------------------------------------------------
// the bounded open
// ---------------------------------------------------------------------------

// deadPipe is a byte stream to a host that accepts everything and answers
// nothing: writes are swallowed, and a read blocks until the stream is closed.
// That is what an SFTP handshake meets when sshd is healthy but sftp-server is
// missing or stuck — the INIT goes out and the VERSION never comes back.
//
// Every Close is announced on closes, which is how a test can tell the
// difference between "the caller gave up" and "the handshake was released":
// NewClientPipe closes the write side itself when recvVersion fails, so a
// second Close can only happen once that parked read has actually returned.
type deadPipe struct {
	once   sync.Once
	gone   chan struct{}
	closes chan struct{}
}

func newDeadPipe() *deadPipe {
	return &deadPipe{gone: make(chan struct{}), closes: make(chan struct{}, 8)}
}

func (d *deadPipe) Read(p []byte) (int, error) {
	<-d.gone
	return 0, io.EOF
}

func (d *deadPipe) Write(p []byte) (int, error) { return len(p), nil }

func (d *deadPipe) Close() error {
	d.once.Do(func() { close(d.gone) })
	select {
	case d.closes <- struct{}{}:
	default:
	}
	return nil
}

// TestNewContextBoundsAWedgedHandshake is the constructor's version of the
// worker-slot problem. sftp.NewClientPipe reads VERSION with no context and no
// deadline under it, so against this host New never returns — and it runs
// outside do(), so nothing counts it, nothing reaps it and the channel teardown
// never fires.
//
// The second assertion is the one with teeth. Returning ctx.Err() on time is
// easy and worthless if the goroutine behind the handshake stays parked on the
// same read for the life of the connection; the bound is only real because the
// stream is closed, which turns that read into an EOF.
func TestNewContextBoundsAWedgedHandshake(t *testing.T) {
	pipe := newDeadPipe()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	c, err := NewContext(ctx, pipe)
	if err == nil {
		_ = c.Close()
		t.Fatal("NewContext succeeded against a host that never sent VERSION")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("NewContext took %s to notice a 100ms deadline", d)
	}

	// Close #1 is the timeout path's. Close #2 is pkg/sftp's own cleanup on the
	// recvVersion error path, which cannot run until the parked read returns.
	for i := 0; i < 2; i++ {
		select {
		case <-pipe.closes:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of 2 closes; the handshake goroutine is still parked on the dead host", i)
		}
	}
}

// TestNewContextPassesTheHandshakeThrough keeps the bound from being a
// regression in the ordinary case: a host that does answer gets a working
// client, and the deadline is never involved.
func TestNewContextPassesTheHandshakeThrough(t *testing.T) {
	root := t.TempDir()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	clientEnd, serverEnd := net.Pipe()
	srv, err := sftp.NewServer(serverEnd)
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan struct{})
	go func() { defer close(served); _ = srv.Serve(); serverEnd.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := NewContext(ctx, clientEnd)
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	defer func() {
		_ = c.Close()
		select {
		case <-served:
		case <-time.After(5 * time.Second):
			t.Error("sftp test server did not stop")
		}
	}()
	if _, err := c.ListDir(context.Background(), filepath.ToSlash(resolved)); err != nil {
		t.Fatalf("ListDir on a client from NewContext: %v", err)
	}
}

// muteSFTPServer is an ssh server that accepts a session channel and agrees to
// the "sftp" subsystem — and then says nothing further. No sftp-server behind
// it, which is the exact failure OpenContext exists for: TCP up, SSH up,
// channel open, VERSION never sent.
type muteSFTPServer struct {
	addr    string
	hostPub ssh.PublicKey
	// channelGone receives once per session channel the client closes. It is
	// the far side's view of the abandoned handshake being released.
	channelGone chan struct{}
}

func startMuteSFTPServer(t *testing.T) *muteSFTPServer {
	t.Helper()
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "u" && string(pass) == "pw" {
				return &ssh.Permissions{}, nil
			}
			return nil, io.EOF
		},
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	srv := &muteSFTPServer{addr: ln.Addr().String(), hostPub: signer.PublicKey(), channelGone: make(chan struct{}, 8)}
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.serve(nc, cfg)
		}
	}()
	return srv
}

func (m *muteSFTPServer) serve(nc net.Conn, cfg *ssh.ServerConfig) {
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
				if r.WantReply {
					_ = r.Reply(r.Type == "subsystem", nil)
				}
			}
		}(ch, chReqs)
		go func(ch ssh.Channel) {
			// Reads whatever the client sends (the INIT packet) and never
			// answers. Returns when the client closes the channel.
			_, _ = io.Copy(io.Discard, ch)
			select {
			case m.channelGone <- struct{}{}:
			default:
			}
			_ = ch.Close()
		}(ch)
	}
}

func (m *muteSFTPServer) dial(t *testing.T) *sshclient.Conn {
	t.Helper()
	host, port, err := net.SplitHostPort(m.addr)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := sshclient.DialConn(ctx, sshclient.Config{
		Host: host, Port: port, User: "u",
		Auth:      sshclient.PasswordAuth{Password: "pw"},
		HostKeyCb: ssh.FixedHostKey(m.hostPub),
	})
	if err != nil {
		t.Fatalf("DialConn: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// TestOpenContextBoundsAWedgedHandshake is the end-to-end form, over a real SSH
// connection, of the failure that matters most: this is the redial path. Done()
// means "reopen before the next operation", so a client torn down for stalling
// comes back through here, and an open that never returns turns a recoverable
// stall into a permanently burnt worker slot.
func TestOpenContextBoundsAWedgedHandshake(t *testing.T) {
	srv := startMuteSFTPServer(t)
	conn := srv.dial(t)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	c, err := OpenContext(ctx, conn)
	if err == nil {
		_ = c.Close()
		t.Fatal("OpenContext succeeded against a host with no sftp-server behind the subsystem")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("OpenContext took %s to notice a 300ms deadline", d)
	}

	// The abandoned handshake was released, not merely stopped being waited on:
	// the far side sees its channel go away, which only happens because the
	// timeout path closes the stream.
	select {
	case <-srv.channelGone:
	case <-time.After(5 * time.Second):
		t.Fatal("the sftp channel stayed open; the handshake goroutine is still parked on it")
	}

	// And the damage is confined to that channel. The Conn is shared with the
	// terminal and every tunnel on the host, so a failed open must leave it
	// usable — including for the retry that follows.
	select {
	case <-conn.Done():
		t.Fatal("a timed-out SFTP open took the whole ssh connection down")
	default:
	}
	retry, rcancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer rcancel()
	if _, err := OpenContext(retry, conn); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("retry err = %v, want context.DeadlineExceeded", err)
	}
}

// TestOpenContextAlreadyCancelledDoesNotDial covers the redial-storm case: a
// caller whose context is already gone must not leave a session channel open on
// the far side.
func TestOpenContextAlreadyCancelledDoesNotDial(t *testing.T) {
	srv := startMuteSFTPServer(t)
	conn := srv.dial(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, err := OpenContext(ctx, conn)
	if err == nil {
		_ = c.Close()
		t.Fatal("OpenContext with a cancelled context returned a client")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	// Whatever got as far as being opened is closed behind us.
	select {
	case <-srv.channelGone:
	case <-time.After(5 * time.Second):
		t.Fatal("a channel was left open on the host after an already-cancelled open")
	}
}

// ---------------------------------------------------------------------------
// error messages
// ---------------------------------------------------------------------------

// TestErrorsNameTheOperationAndThePath: the local executor gets this for free
// from *PathError ("stat /srv/app.conf: permission denied"). Over SFTP the
// transport says only `sftp: "Permission denied" (SSH_FX_PERMISSION_DENIED)`,
// and that string is the whole of what the user is shown.
func TestErrorsNameTheOperationAndThePath(t *testing.T) {
	c, root, _ := newTestClient(t)
	ctx := context.Background()
	mustWrite(t, filepath.Join(root, "f"), "x")

	cases := []struct {
		name string
		run  func() error
		want string // expected message prefix
		path string
	}{
		{"list a file", func() error {
			_, err := c.ListDir(ctx, path.Join(root, "f"))
			return err
		}, "list ", path.Join(root, "f")},
		{"list a missing directory", func() error {
			_, err := c.ListDir(ctx, path.Join(root, "nope"))
			return err
		}, "list ", path.Join(root, "nope")},
		{"stat a missing file", func() error {
			_, err := c.FileMeta(ctx, path.Join(root, "nope"))
			return err
		}, "stat ", path.Join(root, "nope")},
		{"read a directory", func() error {
			_, err := c.ReadChunk(ctx, root, 0, 10)
			return err
		}, "read ", root},
		{"write onto an existing file", func() error {
			_, err := c.WriteFile(ctx, path.Join(root, "f"), []byte("x"), 0, true)
			return err
		}, "write ", path.Join(root, "f")},
		{"mkdir over a file", func() error {
			_, err := c.Mkdir(ctx, path.Join(root, "f"))
			return err
		}, "mkdir ", path.Join(root, "f")},
		{"remove a missing path", func() error {
			return c.Remove(ctx, path.Join(root, "nope"), false)
		}, "remove ", path.Join(root, "nope")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.HasPrefix(err.Error(), tc.want) {
				t.Errorf("err = %q, want it to start with %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), tc.path) {
				t.Errorf("err = %q, want it to name %q", err, tc.path)
			}
		})
	}
}

// TestServerSideFailureCarriesThePath is the case the sentinels cannot cover: a
// plain permission refusal from the far side, which arrives with no path in it
// at all.
func TestServerSideFailureCarriesThePath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission bits would not refuse anything")
	}
	c, root, _ := newTestClient(t)
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	// Restore before TempDir's own cleanup tries to remove it.
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	_, err := c.ListDir(context.Background(), path.Join(root, "locked"))
	if err == nil {
		t.Fatal("listing an unreadable directory succeeded")
	}
	if !strings.Contains(err.Error(), path.Join(root, "locked")) {
		t.Errorf("err = %q, want it to name the directory", err)
	}
	if !strings.HasPrefix(err.Error(), "list ") {
		t.Errorf("err = %q, want it to name the operation", err)
	}
}

// TestRenameBlamesTheDestination: a destination-side failure labelled with the
// source sends the user to look at the wrong file — and in a rename, the file
// they are being told about is the one that is fine.
func TestRenameBlamesTheDestination(t *testing.T) {
	c, root, _ := newTestClient(t)
	mustWrite(t, filepath.Join(root, "moving"), "body")
	mustWrite(t, filepath.Join(root, "occupied"), "other")

	_, err := c.Rename(context.Background(), path.Join(root, "moving"), path.Join(root, "occupied"))
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("err = %v, want ErrAlreadyExists", err)
	}
	if !strings.Contains(err.Error(), "occupied") {
		t.Errorf("err = %q, want it to name the destination", err)
	}
	if strings.Contains(err.Error(), "moving") {
		t.Errorf("err = %q blames the source for a destination-side refusal", err)
	}
}
