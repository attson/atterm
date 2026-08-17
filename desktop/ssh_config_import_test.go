package main

import (
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
