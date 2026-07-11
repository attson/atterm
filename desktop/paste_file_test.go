package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestSanitizeAttachmentName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"foo.pdf", "foo.pdf"},
		{"../../../etc/passwd", "passwd"},
		{"foo\x00bar.txt", "foobar.txt"},
		{"", "file"},
		{".", "file"},
		{"..", "file"},
		{"CON", "_CON"},
		{"COM1", "_COM1"},
		{"lpt9", "_lpt9"},
		{"a/b/c.log", "c.log"},
		{`a\b\c.log`, "c.log"},
		{"日本語.txt", "日本語.txt"},
	}
	for _, c := range cases {
		got := sanitizeAttachmentName(c.in)
		if got != c.want {
			t.Errorf("sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeAttachmentNameTruncatesLongName(t *testing.T) {
	long := strings.Repeat("a", 300) + ".log"
	got := sanitizeAttachmentName(long)
	// Total rune length must be ≤128 and end in ".log".
	if len([]rune(got)) > 128 {
		t.Errorf("length %d > 128", len([]rune(got)))
	}
	if !strings.HasSuffix(got, ".log") {
		t.Errorf("lost extension: %q", got)
	}
}

func TestDedupFilenameHappyPath(t *testing.T) {
	dir := t.TempDir()
	got1, err := dedupFilename(dir, "foo.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got1) != "foo.pdf" {
		t.Errorf("first: got %q", filepath.Base(got1))
	}
	got2, err := dedupFilename(dir, "foo.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got2) != "foo (1).pdf" {
		t.Errorf("second: got %q", filepath.Base(got2))
	}
	got3, err := dedupFilename(dir, "foo.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got3) != "foo (2).pdf" {
		t.Errorf("third: got %q", filepath.Base(got3))
	}
}

func TestDedupFilenameConcurrent(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	results := make([]string, 20)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := dedupFilename(dir, "shared.log")
			if err != nil {
				t.Errorf("dedup %d: %v", i, err)
				return
			}
			results[i] = p
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i, p := range results {
		if p == "" {
			t.Errorf("result %d empty", i)
			continue
		}
		if seen[p] {
			t.Errorf("duplicate path %q at %d", p, i)
		}
		seen[p] = true
	}
}

func TestSavePastedFileHappyPath(t *testing.T) {
	sid := uuid.New()
	p := proto.PasteFilePayload{
		Filename:    "notes.pdf",
		ContentType: "application/pdf",
		Data:        []byte("hello world"),
	}
	path, err := savePastedFile(sid, p)
	if err != nil {
		t.Fatal(err)
	}
	// Cleanup the session dir on completion.
	t.Cleanup(func() {
		dir, _ := pasteFileDir(sid)
		_ = os.RemoveAll(dir)
	})

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, p.Data) {
		t.Errorf("content mismatch")
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v, want 0600", st.Mode().Perm())
	}
	if !strings.Contains(path, sid.String()) {
		t.Errorf("path %q missing session id %q", path, sid.String())
	}
	if filepath.Base(path) != "notes.pdf" {
		t.Errorf("basename = %q, want notes.pdf", filepath.Base(path))
	}
}

func TestSavePastedFileEmpty(t *testing.T) {
	_, err := savePastedFile(uuid.New(), proto.PasteFilePayload{Filename: "x.txt", Data: nil})
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestSavePastedFileTooLarge(t *testing.T) {
	_, err := savePastedFile(uuid.New(), proto.PasteFilePayload{
		Filename: "x.bin",
		Data:     make([]byte, maxPasteFileBytes+1),
	})
	if err == nil {
		t.Fatal("expected error for oversize")
	}
}
