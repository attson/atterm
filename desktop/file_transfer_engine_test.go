package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"testing"
)

type fakeTransferNode struct {
	kind    transferKind
	data    []byte
	modTime int64
	reason  string
}

type fakeTransferEndpoint struct {
	id             string
	nodes          map[string]*fakeTransferNode
	truncated      map[string]bool
	maxWrite       int
	aborts         int
	failWriteAfter int
	mutateOnClose  string
}

func newFakeTransferEndpoint(id string) *fakeTransferEndpoint {
	return &fakeTransferEndpoint{
		id:        id,
		nodes:     map[string]*fakeTransferNode{"/": {kind: transferDir}},
		truncated: map[string]bool{},
	}
}

func (f *fakeTransferEndpoint) ID() string { return f.id }

func (f *fakeTransferEndpoint) Clean(p string) (string, error) {
	if !path.IsAbs(p) {
		return "", errors.New("path must be absolute")
	}
	return path.Clean(p), nil
}

func (f *fakeTransferEndpoint) Join(base string, parts ...string) string {
	return path.Join(append([]string{base}, parts...)...)
}
func (f *fakeTransferEndpoint) Base(p string) string { return path.Base(p) }

func (f *fakeTransferEndpoint) Lstat(_ context.Context, name string) (transferEntry, error) {
	p := path.Clean(name)
	node, ok := f.nodes[p]
	if !ok {
		return transferEntry{}, os.ErrNotExist
	}
	return transferEntry{
		SourcePath: p,
		Kind:       node.kind,
		Size:       int64(len(node.data)),
		ModTime:    node.modTime,
		SkipReason: node.reason,
	}, nil
}

func (f *fakeTransferEndpoint) List(_ context.Context, name string) ([]transferEntry, bool, error) {
	dir := path.Clean(name)
	if node, ok := f.nodes[dir]; !ok {
		return nil, false, os.ErrNotExist
	} else if node.kind != transferDir {
		return nil, false, errors.New("not a directory")
	}
	prefix := strings.TrimSuffix(dir, "/") + "/"
	var entries []transferEntry
	for p := range f.nodes {
		if p == dir || !strings.HasPrefix(p, prefix) {
			continue
		}
		rel := strings.TrimPrefix(p, prefix)
		if strings.Contains(rel, "/") {
			continue
		}
		entry, err := f.Lstat(context.Background(), p)
		if err != nil {
			return nil, false, err
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].SourcePath < entries[j].SourcePath })
	return entries, f.truncated[dir], nil
}

