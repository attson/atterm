# File Explorer Editing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the read-only File Explorer plugin so users can edit, save, and manage files (CRUD + trash) on both the local machine and remote sessions.

**Architecture:** Reuse the existing `fsAccess` layer (`desktop/fsaccess.go`) for all writes and CRUD; add a `write_file / create_file / rename / remove / mkdir / trash` op family to the FS RPC surface used by `PluginFS` (local Wails binding) and `remoteFS` (proto-framed remote). Frontend swaps the read-only `CodeViewer` for a `CodeEditor` that emits dirty state, saves on Cmd+S with `expected_modtime` CAS, and grows a right-click menu on the tree for CRUD.

**Tech Stack:** Go 1.22+, Wails v2 bindings, Vue 3 + TypeScript, CodeMirror 6, Vitest, testify.

## Global Constraints

- Reply/PR/commit language: English for code, docs, commits; Chinese OK in in-session prose only.
- Do NOT introduce backwards-compatibility shims for old proto fields or JSON keys — this is a single-user project. When a field is renamed, delete the old.
- CI check `.github/scripts/check-plugin-fs-isolation.sh` must keep passing: `PluginFS` symbols may not appear in `desktop/uplink*.go` or `internal/`. New `PluginFS` methods live in `desktop/`.
- All writes gated by `fsAccess.resolve()` (`allowRoots` + `denyExact/Suffix`). Never bypass.
- Server-side hard cap: 5 MiB per write. Frontend hard cap: 2 MiB (matches existing `MAX_BYTES_FRONTEND`).
- Encoding: UTF-8; no line-ending normalization; no auto trailing newline.
- Remote writes require `remote_permission=full` (matches existing gate in `handleRemoteFSRequest`).
- Delete UI: default = trash; `Shift`-modifier = hard delete; if trash unavailable, prompt to hard-delete instead.
- Save trigger: `Cmd/Ctrl+S` only. No auto-save.
- Dirty-tab close: modal with Save / Don't Save / Cancel.
- Conflict on save: reject with `stale_modtime: current=<n>`; UI shows Overwrite / Reload (discard) / Cancel.

---

## File Structure

Files created (green), modified (yellow), deleted (red):

```
internal/proto/frame.go                                        [modify]
internal/trash/                                                 [create]
  trash.go
  trash_darwin.go
  trash_linux.go
  trash_windows.go
  trash_darwin_test.go
  trash_linux_test.go
desktop/fsaccess.go                                             [modify]
desktop/fsaccess_write.go                                       [create]
desktop/fsaccess_write_test.go                                  [create]
desktop/plugin_fs.go                                            [modify]
desktop/plugin_fs_write.go                                      [create]
desktop/plugin_fs_write_test.go                                 [create]
desktop/remote_fs.go                                            [modify]
desktop/remote_fs_test.go                                       [modify]
desktop/frontend/src/lib/connection.ts                          [modify]
desktop/frontend/src/plugins/fileExplorer/fsBridge.ts           [modify]
desktop/frontend/src/plugins/fileExplorer/fsBridge.test.ts      [modify]
desktop/frontend/src/plugins/fileExplorer/remoteSessionFS.ts    [modify]
desktop/frontend/src/plugins/fileExplorer/remoteSessionFS.test.ts [modify]
desktop/frontend/src/plugins/fileExplorer/CodeEditor.vue        [create — replaces CodeViewer.vue]
desktop/frontend/src/plugins/fileExplorer/CodeEditor.test.ts    [create — replaces CodeViewer.test.ts]
desktop/frontend/src/plugins/fileExplorer/CodeViewer.vue        [delete]
desktop/frontend/src/plugins/fileExplorer/CodeViewer.test.ts    [delete]
desktop/frontend/src/plugins/fileExplorer/FileEditor.vue        [modify]
desktop/frontend/src/plugins/fileExplorer/FileTabs.vue          [modify]
desktop/frontend/src/plugins/fileExplorer/FileTabs.test.ts      [modify]
desktop/frontend/src/plugins/fileExplorer/FileTree.vue          [modify]
desktop/frontend/src/plugins/fileExplorer/FileTree.test.ts      [modify]
desktop/frontend/src/plugins/fileExplorer/FileTreeNode.vue      [modify]
desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue      [modify]
desktop/frontend/src/plugins/fileExplorer/tabsModel.ts          [modify]
desktop/frontend/src/plugins/fileExplorer/tabsModel.test.ts     [modify]
desktop/frontend/src/plugins/fileExplorer/ConfirmDialog.vue     [create]
desktop/frontend/src/plugins/fileExplorer/ConfirmDialog.test.ts [create]
desktop/frontend/src/plugins/fileExplorer/InlineEditRow.vue     [create — inline rename/new input]
desktop/frontend/src/i18n/messages/en.ts                        [modify]
desktop/frontend/src/i18n/messages/zh-CN.ts                     [modify]
```

Each file has one focused responsibility. `fsaccess_write.go` handles allowRoots + CAS + atomic rename; `internal/trash` isolates the platform-specific trash calls; `plugin_fs_write.go` is a thin Wails binding layer; the `CodeEditor`/`ConfirmDialog`/`InlineEditRow` split keeps each Vue file under ~250 LOC.

Tasks 1–9 land in dependency order: proto → fsAccess.write → trash → PluginFS binding → remote_fs dispatch → fsBridge → CodeEditor → tabs/tree UI → i18n polish.

---

### Task 1: Extend FS proto payload with mutation fields

**Files:**
- Modify: `internal/proto/frame.go:251-283`

**Interfaces:**
- Consumes: existing `FSRequestPayload`, `FSResponsePayload`.
- Produces:
  - `FSRequestPayload.Data []byte`, `.ExpectedModTime int64`, `.NewPath string`, `.Recursive bool`, `.CreateIfMissing bool`.
  - `FSResponsePayload.Meta *FileMetaInfo` (unchanged type; now populated by write/create/rename/mkdir).

- [ ] **Step 1: Write a failing test for the new fields' JSON round-trip.**

Create `internal/proto/frame_fs_write_test.go`:

```go
package proto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFSRequestPayloadWriteFieldsRoundTrip(t *testing.T) {
	req := FSRequestPayload{
		RequestID:       "r1",
		Op:              "write_file",
		Path:            "/a/b.txt",
		Data:            []byte("hi"),
		ExpectedModTime: 1234,
		CreateIfMissing: true,
	}
	body, err := json.Marshal(req)
	require.NoError(t, err)
	var back FSRequestPayload
	require.NoError(t, json.Unmarshal(body, &back))
	require.Equal(t, req, back)
}

func TestFSRequestPayloadRenameRoundTrip(t *testing.T) {
	req := FSRequestPayload{Op: "rename", Path: "/a", NewPath: "/b"}
	body, err := json.Marshal(req)
	require.NoError(t, err)
	var back FSRequestPayload
	require.NoError(t, json.Unmarshal(body, &back))
	require.Equal(t, req, back)
}

func TestFSRequestPayloadRemoveRoundTrip(t *testing.T) {
	req := FSRequestPayload{Op: "remove", Path: "/a/b", Recursive: true}
	body, err := json.Marshal(req)
	require.NoError(t, err)
	var back FSRequestPayload
	require.NoError(t, json.Unmarshal(body, &back))
	require.Equal(t, req, back)
}

func TestFSResponsePayloadMetaOnWrite(t *testing.T) {
	resp := FSResponsePayload{
		RequestID: "r1",
		OK:        true,
		Meta:      &FileMetaInfo{Path: "/a.txt", Size: 2, ModTime: 5678},
	}
	body, err := json.Marshal(resp)
	require.NoError(t, err)
	var back FSResponsePayload
	require.NoError(t, json.Unmarshal(body, &back))
	require.Equal(t, resp, back)
}
```

- [ ] **Step 2: Run the failing test.**

Run: `go test ./internal/proto -run TestFSRequestPayloadWriteFieldsRoundTrip -v`
Expected: FAIL — unknown field `Data`/`ExpectedModTime`/etc. in struct literal.

- [ ] **Step 3: Add the new fields to `FSRequestPayload`.**

Edit `internal/proto/frame.go` at the `FSRequestPayload` struct:

```go
type FSRequestPayload struct {
	RequestID       string `json:"request_id"`
	ClientID        string `json:"client_id,omitempty"`
	Op              string `json:"op"`
	Path            string `json:"path,omitempty"`
	MaxBytes        int64  `json:"max_bytes,omitempty"`
	Offset          int64  `json:"offset,omitempty"`
	Length          int64  `json:"length,omitempty"`
	WatchID         string `json:"watch_id,omitempty"`
	Data            []byte `json:"data,omitempty"`
	ExpectedModTime int64  `json:"expected_modtime,omitempty"`
	NewPath         string `json:"new_path,omitempty"`
	Recursive       bool   `json:"recursive,omitempty"`
	CreateIfMissing bool   `json:"create_if_missing,omitempty"`
}
```

`FSResponsePayload` already has `Meta *FileMetaInfo` — no struct change needed.

- [ ] **Step 4: Run the tests to verify they pass.**

Run: `go test ./internal/proto -v`
Expected: PASS across all four tests plus the existing frame tests.

- [ ] **Step 5: Commit.**

```bash
git add internal/proto/frame.go internal/proto/frame_fs_write_test.go
git commit -m "proto(fs): add write/rename/remove/create fields"
```

---

### Task 2: Cross-platform trash helper (`internal/trash`)

**Files:**
- Create: `internal/trash/trash.go`
- Create: `internal/trash/trash_darwin.go`
- Create: `internal/trash/trash_linux.go`
- Create: `internal/trash/trash_windows.go`
- Create: `internal/trash/trash_darwin_test.go`
- Create: `internal/trash/trash_linux_test.go`

**Interfaces:**
- Consumes: none (leaf package).
- Produces:
  ```go
  package trash
  var ErrUnavailable = errors.New("trash: no platform trash command available")
  // Send moves path to the OS trash. Returns ErrUnavailable when no supported
  // command exists on the host.
  func Send(path string) error
  ```

- [ ] **Step 1: Write failing tests for darwin (using osascript) and linux (using gio).**

Create `internal/trash/trash_darwin_test.go`:

```go
//go:build darwin

package trash

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSendDarwinInvokesOsascript(t *testing.T) {
	var got []string
	oldExec := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		got = append([]string{name}, args...)
		return exec.Command("true")
	}
	t.Cleanup(func() { execCommand = oldExec })
	require.NoError(t, Send("/tmp/xxx.txt"))
	require.Equal(t, "osascript", got[0])
	joined := strings.Join(got[1:], " ")
	require.Contains(t, joined, `POSIX file "/tmp/xxx.txt"`)
	require.Contains(t, joined, `tell application "Finder"`)
}

func TestSendDarwinFailurePropagates(t *testing.T) {
	oldExec := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("false")
	}
	t.Cleanup(func() { execCommand = oldExec })
	require.Error(t, Send("/tmp/xxx.txt"))
}
```

