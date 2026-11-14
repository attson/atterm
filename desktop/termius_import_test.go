package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/safekeyring"
	"github.com/attson/atterm/internal/termius"
)

func TestTermiusPreviewNeverSerializesSecrets(t *testing.T) {
	a := newHostsTestApp(t)
	preview := a.stageTermiusImport(&termius.Data{
		Hosts:    []termius.Host{{ID: "host", Name: "server", Address: "localhost", Port: "22", Username: "root", Password: "password-canary", KeyID: "key"}},
		Keys:     []termius.Key{{ID: "key", Name: "key", PrivateKey: "private-canary", Passphrase: "passphrase-canary"}},
		Snippets: []termius.Snippet{{ID: "id:deploy", Label: "Deploy", Script: "script-canary"}},
	})
	if len(preview.Snippets) != 1 || preview.Snippets[0].ID != "id:deploy" || preview.Snippets[0].Name != "Deploy" {
		t.Fatalf("snippet metadata missing from preview: %+v", preview.Snippets)
	}
	b, e := json.Marshal(preview)
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{"password-canary", "private-canary", "passphrase-canary", "script-canary"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("secret escaped into preview")
		}
	}
	a.CancelTermiusImport(preview.Token)
	if a.termiusImport.Load() != nil {
		t.Fatal("cancel retained credentials")
	}
}

func TestTermiusConcurrentConfirmConsumesPreviewOnce(t *testing.T) {
	a := newHostsTestApp(t)
	p := a.stageTermiusImport(&termius.Data{Hosts: []termius.Host{{ID: "host", Name: "server", Address: "localhost", Port: "22", Username: "root", Password: "pw"}}})
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, e := a.ImportTermius(p.Token, []string{"host"}, nil, nil)
			results <- e
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for e := range results {
		if e == nil {
			successes++
		} else if !strings.Contains(e.Error(), "preview_expired") {
			t.Fatal(e)
		}
	}
	if successes != 1 || len(a.ListSSHHosts()) != 1 {
		t.Fatalf("confirm did not import exactly once: %d", successes)
	}
	cred, e := sshCredentialSlot(a.ListSSHHosts()[0].ID).Load()
	if e != nil || cred.Password != "pw" {
		t.Fatal("credential not imported")
	}
}

func TestTermiusInvalidSelectionDoesNotConsumePreview(t *testing.T) {
	a := newHostsTestApp(t)
	p := a.stageTermiusImport(&termius.Data{Hosts: []termius.Host{{ID: "host", Name: "server", Address: "localhost", Port: "22", Username: "root", Password: "pw"}}})
	if _, e := a.ImportTermius(p.Token, []string{"unknown"}, nil, nil); e == nil {
		t.Fatal("unknown source ID accepted")
	}
	if _, e := a.ImportTermius(p.Token, []string{"host"}, nil, nil); e != nil {
		t.Fatal(e)
	}
	p = a.stageTermiusImport(&termius.Data{Hosts: []termius.Host{{ID: "host", Name: "server", Address: "localhost", Port: "22", Username: "root", Password: "different"}}})
	report, e := a.ImportTermius(p.Token, []string{"host"}, nil, nil)
	if e != nil || report.Skipped != 1 || report.Hosts != 0 {
		t.Fatalf("re-import must preserve existing hosts: %+v %v", report, e)
	}
	cred, _ := sshCredentialSlot(a.ListSSHHosts()[0].ID).Load()
	if cred.Password != "pw" {
		t.Fatal("re-import overwrote existing credentials")
	}
}

