package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/sshconfig"
)

func TestImportPreservesUserEditsOnRename(t *testing.T) {
	existing := SSHHost{
		ID: "keep-me", Alias: "web1", Host: "old.example",
		User: "old", KeyID: "key-1",
		Tags: []string{"prod"}, Note: "my note",
	}
	incoming := SSHHost{Alias: "web1", Host: "new.example", User: "new"}

	merged := mergeImportedHost(existing, incoming)

	if merged.ID != "keep-me" {
		t.Fatalf("ID must be stable, got %q", merged.ID)
	}
	if merged.Host != "new.example" || merged.User != "new" {
		t.Fatalf("config fields must update: %+v", merged)
	}
	if merged.KeyID != "key-1" {
		t.Fatalf("KeyID must survive re-import, got %q", merged.KeyID)
	}
	if len(merged.Tags) != 1 || merged.Tags[0] != "prod" {
		t.Fatalf("Tags must survive re-import: %+v", merged.Tags)
	}
	if merged.Note != "my note" {
		t.Fatalf("Note must survive re-import, got %q", merged.Note)
	}
}

// TestImportDoesNotWipeTagsWhenIncomingHasNone covers the nil-vs-empty edge:
// an incoming entry never carries Tags/Note at all (ssh_config has no such
// concept), so a merge must not confuse "incoming omitted this" with "user
// wants it cleared".
func TestImportDoesNotWipeTagsWhenIncomingHasNone(t *testing.T) {
	existing := SSHHost{ID: "keep-me", Alias: "web1", Tags: []string{"prod", "db"}, Note: "important"}
	incoming := SSHHost{Alias: "web1", Host: "new.example"}

	merged := mergeImportedHost(existing, incoming)

	if len(merged.Tags) != 2 {
		t.Fatalf("Tags must survive when incoming has none: %+v", merged.Tags)
	}
	if merged.Note != "important" {
		t.Fatalf("Note must survive when incoming has none, got %q", merged.Note)
	}
}

// TestMergeImportedHostPreservesAuthKindWithKeyID covers the traced
// regression: AuthKind and KeyID are a coupled pair (KeyID only means
// anything when AuthKind=="key"), so re-import must preserve them together,
// not just KeyID. Scenario: an existing host connects via a key attached
// through the UI; the user later drops the IdentityFile line from
// ~/.ssh/config (switches to an agent, tidies the file, whatever) and
// re-imports. If AuthKind flipped to "password" while KeyID stayed behind,
// NewSshSessionByID would take the password branch, find no stored
// credential, and a host that used to connect would start failing
// errCredentialMissing.
func TestMergeImportedHostPreservesAuthKindWithKeyID(t *testing.T) {
	existing := SSHHost{ID: "keep-me", Alias: "web1", AuthKind: "key", KeyID: "key-1"}
	incoming := SSHHost{Alias: "web1", Host: "new.example", AuthKind: "password"} // IdentityFile line is gone now

	merged := mergeImportedHost(existing, incoming)

	if merged.AuthKind != "key" {
		t.Fatalf("AuthKind must survive re-import alongside KeyID, got %q", merged.AuthKind)
	}
	if merged.KeyID != "key-1" {
		t.Fatalf("KeyID must survive re-import, got %q", merged.KeyID)
	}
	if merged.Host != "new.example" {
		t.Fatalf("config-derived fields must still update: %+v", merged)
	}
}

func TestIdentityFileSetsKeyAuthWithoutKeyID(t *testing.T) {
	e := sshconfig.Entry{Alias: "box", HostName: "10.0.0.1", IdentityFile: "~/.ssh/id_ed25519"}
	h := hostFromEntry(e)
	if h.AuthKind != "key" {
		t.Fatalf("want auth_kind=key, got %q", h.AuthKind)
	}
	if h.KeyID != "" {
		t.Fatalf("import must not invent a KeyID, got %q", h.KeyID)
	}
	if h.IdentityFile != "~/.ssh/id_ed25519" {
		t.Fatalf("IdentityFile path must be recorded verbatim, got %q", h.IdentityFile)
	}
}

// --- App-level bindings: PreviewSSHConfigImport / ImportSSHHosts ---
//
// os.UserHomeDir() respects $HOME on POSIX, and newTestConfigStore already
// points $HOME at an isolated per-test temp dir — so writing (or not
// writing) a ~/.ssh/config under that same temp dir is enough to drive
// PreviewSSHConfigImport without any injection seam.

func TestPreviewSSHConfigImportMissingFileIsReadableError(t *testing.T) {
	cs := newTestConfigStore(t) // isolated $HOME with no ~/.ssh at all
	a := &App{cfgStore: cs}

	preview, err := a.PreviewSSHConfigImport()
	if err == nil {
		t.Fatalf("expected an error for a missing ~/.ssh/config, got preview %+v", preview)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error must read as 'not found', got %v", err)
	}
}

func TestPreviewSSHConfigImportUnreadableFileIsReadableError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits don't block owner reads the same way on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	cs := newTestConfigStore(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(sshDir, "config")
	if err := os.WriteFile(cfgPath, []byte("Host x\n"), 0o000); err != nil {
		t.Fatal(err)
	}

	a := &App{cfgStore: cs}
	preview, err := a.PreviewSSHConfigImport()
	if err == nil {
		t.Fatalf("expected an error for an unreadable ~/.ssh/config, got preview %+v", preview)
	}
	if !strings.Contains(err.Error(), "could not read") {
		t.Fatalf("error must read as 'cannot read', got %v", err)
	}
}

// TestPreviewAndImportSSHConfigRoundTrip is the deliverable-level test the
// pure-helper tests above don't cover: it exercises both bound methods
// end-to-end against a real ~/.ssh/config, and — critically — asserts
// markSSHHostsDirty ran. A missing dirty-mark wouldn't fail any assertion
// about the stored hosts themselves; it would just mean imported hosts
// never sync to other devices, silently.
func TestPreviewAndImportSSHConfigRoundTrip(t *testing.T) {
	cs := newTestConfigStore(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := "Host web1\n  HostName 10.0.0.1\n  User deploy\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	a := &App{cfgStore: cs}

	preview, err := a.PreviewSSHConfigImport()
	if err != nil {
		t.Fatalf("PreviewSSHConfigImport: %v", err)
	}
	if len(preview.Entries) != 1 || preview.Entries[0].Alias != "web1" {
		t.Fatalf("unexpected preview entries: %+v", preview.Entries)
	}
	if preview.Note == "" {
		t.Fatal("Note must be populated so the UI can footnote parser coverage")
	}

	n, err := a.ImportSSHHosts(preview.Entries)
	if err != nil {
		t.Fatalf("ImportSSHHosts: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 host imported, got %d", n)
	}

	hosts := a.ListSSHHosts()
	if len(hosts) != 1 {
		t.Fatalf("want 1 stored host, got %d: %+v", len(hosts), hosts)
	}
	got := hosts[0]
	if got.Alias != "web1" || got.Host != "10.0.0.1" || got.User != "deploy" {
		t.Fatalf("unexpected stored host: %+v", got)
	}
	if got.ID == "" {
		t.Fatal("imported host must get an ID")
	}

	cfg := cs.Get()
	meta, ok := cfg.PrefsMeta["ssh_hosts_encrypted"]
	if !ok || !meta.Dirty {
		t.Fatalf("ImportSSHHosts must mark ssh_hosts_encrypted dirty so it syncs, got %+v", meta)
	}
}
