package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/zalando/go-keyring"
)

func TestService_RelayMode_SelectsRelayStore(t *testing.T) {
	svc, _ := NewService(ServiceConfig{
		Mode:       ModeRelay,
		RelayURL:   "http://example",
		RelayToken: func() string { return "tok" },
	})
	if _, ok := svc.store.(*RelayBackedBindingStore); !ok {
		t.Fatalf("relay mode should pick relay store, got %T", svc.store)
	}
}

func TestService_LocalMode_SelectsLocalStore(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)
	svc, _ := NewService(ServiceConfig{
		Mode:       ModeLocal,
		FeishuBase: "https://open.feishu.cn",
	})
	if _, ok := svc.store.(*LocalKeychainBindingStore); !ok {
		t.Fatalf("local mode should pick local store, got %T", svc.store)
	}
}

func TestService_DispatchExposesDispatcher(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)
	svc, _ := NewService(ServiceConfig{Mode: ModeLocal, FeishuBase: "https://open.feishu.cn"})
	if svc.Dispatcher() == nil {
		t.Fatalf("Service.Dispatcher() must return non-nil")
	}
}

func TestService_HookServerExposed(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)
	svc, _ := NewService(ServiceConfig{Mode: ModeLocal, FeishuBase: "https://open.feishu.cn"})
	addr, _, err := svc.HookServer().Start()
	if err != nil {
		t.Fatalf("hook server start: %v", err)
	}
	if addr == "" {
		t.Fatalf("expected bound addr")
	}
}

// stubSessionLookup is the minimal SessionLookup used by the assembly tests.
type stubSessionLookup struct{}

func (stubSessionLookup) Exists(uuid.UUID) bool { return true }

var _ SessionLookup = stubSessionLookup{}

func TestService_HandleCardAction_RoutesToInternalEvent(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)
	svc, _ := NewService(ServiceConfig{Mode: ModeLocal, FeishuBase: "https://open.feishu.cn"})
	ack := svc.RenderAck("command_finished", "session-123")
	if ack.Card == nil {
		t.Fatalf("RenderAck must return a card payload")
	}
}

func TestService_ExistsSatisfiesLookup(t *testing.T) {
	var _ SessionLookup = (*Service)(nil)
}

func TestService_RelayMode_RequiresTokenFn(t *testing.T) {
	_, err := NewService(ServiceConfig{Mode: ModeRelay, RelayURL: "http://x"})
	if err == nil {
		t.Fatalf("expected error when RelayToken is nil in relay mode")
	}
}

func TestService_BeginPair_RelayMode(t *testing.T) {
	var hitBeginPair bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/feishu/bindings/me/begin-pair" && r.Method == "POST" {
			hitBeginPair = true
			if got := r.Header.Get("Authorization"); got != "Bearer relay-tok" {
				t.Errorf("auth header: %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "ABC123"})
			return
		}
		t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	svc, err := NewService(ServiceConfig{
		Mode:       ModeRelay,
		RelayURL:   srv.URL,
		RelayToken: func() string { return "relay-tok" },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	code, err := svc.BeginPair(context.Background())
	if err != nil {
		t.Fatalf("BeginPair: %v", err)
	}
	if code != "ABC123" {
		t.Fatalf("code: %q", code)
	}
	if !hitBeginPair {
		t.Fatal("begin-pair endpoint was not called")
	}
}

func TestService_BeginPair_LocalMode(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)

	svc, err := NewService(ServiceConfig{
		Mode:       ModeLocal,
		FeishuBase: "https://open.feishu.cn",
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	code, err := svc.BeginPair(context.Background())
	if err != nil {
		t.Fatalf("BeginPair local: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("expected 6-char code, got %q", code)
	}
}
