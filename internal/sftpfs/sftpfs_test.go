package sftpfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
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
	held    [][]byte
}

func (g *gatedPipe) Write(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stalled {
		g.held = append(g.held, append([]byte(nil), p...))
		return len(p), nil
	}
	return g.Conn.Write(p)
}

func (g *gatedPipe) stall() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stalled = true
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

	gated := &gatedPipe{Conn: clientEnd}
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

// TestCancelledListDirLeavesNothingBehind is the stronger form of the previous
// test for the one operation that can manage it: ReadDirContext threads ctx
// into the listing loop itself, so the goroutine behind a cancelled listing
// unwinds on its own — no orphan, nothing to reap, even though the host is
// still refusing to answer.
func TestCancelledListDirLeavesNothingBehind(t *testing.T) {
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

// TestOrphanedOpBelowCapKeepsTheClientUsable pins the other half of the
// trade-off: one caller giving up must not poison the shared client for
// everyone else. FileMeta is used rather than ListDir because it is a genuine
// orphan — pkg/sftp's Stat takes no context, so its goroutine really is parked
// on the stalled host until the answer arrives. When it does, the goroutine
// unwinds and the next operation still works.
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
