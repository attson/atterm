// cmd/atterm-hook/main_test.go
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// buildHook compiles the CLI into a temp binary once per test run.
func buildHook(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "atterm-hook")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func runHook(t *testing.T, bin string, env []string, stdin string) (stdout, stderr string, exit int) {
	t.Helper()
	cmd := exec.Command(bin)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err == nil {
		return outBuf.String(), errBuf.String(), 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return outBuf.String(), errBuf.String(), ee.ExitCode()
	}
	t.Fatalf("run: %v", err)
	return "", "", -1
}

func TestHook_HappyPath(t *testing.T) {
	bin := buildHook(t)
	var received atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received.Store(string(body))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	env := append(os.Environ(),
		"ATTERM_SESSION_ID=00000000-0000-0000-0000-000000000001",
		"ATTERM_HOOK_ENDPOINT="+srv.URL,
	)
	// The CLI is schema-agnostic — it just relays stdin verbatim. We use
	// a realistic claude-code Notification payload here so the fixture
	// matches what production sees, but any well-formed JSON would do.
	stdin := `{"hook_event_name":"Notification","notification_type":"permission_prompt","message":"hi"}`
	_, stderr, exit := runHook(t, bin, env, stdin)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	got, _ := received.Load().(string)
	if got == "" {
		t.Fatalf("server received nothing")
	}
	var req map[string]any
	if err := json.Unmarshal([]byte(got), &req); err != nil {
		t.Fatalf("body not JSON: %v\n%s", err, got)
	}
	if req["session_id"] != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("session_id: %v", req["session_id"])
	}
	if req["agent_kind"] != "claude-code" {
		t.Fatalf("agent_kind: %v", req["agent_kind"])
	}
}

func TestHook_MissingSessionID_Silent(t *testing.T) {
	bin := buildHook(t)
	env := []string{"ATTERM_HOOK_ENDPOINT=http://127.0.0.1:1"}
	_, stderr, exit := runHook(t, bin, env, `{}`)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
}

func TestHook_MissingEndpoint_Silent(t *testing.T) {
	bin := buildHook(t)
	env := []string{"ATTERM_SESSION_ID=00000000-0000-0000-0000-000000000001"}
	_, stderr, exit := runHook(t, bin, env, `{}`)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
}

func TestHook_EndpointFromFile(t *testing.T) {
	bin := buildHook(t)
	var hit atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	cfgDir := filepath.Join(t.TempDir(), "atterm")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "hook-endpoint"), []byte(srv.URL), 0o644); err != nil {
		t.Fatal(err)
	}
	env := []string{
		"HOME=" + filepath.Dir(cfgDir),
		"XDG_CONFIG_HOME=" + filepath.Dir(cfgDir),
		"ATTERM_SESSION_ID=sid",
	}
	_, stderr, exit := runHook(t, bin, env, `{}`)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	if hit.Load() != 1 {
		t.Fatalf("endpoint file not read; expected 1 hit, got %d", hit.Load())
	}
}

func TestHook_PostFailure_Silent(t *testing.T) {
	bin := buildHook(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()
	env := []string{
		"ATTERM_SESSION_ID=sid",
		"ATTERM_HOOK_ENDPOINT=" + srv.URL,
	}
	_, _, exit := runHook(t, bin, env, `{}`)
	if exit != 0 {
		t.Fatalf("expected exit 0 on POST failure, got %d", exit)
	}
}

func TestHook_StdinTooLarge_Drops(t *testing.T) {
	bin := buildHook(t)
	var hit atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	env := []string{
		"ATTERM_SESSION_ID=sid",
		"ATTERM_HOOK_ENDPOINT=" + srv.URL,
	}
	huge := strings.Repeat("x", 65*1024)
	_, stderr, exit := runHook(t, bin, env, huge)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if hit.Load() != 0 {
		t.Fatalf("expected POST suppressed for oversize stdin")
	}
	if !strings.Contains(stderr, "stdin too large") {
		t.Fatalf("expected stderr warning, got %q", stderr)
	}
}

func TestVersionFlag(t *testing.T) {
	// Build the CLI into a temp dir.
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "atterm-hook")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("--version: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if !strings.HasPrefix(got, "atterm-hook ") {
		t.Errorf("unexpected version line %q", got)
	}
}

func TestAgentKindFromArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"space form", []string{"--agent", "codex"}, "codex"},
		{"equals form", []string{"--agent=codex"}, "codex"},
		{"claude explicit", []string{"--agent", "claude-code"}, "claude-code"},
		{"no flag falls back", nil, "claude-code"},
		{"missing value falls back", []string{"--agent"}, "claude-code"},
		{"empty value falls back", []string{"--agent="}, "claude-code"},
		{"ignores other args", []string{"--verbose", "--agent", "codex"}, "codex"},
	}
	for _, c := range cases {
		if got := agentKindFromArgs(c.args); got != c.want {
			t.Fatalf("%s: agentKindFromArgs(%v) = %q, want %q", c.name, c.args, got, c.want)
		}
	}
}
