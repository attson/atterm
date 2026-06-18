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
}

func (r *recordingDispatcher) DispatchWaitingInput(ctx context.Context, ev WaitingInputDispatchEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.waiting = append(r.waiting, ev)
}

type sessionsFake struct {
	known map[string]bool
}

func (f *sessionsFake) Exists(sid uuid.UUID) bool { return f.known[sid.String()] }

func TestHookServer_HappyPath(t *testing.T) {
	disp := &recordingDispatcher{}
	sid := uuid.MustParse("00000000-0000-0000-0000-000000000099")
	sessions := &sessionsFake{known: map[string]bool{sid.String(): true}}
	h := NewHookServer(disp, sessions)

	body := mustJSONBytes(t, hookNotifyRequest{
		SessionID: sid.String(),
		AgentKind: "claude-code",
		HookInput: json.RawMessage(`{"matcher":{"type":"idle_prompt","tool":"AskUserQuestion"},"prompt_id":"p","context":{"tool_input":{"question":"go?"}}}`),
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