func (f *fakeTransferEndpoint) OpenReader(_ context.Context, name string) (io.ReadCloser, error) {
	p := path.Clean(name)
	node, ok := f.nodes[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &fakeTransferReader{
		Reader: bytes.NewReader(append([]byte(nil), node.data...)),
		close: func() {
			if f.mutateOnClose == p {
				node.modTime++
			}
		},
	}, nil
}

func (f *fakeTransferEndpoint) CreateStagedWriter(_ context.Context, name string) (transferStagedWriter, error) {
	return &fakeTransferWriter{endpoint: f, target: path.Clean(name)}, nil
}

func (f *fakeTransferEndpoint) Mkdir(_ context.Context, name string) error {
	p := path.Clean(name)
	if _, exists := f.nodes[p]; exists {
		return os.ErrExist
	}
	f.nodes[p] = &fakeTransferNode{kind: transferDir}
	return nil
}

func (f *fakeTransferEndpoint) Remove(_ context.Context, name string, recursive bool) error {
	p := path.Clean(name)
	if !recursive {
		delete(f.nodes, p)
		return nil
	}
	for candidate := range f.nodes {
		if candidate == p || strings.HasPrefix(candidate, strings.TrimSuffix(p, "/")+"/") {
			delete(f.nodes, candidate)
		}
	}
	return nil
}

func (f *fakeTransferEndpoint) Close() error { return nil }

type fakeTransferReader struct {
	*bytes.Reader
	close func()
}

func (r *fakeTransferReader) Close() error {
	if r.close != nil {
		r.close()
	}
	return nil
}

type fakeTransferWriter struct {
	endpoint *fakeTransferEndpoint
	target   string
	buf      bytes.Buffer
	aborted  bool
}

func (w *fakeTransferWriter) Write(p []byte) (int, error) {
	if len(p) > w.endpoint.maxWrite {
		w.endpoint.maxWrite = len(p)
	}
	if w.endpoint.failWriteAfter > 0 && w.buf.Len()+len(p) > w.endpoint.failWriteAfter {
		return 0, errors.New("injected write failure")
	}
	return w.buf.Write(p)
}

func (w *fakeTransferWriter) Commit(_ context.Context, replace bool) error {
	if _, exists := w.endpoint.nodes[w.target]; exists && !replace {
		return os.ErrExist
	}
	w.endpoint.nodes[w.target] = &fakeTransferNode{
		kind:    transferFile,
		data:    append([]byte(nil), w.buf.Bytes()...),
		modTime: 1,
	}
	return nil
}

func (w *fakeTransferWriter) Abort() error {
	if !w.aborted {
		w.aborted = true
		w.endpoint.aborts++
	}
	return nil
}

func TestTransferEngineCopiesNestedAndEmptyDirectoriesWithBoundedWrites(t *testing.T) {
	src := newFakeTransferEndpoint("src")
	src.nodes["/tree"] = &fakeTransferNode{kind: transferDir}
	src.nodes["/tree/empty"] = &fakeTransferNode{kind: transferDir}
	src.nodes["/tree/file.bin"] = &fakeTransferNode{
		kind:    transferFile,
		data:    bytes.Repeat([]byte("x"), 2*(256<<10)+17),
		modTime: 7,
	}
	dst := newFakeTransferEndpoint("dst")
	dst.nodes["/out"] = &fakeTransferNode{kind: transferDir}

	manifest, err := buildTransferManifest(context.Background(), src, []string{"/tree"})
	if err != nil {
		t.Fatal(err)
	}
	var progress []FileTransferProgress
	result := runTransfer(context.Background(), src, dst, manifest, "/out", conflictSkip, func(p FileTransferProgress) {
		progress = append(progress, p)
	})

	if result.State != "completed" || result.Error != "" {
		t.Fatalf("result = %+v", result)
	}
	if got := dst.nodes["/out/tree/file.bin"]; got == nil || !bytes.Equal(got.data, src.nodes["/tree/file.bin"].data) {
		t.Fatal("regular file was not copied")
	}
	if got := dst.nodes["/out/tree/empty"]; got == nil || got.kind != transferDir {
		t.Fatal("empty directory was not copied")
	}
	if dst.maxWrite > 256<<10 {
		t.Fatalf("largest Write = %d, want <= %d", dst.maxWrite, 256<<10)
	}
	for i := 1; i < len(progress); i++ {
		if progress[i].BytesDone < progress[i-1].BytesDone || progress[i].EntriesDone < progress[i-1].EntriesDone {
			t.Fatalf("progress moved backwards: %+v then %+v", progress[i-1], progress[i])
		}
	}
}

func TestTransferEngineRejectsTruncatedDirectoryManifest(t *testing.T) {
	src := newFakeTransferEndpoint("src")
	src.nodes["/many"] = &fakeTransferNode{kind: transferDir}
	src.nodes["/many/a"] = &fakeTransferNode{kind: transferFile, data: []byte("a")}
	src.truncated["/many"] = true

	if _, err := buildTransferManifest(context.Background(), src, []string{"/many"}); err == nil {
		t.Fatal("truncated directory was accepted")
	}
}

func TestTransferEngineSkipsSymlinkWithoutFollowingIt(t *testing.T) {
	src := newFakeTransferEndpoint("src")
	src.nodes["/link"] = &fakeTransferNode{kind: transferSkipped, reason: "symlink"}
	dst := newFakeTransferEndpoint("dst")
	dst.nodes["/out"] = &fakeTransferNode{kind: transferDir}

	manifest, err := buildTransferManifest(context.Background(), src, []string{"/link"})
	if err != nil {
		t.Fatal(err)
	}
	result := runTransfer(context.Background(), src, dst, manifest, "/out", conflictSkip, nil)
	if result.State != "completed" || len(result.Warnings) != 1 {
		t.Fatalf("result = %+v, want one skip warning", result)
	}
	if _, exists := dst.nodes["/out/link"]; exists {
		t.Fatal("symlink was copied")
	}
}

func TestTransferEngineRejectsCopyIntoSourceDescendant(t *testing.T) {
	ep := newFakeTransferEndpoint("same")
	ep.nodes["/tree"] = &fakeTransferNode{kind: transferDir}
	ep.nodes["/tree/file"] = &fakeTransferNode{kind: transferFile, data: []byte("x")}
	manifest, err := buildTransferManifest(context.Background(), ep, []string{"/tree"})
	if err != nil {
		t.Fatal(err)
	}

	result := runTransfer(context.Background(), ep, ep, manifest, "/tree/child", conflictSkip, nil)
	if result.State != "failed" {
		t.Fatalf("result = %+v, want failed", result)
	}
}

func TestTransferEngineConflictPolicies(t *testing.T) {
	for _, tc := range []struct {
		name       string
		policy     transferConflict
		wantTarget string
		wantCopy   string
	}{
		{name: "skip", policy: conflictSkip, wantTarget: "old"},
		{name: "replace", policy: conflictReplace, wantTarget: "new"},
		{name: "rename", policy: conflictRename, wantTarget: "old", wantCopy: "new"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := newFakeTransferEndpoint("src")
			src.nodes["/a.txt"] = &fakeTransferNode{kind: transferFile, data: []byte("new"), modTime: 1}
			dst := newFakeTransferEndpoint("dst")
			dst.nodes["/out"] = &fakeTransferNode{kind: transferDir}
			dst.nodes["/out/a.txt"] = &fakeTransferNode{kind: transferFile, data: []byte("old")}
			manifest, err := buildTransferManifest(context.Background(), src, []string{"/a.txt"})
			if err != nil {
				t.Fatal(err)
			}

			result := runTransfer(context.Background(), src, dst, manifest, "/out", tc.policy, nil)
			if result.State != "completed" {
				t.Fatalf("result = %+v", result)
			}
			if got := string(dst.nodes["/out/a.txt"].data); got != tc.wantTarget {
				t.Fatalf("target = %q, want %q", got, tc.wantTarget)
			}
			if tc.wantCopy != "" {
				if got := dst.nodes["/out/a copy 1.txt"]; got == nil || string(got.data) != tc.wantCopy {
					t.Fatalf("renamed copy = %+v, want %q", got, tc.wantCopy)
				}
			}
		})
	}
}

