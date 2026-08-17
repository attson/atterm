package sshconfig

import (
	"io"
	"io/fs"
	"strings"
	"testing"
)

// memFS is an in-memory Opener used by tests: keys are the exact paths
// resolved by the parser (base + relative pattern, or an absolute pattern).
type memFS map[string]string

func (m memFS) Open(p string) (io.ReadCloser, error) {
	s, ok := m[p]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return io.NopCloser(strings.NewReader(s)), nil
}

// --- Brief's required tests, verbatim ---

func TestWildcardBlockAppliesButIsNotImported(t *testing.T) {
	cfg := "Host *\n  User deploy\n\nHost web1\n  HostName 10.0.0.1\n"
	entries, _, err := Parse(strings.NewReader(cfg), "/base", memFS{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d: %+v", len(entries), entries)
	}
	if entries[0].Alias != "web1" || entries[0].User != "deploy" {
		t.Fatalf("wildcard User not applied: %+v", entries[0])
	}
}

func TestFirstValueWins(t *testing.T) {
	// ssh_config semantics: the FIRST obtained value for a keyword wins.
	cfg := "Host web1\n  User alice\n\nHost *\n  User deploy\n"
	entries, _, _ := Parse(strings.NewReader(cfg), "/base", memFS{})
	if entries[0].User != "alice" {
		t.Fatalf("later block overrode earlier: %+v", entries[0])
	}
}

func TestHostNameDefaultsToAlias(t *testing.T) {
	entries, _, _ := Parse(strings.NewReader("Host box\n"), "/base", memFS{})
	if entries[0].HostName != "box" {
		t.Fatalf("want HostName=box, got %q", entries[0].HostName)
	}
}

func TestMatchBlockSkippedWithReason(t *testing.T) {
	cfg := "Match host bastion\n  User root\n"
	entries, skipped, _ := Parse(strings.NewReader(cfg), "/base", memFS{})
	if len(entries) != 0 {
		t.Fatalf("Match block must not import: %+v", entries)
	}
	if len(skipped) != 1 || skipped[0].Reason == "" {
		t.Fatalf("want one skipped with a reason, got %+v", skipped)
	}
}

func TestIncludeResolvesRelativeToBase(t *testing.T) {
	fsys := memFS{"/base/extra": "Host inc1\n  HostName 10.9.9.9\n"}
	entries, _, _ := Parse(strings.NewReader("Include extra\n"), "/base", fsys)
	if len(entries) != 1 || entries[0].Alias != "inc1" {
		t.Fatalf("include not resolved: %+v", entries)
	}
}

func TestIncludeCycleIsReportedNotHung(t *testing.T) {
	fsys := memFS{"/base/a": "Include a\n"}
	_, skipped, err := Parse(strings.NewReader("Include a\n"), "/base", fsys)
	if err != nil {
		t.Fatalf("cycle must not error out: %v", err)
	}
	if len(skipped) == 0 {
		t.Fatal("cycle must produce a Skipped entry")
	}
}

func TestProxyDirectivesCaptured(t *testing.T) {
	cfg := "Host db\n  HostName 10.0.0.5\n  ProxyJump bastion\n"
	entries, _, _ := Parse(strings.NewReader(cfg), "/base", memFS{})
	if entries[0].ProxyJump != "bastion" {
		t.Fatalf("ProxyJump not captured: %+v", entries[0])
	}
}

// --- Additional tests beyond the brief ---

func TestProxyCommandCaptured(t *testing.T) {
	cfg := "Host db\n  ProxyCommand ssh -W %h:%p bastion\n"
	entries, _, err := Parse(strings.NewReader(cfg), "/base", memFS{})
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].ProxyCommand != "ssh -W %h:%p bastion" {
		t.Fatalf("ProxyCommand not captured: %+v", entries[0])
	}
}

func TestQuotedValueIsUnquoted(t *testing.T) {
	cfg := "Host box\n  User \"dep loy\"\n"
	entries, _, err := Parse(strings.NewReader(cfg), "/base", memFS{})
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].User != "dep loy" {
		t.Fatalf("want unquoted %q, got %q", "dep loy", entries[0].User)
	}
}

