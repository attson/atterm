package feishu

import (
	"context"
	"encoding/json"
	"errors"
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

func (stubSessionLookup) Exists(uuid.UUID) bool          { return true }
func (stubSessionLookup) Inject(uuid.UUID, string) error { return nil }

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

type fakeInjector struct {
	gotSID  uuid.UUID
	gotText string
	err     error
}

func (f *fakeInjector) Exists(uuid.UUID) bool { return true }
func (f *fakeInjector) Inject(sid uuid.UUID, text string) error {
	f.gotSID, f.gotText = sid, text
	return f.err
}

func TestHandleReplyMessage_InjectsToMappedSession(t *testing.T) {
	inj := &fakeInjector{}
	d := NewDispatcher(DispatcherConfig{})
	sid := uuid.New()
	d.cardMsgs.remember("om_card1", sid)
	s := &Service{cfg: ServiceConfig{Sessions: inj}, dispatcher: d}

	s.handleReplyMessage(context.Background(), "ou_op", "om_card1", "looks good")

	if inj.gotSID != sid {
		t.Fatalf("inject sid = %s, want %s", inj.gotSID, sid)
	}
	if inj.gotText != "looks good\n" {
		t.Fatalf("inject text = %q, want %q", inj.gotText, "looks good\n")
	}
}

func TestHandleReplyMessage_UnknownParentIgnored(t *testing.T) {
	inj := &fakeInjector{}
	d := NewDispatcher(DispatcherConfig{})
	s := &Service{cfg: ServiceConfig{Sessions: inj}, dispatcher: d}

	s.handleReplyMessage(context.Background(), "ou_op", "om_unknown", "hi")
	if inj.gotText != "" {
		t.Fatalf("unmapped parent must not inject, got %q", inj.gotText)
	}
}

func TestHandleReplyMessage_BindCommandIgnored(t *testing.T) {
	inj := &fakeInjector{}
	d := NewDispatcher(DispatcherConfig{})
	d.cardMsgs.remember("om_card1", uuid.New())
	s := &Service{cfg: ServiceConfig{Sessions: inj}, dispatcher: d}

	s.handleReplyMessage(context.Background(), "ou_op", "om_card1", "/bind ABC123")
	if inj.gotText != "" {
		t.Fatalf("/bind must not be injected as a reply, got %q", inj.gotText)
	}
}

func TestHandleReplyMessage_EmptyParentOrTextIgnored(t *testing.T) {
	inj := &fakeInjector{}
	d := NewDispatcher(DispatcherConfig{})
	sid := uuid.New()
	d.cardMsgs.remember("om_card1", sid)
	s := &Service{cfg: ServiceConfig{Sessions: inj}, dispatcher: d}

	s.handleReplyMessage(context.Background(), "ou_op", "", "looks good")
	if inj.gotText != "" {
		t.Fatalf("empty parent must not inject, got %q", inj.gotText)
	}
	s.handleReplyMessage(context.Background(), "ou_op", "om_card1", "   ")
	if inj.gotText != "" {
		t.Fatalf("blank text must not inject, got %q", inj.gotText)
	}
}

func TestHandleCardAction_InjectWritesText(t *testing.T) {
	inj := &fakeInjector{}
	s := &Service{cfg: ServiceConfig{Sessions: inj}}
	sid := uuid.New()
	s.handleCardAction(context.Background(), sid.String(), "inject", "", "1\n")
	if inj.gotSID != sid || inj.gotText != "1\n" {
		t.Fatalf("inject got sid=%s text=%q", inj.gotSID, inj.gotText)
	}
}

func TestHandleCardAction_NonInjectIgnored(t *testing.T) {
	inj := &fakeInjector{}
	s := &Service{cfg: ServiceConfig{Sessions: inj}}
	s.handleCardAction(context.Background(), uuid.New().String(), "ack", "command_finished", "")
	if inj.gotText != "" {
		t.Fatalf("ack should not inject, got %q", inj.gotText)
	}
}

func TestHandleCardAction_InjectErrorDoesNotPanic(t *testing.T) {
	inj := &fakeInjector{err: errors.New("inbound full")}
	s := &Service{cfg: ServiceConfig{Sessions: inj}}
	// 不应 panic;错误被 log 吞掉。
	s.handleCardAction(context.Background(), uuid.New().String(), "inject", "", "x\n")
	if inj.gotText != "x\n" {
		t.Fatalf("inject should still be attempted, got %q", inj.gotText)
	}
}

func TestHandleCardAction_InvalidSessionIDIgnored(t *testing.T) {
	inj := &fakeInjector{}
	s := &Service{cfg: ServiceConfig{Sessions: inj}}
	s.handleCardAction(context.Background(), "not-a-uuid", "inject", "", "x\n")
	if inj.gotText != "" {
		t.Fatalf("invalid uuid must not inject, got %q", inj.gotText)
	}
}

func TestHandleCardAction_EmptyTextIgnored(t *testing.T) {
	inj := &fakeInjector{}
	s := &Service{cfg: ServiceConfig{Sessions: inj}}
	s.handleCardAction(context.Background(), uuid.New().String(), "inject", "", "")
	if inj.gotText != "" {
		t.Fatalf("empty text must not inject, got %q", inj.gotText)
	}
}