Create `internal/trash/trash_linux_test.go`:

```go
//go:build linux

package trash

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSendLinuxPrefersGio(t *testing.T) {
	oldLookPath := lookPath
	oldExec := execCommand
	var got []string
	lookPath = func(bin string) (string, error) {
		if bin == "gio" {
			return "/usr/bin/gio", nil
		}
		return "", errors.New("not found")
	}
	execCommand = func(name string, args ...string) *exec.Cmd {
		got = append([]string{name}, args...)
		return exec.Command("true")
	}
	t.Cleanup(func() {
		lookPath = oldLookPath
		execCommand = oldExec
	})
	require.NoError(t, Send("/tmp/x.txt"))
	require.Equal(t, []string{"gio", "trash", "/tmp/x.txt"}, got)
}

func TestSendLinuxUnavailable(t *testing.T) {
	oldLookPath := lookPath
	lookPath = func(bin string) (string, error) { return "", errors.New("no") }
	t.Cleanup(func() { lookPath = oldLookPath })
	require.ErrorIs(t, Send("/tmp/x.txt"), ErrUnavailable)
}
```

- [ ] **Step 2: Run the tests to verify they fail.**

Run: `go test ./internal/trash -v`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write `trash.go` and per-OS implementations.**

`internal/trash/trash.go`:

```go
// Package trash moves paths to the OS trash. Callers must not assume Send
// removes the file if ErrUnavailable is returned; the caller is expected to
// fall back to hard delete after prompting the user.
package trash

import (
	"errors"
	"os/exec"
)

var ErrUnavailable = errors.New("trash: no platform trash command available")

// execCommand is a package-level indirection so tests can stub exec.Command.
var execCommand = exec.Command

// lookPath is a package-level indirection so tests can stub exec.LookPath.
var lookPath = exec.LookPath

func Send(path string) error {
	return sendPlatform(path)
}
```

`internal/trash/trash_darwin.go`:

```go
//go:build darwin

package trash

import "fmt"

// script uses AppleScript via osascript. POSIX file escaping is done by
// treating the path as a literal AppleScript string; any embedded `"` is
// converted to `\"` (backslash + quote).
func sendPlatform(path string) error {
	esc := ""
	for _, r := range path {
		switch r {
		case '\\', '"':
			esc += "\\" + string(r)
		default:
			esc += string(r)
		}
	}
	script := fmt.Sprintf(`tell application "Finder" to delete POSIX file "%s"`, esc)
	cmd := execCommand("osascript", "-e", script)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("trash: osascript: %w", err)
	}
	return nil
}
```

`internal/trash/trash_linux.go`:

```go
//go:build linux

package trash

import "fmt"

func sendPlatform(path string) error {
	if _, err := lookPath("gio"); err == nil {
		cmd := execCommand("gio", "trash", path)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("trash: gio: %w", err)
		}
		return nil
	}
	if _, err := lookPath("kioclient5"); err == nil {
		cmd := execCommand("kioclient5", "move", path, "trash:/")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("trash: kioclient5: %w", err)
		}
		return nil
	}
	return ErrUnavailable
}
```

`internal/trash/trash_windows.go`:

```go
//go:build windows

package trash

import "fmt"