func TestTermiusImportsSnippetOnlyAndPreservesItOnReimport(t *testing.T) {
	a := newHostsTestApp(t)
	source := termius.Snippet{ID: "id:deploy", Label: "Deploy", Script: "make deploy\nmake verify"}
	preview := a.stageTermiusImport(&termius.Data{Snippets: []termius.Snippet{source}})

	report, err := a.ImportTermius(preview.Token, nil, nil, []string{source.ID})
	if err != nil {
		t.Fatal(err)
	}
	if report.Snippets != 1 || report.Hosts != 0 || report.Keys != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	got := a.GetSnippets()
	if len(got) != 1 || got[0].ID == "" || got[0].Label != source.Label || got[0].Text != source.Script {
		t.Fatalf("snippet was not imported: %+v", got)
	}

	got[0].Label = "Custom label"
	got[0].Text = "custom command"
	if err := a.SetSnippets(got); err != nil {
		t.Fatal(err)
	}
	reimport := a.stageTermiusImport(&termius.Data{Snippets: []termius.Snippet{source}})
	if len(reimport.Snippets) != 1 || !reimport.Snippets[0].Existing || !reimport.Snippets[0].Blocked {
		t.Fatalf("re-import was not recognized: %+v", reimport.Snippets)
	}
	if current := a.GetSnippets()[0]; current.Label != "Custom label" || current.Text != "custom command" {
		t.Fatalf("preview overwrote edited snippet: %+v", current)
	}
}

func TestTermiusImportsUntitledSnippetWithoutPreviewingItsScript(t *testing.T) {
	a := newHostsTestApp(t)
	source := termius.Snippet{ID: "id:untitled", Script: "\n  echo secret-preview-canary --all\nnext command"}
	preview := a.stageTermiusImport(&termius.Data{Snippets: []termius.Snippet{source}})
	if len(preview.Snippets) != 1 || preview.Snippets[0].Name != "" || preview.Snippets[0].Blocked {
		t.Fatalf("untitled snippet should remain selectable without exposing a generated label: %+v", preview.Snippets)
	}
	encoded, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret-preview-canary") {
		t.Fatal("untitled snippet script escaped into preview")
	}
	if _, err := a.ImportTermius(preview.Token, nil, nil, []string{source.ID}); err != nil {
		t.Fatal(err)
	}
	got := a.GetSnippets()
	if len(got) != 1 || got[0].Label != "echo secret-preview-canary --all" || got[0].Text != source.Script {
		t.Fatalf("untitled snippet fallback label or script is wrong: %+v", got)
	}
}

func TestTermiusSnippetValidationKeepsPreviewRetryable(t *testing.T) {
	a := newHostsTestApp(t)
	existing := make([]Snippet, snippetMaxItems)
	for i := range existing {
		existing[i] = Snippet{ID: fmt.Sprintf("saved-%d", i), Label: fmt.Sprintf("Saved %d", i), Text: "echo saved"}
	}
	if err := a.SetSnippets(existing); err != nil {
		t.Fatal(err)
	}
	source := termius.Snippet{ID: "id:extra", Label: "Extra", Script: "echo extra"}
	preview := a.stageTermiusImport(&termius.Data{Snippets: []termius.Snippet{source}})
	if _, err := a.ImportTermius(preview.Token, nil, nil, []string{source.ID}); err == nil {
		t.Fatal("collection limit was not enforced")
	}
	if len(a.GetSnippets()) != snippetMaxItems {
		t.Fatal("failed import changed snippets")
	}
	if state := a.termiusImport.Load(); state == nil || state.token != preview.Token {
		t.Fatal("validation failure consumed preview")
	}
}

