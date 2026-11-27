package sftpfs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGetwdReturnsAbsoluteDirectory(t *testing.T) {
	c, _, _ := newTestClient(t)
	got, err := c.Getwd(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !path.IsAbs(got) {
		t.Fatalf("Getwd() = %q, want an absolute remote path", got)
	}
}

func TestLstatDoesNotFollowSymlink(t *testing.T) {
	c, root, _ := newTestClient(t)
	mustWrite(t, filepath.Join(root, "target"), "body")
	if err := os.Symlink("target", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	info, err := c.Lstat(context.Background(), path.Join(root, "link"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("Lstat mode = %v, want ModeSymlink", info.Mode())
	}
}

func TestOpenReaderStreamsLargeFile(t *testing.T) {
	c, root, _ := newTestClient(t)
	payload := bytes.Repeat([]byte("0123456789abcdef"), 12*1024*1024/16)
	if err := os.WriteFile(filepath.Join(root, "large.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := c.OpenReader(context.Background(), path.Join(root, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	var out bytes.Buffer
	n, err := io.CopyBuffer(&out, r, make([]byte, 64<<10))
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(payload)) || !bytes.Equal(out.Bytes(), payload) {
		t.Fatalf("streamed %d bytes, want %d matching bytes", n, len(payload))
	}
}

func TestStagedWriterPublishesOnlyOnCommit(t *testing.T) {
	c, root, _ := newTestClient(t)
	target := path.Join(root, "dst.bin")
	payload := bytes.Repeat([]byte("streamed"), 512*1024)

	w, err := c.CreateStagedWriter(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyBuffer(w, bytes.NewReader(payload), make([]byte, 256<<10)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "dst.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination became visible before commit: %v", err)
	}
	if err := w.Commit(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "dst.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("committed bytes differ")
	}
	assertNoTransferPartials(t, root)
}

func TestStagedWriterRefusesOccupiedTargetWithoutReplace(t *testing.T) {
	c, root, _ := newTestClient(t)
	mustWrite(t, filepath.Join(root, "dst"), "original")
	w, err := c.CreateStagedWriter(context.Background(), path.Join(root, "dst"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("replacement")); err != nil {
		t.Fatal(err)
	}
	if err := w.Commit(context.Background(), false); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("Commit error = %v, want ErrAlreadyExists", err)
	}
	if got := readBack(t, filepath.Join(root, "dst")); got != "original" {
		t.Fatalf("occupied target changed to %q", got)
	}
	assertNoTransferPartials(t, root)
}

func TestStagedWriterAbortAndCancellationRemovePartial(t *testing.T) {
	c, root, _ := newTestClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	w, err := c.CreateStagedWriter(ctx, path.Join(root, "cancelled"))
	if err != nil {
		t.Fatal(err)
	}
	cancel()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := w.Write([]byte("x")); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("writer stayed writable after its context was cancelled")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := w.Abort(); err != nil {
		t.Fatal(err)
	}
	if err := w.Abort(); err != nil {
		t.Fatalf("second Abort must be idempotent: %v", err)
	}
	assertNoTransferPartials(t, root)
}

func assertNoTransferPartials(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), transferTempPrefix) && strings.HasSuffix(entry.Name(), transferTempSuffix) {
			t.Fatalf("transfer partial left behind: %s", entry.Name())
		}
	}
}