func TestKeyEqualsValueSyntax(t *testing.T) {
	cfg := "Host box\n  HostName=10.1.1.1\n  Port = 2222\n"
	entries, _, err := Parse(strings.NewReader(cfg), "/base", memFS{})
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].HostName != "10.1.1.1" {
		t.Fatalf("Key=Value not parsed: %+v", entries[0])
	}
	if entries[0].Port != "2222" {
		t.Fatalf("Key = Value (spaced) not parsed: %+v", entries[0])
	}
}

func TestMultiPatternHostLineProducesEntryPerPattern(t *testing.T) {
	cfg := "Host web1 web2\n  User ops\n"
	entries, _, err := Parse(strings.NewReader(cfg), "/base", memFS{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(entries), entries)
	}
	byAlias := map[string]Entry{}
	for _, e := range entries {
		byAlias[e.Alias] = e
	}
	for _, alias := range []string{"web1", "web2"} {
		e, ok := byAlias[alias]
		if !ok {
			t.Fatalf("missing entry for %q: %+v", alias, entries)
		}
		if e.User != "ops" || e.HostName != alias {
			t.Fatalf("entry for %q wrong: %+v", alias, e)
		}
	}
}

func TestNegatedPatternExcludesHost(t *testing.T) {
	// "Host * !excluded" applies User to everything except the host literally
	// named "excluded" — even though "*" would otherwise match it too.
	cfg := "Host * !excluded\n  User shared\n\n" +
		"Host excluded\n  HostName 10.0.0.9\n\n" +
		"Host other\n  HostName 10.0.0.8\n"
	entries, _, err := Parse(strings.NewReader(cfg), "/base", memFS{})
	if err != nil {
		t.Fatal(err)
	}
	byAlias := map[string]Entry{}
	for _, e := range entries {
		byAlias[e.Alias] = e
	}
	if byAlias["other"].User != "shared" {
		t.Fatalf("negated pattern wrongly excluded unrelated host: %+v", byAlias["other"])
	}
	if byAlias["excluded"].User != "" {
		t.Fatalf("negated pattern failed to exclude its target: %+v", byAlias["excluded"])
	}
}

func TestInlineCommentAndCaseInsensitiveKeywords(t *testing.T) {
	cfg := "HOST box # a comment\n  UsEr alice # trailing comment\n"
	entries, _, err := Parse(strings.NewReader(cfg), "/base", memFS{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Alias != "box" || entries[0].User != "alice" {
		t.Fatalf("case-insensitive keyword / inline comment handling failed: %+v", entries)
	}
}

func TestIncludeCycleReasonIsCycleSpecific(t *testing.T) {
	// The brief's cycle test only checks that *some* Skipped is produced,
	// which the depth cap alone would also satisfy after 16 levels of
	// (undetected) re-inclusion. This test pins down that the cycle is
	// actually caught by the visited-path set, quickly, and reported as a
	// cycle — not silently absorbed by the depth cap as a fallback.
	fsys := memFS{"/base/a": "Include a\n"}
	_, skipped, err := Parse(strings.NewReader("Include a\n"), "/base", fsys)
	if err != nil {
		t.Fatalf("cycle must not error out: %v", err)
	}
	if len(skipped) != 1 {
		t.Fatalf("want exactly 1 skipped (caught on 2nd visit), got %d: %+v", len(skipped), skipped)
	}
	if !strings.Contains(skipped[0].Reason, "循环") {
		t.Fatalf("want a cycle-specific reason, got %q", skipped[0].Reason)
	}
}

func TestIncludeDepthCapProducesSkippedNotHang(t *testing.T) {
	// A long (non-cyclic) Include chain must still terminate: depth is capped
	// at 16, past which the parser must emit a Skipped rather than recurse
	// forever or blow the stack.
	fsys := memFS{}
	const chain = 20
	for i := 0; i < chain; i++ {
		next := "lvl" + itoa(i+1)
		fsys["/base/lvl"+itoa(i)] = "Include " + next + "\n"
	}
	fsys["/base/lvl"+itoa(chain)] = "Host deep\n  HostName 10.0.0.1\n"

	entries, skipped, err := Parse(strings.NewReader("Include lvl0\n"), "/base", fsys)
	if err != nil {
		t.Fatalf("depth cap must not error out: %v", err)
	}
	if len(skipped) == 0 {
		t.Fatal("depth cap must produce a Skipped entry")
	}
	for _, e := range entries {
		if e.Alias == "deep" {
			t.Fatalf("host past the include depth cap should not have been reached: %+v", entries)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