func TestTermiusKeyReuseAndForwardImport(t *testing.T) {
	a := newHostsTestApp(t)
	private := testKeyPEM(t)
	saved, e := a.AddSSHKey("already saved", private, "")
	if e != nil {
		t.Fatal(e)
	}
	p := a.stageTermiusImport(&termius.Data{Hosts: []termius.Host{{ID: "host", Name: "server", Address: "localhost", Port: "22", Username: "root", KeyID: "key", Forwards: []termius.Forward{{Name: "web tunnel", Kind: "local", BindAddr: "127.0.0.1", BindPort: "8080", TargetHost: "localhost", TargetPort: "80"}}}}, Keys: []termius.Key{{ID: "key", Name: "source key", PrivateKey: private}}})
	report, e := a.ImportTermius(p.Token, []string{"host"}, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	if report.Hosts != 1 || report.Keys != 0 || len(a.ListSSHKeys()) != 1 {
		t.Fatalf("identical key not reused: %+v", report)
	}
	h := a.ListSSHHosts()[0]
	if h.KeyID != saved.ID || len(h.Forwards) != 1 || h.Forwards[0].BindPort != "8080" || h.Forwards[0].Note != "web tunnel" {
		t.Fatal("key reference or tunnel lost")
	}
	if len(a.ListActiveForwards()) != 0 {
		t.Fatal("import started a tunnel")
	}
}

func TestTermiusConfigFailureRollsBackCredentialsAndAllowsRetry(t *testing.T) {
	a := newHostsTestApp(t)
	vault := t.TempDir()
	safekeyring.SetFileDirForTest(vault)
	snippet := termius.Snippet{ID: "id:rollback", Label: "Rollback", Script: "echo rollback"}
	p := a.stageTermiusImport(&termius.Data{Hosts: []termius.Host{{ID: "host", Name: "server", Address: "localhost", Port: "22", Username: "root", Password: "rollback-canary", KeyID: "key"}}, Keys: []termius.Key{{ID: "key", Name: "source key", PrivateKey: testKeyPEM(t)}}, Snippets: []termius.Snippet{snippet}})
	target := configPath()
	if e := os.MkdirAll(target, 0700); e != nil {
		t.Fatal(e)
	}
	if _, e := a.ImportTermius(p.Token, []string{"host"}, nil, []string{snippet.ID}); e == nil {
		t.Fatal("configuration write unexpectedly succeeded")
	}
	if len(a.ListSSHHosts()) != 0 || len(a.ListSSHKeys()) != 0 || len(a.GetSnippets()) != 0 {
		t.Fatal("failed configuration remained visible")
	}
	b, e := os.ReadFile(filepath.Join(vault, "keyring-fallback.json"))
	if e != nil {
		t.Fatal(e)
	}
	var entries map[string]string
	if json.Unmarshal(b, &entries) != nil || len(entries) != 0 {
		t.Fatal("failed import left credentials behind")
	}
	if e := os.Remove(target); e != nil {
		t.Fatal(e)
	}
	if _, e := a.ImportTermius(p.Token, []string{"host"}, nil, []string{snippet.ID}); e != nil {
		t.Fatal("snapshot was not retryable", e)
	}
	if len(a.GetSnippets()) != 1 {
		t.Fatal("successful retry did not persist snippet")
	}
}

func TestTermiusKeychainFailureIsRetryable(t *testing.T) {
	a := newHostsTestApp(t)
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if e := os.WriteFile(blocked, []byte("blocked"), 0600); e != nil {
		t.Fatal(e)
	}
	safekeyring.SetFileDirForTest(blocked)
	p := a.stageTermiusImport(&termius.Data{Hosts: []termius.Host{{ID: "host", Name: "server", Address: "localhost", Port: "22", Username: "root", Password: "pw"}}})
	if _, e := a.ImportTermius(p.Token, []string{"host"}, nil, nil); e == nil || e.Error() != "termius_keychain_failed" {
		t.Fatal("keychain failure was hidden", e)
	}
	if len(a.ListSSHHosts()) != 0 {
		t.Fatal("host saved without its credential")
	}
	safekeyring.SetFileDirForTest(t.TempDir())
	if _, e := a.ImportTermius(p.Token, []string{"host"}, nil, nil); e != nil {
		t.Fatal("cannot retry after keychain recovery", e)
	}
}

func TestTermiusStaleCancellationDoesNotClearNewPreview(t *testing.T) {
	a := newHostsTestApp(t)
	data := &termius.Data{Hosts: []termius.Host{{ID: "host", Name: "server", Address: "localhost", Port: "22", Username: "root"}}}
	old := a.stageTermiusImport(data)
	current := a.stageTermiusImport(data)
	a.CancelTermiusImport(old.Token)
	if a.termiusImport.Load() == nil || a.termiusImport.Load().token != current.Token {
		t.Fatal("stale cancellation cleared a new preview")
	}
	state := a.termiusImport.Load()
	state.expires = time.Now().Add(-time.Minute)
	if _, e := a.ImportTermius(current.Token, []string{"host"}, nil, nil); e == nil {
		t.Fatal("expired preview accepted")
	}
	a.CancelTermiusImport(current.Token)
}

func (a *App) stageTermiusImport(data *termius.Data) TermiusImportPreview {
	preview, err := a.finishTermiusImport(a.beginTermiusImport(), data)
	if err != nil {
		panic(err)
	}
	return preview
}

func TestTermiusSlowOldScanCannotReplaceNewPreview(t *testing.T) {
	a := newHostsTestApp(t)
	data := &termius.Data{Hosts: []termius.Host{{ID: "host", Name: "server", Address: "localhost", Port: "22", Username: "root"}}}
	old := a.beginTermiusImport()
	newer := a.beginTermiusImport()
	current, err := a.finishTermiusImport(newer, data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.finishTermiusImport(old, data); err == nil {
		t.Fatal("slow stale scan replaced newer snapshot")
	}
	if state := a.termiusImport.Load(); state == nil || state.token != current.Token {
		t.Fatal("newer preview was lost")
	}
	a.CancelTermiusImport(current.Token)
}

func TestTermiusReimportRestoresOnlyMissingGroupPath(t *testing.T) {
	a := newHostsTestApp(t)
	source := termius.Host{ID: "h", Name: "server", Address: "localhost", Port: "22", Username: "root", Password: "original"}
	p := a.stageTermiusImport(&termius.Data{Hosts: []termius.Host{source}})
	if _, err := a.ImportTermius(p.Token, []string{"h"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	saved := a.ListSSHHosts()[0]
	source.GroupPath = []string{"公司", "Jumps"}
	source.Password = "must-not-replace"
	p = a.stageTermiusImport(&termius.Data{Hosts: []termius.Host{source}})
	if p.Hosts[0].Existing {
		t.Fatal("missing hierarchy must be selectable for repair")
	}
	if _, err := a.ImportTermius(p.Token, []string{"h"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	hosts := a.ListSSHHosts()
	if len(hosts) != 1 || hosts[0].ID != saved.ID || len(hosts[0].GroupPath) != 2 || hosts[0].GroupPath[1] != "Jumps" {
		t.Fatalf("group repair failed: %+v", hosts)
	}
	cred, err := sshCredentialSlot(saved.ID).Load()
	if err != nil || cred.Password != "original" {
		t.Fatal("repair changed credential", err)
	}
	edited := hosts[0]
	edited.GroupPath = nil
	edited.Note = "edited"
	if err := a.UpdateSSHHost(edited, nil); err != nil {
		t.Fatal(err)
	}
	if len(a.ListSSHHosts()[0].GroupPath) != 2 {
		t.Fatal("editing lost group path")
	}
	source.GroupPath = []string{"elsewhere"}
	p = a.stageTermiusImport(&termius.Data{Hosts: []termius.Host{source}})
	if _, err := a.ImportTermius(p.Token, []string{"h"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(a.ListSSHHosts()) != 1 || a.ListSSHHosts()[0].GroupPath[0] != "公司" {
		t.Fatal("reimport overwrote existing group path")
	}
}

func TestTermiusRepairTruncatedLeafGroup(t *testing.T) {
	a := newHostsTestApp(t)
	source := termius.Host{ID: "h", Name: "server", Address: "localhost", Port: "22", Username: "root", GroupPath: []string{"Jumps"}}
	p := a.stageTermiusImport(&termius.Data{Hosts: []termius.Host{source}})
	if _, err := a.ImportTermius(p.Token, []string{"h"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	original := a.ListSSHHosts()[0]
	source.GroupPath = []string{"玩心不止", "Jumps"}
	p = a.stageTermiusImport(&termius.Data{Hosts: []termius.Host{source}})
	if p.Hosts[0].Existing {
		t.Fatal("truncated group cannot be selected for repair")
	}
	if _, err := a.ImportTermius(p.Token, []string{"h"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	hosts := a.ListSSHHosts()
	if len(hosts) != 1 || hosts[0].ID != original.ID || len(hosts[0].GroupPath) != 2 || hosts[0].GroupPath[0] != "玩心不止" {
		t.Fatalf("repair failed: %+v", hosts)
	}
	p = a.stageTermiusImport(&termius.Data{Hosts: []termius.Host{source}})
	if !p.Hosts[0].Existing {
		t.Fatal("repaired host should be skipped")
	}
}

func TestTermiusReimportRestoresForwardNames(t *testing.T) {
	a := newHostsTestApp(t)
	rule := termius.Forward{Kind: "local", BindAddr: "127.0.0.1", BindPort: "9112", TargetHost: "db.internal", TargetPort: "3306"}
	source := termius.Host{ID: "h", Name: "server", Address: "localhost", Port: "22", Username: "root", Forwards: []termius.Forward{rule}}
	p := a.stageTermiusImport(&termius.Data{Hosts: []termius.Host{source}})
	if _, err := a.ImportTermius(p.Token, []string{"h"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	saved := a.ListSSHHosts()[0]
	source.Forwards[0].Name = "写库 [广告2库] 9112"
	p = a.stageTermiusImport(&termius.Data{Hosts: []termius.Host{source}})
	if p.Hosts[0].Existing {
		t.Fatal("missing name not eligible for repair")
	}
	if _, err := a.ImportTermius(p.Token, []string{"h"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	hosts := a.ListSSHHosts()
	if len(hosts) != 1 || hosts[0].ID != saved.ID || len(hosts[0].Forwards) != 1 || hosts[0].Forwards[0].ID != saved.Forwards[0].ID || hosts[0].Forwards[0].Note != source.Forwards[0].Name {
		t.Fatal("name repair changed identity or lost name")
	}
	source.Forwards[0].Name = "do not overwrite"
	p = a.stageTermiusImport(&termius.Data{Hosts: []termius.Host{source}})
	if !p.Hosts[0].Existing {
		t.Fatal("existing name should be preserved")
	}
	if len(a.ListActiveForwards()) != 0 {
		t.Fatal("repair started tunnel")
	}
}

func TestTermiusForwardNameRepairRequiresUniqueMatch(t *testing.T) {
	src := termius.Forward{Name: "name", Kind: "local", BindAddr: "127.0.0.1", BindPort: "9112", TargetHost: "db", TargetPort: "3306"}
	saved := ForwardRule{ID: "f", Kind: src.Kind, BindAddr: src.BindAddr, BindPort: src.BindPort, TargetHost: src.TargetHost, TargetPort: src.TargetPort}
	if _, changed := termiusForwardNames([]ForwardRule{saved}, []termius.Forward{src, src}); changed {
		t.Fatal("ambiguous source matched")
	}
	if _, changed := termiusForwardNames([]ForwardRule{saved, saved}, []termius.Forward{src}); changed {
		t.Fatal("ambiguous destination matched")
	}
	edited := saved
	edited.Note = "custom"
	if _, changed := termiusForwardNames([]ForwardRule{edited}, []termius.Forward{src}); changed {
		t.Fatal("custom name overwritten")
	}
	changedRoute := src
	changedRoute.TargetHost = "elsewhere"
	if _, changed := termiusForwardNames([]ForwardRule{saved}, []termius.Forward{changedRoute}); changed {
		t.Fatal("different route matched")
	}
	result, changed := termiusForwardNames([]ForwardRule{saved}, []termius.Forward{src})
	if !changed || result[0].Note != "name" || saved.Note != "" {
		t.Fatal("name lost or input mutated")
	}
}