func TestTransferEngineAbortsPartialOnWriteError(t *testing.T) {
	src := newFakeTransferEndpoint("src")
	src.nodes["/file"] = &fakeTransferNode{kind: transferFile, data: bytes.Repeat([]byte("x"), 512<<10)}
	dst := newFakeTransferEndpoint("dst")
	dst.nodes["/out"] = &fakeTransferNode{kind: transferDir}
	dst.failWriteAfter = 128 << 10
	manifest, err := buildTransferManifest(context.Background(), src, []string{"/file"})
	if err != nil {
		t.Fatal(err)
	}

	result := runTransfer(context.Background(), src, dst, manifest, "/out", conflictSkip, nil)
	if result.State != "failed" || dst.aborts != 1 {
		t.Fatalf("result = %+v, aborts = %d", result, dst.aborts)
	}
	if _, exists := dst.nodes["/out/file"]; exists {
		t.Fatal("failed transfer published its target")
	}
}

func TestTransferEngineReportsSourceChangedAndCancellation(t *testing.T) {
	src := newFakeTransferEndpoint("src")
	src.nodes["/file"] = &fakeTransferNode{kind: transferFile, data: []byte("body"), modTime: 1}
	src.mutateOnClose = "/file"
	dst := newFakeTransferEndpoint("dst")
	dst.nodes["/out"] = &fakeTransferNode{kind: transferDir}
	manifest, err := buildTransferManifest(context.Background(), src, []string{"/file"})
	if err != nil {
		t.Fatal(err)
	}

	result := runTransfer(context.Background(), src, dst, manifest, "/out", conflictSkip, nil)
	if result.State != "completed" || len(result.Warnings) != 1 {
		t.Fatalf("source-changed result = %+v", result)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	result = runTransfer(cancelled, src, dst, manifest, "/out", conflictReplace, nil)
	if result.State != "cancelled" {
		t.Fatalf("cancelled result = %+v", result)
	}
}