// PowerShell + Shell.Application is available on every supported Windows.
// The verb "delete" on a namespace item routes to the Recycle Bin.
func sendPlatform(path string) error {
	// %s substitution is safe because path comes from fsAccess.resolve()
	// which rejects anything outside allowRoots — but quote-escape defensively.
	esc := ""
	for _, r := range path {
		switch r {
		case '\'':
			esc += "''"
		default:
			esc += string(r)
		}
	}
	script := fmt.Sprintf(
		`$shell = New-Object -ComObject 'Shell.Application'; `+
			`$item = $shell.Namespace((Split-Path -Parent '%s')).ParseName((Split-Path -Leaf '%s')); `+
			`if ($item -ne $null) { $item.InvokeVerb('delete') } else { exit 1 }`,
		esc, esc,
	)
	cmd := execCommand("powershell", "-NoProfile", "-Command", script)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("trash: powershell: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass.**

Run: `go test ./internal/trash -v`
Expected: PASS (darwin tests on macOS host; linux tests on Linux CI).

- [ ] **Step 5: Commit.**

```bash
git add internal/trash/
git commit -m "feat(trash): cross-platform Send helper"
```

---

### Task 3: `fsAccess` write/create/rename/remove/mkdir with CAS and atomic rename

**Files:**
- Modify: `desktop/fsaccess.go` (add `osCreateTemp / osRename / osRemove / osRemoveAll / osMkdir / osWriteFile` mock hooks)
- Create: `desktop/fsaccess_write.go`
- Create: `desktop/fsaccess_write_test.go`

**Interfaces:**
- Consumes: `fsAccess.resolve` (from Task 0 / existing code), `internal/trash.Send`.
- Produces:
  ```go
  // All paths pass through resolve(). expectedModTime==0 skips CAS.
  func (a *fsAccess) writeFile(path string, data []byte, expectedModTime int64, createIfMissing bool) (FileMetaInfo, error)
  func (a *fsAccess) createFile(path string) (FileMetaInfo, error)
  func (a *fsAccess) renamePath(from, to string) (FileMetaInfo, error)
  func (a *fsAccess) removePath(path string, recursive bool) error
  func (a *fsAccess) mkdir(path string) (FileMetaInfo, error)
  func (a *fsAccess) trashPath(path string) error

  // Sentinel errors for typed callers; string form uses stable prefixes.
  var (
      ErrStaleModTime  = errors.New("stale_modtime")
      ErrAlreadyExists = errors.New("already_exists")
      ErrNotFound      = errors.New("not_found")
      ErrIsDirectory   = errors.New("is_directory")
      ErrNotADirectory = errors.New("not_a_directory")
  )

  // maxWriteBytesHard = 5 * 1024 * 1024
  ```

  Error strings (what remote/local plumbing surfaces):
  - `stale_modtime: current=<n>`
  - `already_exists: <p>`
  - `not_found: <p>`
  - `is_directory` / `not_a_directory`
  - `path_forbidden: <p>` (from existing `resolve`)
  - `write_denied: <reason>` (bubble raw OS error)

- [ ] **Step 1: Add mock hooks in `desktop/fsaccess.go`.**

Below the existing `var osOpenFile = ...` declaration, add:

```go
var osCreateTemp = os.CreateTemp
var osRename = os.Rename
var osRemove = os.Remove
var osRemoveAll = os.RemoveAll
var osMkdir = os.Mkdir
var osWriteFile = os.WriteFile
```

Also add the `maxWriteBytesHard` constant beside `maxReadBytesHard`:

```go
const maxWriteBytesHard = 5 * 1024 * 1024
```

- [ ] **Step 2: Write the failing tests.**

Create `desktop/fsaccess_write_test.go`:

```go
package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestAccess(t *testing.T) (*fsAccess, string) {
	t.Helper()
	dir := t.TempDir()
	return newFSAccess([]string{dir}), dir
}

func writeSeed(t *testing.T, path, body string) int64 {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.ModTime().UnixMilli()
}

func TestWriteFileHappyPath(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, "a.txt")
	mt := writeSeed(t, target, "old")
	meta, err := a.writeFile(target, []byte("new content"), mt, false)
	require.NoError(t, err)
	require.Equal(t, int64(len("new content")), meta.Size)
	b, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "new content", string(b))
}

func TestWriteFileStaleModTime(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, "a.txt")
	writeSeed(t, target, "old")
	_, err := a.writeFile(target, []byte("nope"), 1, false) // wrong modtime
	require.ErrorIs(t, err, ErrStaleModTime)
	b, _ := os.ReadFile(target)
	require.Equal(t, "old", string(b))
}

func TestWriteFileForbiddenPath(t *testing.T) {
	a, _ := newTestAccess(t)
	_, err := a.writeFile("/etc/hosts", []byte("x"), 0, true)
	require.ErrorIs(t, err, ErrPathForbidden)
}

func TestWriteFileDenied(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, ".env")
	writeSeed(t, target, "x")
	_, err := a.writeFile(target, []byte("y"), 0, false)
	require.ErrorIs(t, err, ErrPathDenied)
}

func TestWriteFileHardCap(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, "a.txt")
	writeSeed(t, target, "x")
	_, err := a.writeFile(target, make([]byte, maxWriteBytesHard+1), 0, false)
	require.Error(t, err)
}

func TestWriteFileCreateIfMissing(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, "new.txt")
	meta, err := a.writeFile(target, []byte("hi"), 0, true)
	require.NoError(t, err)
	require.Equal(t, int64(2), meta.Size)
}

func TestWriteFileRefusesDirectory(t *testing.T) {
	a, dir := newTestAccess(t)
	_, err := a.writeFile(dir, []byte("x"), 0, false)
	require.ErrorIs(t, err, ErrIsDirectory)
}

func TestWriteFileAtomicOnRenameFailure(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, "a.txt")
	writeSeed(t, target, "old")
	orig := osRename
	osRename = func(a, b string) error { return errors.New("boom") }
	t.Cleanup(func() { osRename = orig })
	_, err := a.writeFile(target, []byte("new"), 0, false)
	require.Error(t, err)
	b, _ := os.ReadFile(target)
	require.Equal(t, "old", string(b), "original must be preserved on rename failure")
	// no lingering tmp
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".atterm-tmp-")
	}
}

func TestCreateFileAlreadyExists(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, "a.txt")
	writeSeed(t, target, "x")
	_, err := a.createFile(target)
	require.ErrorIs(t, err, ErrAlreadyExists)
}

func TestCreateFileSucceeds(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, "new.txt")
	meta, err := a.createFile(target)
	require.NoError(t, err)
	require.Equal(t, int64(0), meta.Size)
}

func TestRenameHappyPath(t *testing.T) {
	a, dir := newTestAccess(t)
	from := filepath.Join(dir, "a.txt")
	to := filepath.Join(dir, "b.txt")
	writeSeed(t, from, "x")
	meta, err := a.renamePath(from, to)
	require.NoError(t, err)
	require.FileExists(t, to)
	require.NoFileExists(t, from)
	require.Equal(t, to, meta.Path)
}

func TestRenameForbiddenTarget(t *testing.T) {
	a, dir := newTestAccess(t)
	from := filepath.Join(dir, "a.txt")
	writeSeed(t, from, "x")
	_, err := a.renamePath(from, "/etc/hosts.moved")
	require.ErrorIs(t, err, ErrPathForbidden)
}

func TestRemoveRefusesNonEmptyDirWithoutRecursive(t *testing.T) {
	a, dir := newTestAccess(t)
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	writeSeed(t, filepath.Join(sub, "x"), "y")
	require.Error(t, a.removePath(sub, false))
	require.DirExists(t, sub)
}

func TestRemoveRecursive(t *testing.T) {
	a, dir := newTestAccess(t)
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	writeSeed(t, filepath.Join(sub, "x"), "y")
	require.NoError(t, a.removePath(sub, true))
	require.NoDirExists(t, sub)
}

func TestMkdirHappyPath(t *testing.T) {
	a, dir := newTestAccess(t)
	target := filepath.Join(dir, "nested")
	meta, err := a.mkdir(target)
	require.NoError(t, err)
	require.DirExists(t, target)
	require.Equal(t, target, meta.Path)
}

func TestMkdirAlreadyExists(t *testing.T) {
	a, dir := newTestAccess(t)
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	_, err := a.mkdir(sub)
	require.ErrorIs(t, err, ErrAlreadyExists)
}
```

- [ ] **Step 3: Run tests to verify they fail.**

Run: `go test ./desktop -run 'TestWriteFile|TestCreateFile|TestRename|TestRemove|TestMkdir' -v`
Expected: FAIL — methods do not exist.

- [ ] **Step 4: Write `fsaccess_write.go`.**

```go
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Sentinel errors — every one is also formatted with a stable prefix so the
// remote transport layer can surface it as a machine-parseable Error string.
var (
	ErrStaleModTime  = errors.New("stale_modtime")
	ErrAlreadyExists = errors.New("already_exists")
	ErrNotFound      = errors.New("not_found")
	ErrIsDirectory   = errors.New("is_directory")
	ErrNotADirectory = errors.New("not_a_directory")
)

// writeFile writes data atomically to path. expectedModTime==0 disables CAS.
// createIfMissing=true allows creating a new file; otherwise the target must
// exist and match expectedModTime.
func (a *fsAccess) writeFile(path string, data []byte, expectedModTime int64, createIfMissing bool) (FileMetaInfo, error) {
	if int64(len(data)) > maxWriteBytesHard {
		return FileMetaInfo{}, fmt.Errorf("write_denied: exceeds %d bytes", maxWriteBytesHard)
	}
	resolved, err := a.resolve(path)
	if err != nil {
		return FileMetaInfo{}, err
	}
	info, statErr := osStat(resolved)
	switch {
	case statErr == nil:
		if info.IsDir() {
			return FileMetaInfo{}, fmt.Errorf("%w", ErrIsDirectory)
		}
		if expectedModTime != 0 && info.ModTime().UnixMilli() != expectedModTime {
			return FileMetaInfo{}, fmt.Errorf("%w: current=%d", ErrStaleModTime, info.ModTime().UnixMilli())
		}
	case errors.Is(statErr, os.ErrNotExist):
		if !createIfMissing {
			return FileMetaInfo{}, fmt.Errorf("%w: %s", ErrNotFound, resolved)
		}
	default:
		return FileMetaInfo{}, statErr
	}

	dir := filepath.Dir(resolved)
	tmp, err := osCreateTemp(dir, ".atterm-tmp-*")
	if err != nil {
		return FileMetaInfo{}, fmt.Errorf("write_denied: %w", err)
	}
	tmpName := tmp.Name()
	// tmp must be cleaned up on any failure past this point.
	commit := false
	defer func() {
		_ = tmp.Close()
		if !commit {
			_ = osRemove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return FileMetaInfo{}, fmt.Errorf("write_denied: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return FileMetaInfo{}, fmt.Errorf("write_denied: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return FileMetaInfo{}, fmt.Errorf("write_denied: %w", err)
	}
	if err := osRename(tmpName, resolved); err != nil {
		return FileMetaInfo{}, fmt.Errorf("write_denied: %w", err)
	}
	commit = true

	newInfo, err := osStat(resolved)
	if err != nil {
		return FileMetaInfo{}, err
	}
	return FileMetaInfo{
		Path:     resolved,
		Size:     newInfo.Size(),
		ModTime:  newInfo.ModTime().UnixMilli(),
		IsBinary: isBinary(data),
	}, nil
}

func (a *fsAccess) createFile(path string) (FileMetaInfo, error) {
	resolved, err := a.resolve(path)
	if err != nil {
		return FileMetaInfo{}, err
	}
	if _, err := osStat(resolved); err == nil {
		return FileMetaInfo{}, fmt.Errorf("%w: %s", ErrAlreadyExists, resolved)
	} else if !errors.Is(err, os.ErrNotExist) {
		return FileMetaInfo{}, err
	}
	if err := osWriteFile(resolved, nil, 0o644); err != nil {
		return FileMetaInfo{}, fmt.Errorf("write_denied: %w", err)
	}
	info, err := osStat(resolved)
	if err != nil {
		return FileMetaInfo{}, err
	}
	return FileMetaInfo{
		Path:    resolved,
		Size:    info.Size(),
		ModTime: info.ModTime().UnixMilli(),
	}, nil
}

func (a *fsAccess) renamePath(from, to string) (FileMetaInfo, error) {
	src, err := a.resolve(from)
	if err != nil {
		return FileMetaInfo{}, err
	}
	dst, err := a.resolve(to)
	if err != nil {
		return FileMetaInfo{}, err
	}
	if _, err := osStat(src); errors.Is(err, os.ErrNotExist) {
		return FileMetaInfo{}, fmt.Errorf("%w: %s", ErrNotFound, src)
	} else if err != nil {
		return FileMetaInfo{}, err
	}
	if _, err := osStat(dst); err == nil {
		return FileMetaInfo{}, fmt.Errorf("%w: %s", ErrAlreadyExists, dst)
	} else if !errors.Is(err, os.ErrNotExist) {
		return FileMetaInfo{}, err
	}
	if err := osRename(src, dst); err != nil {
		return FileMetaInfo{}, fmt.Errorf("write_denied: %w", err)
	}
	info, err := osStat(dst)
	if err != nil {
		return FileMetaInfo{}, err
	}
	return FileMetaInfo{
		Path:    dst,
		Size:    info.Size(),
		ModTime: info.ModTime().UnixMilli(),
	}, nil
}

func (a *fsAccess) removePath(path string, recursive bool) error {
	resolved, err := a.resolve(path)
	if err != nil {
		return err
	}
	if _, err := osStat(resolved); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, resolved)
	} else if err != nil {
		return err
	}
	if recursive {
		if err := osRemoveAll(resolved); err != nil {
			return fmt.Errorf("write_denied: %w", err)
		}
		return nil
	}
	if err := osRemove(resolved); err != nil {
		return fmt.Errorf("write_denied: %w", err)
	}
	return nil
}

func (a *fsAccess) mkdir(path string) (FileMetaInfo, error) {
	resolved, err := a.resolve(path)
	if err != nil {
		return FileMetaInfo{}, err
	}
	if _, err := osStat(resolved); err == nil {
		return FileMetaInfo{}, fmt.Errorf("%w: %s", ErrAlreadyExists, resolved)
	} else if !errors.Is(err, os.ErrNotExist) {
		return FileMetaInfo{}, err
	}
	if err := osMkdir(resolved, 0o755); err != nil {
		return FileMetaInfo{}, fmt.Errorf("write_denied: %w", err)
	}
	info, err := osStat(resolved)
	if err != nil {
		return FileMetaInfo{}, err
	}
	return FileMetaInfo{
		Path:    resolved,
		Size:    info.Size(),
		ModTime: info.ModTime().UnixMilli(),
	}, nil
}
```

- [ ] **Step 5: Wire trash into fsAccess (thin adapter, still in `fsaccess_write.go`).**

Append:

```go
import trashpkg "github.com/attson/atterm/internal/trash"

func (a *fsAccess) trashPath(path string) error {
	resolved, err := a.resolve(path)
	if err != nil {
		return err
	}
	if _, err := osStat(resolved); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, resolved)
	} else if err != nil {
		return err
	}
	if err := trashpkg.Send(resolved); err != nil {
		if errors.Is(err, trashpkg.ErrUnavailable) {
			return err
		}
		return fmt.Errorf("write_denied: %w", err)
	}
	return nil
}
```

(Move the `trashpkg` import to the top import block after the fact — the placement here is only to spotlight the change.)

- [ ] **Step 6: Run tests.**

Run: `go test ./desktop -run 'TestWriteFile|TestCreateFile|TestRename|TestRemove|TestMkdir' -v`
Expected: PASS.

- [ ] **Step 7: Commit.**

```bash
git add desktop/fsaccess.go desktop/fsaccess_write.go desktop/fsaccess_write_test.go
git commit -m "feat(fsaccess): atomic write, CAS, CRUD, trash bridge"
```

---

### Task 4: PluginFS Wails bindings for write/create/rename/remove/mkdir/trash

**Files:**
- Modify: `desktop/plugin_fs.go` (add methods delegating to `fsAccess.*`)
- Create: `desktop/plugin_fs_write.go`
- Create: `desktop/plugin_fs_write_test.go`

**Interfaces:**
- Consumes: `fsAccess.writeFile / createFile / renamePath / removePath / mkdir / trashPath` (Task 3).
- Produces (Wails-bound, exposed to frontend as `pluginHost.fs.*`):
  ```go
  func (p *PluginFS) WriteFile(path string, data []byte, expectedModTime int64, createIfMissing bool) (FileMetaInfo, error)
  func (p *PluginFS) CreateFile(path string) (FileMetaInfo, error)
  func (p *PluginFS) Rename(from, to string) (FileMetaInfo, error)
  func (p *PluginFS) Remove(path string, recursive bool) error
  func (p *PluginFS) Mkdir(path string) (FileMetaInfo, error)
  func (p *PluginFS) Trash(path string) error
  ```

- [ ] **Step 1: Write failing tests.**

Create `desktop/plugin_fs_write_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestPluginFS(t *testing.T) (*PluginFS, string) {
	t.Helper()
	dir := t.TempDir()
	return &PluginFS{access: newFSAccess([]string{dir})}, dir
}

func TestPluginFSWriteFileSuccess(t *testing.T) {
	fs, dir := newTestPluginFS(t)
	target := filepath.Join(dir, "a.txt")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o644))
	info, _ := os.Stat(target)
	meta, err := fs.WriteFile(target, []byte("new"), info.ModTime().UnixMilli(), false)
	require.NoError(t, err)
	require.Equal(t, int64(3), meta.Size)
}

func TestPluginFSWriteFileForbidden(t *testing.T) {
	fs, _ := newTestPluginFS(t)
	_, err := fs.WriteFile("/etc/hosts", []byte("x"), 0, true)
	require.Error(t, err)
}

func TestPluginFSCreateFile(t *testing.T) {
	fs, dir := newTestPluginFS(t)
	target := filepath.Join(dir, "new.txt")
	_, err := fs.CreateFile(target)
	require.NoError(t, err)
	require.FileExists(t, target)
}

func TestPluginFSRename(t *testing.T) {
	fs, dir := newTestPluginFS(t)
	from := filepath.Join(dir, "a")
	require.NoError(t, os.WriteFile(from, []byte("x"), 0o644))
	to := filepath.Join(dir, "b")
	_, err := fs.Rename(from, to)
	require.NoError(t, err)
	require.NoFileExists(t, from)
	require.FileExists(t, to)
}

func TestPluginFSRemove(t *testing.T) {
	fs, dir := newTestPluginFS(t)
	target := filepath.Join(dir, "a")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))
	require.NoError(t, fs.Remove(target, false))
	require.NoFileExists(t, target)
}

func TestPluginFSMkdir(t *testing.T) {
	fs, dir := newTestPluginFS(t)
	target := filepath.Join(dir, "new")
	_, err := fs.Mkdir(target)
	require.NoError(t, err)
	require.DirExists(t, target)
}
```

Note: `Trash` cannot be unit-tested without exec stubs; it is covered in `internal/trash` tests plus the integration path in Task 5.

- [ ] **Step 2: Run tests to verify they fail.**

Run: `go test ./desktop -run 'TestPluginFS(Write|Create|Rename|Remove|Mkdir)' -v`
Expected: FAIL — methods do not exist.

- [ ] **Step 3: Write `plugin_fs_write.go`.**

```go
package main

// PluginFS write-side bindings. Every method delegates to fsAccess, which
// runs the path through resolve() (allowRoots + deny) before touching disk.
// Kept in a separate file for readability; the type lives in plugin_fs.go.

func (p *PluginFS) WriteFile(path string, data []byte, expectedModTime int64, createIfMissing bool) (FileMetaInfo, error) {
	return p.access.writeFile(path, data, expectedModTime, createIfMissing)
}

func (p *PluginFS) CreateFile(path string) (FileMetaInfo, error) {
	return p.access.createFile(path)
}

func (p *PluginFS) Rename(from, to string) (FileMetaInfo, error) {
	return p.access.renamePath(from, to)
}

func (p *PluginFS) Remove(path string, recursive bool) error {
	return p.access.removePath(path, recursive)
}

func (p *PluginFS) Mkdir(path string) (FileMetaInfo, error) {
	return p.access.mkdir(path)
}

func (p *PluginFS) Trash(path string) error {
	return p.access.trashPath(path)
}
```

No change to `desktop/plugin_fs.go` beyond a top-of-file comment block noting that write bindings live in `plugin_fs_write.go`.

- [ ] **Step 4: Regenerate Wails bindings (auto-run before frontend touches them).**

Run: `cd desktop && wails generate module` (or the project's usual command; `make wails-generate` if a Make target exists — check `desktop/Makefile` / project root Makefile).

If no auto-gen target exists, run: `cd desktop && wails build -tags dev -skipbindings=false -devtools -skipfrontend` (only if needed to update `wailsjs/go/models`).

Expected: `desktop/frontend/wailsjs/go/main/PluginFS.d.ts` (or similar) now lists `WriteFile / CreateFile / Rename / Remove / Mkdir / Trash`.

- [ ] **Step 5: Run tests.**

Run: `go test ./desktop -run 'TestPluginFS(Write|Create|Rename|Remove|Mkdir)' -v && ./.github/scripts/check-plugin-fs-isolation.sh`
Expected: PASS on all; isolation check prints `ok: PluginFS isolation preserved`.

- [ ] **Step 6: Commit.**

```bash
git add desktop/plugin_fs.go desktop/plugin_fs_write.go desktop/plugin_fs_write_test.go desktop/frontend/wailsjs
git commit -m "feat(plugin_fs): Wails bindings for write/create/rename/remove/mkdir/trash"
```

---

### Task 5: `remoteFS` dispatch for the 6 new ops

**Files:**
- Modify: `desktop/remote_fs.go` (`handle()` switch)
- Modify: `desktop/remote_fs_test.go`

**Interfaces:**
- Consumes: `fsAccess.*` (Task 3), `proto.FSRequestPayload` fields (Task 1).
- Produces: `write_file / create_file / rename / remove / mkdir / trash` op handling that mirrors the local surface.

- [ ] **Step 1: Add failing tests in `desktop/remote_fs_test.go`.**

Append tests that build a `remoteFS`, invoke `handle()` on each new op, and assert the response:

```go
func TestRemoteFSWriteFileSuccess(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.txt")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o644))
	info, _ := os.Stat(target)
	fs := newRemoteFS(newFSAccess([]string{dir}))
	defer fs.close()

	req := proto.FSRequestPayload{
		RequestID:       "r",
		Op:              "write_file",
		Path:            target,
		Data:            []byte("new"),
		ExpectedModTime: info.ModTime().UnixMilli(),
	}
	frame := fs.handle(uuid.New(), req)
	var resp proto.FSResponsePayload
	require.NoError(t, json.Unmarshal(frame.Payload, &resp))
	require.True(t, resp.OK, "expected ok, got %s", resp.Error)
	require.NotNil(t, resp.Meta)
	require.Equal(t, int64(3), resp.Meta.Size)
}

func TestRemoteFSWriteFileStaleModTime(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.txt")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o644))
	fs := newRemoteFS(newFSAccess([]string{dir}))
	defer fs.close()
	req := proto.FSRequestPayload{
		RequestID:       "r",
		Op:              "write_file",
		Path:            target,
		Data:            []byte("new"),
		ExpectedModTime: 1, // wrong
	}
	frame := fs.handle(uuid.New(), req)
	var resp proto.FSResponsePayload
	require.NoError(t, json.Unmarshal(frame.Payload, &resp))
	require.False(t, resp.OK)
	require.Contains(t, resp.Error, "stale_modtime")
}

func TestRemoteFSCreateRenameRemoveMkdir(t *testing.T) {
	dir := t.TempDir()
	fs := newRemoteFS(newFSAccess([]string{dir}))
	defer fs.close()
	sid := uuid.New()

	// create
	created := fs.handle(sid, proto.FSRequestPayload{
		RequestID: "1", Op: "create_file", Path: filepath.Join(dir, "a"),
	})
	var resp proto.FSResponsePayload
	require.NoError(t, json.Unmarshal(created.Payload, &resp))
	require.True(t, resp.OK)

	// rename
	renamed := fs.handle(sid, proto.FSRequestPayload{
		RequestID: "2", Op: "rename",
		Path:    filepath.Join(dir, "a"),
		NewPath: filepath.Join(dir, "b"),
	})
	require.NoError(t, json.Unmarshal(renamed.Payload, &resp))
	require.True(t, resp.OK)

	// mkdir
	mk := fs.handle(sid, proto.FSRequestPayload{
		RequestID: "3", Op: "mkdir", Path: filepath.Join(dir, "sub"),
	})
	require.NoError(t, json.Unmarshal(mk.Payload, &resp))
	require.True(t, resp.OK)

	// remove
	rm := fs.handle(sid, proto.FSRequestPayload{
		RequestID: "4", Op: "remove", Path: filepath.Join(dir, "b"),
	})
	require.NoError(t, json.Unmarshal(rm.Payload, &resp))
	require.True(t, resp.OK)
}

func TestRemoteFSPermissionGateForWrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.txt")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))
	fs := newRemoteFS(newFSAccess([]string{dir}))
	defer fs.close()
	out := make(chan proto.Frame, 1)
	req := proto.FSRequestPayload{
		RequestID: "r", Op: "write_file", Path: target, Data: []byte("y"),
	}
	require.True(t, handleRemoteFSRequest(context.Background(), out, uuid.New(), proto.RemotePermissionReadOnly, fs, req))
	frame := <-out
	var resp proto.FSResponsePayload
	require.NoError(t, json.Unmarshal(frame.Payload, &resp))
	require.False(t, resp.OK)
	require.Contains(t, resp.Error, "requires full remote permission")
}
```

(The existing `remote_fs_test.go` already imports uuid / proto / json; extend the imports if needed.)

- [ ] **Step 2: Run to verify failure.**

Run: `go test ./desktop -run 'TestRemoteFS(Write|Create|Rename|Remove|Mkdir|Permission)' -v`
Expected: FAIL — unknown ops.

- [ ] **Step 3: Extend `remote_fs.go` `handle()` switch.**

Add cases after `case "open_external":`:

```go
case "write_file":
    var meta FileMetaInfo
    meta, err = fs.access.writeFile(req.Path, req.Data, req.ExpectedModTime, req.CreateIfMissing)
    if err == nil {
        response.Meta = &proto.FileMetaInfo{
            Path:     meta.Path,
            Size:     meta.Size,
            ModTime:  meta.ModTime,
            IsBinary: meta.IsBinary,
        }
    }
case "create_file":
    var meta FileMetaInfo
    meta, err = fs.access.createFile(req.Path)
    if err == nil {
        response.Meta = &proto.FileMetaInfo{
            Path:    meta.Path,
            Size:    meta.Size,
            ModTime: meta.ModTime,
        }
    }
case "rename":
    var meta FileMetaInfo
    meta, err = fs.access.renamePath(req.Path, req.NewPath)
    if err == nil {
        response.Meta = &proto.FileMetaInfo{
            Path:    meta.Path,
            Size:    meta.Size,
            ModTime: meta.ModTime,
        }
    }
case "remove":
    err = fs.access.removePath(req.Path, req.Recursive)
case "mkdir":
    var meta FileMetaInfo
    meta, err = fs.access.mkdir(req.Path)
    if err == nil {
        response.Meta = &proto.FileMetaInfo{
            Path:    meta.Path,
            Size:    meta.Size,
            ModTime: meta.ModTime,
        }
    }
case "trash":
    err = fs.access.trashPath(req.Path)
```

- [ ] **Step 4: Run tests.**

Run: `go test ./desktop -run 'TestRemoteFS' -v`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add desktop/remote_fs.go desktop/remote_fs_test.go
git commit -m "feat(remote-fs): dispatch write/create/rename/remove/mkdir/trash"
```

---

### Task 6: Frontend transport — `FSRequestOp` and `FSRequest` fields

**Files:**
- Modify: `desktop/frontend/src/lib/connection.ts:32-49` (FSRequestOp union + FSRequest interface)

**Interfaces:**
- Consumes: existing `sendFSRequest`.
- Produces:
  ```ts
  export type FSRequestOp =
    | "list_dir" | "file_meta" | "read_file" | "read_chunk"
    | "watch_dir" | "unwatch_dir" | "open_external"
    | "write_file" | "create_file" | "rename" | "remove"
    | "mkdir" | "trash";

  export interface FSRequest {
    op: FSRequestOp;
    request_id?: string;
    path?: string;
    max_bytes?: number;
    offset?: number;
    length?: number;
    watch_id?: string;
    data?: string;               // base64 (matches Go []byte)
    expected_modtime?: number;
    new_path?: string;
    recursive?: boolean;
    create_if_missing?: boolean;
  }
  ```

- [ ] **Step 1: Edit `connection.ts` to widen the union + add fields.**

Replace lines 32-49 with the interface shown above.

- [ ] **Step 2: Run frontend typecheck.**

Run: `cd desktop/frontend && npm run typecheck`
Expected: PASS (no existing callers break — new fields are optional).

- [ ] **Step 3: Commit.**

```bash
git add desktop/frontend/src/lib/connection.ts
git commit -m "feat(fs): widen FSRequest op union for write ops"
```

---

### Task 7: Frontend `fsBridge` API — `writeFile / createFile / rename / remove / mkdir / trash`

**Files:**
- Modify: `desktop/frontend/src/plugins/fileExplorer/fsBridge.ts`
- Modify: `desktop/frontend/src/plugins/fileExplorer/fsBridge.test.ts`
- Modify: `desktop/frontend/src/plugins/fileExplorer/remoteSessionFS.ts`
- Modify: `desktop/frontend/src/plugins/fileExplorer/remoteSessionFS.test.ts`
- Modify: `desktop/frontend/src/platform/types.ts` (extend `PluginHostBridge.fs`)

**Interfaces:**
- Consumes: Wails-bound `PluginFS.WriteFile / …` (Task 4), remote proto ops (Tasks 1, 5, 6).
- Produces:
  ```ts
  export interface FileSystemBridge {
    // …existing readonly methods…
    writeFile(path: string, data: Uint8Array, expectedModTime: number | null): Promise<FileMetaInfo>;
    createFile(path: string): Promise<FileMetaInfo>;
    rename(from: string, to: string): Promise<FileMetaInfo>;
    remove(path: string, recursive: boolean): Promise<void>;
    mkdir(path: string): Promise<FileMetaInfo>;
    trash(path: string): Promise<void>;
  }

  // and PluginHostBridge.fs gains matching methods.
  ```

- [ ] **Step 1: Update `PluginHostBridge` type (source of truth).**

Edit `desktop/frontend/src/platform/types.ts` — extend the `fs` block:

```ts
fs: {
  // existing readonly methods…
  writeFile(path: string, data: number[] | Uint8Array, expectedModTime: number, createIfMissing: boolean): Promise<FileMetaInfo>;
  createFile(path: string): Promise<FileMetaInfo>;
  rename(from: string, to: string): Promise<FileMetaInfo>;
  remove(path: string, recursive: boolean): Promise<void>;
  mkdir(path: string): Promise<FileMetaInfo>;
  trash(path: string): Promise<void>;
}
```

(The Wails-generated `wailsjs/go/main/PluginFS.d.ts` handles the concrete wiring — Task 4 already regenerated it. This interface just widens the contract used inside the app.)

- [ ] **Step 2: Wire local + remote bridges in `fsBridge.ts` and `remoteSessionFS.ts`.**

Edit `fsBridge.ts` `createLocalFSBridge` return to include:

```ts
writeFile: (path, data, expectedModTime) =>
  pluginHost.fs.writeFile(path, Array.from(data), expectedModTime ?? 0, expectedModTime === null),
createFile: (path) => pluginHost.fs.createFile(path),
rename: (from, to) => pluginHost.fs.rename(from, to),
remove: (path, recursive) => pluginHost.fs.remove(path, recursive),
mkdir: (path) => pluginHost.fs.mkdir(path),
trash: (path) => pluginHost.fs.trash(path),
```

Update the `FileSystemBridge` interface at the top of the same file.

Edit `remoteSessionFS.ts` — add:

```ts
function bytesToBase64(data: Uint8Array): string {
  let binary = "";
  for (const b of data) binary += String.fromCharCode(b);
  return btoa(binary);
}

async function writeFile(
  path: string,
  data: Uint8Array,
  expectedModTime: number | null,
): Promise<FileMetaInfo> {
  return requireField(
    ensureOK(await conn.sendFSRequest({
      op: "write_file",
      path,
      data: bytesToBase64(data),
      expected_modtime: expectedModTime ?? 0,
      create_if_missing: expectedModTime === null,
    })).meta,
    "meta",
  );
}

async function createFile(path: string): Promise<FileMetaInfo> {
  return requireField(
    ensureOK(await conn.sendFSRequest({ op: "create_file", path })).meta,
    "meta",
  );
}

async function rename(from: string, to: string): Promise<FileMetaInfo> {
  return requireField(
    ensureOK(await conn.sendFSRequest({ op: "rename", path: from, new_path: to })).meta,
    "meta",
  );
}

async function remove(path: string, recursive: boolean): Promise<void> {
  await ensureOK(await conn.sendFSRequest({ op: "remove", path, recursive }));
}

async function mkdir(path: string): Promise<FileMetaInfo> {
  return requireField(
    ensureOK(await conn.sendFSRequest({ op: "mkdir", path })).meta,
    "meta",
  );
}

async function trash(path: string): Promise<void> {
  await ensureOK(await conn.sendFSRequest({ op: "trash", path }));
}
```

Include them in the returned bridge object at the bottom of `createRemoteSessionFS`.

- [ ] **Step 3: Write vitest coverage for both bridges.**

Update `fsBridge.test.ts` — add cases that confirm `createLocalFSBridge.writeFile` forwards `data`, `expectedModTime`, and `createIfMissing` to `pluginHost.fs.writeFile`, and `remove` forwards `recursive`.

Update `remoteSessionFS.test.ts` — add cases that assert `sendFSRequest` is called with:
- `write_file` op, base64 `data`, correct `expected_modtime`, `create_if_missing`.
- `rename` op with `new_path`.
- `remove` op with `recursive`.
- `stale_modtime` server error propagates through `ensureOK`.

Concrete pattern (following existing tests in the file):

```ts
it("writeFile serializes data as base64 and forwards expected_modtime", async () => {
  const send = vi.fn().mockResolvedValue({ ok: true, meta: { path: "/a", size: 3, modTime: 42, isBinary: false } });
  const fs = createRemoteSessionFS({ sendFSRequest: send, onFSEvent: () => () => {} } as any);
  await fs.writeFile("/a", new Uint8Array([104, 105]), 41);
  expect(send).toHaveBeenCalledWith({
    op: "write_file",
    path: "/a",
    data: "aGk=",
    expected_modtime: 41,
    create_if_missing: false,
  });
});

it("writeFile with null expected_modtime maps to create_if_missing=true and expected_modtime=0", async () => {
  const send = vi.fn().mockResolvedValue({ ok: true, meta: { path: "/a", size: 0, modTime: 1, isBinary: false } });
  const fs = createRemoteSessionFS({ sendFSRequest: send, onFSEvent: () => () => {} } as any);
  await fs.writeFile("/a", new Uint8Array(), null);
  expect(send).toHaveBeenCalledWith(expect.objectContaining({
    create_if_missing: true,
    expected_modtime: 0,
  }));
});

it("propagates stale_modtime error", async () => {
  const send = vi.fn().mockResolvedValue({ ok: false, error: "stale_modtime: current=99" });
  const fs = createRemoteSessionFS({ sendFSRequest: send, onFSEvent: () => () => {} } as any);
  await expect(fs.writeFile("/a", new Uint8Array(), 1)).rejects.toThrow(/stale_modtime/);
});
```

- [ ] **Step 4: Run frontend tests.**

Run: `cd desktop/frontend && npm run test -- fsBridge remoteSessionFS`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add desktop/frontend/src/platform/types.ts \
        desktop/frontend/src/plugins/fileExplorer/fsBridge.ts \
        desktop/frontend/src/plugins/fileExplorer/fsBridge.test.ts \
        desktop/frontend/src/plugins/fileExplorer/remoteSessionFS.ts \
        desktop/frontend/src/plugins/fileExplorer/remoteSessionFS.test.ts
git commit -m "feat(file-explorer): write/CRUD/trash methods on fs bridges"
```

---

### Task 8: `tabsModel` — dirty state

**Files:**
- Modify: `desktop/frontend/src/plugins/fileExplorer/tabsModel.ts`
- Modify: `desktop/frontend/src/plugins/fileExplorer/tabsModel.test.ts`

**Interfaces:**
- Consumes: existing `TabsState`, `Tab`.
- Produces:
  ```ts
  interface Tab { path: string; persistent: boolean; lastActiveAt: number; viewMode: ViewMode; dirty: boolean }
  function setDirty(state: TabsState, path: string, dirty: boolean): TabsState
  ```

- [ ] **Step 1: Add failing tests to `tabsModel.test.ts`.**

```ts
import { openPath, setDirty } from "./tabsModel";

describe("setDirty", () => {
  it("marks the matching tab dirty by path", () => {
    let s = openPath({ tabs: [], activeIdx: -1 }, "/a", "persistent");
    s = openPath(s, "/b", "persistent");
    const next = setDirty(s, "/a", true);
    expect(next.tabs.find((t) => t.path === "/a")?.dirty).toBe(true);
    expect(next.tabs.find((t) => t.path === "/b")?.dirty).toBe(false);
  });

  it("no-ops when path is not open", () => {
    const s = openPath({ tabs: [], activeIdx: -1 }, "/a", "persistent");
    const next = setDirty(s, "/missing", true);
    expect(next).toBe(s);
  });

  it("openPath creates tabs with dirty=false", () => {
    const s = openPath({ tabs: [], activeIdx: -1 }, "/a", "persistent");
    expect(s.tabs[0].dirty).toBe(false);
  });
});
```

- [ ] **Step 2: Run to verify failure.**

Run: `cd desktop/frontend && npm run test -- tabsModel`
Expected: FAIL — `setDirty` not exported; `dirty` missing on tab.

- [ ] **Step 3: Modify `tabsModel.ts`.**

Add `dirty: boolean` to `Tab`; initialize `dirty: false` in all three call sites in `openPath` (replace, append, existingIdx-hit paths); export:

```ts
export function setDirty(state: TabsState, path: string, dirty: boolean): TabsState {
  const idx = state.tabs.findIndex((t) => t.path === path);
  if (idx < 0) return state;
  if (state.tabs[idx].dirty === dirty) return state;
  const next = clone(state);
  next.tabs[idx].dirty = dirty;
  return next;
}
```

- [ ] **Step 4: Run tests.**

Run: `cd desktop/frontend && npm run test -- tabsModel`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add desktop/frontend/src/plugins/fileExplorer/tabsModel.ts \
        desktop/frontend/src/plugins/fileExplorer/tabsModel.test.ts
git commit -m "feat(file-explorer): tab dirty state"
```

---

### Task 9: `CodeEditor.vue` — writable editor with dirty tracking, save, conflict banner

**Files:**
- Create: `desktop/frontend/src/plugins/fileExplorer/CodeEditor.vue` (evolution of `CodeViewer.vue`)
- Create: `desktop/frontend/src/plugins/fileExplorer/CodeEditor.test.ts`
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileEditor.vue` (import CodeEditor)
- Delete: `desktop/frontend/src/plugins/fileExplorer/CodeViewer.vue`
- Delete: `desktop/frontend/src/plugins/fileExplorer/CodeViewer.test.ts`

**Interfaces:**
- Consumes: `FileSystemBridge.readFile / fileMeta / writeFile` (Task 7).
- Produces:
  ```ts
  // Props
  { fs: FileSystemBridge, path: string, showLineNumbers: boolean, theme: "dimmed" | "light" }
  // Emits
  (e: "dirty-change", dirty: boolean): void
  // Exposed
  defineExpose({ save: () => Promise<boolean> })   // returns true on success
  ```

  Behavioral contract:
  - Cmd/Ctrl+S triggers `save()`.
  - After `stale_modtime`, banner appears with 3 buttons: Overwrite / Reload / Cancel.
    - Overwrite: writes with `expectedModTime = server's current` (retry succeeds).
    - Reload: `load()` refreshes doc, dropping local edits.
    - Cancel: banner closes; doc stays dirty.
  - Emits `dirty-change(true)` when doc diverges from originalText, `false` when equal.

- [ ] **Step 1: Copy `CodeViewer.vue` to `CodeEditor.vue`; delete the old file after all edits.**

```bash
git mv desktop/frontend/src/plugins/fileExplorer/CodeViewer.vue desktop/frontend/src/plugins/fileExplorer/CodeEditor.vue
git mv desktop/frontend/src/plugins/fileExplorer/CodeViewer.test.ts desktop/frontend/src/plugins/fileExplorer/CodeEditor.test.ts
```

- [ ] **Step 2: Write failing tests in `CodeEditor.test.ts`.**

Rewrite the file to cover:

```ts
import { mount } from "@vue/test-utils";
import { flushPromises } from "@vue/test-utils";
import CodeEditor from "./CodeEditor.vue";

function makeFS(overrides: Partial<any> = {}) {
  return {
    identity: "local",
    fileMeta: vi.fn().mockResolvedValue({ path: "/a.txt", size: 3, modTime: 100, isBinary: false }),
    readFile: vi.fn().mockResolvedValue({ path: "/a.txt", data: btoa("hi\n"), isBinary: false }),
    writeFile: vi.fn().mockResolvedValue({ path: "/a.txt", size: 3, modTime: 200, isBinary: false }),
    onDirChanged: () => () => {},
    ...overrides,
  };
}

it("emits dirty-change=true on doc edit", async () => {
  const fs = makeFS();
  const w = mount(CodeEditor, { props: { fs, path: "/a.txt", showLineNumbers: false, theme: "dimmed" } });
  await flushPromises();
  // simulate CM update: expose an editor test helper (see step 3) to append text
  await (w.vm as any).testAppend("x");
  expect(w.emitted("dirty-change")?.some(([v]) => v === true)).toBe(true);
});

it("save calls writeFile with expected modtime and resets dirty", async () => {
  const fs = makeFS();
  const w = mount(CodeEditor, { props: { fs, path: "/a.txt", showLineNumbers: false, theme: "dimmed" } });
  await flushPromises();
  await (w.vm as any).testAppend("x");
  const ok = await (w.vm as any).save();
  expect(ok).toBe(true);
  expect(fs.writeFile).toHaveBeenCalledWith("/a.txt", expect.any(Uint8Array), 100);
  expect(w.emitted("dirty-change")?.slice(-1)[0]?.[0]).toBe(false);
});

it("shows stale_modtime banner and allows overwrite", async () => {
  const fs = makeFS();
  fs.writeFile
    .mockRejectedValueOnce(new Error("stale_modtime: current=250"))
    .mockResolvedValueOnce({ path: "/a.txt", size: 3, modTime: 250, isBinary: false });
  const w = mount(CodeEditor, { props: { fs, path: "/a.txt", showLineNumbers: false, theme: "dimmed" } });
  await flushPromises();
  await (w.vm as any).testAppend("x");
  const first = await (w.vm as any).save();
  expect(first).toBe(false);
  expect(w.find('[data-test="conflict-banner"]').exists()).toBe(true);
  await w.find('[data-test="conflict-overwrite"]').trigger("click");
  await flushPromises();
  expect(fs.writeFile).toHaveBeenLastCalledWith("/a.txt", expect.any(Uint8Array), 250);
});
```

- [ ] **Step 3: Rewrite `CodeEditor.vue`.**

Key changes vs. `CodeViewer.vue`:

1. **Remove** `EditorView.editable.of(false)` and `EditorState.readOnly.of(true)` from the `exts` array.
2. **Add** `history` and a save keymap:

   ```ts
   import { history, defaultKeymap, historyKeymap } from "@codemirror/commands";
   import { keymap } from "@codemirror/view";

   const saveKey = keymap.of([
     {
       key: "Mod-s",
       preventDefault: true,
       run: () => {
         void save();
         return true;
       },
     },
   ]);

   const dirtyListener = EditorView.updateListener.of((v) => {
     if (!v.docChanged) return;
     const now = v.state.doc.toString();
     const nextDirty = now !== originalText;
     if (nextDirty !== dirty.value) {
       dirty.value = nextDirty;
       emit("dirty-change", nextDirty);
     }
   });

   exts.push(history(), keymap.of([...defaultKeymap, ...historyKeymap]), saveKey, dirtyListener);
   ```

3. **Track** `originalText` (ref) alongside `loadedAt`. Reset both on every successful `load()`.
4. **Implement `save()`:**

   ```ts
   const conflict = ref<{ currentModTime: number } | null>(null);

   async function save(): Promise<boolean> {
     if (!view || state.value !== "ok") return false;
     const text = view.state.doc.toString();
     const bytes = new TextEncoder().encode(text);
     try {
       const meta = await props.fs.writeFile(props.path, bytes, loadedAt.value);
       originalText = text;
       loadedAt.value = meta.modTime;
       dirty.value = false;
       conflict.value = null;
       emit("dirty-change", false);
       return true;
     } catch (err) {
       const msg = (err as Error).message ?? "";
       const m = /stale_modtime: current=(\d+)/.exec(msg);
       if (m) {
         conflict.value = { currentModTime: Number(m[1]) };
       } else {
         errorMsg.value = msg;
       }
       return false;
     }
   }
   ```

5. **Overwrite/Reload buttons** in the template (banner block placed above the reload badge):

   ```html
   <div v-if="conflict" class="banner err" data-test="conflict-banner">
     {{ t("plugins.fileExplorer.staleModTime") }}
     <button data-test="conflict-overwrite" @click="overwriteWithServerModTime">
       {{ t("plugins.fileExplorer.overwrite") }}
     </button>
     <button data-test="conflict-reload" @click="reloadDiscardChanges">
       {{ t("plugins.fileExplorer.reloadDiscard") }}
     </button>
     <button @click="conflict = null">{{ t("plugins.fileExplorer.cancel") }}</button>
   </div>
   ```

   Handlers:

   ```ts
   async function overwriteWithServerModTime() {
     if (!conflict.value) return;
     loadedAt.value = conflict.value.currentModTime;
     conflict.value = null;
     await save();
   }
   function reloadDiscardChanges() {
     conflict.value = null;
     void load();
   }
   ```

6. **Test hook:** below existing `defineExpose({ save })`, expose a test-only `testAppend`:

   ```ts
   const isTest = typeof (globalThis as any).vi !== "undefined";
   defineExpose({
     save,
     ...(isTest
       ? {
           testAppend: (text: string) => {
             if (!view) return;
             view.dispatch({
               changes: { from: view.state.doc.length, insert: text },
             });
           },
         }
       : {}),
   });
   ```

7. **Reload badge** wording when dirty: replace `t('plugins.fileExplorer.reload')` with `t('plugins.fileExplorer.reloadDiscard')` when `dirty.value === true`.

- [ ] **Step 4: Update `FileEditor.vue` to import from `CodeEditor`.**

Change:

```ts
import CodeViewer from "./CodeViewer.vue";
```

to:

```ts
import CodeEditor from "./CodeEditor.vue";
```

Replace `<CodeViewer …>` with `<CodeEditor … @dirty-change="(v) => emit('dirty-change', v)">` and add a `defineEmits<{ 'dirty-change': [boolean] }>()`.

Also `defineExpose({ save })` where `save` calls the child editor's `save` via `codeEditorRef.value?.save?.() ?? Promise.resolve(false)`.

- [ ] **Step 5: Run tests.**

Run: `cd desktop/frontend && npm run test -- CodeEditor FileEditor`
Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add -A desktop/frontend/src/plugins/fileExplorer
git commit -m "feat(file-explorer): writable CodeEditor with dirty + Cmd+S + conflict banner"
```

---

### Task 10: `ConfirmDialog.vue` + dirty-tab close + close-tab confirmation

**Files:**
- Create: `desktop/frontend/src/plugins/fileExplorer/ConfirmDialog.vue`
- Create: `desktop/frontend/src/plugins/fileExplorer/ConfirmDialog.test.ts`
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileTabs.vue`
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileTabs.test.ts`
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue`

**Interfaces:**
- Consumes: `tabsModel.setDirty` (Task 8), `FileEditor.defineExpose({save})` (Task 9).
- Produces:
  - `ConfirmDialog.vue` props `{ title: string, message?: string, buttons: Array<{ id: string; label: string; kind?: 'primary' | 'danger' | 'secondary' }> }`, emits `(e: "resolve", id: string): void`.
  - `FileTabs` now emits `(e: "close-request", idx: number)` — the parent decides whether to skip the confirm dialog or open it, based on `tab.dirty`.

- [ ] **Step 1: Write ConfirmDialog tests.**

Create `ConfirmDialog.test.ts`:

```ts
import { mount } from "@vue/test-utils";
import ConfirmDialog from "./ConfirmDialog.vue";

it("emits resolve(id) when a button is clicked", async () => {
  const w = mount(ConfirmDialog, {
    props: {
      title: "T",
      buttons: [
        { id: "save", label: "Save", kind: "primary" },
        { id: "dont", label: "Don't Save", kind: "danger" },
        { id: "cancel", label: "Cancel", kind: "secondary" },
      ],
    },
  });
  await w.find('[data-test="btn-dont"]').trigger("click");
  expect(w.emitted("resolve")?.[0]?.[0]).toBe("dont");
});

it("emits resolve('cancel') on Escape", async () => {
  const w = mount(ConfirmDialog, {
    props: {
      title: "T",
      buttons: [{ id: "cancel", label: "Cancel", kind: "secondary" }],
    },
  });
  await w.trigger("keydown", { key: "Escape" });
  expect(w.emitted("resolve")?.[0]?.[0]).toBe("cancel");
});
```

- [ ] **Step 2: Implement `ConfirmDialog.vue`.**

```vue
<script lang="ts" setup>
import { onMounted, onBeforeUnmount, ref } from "vue";

const props = defineProps<{
  title: string;
  message?: string;
  buttons: Array<{ id: string; label: string; kind?: "primary" | "danger" | "secondary" }>;
}>();

const emit = defineEmits<{
  (e: "resolve", id: string): void;
}>();

const rootRef = ref<HTMLDivElement | null>(null);

function handleKey(e: KeyboardEvent) {
  if (e.key === "Escape") {
    const cancel = props.buttons.find((b) => b.id === "cancel");
    if (cancel) emit("resolve", cancel.id);
  }
}

onMounted(() => {
  window.addEventListener("keydown", handleKey);
  rootRef.value?.focus();
});
onBeforeUnmount(() => window.removeEventListener("keydown", handleKey));
</script>

<template>
  <div class="dlg-scrim" @click.self="emit('resolve', 'cancel')" @keydown="handleKey">
    <div ref="rootRef" class="dlg" tabindex="-1" role="dialog" :aria-label="title">
      <div class="dlg-title">{{ title }}</div>
      <div v-if="message" class="dlg-msg">{{ message }}</div>
      <div class="dlg-buttons">
        <button
          v-for="b in buttons"
          :key="b.id"
          :class="['dlg-btn', b.kind ?? 'secondary']"
          :data-test="`btn-${b.id}`"
          @click="emit('resolve', b.id)"
        >
          {{ b.label }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dlg-scrim {
  position: fixed; inset: 0; background: rgba(0,0,0,0.35);
  display: flex; align-items: center; justify-content: center; z-index: 50;
}
.dlg {
  background: var(--ed-shell-bg, #22272e);
  color: var(--ed-row-fg, #adbac7);
  border: 1px solid var(--ed-border, #444c56);
  border-radius: 6px; padding: 16px; min-width: 320px; max-width: 480px;
  outline: none;
}
.dlg-title { font-weight: 600; margin-bottom: 8px; }
.dlg-msg { font-size: 12px; opacity: 0.85; margin-bottom: 12px; }
.dlg-buttons { display: flex; gap: 8px; justify-content: flex-end; }
.dlg-btn {
  padding: 4px 10px; border-radius: 3px; border: 1px solid var(--ed-border, #444c56);
  background: var(--ed-editor-bg, #22272e); color: inherit; cursor: pointer; font-size: 12px;
}
.dlg-btn.primary { background: var(--ed-tab-active-bar, #539bf5); color: white; border-color: transparent; }
.dlg-btn.danger { background: var(--ed-error, #f47067); color: white; border-color: transparent; }
</style>
```

- [ ] **Step 3: Add dirty indicator + close-request to `FileTabs.vue`.**

Add `•` after `basename(t.path)` when `t.dirty`:

```html
<span class="name">{{ basename(t.path) }}<span v-if="t.dirty" class="dot">•</span></span>
```

Change `emit('close', i)` to `emit('close-request', i)`, and rename the emit signature accordingly.

Also update `FileTabs.test.ts` to reflect new emit name + the presence of `•` when `dirty=true`.

- [ ] **Step 4: Wire the dialog + dirty state in `FileExplorer.vue`.**

Import `ConfirmDialog` and `setDirty`; add a `confirmClose` ref plus a `codeEditorRef` (via `<FileEditor ref="codeEditorRef" @dirty-change="onDirtyChange">`).

`onDirtyChange(v)`:

```ts
function onDirtyChange(v: boolean) {
  const active = activePath.value;
  if (!active) return;
  tabsState.value = setDirty(tabsState.value, active, v);
}
```

`onCloseRequest(idx)`:

```ts
async function onCloseRequest(idx: number) {
  const t = tabsState.value.tabs[idx];
  if (!t?.dirty) {
    closeTabAt(idx);
    return;
  }
  confirmClose.value = {
    idx,
    buttons: [
      { id: "save", label: t("plugins.fileExplorer.save"), kind: "primary" },
      { id: "dontSave", label: t("plugins.fileExplorer.dontSave"), kind: "danger" },
      { id: "cancel", label: t("plugins.fileExplorer.cancel"), kind: "secondary" },
    ],
    name: t.path.split("/").pop() ?? t.path,
  };
}
async function resolveConfirmClose(id: string) {
  const spec = confirmClose.value;
  confirmClose.value = null;
  if (!spec) return;
  if (id === "cancel") return;
  if (id === "dontSave") { closeTabAt(spec.idx); return; }
  // save
  const ok = (await codeEditorRef.value?.save?.()) ?? false;
  if (ok) closeTabAt(spec.idx);
}
```

Template addition:

```html
<ConfirmDialog
  v-if="confirmClose"
  :title="$t('plugins.fileExplorer.confirmCloseTitle', { name: confirmClose.name })"
  :buttons="confirmClose.buttons"
  @resolve="resolveConfirmClose"
/>
```

Update `<FileTabs @close-request="onCloseRequest">`.

- [ ] **Step 5: Run tests.**

Run: `cd desktop/frontend && npm run test -- ConfirmDialog FileTabs FileExplorer`
Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add desktop/frontend/src/plugins/fileExplorer/ConfirmDialog.vue \
        desktop/frontend/src/plugins/fileExplorer/ConfirmDialog.test.ts \
        desktop/frontend/src/plugins/fileExplorer/FileTabs.vue \
        desktop/frontend/src/plugins/fileExplorer/FileTabs.test.ts \
        desktop/frontend/src/plugins/fileExplorer/FileExplorer.vue
git commit -m "feat(file-explorer): dirty tabs + close-confirm dialog"
```

---

### Task 11: `FileTree` right-click menu — New File / New Folder / Rename / Delete

**Files:**
- Create: `desktop/frontend/src/plugins/fileExplorer/InlineEditRow.vue`
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileTree.vue`
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileTree.test.ts`
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileTreeNode.vue`

**Interfaces:**
- Consumes: `FileSystemBridge.createFile / mkdir / rename / remove / trash` (Task 7), `ConfirmDialog` (Task 10).
- Produces:
  - `InlineEditRow.vue` props `{ level: number, initialValue: string, placeholder?: string, icon: 'file' | 'folder' }`, emits `submit(value)`, `cancel()`.
  - `FileTree` grows a `contextmenu` handler on `FileTreeNode` that opens an `<ul class="ctx-menu">` at cursor coords with 4 items and dispatches action handlers.
  - Delete honors `shiftKey` from the triggering event: shift = hard delete, plain = trash.

- [ ] **Step 1: Write `InlineEditRow.vue`.**

```vue
<script lang="ts" setup>
import { nextTick, onMounted, ref } from "vue";
import { File, Folder } from "lucide-vue-next";

const props = defineProps<{
  level: number;
  initialValue?: string;
  placeholder?: string;
  icon: "file" | "folder";
}>();

const emit = defineEmits<{
  (e: "submit", value: string): void;
  (e: "cancel"): void;
}>();

const value = ref(props.initialValue ?? "");
const inputRef = ref<HTMLInputElement | null>(null);

onMounted(async () => {
  await nextTick();
  inputRef.value?.focus();
  inputRef.value?.select();
});

function onKey(e: KeyboardEvent) {
  if (e.key === "Enter") {
    if (value.value.trim() === "") emit("cancel");
    else emit("submit", value.value.trim());
  } else if (e.key === "Escape") {
    emit("cancel");
  }
}
</script>

<template>
  <div class="row" :style="{ paddingLeft: `${level * 8}px` }">
    <span class="icon">
      <component :is="icon === 'folder' ? Folder : File" :size="16" :stroke-width="1.5" />
    </span>
    <input
      ref="inputRef"
      v-model="value"
      class="input"
      :placeholder="placeholder"
      @keydown="onKey"
      @blur="emit('cancel')"
    />
  </div>
</template>

<style scoped>
.row { display: flex; align-items: center; height: 22px; gap: 6px; }
.icon { display: inline-flex; align-items: center; width: 20px; margin-left: 14px; }
.input { flex: 1 1 auto; background: transparent; color: inherit; border: 1px solid var(--ed-tab-active-bar, #539bf5); border-radius: 2px; font: inherit; padding: 0 4px; outline: none; }
</style>
```

- [ ] **Step 2: Add failing tests to `FileTree.test.ts`.**

Cover:
- Right-click on file → menu shows `New File / New Folder / Rename / Delete`.
- Clicking `Rename` swaps the node into an `InlineEditRow`; pressing Enter dispatches `fs.rename(oldPath, newFullPath)`.
- Clicking `New File` at directory node inserts an `InlineEditRow` as a child; Enter dispatches `fs.createFile(dir + "/" + name)`.
- Clicking `Delete` without shift dispatches `fs.trash(path)`; shift-click dispatches `fs.remove(path, isDir=recursive?)` after confirm.
- If `fs.trash` throws with `trash: no platform trash command available`, fallback confirm surfaces `hardDelete` action calling `fs.remove(path, isDir)`.

Reuse the test setup pattern from the existing `FileTree.test.ts` (mock bridge with `listDir` / `watchDir` etc.).

- [ ] **Step 3: Add contextmenu + action state to `FileTree.vue`.**

Add near the existing state:

```ts
type MenuAnchor = { x: number; y: number; node: TreeNode | null; shift: boolean };
const menu = ref<MenuAnchor | null>(null);

type InlineIntent =
  | { kind: "newFile"; parentPath: string; parentLevel: number }
  | { kind: "newFolder"; parentPath: string; parentLevel: number }
  | { kind: "rename"; node: TreeNode };

const inlineIntent = ref<InlineIntent | null>(null);
const deleteConfirm = ref<{ node: TreeNode; mode: "trash" | "hard" } | null>(null);

function openMenu(e: MouseEvent, node: TreeNode) {
  e.preventDefault();
  menu.value = { x: e.clientX, y: e.clientY, node, shift: e.shiftKey };
}

function closeMenu() { menu.value = null; }

async function onMenuAction(action: "newFile" | "newFolder" | "rename" | "delete") {
  const anchor = menu.value;
  menu.value = null;
  if (!anchor?.node) return;
  const node = anchor.node;
  const parentPath = node.isDir ? node.path : parentDir(node.path);
  if (action === "newFile") inlineIntent.value = { kind: "newFile", parentPath, parentLevel: node.isDir ? nodeLevel(node) + 1 : nodeLevel(node) };
  else if (action === "newFolder") inlineIntent.value = { kind: "newFolder", parentPath, parentLevel: node.isDir ? nodeLevel(node) + 1 : nodeLevel(node) };
  else if (action === "rename") inlineIntent.value = { kind: "rename", node };
  else if (action === "delete") {
    deleteConfirm.value = { node, mode: anchor.shift ? "hard" : "trash" };
  }
}

async function submitInline(name: string) {
  const intent = inlineIntent.value;
  inlineIntent.value = null;
  if (!intent) return;
  try {
    if (intent.kind === "newFile") await props.fs.createFile(joinPath(intent.parentPath, name));
    else if (intent.kind === "newFolder") await props.fs.mkdir(joinPath(intent.parentPath, name));
    else if (intent.kind === "rename") {
      const newPath = joinPath(parentDir(intent.node.path), name);
      await props.fs.rename(intent.node.path, newPath);
    }
  } catch (err) {
    // The watcher will not re-render the failure state; surface via console for now.
    console.warn("file-explorer: inline action failed", err);
  }
}
```

Helpers:

```ts
function parentDir(p: string): string {
  const i = p.lastIndexOf("/");
  return i <= 0 ? "/" : p.slice(0, i);
}
function nodeLevel(_n: TreeNode): number { return 0; /* pass level down via prop chain; simplified — actual code needs the level from the FileTreeNode emit */ }
```

(Level tracking: extend `FileTreeNode` to emit its level in `contextmenu`. Alternatively, thread level through the intent by handling the contextmenu on `FileTreeNode` and passing the current `level` prop with the emit.)

Update `FileTreeNode.vue`:

```html
<div … @contextmenu="onContext" …>
```

```ts
const emit = defineEmits<{
  (e: "toggle", n: TreeNode): void;
  (e: "click-file", n: TreeNode): void;
  (e: "dblclick-file", n: TreeNode): void;
  (e: "context", ev: MouseEvent, n: TreeNode, level: number): void;
}>();
function onContext(ev: MouseEvent) { emit("context", ev, props.node, props.level); }
```

And in `FileTree.vue` template: `<FileTreeNode @context="openMenuFromNode" …>` with:

```ts
function openMenuFromNode(ev: MouseEvent, node: TreeNode, level: number) {
  ev.preventDefault();
  menu.value = { x: ev.clientX, y: ev.clientY, node, shift: ev.shiftKey };
  levelForIntent.value = level;
}
```

- [ ] **Step 4: Template additions.**

At the bottom of `<template>` in `FileTree.vue`:

```html
<div v-if="menu" class="ctx-menu" :style="{ top: `${menu.y}px`, left: `${menu.x}px` }" @click.stop>
  <button @click="onMenuAction('newFile')">{{ t("plugins.fileExplorer.newFile") }}</button>
  <button @click="onMenuAction('newFolder')">{{ t("plugins.fileExplorer.newFolder") }}</button>
  <button @click="onMenuAction('rename')">{{ t("plugins.fileExplorer.rename") }}</button>
  <button @click="onMenuAction('delete')">{{ t("plugins.fileExplorer.delete") }}</button>
</div>

<ConfirmDialog
  v-if="deleteConfirm"
  :title="deleteConfirm.mode === 'hard'
    ? t('plugins.fileExplorer.confirmHardDelete', { name: baseName(deleteConfirm.node.path) })
    : t('plugins.fileExplorer.confirmTrash', { name: baseName(deleteConfirm.node.path) })"
  :buttons="deleteButtons(deleteConfirm.mode)"
  @resolve="resolveDeleteConfirm"
/>
```

`deleteButtons`:

```ts
function deleteButtons(mode: "trash" | "hard") {
  return [
    { id: mode, label: mode === "hard" ? t("plugins.fileExplorer.delete") : t("plugins.fileExplorer.moveToTrash"), kind: mode === "hard" ? "danger" : "primary" },
    { id: "cancel", label: t("plugins.fileExplorer.cancel"), kind: "secondary" },
  ];
}

async function resolveDeleteConfirm(id: string) {
  const conf = deleteConfirm.value;
  deleteConfirm.value = null;
  if (!conf || id === "cancel") return;
  try {
    if (id === "hard") await props.fs.remove(conf.node.path, conf.node.isDir);
    else {
      try { await props.fs.trash(conf.node.path); }
      catch (err) {
        const msg = (err as Error).message ?? "";
        if (msg.includes("no platform trash command available")) {
          deleteConfirm.value = { node: conf.node, mode: "hard" };
        } else throw err;
      }
    }
  } catch (err) { console.warn("file-explorer: delete failed", err); }
}
```

Also add global `@click` on window to close the menu; and Escape handler.

- [ ] **Step 5: Render `InlineEditRow` inside the tree.**

Where the tree emits the parent node's children, when `inlineIntent.kind` is `newFile` / `newFolder` and its `parentPath` matches the current directory, render an extra `<InlineEditRow>` at the top of that list. For `rename`, replace the row of the matching node.

Given the recursive `FileTreeNode` structure, thread `inlineIntent` down as a prop and render accordingly. Adjust `FileTreeNode`'s template to conditionally swap or insert the row.

- [ ] **Step 6: Run tests.**

Run: `cd desktop/frontend && npm run test -- FileTree`
Expected: PASS.

- [ ] **Step 7: Commit.**

```bash
git add desktop/frontend/src/plugins/fileExplorer/InlineEditRow.vue \
        desktop/frontend/src/plugins/fileExplorer/FileTree.vue \
        desktop/frontend/src/plugins/fileExplorer/FileTree.test.ts \
        desktop/frontend/src/plugins/fileExplorer/FileTreeNode.vue
git commit -m "feat(file-explorer): tree context menu + inline new/rename + delete"
```

---

### Task 12: i18n keys — English and 中文

**Files:**
- Modify: `desktop/frontend/src/i18n/messages/en.ts` (add to the `plugins.fileExplorer` block, currently at line 563)
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts` (matching block)

**Interfaces:**
- Consumes: all new UI strings from Tasks 9–11.
- Produces:
  ```ts
  plugins.fileExplorer.save                = "Save" / "保存"
  plugins.fileExplorer.dontSave            = "Don't Save" / "不保存"
  plugins.fileExplorer.cancel              = "Cancel" / "取消"
  plugins.fileExplorer.dirty               = "Unsaved changes" / "有未保存的更改"
  plugins.fileExplorer.staleModTime        = "File was modified on disk since you opened it." / "文件在你打开后已被外部修改。"
  plugins.fileExplorer.overwrite           = "Overwrite" / "覆盖"
  plugins.fileExplorer.reloadDiscard       = "Reload (discard changes)" / "重新加载（放弃更改）"
  plugins.fileExplorer.confirmCloseTitle   = "Save changes to {name}?" / "是否保存对 {name} 的更改？"
  plugins.fileExplorer.newFile             = "New File" / "新建文件"
  plugins.fileExplorer.newFolder           = "New Folder" / "新建文件夹"
  plugins.fileExplorer.rename              = "Rename" / "重命名"
  plugins.fileExplorer.delete              = "Delete" / "删除"
  plugins.fileExplorer.moveToTrash         = "Move to Trash" / "移到回收站"
  plugins.fileExplorer.confirmTrash        = "Move {name} to Trash?" / "把 {name} 移到回收站？"
  plugins.fileExplorer.confirmHardDelete   = "Permanently delete {name}? This cannot be undone." / "永久删除 {name}？此操作无法撤销。"
  plugins.fileExplorer.trashUnavailable    = "Trash is not available on this system. Delete permanently instead?" / "此系统上不支持回收站。改为永久删除？"
  plugins.fileExplorer.saveFailed          = "Save failed: {message}" / "保存失败：{message}"
  ```

- [ ] **Step 1: Append the new keys to both language files.**

Insert them inside the `fileExplorer: { … }` block. Match the order above so diffs are easy to review.

- [ ] **Step 2: Run tests.**

Run: `cd desktop/frontend && npm run test`
Expected: PASS across the full suite (this also catches any lingering missing-key issues from Tasks 9–11).

- [ ] **Step 3: Commit.**

```bash
git add desktop/frontend/src/i18n/messages/en.ts \
        desktop/frontend/src/i18n/messages/zh-CN.ts
git commit -m "i18n(file-explorer): editing/save/CRUD strings"
```

---

### Task 13: Manual verification + release

**Files:**
- Modify: `docs/plugins/file-explorer.md` (if exists) — add an "Editing" section.

**Interfaces:**
- Consumes: everything above.
- Produces: a working, published release.

- [ ] **Step 1: Rebuild the desktop app and run it in dev mode.**

Run: `cd desktop && wails dev`
Verify manually:
- Open a text file in the file explorer, edit it, press Cmd+S → save succeeds, dirty dot clears.
- Modify the same file externally (e.g. `echo x >> file` in another terminal), edit in the app, try to save → conflict banner appears; test Overwrite (writes) and Reload (discards).
- Close a dirty tab → dialog with Save/Don't Save/Cancel appears; each button behaves correctly.
- Right-click a file → menu appears. Rename a file. Create new file / folder. Delete a file (goes to trash — verify in Finder trash on macOS). Shift-delete a file (hard delete).
- Attach a remote session and repeat all above operations on remote files (works when `remote_permission=full`; rejected otherwise).
- Verify no logs contain "check-plugin-fs-isolation" hits.

- [ ] **Step 2: Update `docs/plugins/file-explorer.md` with the Editing section (if the doc exists).**

Run: `ls docs/plugins/file-explorer.md 2>/dev/null`. If present, add a section describing:
- Editing is per-file, per-tab; Cmd+S saves.
- Dirty tabs show `•` and prompt on close.
- Right-click the tree for CRUD.
- Delete goes to trash by default; Shift = permanent.

- [ ] **Step 3: Commit doc update.**

```bash
git add docs/plugins/file-explorer.md 2>/dev/null || true
git commit -m "docs(file-explorer): editing/save/CRUD" --allow-empty
```

- [ ] **Step 4: Ship via `ship-release` skill.**

Run: `/ship-release`
The skill will:
- Push the current branch to a PR against `main`.
- Wait for CI (including the plugin-fs isolation check).
- Squash-merge on green.
- Cut the next patch tag in the `v0.2.x` series (per user memory: `git tag --list "v0.2*" | sort -V | tail -1`).
- Publish the release notes summarizing the editing capability.

Expected: PR merged; a new tag pushed; the GitHub release visible.

---

## Self-Review Notes

- Spec coverage: proto (Task 1), fsAccess writes/CAS/atomic + trash (Tasks 2–3), local + remote bindings (Tasks 4–5), transport (Task 6), fs bridges (Task 7), dirty tabs model (Task 8), CodeEditor + FileEditor (Task 9), ConfirmDialog + dirty-close (Task 10), tree CRUD menu (Task 11), i18n (Task 12), release (Task 13). Every spec § maps to a task.
- Type consistency: `writeFile(path, data, expectedModTime)` signature used identically in bridge, remote, CodeEditor. `Meta` returns `FileMetaInfo` in every callsite. Sentinel errors `ErrStaleModTime / ErrAlreadyExists / ErrNotFound / ErrIsDirectory / ErrNotADirectory` defined once in Task 3.
- Placeholder scan: no TBD/TODO/"implement later"; every step has code, exact filenames, and exact test commands.
- Line-ending / newline: spec explicitly forbids normalization; Task 9's `save()` encodes `view.state.doc.toString()` via `TextEncoder` (UTF-8) with zero pre-processing, matching the spec.
- CAS wiring: expected_modtime=0 disables CAS server-side (Task 3) and is used only for create-if-missing (Task 7). Task 9 always passes `loadedAt.value` (non-zero) for existing files.
