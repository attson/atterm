// desktop/feishu/hook_server_test.go
package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

type recordingDispatcher struct {
	mu      sync.Mutex
	waiting []WaitingInputDispatchEvent
	turns   []TurnEvent
}

func (r *recordingDispatcher) DispatchWaitingInput(ctx context.Context, ev WaitingInputDispatchEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.waiting = append(r.waiting, ev)
}

func (r *recordingDispatcher) DispatchTurn(sessionID string, ev TurnEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turns = append(r.turns, ev)
}

type sessionsFake struct {
	known map[string]bool
}

func (f *sessionsFake) Exists(sid uuid.UUID) bool { return f.known[sid.String()] }

func (f *sessionsFake) Inject(uuid.UUID, string) error { return nil }

func TestHookServer_HappyPath(t *testing.T) {
	disp := &recordingDispatcher{}
	sid := uuid.MustParse("00000000-0000-0000-0000-000000000099")
	sessions := &sessionsFake{known: map[string]bool{sid.String(): true}}
	h := NewHookServer(disp, sessions)

	body := mustJSONBytes(t, hookNotifyRequest{
		SessionID: sid.String(),
		AgentKind: "claude-code",
		HookInput: json.RawMessage(`{"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"go?","options":[]}]}}`),
	})
	req := httptest.NewRequest("POST", "/atterm-hook/notify", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	disp.mu.Lock()
	defer disp.mu.Unlock()
	if len(disp.waiting) != 1 {
		t.Fatalf("expected 1 dispatch, got %d", len(disp.waiting))
	}
	if disp.waiting[0].QuestionText == "" {
		t.Fatalf("expected question text")
	}
}

func TestHookServer_ForwardsOptionsToDispatcher(t *testing.T) {
	disp := &recordingDispatcher{}
	sid := uuid.MustParse("00000000-0000-0000-0000-0000000000bb")
	sessions := &sessionsFake{known: map[string]bool{sid.String(): true}}
	h := NewHookServer(disp, sessions)

	body := mustJSONBytes(t, hookNotifyRequest{
		SessionID: sid.String(),
		AgentKind: "claude-code",
		HookInput: json.RawMessage(`{
		  "hook_event_name": "PreToolUse",
		  "tool_name": "AskUserQuestion",
		  "tool_input": {"questions": [{
		    "question": "Pick one",
		    "options": [
		      {"label":"A","description":"alpha"},
		      {"label":"B","description":"beta"}
		    ]
		  }]}
		}`),
	})
	req := httptest.NewRequest("POST", "/atterm-hook/notify", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	disp.mu.Lock()
	defer disp.mu.Unlock()
	if len(disp.waiting) != 1 {
		t.Fatalf("expected 1 dispatch, got %d", len(disp.waiting))
	}
	got := disp.waiting[0]
	if len(got.Options) != 2 || got.Options[0].Label != "A" || got.Options[1].Label != "B" {
		t.Fatalf("Options dropped on the way to dispatcher: %+v", got.Options)
	}
}

// TestHookServer_SetDispatcher verifies a runtime dispatcher swap (as happens
// on a relay login/logout) routes subsequent hook events to the new dispatcher
// without restarting the listener.
func TestHookServer_SetDispatcher(t *testing.T) {
	sid := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	sessions := &sessionsFake{known: map[string]bool{sid.String(): true}}
	first := &recordingDispatcher{}
	h := NewHookServer(first, sessions)

	post := func() {
		body := mustJSONBytes(t, hookNotifyRequest{
			SessionID: sid.String(),
			AgentKind: "claude-code",
			HookInput: json.RawMessage(`{"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_input":{"question":"go?"}}`),
		})
		req := httptest.NewRequest("POST", "/atterm-hook/notify", bytes.NewReader(body))
		req.RemoteAddr = "127.0.0.1:5555"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
	}

	post()
	second := &recordingDispatcher{}
	h.SetDispatcher(second)
	post()

	first.mu.Lock()
	defer first.mu.Unlock()
	second.mu.Lock()
	defer second.mu.Unlock()
	if len(first.waiting) != 1 {
		t.Fatalf("first dispatcher: want 1 event before swap, got %d", len(first.waiting))
	}
	if len(second.waiting) != 1 {
		t.Fatalf("second dispatcher: want 1 event after swap, got %d", len(second.waiting))
	}
}

func TestHookServer_UnknownSession(t *testing.T) {
	disp := &recordingDispatcher{}
	sessions := &sessionsFake{known: map[string]bool{}}
	h := NewHookServer(disp, sessions)

	body := mustJSONBytes(t, hookNotifyRequest{
		SessionID: uuid.New().String(),
		AgentKind: "claude-code",
		HookInput: json.RawMessage(`{}`),
	})
	req := httptest.NewRequest("POST", "/atterm-hook/notify", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestHookServer_RejectsRemoteAddr(t *testing.T) {
	h := NewHookServer(&recordingDispatcher{}, &sessionsFake{})
	req := httptest.NewRequest("POST", "/atterm-hook/notify", strings.NewReader("{}"))
	req.RemoteAddr = "10.0.0.1:5555"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestHookServer_OversizeBody(t *testing.T) {
	h := NewHookServer(&recordingDispatcher{}, &sessionsFake{})
	big := bytes.Repeat([]byte("x"), 130*1024)
	req := httptest.NewRequest("POST", "/atterm-hook/notify", bytes.NewReader(big))
	req.RemoteAddr = "127.0.0.1:5555"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestHookServer_BindLocalhost(t *testing.T) {
	h := NewHookServer(&recordingDispatcher{}, &sessionsFake{})
	addr, srv, err := h.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("expected loopback bind, got %q", host)
	}
	_ = atomic.AddInt32(new(int32), 1)
}

func TestHookServer_FiresSuspectOnUnknownAgentKind(t *testing.T) {
	disp := &recordingDispatcher{}
	sid := uuid.New()
	sessions := &sessionsFake{known: map[string]bool{sid.String(): true}}
	srv := NewHookServer(disp, sessions)

	var suspectCalled int
	srv.SetSuspectCallback(func() { suspectCalled++ })

	addr, server, err := srv.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown(context.Background())

	body := bytes.NewBufferString(`{"session_id":"` + sid.String() +
		`","agent_kind":"made-up","hook_input":{}}`)
	resp, err := http.Post("http://"+addr+"/atterm-hook/notify",
		"application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d; want 200", resp.StatusCode)
	}
	if suspectCalled != 1 {
		t.Errorf("suspectCalled = %d; want 1", suspectCalled)
	}
}

func mustJSONBytes(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type taskSinkFake struct {
	mu    sync.Mutex
	calls []string
}

func (f *taskSinkFake) ApplyHookTaskState(_ uuid.UUID, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, state)
}

func (f *taskSinkFake) states() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func postHook(t *testing.T, h *HookServer, sid uuid.UUID, agent, hookInput string) {
	t.Helper()
	body := mustJSONBytes(t, hookNotifyRequest{
		SessionID: sid.String(),
		AgentKind: agent,
		HookInput: json.RawMessage(hookInput),
	})
	req := httptest.NewRequest("POST", "/atterm-hook/notify", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	h.ServeHTTP(httptest.NewRecorder(), req)
}

func TestHookServer_ForwardsTaskState(t *testing.T) {
	sid := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	sessions := &sessionsFake{known: map[string]bool{sid.String(): true}}
	h := NewHookServer(&recordingDispatcher{}, sessions)
	sink := &taskSinkFake{}
	h.SetTaskStateSink(sink)

	postHook(t, h, sid, "claude-code", `{"hook_event_name":"Stop"}`)

	if got := sink.states(); len(got) != 1 || got[0] != "waiting_input" {
		t.Fatalf("sink states = %v, want [waiting_input]", got)
	}
}

// Events with no state meaning must not reach the sink at all — the sink is
// authoritative, so a spurious call would overwrite a real state.
func TestHookServer_IgnoresNonStateEvents(t *testing.T) {
	sid := uuid.MustParse("00000000-0000-0000-0000-0000000000bb")
	sessions := &sessionsFake{known: map[string]bool{sid.String(): true}}
	h := NewHookServer(&recordingDispatcher{}, sessions)
	sink := &taskSinkFake{}
	h.SetTaskStateSink(sink)

	postHook(t, h, sid, "claude-code", `{"hook_event_name":"SessionStart"}`)

	if got := sink.states(); len(got) != 0 {
		t.Fatalf("sink states = %v, want none", got)
	}
}

// A POST for a session this host does not know is rejected before any consumer
// runs; the task sink must not be an exception.
func TestHookServer_UnknownSessionReachesNoSink(t *testing.T) {
	sessions := &sessionsFake{known: map[string]bool{}}
	h := NewHookServer(&recordingDispatcher{}, sessions)
	sink := &taskSinkFake{}
	h.SetTaskStateSink(sink)

	postHook(t, h, uuid.New(), "claude-code", `{"hook_event_name":"Stop"}`)

	if got := sink.states(); len(got) != 0 {
		t.Fatalf("sink states = %v, want none", got)
	}
}

// Codex rides the same ingress; only the attention event name differs.
func TestHookServer_ForwardsCodexTaskState(t *testing.T) {
	sid := uuid.MustParse("00000000-0000-0000-0000-0000000000cc")
	sessions := &sessionsFake{known: map[string]bool{sid.String(): true}}
	h := NewHookServer(&recordingDispatcher{}, sessions)
	sink := &taskSinkFake{}
	h.SetTaskStateSink(sink)

	postHook(t, h, sid, "codex", `{"hook_event_name":"UserPromptSubmit"}`)
	postHook(t, h, sid, "codex", `{"hook_event_name":"PermissionRequest"}`)

	want := []string{"running", "waiting_input"}
	got := sink.states()
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("sink states = %v, want %v", got, want)
	}
}
